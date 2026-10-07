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
		if err := checkSegment(segment); err != nil {
			return "", err
		}
	}

	full := filepath.Join(append([]string{root}, segments...)...)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("the name escapes the destination")
	}
	return full, nil
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

// checkSegment refuses a segment the destination must not be asked to hold.
//
// The Windows rules apply on every platform. A name Windows would reinterpret is
// suspect whatever the host is, and a rule that changed with the host would be one
// more thing to get wrong -- the same archive is extracted on all three.
func checkSegment(segment string) error {
	switch segment {
	case "":
		return errors.New("the name has an empty segment")
	case ".", "..":
		return fmt.Errorf("the name has a %q segment", segment)
	}
	for _, r := range segment {
		if r < ' ' {
			return fmt.Errorf("the segment %q holds a control character", segment)
		}
	}
	if strings.ContainsAny(segment, `<>:"|?*`) {
		return fmt.Errorf("the segment %q holds a character no filename may", segment)
	}
	if strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
		return fmt.Errorf("the segment %q ends in a dot or a space", segment)
	}
	if reservedName(segment) {
		return fmt.Errorf("the segment %q is a Windows device name", segment)
	}
	return nil
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
