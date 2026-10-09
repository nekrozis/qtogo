package transport

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

func TestNewRefusesInsecureSourceBeforeAnyRequest(t *testing.T) {
	reached := &hits{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.inc()
		http.Error(w, "reached", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := New(Config{Sources: []string{srv.URL}})
	assertFailure(t, err, exitcode.Usage, errs.CodeInsecureScheme)
	if reached.count() != 0 {
		t.Errorf("the refused source was contacted %d times", reached.count())
	}
}

func TestNewRefusesSchemesThatAreNotHTTP(t *testing.T) {
	_, err := New(Config{Sources: []string{"ftp://example.invalid/qt"}})
	assertFailure(t, err, exitcode.Usage, errs.CodeUnsupportedScheme)

	_, err = New(Config{Sources: []string{"https://"}})
	assertFailure(t, err, exitcode.Usage, errs.CodeUnsupportedScheme)
}

func TestAllowInsecureHTTPWarnsAndWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	var warn bytes.Buffer
	c, err := New(Config{Sources: []string{srv.URL}, AllowInsecureHTTP: true, Warn: &warn})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warn.String(), srv.URL) {
		t.Errorf("no insecure-http warning was written: %q", warn.String())
	}
	if _, err := c.Get(context.Background(), "listing/"); err != nil {
		t.Fatalf("Get over the allowed http source: %v", err)
	}
}

func TestWeakAlgorithmNeedsTheOptIn(t *testing.T) {
	_, err := New(Config{Algorithm: "sha1"})
	assertFailure(t, err, exitcode.Integrity, errs.CodeChecksumWeak)

	if _, err := New(Config{Algorithm: "sha1", AllowWeakChecksum: true}); err != nil {
		t.Fatalf("with the opt-in: %v", err)
	}
}

func TestUnknownAlgorithmIsAUsageError(t *testing.T) {
	_, err := New(Config{Algorithm: "crc32"})
	assertFailure(t, err, exitcode.Usage, errs.CodeUnexpectedValue)
}

func TestDefaultIsTheOfficialEndpointAlone(t *testing.T) {
	c, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.official.String(); got != DefaultOfficial {
		t.Errorf("official = %q, want %q", got, DefaultOfficial)
	}
	if len(c.sources) != 1 || c.sources[0].String() != DefaultOfficial {
		t.Errorf("sources = %v, want the official endpoint alone", c.sources)
	}
	if c.alg != algSHA256 {
		t.Errorf("algorithm = %q, want sha256", c.alg)
	}
}
