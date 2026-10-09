package cli

import "testing"

// TestCommandTreeIsWellFormed walks every node. It fails if a command is added
// without a summary, if two siblings share a name, or if a node claims an option
// the option table does not define.
func TestCommandTreeIsWellFormed(t *testing.T) {
	var walk func(path string, nodes []commandNode)
	walk = func(path string, nodes []commandNode) {
		seen := make(map[string]bool, len(nodes))

		for _, n := range nodes {
			where := path + n.name
			if n.name == "" {
				t.Errorf("%s: empty name", where)
			}
			if n.summary == "" {
				t.Errorf("%s: empty summary", where)
			}
			if seen[n.name] {
				t.Errorf("%s: duplicate name at this level", where)
			}
			seen[n.name] = true

			for _, id := range n.options {
				if _, ok := optionByID(id); !ok {
					t.Errorf("%s: declares option id %d, which is not in the option table", where, id)
				}
			}

			seenArg := make(map[string]bool, len(n.args))
			optionalSeen := false
			for _, a := range n.args {
				if a.name == "" {
					t.Errorf("%s: positional argument with an empty name", where)
				}
				if a.summary == "" {
					t.Errorf("%s: argument %q has no summary", where, a.name)
				}
				if seenArg[a.name] {
					t.Errorf("%s: duplicate argument %q", where, a.name)
				}
				seenArg[a.name] = true
				if a.optional {
					optionalSeen = true
				} else if optionalSeen {
					t.Errorf("%s: required argument %q follows an optional one", where, a.name)
				}
			}

			walk(where+" ", n.children)
		}
	}
	walk("", commandRoots())
}

// TestMetaCommandsAreReserved pins that help and version resolve to the
// dispatcher's own commands rather than to business nodes.
func TestMetaCommandsAreReserved(t *testing.T) {
	reserved := map[string]metaAction{
		"help":    metaHelp,
		"version": metaVersion,
	}

	for name, want := range reserved {
		node, ok := findChild(commandRoots(), name)
		if !ok {
			t.Fatalf("%q is not in the command tree", name)
		}
		if node.meta != want {
			t.Errorf("%q: meta = %d, want %d", name, node.meta, want)
		}
		if node.id != cmdNone {
			t.Errorf("%q: id = %d, want cmdNone because meta commands are not dispatched by id", name, node.id)
		}
	}
}

func TestOptionTableIsConsistent(t *testing.T) {
	ids := make(map[optionID]bool, len(optionTable))
	longs := make(map[string]bool, len(optionTable))
	aliases := make(map[string]bool, len(optionTable))

	for _, s := range optionTable {
		if s.long == "" {
			t.Errorf("option id %d has no long name", s.id)
		}
		if s.summary == "" {
			t.Errorf("--%s has no summary", s.long)
		}
		if ids[s.id] {
			t.Errorf("option id %d is declared twice", s.id)
		}
		ids[s.id] = true
		if longs[s.long] {
			t.Errorf("--%s is declared twice", s.long)
		}
		longs[s.long] = true

		for _, a := range s.aliases {
			if aliases[a] {
				t.Errorf("alias -%s is declared twice", a)
			}
			if longs[a] {
				t.Errorf("alias -%s collides with a long name", a)
			}
			aliases[a] = true
		}
	}

	for _, id := range append(append([]optionID{}, rootOptions...), commonOptions...) {
		if !ids[id] {
			t.Errorf("option id %d is used by the root or common set but is not in the option table", id)
		}
	}
}

func TestOptionLabels(t *testing.T) {
	tests := []struct {
		id      optionID
		display string
		label   string
	}{
		{optHelp, "--help", "-h, --help"},
		{optVersion, "--version", "--version"},
		{optJSON, "--json", "--json"},
	}

	for _, tt := range tests {
		spec, ok := optionByID(tt.id)
		if !ok {
			t.Fatalf("option id %d is missing from the table", tt.id)
		}
		if got := spec.display(); got != tt.display {
			t.Errorf("id %d: display() = %q, want %q", tt.id, got, tt.display)
		}
		if got := spec.label(); got != tt.label {
			t.Errorf("id %d: label() = %q, want %q", tt.id, got, tt.label)
		}
	}
}

func TestCommandAccepts(t *testing.T) {
	help, _ := findChild(commandRoots(), "help")
	version, _ := findChild(commandRoots(), "version")

	tests := []struct {
		name string
		node commandNode
		id   optionID
		want bool
	}{
		{"common options reach every command", help, optHelp, true},
		{"version declares json", version, optJSON, true},
		{"version does not declare the version flag", version, optVersion, false},
		{"help does not declare json", help, optJSON, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.node.accepts(tt.id); got != tt.want {
				t.Errorf("accepts(%d) = %t, want %t", tt.id, got, tt.want)
			}
		})
	}
}
