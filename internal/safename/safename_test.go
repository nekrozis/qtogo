package safename

import (
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	tests := []struct {
		name string
		want Problem
	}{
		{"qtbase-Windows-X86_64.7z", OK},
		{"x", OK},
		{"", Empty},
		{".", DotSegment},
		{"..", DotSegment},
		{`a\b`, Separator},
		{"a/b", Separator},
		{"a\x01b", Control},
		{"a:b", IllegalChar},
		{"a?b", IllegalChar},
		{"x.", TrailingDotOrSpace},
		{"x ", TrailingDotOrSpace},
		{"CON", DeviceName},
		{"con.7z", DeviceName},
		{"NUL", DeviceName},
		{"com1", DeviceName},
		{"LPT9.tar.gz", DeviceName},
		{"com0", OK},    // not a device
		{"console", OK}, // not the device CON
	}
	for _, tt := range tests {
		if got := Check(tt.name); got != tt.want {
			t.Errorf("Check(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func FuzzCheck(f *testing.F) {
	f.Add("qtbase-Windows-X86_64.7z")
	f.Add("..")
	f.Add(`a\b`)
	f.Add("CON")
	f.Fuzz(func(t *testing.T, name string) {
		p := Check(name)
		if p > DeviceName {
			t.Fatalf("Check(%q) = %d, outside the known problems", name, p)
		}
		if p != OK {
			return
		}
		// Whatever else is accepted, these can never be: a separator would let the
		// name leave its directory, and a control character is never ordinary.
		if name == "" || name == "." || name == ".." {
			t.Fatalf("Check(%q) accepted a name it must refuse", name)
		}
		if strings.ContainsAny(name, `/\`) {
			t.Fatalf("Check(%q) accepted a name holding a path separator", name)
		}
		for _, r := range name {
			if r < ' ' {
				t.Fatalf("Check(%q) accepted a name holding a control character", name)
			}
		}
	})
}
