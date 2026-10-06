package model

import "testing"

// versionPtr parses a version a test knows is valid, for use as a bound.
func versionPtr(t *testing.T, raw string) *Version {
	t.Helper()

	v := mustVersion(t, raw)
	return &v
}

func TestVersionRangeContains(t *testing.T) {
	tests := []struct {
		name    string
		r       VersionRange
		version string
		want    bool
	}{
		{"lower bound is inclusive", VersionRange{Since: versionPtr(t, "5.14.0")}, "5.14.0", true},
		{"below the lower bound", VersionRange{Since: versionPtr(t, "5.14.0")}, "5.13.9", false},
		{"above the lower bound", VersionRange{Since: versionPtr(t, "5.14.0")}, "6.8.0", true},
		{"upper bound is exclusive", VersionRange{Until: versionPtr(t, "5.14.0")}, "5.14.0", false},
		{"below the upper bound", VersionRange{Until: versionPtr(t, "5.14.0")}, "5.13.9", true},
		{"inside both bounds", VersionRange{Since: versionPtr(t, "5.9.0"), Until: versionPtr(t, "6.0.0")}, "5.15.2", true},
		{"at the lower edge", VersionRange{Since: versionPtr(t, "5.9.0"), Until: versionPtr(t, "6.0.0")}, "5.9.0", true},
		{"at the upper edge", VersionRange{Since: versionPtr(t, "5.9.0"), Until: versionPtr(t, "6.0.0")}, "6.0.0", false},
		{"outside both bounds", VersionRange{Since: versionPtr(t, "5.9.0"), Until: versionPtr(t, "6.0.0")}, "5.8.9", false},
		{"no bounds at all", VersionRange{}, "0.0.1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.Contains(mustVersion(t, tt.version)); got != tt.want {
				t.Errorf("Contains(%s) = %t, want %t", tt.version, got, tt.want)
			}
		})
	}
}

// TestAdjacentRangesClaimEachVersionOnce is the property the half-open bound
// exists for: neighbouring ranges must not both match the version on their
// shared boundary.
func TestAdjacentRangesClaimEachVersionOnce(t *testing.T) {
	middle := versionPtr(t, "6.0.0")

	lower, err := NewVersionRange(versionPtr(t, "5.0.0"), middle)
	if err != nil {
		t.Fatalf("building the lower range: %v", err)
	}
	upper, err := NewVersionRange(middle, versionPtr(t, "7.0.0"))
	if err != nil {
		t.Fatalf("building the upper range: %v", err)
	}

	tests := []struct {
		version string
		claims  int
	}{
		{"4.9.0", 0},
		{"5.0.0", 1},
		{"5.9.0", 1},
		{"6.0.0", 1},
		{"6.8.0", 1},
		{"7.0.0", 0},
	}

	for _, tt := range tests {
		v := mustVersion(t, tt.version)

		claims := 0
		for _, r := range []VersionRange{lower, upper} {
			if r.Contains(v) {
				claims++
			}
		}

		if claims != tt.claims {
			t.Errorf("%s is claimed by %d ranges, want %d", tt.version, claims, tt.claims)
		}
	}
}

func TestNewVersionRangeValidatesBounds(t *testing.T) {
	accepted := []struct {
		name  string
		since *Version
		until *Version
	}{
		{"proper bounds", versionPtr(t, "5.0.0"), versionPtr(t, "6.0.0")},
		{"no bounds", nil, nil},
		{"only a lower bound", versionPtr(t, "5.0.0"), nil},
		{"only an upper bound", nil, versionPtr(t, "6.0.0")},
	}

	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewVersionRange(tt.since, tt.until); err != nil {
				t.Errorf("NewVersionRange = %v, want no error", err)
			}
		})
	}

	rejected := []struct {
		name  string
		since *Version
		until *Version
	}{
		{"equal bounds", versionPtr(t, "6.0.0"), versionPtr(t, "6.0.0")},
		{"inverted bounds", versionPtr(t, "6.0.0"), versionPtr(t, "5.0.0")},
	}

	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewVersionRange(tt.since, tt.until); err == nil {
				t.Error("NewVersionRange = nil error, want a failure")
			}
		})
	}
}
