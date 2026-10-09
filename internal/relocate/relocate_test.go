package relocate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
)

func version(t *testing.T, raw string) model.Version {
	t.Helper()
	v, err := model.ParseVersion(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func target(host model.Host, kind model.Kind, v string) model.Target {
	parsed, err := model.ParseVersion(v)
	if err != nil {
		panic(err)
	}
	return model.Target{Host: host, Kind: kind, Version: parsed, Arch: "x"}
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

// write puts a file under root, creating the directories on the way.
func write(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func exists(root, rel string) bool {
	_, err := os.Lstat(filepath.Join(root, rel))
	return err == nil
}

// ---- Capability ----

func TestCapabilitySupports(t *testing.T) {
	since := version(t, "5.14.0")
	c := Capability{Kinds: []model.Kind{model.KindDesktop}, Since: &since}

	tests := []struct {
		name    string
		host    model.Host
		kind    model.Kind
		version string
		want    bool
	}{
		{"at the floor", model.HostLinux, model.KindDesktop, "5.14.0", true},
		{"above the floor", model.HostWindows, model.KindDesktop, "6.8.0", true},
		{"below the floor", model.HostMac, model.KindDesktop, "5.13.2", false},
		{"wrong kind", model.HostLinux, model.KindAndroid, "6.8.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Supports(tt.host, tt.kind, version(t, tt.version)); got != tt.want {
				t.Errorf("Supports = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCapabilityUntilIsExclusive(t *testing.T) {
	until := version(t, "5.14.0")
	c := Capability{Until: &until}

	if c.Supports(model.HostLinux, model.KindDesktop, version(t, "5.14.0")) {
		t.Error("Until was inclusive at its bound")
	}
	if !c.Supports(model.HostLinux, model.KindDesktop, version(t, "5.13.9")) {
		t.Error("a version below the bound was refused")
	}
}

// ---- Selector ----

func TestSelectorCoversTheModernDesktop(t *testing.T) {
	s := NewSelector()

	policy, err := s.Select(model.HostLinux, model.KindDesktop, version(t, "6.8.0"))
	if err != nil {
		t.Fatal(err)
	}
	if policy.Capability().Name != "modern-desktop" {
		t.Errorf("policy = %q, want modern-desktop", policy.Capability().Name)
	}
}

func TestSelectorRefusesQtBelowTheFloor(t *testing.T) {
	err := NewSelector().Check(target(model.HostLinux, model.KindDesktop, "5.9.0"))
	assertFailure(t, err, exitcode.Relocate, errs.CodeRelocateUnsupported)

	// Relocate refuses too, so a caller that skipped Check cannot produce an
	// un-relocated tree through this path.
	if _, err := NewSelector().Relocate(context.Background(), Dir(t.TempDir()),
		target(model.HostLinux, model.KindDesktop, "5.9.0")); err == nil {
		t.Error("Relocate accepted a version it does not cover")
	}
}

func TestSelectorRefusesACrossTarget(t *testing.T) {
	for _, kind := range []model.Kind{model.KindAndroid, model.KindIOS, model.KindWASM, model.KindWinRT} {
		err := NewSelector().Check(target(model.HostAll, kind, "6.8.0"))
		assertFailure(t, err, exitcode.Relocate, errs.CodeRelocateUnsupported)
	}
}

// The refusal names what is covered, so a reader can tell "too old" from "unknown".
func TestUnsupportedErrorNamesTheMatrix(t *testing.T) {
	// A kind no policy names takes the selector's own refusal, which lists the
	// covered matrix.
	err := NewSelector().Check(target(model.HostLinux, model.Kind("nonesuch"), "6.8.0"))
	if msg := err.Error(); !strings.Contains(msg, "modern-desktop") {
		t.Errorf("refusal does not list the covered policies: %s", msg)
	}

	// A covered-but-unready target names the policy it matched.
	err = NewSelector().Check(target(model.HostLinux, model.KindDesktop, "5.9.0"))
	if msg := err.Error(); !strings.Contains(msg, "legacy-qt5-desktop") {
		t.Errorf("refusal does not name the policy: %s", msg)
	}
}

func TestCheckMirrorsSelect(t *testing.T) {
	s := NewSelector()

	if err := s.Check(target(model.HostLinux, model.KindDesktop, "6.8.0")); err != nil {
		t.Errorf("Check(modern desktop) = %v, want nil", err)
	}
	if err := s.Check(target(model.HostLinux, model.KindDesktop, "5.9.0")); err == nil {
		t.Error("Check(low Qt 5) = nil, want a refusal")
	}
}

// Select returns the policy that covers a target even when it is not ready, so a
// caller can name it; readiness is Check's question.
func TestSelectReturnsTheCoveringPolicy(t *testing.T) {
	policy, err := NewSelector().Select(model.HostLinux, model.KindDesktop, version(t, "5.9.0"))
	if err != nil {
		t.Fatalf("Select(low Qt 5) = %v, want the covering policy", err)
	}
	if policy.Capability().Name != "legacy-qt5-desktop" {
		t.Errorf("policy = %q, want legacy-qt5-desktop", policy.Capability().Name)
	}
	if policy.Ready() {
		t.Error("the legacy policy reports itself ready")
	}
}

// A target no policy even names is refused by Select.
func TestSelectRefusesAnUnnamedTarget(t *testing.T) {
	// A host and kind pair no policy lists: the cross policy names the non-desktop
	// kinds and the modern one the desktop, so a made-up kind matches neither.
	_, err := NewSelector().Select(model.HostLinux, model.Kind("nonesuch"), version(t, "6.8.0"))
	assertFailure(t, err, exitcode.Relocate, errs.CodeRelocateUnsupported)
}

// ---- Modern desktop primitives ----

func TestRelocateWritesQtConf(t *testing.T) {
	root := t.TempDir()

	report, err := NewSelector().Relocate(context.Background(), Dir(root),
		target(model.HostLinux, model.KindDesktop, "6.8.0"))
	if err != nil {
		t.Fatal(err)
	}

	if got := read(t, root, filepath.Join("bin", "qt.conf")); got != qtConf {
		t.Errorf("qt.conf = %q, want %q", got, qtConf)
	}
	if !strings.Contains(report.Changes[0].Path, "qt.conf") || report.Changes[0].Action != "created" {
		t.Errorf("report = %+v, want a created qt.conf", report.Changes)
	}
}

// A second run changes nothing, which is what idempotent means here.
func TestRelocateIsIdempotent(t *testing.T) {
	root := t.TempDir()
	write(t, root, filepath.Join("mkspecs", "qconfig.pri"), "QT_EDITION = Commercial\nQT_LICHECK = armed\n")
	write(t, root, filepath.Join("lib", "pkgconfig", "Qt6Core.pc"), "prefix=/home/qt/work/install\nName: Qt6Core\n")
	write(t, root, filepath.Join("lib", "libQt6Core.prl"), "QMAKE_PRL_LIBS = /home/qt/work/install/lib\n")
	write(t, root, filepath.Join("lib", "libQt6Core.la"), "libdir='/home/qt/work/install/lib'\n")

	s := NewSelector()
	first, err := s.Relocate(context.Background(), Dir(root), target(model.HostLinux, model.KindDesktop, "6.8.0"))
	if err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, root)

	second, err := s.Relocate(context.Background(), Dir(root), target(model.HostLinux, model.KindDesktop, "6.8.0"))
	if err != nil {
		t.Fatal(err)
	}
	after := snapshot(t, root)

	if before != after {
		t.Error("the second run changed the tree")
	}
	if len(second.Changes) != 0 {
		t.Errorf("the second run reported changes: %+v", second.Changes)
	}
	if len(first.Changes) == 0 {
		t.Error("the first run reported no changes, so nothing was exercised")
	}
}

func TestRelocateSetsTheLicense(t *testing.T) {
	root := t.TempDir()
	write(t, root, filepath.Join("mkspecs", "qconfig.pri"),
		"QT_EDITION = Commercial\nQT_LICHECK = /home/qt/work/install/bin/qtliccheck\nOTHER = kept\n")

	if _, err := NewSelector().Relocate(context.Background(), Dir(root),
		target(model.HostLinux, model.KindDesktop, "6.8.0")); err != nil {
		t.Fatal(err)
	}

	got := read(t, root, filepath.Join("mkspecs", "qconfig.pri"))
	for _, want := range []string{"QT_EDITION = OpenSource\n", "QT_LICHECK =\n", "OTHER = kept\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("qconfig.pri missing %q:\n%s", want, got)
		}
	}
}

func TestRelocateRelativisesPkgConfig(t *testing.T) {
	root := t.TempDir()
	write(t, root, filepath.Join("lib", "pkgconfig", "Qt6Core.pc"),
		"prefix=/home/qt/work/install\nlibdir=${prefix}/lib\n")

	if _, err := NewSelector().Relocate(context.Background(), Dir(root),
		target(model.HostLinux, model.KindDesktop, "6.8.0")); err != nil {
		t.Fatal(err)
	}

	got := read(t, root, filepath.Join("lib", "pkgconfig", "Qt6Core.pc"))
	if !strings.Contains(got, "prefix=${pcfiledir}/../..\n") {
		t.Errorf("pc prefix was not relativised:\n%s", got)
	}
	if strings.Contains(got, "/home/qt/work/install") {
		t.Errorf("pc still holds the build prefix:\n%s", got)
	}
}

// On macOS a .pc carries a -F flag path too, which has to be relativised the same
// way.
func TestRelocateRelativisesMacPkgConfigFlag(t *testing.T) {
	root := t.TempDir()
	write(t, root, filepath.Join("lib", "pkgconfig", "Qt6Core.pc"),
		"prefix=/Users/qt/work/install\nLibs: -F/Users/qt/work/install/lib -lQt6Core\n")

	if _, err := NewSelector().Relocate(context.Background(), Dir(root),
		target(model.HostMac, model.KindDesktop, "6.8.0")); err != nil {
		t.Fatal(err)
	}

	got := read(t, root, filepath.Join("lib", "pkgconfig", "Qt6Core.pc"))
	if strings.Contains(got, "/Users/qt/work/install") {
		t.Errorf("pc still holds the build prefix:\n%s", got)
	}
	if !strings.Contains(got, "-F${pcfiledir}/../..") {
		t.Errorf("the -F flag was not relativised:\n%s", got)
	}
	if !strings.Contains(got, "-lQt6Core") {
		t.Errorf("the flag rewrite ate the rest of the line:\n%s", got)
	}
}

// FuzzReplaceFlag pins the one piece of parsing here: it terminates on any input,
// and no path written directly after a -F flag survives. A bare prefix with no flag
// is not its business — replaceBuildPrefix handles that — so it is not asserted.
func FuzzReplaceFlag(f *testing.F) {
	f.Add("-F/Users/qt/work/install/lib -lQt6Core")
	f.Add("-F=/tmp/a/lib")
	f.Add("-F")
	f.Add("nothing to see")
	f.Add("")

	const prefix = "/Users/qt/work/install"
	f.Fuzz(func(t *testing.T, text string) {
		got := replaceFlag(text, "-F", "${pcfiledir}/../..")
		// The promise is exactly this: a flagged prefix in the input is not in the
		// output. A prefix the input wrote bare is another function's business.
		if strings.Contains(text, "-F"+prefix) && strings.Contains(got, "-F"+prefix) {
			t.Fatalf("replaceFlag left a flagged build prefix:\n%s", got)
		}
	})
}

func TestRelocateRewritesPrl(t *testing.T) {
	root := t.TempDir()
	write(t, root, filepath.Join("lib", "libQt6Core.prl"),
		"QMAKE_PRL_LIBS = /home/qt/work/install/lib -lm\n")

	if _, err := NewSelector().Relocate(context.Background(), Dir(root),
		target(model.HostLinux, model.KindDesktop, "6.8.0")); err != nil {
		t.Fatal(err)
	}

	got := read(t, root, filepath.Join("lib", "libQt6Core.prl"))
	if !strings.Contains(got, "$$[QT_INSTALL_LIBS]") {
		t.Errorf("prl was not rewritten:\n%s", got)
	}
	if strings.Contains(got, "/home/qt/work/install") {
		t.Errorf("prl still holds the build prefix:\n%s", got)
	}
}

func TestRelocateRemovesLibtoolFiles(t *testing.T) {
	root := t.TempDir()
	rel := filepath.Join("lib", "libQt6Core.la")
	write(t, root, rel, "libdir='/home/qt/work/install/lib'\n")

	report, err := NewSelector().Relocate(context.Background(), Dir(root),
		target(model.HostLinux, model.KindDesktop, "6.8.0"))
	if err != nil {
		t.Fatal(err)
	}

	if exists(root, rel) {
		t.Error("the .la file was not removed")
	}
	found := false
	for _, c := range report.Changes {
		if strings.Contains(c.Path, "libQt6Core.la") && c.Action == "removed" {
			found = true
		}
	}
	if !found {
		t.Errorf("report does not record the removal: %+v", report.Changes)
	}
}

// A tree without the files a policy looks for is not a failure: a newer Qt simply
// does not carry them.
func TestRelocateSkipsMissingFiles(t *testing.T) {
	root := t.TempDir()

	report, err := NewSelector().Relocate(context.Background(), Dir(root),
		target(model.HostLinux, model.KindDesktop, "6.8.0"))
	if err != nil {
		t.Fatalf("Relocate on a bare tree = %v, want no error", err)
	}
	if len(report.Skipped) == 0 {
		t.Error("no skips were reported for a tree without the optional files")
	}
}

// ---- The no-absolute-path rule ----

// The ZAP rule: nothing relocation writes may contain the install path, so two
// installs of the same tree are byte-identical wherever they land.
func TestRelocateWritesNoAbsolutePath(t *testing.T) {
	root := t.TempDir()
	write(t, root, filepath.Join("lib", "pkgconfig", "Qt6Core.pc"), "prefix=/home/qt/work/install\n")
	write(t, root, filepath.Join("lib", "libQt6Core.prl"), "QMAKE_PRL_LIBS = /home/qt/work/install/lib\n")

	if _, err := NewSelector().Relocate(context.Background(), Dir(root),
		target(model.HostLinux, model.KindDesktop, "6.8.0")); err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{
		filepath.Join("bin", "qt.conf"),
		filepath.Join("lib", "pkgconfig", "Qt6Core.pc"),
		filepath.Join("lib", "libQt6Core.prl"),
	} {
		body := read(t, root, rel)
		for _, forbidden := range []string{root, "/home/qt/work/install"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s holds %q:\n%s", rel, forbidden, body)
			}
		}
	}
}

// A cancelled context stops before doing more work.
func TestRelocateStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewSelector().Relocate(ctx, Dir(t.TempDir()),
		target(model.HostLinux, model.KindDesktop, "6.8.0"))
	if err == nil {
		t.Fatal("Relocate with a cancelled context = nil, want a failure")
	}
	if exitcode.Classify(err) != exitcode.Interrupted {
		t.Errorf("exit = %d, want %d", exitcode.Classify(err), exitcode.Interrupted)
	}
}

// ---- helpers ----

// snapshot reads every file under root into one string, for an equality check.
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var b []byte
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b = append(b, []byte(rel+"\x00"+string(body)+"\n")...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
