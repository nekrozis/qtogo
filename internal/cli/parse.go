package cli

import (
	"fmt"
	"strings"

	"github.com/nekrozis/qtogo/internal/buildinfo"
	"github.com/nekrozis/qtogo/internal/errs"
)

// invocation is one parsed command line.
type invocation struct {
	// meta is set when the line asks for something the dispatcher answers
	// itself.
	meta metaAction

	// helpPath is the command to describe when meta is metaHelp; empty means
	// the root.
	helpPath []string

	// node is the resolved command, or nil when no command word was given.
	node *commandNode

	// cmd is node.id when node is set.
	cmd commandID

	// json mirrors --json.
	json bool

	// path is the command words that were consumed, e.g. ["plan", "install-qt"].
	// It names the command in a failure even for a namespace node, which carries
	// no id of its own.
	path []string

	// args holds the positional arguments, in the order the command declares
	// them. arg looks one up by name.
	args []string

	// values holds the value of each value-taking option, in the order they were
	// written. A repeated option keeps every value, so the first is the one an
	// option that takes one value uses and a list option reads them all.
	values map[optionID][]string
}

// parseArgs turns a command line into an invocation. All grammar lives here and
// in options.go, which is what keeps the parser replaceable.
func parseArgs(args []string) (invocation, error) {
	var inv invocation

	used, words, err := split(args)
	if err != nil {
		return invocation{}, err
	}
	inv.values = collectValues(used)
	inv.json = hasOption(used, optJSON)

	// Help wins over everything: it is the one thing a user can always ask for.
	if hasOption(used, optHelp) {
		path, node, err := resolveHelpTopic(words)
		if err != nil {
			return invocation{}, err
		}
		if err := checkOptions(used, node, topicLabel(path)); err != nil {
			return invocation{}, err
		}
		inv.meta, inv.helpPath, inv.node = metaHelp, path, node
		return inv, nil
	}

	// A bare --version is the version request; after a command word, that
	// command's own options decide.
	if hasOption(used, optVersion) && len(words) == 0 {
		inv.meta = metaVersion
		return inv, nil
	}

	if len(words) == 0 {
		// No command at all: the caller reports the bare invocation, but the
		// options still have to be ones the root accepts.
		if err := checkOptions(used, nil, buildinfo.ProgramName); err != nil {
			return invocation{}, err
		}
		return inv, nil
	}

	node, consumed, err := resolveCommand(words)
	if err != nil {
		return invocation{}, err
	}
	inv.node = &node
	inv.cmd = node.id
	inv.meta = node.meta

	if err := checkOptions(used, &node, topicLabel(words[:consumed])); err != nil {
		return invocation{}, err
	}

	rest := words[consumed:]
	if node.meta == metaHelp {
		path, _, err := resolveHelpTopic(rest)
		if err != nil {
			return invocation{}, err
		}
		inv.helpPath = path
		return inv, nil
	}
	if err := checkArity(&node, rest, words[:consumed]); err != nil {
		return invocation{}, err
	}
	inv.path = words[:consumed]
	inv.args = rest

	return inv, nil
}

// arg returns the positional argument with the declared name, or "" when it was
// not given. An argument that can be absent reads its absence as the empty
// string; names come from the command node, so a command reorders its arguments
// without changing its readers.
func (inv invocation) arg(name string) string {
	if inv.node == nil {
		return ""
	}
	for i, a := range inv.node.args {
		if a.name == name && i < len(inv.args) {
			return inv.args[i]
		}
	}
	return ""
}

// checkArity reports a command line with too few or too many positional
// arguments. Only trailing arguments may be optional, so the required ones are a
// prefix and the first missing one is the one to name.
func checkArity(node *commandNode, words []string, path []string) error {
	if len(words) < node.requiredArgs() {
		return errs.Usagef(errs.CodeMissingArgument, "%s needs %s",
			topicLabel(path), node.args[len(words)].display()).
			WithSuggestion("%s", helpHint())
	}
	if len(words) > len(node.args) {
		return unexpectedArgument(words[len(node.args)])
	}
	return nil
}

// usedOption is one option found on the command line, with its value when it
// takes one.
type usedOption struct {
	spec  optionSpec
	value string
}

// collectValues gathers the value of each value-taking option, keeping repeats.
func collectValues(used []usedOption) map[optionID][]string {
	values := make(map[optionID][]string)
	for _, u := range used {
		if u.spec.takesValue {
			values[u.spec.id] = append(values[u.spec.id], u.value)
		}
	}
	return values
}

// split separates options from words. A "--" token ends option parsing. An option
// that takes a value reads it from "--name=value" or from the word after it.
func split(args []string) ([]usedOption, []string, error) {
	used := make([]usedOption, 0, len(args))
	words := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			words = append(words, args[i+1:]...)
			break
		}
		if !isOptionToken(a) {
			words = append(words, a)
			continue
		}

		name, inline, hasValue := cutOption(a)
		spec, ok := lookupOption(name)
		if !ok {
			return nil, nil, unknownOption(name)
		}
		if !spec.takesValue {
			if hasValue {
				return nil, nil, errs.Usagef(errs.CodeUnexpectedValue, "option %s does not take a value", spec.display()).
					WithSuggestion("write %s on its own", spec.display())
			}
			used = append(used, usedOption{spec: spec})
			continue
		}
		if !hasValue {
			if i+1 == len(args) || isOptionToken(args[i+1]) {
				return nil, nil, errs.Usagef(errs.CodeMissingArgument, "option %s needs a value", spec.display()).
					WithSuggestion("write it as %s", spec.display())
			}
			i++
			inline = args[i]
		}
		used = append(used, usedOption{spec: spec, value: inline})
	}

	return used, words, nil
}

// isOptionToken reports whether the token is an option rather than a word. A
// lone "-" is a word.
func isOptionToken(a string) bool { return len(a) > 1 && a[0] == '-' }

// cutOption splits "--name=value" into its parts. An alias is written "-h";
// clustering is not supported.
func cutOption(a string) (name, value string, hasValue bool) {
	name = strings.TrimPrefix(a, "-")
	name = strings.TrimPrefix(name, "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		return name[:i], name[i+1:], true
	}
	return name, "", false
}

// resolveCommand walks the tree one word at a time and reports how many words it
// consumed. Words past the deepest match are the command's arguments.
func resolveCommand(words []string) (commandNode, int, error) {
	nodes := commandRoots()
	var current commandNode
	consumed := 0

	for _, w := range words {
		child, ok := findChild(nodes, w)
		if !ok {
			break
		}
		current = child
		nodes = child.children
		consumed++
	}
	if consumed == 0 {
		return commandNode{}, 0, unknownCommand(words[0])
	}
	return current, consumed, nil
}

// resolveHelpTopic resolves the words after "help", or the words given with
// --help, to the command to describe. An empty topic is the root.
func resolveHelpTopic(words []string) ([]string, *commandNode, error) {
	if len(words) == 0 {
		return nil, nil, nil
	}
	node, consumed, err := resolveCommand(words)
	if err != nil {
		return nil, nil, err
	}
	if consumed != len(words) {
		return nil, nil, unknownCommand(words[consumed])
	}
	return words, &node, nil
}

// checkOptions reports the first option the command does not accept.
func checkOptions(used []usedOption, node *commandNode, command string) error {
	allowed := rootOptions
	if node != nil {
		allowed = node.options
	}

	for _, u := range used {
		if isCommonOption(u.spec.id) || containsOption(allowed, u.spec.id) {
			continue
		}
		return errs.Usagef(errs.CodeUnknownOption, "option %s is not accepted by %s", u.spec.display(), command).
			WithSuggestion("%s", helpHint())
	}
	return nil
}

func hasOption(used []usedOption, id optionID) bool {
	for _, u := range used {
		if u.spec.id == id {
			return true
		}
	}
	return false
}

// topicLabel names a command path the way the user would type it.
func topicLabel(path []string) string {
	if len(path) == 0 {
		return buildinfo.ProgramName
	}
	return buildinfo.ProgramName + " " + strings.Join(path, " ")
}

// wantsJSON reports whether the raw command line asks for JSON output. It reads
// argv rather than the parse result on purpose: when parsing is what failed, the
// failure still has to arrive as JSON for the automation that asked for it.
func wantsJSON(args []string) bool {
	for _, a := range args {
		if a == "--json" {
			return true
		}
	}
	return false
}

func unknownCommand(name string) *errs.Error {
	return errs.Usagef(errs.CodeUnknownCommand, "unknown command %q", name).
		WithSuggestion("%s", helpHint())
}

func unknownOption(name string) *errs.Error {
	if hint, ok := removedOptions[name]; ok {
		return errs.Usagef(errs.CodeUnknownOption, "option --%s is not supported", name).
			WithSuggestion("%s", hint)
	}
	return errs.Usagef(errs.CodeUnknownOption, "unknown option --%s", name).
		WithSuggestion("%s", helpHint())
}

func unexpectedArgument(word string) *errs.Error {
	return errs.Usagef(errs.CodeUnexpectedArg, "unexpected argument %q", word).
		WithSuggestion("%s", helpHint())
}

// removedOptions maps an option the reference implementation accepts but this
// build does not, to the text the user should read instead. Every hint has to
// name something the tree actually has.
var removedOptions = map[string]string{
	"host":   "write the host positionally, as in `qtogo list-qt windows desktop`",
	"target": "write the target positionally, as in `qtogo list-qt windows desktop`",
}

// helpHint is the one "what now" line every usage failure carries.
func helpHint() string {
	return fmt.Sprintf("run %q to see the available commands", buildinfo.ProgramName+" help")
}
