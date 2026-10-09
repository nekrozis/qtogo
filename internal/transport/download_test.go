package transport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

func sha256hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func assertNoFile(t *testing.T, dir, name string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
		t.Errorf("%s exists, want it not to", name)
	}
}

func TestDownloadVerifiesAndRenames(t *testing.T) {
	const body = "archive bytes"
	sum := sha256hex(body)
	official := fileServer(t, map[string]string{"/qt/x.7z.sha256": sum + "  x.7z"}, nil)
	source := fileServer(t, map[string]string{"/qt/x.7z": body}, nil)

	c, err := newClient(Config{Sources: []string{source.URL}, Official: official.URL}, trustAll(official, source))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	got, err := c.Download(context.Background(), "qt/x.7z", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Digest != sum || got.Size != int64(len(body)) {
		t.Errorf("Downloaded = %+v, want digest %s and %d bytes", got, sum, len(body))
	}
	blob, err := os.ReadFile(filepath.Join(dir, "x.7z"))
	if err != nil {
		t.Fatal(err)
	}
	if string(blob) != body {
		t.Errorf("file = %q, want %q", blob, body)
	}
	assertNoFile(t, dir, "x.7z.part")
}

func TestDownloadMismatchLeavesNothingBehind(t *testing.T) {
	official := fileServer(t, map[string]string{"/qt/x.7z.sha256": sha256hex("expected")}, nil)
	source := fileServer(t, map[string]string{"/qt/x.7z": "something else"}, nil)

	c, err := newClient(Config{Sources: []string{source.URL}, Official: official.URL}, trustAll(official, source))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_, err = c.Download(context.Background(), "qt/x.7z", dir)
	assertFailure(t, err, exitcode.Integrity, errs.CodeChecksumMismatch)
	assertNoFile(t, dir, "x.7z")
	assertNoFile(t, dir, "x.7z.part")
}

func TestDownloadRefusesWhenTheDigestIsMissing(t *testing.T) {
	official := fileServer(t, map[string]string{}, nil) // the sidecar is a 404
	fetched := &hits{}
	source := fileServer(t, map[string]string{"/qt/x.7z": "bytes"}, fetched)

	c, err := newClient(Config{Sources: []string{source.URL}, Official: official.URL}, trustAll(official, source))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_, err = c.Download(context.Background(), "qt/x.7z", dir)
	assertFailure(t, err, exitcode.Integrity, errs.CodeChecksumMissing)
	if fetched.count() != 0 {
		t.Errorf("the archive was fetched %d times before its digest was known", fetched.count())
	}
}

func TestDownloadTrustBaseChecksum(t *testing.T) {
	const body = "bytes"
	official := fileServer(t, map[string]string{}, nil) // the official endpoint cannot answer
	source := fileServer(t, map[string]string{
		"/qt/x.7z":        body,
		"/qt/x.7z.sha256": sha256hex(body),
	}, nil)

	plain, err := newClient(Config{Sources: []string{source.URL}, Official: official.URL}, trustAll(official, source))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plain.Download(context.Background(), "qt/x.7z", t.TempDir()); err == nil {
		t.Error("a private base's digest was trusted without the opt-in")
	}

	trusting, err := newClient(
		Config{Sources: []string{source.URL}, Official: official.URL, TrustBaseChecksum: true},
		trustAll(official, source))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trusting.Download(context.Background(), "qt/x.7z", t.TempDir()); err != nil {
		t.Fatalf("with trust-base-checksum: %v", err)
	}
}

func TestDownloadDoesNotFollowADigestRedirect(t *testing.T) {
	const body = "bytes"
	reached := &hits{}
	elsewhere := fileServer(t, map[string]string{"/qt/x.7z.sha256": sha256hex(body)}, reached)
	official := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(official.Close)
	source := fileServer(t, map[string]string{"/qt/x.7z": body}, nil)

	c, err := newClient(Config{Sources: []string{source.URL}, Official: official.URL}, trustAll(official, source, elsewhere))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Download(context.Background(), "qt/x.7z", t.TempDir())
	assertFailure(t, err, exitcode.Integrity, errs.CodeDigestRedirected)
	if reached.count() != 0 {
		t.Errorf("the redirect target was contacted %d times for a checksum", reached.count())
	}
}

func TestDownloadFollowsAnArchiveRedirect(t *testing.T) {
	const body = "bytes"
	official := fileServer(t, map[string]string{"/qt/x.7z.sha256": sha256hex(body)}, nil)
	mirror := fileServer(t, map[string]string{"/qt/x.7z": body}, nil)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mirror.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	c, err := newClient(Config{Sources: []string{origin.URL}, Official: official.URL}, trustAll(official, origin, mirror))
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Download(context.Background(), "qt/x.7z", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.URL, mirror.URL) {
		t.Errorf("URL = %q, want the mirror", got.URL)
	}
}

func TestDownloadRefusesAnArchiveDowngrade(t *testing.T) {
	const body = "bytes"
	official := fileServer(t, map[string]string{"/qt/x.7z.sha256": sha256hex(body)}, nil)
	plain := &hits{}
	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plain.inc()
		_, _ = io.WriteString(w, body)
	}))
	defer plainSrv.Close()
	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plainSrv.URL+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(secure.Close)

	c, err := newClient(Config{Sources: []string{secure.URL}, Official: official.URL}, trustAll(official, secure))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Download(context.Background(), "qt/x.7z", t.TempDir())
	assertFailure(t, err, exitcode.Integrity, errs.CodeSchemeDowngrade)
	if plain.count() != 0 {
		t.Errorf("the http host was contacted %d times after the downgrade was refused", plain.count())
	}
}

func TestDownloadCleansUpAfterATruncatedBody(t *testing.T) {
	official := fileServer(t, map[string]string{"/qt/x.7z.sha256": sha256hex("never arrives")}, nil)
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = io.WriteString(w, "short")
	}))
	t.Cleanup(source.Close)

	c, err := newClient(Config{Sources: []string{source.URL}, Official: official.URL}, trustAll(official, source))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_, err = c.Download(context.Background(), "qt/x.7z", dir)
	assertFailure(t, err, exitcode.Network, errs.CodeSourcesExhausted)
	assertNoFile(t, dir, "x.7z")
	assertNoFile(t, dir, "x.7z.part")
}

func TestDownloadPublishesOnlyWhenComplete(t *testing.T) {
	const body = "0123456789"
	official := fileServer(t, map[string]string{"/qt/x.7z.sha256": sha256hex(body)}, nil)
	release := make(chan struct{})
	started := make(chan struct{})
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body[:5])
		w.(http.Flusher).Flush()
		close(started)
		<-release
		_, _ = io.WriteString(w, body[5:])
	}))
	t.Cleanup(source.Close)

	c, err := newClient(Config{Sources: []string{source.URL}, Official: official.URL}, trustAll(official, source))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	done := make(chan error, 1)
	go func() {
		_, err := c.Download(context.Background(), "qt/x.7z", dir)
		done <- err
	}()

	<-started
	if _, err := os.Stat(filepath.Join(dir, "x.7z")); !os.IsNotExist(err) {
		t.Error("the final name appeared before the bytes were complete")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "x.7z")); err != nil {
		t.Errorf("the final name is missing after a complete download: %v", err)
	}
}

func TestDownloadCancellationLeavesNothingBehind(t *testing.T) {
	const body = "0123456789"
	official := fileServer(t, map[string]string{"/qt/x.7z.sha256": sha256hex(body)}, nil)
	blocked := make(chan struct{})
	started := make(chan struct{})
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = io.WriteString(w, body[:1])
		w.(http.Flusher).Flush()
		close(started)
		<-blocked
	}))
	t.Cleanup(source.Close)

	c, err := newClient(Config{Sources: []string{source.URL}, Official: official.URL}, trustAll(official, source))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err = c.Download(ctx, "qt/x.7z", dir)
	close(blocked)
	assertFailure(t, err, exitcode.Interrupted, errs.CodeInterrupted)
	assertNoFile(t, dir, "x.7z")
	assertNoFile(t, dir, "x.7z.part")
}

func TestDownloadRetriesTransientFailures(t *testing.T) {
	const body = "bytes"
	official := fileServer(t, map[string]string{"/qt/x.7z.sha256": sha256hex(body)}, nil)
	attempts := &hits{}
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.inc()
		if attempts.count() < 3 {
			http.Error(w, "later", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(source.Close)

	c, err := newClient(Config{Sources: []string{source.URL}, Official: official.URL}, trustAll(official, source))
	if err != nil {
		t.Fatal(err)
	}
	var slept []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	if _, err := c.Download(context.Background(), "qt/x.7z", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(slept) != 2 {
		t.Errorf("backed off %d times, want 2 (two failures before the success)", len(slept))
	}
}

func TestDownloadSwitchesSourceOnAMismatch(t *testing.T) {
	const good = "the right bytes"
	official := fileServer(t, map[string]string{"/qt/x.7z.sha256": sha256hex(good)}, nil)
	corrupt := &hits{}
	first := fileServer(t, map[string]string{"/qt/x.7z": "corrupt"}, corrupt)
	second := fileServer(t, map[string]string{"/qt/x.7z": good}, nil)

	c, err := newClient(
		Config{Sources: []string{first.URL, second.URL}, Official: official.URL},
		trustAll(official, first, second))
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Download(context.Background(), "qt/x.7z", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.URL, second.URL) {
		t.Errorf("URL = %q, want the second source", got.URL)
	}
	if corrupt.count() != 1 {
		t.Errorf("the corrupt source was tried %d times, want exactly once (no retry of a mismatched URL)", corrupt.count())
	}
}
