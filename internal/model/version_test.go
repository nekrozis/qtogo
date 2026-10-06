package model

import (
	"encoding/json"
	"strings"
	"testing"
)

// mustVersion parses a version the test knows is valid.
func mustVersion(t *testing.T, raw string) Version {
	t.Helper()

	v, err := ParseVersion(raw)
	if err != nil {
		t.Fatalf("ParseVersion(%q) = %v", raw, err)
	}
	return v
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name                string
		raw                 string
		major, minor, patch int
		suffix              string
	}{
		{"release", "6.8.0", 6, 8, 0, ""},
		{"patch release", "5.15.2", 5, 15, 2, ""},
		{"build stamp", "5.15.2-0-202011130601", 5, 15, 2, "-0-202011130601"},
		{"preview marker", "6.9.0-beta1", 6, 9, 0, "-beta1"},
		{"two components", "5.9", 5, 9, 0, ""},
		{"one component", "6", 6, 0, 0, ""},
		{"surrounding space", "  6.8.0  ", 6, 8, 0, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := mustVersion(t, tt.raw)

			if v.Major != tt.major || v.Minor != tt.minor || v.Patch != tt.patch {
				t.Errorf("numbers = %d.%d.%d, want %d.%d.%d",
					v.Major, v.Minor, v.Patch, tt.major, tt.minor, tt.patch)
			}
			if v.Suffix != tt.suffix {
				t.Errorf("Suffix = %q, want %q", v.Suffix, tt.suffix)
			}
			if got, want := v.String(), strings.TrimSpace(tt.raw); got != want {
				t.Errorf("String() = %q, want the input %q", got, want)
			}
		})
	}
}

func TestParseVersionRejects(t *testing.T) {
	rejected := []string{
		"",
		"   ",
		"abc",
		"6.",
		".6",
		"6..8",
		"6.8.0.1",
		"6.x.0",
		"-6.8.0",
		"99999999999999999999.0.0", // too large for an int
	}

	for _, raw := range rejected {
		if _, err := ParseVersion(raw); err == nil {
			t.Errorf("ParseVersion(%q) = nil error, want a failure", raw)
		}
	}
}

// TestVersionCompare walks one ascending list, so a regression in any single
// comparison shows up as a pair that is no longer ordered.
func TestVersionCompare(t *testing.T) {
	ascending := []string{
		"5.9.0",
		"5.15.2-0-202011130601",
		"5.15.2-0-202011130724",
		"5.15.2",
		"6.0.0",
		"6.8.0-beta1",
		"6.8.0",
		"6.8.1",
		"6.11.2",
	}

	versions := make([]Version, len(ascending))
	for i, raw := range ascending {
		versions[i] = mustVersion(t, raw)
	}

	for i, a := range versions {
		for j, b := range versions {
			if got, want := a.Compare(b), compareInt(i, j); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", ascending[i], ascending[j], got, want)
			}
		}
	}
}

func TestVersionCompareTreatsTheSameNumbersAsEqual(t *testing.T) {
	short := mustVersion(t, "5.9")
	long := mustVersion(t, "5.9.0")

	if short.Raw == long.Raw {
		t.Fatal("the two spellings must differ for this test to mean anything")
	}
	if got := short.Compare(long); got != 0 {
		t.Errorf("Compare(5.9, 5.9.0) = %d, want 0", got)
	}
}

func TestVersionIsZero(t *testing.T) {
	var zero Version
	if !zero.IsZero() {
		t.Error("the zero Version should report IsZero")
	}
	if mustVersion(t, "6.8.0").IsZero() {
		t.Error("a parsed version should not report IsZero")
	}
}

// TestVersionJSONIsTheRawString pins the document shape: a version appears as
// the string the repository used, not as an object of numbers.
func TestVersionJSONIsTheRawString(t *testing.T) {
	blob, err := json.Marshal(mustVersion(t, "5.15.2-0-202011130601"))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	const want = `"5.15.2-0-202011130601"`
	if got := string(blob); got != want {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}
