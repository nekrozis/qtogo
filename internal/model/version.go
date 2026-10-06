package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Version is a Qt version.
//
// Raw is the string the version was parsed from and is never recomputed: that
// string, not a re-rendered one, is what builds a URL or names a directory. A
// package version carries a build stamp, and a directory may say only "5.9", so
// neither can be recovered from the numbers alone.
//
// The zero Version means "unspecified" and is not a valid release.
type Version struct {
	Raw                 string
	Major, Minor, Patch int
	Suffix              string
}

// ParseVersion parses "6.8.0", "5.15.2-0-202011130601" or a shorter form such as
// "5.9". One to three numbers are required and the rest is kept verbatim in
// Suffix.
func ParseVersion(raw string) (Version, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return Version{}, fmt.Errorf("empty version")
	}

	numbers, suffix := splitNumbers(trimmed)
	if numbers == "" {
		return Version{}, fmt.Errorf("version %q does not start with a number", raw)
	}

	parts := strings.Split(numbers, ".")
	if len(parts) > 3 {
		return Version{}, fmt.Errorf("version %q has %d numeric components, want at most 3", raw, len(parts))
	}

	v := Version{Raw: trimmed, Suffix: suffix}
	fields := []*int{&v.Major, &v.Minor, &v.Patch}
	for i, part := range parts {
		n, err := parseNumber(part)
		if err != nil {
			return Version{}, fmt.Errorf("version %q: %w", raw, err)
		}
		*fields[i] = n
	}
	return v, nil
}

// Compare orders v against o: the numbers first, then the suffix, where a
// missing suffix is newer than any suffix. That puts a release after its own
// previews ("6.9.0-beta1" < "6.9.0") and orders Qt's zero-padded build stamps
// correctly.
//
// Compare is the ordering API; "==" is not.
func (v Version) Compare(o Version) int {
	if c := compareInt(v.Major, o.Major); c != 0 {
		return c
	}
	if c := compareInt(v.Minor, o.Minor); c != 0 {
		return c
	}
	if c := compareInt(v.Patch, o.Patch); c != 0 {
		return c
	}

	switch {
	case v.Suffix == o.Suffix:
		return 0
	case v.Suffix == "":
		return 1
	case o.Suffix == "":
		return -1
	default:
		return strings.Compare(v.Suffix, o.Suffix)
	}
}

// IsZero reports whether the version was never set.
func (v Version) IsZero() bool { return v.Raw == "" }

// String returns the string the version was parsed from.
func (v Version) String() string { return v.Raw }

// MarshalJSON renders the version as its raw string, so a document carries the
// version the repository used rather than a re-rendered object.
func (v Version) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.Raw)
}

// splitNumbers cuts the leading dotted numbers from whatever follows.
func splitNumbers(s string) (numbers, suffix string) {
	i := 0
	for i < len(s) && (isDigit(s[i]) || s[i] == '.') {
		i++
	}
	return s[:i], s[i:]
}

// parseNumber parses one numeric component, rejecting the empty component that
// "6." and "6..8" produce. A component is never negative: the scanner that
// splits the version stops at any character a number cannot contain. The only
// way the conversion itself fails is a value too large for an int, so the error
// keeps strconv's wording rather than claiming the text is not a number.
func parseNumber(part string) (int, error) {
	if part == "" {
		return 0, fmt.Errorf("empty numeric component")
	}
	n, err := strconv.Atoi(part)
	if err != nil {
		return 0, fmt.Errorf("numeric component %q: %w", part, err)
	}
	return n, nil
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
