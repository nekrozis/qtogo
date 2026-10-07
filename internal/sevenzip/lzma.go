// Package sevenzip reads 7z archives through the vendored 7-Zip decoder.
//
// The decoder is internal/lzma and does the decoding; this package is where qtogo
// decides what an archive means. It hands out items in archive order — which is
// what a solid block wants, since its files share one compressed stream — keeps
// the file descriptor to itself, and checks each item against the checksum the
// archive records.
//
// It does not decide where bytes go or which names are acceptable: that is
// internal/extract's business.
package sevenzip

import (
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/nekrozis/qtogo/internal/lzma"
)

// Entry is what an archive says about one item.
type Entry struct {
	Name  string
	Size  uint64
	IsDir bool

	// CRC is the item's checksum and HasCRC says whether the archive recorded one.
	// The decoder checks the checksum of each solid block it decodes, but not the
	// per-item ones; Bytes checks those.
	CRC    uint32
	HasCRC bool
}

// Archive is an open 7z archive.
type Archive struct {
	handle lzma.Handle
	file   *os.File
	once   sync.Once
}

var initOnce sync.Once

// Open reads the archive at path, allowing the decoder to hold at most
// memoryBudget bytes at any moment.
//
// Everything the decoder takes counts against that budget, including the fixed
// buffer it streams the file through, so a budget below lzma.InBufSize opens
// nothing. The descriptor never leaves the package.
func Open(path string, memoryBudget uint64) (*Archive, error) {
	initOnce.Do(lzma.Init)

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	handle, err := lzma.Open(f.Fd(), memoryBudget)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Archive{handle: handle, file: f}, nil
}

// Len is the number of items, directories included.
func (a *Archive) Len() int {
	return lzma.NumItems(a.handle)
}

// Entry reads the metadata of item i.
func (a *Archive) Entry(i int) (Entry, error) {
	if i < 0 || i >= a.Len() {
		return Entry{}, fmt.Errorf("item %d is out of range", i)
	}

	length := lzma.ItemNameLen(a.handle, i)
	if length <= 0 {
		return Entry{}, fmt.Errorf("item %d has no name", i)
	}
	units := make([]uint16, length)
	lzma.ItemName(a.handle, i, units)

	entry := Entry{
		Name:  string(utf16.Decode(units[:length-1])),
		Size:  lzma.ItemSize(a.handle, i),
		IsDir: lzma.IsDir(a.handle, i),
	}
	entry.CRC, entry.HasCRC = lzma.ItemCRC(a.handle, i)
	return entry, nil
}

// LargestBlock is the size of the biggest decoded block the archive asks for, read
// from its metadata before anything is decoded.
func (a *Archive) LargestBlock() uint64 {
	return lzma.LargestBlock(a.handle)
}

// WriteEntry writes item i's contents to w.
//
// The bytes live in the block the decoder keeps, so they are checked against the
// checksum the archive records before any of them are written — a caller that had
// already written half an item would have to undo it. Nothing that outlives the
// call is handed out, which is why this writes rather than returning a slice: the
// buffer's lifetime is the decoder's, and a caller holding on to it is a bug the
// type system cannot catch.
func (a *Archive) WriteEntry(i int, w io.Writer) error {
	if i < 0 || i >= a.Len() {
		return fmt.Errorf("item %d is out of range", i)
	}
	if lzma.IsDir(a.handle, i) {
		return nil
	}

	data, size, err := lzma.Extract(a.handle, i)
	if err != nil {
		return fmt.Errorf("item %d: %w", i, err)
	}
	if size == 0 {
		return nil
	}

	body := unsafe.Slice((*byte)(data), int(size)) //nolint:gosec // a view of the decoder's own buffer, alive for this call
	if want, ok := lzma.ItemCRC(a.handle, i); ok && crc32.ChecksumIEEE(body) != want {
		return fmt.Errorf("item %d: checksum mismatch", i)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("item %d: %w", i, err)
	}
	return nil
}

// Held is the number of bytes the decoder currently holds.
func (a *Archive) Held() uint64 {
	return lzma.Held(a.handle)
}

// Close releases the decoder and the file, and is safe to call twice.
//
// It reports a decoder that did not hand everything back: that is a bug, in this
// package or in the decoder, and silence would hide it.
func (a *Archive) Close() error {
	var err error
	a.once.Do(func() {
		decoderErr := lzma.Close(a.handle)
		a.handle = nil

		fileErr := a.file.Close()
		switch {
		case decoderErr != nil:
			err = decoderErr
		case fileErr != nil:
			err = fileErr
		}
	})
	return err
}
