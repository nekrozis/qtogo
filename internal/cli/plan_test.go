package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/catalog"
	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
)

func planFixture() catalog.Plan {
	return catalog.Plan{
		Version:  "6.8.0",
		Token:    "680",
		Arch:     "win64_msvc2022_64",
		Packages: []string{"qt.qt6.680.win64_msvc2022_64"},
		Archives: []catalog.Archive{
			{Name: "qtbase.7z", Package: "qt.qt6.680.win64_msvc2022_64", URL: "https://example.invalid/qtbase.7z", InstallPath: "6.8.0/msvc2022_64"},
		},
	}
}

func TestPlanInstallQtWritesOneArchivePerLine(t *testing.T) {
	svc := &fakeServices{plan: planFixture()}

	code, stdout, stderr := runWith(svc, "plan", "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitcode.OK, stderr)
	}
	if want := "https://example.invalid/qtbase.7z\t6.8.0/msvc2022_64\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if !svc.planCalled || svc.host != model.HostWindows || svc.kind != model.KindDesktop {
		t.Errorf("service was asked for %s/%s, want windows/desktop", svc.host, svc.kind)
	}
	if svc.planArch != "win64_msvc2022_64" {
		t.Errorf("arch = %q, want win64_msvc2022_64", svc.planArch)
	}
}

func TestPlanInstallQtWritesADocumentForJSON(t *testing.T) {
	svc := &fakeServices{plan: planFixture()}

	code, stdout, _ := runWith(svc, "plan", "install-qt", "linux", "desktop", "6.8.0", "--json")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d", code, exitcode.OK)
	}
	var got planPayload
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout %q is not a document: %v", stdout, err)
	}
	if got.Host != "linux" || got.Version != "6.8.0" || got.Arch != "win64_msvc2022_64" {
		t.Errorf("payload = %+v", got)
	}
	if len(got.Archives) != 1 || got.Archives[0].InstallPath != "6.8.0/msvc2022_64" {
		t.Errorf("archives = %+v", got.Archives)
	}
}

// The architecture is optional: omitted, the catalog resolves it, and the command
// passes an empty arch through.
func TestPlanInstallQtAllowsAnOmittedArch(t *testing.T) {
	svc := &fakeServices{plan: planFixture()}

	code, _, stderr := runWith(svc, "plan", "install-qt", "windows", "desktop", "6.8.0")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitcode.OK, stderr)
	}
	if svc.planArch != "" {
		t.Errorf("arch = %q, want empty so the service resolves it", svc.planArch)
	}
}

// Modules are the reference tool's repeatable option, and each value is also read
// as a comma-separated list.
func TestPlanInstallQtCollectsModules(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"repeated", []string{"--modules", "a", "--modules", "b"}, "a,b"},
		{"equals", []string{"--modules=a"}, "a"},
		{"comma-separated", []string{"--modules", "a,b"}, "a,b"},
		{"mixed", []string{"--modules=a,b", "--modules", "c"}, "a,b,c"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &fakeServices{plan: planFixture()}
			args := append([]string{"plan", "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64"}, tt.args...)

			if code, _, stderr := runWith(svc, args...); code != exitcode.OK {
				t.Fatalf("exit = %d, want %d (%s)", code, exitcode.OK, stderr)
			}
			if got := strings.Join(svc.planModules, ","); got != tt.want {
				t.Errorf("modules = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlanInstallQtFailures(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no host", []string{"plan", "install-qt"}, "needs <host>"},
		{"no target", []string{"plan", "install-qt", "windows"}, "needs <target>"},
		{"no version", []string{"plan", "install-qt", "windows", "desktop"}, "needs <version>"},
		{"too many", []string{"plan", "install-qt", "windows", "desktop", "6.8.0", "x", "extra"}, `unexpected argument "extra"`},
		{"unknown host", []string{"plan", "install-qt", "plan9", "desktop", "6.8.0"}, `unknown host "plan9"`},
		{"bad version", []string{"plan", "install-qt", "windows", "desktop", "nope"}, "does not start with a number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runWith(&fakeServices{}, tt.args...)
			if code != exitcode.Usage {
				t.Errorf("exit = %d, want %d", code, exitcode.Usage)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.want)
			}
		})
	}
}

// A namespace node is not runnable on its own.
func TestPlanNeedsASubcommand(t *testing.T) {
	code, _, stderr := runWith(&fakeServices{}, "plan")
	if code != exitcode.Usage {
		t.Errorf("exit = %d, want %d", code, exitcode.Usage)
	}
	if !strings.Contains(stderr, "needs a subcommand") {
		t.Errorf("stderr = %q, want it to ask for a subcommand", stderr)
	}
}

func TestPlanInstallQtReportsAServiceFailure(t *testing.T) {
	want := errs.New(exitcode.NotFound, errs.CodePackageNotFound, errs.PhaseResolve, "no package")
	svc := &fakeServices{err: want}

	code, _, _ := runWith(svc, "plan", "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64")

	if code != exitcode.NotFound {
		t.Errorf("exit = %d, want %d", code, exitcode.NotFound)
	}
}

func TestHelpDescribesPlanInstallQt(t *testing.T) {
	code, stdout, _ := runWith(nil, "help", "plan", "install-qt")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d", code, exitcode.OK)
	}
	for _, want := range []string{"plan install-qt <host> <target> <version> [<arch>]", "Arguments:", "[<arch>]", "--modules"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %q:\n%s", want, stdout)
		}
	}
}
