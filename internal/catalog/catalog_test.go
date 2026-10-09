package catalog

import (
	"errors"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/repository"
)

const leaf = "online/qtsdkrepository/windows_x86/desktop/qt6_680/qt6_680"

func version(t *testing.T, raw string) model.Version {
	t.Helper()
	v, err := model.ParseVersion(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// directoryVersion is the version a directory spells: the token is Raw, and the
// numbers are for a person. "680" is the spelling package names are built from.
func directoryVersion(t *testing.T, dotted string, token string) model.Version {
	t.Helper()
	v := version(t, dotted)
	v.Raw = token
	return v
}

func assertFailure(t *testing.T, err error, wantExit int, wantCode string) {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("error = %v, want an *errs.Error", err)
	}
	if e.ExitCode() != wantExit || e.Code() != wantCode {
		t.Errorf("failure = exit %d, code %q; want exit %d, code %q", e.ExitCode(), e.Code(), wantExit, wantCode)
	}
}

// basePackage is a metadata component: its name, version stamp, downloads, and
// the destinations an Extract operation states.
func basePackage(name string, downloads ...string) repository.PackageUpdate {
	var ops []repository.Operation
	for _, d := range downloads {
		ops = append(ops, repository.Operation{
			Name:      "Extract",
			Arguments: []string{"@TargetDir@/6.8.0/msvc2022_64", d},
		})
	}
	return repository.PackageUpdate{
		Name:          name,
		Version:       mustVersion("6.8.0-0-202410030750"),
		Downloadables: downloads,
		Operations:    ops,
	}
}

func mustVersion(raw string) model.Version {
	v, err := model.ParseVersion(raw)
	if err != nil {
		panic(err)
	}
	return v
}

func TestBuildSelectsTheBaseAndItsModulesInOrder(t *testing.T) {
	packages := []repository.PackageUpdate{
		{Name: "qt.qt6.680.addons.qtcharts.win64_msvc2022_64", Version: mustVersion("6.8.0-0-202410030750"), Downloadables: []string{"qtcharts.7z"}},
		basePackage("qt.qt6.680.win64_msvc2022_64", "qtbase.7z"),
	}

	plan, err := Build(packages, leaf, Request{
		Version: directoryVersion(t, "6.8.0", "680"),
		Arch:    "win64_msvc2022_64",
		Modules: []string{"qtcharts"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if plan.Version != "6.8.0" || plan.Token != "680" || plan.Arch != "win64_msvc2022_64" {
		t.Errorf("plan identity = %+v", plan)
	}
	want := "qt.qt6.680.win64_msvc2022_64,qt.qt6.680.addons.qtcharts.win64_msvc2022_64"
	if strings.Join(plan.Packages, ",") != want {
		t.Errorf("packages = %v, want %q", plan.Packages, want)
	}
}

func TestBuildPairsEachExtractDestinationWithItsArchive(t *testing.T) {
	pkg := repository.PackageUpdate{
		Name:    "qt.qt6.680.win64_msvc2022_64",
		Version: mustVersion("6.8.0-0-202410030750"),
		Operations: []repository.Operation{
			{Name: "Extract", Arguments: []string{"@TargetDir@/6.8.0/msvc2022_64", "qtbase.7z"}},
			{Name: "Extract", Arguments: []string{"@TargetDir@/6.8.0/msvc2022_64", "qtsvg.7z"}},
		},
	}

	plan, err := Build([]repository.PackageUpdate{pkg}, leaf, Request{
		Version: directoryVersion(t, "6.8.0", "680"),
		Arch:    "win64_msvc2022_64",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Archives) != 2 {
		t.Fatalf("archives = %+v, want two", plan.Archives)
	}
	wantURL := leaf + "/qt.qt6.680.win64_msvc2022_64/6.8.0-0-202410030750qtbase.7z"
	if plan.Archives[0].URL != wantURL {
		t.Errorf("url = %q, want %q", plan.Archives[0].URL, wantURL)
	}
	for _, a := range plan.Archives {
		if a.InstallPath != "6.8.0/msvc2022_64" {
			t.Errorf("installPath = %q, want the Extract destination without its prefix", a.InstallPath)
		}
	}
}

// A package with no Extract operation extracts at the root: the archive carries
// its version and architecture itself.
func TestBuildLeavesAPackageWithoutExtractAtTheRoot(t *testing.T) {
	pkg := repository.PackageUpdate{
		Name:          "qt.qt6.680.win64_msvc2022_64",
		Version:       mustVersion("6.8.0-0-202410030750"),
		Downloadables: []string{"qtbase.7z"},
	}

	plan, err := Build([]repository.PackageUpdate{pkg}, leaf, Request{
		Version: directoryVersion(t, "6.8.0", "680"),
		Arch:    "win64_msvc2022_64",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Archives) != 1 || plan.Archives[0].InstallPath != "" {
		t.Errorf("archives = %+v, want one with an empty install path", plan.Archives)
	}
}

// A virtual package lists no downloads but is still part of the plan.
func TestBuildKeepsAVirtualPackageWithoutArchives(t *testing.T) {
	pkg := repository.PackageUpdate{
		Name:    "qt.qt6.680.addons.qtcharts.win64_msvc2022_64",
		Version: mustVersion("6.8.0-0-202410030750"),
		Virtual: true,
	}

	plan, err := Build([]repository.PackageUpdate{
		basePackage("qt.qt6.680.win64_msvc2022_64", "qtbase.7z"),
		pkg,
	}, leaf, Request{
		Version: directoryVersion(t, "6.8.0", "680"),
		Arch:    "win64_msvc2022_64",
		Modules: []string{"qtcharts"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(plan.Packages) != 2 {
		t.Errorf("packages = %v, want the base and the module", plan.Packages)
	}
	if len(plan.Archives) != 1 {
		t.Errorf("archives = %+v, want only the base's", plan.Archives)
	}
}

func TestBuildAcceptsTheQt5BaseNameForm(t *testing.T) {
	pkg := basePackage("qt.5152.win64_msvc2019_64", "qtbase.7z")

	plan, err := Build([]repository.PackageUpdate{pkg}, leaf, Request{
		Version: directoryVersion(t, "5.15.2", "5152"),
		Arch:    "win64_msvc2019_64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Packages[0] != "qt.5152.win64_msvc2019_64" {
		t.Errorf("packages = %v", plan.Packages)
	}
}

func TestBuildInfersASoleArchitecture(t *testing.T) {
	plan, err := Build([]repository.PackageUpdate{basePackage("qt.qt6.680.win64_msvc2022_64", "qtbase.7z")}, leaf,
		Request{Version: directoryVersion(t, "6.8.0", "680")})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Arch != "win64_msvc2022_64" {
		t.Errorf("arch = %q, want it inferred", plan.Arch)
	}
}

func TestBuildRefusesAnAmbiguousArchitecture(t *testing.T) {
	packages := []repository.PackageUpdate{
		basePackage("qt.qt6.680.win64_msvc2022_64", "a.7z"),
		basePackage("qt.qt6.680.win64_mingw", "b.7z"),
	}
	_, err := Build(packages, leaf, Request{Version: directoryVersion(t, "6.8.0", "680")})
	assertFailure(t, err, exitcode.NotFound, errs.CodePackageNotFound)
}

func TestBuildRefusesAMissingBase(t *testing.T) {
	_, err := Build(nil, leaf, Request{
		Version: directoryVersion(t, "6.8.0", "680"),
		Arch:    "win64_msvc2022_64",
	})
	assertFailure(t, err, exitcode.NotFound, errs.CodePackageNotFound)
}

func TestBuildRefusesAMissingModuleByName(t *testing.T) {
	_, err := Build([]repository.PackageUpdate{basePackage("qt.qt6.680.win64_msvc2022_64", "qtbase.7z")}, leaf, Request{
		Version: directoryVersion(t, "6.8.0", "680"),
		Arch:    "win64_msvc2022_64",
		Modules: []string{"qtcharts"},
	})
	assertFailure(t, err, exitcode.NotFound, errs.CodePackageNotFound)
	if !strings.Contains(err.Error(), "qtcharts") {
		t.Errorf("failure %q does not name the missing module", err)
	}
}

// Selection does not depend on the order the packages appear in.
func TestBuildIsIndependentOfInputOrder(t *testing.T) {
	forward := []repository.PackageUpdate{
		basePackage("qt.qt6.680.win64_msvc2022_64", "qtbase.7z"),
		{Name: "qt.qt6.680.addons.qtcharts.win64_msvc2022_64", Version: mustVersion("6.8.0-0-202410030750"), Downloadables: []string{"qtcharts.7z"}},
	}
	backward := []repository.PackageUpdate{forward[1], forward[0]}

	req := Request{
		Version: directoryVersion(t, "6.8.0", "680"),
		Arch:    "win64_msvc2022_64",
		Modules: []string{"qtcharts"},
	}
	a, err := Build(forward, leaf, req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(backward, leaf, req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(a.Packages, ",") != strings.Join(b.Packages, ",") || len(a.Archives) != len(b.Archives) {
		t.Errorf("plans differ with input order:\n%+v\n%+v", a, b)
	}
}

func TestBuildNeedsAVersion(t *testing.T) {
	if _, err := Build(nil, leaf, Request{Arch: "x"}); err == nil {
		t.Error("Build with no version = nil, want a failure")
	}
}
