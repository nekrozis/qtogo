package transport

import (
	"net/http"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// hopLimit is the most redirects one request may follow. net/http's own default
// is 10, but this layer enforces it so the count itself is a rule rather than a
// library default.
const hopLimit = 10

// redirectPolicy says what a redirect may change. The two requests a download
// makes have different rules, so each Client carries one policy as a static
// property rather than reading it from the request context, where a caller could
// forget to set it.
type redirectPolicy uint8

const (
	// followAnywhere is for listings, metadata and archives: the official endpoint
	// itself redirects archive bytes to a nearby mirror it names, so a new host is
	// expected. The bytes are verified against the digest regardless.
	followAnywhere redirectPolicy = iota
	// stayOnHost is for the checksum request, which is the root of trust: a
	// redirect to another host would let a mirror hand back the expected value.
	stayOnHost
)

// checkRedirect is the Client.CheckRedirect for one policy. It is a pure function
// so its rules can be tested and fuzzed without a server.
func checkRedirect(policy redirectPolicy, req *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) > hopLimit {
		return errs.New(exitcode.Network, errs.CodeRedirectLimit, errs.PhaseDownload,
			"stopped after %d redirects", hopLimit)
	}
	prev := via[len(via)-1]
	if prev.URL == nil || req.URL == nil {
		return errs.New(exitcode.Network, errs.CodeRequestFailed, errs.PhaseDownload, "malformed redirect")
	}
	// A downgrade is refused on every request, including one a mirror issued: it is
	// the one hop that would defeat verifying the bytes.
	if prev.URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errs.New(exitcode.Integrity, errs.CodeSchemeDowngrade, errs.PhaseDownload,
			"redirect from %s to %s would drop https", prev.URL.Scheme, req.URL.Scheme)
	}
	if policy == stayOnHost && req.URL.Host != via[0].URL.Host {
		return errs.New(exitcode.Integrity, errs.CodeDigestRedirected, errs.PhaseVerify,
			"checksum request for %s was redirected to another host, %s", via[0].URL.Host, req.URL.Host)
	}
	return nil
}
