package errs

import "errors"

// Diagnostic is the machine-readable form of a failure. The field names are a
// contract: automation reads them.
type Diagnostic struct {
	Code       string `json:"code"`
	Phase      string `json:"phase"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// Envelope is the failure document written in JSON mode.
type Envelope struct {
	OK    bool       `json:"ok"`
	Error Diagnostic `json:"error"`
}

// Diagnostic projects this error.
func (e *Error) Diagnostic() Diagnostic {
	return Diagnostic{
		Code:       e.code,
		Phase:      e.phase,
		Message:    e.message,
		Suggestion: e.suggestion,
	}
}

// DiagnosticOf projects any error, so the JSON shape stays total: an error that
// escaped the taxonomy still produces a Diagnostic.
func DiagnosticOf(err error) Diagnostic {
	var e *Error
	if errors.As(err, &e) {
		return e.Diagnostic()
	}
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return Diagnostic{Code: CodeUnclassified, Phase: PhaseRun, Message: msg}
}

// EnvelopeOf wraps any error in the failure document.
func EnvelopeOf(err error) Envelope {
	return Envelope{OK: false, Error: DiagnosticOf(err)}
}

// SuggestionOf returns the suggestion carried by an *Error, or "" for anything
// else.
func SuggestionOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.suggestion
	}
	return ""
}
