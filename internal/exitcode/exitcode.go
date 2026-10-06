// Package exitcode defines the process exit-code contract.
//
// The numbers are a public interface: scripts and CI branch on them, so a
// meaning must never be redefined. Classify is the only place that turns a
// failure into one of them.
package exitcode

import "errors"

const (
	OK          = 0   // success
	Internal    = 1   // unclassified failure; not necessarily a bug
	Usage       = 2   // command-line usage error
	NotFound    = 3   // version or package does not exist
	Network     = 4   // network or mirror failure
	Integrity   = 5   // checksum mismatch, bad signature, unsafe archive entry
	Filesystem  = 6   // extraction, disk, or permission failure
	Relocate    = 7   // extracted, but the installation was not made usable
	Config      = 8   // invalid configuration
	Auth        = 9   // authentication or credentials
	Interrupted = 130 // cancelled by the user
)

// CodedError is an error that reports its own exit code.
type CodedError interface {
	error
	ExitCode() int
}

// Classify returns the exit code for err. nil is OK, a CodedError reports its
// own code, and anything else is Internal: an error outside the taxonomy must
// surface as unclassified rather than borrow a meaning it does not have.
func Classify(err error) int {
	if err == nil {
		return OK
	}
	var coded CodedError
	if errors.As(err, &coded) {
		if code := coded.ExitCode(); code != OK {
			return code
		}
	}
	return Internal
}
