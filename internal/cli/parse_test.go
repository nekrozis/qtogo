package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

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

		{name: "unknown command", args: []string{"list-qt"}, wantErr: errs.CodeUnknownCommand},
		{name: "dash as a word", args: []string{"-"}, wantErr: errs.CodeUnknownCommand},
		{name: "unknown help topic", args: []string{"help", "nope"}, wantErr: errs.CodeUnknownCommand},
		{name: "unknown option", args: []string{"--nope"}, wantErr: errs.CodeUnknownOption},
		{name: "value on a flag option", args: []string{"--json=1"}, wantErr: errs.CodeUnexpectedValue},
		{name: "argument after a meta command", args: []string{"version", "extra"}, wantErr: errs.CodeUnexpectedArg},
		{name: "json is not accepted by help", args: []string{"help", "--json"}, wantErr: errs.CodeUnknownOption},
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
		})
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
