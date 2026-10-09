package service

import (
	"context"
	"errors"
	"path"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/transport"
)

const desktop = "online/qtsdkrepository/windows_x86/desktop"

// memFetcher serves bodies by repository-relative path.
type memFetcher map[string]string

func (m memFetcher) Get(_ context.Context, p string) (transport.Document, error) {
	body, ok := m[path.Clean(p)]
	if !ok {
		return transport.Document{}, errs.New(exitcode.NotFound, errs.CodeHTTPNotFound,
			errs.PhaseDownload, "%s: not found", p)
	}
	return transport.Document{URL: "https://example.invalid/" + strings.TrimSuffix(p, "/"), Body: []byte(body)}, nil
}

// targetIndex is a target directory's listing, naming the children given.
func targetIndex(children ...string) string {
	var b strings.Builder
	b.WriteString(`<html><head><title>Index of /` + desktop + `</title></head><body>`)
	b.WriteString(`<h1>Index of /` + desktop + `</h1>`)
	for _, c := range children {
		b.WriteString(`<a href="` + c + `/">` + c + `/</a>`)
	}
	b.WriteString(`</body></html>`)
	return b.String()
}

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

func TestListQtVersionsOrdersAndDeduplicates(t *testing.T) {
	// The repository's order is not the version order, and one name appears twice.
	index := targetIndex("qt6_680", "qt5_5152", "qt6_6110", "qt6_6110")
	s := New(memFetcher{desktop: index})

	got, err := s.ListQtVersions(context.Background(), model.HostWindows, model.KindDesktop)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(got))
	for i, v := range got {
		names[i] = v.Dotted()
	}
	if want := "5.15.2,6.8.0,6.11.0"; strings.Join(names, ",") != want {
		t.Errorf("versions = %v, want %s", names, want)
	}
}

func TestListQtVersionsReportsAnEmptyTarget(t *testing.T) {
	s := New(memFetcher{desktop: targetIndex()})

	_, err := s.ListQtVersions(context.Background(), model.HostWindows, model.KindDesktop)
	assertFailure(t, err, exitcode.NotFound, errs.CodeVersionNotFound)
}

func TestListQtVersionsRefusesACrossTarget(t *testing.T) {
	s := New(memFetcher{})

	_, err := s.ListQtVersions(context.Background(), model.HostAll, model.KindAndroid)
	assertFailure(t, err, exitcode.Usage, errs.CodeNotEnabled)
}
