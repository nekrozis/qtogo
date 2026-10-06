package model

import "fmt"

// VersionRange selects versions with a half-open upper bound: Since is inclusive
// and Until is exclusive. A nil bound means unbounded on that side.
//
// Half-open bounds are what let neighbouring ranges meet without overlapping:
// [5.0, 6.0) and [6.0, 7.0) cover everything exactly once. It is also how the
// rules read — "5.14 and newer need no patch" is Until: 5.14.0, because it says
// something about everything below that release.
type VersionRange struct {
	Since *Version
	Until *Version
}

// NewVersionRange validates the bounds. A lower bound at or above the exclusive
// upper bound can never match anything, which is always a mistake in a rule
// rather than an intent.
func NewVersionRange(since, until *Version) (VersionRange, error) {
	if since != nil && until != nil && since.Compare(*until) >= 0 {
		return VersionRange{}, fmt.Errorf("empty version range: since %s is not below until %s", since, until)
	}
	return VersionRange{Since: since, Until: until}, nil
}

// Contains reports whether v falls in the range.
func (r VersionRange) Contains(v Version) bool {
	if r.Since != nil && v.Compare(*r.Since) < 0 {
		return false
	}
	if r.Until != nil && v.Compare(*r.Until) >= 0 {
		return false
	}
	return true
}
