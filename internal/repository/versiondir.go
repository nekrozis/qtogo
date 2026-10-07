package repository

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nekrozis/qtogo/internal/model"
)

// VersionDirectory is a repository version directory name such as "qt6_6110",
// "qt6_6110_msvc2022_64" or "qt5_515_preview": the Qt version it stands for and
// the architecture extension it carries.
//
// Name is kept whole because it is the repository's own spelling and is what a
// URL is built from. Extension is everything after "qt<major>_<version>" and may
// itself contain underscores ("msvc2022_arm64_cross_compiled", "wasm_singlethread"),
// so it is taken whole rather than split further.
type VersionDirectory struct {
	Name      string
	Version   model.Version
	Extension string
}

// ParseVersionDirectory reads a version directory name into the version and the
// extension it encodes.
//
// The name is "<qt><major>_<version>[_<extension>]", where <version> is the
// version's digits without the dots: "qt6_6110" is 6.11.0 and "qt6_693" is 6.9.3.
// An extension containing "preview" marks a preview, which names only the major
// and minor ("qt5_515_preview" is 5.15.0), so it is read with those rules rather
// than the release ones.
//
// The version keeps the directory's own spelling in Version.Raw: the digits
// exactly as they name the directory ("6110"), with "-preview" in Version.Suffix
// for a preview. Raw is a source representation, not a path segment: turning a
// version back into a directory name or URL needs the repository layout, which is
// deliberately not this function's business.
//
// A name that does not fit one of these shapes is an error, never a guess: the
// zero Version is not returned as a successful result.
func ParseVersionDirectory(name string) (VersionDirectory, error) {
	parts := strings.SplitN(name, "_", 3)
	if len(parts) < 2 {
		return VersionDirectory{}, fmt.Errorf("%q is not a version directory", name)
	}
	if !isQtMajor(parts[0]) {
		return VersionDirectory{}, fmt.Errorf("%q does not start with qt<major>", name)
	}
	if digits := parts[1]; digits == "" || !allDigits(digits) {
		return VersionDirectory{}, fmt.Errorf("%q carries no numeric version", name)
	}

	extension := ""
	if len(parts) == 3 {
		extension = parts[2]
	}

	major, minor, patch, suffix, err := splitVersion(parts[1], strings.Contains(extension, "preview"))
	if err != nil {
		return VersionDirectory{}, fmt.Errorf("%q: %w", name, err)
	}

	return VersionDirectory{
		Name: name,
		Version: model.Version{
			Raw:    parts[1],
			Major:  major,
			Minor:  minor,
			Patch:  patch,
			Suffix: suffix,
		},
		Extension: extension,
	}, nil
}

// splitVersion turns a directory's version digits into version components.
//
// A release spells the version in the fewest digits that fit: two digits are
// major and minor ("59" is 5.9.0), three are major, minor and patch ("693" is
// 6.9.3), and four or more give minor two digits and patch the rest ("6110" is
// 6.11.0). A preview instead spells only major and minor, so "515" is 5.15.0 and
// not 5.1.5.
func splitVersion(digits string, preview bool) (major, minor, patch int, suffix string, err error) {
	if preview {
		if n := len(digits); n < 2 || n > 3 {
			return 0, 0, 0, "", fmt.Errorf("a preview version is written <major><minor>, but %q has %d digits", digits, n)
		}
		return number(digits[:1]), number(digits[1:]), 0, "-preview", nil
	}

	switch {
	case len(digits) == 2:
		return number(digits[:1]), number(digits[1:]), 0, "", nil
	case len(digits) == 3:
		return number(digits[:1]), number(digits[1:2]), number(digits[2:]), "", nil
	case len(digits) >= 4:
		// Only the patch is unbounded: every other component is one or two of the
		// digits the caller already checked, so it cannot overflow an int.
		patch, err = strconv.Atoi(digits[3:])
		if err != nil {
			return 0, 0, 0, "", fmt.Errorf("patch %q: %w", digits[3:], err)
		}
		return number(digits[:1]), number(digits[1:3]), patch, "", nil
	default:
		return 0, 0, 0, "", fmt.Errorf("version %q has too few digits", digits)
	}
}

// number reads a run of digits that the caller has already bounded to two, so it
// cannot overflow.
func number(digits string) int {
	n := 0
	for i := 0; i < len(digits); i++ {
		n = n*10 + int(digits[i]-'0')
	}
	return n
}

// isQtMajor reports whether a prefix is "qt" followed by the major version's
// digits, such as "qt6".
func isQtMajor(prefix string) bool {
	rest, ok := strings.CutPrefix(prefix, "qt")
	return ok && allDigits(rest)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
