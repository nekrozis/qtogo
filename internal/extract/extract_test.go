package extract

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/sevenzip"
)

// The fixtures are the small archives internal/sevenzip's tests use too;
// testdata/README.md records how they were built.
func fixturePath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
}

func openFixture(t *testing.T, name string) *sevenzip.Archive {
	t.Helper()
	a, err := sevenzip.Open(fixturePath("archive", name), 8<<20)
	if err != nil {
		t.Fatalf("Open(%s) = %v", name, err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return a
}

// roomy is a limit nothing in the fixtures comes near.
func roomy() Limits {
	return Limits{MaxEntries: 1000, MaxBytes: 64 << 20, MaxEntry: 32 << 20, MaxBlock: 32 << 20}
}

// dest is the empty directory the tests own, which is what All insists on.
func dest(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// files reads back what an extraction left: every file under root, by its
// slash-separated path.
func files(t *testing.T, root string) map[string]string {
	t.Helper()
	found := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return found
}

func assertFailure(t *testing.T, err error, wantExit int, wantCode string) {
	t.Helper()
	var e *errs.Error
	if !errors.As(err, &e) {
		t.Fatalf("error = %v, want an *errs.Error", err)
	}
	if e.ExitCode() != wantExit {
		t.Errorf("exit code = %d, want %d", e.ExitCode(), wantExit)
	}
	if e.Code() != wantCode {
		t.Errorf("code = %q, want %q", e.Code(), wantCode)
	}
	if e.Phase() != errs.PhaseExtract {
		t.Errorf("phase = %q, want %q", e.Phase(), errs.PhaseExtract)
	}
}

func TestAllWritesWhatTheArchiveHolds(t *testing.T) {
	tests := map[string]map[string]string{
		"plain.7z": {
			"docs/readme.md":  "readme\n",
			"docs/reading.md": "reading\n",
		},
		"solid.7z": {
			"a.txt": "alpha\n",
			"b.txt": "beta\n",
			"c.txt": "gamma\n",
		},
	}

	for fixture, want := range tests {
		t.Run(fixture, func(t *testing.T) {
			a := openFixture(t, fixture)
			dir := dest(t)

			report, err := All(context.Background(), a, dir, roomy())
			if err != nil {
				t.Fatalf("All = %v", err)
			}

			var total uint64
			for _, contents := range want {
				total += uint64(len(contents))
			}
			if report.Files != len(want) {
				t.Errorf("Files = %d, want %d", report.Files, len(want))
			}
			if report.Bytes != total {
				t.Errorf("Bytes = %d, want %d", report.Bytes, total)
			}

			got := files(t, dir)
			for name, contents := range want {
				if got[name] != contents {
					t.Errorf("%s: contents %q, want %q", name, got[name], contents)
				}
				delete(got, name)
			}
			for name := range got {
				t.Errorf("unexpected file %q", name)
			}
		})
	}
}

// Directories and empty files arrive as entries too, and each has to come out as
// what it is.
func TestAllWritesDirectoriesAndEmptyFiles(t *testing.T) {
	a := openFixture(t, "tree.7z")
	dir := dest(t)

	report, err := All(context.Background(), a, dir, roomy())
	if err != nil {
		t.Fatalf("All = %v", err)
	}
	if report.Dirs != 1 {
		t.Errorf("Dirs = %d, want 1", report.Dirs)
	}
	if report.Files != 2 {
		t.Errorf("Files = %d, want 2", report.Files)
	}

	if info, err := os.Stat(filepath.Join(dir, "dir")); err != nil || !info.IsDir() {
		t.Errorf("dir: err=%v, want a directory", err)
	}
	if body, err := os.ReadFile(filepath.Join(dir, "empty.txt")); err != nil || len(body) != 0 {
		t.Errorf("empty.txt: err=%v, %d bytes, want an empty file", err, len(body))
	}
	if _, err := os.Stat(filepath.Join(dir, "dir", "file.txt")); err != nil {
		t.Errorf("dir/file.txt: %v", err)
	}
}

// An entry whose name leaves the destination is refused before anything is
// written, so a tree that could have been damaged is left as it was found.
func TestAllRefusesAnUnsafeEntryBeforeWriting(t *testing.T) {
	a := openFixture(t, "backslash.7z")
	dir := dest(t)

	_, err := All(context.Background(), a, dir, roomy())
	assertFailure(t, err, exitcode.Integrity, errs.CodeUnsafeArchive)

	if left := files(t, dir); len(left) != 0 {
		t.Errorf("the destination holds %v, want nothing", left)
	}
}

func TestAllRefusesAnArchiveThatOutgrowsItsLimits(t *testing.T) {
	tests := map[string]Limits{
		"entries": {MaxEntries: 1, MaxBytes: 1 << 20, MaxEntry: 1 << 20, MaxBlock: 1 << 20},
		"bytes":   {MaxEntries: 100, MaxBytes: 8, MaxEntry: 1 << 20, MaxBlock: 1 << 20},
		"entry":   {MaxEntries: 100, MaxBytes: 1 << 20, MaxEntry: 4, MaxBlock: 1 << 20},
		"block":   {MaxEntries: 100, MaxBytes: 1 << 20, MaxEntry: 1 << 20, MaxBlock: 1},
	}

	for name, limits := range tests {
		t.Run(name, func(t *testing.T) {
			a := openFixture(t, "plain.7z")
			dir := dest(t)

			_, err := All(context.Background(), a, dir, limits)
			assertFailure(t, err, exitcode.Integrity, errs.CodeExtractLimit)

			if left := files(t, dir); len(left) != 0 {
				t.Errorf("the destination holds %v, want nothing", left)
			}
		})
	}
}

func TestAllRefusesALimitNobodySet(t *testing.T) {
	a := openFixture(t, "plain.7z")

	_, err := All(context.Background(), a, dest(t), Limits{})
	assertFailure(t, err, exitcode.Internal, errs.CodeUnclassified)
}

func TestAllRefusesADestinationItCannotOwn(t *testing.T) {
	a := openFixture(t, "plain.7z")

	t.Run("missing", func(t *testing.T) {
		_, err := All(context.Background(), a, filepath.Join(t.TempDir(), "absent"), roomy())
		assertFailure(t, err, exitcode.Filesystem, errs.CodeExtractFailed)
	})

	t.Run("a file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := All(context.Background(), a, path, roomy())
		assertFailure(t, err, exitcode.Filesystem, errs.CodeExtractFailed)
	})

	t.Run("not empty", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "there"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := All(context.Background(), a, dir, roomy())
		assertFailure(t, err, exitcode.Filesystem, errs.CodeExtractFailed)
	})

	t.Run("a symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("making a symlink needs a privilege Windows may not grant")
		}
		link := filepath.Join(t.TempDir(), "link")
		if err := os.Symlink(t.TempDir(), link); err != nil {
			t.Fatal(err)
		}
		_, err := All(context.Background(), a, link, roomy())
		assertFailure(t, err, exitcode.Filesystem, errs.CodeExtractFailed)
	})
}

// A symlink is recreated, not followed and not turned into a file: the archive's
// own library names are the point of it, and a regular file holding the text of a
// target would be a silent breakage.
func TestAllCreatesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this build refuses link entries on Windows")
	}
	a := openFixture(t, "symlink.7z")
	dir := dest(t)

	report, err := All(context.Background(), a, dir, roomy())
	if err != nil {
		t.Fatalf("All = %v", err)
	}
	if report.Links != 4 {
		t.Errorf("Links = %d, want 4", report.Links)
	}

	if got, err := os.Readlink(filepath.Join(dir, "lib", "libfoo.so")); err != nil || got != "libfoo.so.1" {
		t.Errorf("lib/libfoo.so -> %q (%v), want libfoo.so.1", got, err)
	}
	// The chain of links resolves to the real file, which is what makes the
	// reconstruction worth doing.
	if body, err := os.ReadFile(filepath.Join(dir, "lib", "libfoo.so")); err != nil || string(body) != "the real library\n" {
		t.Errorf("reading through lib/libfoo.so = %q (%v)", body, err)
	}
	// A target that steps up but stays inside is kept exactly as written.
	if got, err := os.Readlink(filepath.Join(dir, "sub", "up-link")); err != nil || got != "../top.txt" {
		t.Errorf("sub/up-link -> %q (%v), want ../top.txt", got, err)
	}

	// Modes come from the archive, not from the umask.
	dirInfo, err := os.Stat(filepath.Join(dir, "lib"))
	if err != nil {
		t.Fatalf("stat lib: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o755 {
		t.Errorf("lib mode = %v, want 0755", perm)
	}
	fileInfo, err := os.Stat(filepath.Join(dir, "top.txt"))
	if err != nil {
		t.Fatalf("stat top.txt: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o644 {
		t.Errorf("top.txt mode = %v, want 0644", perm)
	}
}

func TestAllRefusesASymlinkThatLeavesTheDestination(t *testing.T) {
	a := openFixture(t, "symlink-escape.7z")
	dir := dest(t)

	_, err := All(context.Background(), a, dir, roomy())
	assertFailure(t, err, exitcode.Integrity, errs.CodeUnsafeArchive)

	if _, err := os.Lstat(filepath.Join(dir, "lib", "evil")); !os.IsNotExist(err) {
		t.Errorf("lib/evil was created, want it refused: %v", err)
	}
	if left := files(t, dir); len(left) != 0 {
		t.Errorf("the destination holds %v, want no files", left)
	}
}

func TestAllRefusesLinkEntriesOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only Windows refuses link entries")
	}
	a := openFixture(t, "symlink.7z")

	_, err := All(context.Background(), a, dest(t), roomy())
	assertFailure(t, err, exitcode.Integrity, errs.CodeUnsafeArchive)
}

func TestAllStopsOnACancelledContext(t *testing.T) {
	a := openFixture(t, "plain.7z")
	dir := dest(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := All(ctx, a, dir, roomy())
	assertFailure(t, err, exitcode.Interrupted, errs.CodeInterrupted)

	if left := files(t, dir); len(left) != 0 {
		t.Errorf("the destination holds %v, want nothing", left)
	}
}
