package cli

import (
	"context"
	"io"

	"github.com/nekrozis/qtogo/internal/buildinfo"
	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// Run parses one command line and executes it. Payloads go to out and failures
// come back as an error; nothing here touches os.Args, os.Stdout or os.Exit,
// which is what makes the front end testable.
func Run(ctx context.Context, args []string, out, errOut io.Writer, svc Services) error {
	inv, err := parseArgs(args)
	if err != nil {
		return err
	}
	return dispatch(ctx, inv, newRenderer(out, errOut, inv.json), svc)
}

// Main runs one command line and returns the process exit code. It is the only
// caller of exitcode.Classify, so the contract has a single implementation.
func Main(ctx context.Context, args []string, out, errOut io.Writer, svc Services) int {
	if err := Run(ctx, args, out, errOut, svc); err != nil {
		writeDiagnostic(errOut, err, wantsJSON(args))
		return exitcode.Classify(err)
	}
	return exitcode.OK
}

// dispatch executes a parsed invocation.
func dispatch(ctx context.Context, inv invocation, r renderer, svc Services) error {
	switch inv.meta {
	case metaHelp:
		return r.help(inv.helpPath)
	case metaVersion:
		return r.version()
	}

	if inv.node == nil {
		return bareInvocation(r)
	}

	if inv.node.id == cmdListQt {
		return r.listQt(ctx, inv, svc)
	}
	if inv.node.id == cmdPlanInstallQt {
		return r.planInstallQt(ctx, inv, svc)
	}
	if inv.node.id == cmdInstallQt {
		return r.installQt(ctx, inv, svc)
	}

	// A namespace node groups subcommands and runs nothing itself, so a line
	// that stops at one is missing its subcommand.
	if len(inv.node.children) > 0 {
		return errs.Usagef(errs.CodeMissingArgument, "%s needs a subcommand", topicLabel(inv.path)).
			WithSuggestion("%s", helpHint())
	}

	// Business commands are dispatched here as they are implemented. A node that
	// reaches this line was added to the tree without a handler, which must fail
	// rather than return success.
	return errs.Usagef(errs.CodeUnknownCommand, "%s has no handler in this build", inv.cmd.path())
}

// bareInvocation reports a command line with no command at all. The usage goes
// to stderr and the failure is a usage error, so forgetting the command cannot
// read as success in a script.
func bareInvocation(r renderer) error {
	_, _ = io.WriteString(r.errOut, buildinfo.String()+"\n\n"+rootUsage())
	return errs.Usagef(errs.CodeMissingArgument, "no command given").
		WithSuggestion("%s", helpHint())
}
