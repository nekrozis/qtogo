package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// FuzzParseArgs pins the parser's two promises: it never panics on any words, and
// when it succeeds the positional count matches what the resolved node declares.
// Every failure it does produce is an *errs.Error, so the exit code is always
// classifiable. Args are NUL-separated inside the fuzz input.
func FuzzParseArgs(f *testing.F) {
	seeds := []string{
		"list-qt\x00windows\x00desktop",
		"list-qt\x00windows",
		"list-qt\x00windows\x00desktop\x00extra",
		"list-qt\x00--json\x00--\x00windows\x00desktop",
		"plan\x00install-qt\x00windows\x00desktop\x006.8.0\x00win64_msvc2022_64",
		"plan\x00install-qt\x00windows\x00desktop\x006.8.0\x00--modules\x00qtcharts",
		"plan",
		"--\x00--json",
		"--modules\x00--json",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		var args []string
		if raw != "" {
			args = strings.Split(raw, "\x00")
		}

		inv, err := parseArgs(args)
		if err != nil {
			var e *errs.Error
			if !errors.As(err, &e) {
				t.Fatalf("parseArgs(%q) = %v, want an *errs.Error", args, err)
			}
			return
		}
		if inv.node != nil && len(inv.args) > len(inv.node.args) {
			t.Fatalf("parseArgs(%q) accepted %d positional arguments for a node declaring %d",
				args, len(inv.args), len(inv.node.args))
		}
	})
}

// errorCode reports the machine code of a failure, and fails the test when err
// did not come from the error taxonomy.
func errorCode(t *testing.T, err error) string {
	t.Helper()

	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v is not an *errs.Error", err)
	}
	return e.Code()
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantMeta metaAction
		wantHelp []string
		wantJSON bool
		wantNode bool
		wantArgs []string
		wantErr  string
	}{
		{name: "no arguments", args: nil},
		{name: "end of options only", args: []string{"--"}},
		{name: "json alone", args: []string{"--json"}, wantJSON: true},
		{name: "long version", args: []string{"--version"}, wantMeta: metaVersion},
		{name: "version command", args: []string{"version"}, wantMeta: metaVersion, wantNode: true},
		{name: "version command with json", args: []string{"version", "--json"}, wantMeta: metaVersion, wantJSON: true, wantNode: true},
		{name: "root version with json", args: []string{"--version", "--json"}, wantMeta: metaVersion, wantJSON: true},
		{name: "short help", args: []string{"-h"}, wantMeta: metaHelp},
		{name: "long help", args: []string{"--help"}, wantMeta: metaHelp},
		{name: "help command", args: []string{"help"}, wantMeta: metaHelp, wantNode: true},
		{name: "help with topic", args: []string{"help", "version"}, wantMeta: metaHelp, wantHelp: []string{"version"}, wantNode: true},
		{name: "help flag with topic", args: []string{"-h", "version"}, wantMeta: metaHelp, wantHelp: []string{"version"}, wantNode: true},
		{name: "end of options before a command", args: []string{"--", "version"}, wantMeta: metaVersion, wantNode: true},

		{name: "positional arguments", args: []string{"list-qt", "windows", "desktop"}, wantNode: true, wantArgs: []string{"windows", "desktop"}},
		{name: "positional arguments after --", args: []string{"list-qt", "--json", "--", "windows", "desktop"}, wantJSON: true, wantNode: true, wantArgs: []string{"windows", "desktop"}},

		{name: "unknown command", args: []string{"no-such-command"}, wantErr: errs.CodeUnknownCommand},
		{name: "dash as a word", args: []string{"-"}, wantErr: errs.CodeUnknownCommand},
		{name: "unknown help topic", args: []string{"help", "nope"}, wantErr: errs.CodeUnknownCommand},
		{name: "unknown option", args: []string{"--nope"}, wantErr: errs.CodeUnknownOption},
		{name: "value on a flag option", args: []string{"--json=1"}, wantErr: errs.CodeUnexpectedValue},
		{name: "argument after a meta command", args: []string{"version", "extra"}, wantErr: errs.CodeUnexpectedArg},
		{name: "json is not accepted by help", args: []string{"help", "--json"}, wantErr: errs.CodeUnknownOption},
		{name: "too few positional arguments", args: []string{"list-qt", "windows"}, wantErr: errs.CodeMissingArgument},
		{name: "too many positional arguments", args: []string{"list-qt", "windows", "desktop", "extra"}, wantErr: errs.CodeUnexpectedArg},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv, err := parseArgs(tt.args)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("parseArgs(%q) = nil error, want %s", tt.args, tt.wantErr)
				}
				if got := errorCode(t, err); got != tt.wantErr {
					t.Fatalf("code = %q, want %q", got, tt.wantErr)
				}
				if got := exitcode.Classify(err); got != exitcode.Usage {
					t.Errorf("exit code = %d, want %d", got, exitcode.Usage)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseArgs(%q) = %v, want no error", tt.args, err)
			}
			if inv.meta != tt.wantMeta {
				t.Errorf("meta = %d, want %d", inv.meta, tt.wantMeta)
			}
			if inv.json != tt.wantJSON {
				t.Errorf("json = %t, want %t", inv.json, tt.wantJSON)
			}
			if (inv.node != nil) != tt.wantNode {
				t.Errorf("node = %v, want present=%t", inv.node, tt.wantNode)
			}
			if got, want := strings.Join(inv.helpPath, " "), strings.Join(tt.wantHelp, " "); got != want {
				t.Errorf("helpPath = %q, want %q", got, want)
			}
			if got, want := strings.Join(inv.args, " "), strings.Join(tt.wantArgs, " "); got != want {
				t.Errorf("args = %q, want %q", got, want)
			}
		})
	}
}

// An option that takes a value reads it the same way whether it is written with
// "=" or as the next word, and a repeated one keeps every value in order.
func TestSplitReadsOptionValues(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		want  []string
		words []string
	}{
		{"value as the next word", []string{"--modules", "qtcharts"}, []string{"qtcharts"}, nil},
		{"value after =", []string{"--modules=qtcharts"}, []string{"qtcharts"}, nil},
		{"repeated keeps order", []string{"--modules", "a", "--modules=b"}, []string{"a", "b"}, nil},
		{"words are left alone", []string{"windows", "--modules", "qtcharts", "desktop"}, []string{"qtcharts"}, []string{"windows", "desktop"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			used, words, err := split(tt.args)
			if err != nil {
				t.Fatalf("split(%q) = %v", tt.args, err)
			}
			if got := strings.Join(collectValues(used)[optModules], " "); got != strings.Join(tt.want, " ") {
				t.Errorf("values = %q, want %q", got, strings.Join(tt.want, " "))
			}
			if got := strings.Join(words, " "); got != strings.Join(tt.words, " ") {
				t.Errorf("words = %q, want %q", got, strings.Join(tt.words, " "))
			}
		})
	}
}

// A value-taking option whose value is missing, or is itself an option, is a
// usage error rather than a silent empty value.
func TestSplitRefusesAMissingOptionValue(t *testing.T) {
	for _, args := range [][]string{{"--modules"}, {"--modules", "--json"}} {
		_, _, err := split(args)
		if err == nil {
			t.Fatalf("split(%q) = nil, want a missing-argument error", args)
		}
		if got := errorCode(t, err); got != errs.CodeMissingArgument {
			t.Errorf("split(%q) code = %q, want %q", args, got, errs.CodeMissingArgument)
		}
	}
}

func TestParseArgsUsageErrorsCarryAHint(t *testing.T) {
	for _, args := range [][]string{{"nope"}, {"--nope"}, {"version", "extra"}, {"help", "nope"}} {
		_, err := parseArgs(args)
		if err == nil {
			t.Fatalf("parseArgs(%q) = nil error", args)
		}
		if errs.SuggestionOf(err) == "" {
			t.Errorf("parseArgs(%q) failure has no suggestion", args)
		}
	}
}
