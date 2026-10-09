package transport

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/repository"
)

const listing = `<html><head><title>Index of /qt/</title></head><body>
<a href="child/">child/</a>
</body></html>`

func TestGetUsesTheFirstSourceThatAnswers(t *testing.T) {
	first := fileServer(t, map[string]string{}, nil) // everything is a 404
	second := fileServer(t, map[string]string{"/qt/": listing}, nil)

	c, err := newClient(Config{Sources: []string{first.URL, second.URL}}, trustAll(first, second))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := c.Get(context.Background(), "qt/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc.URL, second.URL) {
		t.Errorf("URL = %q, want the second source", doc.URL)
	}
	if string(doc.Body) != listing {
		t.Errorf("body = %q", doc.Body)
	}
}

func TestGetFeedsTheRepositoryParser(t *testing.T) {
	srv := fileServer(t, map[string]string{"/qt/": listing}, nil)
	c, err := newClient(Config{Sources: []string{srv.URL}}, trustAll(srv))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := c.Get(context.Background(), "qt/")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := repository.ParseIndex(doc.URL, doc.Body)
	if err != nil {
		t.Fatalf("ParseIndex on what Get returned: %v", err)
	}
	if len(entries) != 1 || entries[0].URL != doc.URL+"child/" {
		t.Errorf("entries = %+v, want one child resolved against %q", entries, doc.URL)
	}
}

func TestGetFollowsACrossHostRedirect(t *testing.T) {
	mirror := fileServer(t, map[string]string{"/moved/": listing}, nil)
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mirror.URL+"/moved/", http.StatusFound)
	}))
	t.Cleanup(origin.Close)

	c, err := newClient(Config{Sources: []string{origin.URL}}, trustAll(origin, mirror))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := c.Get(context.Background(), "qt/")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(doc.URL, mirror.URL) {
		t.Errorf("URL = %q, want the mirror", doc.URL)
	}
}

func TestGetRefusesADowngrade(t *testing.T) {
	plain := &hits{}
	plainSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plain.inc()
		_, _ = io.WriteString(w, listing)
	}))
	defer plainSrv.Close()

	secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plainSrv.URL+"/qt/", http.StatusFound)
	}))
	t.Cleanup(secure.Close)

	c, err := newClient(Config{Sources: []string{secure.URL}}, trustAll(secure))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Get(context.Background(), "qt/")
	assertFailure(t, err, exitcode.Integrity, errs.CodeSchemeDowngrade)
	if plain.count() != 0 {
		t.Errorf("the http host was contacted %d times after the downgrade was refused", plain.count())
	}
}

func TestGetStopsAtTheHopLimit(t *testing.T) {
	reached := &hits{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.inc()
		http.Redirect(w, r, "/qt/", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	c, err := newClient(Config{Sources: []string{srv.URL}}, trustAll(srv))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Get(context.Background(), "qt/")
	assertFailure(t, err, exitcode.Network, errs.CodeRedirectLimit)
	if got := reached.count(); got != hopLimit+1 {
		t.Errorf("the server saw %d requests, want %d", got, hopLimit+1)
	}
}
