// Package errs holds qtogo's error type and its machine-readable codes.
//
// Every failure qtogo raises is an *Error: a stable machine code and phase for
// automation, plus a message and an optional suggestion for people. The exit
// code comes from internal/exitcode.
package errs

import (
	"fmt"

	"github.com/nekrozis/qtogo/internal/exitcode"
)

// Phases name the stage a failure happened in.
const (
	PhaseParse    = "parse"
	PhaseResolve  = "resolve"
	PhasePlan     = "plan"
	PhaseDownload = "download"
	PhaseVerify   = "verify"
	PhaseExtract  = "extract"
	PhasePublish  = "publish"
	PhaseRelocate = "relocate"
	PhaseConfig   = "config"
	PhaseRun      = "run"
)

// Machine codes, namespaced by phase. They are part of the JSON contract.
const (
	CodeUnknownCommand  = "usage.unknown_command"
	CodeUnknownOption   = "usage.unknown_option"
	CodeMissingArgument = "usage.missing_argument"
	CodeUnexpectedValue = "usage.unexpected_value"
	CodeUnexpectedArg   = "usage.unexpected_argument"
	CodeNotEnabled      = "usage.not_enabled"

	CodeVersionNotFound = "resolve.version_not_found"
	CodePackageNotFound = "resolve.package_not_found"

	// Transport: the scheme a source may use, the request itself, and the checks
	// that a download is what it claims to be (ADR-007). The scheme refusals are
	// usage errors, but their offending value is configuration rather than command
	// syntax, which is why the phase they carry is config.
	CodeInsecureScheme    = "usage.insecure_scheme"
	CodeUnsupportedScheme = "usage.unsupported_scheme"

	CodeRequestFailed    = "network.request_failed"
	CodeHTTPStatus       = "network.status"
	CodeHTTPNotFound     = "network.not_found"
	CodeRedirectLimit    = "network.redirect_limit"
	CodeSourcesExhausted = "network.sources_exhausted"
	CodeDocumentTooLarge = "network.document_too_large"

	CodeSchemeDowngrade   = "download.scheme_downgrade"
	CodeFilesystem        = "download.filesystem"
	CodeChecksumMissing   = "verify.checksum_missing"
	CodeChecksumMalformed = "verify.checksum_malformed"
	CodeChecksumWeak      = "verify.checksum_weak"
	CodeDigestRedirected  = "verify.digest_source_redirected"

	CodeChecksumMismatch = "verify.checksum_mismatch"
	CodeUnsafeArchive    = "archive.unsafe_entry"

	CodeDiskFull      = "extract.disk_full"
	CodeExtractFailed = "extract.failed"
	CodeExtractLimit  = "extract.limit_exceeded"
	CodeExtractDecode = "extract.decode_failed"

	CodeRelocateUnsupported = "relocate.unsupported_target"
	CodeRelocateFailed      = "relocate.failed"

	CodeConfigInvalid = "config.invalid"

	CodeInterrupted = "run.interrupted"

	// CodeUnclassified marks a failure that escaped the taxonomy.
	CodeUnclassified = "internal.unclassified"
)

// Error is the only error type qtogo's own code returns. The fields are
// unexported so instances come from New or Wrap and are refined only by
// WithSuggestion.
type Error struct {
	exit       int
	code       string
	phase      string
	message    string
	suggestion string
	err        error
}

// New builds an *Error. exit must be one of the internal/exitcode constants.
func New(exit int, code, phase, format string, args ...any) *Error {
	return &Error{
		exit:    exit,
		code:    code,
		phase:   phase,
		message: fmt.Sprintf(format, args...),
	}
}

// Wrap builds an *Error around cause, which stays reachable through errors.Is
// and errors.As.
func Wrap(exit int, code, phase string, cause error, format string, args ...any) *Error {
	e := New(exit, code, phase, format, args...)
	e.err = cause
	return e
}

// Usagef reports a command-line usage error.
func Usagef(code, format string, args ...any) *Error {
	return New(exitcode.Usage, code, PhaseParse, format, args...)
}

// WithSuggestion returns a copy carrying a suggestion, leaving the receiver
// untouched.
func (e *Error) WithSuggestion(format string, args ...any) *Error {
	c := *e
	c.suggestion = fmt.Sprintf(format, args...)
	return &c
}

// Error renders "phase: message", or the message alone when there is no phase.
func (e *Error) Error() string {
	if e.phase == "" {
		return e.message
	}
	return e.phase + ": " + e.message
}

// Unwrap exposes the wrapped cause.
func (e *Error) Unwrap() error { return e.err }

// ExitCode reports the process exit code for this failure.
func (e *Error) ExitCode() int { return e.exit }

// Code returns the stable machine code.
func (e *Error) Code() string { return e.code }

// Phase returns the pipeline stage.
func (e *Error) Phase() string { return e.phase }

// Message returns the human-readable description.
func (e *Error) Message() string { return e.message }

// Suggestion returns the optional next step, or "".
func (e *Error) Suggestion() string { return e.suggestion }
