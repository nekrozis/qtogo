package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/buildinfo"
	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// run executes one command line and returns the exit code with both streams. The
// commands these tests drive are the ones that need no service.
func run(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := Main(context.Background(), args, &out, &errOut, nil)
	return code, out.String(), errOut.String()
}

func TestMainVersion(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"version"}} {
		code, stdout, stderr := run(args...)

		if code != exitcode.OK {
			t.Errorf("%q: exit = %d, want %d", args, code, exitcode.OK)
		}
		if want := buildinfo.String() + "\n"; stdout != want {
			t.Errorf("%q: stdout = %q, want %q", args, stdout, want)
		}
		if stderr != "" {
			t.Errorf("%q: stderr = %q, want empty", args, stderr)
		}
	}
}

func TestMainVersionJSON(t *testing.T) {
	code, stdout, stderr := run("version", "--json")

	if code != exitcode.OK {
		t.Fatalf("exit = %d, want %d", code, exitcode.OK)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}

	var payload struct {
		Program string `json:"program"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout, err)
	}
	if payload.Program != buildinfo.ProgramName {
		t.Errorf("program = %q, want %q", payload.Program, buildinfo.ProgramName)
	}
	if payload.Version != buildinfo.Version {
		t.Errorf("version = %q, want %q", payload.Version, buildinfo.Version)
	}
}

func TestMainBareInvocationFails(t *testing.T) {
	code, stdout, stderr := run()

	if code != exitcode.Usage {
		t.Errorf("exit = %d, want %d", code, exitcode.Usage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	for _, want := range []string{buildinfo.String(), "Usage: qtogo", "no command given", "hint:"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q\nstderr:\n%s", want, stderr)
		}
	}
}

func TestMainUsageFailuresGoToStderr(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown command", []string{"no-such-command"}, `unknown command "no-such-command"`},
		{"unknown option", []string{"--nope"}, "unknown option --nope"},
		{"unexpected argument", []string{"version", "extra"}, `unexpected argument "extra"`},
		{"value on a flag", []string{"--json=1"}, "does not take a value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(tt.args...)

			if code != exitcode.Usage {
				t.Errorf("exit = %d, want %d", code, exitcode.Usage)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr missing %q\nstderr:\n%s", tt.want, stderr)
			}
		})
	}
}

// TestMainJSONFailureShape covers the case the pre-scan exists for: parsing
// itself fails, and the failure still has to be machine-readable.
func TestMainJSONFailureShape(t *testing.T) {
	code, stdout, stderr := run("no-such-command", "--json")

	if code != exitcode.Usage {
		t.Fatalf("exit = %d, want %d", code, exitcode.Usage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}

	var env errs.Envelope
	if err := json.Unmarshal([]byte(stderr), &env); err != nil {
		t.Fatalf("stderr %q is not a JSON envelope: %v", stderr, err)
	}
	if env.OK {
		t.Error("ok = true, want false")
	}
	if env.Error.Code != errs.CodeUnknownCommand {
		t.Errorf("code = %q, want %q", env.Error.Code, errs.CodeUnknownCommand)
	}
	if env.Error.Phase != errs.PhaseParse {
		t.Errorf("phase = %q, want %q", env.Error.Phase, errs.PhaseParse)
	}
}

func TestMainHelp(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"short flag", []string{"-h"}, "Usage: qtogo <command>"},
		{"help command", []string{"help"}, "Usage: qtogo <command>"},
		{"help version", []string{"help", "version"}, "Usage: qtogo version"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(tt.args...)

			if code != exitcode.OK {
				t.Errorf("exit = %d, want %d", code, exitcode.OK)
			}
			if stderr != "" {
				t.Errorf("stderr = %q, want empty", stderr)
			}
			if !strings.Contains(stdout, tt.want) {
				t.Errorf("stdout missing %q\nstdout:\n%s", tt.want, stdout)
			}
		})
	}
}

// TestRunReturnsFailureWithoutWritingDiagnostics pins the split of duties: Run
// reports the failure, Main renders it.
func TestRunReturnsFailureWithoutWritingDiagnostics(t *testing.T) {
	var out, errOut bytes.Buffer

	err := Run(context.Background(), []string{"nope"}, &out, &errOut, nil)

	if err == nil {
		t.Fatal("Run returned nil error for an unknown command")
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("Run wrote output: stdout=%q stderr=%q", out.String(), errOut.String())
	}
}
