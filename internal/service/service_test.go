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

// planFixture wires a target with one flat version directory whose leaf holds an
// Updates.xml naming a base package.
func planFixture() memFetcher {
	const leaf = desktop + "/qt6_680"
	updates := `<Updates><PackageUpdate><Name>qt.qt6.680.win64_msvc2022_64</Name>` +
		`<Version>6.8.0-0-202410030750</Version>` +
		`<DownloadableArchives>qtbase.7z</DownloadableArchives>` +
		`<Operations><Operation name="Extract"><Argument>@TargetDir@/6.8.0/msvc2022_64</Argument>` +
		`<Argument>qtbase.7z</Argument></Operation></Operations></PackageUpdate></Updates>`
	leafIndex := `<html><head><title>Index of /` + leaf + `</title></head><body>` +
		`<h1>Index of /` + leaf + `</h1><a href="Updates.xml">Updates.xml</a></body></html>`

	return memFetcher{
		desktop:               targetIndex("qt6_680"),
		leaf:                  leafIndex,
		leaf + "/Updates.xml": updates,
	}
}

func TestPlanInstallQtBuildsAPlan(t *testing.T) {
	s := New(planFixture())
	version, err := model.ParseVersion("6.8.0")
	if err != nil {
		t.Fatal(err)
	}

	plan, err := s.PlanInstallQt(context.Background(), model.HostWindows, model.KindDesktop,
		version, "win64_msvc2022_64", nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != "6.8.0" || plan.Token != "680" {
		t.Errorf("plan identity = %+v", plan)
	}
	if len(plan.Archives) != 1 || plan.Archives[0].InstallPath != "6.8.0/msvc2022_64" {
		t.Errorf("archives = %+v", plan.Archives)
	}
}

// A version is matched by its numbers, so the request names the release and not
// the directory token it is spelled with.
func TestPlanInstallQtMatchesByNumbers(t *testing.T) {
	s := New(planFixture())
	version, err := model.ParseVersion("6.8.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PlanInstallQt(context.Background(), model.HostWindows, model.KindDesktop,
		version, "", nil); err != nil {
		t.Fatalf("PlanInstallQt = %v, want it to find the directory spelled 680", err)
	}
}

func TestPlanInstallQtReportsAMissingVersion(t *testing.T) {
	s := New(planFixture())
	version, err := model.ParseVersion("6.9.0")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PlanInstallQt(context.Background(), model.HostWindows, model.KindDesktop, version, "", nil)
	assertFailure(t, err, exitcode.NotFound, errs.CodeVersionNotFound)
}

// A version directory with several leaves needs the architecture to choose one.
func TestPlanInstallQtNeedsAnArchForASplitLayout(t *testing.T) {
	const dir = desktop + "/qt6_680"
	index := `<html><head><title>Index of /` + dir + `</title></head><body>` +
		`<h1>Index of /` + dir + `</h1>` +
		`<a href="qt6_680_mingw/">qt6_680_mingw/</a>` +
		`<a href="qt6_680_msvc2022_64/">qt6_680_msvc2022_64/</a></body></html>`
	s := New(memFetcher{desktop: targetIndex("qt6_680"), dir: index})
	version, err := model.ParseVersion("6.8.0")
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.PlanInstallQt(context.Background(), model.HostWindows, model.KindDesktop, version, "", nil)
	assertFailure(t, err, exitcode.NotFound, errs.CodePackageNotFound)
}
