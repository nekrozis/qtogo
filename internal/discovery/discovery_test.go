package discovery

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/transport"
)

const base = "https://example.invalid/"

// desktop is the target directory the fixtures live under.
const desktop = Root + "/windows_x86/desktop"

// memFetcher serves bodies by repository-relative path, and answers anything else
// the way a real endpoint answers a path it does not have.
type memFetcher map[string]string

func (m memFetcher) Get(_ context.Context, p string) (transport.Document, error) {
	body, ok := m[path.Clean(p)]
	if !ok {
		return transport.Document{}, errs.New(exitcode.NotFound, errs.CodeHTTPNotFound,
			errs.PhaseDownload, "%s: not found", p)
	}
	return transport.Document{URL: base + strings.TrimSuffix(p, "/"), Body: []byte(body)}, nil
}

func fixture(t *testing.T, parts ...string) string {
	t.Helper()
	name := filepath.Join(append([]string{"..", "..", "testdata", "repository"}, parts...)...)
	blob, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(blob)
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

func TestLeavesFindsTheFlatLayout(t *testing.T) {
	dir := desktop + "/qt5_5152"
	d := New(memFetcher{dir: fixture(t, "qt5-flat", "index.html")})

	leaves, err := d.Leaves(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(leaves) != 1 || leaves[0].Path != dir || leaves[0].Ext != "" {
		t.Errorf("leaves = %+v, want the version directory itself", leaves)
	}
}

func TestLeavesFindsTheNestedLayout(t *testing.T) {
	dir := desktop + "/qt6_680"
	d := New(memFetcher{dir: fixture(t, "qt6-nested", "index.html")})

	leaves, err := d.Leaves(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	want := dir + "/qt6_680"
	if len(leaves) != 1 || leaves[0].Path != want {
		t.Errorf("leaves = %+v, want %q", leaves, want)
	}
}

func TestLeavesFindsTheArchSplitLayout(t *testing.T) {
	dir := desktop + "/qt6_6110"
	d := New(memFetcher{dir: fixture(t, "qt6-arch-split", "index.html")})

	leaves, err := d.Leaves(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	gots := make([]string, len(leaves))
	for i, l := range leaves {
		gots[i] = l.Ext
		if want := dir + "/qt6_6110_" + l.Ext; l.Path != want {
			t.Errorf("leaf path = %q, want %q", l.Path, want)
		}
	}
	sort.Strings(gots)
	want := []string{"mingw", "msvc2022_64", "msvc2022_arm64_cross_compiled"}
	if strings.Join(gots, ",") != strings.Join(want, ",") {
		t.Errorf("extensions = %v, want %v", gots, want)
	}
}

func TestLeavesRefusesAnUnknownLayout(t *testing.T) {
	dir := desktop + "/qt6_999"
	body := `<html><head><title>Index of /online/qtsdkrepository/windows_x86/desktop/qt6_999</title></head>` +
		`<body><h1>Index of /online/qtsdkrepository/windows_x86/desktop/qt6_999</h1>` +
		`<a href="something-else/">something-else/</a></body></html>`
	d := New(memFetcher{dir: body})

	_, err := d.Leaves(context.Background(), dir)
	assertFailure(t, err, exitcode.NotFound, errs.CodeVersionNotFound)
}

func TestVersionsDecodesTheChildren(t *testing.T) {
	const index = `<html><head><title>Index of /online/qtsdkrepository/windows_x86/desktop</title></head>` +
		`<body><h1>Index of /online/qtsdkrepository/windows_x86/desktop</h1>` +
		`<a href="qt5_5152/">qt5_5152/</a>` +
		`<a href="qt6_680/">qt6_680/</a>` +
		`<a href="qt6_6110/">qt6_6110/</a>` +
		`<a href="not-a-version/">not-a-version/</a>` +
		`<a href="Updates.xml">Updates.xml</a></body></html>`
	d := New(memFetcher{desktop: index})

	versions, err := d.Versions(context.Background(), desktop)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 3 {
		t.Fatalf("versions = %+v, want three (the child that is not one is skipped)", versions)
	}
	want := []struct{ major, minor, patch int }{
		{5, 15, 2}, {6, 8, 0}, {6, 11, 0},
	}
	for i, w := range want {
		v := versions[i].Directory.Version
		if v.Major != w.major || v.Minor != w.minor || v.Patch != w.patch {
			t.Errorf("versions[%d] = %d.%d.%d, want %d.%d.%d",
				i, v.Major, v.Minor, v.Patch, w.major, w.minor, w.patch)
		}
		if versions[i].Path != desktop+"/"+versions[i].Directory.Name {
			t.Errorf("versions[%d].Path = %q, want the directory it came from", i, versions[i].Path)
		}
	}
}

func TestMetadataReadsTheLeaf(t *testing.T) {
	leaf := Leaf{Path: desktop + "/qt6_680/qt6_680"}
	d := New(memFetcher{leaf.Path + "/Updates.xml": fixture(t, "updates-minimal", "Updates.xml")})

	packages, meta, err := d.Metadata(context.Background(), leaf)
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Name != "qt.qt5.5152.win64_msvc2019_64" {
		t.Errorf("packages = %+v, want the one in the fixture", packages)
	}
	if meta.MetadataName != "" {
		t.Errorf("metadata = %+v, want no metadata name in the minimal fixture", meta)
	}
}

func TestSegmentCoversTheDesktopHosts(t *testing.T) {
	cases := map[model.Host]string{
		model.HostWindows: "windows_x86",
		model.HostLinux:   "linux_x64",
		model.HostMac:     "mac_x64",
	}
	for host, want := range cases {
		got, err := Segment(host, model.KindDesktop)
		if err != nil || got != want {
			t.Errorf("Segment(%s, desktop) = %q, %v; want %q", host, got, err, want)
		}
	}
	for _, kind := range []model.Kind{model.KindAndroid, model.KindWASM, model.KindIOS} {
		if _, err := Segment(model.HostAll, kind); err == nil {
			t.Errorf("Segment(all_os, %s) was given a segment before its layout is known", kind)
		}
	}
}

// A desktop host the table has not been taught is refused rather than pointed at
// a guess. all_os is one: it addresses cross targets, not a desktop.
func TestSegmentRefusesADesktopHostItDoesNotKnow(t *testing.T) {
	if _, err := Segment(model.HostAll, model.KindDesktop); err == nil {
		t.Error("Segment(all_os, desktop) was given a segment before its layout is known")
	}
}

// A listing with nothing version-shaped under it is a refusal, not an empty
// answer: the caller asked what versions a target holds and there are none to
// name.
func TestVersionsRefusesAListingWithoutVersions(t *testing.T) {
	const index = `<html><head><title>Index of /online/qtsdkrepository/windows_x86/desktop</title></head>` +
		`<body><h1>Index of /online/qtsdkrepository/windows_x86/desktop</h1>` +
		`<a href="not-a-version/">not-a-version/</a>` +
		`<a href="Updates.xml">Updates.xml</a></body></html>`
	d := New(memFetcher{desktop: index})

	_, err := d.Versions(context.Background(), desktop)
	assertFailure(t, err, exitcode.NotFound, errs.CodeVersionNotFound)
}

func TestVersionsReportsAFetchFailure(t *testing.T) {
	d := New(memFetcher{})

	_, err := d.Versions(context.Background(), desktop)
	assertFailure(t, err, exitcode.NotFound, errs.CodeHTTPNotFound)
}

// A page that does not call itself a listing is refused by the repository
// reader, and discovery passes that refusal through rather than reading an empty
// directory out of a mirror or error page (ADR-008 decision 4).
func TestVersionsRefusesAPageThatIsNotAListing(t *testing.T) {
	d := New(memFetcher{desktop: `<html><body>a mirror status page, not a listing</body></html>`})

	_, err := d.Versions(context.Background(), desktop)
	if err == nil {
		t.Error("Versions on a page that is not a listing = nil, want a refusal")
	}
}

func TestLeavesReportsAFetchFailure(t *testing.T) {
	d := New(memFetcher{})

	_, err := d.Leaves(context.Background(), desktop+"/qt6_680")
	assertFailure(t, err, exitcode.NotFound, errs.CodeHTTPNotFound)
}

func TestMetadataReportsAFetchFailure(t *testing.T) {
	leaf := Leaf{Path: desktop + "/qt6_680/qt6_680"}
	d := New(memFetcher{})

	_, _, err := d.Metadata(context.Background(), leaf)
	assertFailure(t, err, exitcode.NotFound, errs.CodeHTTPNotFound)
}

// A truncated Updates.xml is refused by the parser, and discovery passes that
// refusal through: the metadata is the authority on what a version holds, so a
// document that cannot be read is a failure rather than an empty package list.
func TestMetadataReportsAMalformedDocument(t *testing.T) {
	leaf := Leaf{Path: desktop + "/qt6_680/qt6_680"}
	d := New(memFetcher{leaf.Path + "/Updates.xml": fixture(t, "updates-malformed", "Updates.xml")})

	_, _, err := d.Metadata(context.Background(), leaf)
	if err == nil {
		t.Error("Metadata on a malformed document = nil, want a refusal")
	}
}
