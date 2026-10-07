package sevenzip

import (
	"bytes"
	"errors"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/nekrozis/qtogo/internal/lzma"
)

// The fixtures are tiny archives made for these tests; testdata/README.md records
// how they were built.
func fixturePath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
}

func openFixture(t *testing.T, name string) *Archive {
	t.Helper()

	a, err := Open(fixturePath("archive", name), 4<<20)
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

func entryBytes(t *testing.T, a *Archive, i int) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := a.WriteEntry(i, &buf); err != nil {
		t.Fatalf("WriteEntry(%d) = %v", i, err)
	}
	return buf.Bytes()
}

func TestOpenListsTheEntries(t *testing.T) {
	tests := []struct {
		fixture string
		want    map[string]string
	}{
		{"plain.7z", map[string]string{"docs/readme.md": "readme\n", "docs/reading.md": "reading\n"}},
		{"solid.7z", map[string]string{"a.txt": "alpha\n", "b.txt": "beta\n", "c.txt": "gamma\n"}},
	}

	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			a := openFixture(t, tt.fixture)
			if a.Len() != len(tt.want) {
				t.Fatalf("Len() = %d, want %d", a.Len(), len(tt.want))
			}

			seen := map[string]bool{}
			for i := 0; i < a.Len(); i++ {
				entry, err := a.Entry(i)
				if err != nil {
					t.Fatalf("Entry(%d) = %v", i, err)
				}
				if entry.IsDir {
					t.Errorf("%s: reported as a directory", entry.Name)
				}
				body := entryBytes(t, a, i)
				if want, ok := tt.want[entry.Name]; !ok {
					t.Errorf("unexpected entry %q", entry.Name)
				} else if string(body) != want {
					t.Errorf("%s: contents %q, want %q", entry.Name, body, want)
				}
				if entry.Size != uint64(len(body)) {
					t.Errorf("%s: size %d, want %d", entry.Name, entry.Size, len(body))
				}
				// What the archive records must be the standard CRC-32 of the
				// contents, or WriteEntry would reject its own output.
				if entry.HasCRC && entry.CRC != crc32.ChecksumIEEE(body) {
					t.Errorf("%s: archive CRC %08x, contents CRC %08x", entry.Name, entry.CRC, crc32.ChecksumIEEE(body))
				}
				seen[entry.Name] = true
			}

			for name := range tt.want {
				if !seen[name] {
					t.Errorf("entry %q was never returned", name)
				}
			}
		})
	}
}

// A solid archive keeps its files in one compressed stream, so reading an earlier
// file after a later one has to work too: the decoder re-decodes the block.
func TestSolidArchiveReadsInAnyOrder(t *testing.T) {
	a := openFixture(t, "solid.7z")

	want := map[string]string{"a.txt": "alpha\n", "b.txt": "beta\n", "c.txt": "gamma\n"}
	for _, i := range []int{2, 0, 2, 1} {
		entry, err := a.Entry(i)
		if err != nil {
			t.Fatalf("Entry(%d) = %v", i, err)
		}
		if body := entryBytes(t, a, i); string(body) != want[entry.Name] {
			t.Errorf("%s: contents %q, want %q", entry.Name, body, want[entry.Name])
		}
	}
}

// LargestBlock comes from the metadata, so the caller can judge an archive before
// decoding any of it.
func TestLargestBlockComesFromTheMetadata(t *testing.T) {
	a := openFixture(t, "solid.7z")

	// The fixture holds three files of six, five and six bytes, all in one block.
	if got := a.LargestBlock(); got != 17 {
		t.Errorf("LargestBlock() = %d, want 17", got)
	}
}

func TestMemoryBudgetIsEnforced(t *testing.T) {
	// Below the decoder's own read buffer, nothing can be opened at all.
	if _, err := Open(fixturePath("archive", "plain.7z"), 1024); !errors.Is(err, lzma.ErrMemory) {
		t.Errorf("Open with a 1 KiB budget = %v, want lzma.ErrMemory", err)
	}
	// Above it, the same archive opens, and the decoder stays within the budget.
	a, err := Open(fixturePath("archive", "solid.7z"), 512<<10)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	defer a.Close()

	if held := a.Held(); held > 512<<10 {
		t.Errorf("decoder holds %d bytes, above the %d byte budget", held, 512<<10)
	}
}

func TestCloseReleasesEverything(t *testing.T) {
	a, err := Open(fixturePath("archive", "solid.7z"), 4<<20)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	if err := a.WriteEntry(0, io.Discard); err != nil {
		t.Fatalf("WriteEntry(0) = %v", err)
	}
	if a.Held() == 0 {
		t.Error("Held() = 0 while open, want what the decoder is holding")
	}

	// Close reports a decoder that kept anything, so a nil error is the check.
	if err := a.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// Using a closed archive is refused rather than passed to the decoder as a nil
// handle.
func TestUseAfterCloseIsRefused(t *testing.T) {
	a, err := Open(fixturePath("archive", "plain.7z"), 4<<20)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if _, err := a.Entry(0); !errors.Is(err, errClosed) {
		t.Errorf("Entry after Close = %v, want errClosed", err)
	}
	if err := a.WriteEntry(0, io.Discard); !errors.Is(err, errClosed) {
		t.Errorf("WriteEntry after Close = %v, want errClosed", err)
	}
	if got := a.Len(); got != 0 {
		t.Errorf("Len after Close = %d, want 0", got)
	}
	if got := a.Held(); got != 0 {
		t.Errorf("Held after Close = %d, want 0", got)
	}
	if got := a.LargestBlock(); got != 0 {
		t.Errorf("LargestBlock after Close = %d, want 0", got)
	}
}

func TestCloseReportsAFileItCannotClose(t *testing.T) {
	a, err := Open(fixturePath("archive", "plain.7z"), 4<<20)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	// Closing the file behind the archive's back leaves Close with an error to
	// report rather than a success to claim.
	if err := a.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err == nil {
		t.Error("Close reported success after the file was already closed")
	}
}

func TestOpenRejectsWhatIsNotAnArchive(t *testing.T) {
	dir := t.TempDir()

	tests := map[string][]byte{
		"empty.7z":     {},
		"text.7z":      []byte("this is not an archive"),
		"signature.7z": append([]byte("7z\xbc\xaf'\x1c"), make([]byte, 64)...),
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			if a, err := Open(path, 4<<20); err == nil {
				a.Close()
				t.Error("Open succeeded, want a failure")
			}
		})
	}
}

func TestReadingATruncatedArchiveFails(t *testing.T) {
	body, err := os.ReadFile(fixturePath("archive", "solid.7z"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "truncated.7z")
	if err := os.WriteFile(path, body[:len(body)-40], 0o600); err != nil {
		t.Fatal(err)
	}

	a, err := Open(path, 4<<20)
	if err != nil {
		return // rejecting it at open is also a correct outcome
	}
	defer a.Close()

	for i := 0; i < a.Len(); i++ {
		if err := a.WriteEntry(i, io.Discard); err != nil {
			return
		}
	}
	t.Error("reading a truncated archive succeeded, want a failure")
}

// A flipped byte in the compressed stream must not pass unnoticed: the decoder
// checks each solid block against the checksum the header records. The byte is
// taken from the data, after the start header and before the header at the end, so
// the archive still opens and the damage only shows when a block is decoded.
func TestCorruptDataIsCaught(t *testing.T) {
	body, err := os.ReadFile(fixturePath("archive", "solid.7z"))
	if err != nil {
		t.Fatal(err)
	}
	body[40] ^= 0xff
	path := filepath.Join(t.TempDir(), "corrupt.7z")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	a, err := Open(path, 4<<20)
	if err != nil {
		return // rejecting it while reading the header is fine too
	}
	defer a.Close()

	for i := 0; i < a.Len(); i++ {
		if err := a.WriteEntry(i, io.Discard); err != nil {
			return
		}
		t.Errorf("item %d decoded without reporting an error", i)
	}
}

// Directories and empty files arrive as entries too: a repository archive carries
// them, and the caller has to be able to tell them apart from files with contents.
func TestDirectoryAndEmptyEntries(t *testing.T) {
	a := openFixture(t, "tree.7z")

	got := map[string]Entry{}
	for i := 0; i < a.Len(); i++ {
		entry, err := a.Entry(i)
		if err != nil {
			t.Fatalf("Entry(%d) = %v", i, err)
		}
		got[entry.Name] = entry
	}

	if entry, ok := got["dir"]; !ok || !entry.IsDir {
		t.Errorf("dir = %+v, want a directory", entry)
	}
	if entry, ok := got["empty.txt"]; !ok || entry.IsDir || entry.Size != 0 {
		t.Errorf("empty.txt = %+v, want an empty file", entry)
	}
	if entry, ok := got["dir/file.txt"]; !ok || entry.IsDir || entry.Size != 1 {
		t.Errorf("dir/file.txt = %+v, want a one byte file", entry)
	}

	// Writing a directory and an empty file produces nothing and no failure; the
	// caller distinguishes them by the entry it just read.
	for i := 0; i < a.Len(); i++ {
		entry, err := a.Entry(i)
		if err != nil {
			t.Fatalf("Entry(%d) = %v", i, err)
		}
		body := entryBytes(t, a, i)
		if entry.Name != "dir/file.txt" && len(body) != 0 {
			t.Errorf("%s: got %d bytes, want none", entry.Name, len(body))
		}
	}
}

// A name can carry '\' where the format usually has '/': an archive built on
// Windows can use it, so the layer that turns names into paths has to treat it as
// a separator too. This fixture pins that such a name reaches the caller verbatim.
func TestBackslashNamesAreReportedVerbatim(t *testing.T) {
	a := openFixture(t, "backslash.7z")

	want := map[string]string{
		"sub\\nested\\f.txt": "nested",
		"..\\escape.txt":     "escape",
	}

	files, dirs := 0, 0
	for i := 0; i < a.Len(); i++ {
		entry, err := a.Entry(i)
		if err != nil {
			t.Fatalf("Entry(%d) = %v", i, err)
		}
		if entry.IsDir {
			dirs++
			continue
		}
		files++
		body := entryBytes(t, a, i)
		if contents, ok := want[entry.Name]; !ok {
			t.Errorf("unexpected entry %q", entry.Name)
		} else if string(body) != contents {
			t.Errorf("%s: contents %q, want %q", entry.Name, body, contents)
		}
	}

	if files != len(want) {
		t.Errorf("got %d files, want %d", files, len(want))
	}
	// The directories are "sub", "sub\nested" and "..".
	if dirs != 3 {
		t.Errorf("got %d directories, want 3", dirs)
	}
}

// A Windows-written archive records DOS attributes only. The Unix-extension bit is
// what says the high half is a mode, so it must stay clear here: reading it anyway
// would turn an archive attribute into a nonsense file type.
func TestDosOnlyAttributesCarryNoUnixMode(t *testing.T) {
	const (
		dosArchive   = 0x20
		dosDirectory = 0x10
	)

	a := openFixture(t, "tree.7z")

	for i := 0; i < a.Len(); i++ {
		entry, err := a.Entry(i)
		if err != nil {
			t.Fatalf("Entry(%d) = %v", i, err)
		}
		if !entry.HasAttribs {
			t.Errorf("%s: no attributes recorded", entry.Name)
			continue
		}
		if entry.Attribs&UnixExtension != 0 {
			t.Errorf("%s: attributes %#x carry the Unix extension %#x, want DOS only",
				entry.Name, entry.Attribs, uint32(UnixExtension))
		}
		if entry.IsDir && entry.Attribs&dosDirectory == 0 {
			t.Errorf("%s: directory attributes %#x lack the directory bit", entry.Name, entry.Attribs)
		}
		if !entry.IsDir && entry.Attribs&dosArchive == 0 {
			t.Errorf("%s: file attributes %#x lack the archive bit", entry.Name, entry.Attribs)
		}
	}
}

func TestEntryIndexIsBoundsChecked(t *testing.T) {
	a := openFixture(t, "plain.7z")

	for _, i := range []int{-1, a.Len()} {
		if _, err := a.Entry(i); err == nil {
			t.Errorf("Entry(%d) succeeded, want a failure", i)
		}
		if err := a.WriteEntry(i, io.Discard); err == nil {
			t.Errorf("WriteEntry(%d) succeeded, want a failure", i)
		}
	}
}

func TestOpenReportsAMissingFile(t *testing.T) {
	if _, err := Open(filepath.Join(t.TempDir(), "absent.7z"), 4<<20); err == nil {
		t.Error("Open on a missing file succeeded, want a failure")
	}
}

// A refusal says which refusal it was, so the layer above can decide what to tell
// the user and which exit code that is.
func TestOpenSaysWhyItRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "text.7z")
	if err := os.WriteFile(path, []byte("this is not an archive"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path, 4<<20); !errors.Is(err, lzma.ErrNotArchive) {
		t.Errorf("Open = %v, want lzma.ErrNotArchive", err)
	}
}
