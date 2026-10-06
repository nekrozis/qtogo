package model

import (
	"fmt"
	"strings"
)

// SpecKind is the shape of a version request.
type SpecKind uint8

const (
	SpecMajor  SpecKind = iota + 1 // "6"
	SpecMinor                      // "6.8"
	SpecPatch                      // "6.8.0"
	SpecLatest                     // "latest"
)

// LatestSpec is the request for the newest available version.
const LatestSpec = "latest"

// VersionSpec is what a caller asked for, which is not the same thing as a
// Version the repository offers. Keeping them apart is what lets "no such
// version" be reported instead of silently selecting something else.
//
// The zero VersionSpec matches nothing.
type VersionSpec struct {
	Raw  string
	Kind SpecKind

	numbers []int // the requested numbers; empty for LatestSpec
}

// ParseVersionSpec parses a request. The grammar is deliberately strict: a
// leading "v" and "LATEST" are rejected, because Qt's metadata writes neither
// and accepting spellings the upstream protocol does not use invents syntax that
// would have to be kept forever.
func ParseVersionSpec(raw string) (VersionSpec, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return VersionSpec{}, fmt.Errorf("empty version spec")
	}
	if trimmed == LatestSpec {
		return VersionSpec{Raw: trimmed, Kind: SpecLatest}, nil
	}

	numbers, suffix := splitNumbers(trimmed)
	if numbers == "" {
		return VersionSpec{}, fmt.Errorf("version spec %q does not start with a number", raw)
	}
	if suffix != "" {
		return VersionSpec{}, fmt.Errorf("version spec %q must be numbers or %q", raw, LatestSpec)
	}

	parts := strings.Split(numbers, ".")
	if len(parts) > 3 {
		return VersionSpec{}, fmt.Errorf("version spec %q has %d numeric components, want at most 3", raw, len(parts))
	}

	parsed := make([]int, 0, len(parts))
	for _, part := range parts {
		n, err := parseNumber(part)
		if err != nil {
			return VersionSpec{}, fmt.Errorf("version spec %q: %w", raw, err)
		}
		parsed = append(parsed, n)
	}

	kinds := [...]SpecKind{SpecMajor, SpecMinor, SpecPatch}
	return VersionSpec{Raw: trimmed, Kind: kinds[len(parsed)-1], numbers: parsed}, nil
}

// Matches reports whether v satisfies the request. Only the numbers are
// compared, so a suffix on v — a preview marker or a build stamp — does not
// disqualify it: "6.8" accepts "6.8.0-0-202011130601".
func (s VersionSpec) Matches(v Version) bool {
	if s.Kind == SpecLatest {
		return true
	}
	if len(s.numbers) == 0 {
		return false
	}

	have := []int{v.Major, v.Minor, v.Patch}
	for i, want := range s.numbers {
		if have[i] != want {
			return false
		}
	}
	return true
}

// String returns the string the spec was parsed from.
func (s VersionSpec) String() string { return s.Raw }
