// Package safename decides whether a string is an ordinary file name on every
// platform.
//
// The rule is deliberately the same on every host. A name Windows would
// reinterpret is suspect whatever the host is, and a rule that changed with the
// host would be one more thing to get wrong — the same archive is handled on all
// three. The extractor applies it to each segment of an archive entry and the
// downloader to the one file name it writes; each words the answer for its own
// reader.
package safename

import "strings"

// Problem is why a name is not an ordinary file name. OK means it is.
type Problem uint8

const (
	OK Problem = iota
	// Empty is "".
	Empty
	// DotSegment is "." or "..".
	DotSegment
	// Separator is a path separator of either kind, "/" or "\". A caller splits at
	// its own separators first; this catches the one it did not.
	Separator
	// Control is a control character (below the space).
	Control
	// IllegalChar is a character Windows forbids in a file name: <>:"|?*
	IllegalChar
	// TrailingDotOrSpace is a name ending in a dot or a space, which Windows trims.
	TrailingDotOrSpace
	// DeviceName is a name Windows reads as a device rather than a file, which
	// would send the bytes somewhere other than the destination.
	DeviceName
)

// Check reports the first reason name is not an ordinary file name, or OK.
func Check(name string) Problem {
	switch name {
	case "":
		return Empty
	case ".", "..":
		return DotSegment
	}
	if strings.ContainsAny(name, `/\`) {
		return Separator
	}
	for _, r := range name {
		if r < ' ' {
			return Control
		}
	}
	if strings.ContainsAny(name, `<>:"|?*`) {
		return IllegalChar
	}
	if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return TrailingDotOrSpace
	}
	if device(name) {
		return DeviceName
	}
	return OK
}

// device reports whether Windows would take the name for a device rather than a
// file. The check is on the stem, so the extension is irrelevant.
func device(name string) bool {
	stem := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		stem = name[:i]
	}
	switch strings.ToUpper(stem) {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	if len(stem) == 4 {
		head, tail := strings.ToUpper(stem[:3]), stem[3]
		if (head == "COM" || head == "LPT") && tail >= '1' && tail <= '9' {
			return true
		}
	}
	return false
}
