package errs

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/nekrozis/qtogo/internal/exitcode"
)

func TestNewCarriesCodeAndClassification(t *testing.T) {
	err := New(exitcode.NotFound, CodeVersionNotFound, PhaseResolve, "no version %q", "6.99.0")

	if got, want := err.Code(), CodeVersionNotFound; got != want {
		t.Errorf("Code() = %q, want %q", got, want)
	}
	if got, want := err.Phase(), PhaseResolve; got != want {
		t.Errorf("Phase() = %q, want %q", got, want)
	}
	if got, want := err.Message(), `no version "6.99.0"`; got != want {
		t.Errorf("Message() = %q, want %q", got, want)
	}
	if got, want := err.Error(), `resolve: no version "6.99.0"`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got, want := err.ExitCode(), exitcode.NotFound; got != want {
		t.Errorf("ExitCode() = %d, want %d", got, want)
	}
	if got, want := exitcode.Classify(err), exitcode.NotFound; got != want {
		t.Errorf("Classify = %d, want %d", got, want)
	}
}

func TestErrorOmitsEmptyPhase(t *testing.T) {
	err := New(exitcode.Internal, CodeUnclassified, "", "something went wrong")
	if got, want := err.Error(), "something went wrong"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestUsagefIsAUX(t *testing.T) {
	err := Usagef(CodeUnknownCommand, "unknown command %q", "lis-qt")

	if got, want := err.ExitCode(), exitcode.Usage; got != want {
		t.Errorf("ExitCode() = %d, want %d", got, want)
	}
	if got, want := err.Phase(), PhaseParse; got != want {
		t.Errorf("Phase() = %q, want %q", got, want)
	}
	if got, want := exitcode.Classify(err), exitcode.Usage; got != want {
		t.Errorf("Classify = %d, want %d", got, want)
	}
}

func TestWrapKeepsCauseReachable(t *testing.T) {
	cause := errors.New("connection reset")
	err := Wrap(exitcode.Network, "network.mirror_unavailable", PhaseDownload, cause, "mirror %s refused", "mirror.example")

	if !errors.Is(err, cause) {
		t.Fatal("errors.Is(err, cause) = false, want true")
	}
	if got, want := exitcode.Classify(err), exitcode.Network; got != want {
		t.Errorf("Classify = %d, want %d", got, want)
	}
	if got, want := err.Error(), "download: mirror mirror.example refused"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	// Wrapping the *Error* again must not lose the code either.
	if got, want := exitcode.Classify(errors.Join(errors.New("context"), err)), exitcode.Network; got != want {
		t.Errorf("Classify(joined) = %d, want %d", got, want)
	}
}

func TestWithSuggestionDoesNotMutateReceiver(t *testing.T) {
	base := New(exitcode.Usage, CodeUnknownCommand, PhaseParse, "unknown command %q", "lis-qt")

	hinted := base.WithSuggestion("run %q to list commands", "qtogo help")

	if got := base.Suggestion(); got != "" {
		t.Errorf("receiver suggestion = %q, want empty", got)
	}
	if got, want := hinted.Suggestion(), `run "qtogo help" to list commands`; got != want {
		t.Errorf("copy suggestion = %q, want %q", got, want)
	}
	// The copy must still be the same failure in every other respect.
	if got, want := hinted.Code(), base.Code(); got != want {
		t.Errorf("copy code = %q, want %q", got, want)
	}
	if got, want := SuggestionOf(hinted), hinted.Suggestion(); got != want {
		t.Errorf("SuggestionOf = %q, want %q", got, want)
	}
	if got := SuggestionOf(base); got != "" {
		t.Errorf("SuggestionOf(receiver) = %q, want empty", got)
	}
}

func TestDiagnosticProjection(t *testing.T) {
	err := New(exitcode.Integrity, CodeChecksumMismatch, PhaseVerify, "checksum mismatch").
		WithSuggestion("re-run to download again")

	got := err.Diagnostic()
	want := Diagnostic{
		Code:       CodeChecksumMismatch,
		Phase:      PhaseVerify,
		Message:    "checksum mismatch",
		Suggestion: "re-run to download again",
	}
	if got != want {
		t.Errorf("Diagnostic() = %+v, want %+v", got, want)
	}
}

func TestDiagnosticOfForeignErrorIsUnclassified(t *testing.T) {
	got := DiagnosticOf(errors.New("raw failure"))

	if got.Code != CodeUnclassified {
		t.Errorf("Code = %q, want %q", got.Code, CodeUnclassified)
	}
	if got.Phase != PhaseRun {
		t.Errorf("Phase = %q, want %q", got.Phase, PhaseRun)
	}
	if got.Message != "raw failure" {
		t.Errorf("Message = %q, want %q", got.Message, "raw failure")
	}
	if got.Suggestion != "" {
		t.Errorf("Suggestion = %q, want empty", got.Suggestion)
	}
}

func TestEnvelopeJSONShapeIsStable(t *testing.T) {
	env := EnvelopeOf(New(exitcode.Network, "network.timeout", PhaseDownload, "timeout"))

	blob, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"ok":false,"error":{"code":"network.timeout","phase":"download","message":"timeout"}}`
	if string(blob) != want {
		t.Errorf("envelope = %s, want %s", blob, want)
	}
}
