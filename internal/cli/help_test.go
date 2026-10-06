package cli

import (
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/buildinfo"
	"github.com/nekrozis/qtogo/internal/errs"
)

func TestRootUsageListsTheWholeTree(t *testing.T) {
	text := rootUsage()

	for _, want := range []string{
		"Usage: " + buildinfo.ProgramName + " <command> [options]",
		"Commands:",
		"help",
		"Show help for a command",
		"version",
		"Show the version",
		"Options:",
		"-h, --help",
		"--version",
		"--json",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("root usage is missing %q\n%s", want, text)
		}
	}
}

func TestCommandUsageDescribesOneCommand(t *testing.T) {
	text, err := commandUsage([]string{"version"})
	if err != nil {
		t.Fatalf("commandUsage(version) = %v, want no error", err)
	}

	for _, want := range []string{
		"Usage: " + buildinfo.ProgramName + " version [options]",
		"Show the version",
		"-h, --help",
		"--json",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("version usage is missing %q\n%s", want, text)
		}
	}
	if strings.Contains(text, "--version") {
		t.Errorf("version usage offers --version, which the command does not accept\n%s", text)
	}
}

func TestCommandUsageWithoutAPathIsRootUsage(t *testing.T) {
	text, err := commandUsage(nil)
	if err != nil {
		t.Fatalf("commandUsage(nil) = %v, want no error", err)
	}
	if text != rootUsage() {
		t.Errorf("commandUsage(nil) did not return the root usage:\n%s", text)
	}
}

func TestCommandUsageRejectsUnknownTopics(t *testing.T) {
	for _, path := range [][]string{{"nope"}, {"version", "extra"}} {
		_, err := commandUsage(path)
		if err == nil {
			t.Fatalf("commandUsage(%q) = nil error, want a failure", path)
		}
		if got := errorCode(t, err); got != errs.CodeUnknownCommand {
			t.Errorf("commandUsage(%q) code = %q, want %q", path, got, errs.CodeUnknownCommand)
		}
	}
}
