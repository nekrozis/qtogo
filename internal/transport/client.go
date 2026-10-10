package transport

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// DefaultOfficial is the root of trust: the endpoint the expected digest is read
// from, kept separate from wherever the bytes come from (ADR-007 decision 3).
const DefaultOfficial = "https://download.qt.io"

const (
	defaultAttempts = 3
	defaultBase     = 500 * time.Millisecond
	defaultMax      = 30 * time.Second
)

// Retry bounds the backoff between attempts on one URL.
type Retry struct {
	// Attempts is how many times one URL is tried; zero means the default.
	Attempts int
	// Base is the first backoff; zero means the default.
	Base time.Duration
	// Max caps the backoff; zero means the default.
	Max time.Duration
}

// Config builds a Client. Its zero value is sensible: no Sources means the
// official endpoint alone, and the algorithm is sha256.
type Config struct {
	// Sources are the roots to fetch from, in order: position is priority. An
	// empty list means Official alone. There is no built-in mirror list, and the
	// choice is never random (ADR-007 decision 2).
	Sources []string
	// Official is the endpoint the expected digest comes from; empty means
	// DefaultOfficial.
	Official string
	// AllowInsecureHTTP permits an http:// root, warning for each. Without it an
	// http root is refused before any request.
	AllowInsecureHTTP bool
	// AllowWeakChecksum permits sha1 and md5.
	AllowWeakChecksum bool
	// TrustBaseChecksum uses a source's own digest when the official endpoint
	// cannot answer for a path — a private base it does not know.
	TrustBaseChecksum bool
	// Algorithm is "sha256" (the default), "sha1" or "md5".
	Algorithm string
	Retry     Retry
	// Timeout bounds one small-object request (a listing, Updates.xml, a sidecar).
	// An archive has no total timeout; give Download a context with a deadline.
	Timeout time.Duration
	// Warn receives the insecure-http warning, if any. nil discards it.
	Warn io.Writer
}

// Client fetches from a Qt repository under ADR-007's policy. It is safe for
// concurrent use; it holds no worker pool of its own.
type Client struct {
	sources  []*url.URL
	official *url.URL
	alg      algorithm
	retry    Retry
	trust    bool
	sleep    sleeper

	digest  *http.Client // the digest request stays on the official host
	archive *http.Client // archive bytes may be redirected to a mirror
	small   *http.Client // listings and metadata; same redirect rule as archives
}

// New builds a Client from cfg, refusing an insecure or unsupported source
// scheme before any request is made (ADR-007 decision 8: exit code 2).
func New(cfg Config) (*Client, error) {
	return newClient(cfg, defaultTransport())
}

// NewWithTransport builds a Client that routes through rt instead of the default
// transport. It is for a caller that has its own — a test trusting a server's
// certificate, or an embedding program with a configured pool.
func NewWithTransport(cfg Config, rt http.RoundTripper) (*Client, error) {
	return newClient(cfg, rt)
}

// newClient is New with the transport supplied, so a test can hand in an
// httptest server's TLS transport.
func newClient(cfg Config, rt http.RoundTripper) (*Client, error) {
	alg, err := parseAlgorithm(cfg.Algorithm)
	if err != nil {
		return nil, err
	}
	if alg.weak() && !cfg.AllowWeakChecksum {
		return nil, errs.New(exitcode.Integrity, errs.CodeChecksumWeak, errs.PhaseConfig,
			"checksum algorithm %q is weak; pass --allow-weak-checksum to accept it", alg)
	}

	official, err := officialURL(cfg.Official, cfg.AllowInsecureHTTP, cfg.Warn)
	if err != nil {
		return nil, err
	}

	raw := cfg.Sources
	if len(raw) == 0 {
		raw = []string{official.String()}
	}
	sources := make([]*url.URL, 0, len(raw))
	for _, s := range raw {
		u, err := checkScheme(s, cfg.AllowInsecureHTTP, "a source", cfg.Warn)
		if err != nil {
			return nil, err
		}
		sources = append(sources, u)
	}

	retry := cfg.Retry
	if retry.Attempts <= 0 {
		retry.Attempts = defaultAttempts
	}
	if retry.Base <= 0 {
		retry.Base = defaultBase
	}
	if retry.Max <= 0 {
		retry.Max = defaultMax
	}

	follow := func(req *http.Request, via []*http.Request) error { return checkRedirect(followAnywhere, req, via) }
	stay := func(req *http.Request, via []*http.Request) error { return checkRedirect(stayOnHost, req, via) }

	return &Client{
		sources:  sources,
		official: official,
		alg:      alg,
		retry:    retry,
		trust:    cfg.TrustBaseChecksum,
		sleep:    realSleep,
		digest:   &http.Client{Transport: rt, Timeout: cfg.Timeout, CheckRedirect: stay},
		archive:  &http.Client{Transport: rt, CheckRedirect: follow},
		small:    &http.Client{Transport: rt, Timeout: cfg.Timeout, CheckRedirect: follow},
	}, nil
}

// officialURL resolves the digest endpoint, defaulting when unset.
func officialURL(raw string, allowInsecure bool, warn io.Writer) (*url.URL, error) {
	if strings.TrimSpace(raw) == "" {
		raw = DefaultOfficial
	}
	return checkScheme(raw, allowInsecure, "the official endpoint", warn)
}

// checkScheme parses a root and refuses a scheme this policy does not accept. It
// is the pre-flight half of ADR-007: what it rejects is a usage error (2), found
// before a socket is opened.
func checkScheme(raw string, allowInsecure bool, what string, warn io.Writer) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errs.Wrap(exitcode.Usage, errs.CodeUnsupportedScheme, errs.PhaseConfig, err,
			"%s %q is not a URL", what, raw)
	}
	switch {
	case u.Host == "":
		return nil, errs.New(exitcode.Usage, errs.CodeUnsupportedScheme, errs.PhaseConfig,
			"%s %q has no host", what, raw)
	case u.Scheme == "https":
		return u, nil
	case u.Scheme == "http":
		if !allowInsecure {
			return nil, errs.New(exitcode.Usage, errs.CodeInsecureScheme, errs.PhaseConfig,
				"%s %q is http; pass --allow-insecure-http to allow it", what, raw)
		}
		warnf(warn, "warning: %s %q is http; the connection is not encrypted\n", what, raw)
		return u, nil
	default:
		return nil, errs.New(exitcode.Usage, errs.CodeUnsupportedScheme, errs.PhaseConfig,
			"%s %q uses scheme %q; only https is accepted", what, raw, u.Scheme)
	}
}

// warnf writes a warning if a writer was given.
func warnf(w io.Writer, format string, args ...any) {
	if w != nil {
		_, _ = fmt.Fprintf(w, format, args...)
	}
}

// join appends a repository-relative path to a root. The path is Qt's own and is
// taken literally; the root keeps its own path prefix (a mirror such as
// https://host/qtproject).
func join(base *url.URL, path string) string {
	return strings.TrimRight(base.String(), "/") + "/" + strings.TrimLeft(path, "/")
}

// defaultTransport is one connection-pooled transport shared by every request the
// Client makes. The timeouts bound reaching a server and its headers, not the
// body of an archive, which can legitimately take a long time.
func defaultTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
}
