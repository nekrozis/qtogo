package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/service"
)

func installedFixture() service.Installed {
	return service.Installed{
		Plan:        planFixture(),
		Path:        "/tmp/Qt/6.8.0/win64_msvc2022_64",
		Policy:      "modern-desktop",
		Relocatable: true,
	}
}

func TestInstallQtReportsTheInstalledTree(t *testing.T) {
	svc := &fakeServices{installed: installedFixture()}

	code, stdout, stderr := runWith(svc, "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64", "-O", "/tmp/Qt")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitcode.OK, stderr)
	}
	if !strings.Contains(stdout, "/tmp/Qt/6.8.0/win64_msvc2022_64") {
		t.Errorf("stdout does not name the destination:\n%s", stdout)
	}
	if !svc.instCalled || svc.installOpts.OutputDir != "/tmp/Qt" {
		t.Errorf("install was asked for %+v", svc.installOpts)
	}
	if svc.installOpts.DryRun {
		t.Error("a real install reported DryRun")
	}
}

func TestInstallQtWritesADocumentForJSON(t *testing.T) {
	svc := &fakeServices{installed: installedFixture()}

	code, stdout, _ := runWith(svc, "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64", "-O", "/tmp/Qt", "--json")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d", code, exitcode.OK)
	}
	var got installPayload
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout %q is not a document: %v", stdout, err)
	}
	if got.Path != "/tmp/Qt/6.8.0/win64_msvc2022_64" || got.Policy != "modern-desktop" || !got.Relocatable {
		t.Errorf("payload = %+v", got)
	}
}

// --dry-run is the plan command, not a lookalike: it must produce exactly what
// plan install-qt produces, and ask the install service for nothing.
func TestInstallQtDryRunIsThePlanCommand(t *testing.T) {
	planSvc := &fakeServices{plan: planFixture()}
	_, planOut, _ := runWith(planSvc, "plan", "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64", "--json")

	drySvc := &fakeServices{plan: planFixture()}
	code, dryOut, stderr := runWith(drySvc, "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64", "--dry-run", "--json")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitcode.OK, stderr)
	}
	if dryOut != planOut {
		t.Errorf("dry-run output differs from the plan command:\n dry: %q\nplan: %q", dryOut, planOut)
	}
	if drySvc.instCalled {
		t.Error("--dry-run called the install service; it must only plan")
	}
	if !drySvc.planCalled {
		t.Error("--dry-run did not ask for a plan")
	}
}

// The text forms match too.
func TestInstallQtDryRunTextMatchesThePlanCommand(t *testing.T) {
	_, planOut, _ := runWith(&fakeServices{plan: planFixture()},
		"plan", "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64")
	_, dryOut, _ := runWith(&fakeServices{plan: planFixture()},
		"install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64", "--dry-run")

	if dryOut != planOut {
		t.Errorf("dry-run text differs:\n dry: %q\nplan: %q", dryOut, planOut)
	}
}

func TestInstallQtPassesTheFlagsThrough(t *testing.T) {
	svc := &fakeServices{installed: installedFixture()}

	runWith(svc, "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64",
		"-O", "/dest", "--overwrite", "-m", "qtcharts", "--memory-budget", "6G")

	if !svc.installOpts.Overwrite {
		t.Error("--overwrite did not reach the service")
	}
	if svc.installOpts.OutputDir != "/dest" {
		t.Errorf("outputdir = %q, want /dest", svc.installOpts.OutputDir)
	}
	if strings.Join(svc.planModules, ",") != "qtcharts" {
		t.Errorf("modules = %v, want qtcharts", svc.planModules)
	}
	if svc.installOpts.MemoryBudget != 6<<30 {
		t.Errorf("memory budget = %d, want %d", svc.installOpts.MemoryBudget, uint64(6)<<30)
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		raw  string
		want uint64
	}{
		{"4G", 4 << 30},
		{"4g", 4 << 30},
		{"512M", 512 << 20},
		{"512MB", 512 << 20},
		{"2K", 2 << 10},
		{"1T", 1 << 40},
		{"1073741824", 1 << 30},
		{" 3 G ", 3 << 30},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := parseSize(tt.raw)
			if err != nil {
				t.Fatalf("parseSize(%q) = %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("parseSize(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseSizeRefusesNonsense(t *testing.T) {
	for _, raw := range []string{"", "big", "0", "0G", "-1G", "1.5G", "G"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := parseSize(raw); err == nil {
				t.Errorf("parseSize(%q) = nil, want a refusal", raw)
			} else if code := exitcode.Classify(err); code != exitcode.Usage {
				t.Errorf("parseSize(%q) exits %d, want %d", raw, code, exitcode.Usage)
			}
		})
	}
}

func TestInstallQtFailures(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no host", []string{"install-qt"}, "needs <host>"},
		{"no version", []string{"install-qt", "windows", "desktop"}, "needs <version>"},
		{"unknown host", []string{"install-qt", "plan9", "desktop", "6.8.0"}, `unknown host "plan9"`},
		{"bad version", []string{"install-qt", "windows", "desktop", "nope"}, "does not start with a number"},
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

func TestInstallQtReportsAServiceFailure(t *testing.T) {
	want := errs.New(exitcode.Relocate, errs.CodeRelocateUnsupported, errs.PhaseRelocate, "cannot relocate")
	svc := &fakeServices{err: want}

	code, _, _ := runWith(svc, "install-qt", "windows", "desktop", "6.8.0", "win64_msvc2022_64")

	if code != exitcode.Relocate {
		t.Errorf("exit = %d, want %d", code, exitcode.Relocate)
	}
}

func TestHelpDescribesInstallQt(t *testing.T) {
	code, stdout, _ := runWith(nil, "help", "install-qt")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d", code, exitcode.OK)
	}
	for _, want := range []string{"install-qt <host> <target> <version> [<arch>]", "-O, --outputdir <dir>", "--overwrite", "--dry-run", "-m, --modules <module>"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %q:\n%s", want, stdout)
		}
	}
}
