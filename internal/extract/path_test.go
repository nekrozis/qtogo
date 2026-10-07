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

func TestCheckLinkAllowsTargetsThatStayInside(t *testing.T) {
	root := filepath.FromSlash("/dest")

	tests := []struct {
		link   string // the link's path under the root
		target string
	}{
		{"lib/a.so", "b.so"},
		{"lib/a.so", "../outside"}, // root/outside, still inside the root
		{"sub/up", "../top.txt"},
		{"a/b/link", "c/d.txt"},
		{"a/link", "./x"},
	}

	for _, tt := range tests {
		link := filepath.Join(root, filepath.FromSlash(tt.link))
		if err := checkLink(root, link, tt.target); err != nil {
			t.Errorf("checkLink(%q -> %q) = %v, want it allowed", tt.link, tt.target, err)
		}
	}
}

func TestCheckLinkRefusesTargetsThatLeaveTheRoot(t *testing.T) {
	root := filepath.FromSlash("/dest")

	tests := []struct {
		link   string
		target string
	}{
		{"lib/a.so", "../../outside"},
		{"a/b/link", "../../../x"},
		{"link", "../outside"},
		{"link", "/etc/passwd"},
		{"link", `\windows\system32`},
		{"link", "C:\\windows"},
		{"link", ""},
		{"link", "a\x00b"},
	}

	for _, tt := range tests {
		link := filepath.Join(root, filepath.FromSlash(tt.link))
		if err := checkLink(root, link, tt.target); err == nil {
			t.Errorf("checkLink(%q -> %q) = nil, want a refusal", tt.link, tt.target)
		}
	}
}

// FuzzCheckLink holds the same invariant for a symlink that FuzzTarget holds for a
// name: a target the check allows has to resolve inside the root. The resolution
// here is filepath.Join's, not the check's own walk, so the two have to agree.
func FuzzCheckLink(f *testing.F) {
	for _, seed := range [][2]string{
		{"lib/a.so", "b.so"},
		{"sub/up", "../top.txt"},
		{"lib/a.so", "../../outside"},
		{"a/b/link", "../../../x"},
		{"link", "/etc/passwd"},
	} {
		f.Add(seed[0], seed[1])
	}

	root := filepath.FromSlash("/dest")
	sep := string(filepath.Separator)
	normalize := strings.NewReplacer("/", sep, `\`, sep)

	f.Fuzz(func(t *testing.T, name, linkTarget string) {
		link, err := target(root, name)
		if err != nil {
			return
		}
		if err := checkLink(root, link, linkTarget); err != nil {
			return
		}
		resolved := filepath.Join(filepath.Dir(link), normalize.Replace(linkTarget))
		rel, err := filepath.Rel(root, resolved)
		if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+sep) {
			t.Fatalf("checkLink allowed %q -> %q, which resolves to %q", name, linkTarget, resolved)
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
