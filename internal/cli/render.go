package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/nekrozis/qtogo/internal/buildinfo"
	"github.com/nekrozis/qtogo/internal/catalog"
	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/service"
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

// planPayload is the JSON shape of a successful plan install-qt.
type planPayload struct {
	Host     string            `json:"host"`
	Target   string            `json:"target"`
	Version  string            `json:"version"`
	Arch     string            `json:"arch"`
	Packages []string          `json:"packages"`
	Archives []catalog.Archive `json:"archives"`
}

// planInstallQt renders what an installation would fetch, one archive per line in
// text and as a document in JSON.
func (r renderer) planInstallQt(ctx context.Context, inv invocation, svc Services) error {
	host, kind, version, arch, modules, err := planRequest(inv)
	if err != nil {
		return err
	}
	plan, err := svc.PlanInstallQt(ctx, host, kind, version, arch, modules)
	if err != nil {
		return err
	}
	return r.writePlan(host, kind, plan)
}

// installPayload is the JSON shape of a successful install-qt.
type installPayload struct {
	Host        string        `json:"host"`
	Target      string        `json:"target"`
	Path        string        `json:"path,omitempty"`
	Policy      string        `json:"policy,omitempty"`
	Relocatable bool          `json:"relocatable"`
	Replaced    bool          `json:"replaced,omitempty"`
	Plan        *catalog.Plan `json:"plan,omitempty"`
}

// installQt installs, or with --dry-run shows exactly what plan install-qt shows:
// the same planner, the same rendering, by construction.
func (r renderer) installQt(ctx context.Context, inv invocation, svc Services) error {
	host, kind, version, arch, modules, err := planRequest(inv)
	if err != nil {
		return err
	}

	opts := service.InstallOptions{
		OutputDir: optionValue(inv, optOutputDir),
		Overwrite: hasOption(inv.used, optOverwrite),
		DryRun:    hasOption(inv.used, optDryRun),
	}
	if raw := optionValue(inv, optMemoryBudget); raw != "" {
		budget, err := parseSize(raw)
		if err != nil {
			return err
		}
		opts.MemoryBudget = budget
	}
	// A dry run is the plan command, not a plan of a different kind: it renders
	// through the same function so the two outputs cannot drift.
	if opts.DryRun {
		plan, err := svc.PlanInstallQt(ctx, host, kind, version, arch, modules)
		if err != nil {
			return err
		}
		return r.writePlan(host, kind, plan)
	}

	installed, err := svc.InstallQt(ctx, host, kind, version, arch, modules, opts)
	if err != nil {
		return err
	}
	if r.json {
		return writeJSON(r.out, installPayload{
			Host:        string(host),
			Target:      string(kind),
			Path:        installed.Path,
			Policy:      installed.Policy,
			Relocatable: installed.Relocatable,
			Replaced:    installed.Replaced,
			Plan:        &installed.Plan,
		})
	}
	_, err = fmt.Fprintf(r.out, "installed %s %s %s to %s\n",
		installed.Plan.Version, installed.Plan.Arch, installed.Policy, installed.Path)
	return err
}

// writePlan renders a plan: the document in JSON, one archive per line in text.
// Both the plan command and install-qt's dry run go through here, which is what
// makes them the same output rather than merely similar.
func (r renderer) writePlan(host model.Host, kind model.Kind, plan catalog.Plan) error {
	if r.json {
		return writeJSON(r.out, planPayload{
			Host:     string(host),
			Target:   string(kind),
			Version:  plan.Version,
			Arch:     plan.Arch,
			Packages: plan.Packages,
			Archives: plan.Archives,
		})
	}
	for _, a := range plan.Archives {
		line := a.URL
		if a.InstallPath != "" {
			line += "\t" + a.InstallPath
		}
		if _, err := fmt.Fprintln(r.out, line); err != nil {
			return err
		}
	}
	return nil
}

// planRequest reads the host, target, version, architecture and modules a plan
// was asked for. The architecture is optional — the catalog resolves it when the
// metadata offers exactly one — and the modules are the reference tool's
// repeatable --modules, each value also read as a comma-separated list.
func planRequest(inv invocation) (model.Host, model.Kind, model.Version, string, []string, error) {
	fail := func(err error) (model.Host, model.Kind, model.Version, string, []string, error) {
		return "", "", model.Version{}, "", nil, err
	}

	host, err := model.ParseHost(inv.arg("host"))
	if err != nil {
		return fail(errs.Usagef(errs.CodeUnexpectedValue, "%v", err).WithSuggestion("%s", helpHint()))
	}
	kind, err := model.ParseKind(inv.arg("target"))
	if err != nil {
		return fail(errs.Usagef(errs.CodeUnexpectedValue, "%v", err).WithSuggestion("%s", helpHint()))
	}
	version, err := model.ParseVersion(inv.arg("version"))
	if err != nil {
		return fail(errs.Usagef(errs.CodeUnexpectedValue, "%v", err).WithSuggestion("%s", helpHint()))
	}

	return host, kind, version, inv.arg("arch"), modules(inv), nil
}

// modules collects --modules values in order, splitting each on commas so that
// "--modules a,b" reads the same as "--modules a --modules b". The comma form is
// this build's convenience; the reference tool takes a space-separated list after
// one "-m/--modules" (and a bare "all" for every module, which this build does
// not implement).
func modules(inv invocation) []string {
	var out []string
	for _, value := range inv.values[optModules] {
		for _, m := range strings.Split(value, ",") {
			if m = strings.TrimSpace(m); m != "" {
				out = append(out, m)
			}
		}
	}
	return out
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
