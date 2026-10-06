package cli

import (
	"fmt"
	"strings"

	"github.com/nekrozis/qtogo/internal/buildinfo"
)

// rootUsage is the usage text for the whole program, generated from the command
// tree so it cannot advertise a verb this build does not have.
func rootUsage() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s <command> [options]\n", buildinfo.ProgramName)
	writeCommandList(&b, commandRoots())
	writeOptionBlock(&b, rootOptions)
	return b.String()
}

// commandUsage is the usage text for one command path. An empty path is the
// root.
func commandUsage(path []string) (string, error) {
	if len(path) == 0 {
		return rootUsage(), nil
	}
	node, consumed, err := resolveCommand(path)
	if err != nil {
		return "", err
	}
	if consumed != len(path) {
		return "", unknownCommand(path[consumed])
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s %s [options]\n", buildinfo.ProgramName, strings.Join(path, " "))
	if node.summary != "" {
		fmt.Fprintf(&b, "\n%s\n", node.summary)
	}
	writeCommandList(&b, node.children)
	writeOptionBlock(&b, node.options)

	return b.String(), nil
}

// writeCommandList writes the "Commands:" block, or nothing when the node has no
// children.
func writeCommandList(b *strings.Builder, nodes []commandNode) {
	if len(nodes) == 0 {
		return
	}

	width := 0
	for _, n := range nodes {
		if len(n.name) > width {
			width = len(n.name)
		}
	}

	b.WriteString("\nCommands:\n")
	for _, n := range nodes {
		fmt.Fprintf(b, "  %-*s  %s\n", width, n.name, n.summary)
	}
}

// writeOptionBlock writes the "Options:" block for a command's option set plus
// the common options.
func writeOptionBlock(b *strings.Builder, ids []optionID) {
	type entry struct{ label, summary string }

	entries := make([]entry, 0, len(ids)+len(commonOptions))
	for _, id := range unionOptions(ids, commonOptions) {
		spec, ok := optionByID(id)
		if !ok {
			continue
		}
		entries = append(entries, entry{label: spec.label(), summary: spec.summary})
	}

	width := 0
	for _, e := range entries {
		if len(e.label) > width {
			width = len(e.label)
		}
	}

	b.WriteString("\nOptions:\n")
	for _, e := range entries {
		fmt.Fprintf(b, "  %-*s  %s\n", width, e.label, e.summary)
	}
}
