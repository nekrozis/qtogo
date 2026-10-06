package model

import "testing"

func TestParseVersionSpec(t *testing.T) {
	tests := []struct {
		raw  string
		kind SpecKind
	}{
		{"6", SpecMajor},
		{"6.8", SpecMinor},
		{"6.8.0", SpecPatch},
		{"latest", SpecLatest},
		{" 6.8 ", SpecMinor},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			spec, err := ParseVersionSpec(tt.raw)
			if err != nil {
				t.Fatalf("ParseVersionSpec(%q) = %v, want no error", tt.raw, err)
			}
			if spec.Kind != tt.kind {
				t.Errorf("Kind = %d, want %d", spec.Kind, tt.kind)
			}
			if got := spec.String(); got == "" {
				t.Error("String() is empty")
			}
		})
	}
}

func TestParseVersionSpecRejects(t *testing.T) {
	rejected := []string{
		"",
		"   ",
		"v6.8",
		"LATEST",
		"Latest",
		"6.8.0.1",
		"6..8",
		"6.8.0-beta1",
		"abc",
		"6.x",
		"99999999999999999999", // too large for an int
	}

	for _, raw := range rejected {
		if _, err := ParseVersionSpec(raw); err == nil {
			t.Errorf("ParseVersionSpec(%q) = nil error, want a failure", raw)
		}
	}
}

func TestVersionSpecMatches(t *testing.T) {
	tests := []struct {
		spec    string
		version string
		want    bool
	}{
		{"6", "6.8.0", true},
		{"6", "6.0.0", true},
		{"6", "5.15.2", false},
		{"6", "7.0.0", false},
		{"6.8", "6.8.0", true},
		{"6.8", "6.8.5", true},
		{"6.8", "6.9.0", false},
		{"6.8", "6.7.9", false},
		{"6.8.0", "6.8.0", true},
		{"6.8.0", "6.8.0-0-202011130601", true},
		{"6.8.0", "6.8.1", false},
		{"latest", "0.1.0", true},
		{"latest", "6.8.0-beta1", true},
	}

	for _, tt := range tests {
		t.Run(tt.spec+" vs "+tt.version, func(t *testing.T) {
			spec, err := ParseVersionSpec(tt.spec)
			if err != nil {
				t.Fatalf("ParseVersionSpec(%q) = %v", tt.spec, err)
			}
			if got := spec.Matches(mustVersion(t, tt.version)); got != tt.want {
				t.Errorf("%q.Matches(%q) = %t, want %t", tt.spec, tt.version, got, tt.want)
			}
		})
	}
}

func TestZeroVersionSpecMatchesNothing(t *testing.T) {
	var spec VersionSpec
	if spec.Matches(mustVersion(t, "6.8.0")) {
		t.Error("the zero VersionSpec should match nothing")
	}
}
