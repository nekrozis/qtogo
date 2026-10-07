package extract

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// target turns an archive name into the path it must be written to inside root.
//
// An archive name is not a path: it is a string the archive chose, and the format
// lets it separate segments with '/' or '\'. Neither is trusted. Every segment has
// to be an ordinary name, and the result is checked to still be under root rather
// than left to the cleaning that Join happens to do.
func target(root, name string) (string, error) {
	if name == "" {
		return "", errors.New("the name is empty")
	}
	if strings.ContainsRune(name, 0) {
		return "", errors.New("the name holds a NUL")
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return "", errors.New("the name is absolute")
	}

	segments := splitName(name)
	for _, segment := range segments {
		if why := badSegment(segment); why != "" {
			return "", fmt.Errorf("the name has %s", why)
		}
	}

	full := filepath.Join(append([]string{root}, segments...)...)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("the name escapes the destination")
	}
	return full, nil
}

// checkLink refuses a symlink target that is not a plain relative path.
//
// A target answers to the rule a name answers to: no absolute prefix, no drive, and
// no segment that is empty, "." or "..". Resolving the target and refusing it only
// where it lands -- the weaker rule -- accepts two spellings for one location
// ("foo/../bar" and "bar"), which is how an entry collision is made, and it leaves
// the rule as "wherever it ends up is safe" rather than "an entry is a legal
// relative path". Refusing costs nothing here: in every Qt archive measured for the
// record, each link target is a plain relative path.
//
// The target is never resolved, by this check or by the write that follows it: it
// is stored in the link and left alone.
func checkLink(target string) error {
	if target == "" {
		return errors.New("its target is empty")
	}
	if strings.ContainsRune(target, 0) {
		return errors.New("its target holds a NUL")
	}
	if strings.HasPrefix(target, "/") || strings.HasPrefix(target, `\`) || drivePrefix(target) {
		return errors.New("its target is absolute")
	}
	for _, segment := range splitName(target) {
		if why := badSegment(segment); why != "" {
			return fmt.Errorf("its target has %s", why)
		}
	}
	return nil
}

// drivePrefix reports whether a path starts with a Windows drive, which would make
// it absolute there.
func drivePrefix(path string) bool {
	if len(path) < 2 {
		return false
	}
	letter := path[0] >= 'a' && path[0] <= 'z' || path[0] >= 'A' && path[0] <= 'Z'
	return letter && path[1] == ':'
}

// splitName cuts a name at either separator. Empty segments are kept, so a doubled
// separator shows up as one instead of being quietly collapsed.
func splitName(name string) []string {
	segments := make([]string, 0, 4)
	start := 0
	for i := 0; i < len(name); i++ {
		if name[i] == '/' || name[i] == '\\' {
			segments = append(segments, name[start:i])
			start = i + 1
		}
	}
	return append(segments, name[start:])
}

// badSegment says what is wrong with one segment of a path, or returns "" when the
// segment is an ordinary name. A name and a symlink target answer to the same rule.
//
// The Windows rules apply on every platform. A name Windows would reinterpret is
// suspect whatever the host is, and a rule that changed with the host would be one
// more thing to get wrong -- the same archive is extracted on all three.
func badSegment(segment string) string {
	switch segment {
	case "":
		return "an empty segment"
	case ".", "..":
		return fmt.Sprintf("a %q segment", segment)
	}
	for _, r := range segment {
		if r < ' ' {
			return fmt.Sprintf("%q, which holds a control character", segment)
		}
	}
	if strings.ContainsAny(segment, `<>:"|?*`) {
		return fmt.Sprintf("%q, which holds a character no filename may", segment)
	}
	if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
		return fmt.Sprintf("%q, which ends in a dot or a space", segment)
	}
	if reservedName(segment) {
		return fmt.Sprintf("%q, which Windows reads as a device", segment)
	}
	return ""
}

// reservedName reports whether Windows would take the segment for a device rather
// than a file, which would send the bytes somewhere other than the destination.
func reservedName(segment string) bool {
	stem := segment
	if i := strings.IndexByte(segment, '.'); i >= 0 {
		stem = segment[:i]
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
