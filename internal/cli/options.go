package cli

import (
	"math"
	"strconv"
	"strings"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// optionID identifies one option. Ids exist so the command tree can state what
// it accepts as data rather than as name comparisons.
type optionID uint8

const (
	optHelp optionID = iota
	optVersion
	optJSON
	optModules
	optOutputDir
	optOverwrite
	optDryRun
	optMemoryBudget
)

// optionSpec is the vocabulary entry for one option.
type optionSpec struct {
	id      optionID
	long    string
	aliases []string
	summary string
	// takesValue marks an option that carries a value, written either as
	// "--name value" or "--name=value".
	takesValue bool
	// value names what the value is, for the usage text ("--host <host>").
	value string
}

var optionTable = []optionSpec{
	{id: optHelp, long: "help", aliases: []string{"h"}, summary: "Show help"},
	{id: optVersion, long: "version", summary: "Show the version"},
	{id: optJSON, long: "json", summary: "Write machine-readable JSON instead of text"},
	{
		id: optModules, long: "modules", aliases: []string{"m"},
		summary: "A module to include (repeatable)", takesValue: true, value: "module",
	},
	{
		id: optOutputDir, long: "outputdir", aliases: []string{"O"},
		summary: "The directory to install into", takesValue: true, value: "dir",
	},
	{
		id: optOverwrite, long: "overwrite",
		summary: "Replace an existing installation at the destination",
	},
	{
		id: optDryRun, long: "dry-run",
		summary: "Show what would be installed, without installing it",
	},
	{
		id: optMemoryBudget, long: "memory-budget",
		summary:    "The most memory one archive's block may need while extracting",
		takesValue: true, value: "size",
	},
}

// parseSize reads a byte size with an optional unit suffix: "4G", "512M", "1GiB",
// or a bare number of bytes. Case does not matter, and the binary meaning is
// meant (a G is 1 GiB), which is how the limit it feeds is written.
func parseSize(raw string) (uint64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, errs.New(exitcode.Usage, errs.CodeUnexpectedValue, errs.PhaseConfig, "a size is required")
	}
	// A trailing "B" is decoration: "4GB" and "4G" mean the same.
	s = strings.TrimSuffix(strings.TrimSuffix(s, "B"), "b")

	mult := uint64(1)
	switch last := s[len(s)-1]; last {
	case 'K', 'k':
		mult, s = 1<<10, s[:len(s)-1]
	case 'M', 'm':
		mult, s = 1<<20, s[:len(s)-1]
	case 'G', 'g':
		mult, s = 1<<30, s[:len(s)-1]
	case 'T', 't':
		mult, s = 1<<40, s[:len(s)-1]
	}

	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, errs.Wrap(exitcode.Usage, errs.CodeUnexpectedValue, errs.PhaseConfig, err,
			"%q is not a size (try 4G, 512M, or a byte count)", raw)
	}
	if n == 0 {
		return 0, errs.New(exitcode.Usage, errs.CodeUnexpectedValue, errs.PhaseConfig,
			"%q is not a usable size", raw)
	}
	if n > math.MaxUint64/mult {
		return 0, errs.New(exitcode.Usage, errs.CodeUnexpectedValue, errs.PhaseConfig, "%q is too large", raw)
	}
	return n * mult, nil
}

// commonOptions are accepted by every command: asking for help is always
// meaningful, so no command has to remember to declare it.
var commonOptions = []optionID{optHelp}

// rootOptions are accepted when no command was named.
var rootOptions = []optionID{optHelp, optVersion, optJSON}

func isCommonOption(id optionID) bool { return containsOption(commonOptions, id) }

func containsOption(ids []optionID, want optionID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// lookupOption resolves a written option name — long form or alias — to its
// spec.
func lookupOption(name string) (optionSpec, bool) {
	for _, s := range optionTable {
		if s.long == name || s.hasAlias(name) {
			return s, true
		}
	}
	return optionSpec{}, false
}

// optionByID resolves an id back to its spec.
func optionByID(id optionID) (optionSpec, bool) {
	for _, s := range optionTable {
		if s.id == id {
			return s, true
		}
	}
	return optionSpec{}, false
}

// unionOptions returns ids followed by extras, without duplicates.
func unionOptions(ids, extras []optionID) []optionID {
	out := make([]optionID, 0, len(ids)+len(extras))
	for _, group := range [][]optionID{ids, extras} {
		for _, id := range group {
			if !containsOption(out, id) {
				out = append(out, id)
			}
		}
	}
	return out
}

func (s optionSpec) hasAlias(name string) bool {
	for _, a := range s.aliases {
		if a == name {
			return true
		}
	}
	return false
}

// display renders the long form with its value placeholder, the way the user
// would type it.
func (s optionSpec) display() string {
	if s.takesValue {
		return "--" + s.long + " <" + s.value + ">"
	}
	return "--" + s.long
}

// label renders "-h, --help".
func (s optionSpec) label() string {
	if len(s.aliases) == 0 {
		return s.display()
	}
	short := make([]string, 0, len(s.aliases))
	for _, a := range s.aliases {
		short = append(short, "-"+a)
	}
	return strings.Join(short, ", ") + ", " + s.display()
}
