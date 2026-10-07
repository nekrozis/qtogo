package lzma

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"unsafe"
)

func fixturePath(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "testdata"}, parts...)...)
}

func openFixture(t *testing.T, name string) uintptr {
	t.Helper()

	f, err := os.Open(fixturePath("archive", name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f.Fd()
}

// The glue is used through internal/sevenzip, but its own contract is worth
// pinning here: hand it a descriptor, read the metadata, take the bytes, close,
// and leave nothing behind.
func TestGlueReadsAnArchive(t *testing.T) {
	Init()

	h, err := Open(openFixture(t, "plain.7z"), 4<<20)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	if got := NumItems(h); got != 2 {
		t.Errorf("NumItems = %d, want 2", got)
	}
	if Held(h) == 0 {
		t.Error("Held = 0 while open, want what the decoder is holding")
	}
	if LargestBlock(h) == 0 {
		t.Error("LargestBlock = 0, want the size the metadata reports")
	}

	data, size, err := Extract(h, 0)
	if err != nil {
		t.Fatalf("Extract = %v", err)
	}
	if body := unsafe.Slice((*byte)(data), int(size)); len(body) == 0 {
		t.Error("Extract returned no bytes")
	}

	if err := Close(h); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestGlueNamesAndChecksums(t *testing.T) {
	Init()

	h, err := Open(openFixture(t, "plain.7z"), 4<<20)
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	defer Close(h)

	if _, ok := ItemCRC(h, 0); !ok {
		t.Error("ItemCRC reports no checksum for an item the archive covers")
	}
	if !IsDir(h, 0) && ItemSize(h, 0) == 0 {
		t.Error("ItemSize = 0 for a file with contents")
	}

	length := ItemNameLen(h, 0)
	if length <= 1 {
		t.Fatalf("ItemNameLen = %d, want a name and its terminator", length)
	}
	units := make([]uint16, length)
	ItemName(h, 0, units)
	if units[length-1] != 0 {
		t.Error("the name is not terminated")
	}
}

func TestGlueReportsWhatItCannotOpen(t *testing.T) {
	Init()

	fd := openFixture(t, "plain.7z")

	// Below the decoder's own read buffer, nothing opens.
	if _, err := Open(fd, 1024); !errors.Is(err, ErrMemory) {
		t.Errorf("Open with a 1 KiB budget = %v, want ErrMemory", err)
	}
	if InBufSize == 0 {
		t.Error("InBufSize = 0")
	}

	// The same descriptor still reads an archive once the budget allows it, which
	// is what tells the two failures apart.
	h, err := Open(fd, 4<<20)
	if err != nil {
		t.Errorf("Open with a real budget = %v", err)
	} else {
		Close(h)
	}
}

// The result codes are mapped here rather than in the caller, so this pins what
// each one means: the failures a caller can act on, and a plain error for the rest.
func TestResultCodesReadAsFailures(t *testing.T) {
	tests := map[uint32]error{
		resultMem:         ErrMemory,
		resultNoArchive:   ErrNotArchive,
		resultArchive:     ErrInvalidArchive,
		resultCRC:         ErrCorrupt,
		resultData:        ErrCorrupt,
		resultInputEOF:    ErrTruncated,
		resultUnsupported: ErrUnsupported,
		resultRead:        ErrRead,
	}

	for code, want := range tests {
		if got := resultError(code); !errors.Is(got, want) {
			t.Errorf("resultError(%d) = %v, want %v", code, got, want)
		}
	}

	if got := resultError(99); got == nil || got.Error() != "decoder error 99" {
		t.Errorf("resultError(99) = %v, want a generic message", got)
	}
}
