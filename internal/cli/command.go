package cli

import "fmt"

// commandID identifies one runnable leaf of the command tree, so the tree can
// state which command a node is as data rather than as name comparisons in the
// dispatcher.
type commandID uint8

// cmdNone is the id of a node that is not runnable on its own: the root, a
// namespace, or a meta command.
const (
	cmdNone commandID = 0
	// cmdListQt is `qtogo list-qt`.
	cmdListQt commandID = 1
	// cmdPlanInstallQt is `qtogo plan install-qt`.
	cmdPlanInstallQt commandID = 2
	// cmdInstallQt is `qtogo install-qt`.
	cmdInstallQt commandID = 3
)

// metaAction is a request the dispatcher answers itself, before any business
// command runs.
type metaAction uint8

const (
	metaNone metaAction = iota
	metaHelp
	metaVersion
)

// argSpec is one positional argument a leaf declares. Identity — the host,
// target, version and so on a command acts on — is written positionally
// (ADR-009 decision 7); this is where a command says which ones it takes and in
// what order. Only trailing arguments may be optional.
type argSpec struct {
	name     string
	summary  string
	optional bool
}

// display renders the placeholder for a usage line: "<host>" or "[<arch>]".
func (a argSpec) display() string {
	if a.optional {
		return "[<" + a.name + ">]"
	}
	return "<" + a.name + ">"
}

// commandNode is one node of the command tree. The tree is pure data — which
// commands exist, how they nest, what each accepts — so adding a command cannot
// quietly change how a command line parses.
type commandNode struct {
	name     string
	summary  string
	options  []optionID
	args     []argSpec
	children []commandNode
	id       commandID
	meta     metaAction
}

// commandTree is the business surface: read it top to bottom and you have the
// CLI's complete vocabulary. A verb that is not here is absent, not a stub.
var commandTree = []commandNode{
	{
		name:    "list-qt",
		summary: "List the Qt versions a repository offers",
		args: []argSpec{
			{name: "host", summary: "The platform to list for"},
			{name: "target", summary: "The platform family to list for"},
		},
		options: []optionID{optJSON},
		id:      cmdListQt,
	},
	{
		name:    "plan",
		summary: "Show what an installation would do, without doing it",
		children: []commandNode{
			{
				name:    "install-qt",
				summary: "Plan a Qt installation",
				args: []argSpec{
					{name: "host", summary: "The platform to install for"},
					{name: "target", summary: "The platform family to install for"},
					{name: "version", summary: "The Qt version, e.g. 6.8.0"},
					{name: "arch", summary: "The architecture, e.g. win64_msvc2022_64", optional: true},
				},
				options: []optionID{optModules, optJSON},
				id:      cmdPlanInstallQt,
			},
		},
	},
	{
		name:    "install-qt",
		summary: "Install a Qt version",
		args: []argSpec{
			{name: "host", summary: "The platform to install for"},
			{name: "target", summary: "The platform family to install for"},
			{name: "version", summary: "The Qt version, e.g. 6.8.0"},
			{name: "arch", summary: "The architecture, e.g. win64_msvc2022_64", optional: true},
		},
		options: []optionID{optModules, optOutputDir, optOverwrite, optDryRun, optJSON},
		id:      cmdInstallQt,
	},
}

// metaCommands are answered by the dispatcher. They are reserved words, present
// in every build including one with an empty business tree.
var metaCommands = []commandNode{
	{
		name:    "help",
		summary: "Show help for a command",
		options: []optionID{optHelp},
		meta:    metaHelp,
	},
	{
		name:    "version",
		summary: "Show the version",
		options: []optionID{optHelp, optJSON},
		meta:    metaVersion,
	},
}

// commandRoots is the order in which a word is matched: meta commands are
// reserved, so they win over a business command that shares a name.
func commandRoots() []commandNode {
	roots := make([]commandNode, 0, len(metaCommands)+len(commandTree))
	roots = append(roots, metaCommands...)
	roots = append(roots, commandTree...)
	return roots
}

// findChild looks a word up among nodes. The first match wins, so names must be
// unique within a level.
func findChild(nodes []commandNode, name string) (commandNode, bool) {
	for _, n := range nodes {
		if n.name == name {
			return n, true
		}
	}
	return commandNode{}, false
}

// accepts reports whether the node accepts the option. Common options are
// always available; everything else has to be declared.
func (n commandNode) accepts(id optionID) bool {
	if isCommonOption(id) {
		return true
	}
	return containsOption(n.options, id)
}

// requiredArgs counts the positional arguments the node requires. Only trailing
// arguments may be optional, so this is the length of the required prefix.
func (n commandNode) requiredArgs() int {
	required := 0
	for _, a := range n.args {
		if !a.optional {
			required++
		}
	}
	return required
}

// commandPaths maps each runnable leaf to its verb path, so a failure can name
// the command it happened in instead of a number.
var commandPaths = buildCommandPaths()

func buildCommandPaths() map[commandID]string {
	paths := make(map[commandID]string)

	var walk func(prefix string, nodes []commandNode)
	walk = func(prefix string, nodes []commandNode) {
		for _, n := range nodes {
			full := n.name
			if prefix != "" {
				full = prefix + " " + n.name
			}
			if n.id != cmdNone {
				paths[n.id] = full
			}
			walk(full, n.children)
		}
	}
	walk("", commandTree)

	return paths
}

// path returns the command's verb path, or a placeholder when the id is not in
// the tree — which would mean a node was given an id while being unreachable.
func (id commandID) path() string {
	if p, ok := commandPaths[id]; ok {
		return p
	}
	return fmt.Sprintf("commandID(%d)", uint8(id))
}
