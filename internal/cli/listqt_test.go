package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/catalog"
	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/service"
)

// fakeServices answers the commands without a repository, and records what it was
// asked for.
type fakeServices struct {
	versions  []model.Version
	plan      catalog.Plan
	installed service.Installed
	err       error
	host      model.Host
	kind      model.Kind
	// the plan request
	planVersion model.Version
	planArch    string
	planModules []string
	// the install request
	installOpts service.InstallOptions
	called      bool
	planCalled  bool
	instCalled  bool
}

func (f *fakeServices) ListQtVersions(_ context.Context, host model.Host, kind model.Kind) ([]model.Version, error) {
	f.host, f.kind, f.called = host, kind, true
	return f.versions, f.err
}

func (f *fakeServices) PlanInstallQt(_ context.Context, host model.Host, kind model.Kind,
	version model.Version, arch string, modules []string) (catalog.Plan, error) {

	f.host, f.kind = host, kind
	f.planVersion, f.planArch, f.planModules = version, arch, modules
	f.planCalled = true
	return f.plan, f.err
}

func (f *fakeServices) InstallQt(_ context.Context, host model.Host, kind model.Kind,
	version model.Version, arch string, modules []string,
	opts service.InstallOptions) (service.Installed, error) {

	f.host, f.kind = host, kind
	f.planVersion, f.planArch, f.planModules = version, arch, modules
	f.installOpts = opts
	f.instCalled = true
	return f.installed, f.err
}

func runWith(svc Services, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Main(context.Background(), args, &out, &errOut, svc)
	return code, out.String(), errOut.String()
}

func versions(t *testing.T, raws ...string) []model.Version {
	t.Helper()
	parsed := make([]model.Version, len(raws))
	for i, raw := range raws {
		v, err := model.ParseVersion(raw)
		if err != nil {
			t.Fatal(err)
		}
		parsed[i] = v
	}
	return parsed
}

func TestListQtWritesOneVersionPerLine(t *testing.T) {
	svc := &fakeServices{versions: versions(t, "5.15.2", "6.8.0")}

	code, stdout, stderr := runWith(svc, "list-qt", "windows", "desktop")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, exitcode.OK, stderr)
	}
	if want := "5.15.2\n6.8.0\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if !svc.called || svc.host != model.HostWindows || svc.kind != model.KindDesktop {
		t.Errorf("service was asked for %s/%s, want windows/desktop", svc.host, svc.kind)
	}
}

func TestListQtWritesADocumentForJSON(t *testing.T) {
	svc := &fakeServices{versions: versions(t, "5.15.2")}

	code, stdout, _ := runWith(svc, "list-qt", "linux", "desktop", "--json")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d", code, exitcode.OK)
	}
	var got listQtPayload
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout %q is not a document: %v", stdout, err)
	}
	if got.Host != "linux" || got.Target != "desktop" || strings.Join(got.Versions, ",") != "5.15.2" {
		t.Errorf("payload = %+v", got)
	}
}

// Host and target are identity, so they are positional; the options that used to
// carry them are gone, and asking for one says where the value goes now.
func TestListQtCarriesItsIdentityPositionally(t *testing.T) {
	code, stdout, stderr := runWith(&fakeServices{}, "list-qt", "--host", "windows")

	if code != exitcode.Usage {
		t.Errorf("exit = %d, want %d", code, exitcode.Usage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	for _, want := range []string{"--host is not supported", "positionally"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr = %q, want it to contain %q", stderr, want)
		}
	}
}

func TestListQtFailures(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no host", []string{"list-qt"}, "needs <host>"},
		{"no target", []string{"list-qt", "windows"}, "needs <target>"},
		{"too many arguments", []string{"list-qt", "windows", "desktop", "extra"}, `unexpected argument "extra"`},
		{"unknown host", []string{"list-qt", "plan9", "desktop"}, `unknown host "plan9"`},
		{"unknown target", []string{"list-qt", "windows", "solaris"}, `unknown kind "solaris"`},
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

// A service failure reaches the caller with its own code, and the command does
// not turn it into a different one.
func TestListQtReportsAServiceFailure(t *testing.T) {
	want := errs.New(exitcode.NotFound, errs.CodeVersionNotFound, errs.PhaseResolve, "nothing there")
	svc := &fakeServices{err: want}

	code, _, _ := runWith(svc, "list-qt", "windows", "desktop")

	if code != exitcode.NotFound {
		t.Errorf("exit = %d, want %d", code, exitcode.NotFound)
	}
	if !errors.Is(svc.err, want) {
		t.Error("the service failure was replaced")
	}
}

func TestHelpDescribesListQt(t *testing.T) {
	code, stdout, _ := runWith(nil, "help", "list-qt")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d", code, exitcode.OK)
	}
	for _, want := range []string{"list-qt <host> <target>", "Arguments:", "<host>", "<target>"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help does not mention %q:\n%s", want, stdout)
		}
	}
}
