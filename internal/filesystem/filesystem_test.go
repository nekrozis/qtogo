package filesystem

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

func assertFailure(t *testing.T, err error, wantExit int, wantCode string) {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("error = %v, want an *errs.Error", err)
	}
	if e.ExitCode() != wantExit || e.Code() != wantCode {
		t.Errorf("failure = exit %d, code %q; want exit %d, code %q", e.ExitCode(), e.Code(), wantExit, wantCode)
	}
}

// stage is the destination's directory, with a staging tree holding a marker file.
func staged(t *testing.T, dest string) *Stage {
	t.Helper()
	s, err := NewStage(dest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.Dir(), "marker"), []byte("tree"), 0o600); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestCommitMovesTheTreeIntoPlace(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "Qt")
	s := staged(t, dest)

	result, err := s.Commit(Publish{Dest: dest})
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != dest || result.Replaced {
		t.Errorf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dest, "marker")); err != nil {
		t.Errorf("the tree was not published: %v", err)
	}
	// The staging directory is gone: nothing left beside the destination.
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("base holds %d entries, want only the published tree", len(entries))
	}
}

// The destination appears only once the tree is complete: a failure before Commit
// leaves nothing behind after Discard.
func TestDiscardLeavesNoDestination(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "Qt")
	s := staged(t, dest)

	if err := s.Discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the destination exists after Discard")
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the staging directory survived Discard: %v", entries)
	}
}

func TestCommitRefusesAnExistingDestination(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "Qt")
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatal(err)
	}
	s := staged(t, dest)

	_, err := s.Commit(Publish{Dest: dest})
	assertFailure(t, err, exitcode.Filesystem, errs.CodeFilesystem)

	// The refusal leaves the existing destination where it was.
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("the existing destination was disturbed: %v", err)
	}
}

func TestCommitOverwritesWhenAsked(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "Qt")
	if err := os.MkdirAll(dest, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "old"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := staged(t, dest)

	result, err := s.Commit(Publish{Dest: dest, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replaced {
		t.Error("result does not say the destination was replaced")
	}
	if _, err := os.Stat(filepath.Join(dest, "old")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the old destination's contents survived the overwrite")
	}
	if _, err := os.Stat(filepath.Join(dest, "marker")); err != nil {
		t.Errorf("the new tree is not in place: %v", err)
	}
}

// A staging directory is created beside the destination, so the rename never
// crosses a volume boundary.
func TestStageIsBesideTheDestination(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "Qt")
	s := staged(t, dest)

	if got := filepath.Dir(s.Dir()); got != base {
		t.Errorf("staging directory is under %q, want %q", got, base)
	}
	if err := s.Discard(); err != nil {
		t.Fatal(err)
	}
}

// A destination whose parent does not exist has it created: an install into a
// fresh output directory is the ordinary case.
func TestNewStageCreatesTheParent(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "fresh", "Qt")

	s, err := NewStage(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Discard() }()

	if _, err := os.Stat(filepath.Join(base, "fresh")); err != nil {
		t.Errorf("the parent was not created: %v", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Error("the destination itself was created, but only the staging tree should be")
	}
}

// Discard after Commit is a no-op rather than an error: a failure path that runs
// both should not have to know which happened.
func TestDiscardAfterCommitIsQuiet(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "Qt")
	s := staged(t, dest)

	if _, err := s.Commit(Publish{Dest: dest}); err != nil {
		t.Fatal(err)
	}
	if err := s.Discard(); err != nil {
		t.Errorf("Discard after Commit = %v, want nil", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "marker")); err != nil {
		t.Errorf("Discard after Commit removed the published tree: %v", err)
	}
}

func TestWriteManifestPutsTheDocumentInTheTree(t *testing.T) {
	dest := t.TempDir()

	if err := WriteManifest(dest, []byte(`{"program":"qtogo"}`)); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dest, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"program":"qtogo"}` {
		t.Errorf("manifest = %q", body)
	}
}
