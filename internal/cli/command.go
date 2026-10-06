package cli

import "fmt"

// commandID identifies one runnable leaf of the command tree, so the tree can
// state which command a node is as data rather than as name comparisons in the
// dispatcher.
type commandID uint8

// cmdNone is the id of a node that is not runnable on its own: the root, a
// namespace, or a meta command.
const cmdNone commandID = 0

// metaAction is a request the dispatcher answers itself, before any business
// command runs.
type metaAction uint8

const (
	metaNone metaAction = iota
	metaHelp
	metaVersion
)

// commandNode is one node of the command tree. The tree is pure data — which
// commands exist, how they nest, what each accepts — so adding a command cannot
// quietly change how a command line parses.
type commandNode struct {
	name     string
	summary  string
	options  []optionID
	children []commandNode
	id       commandID
	meta     metaAction
}

// commandTree is the business surface: read it top to bottom and you have the
// CLI's complete vocabulary. It stays empty until the listing and install
// commands are implemented; a verb that is not here is absent, not a stub.
var commandTree = []commandNode{}

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
