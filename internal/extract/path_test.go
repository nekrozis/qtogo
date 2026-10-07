package extract

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetResolvesNamesUnderTheRoot(t *testing.T) {
	root := filepath.FromSlash("/dest")

	tests := []struct {
		name   string
		inside string // the slash-separated path it must resolve to, under the root
	}{
		{"readme.md", "readme.md"},
		{"docs/readme.md", "docs/readme.md"},
		{`docs\readme.md`, "docs/readme.md"}, // a backslash separates too
		{"a/b/c.txt", "a/b/c.txt"},
	}

	for _, tt := range tests {
		got, err := target(root, tt.name)
		if err != nil {
			t.Errorf("target(%q) = %v, want a path", tt.name, err)
			continue
		}
		if want := filepath.Join(root, filepath.FromSlash(tt.inside)); got != want {
			t.Errorf("target(%q) = %q, want %q", tt.name, got, want)
		}
	}
}

func TestTargetRefusesNamesThatLeaveTheRoot(t *testing.T) {
	root := filepath.FromSlash("/dest")

	names := []string{
		"",
		".",
		"..",
		"../outside",
		"a/../../outside",
		`..\outside`,
		`a\..\..\outside`,
		"/absolute",
		`\absolute`,
		"//double",
		"C:/windows",
		"a//b",
		`a\\b`,
		"a/./b",
		"trailing.",
		"trailing ",
		"colon:name",
		"nul",
		"CON",
		"com1.txt",
		"a\nb",
		"star*",
		"question?",
		"pipe|",
		`quote"`,
		"angle<",
		"a\x00b",
	}

	for _, name := range names {
		if got, err := target(root, name); err == nil {
			t.Errorf("target(%q) = %q, want a refusal", name, got)
		}
	}
}

func TestCheckLinkAllowsPlainRelativeTargets(t *testing.T) {
	targets := []string{
		"b.so",
		"c/d.txt",
		"Versions/Current/QtGui",
		`sub\b.so`, // a backslash separates too
		"5",
	}

	for _, target := range targets {
		if err := checkLink(target); err != nil {
			t.Errorf("checkLink(%q) = %v, want it allowed", target, err)
		}
	}
}

// A target that steps up and comes back resolves to somewhere inside the
// destination, and is still refused: two spellings must not name one location, or
// an archive can make two entries collide.
func TestCheckLinkRefusesAnythingButAPlainRelativeTarget(t *testing.T) {
	targets := []string{
		"",
		"a\x00b",
		"/etc/passwd",
		`\windows\system32`,
		"C:\\windows",
		"../outside",
		"../../outside",
		"sub/../top.txt",
		"../top.txt",
		"./x",
		".",
		"..",
		"a//b",
		`a\\b`,
		"colon:name",
		"nul",
		"CON",
		"trailing.",
		"trailing ",
		"a\nb",
	}

	for _, target := range targets {
		if err := checkLink(target); err == nil {
			t.Errorf("checkLink(%q) = nil, want a refusal", target)
		}
	}
}

// FuzzCheckLink holds the rule to its consequence: a target the check allows has to
// resolve inside the root, from wherever the link sits. The resolution is
// filepath.Join's, not the check's, so the two have to agree.
func FuzzCheckLink(f *testing.F) {
	for _, seed := range []string{"b.so", "c/d.txt", "../top.txt", "../../outside", "/etc/passwd", `a\b`, "", "CON"} {
		f.Add(seed)
	}

	root := filepath.FromSlash("/dest")
	sep := string(filepath.Separator)
	normalize := strings.NewReplacer("/", sep, `\`, sep)
	link := filepath.Join(root, "a", "b", "link")

	f.Fuzz(func(t *testing.T, linkTarget string) {
		if err := checkLink(linkTarget); err != nil {
			return
		}
		resolved := filepath.Join(filepath.Dir(link), normalize.Replace(linkTarget))
		rel, err := filepath.Rel(root, resolved)
		if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+sep) {
			t.Fatalf("checkLink allowed %q, which resolves to %q", linkTarget, resolved)
		}
	})
}

// FuzzTarget holds the policy to its one invariant: whatever name an archive
// carries, the path it resolves to is either refused or inside the root.
func FuzzTarget(f *testing.F) {
	for _, seed := range []string{"a/b.txt", `a\b.txt`, "../x", "/x", "", "..", "a//b", "nul", "a:b", "C:"} {
		f.Add(seed)
	}

	root := filepath.FromSlash("/dest")
	f.Fuzz(func(t *testing.T, name string) {
		got, err := target(root, name)
		if err != nil {
			return
		}
		rel, err := filepath.Rel(root, got)
		if err != nil {
			t.Fatalf("target(%q) = %q, which is not under the root: %v", name, got, err)
		}
		if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("target(%q) = %q escapes the root", name, got)
		}
	})
}
