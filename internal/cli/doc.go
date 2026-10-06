// Package cli is the command-line front end: it parses a command line, drives
// the dispatcher and renders the result.
//
// All output goes through the io.Writer pair passed to Run, so the front end is
// testable without a process. The exit-code contract lives in
// internal/exitcode, and Main is the only place that applies it.
//
// The command tree in command.go is the complete vocabulary: a verb that is not
// in the tree is absent, never present-and-unimplemented.
package cli
