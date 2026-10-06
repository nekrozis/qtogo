package cli

import (
	"io"

	"github.com/nekrozis/qtogo/internal/buildinfo"
	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// Run parses one command line and executes it. Payloads go to out and failures
// come back as an error; nothing here touches os.Args, os.Stdout or os.Exit,
// which is what makes the front end testable.
func Run(args []string, out, errOut io.Writer) error {
	inv, err := parseArgs(args)
	if err != nil {
		return err
	}
	return dispatch(inv, newRenderer(out, errOut, inv.json))
}

// Main runs one command line and returns the process exit code. It is the only
// caller of exitcode.Classify, so the contract has a single implementation.
func Main(args []string, out, errOut io.Writer) int {
	if err := Run(args, out, errOut); err != nil {
		writeDiagnostic(errOut, err, wantsJSON(args))
		return exitcode.Classify(err)
	}
	return exitcode.OK
}

// dispatch executes a parsed invocation.
func dispatch(inv invocation, r renderer) error {
	switch inv.meta {
	case metaHelp:
		return r.help(inv.helpPath)
	case metaVersion:
		return r.version()
	}

	if inv.node == nil {
		return bareInvocation(r)
	}

	// Business commands are dispatched here as they are implemented. Until one
	// exists, a resolved command is always a meta command, which was handled
	// above — so reaching this line would mean a node was added without a
	// handler, and that must fail rather than return success.
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
