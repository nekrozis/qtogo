package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/nekrozis/qtogo/internal/buildinfo"
	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/model"
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

// listQtPayload is the JSON shape of a successful list-qt.
type listQtPayload struct {
	Host     string   `json:"host"`
	Target   string   `json:"target"`
	Versions []string `json:"versions"`
}

// listQt renders the versions a target offers, one per line in text and as a
// document in JSON.
func (r renderer) listQt(ctx context.Context, inv invocation, svc Services) error {
	host, kind, err := listQtRequest(inv)
	if err != nil {
		return err
	}
	versions, err := svc.ListQtVersions(ctx, host, kind)
	if err != nil {
		return err
	}
	if r.json {
		names := make([]string, len(versions))
		for i, v := range versions {
			names[i] = v.Dotted()
		}
		return writeJSON(r.out, listQtPayload{Host: string(host), Target: string(kind), Versions: names})
	}
	for _, v := range versions {
		if _, err := fmt.Fprintln(r.out, v.Dotted()); err != nil {
			return err
		}
	}
	return nil
}

// listQtRequest reads the host and target list-qt was asked for. Both are
// positional and required, so the arity check has already guaranteed they are
// present.
func listQtRequest(inv invocation) (model.Host, model.Kind, error) {
	host, err := model.ParseHost(inv.arg("host"))
	if err != nil {
		return "", "", errs.Usagef(errs.CodeUnexpectedValue, "%v", err).WithSuggestion("%s", helpHint())
	}

	kind, err := model.ParseKind(inv.arg("target"))
	if err != nil {
		return "", "", errs.Usagef(errs.CodeUnexpectedValue, "%v", err).WithSuggestion("%s", helpHint())
	}
	return host, kind, nil
}

// writeJSON writes a document that automation reads.
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
