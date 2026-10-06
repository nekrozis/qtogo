package model

import (
	"encoding/json"
	"testing"
)

func TestParseHost(t *testing.T) {
	accepted := []string{"windows", "WINDOWS", " windows ", "Mac", "all_os", "linux"}
	for _, raw := range accepted {
		if _, err := ParseHost(raw); err != nil {
			t.Errorf("ParseHost(%q) = %v, want no error", raw, err)
		}
	}

	rejected := []string{"", "   ", "osx", "win", "all"}
	for _, raw := range rejected {
		if _, err := ParseHost(raw); err == nil {
			t.Errorf("ParseHost(%q) = nil error, want a failure", raw)
		}
	}
}

func TestParseKind(t *testing.T) {
	accepted := []string{"desktop", "DESKTOP", " desktop ", "wasm", "winrt", "ios"}
	for _, raw := range accepted {
		if _, err := ParseKind(raw); err != nil {
			t.Errorf("ParseKind(%q) = %v, want no error", raw, err)
		}
	}

	rejected := []string{"", "   ", "server", "mobile", "desk"}
	for _, raw := range rejected {
		if _, err := ParseKind(raw); err == nil {
			t.Errorf("ParseKind(%q) = nil error, want a failure", raw)
		}
	}
}

// TestHostsAndKindsRoundTrip keeps the advertised lists and the parsers from
// drifting apart: a value one lists has to be a value the other accepts.
func TestHostsAndKindsRoundTrip(t *testing.T) {
	seenHosts := make(map[Host]bool)
	for _, h := range Hosts() {
		parsed, err := ParseHost(string(h))
		if err != nil || parsed != h {
			t.Errorf("Hosts() lists %q, but ParseHost gives (%q, %v)", h, parsed, err)
		}
		if seenHosts[h] {
			t.Errorf("Hosts() lists %q twice", h)
		}
		seenHosts[h] = true
	}

	seenKinds := make(map[Kind]bool)
	for _, k := range Kinds() {
		parsed, err := ParseKind(string(k))
		if err != nil || parsed != k {
			t.Errorf("Kinds() lists %q, but ParseKind gives (%q, %v)", k, parsed, err)
		}
		if seenKinds[k] {
			t.Errorf("Kinds() lists %q twice", k)
		}
		seenKinds[k] = true
	}
}

func TestTargetValidate(t *testing.T) {
	complete := Target{
		Host:    HostWindows,
		Kind:    KindDesktop,
		Version: mustVersion(t, "6.8.0"),
		Arch:    "win64_msvc2022_64",
	}

	if err := complete.Validate(); err != nil {
		t.Fatalf("a complete target was rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*Target)
	}{
		{"no host", func(tg *Target) { tg.Host = "" }},
		{"unknown host", func(tg *Target) { tg.Host = "osx" }},
		{"no kind", func(tg *Target) { tg.Kind = "" }},
		{"unknown kind", func(tg *Target) { tg.Kind = "server" }},
		{"no version", func(tg *Target) { tg.Version = Version{} }},
		{"blank architecture", func(tg *Target) { tg.Arch = "   " }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := complete
			tt.mutate(&target)

			if err := target.Validate(); err == nil {
				t.Error("Validate() = nil error, want a failure")
			}
		})
	}
}

func TestTargetString(t *testing.T) {
	base := Target{
		Host:    HostLinux,
		Kind:    KindDesktop,
		Version: mustVersion(t, "5.15.2"),
		Arch:    "gcc_64",
	}

	if got, want := base.String(), "linux desktop 5.15.2 gcc_64"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}

	withExtension := base
	withExtension.Extension = "wasm"
	if got, want := withExtension.String(), "linux desktop 5.15.2 gcc_64 wasm"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestTargetJSON pins the frozen field names and the version-as-string shape.
func TestTargetJSON(t *testing.T) {
	target := Target{
		Host:    HostWindows,
		Kind:    KindDesktop,
		Version: mustVersion(t, "6.8.0"),
		Arch:    "win64_msvc2022_64",
	}

	blob, err := json.Marshal(target)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	const want = `{"host":"windows","kind":"desktop","version":"6.8.0","arch":"win64_msvc2022_64"}`
	if got := string(blob); got != want {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}
