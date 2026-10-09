package transport

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
)

// assertFailure checks the exit code and machine code ADR-002 promises.
func assertFailure(t *testing.T, err error, wantExit int, wantCode string) {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("error = %v, want an *errs.Error", err)
	}
	if e.ExitCode() != wantExit || e.Code() != wantCode {
		t.Errorf("failure = exit %d, code %q; want exit %d, code %q",
			e.ExitCode(), e.Code(), wantExit, wantCode)
	}
}

// hits counts requests, so a test can prove one was never made.
type hits struct {
	mu sync.Mutex
	n  int
}

func (h *hits) inc() { h.mu.Lock(); h.n++; h.mu.Unlock() }

func (h *hits) count() int { h.mu.Lock(); defer h.mu.Unlock(); return h.n }

// trustAll is a transport that trusts every test server's certificate, so two
// servers can stand in for the official endpoint and a mirror.
func trustAll(servers ...*httptest.Server) *http.Transport {
	pool := x509.NewCertPool()
	for _, s := range servers {
		pool.AddCert(s.Certificate())
	}
	return &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
}

// fileServer serves a fixed set of paths over TLS and 404s the rest. It counts
// requests when a counter is supplied.
func fileServer(t *testing.T, files map[string]string, counter *hits) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if counter != nil {
			counter.inc()
		}
		body, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}
