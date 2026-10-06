package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/nekrozis/qtogo/internal/buildinfo"
	"github.com/nekrozis/qtogo/internal/errs"
)

// renderer keeps the two output contracts apart: stdout carries payloads,
// stderr carries diagnostics, JSON is the stable machine shape and text is for
// people.
type renderer struct {
	out    io.Writer
	errOut io.Writer
	json   bool
}

func newRenderer(out, errOut io.Writer, jsonMode bool) renderer {
	return renderer{out: out, errOut: errOut, json: jsonMode}
}

// versionPayload is the JSON shape of the version command.
type versionPayload struct {
	Program string `json:"program"`
	Version string `json:"version"`
	Commit  string `json:"commit,omitempty"`
	Date    string `json:"date,omitempty"`
}

func (r renderer) version() error {
	if r.json {
		return writeJSON(r.out, versionPayload{
			Program: buildinfo.ProgramName,
			Version: buildinfo.Version,
			Commit:  buildinfo.Commit,
			Date:    buildinfo.Date,
		})
	}
	_, err := fmt.Fprintln(r.out, buildinfo.String())
	return err
}

// help writes usage text. Help stays text even when --json was requested: the
// parser refuses that combination rather than accepting an option it ignores.
func (r renderer) help(path []string) error {
	text, err := commandUsage(path)
	if err != nil {
		return err
	}
	_, wErr := io.WriteString(r.out, text)
	return wErr
}

func writeJSON(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}

// writeDiagnostic renders a failure to w. In JSON mode every failure has the
// same document shape; the text form is the fallback when even that cannot be
// written.
func writeDiagnostic(w io.Writer, err error, jsonMode bool) {
	if jsonMode && writeJSON(w, errs.EnvelopeOf(err)) == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "Error: %v\n", err)
	if s := errs.SuggestionOf(err); s != "" {
		_, _ = fmt.Fprintf(w, "hint: %s\n", s)
	}
}
