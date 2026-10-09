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

// tree makes a directory holding a marker file, standing in for a finished tree.
func tree(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("tree"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPublishTreeMovesTheTreeIntoPlace(t *testing.T) {
	base := t.TempDir()
	src := tree(t, filepath.Join(base, ".staging", "5.15.2", "mingw81_64"))
	dest := filepath.Join(base, "5.15.2", "mingw81_64")

	result, err := PublishTree(src, dest, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != dest || result.Replaced {
		t.Errorf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dest, "marker")); err != nil {
		t.Errorf("the tree was not published: %v", err)
	}
}

// The base directory may already exist — a user points at their Qt directory — so
// only the tree itself must be new.
func TestPublishTreeAcceptsAnExistingBase(t *testing.T) {
	base := t.TempDir()
	src := tree(t, filepath.Join(base, ".staging", "6.8.0", "msvc2022_64"))

	if _, err := PublishTree(src, filepath.Join(base, "6.8.0", "msvc2022_64"), false); err != nil {
		t.Fatalf("publishing into an existing base directory = %v", err)
	}
}

func TestPublishTreeRefusesAnExistingTree(t *testing.T) {
	base := t.TempDir()
	dest := tree(t, filepath.Join(base, "5.15.2", "mingw81_64"))
	src := tree(t, filepath.Join(base, ".staging"))

	_, err := PublishTree(src, dest, false)
	assertFailure(t, err, exitcode.Filesystem, errs.CodeFilesystem)

	// The refusal leaves the existing tree where it was.
	if _, err := os.Stat(filepath.Join(dest, "marker")); err != nil {
		t.Errorf("the existing tree was disturbed: %v", err)
	}
}

func TestPublishTreeOverwritesWhenAsked(t *testing.T) {
	base := t.TempDir()
	dest := tree(t, filepath.Join(base, "5.15.2", "mingw81_64"))
	if err := os.WriteFile(filepath.Join(dest, "old"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	src := tree(t, filepath.Join(base, ".staging"))

	result, err := PublishTree(src, dest, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Replaced {
		t.Error("result does not say the tree was replaced")
	}
	if _, err := os.Stat(filepath.Join(dest, "old")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the old tree's contents survived the overwrite")
	}
	if _, err := os.Stat(filepath.Join(dest, "marker")); err != nil {
		t.Errorf("the new tree is not in place: %v", err)
	}
}

// A staging directory is created under a base directory, hidden and unique.
func TestNewStageIsHiddenUnderTheBase(t *testing.T) {
	base := t.TempDir()
	s, err := NewStage(filepath.Join(base, ".staging"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Discard() }()

	if filepath.Dir(s.Dir()) != base {
		t.Errorf("staging directory is under %q, want %q", filepath.Dir(s.Dir()), base)
	}
	if name := filepath.Base(s.Dir()); name[0] != '.' {
		t.Errorf("staging directory %q is not hidden", name)
	}
}

// Discard removes the staging directory and everything in it.
func TestDiscardRemovesTheStagingTree(t *testing.T) {
	base := t.TempDir()
	s, err := NewStage(filepath.Join(base, ".staging"))
	if err != nil {
		t.Fatal(err)
	}
	tree(t, filepath.Join(s.Dir(), "5.15.2", "mingw81_64"))

	if err := s.Discard(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Dir()); !errors.Is(err, os.ErrNotExist) {
		t.Error("the staging directory survived Discard")
	}
}

func TestNewStageCreatesTheParent(t *testing.T) {
	base := t.TempDir()
	s, err := NewStage(filepath.Join(base, "fresh", "staging"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Discard() }()

	if _, err := os.Stat(filepath.Join(base, "fresh")); err != nil {
		t.Errorf("the parent was not created: %v", err)
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
