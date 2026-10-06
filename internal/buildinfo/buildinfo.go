// Package buildinfo holds the identity of the running binary.
//
// Version, Commit and Date are stamped at release time with
// -ldflags "-X github.com/nekrozis/qtogo/internal/buildinfo.Version=...". A
// development build keeps the defaults, so String has two shapes.
package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

// ProgramName is the executable's name, as the user types it.
const ProgramName = "qtogo"

// DefaultVersion is what an unstamped build reports.
const DefaultVersion = "0.1.0"

var (
	// Version is the release version.
	Version = DefaultVersion
	// Commit is the short VCS revision.
	Commit = ""
	// Date is the build timestamp (RFC 3339, UTC).
	Date = ""
)

// String renders the one-line identity printed by the version command:
//
//	development: "qtogo 0.1.0"
//	stamped:     "qtogo 0.1.0 (commit abc123def456, built 2026-10-06T09:00:00Z, go1.27.1)"
//
// A development build omits the toolchain so its output does not change with
// the local Go version.
func String() string {
	line := ProgramName + " " + Version

	details := make([]string, 0, 3)
	if Commit != "" {
		details = append(details, "commit "+Commit)
	}
	if Date != "" {
		details = append(details, "built "+Date)
	}
	if len(details) == 0 {
		return line
	}
	details = append(details, runtime.Version())

	return fmt.Sprintf("%s (%s)", line, strings.Join(details, ", "))
}
