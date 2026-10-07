// Package sevenzip reads 7z archives through the vendored 7-Zip decoder.
//
// The decoder is internal/lzma and does the decoding; this package is where qtogo
// decides what an archive means. It hands out items in archive order — which is
// what a solid block wants, since its files share one compressed stream — keeps
// the file descriptor to itself, and re-checks each item against the checksum the
// archive records.
//
// It does not decide where bytes go or which names are acceptable: that is
// internal/extract's business. A name is reported exactly as the archive stores
// it; its path segments are normally separated by '/', but an archive can carry
// '\' instead. Whoever turns a name into path segments therefore has to treat both
// as separators — a check that only knew '/' would let "..\" through — which is
// why that decision is the layer above's rather than a detail here.
package sevenzip

import (
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/nekrozis/qtogo/internal/lzma"
)

// UnixExtension is the bit in an attribute word that says its high 16 bits are a
// Unix st_mode. 7-Zip sets it only for entries taken from a Unix filesystem, so
// without it the high half means nothing.
const UnixExtension = 0x8000

// Entry is what an archive says about one item.
type Entry struct {
	Name  string
	Size  uint64
	IsDir bool

	// CRC is the item's checksum and HasCRC says whether the archive recorded one.
	// The decoder verifies it while decoding the item, so a wrong checksum is
	// reported as a decode failure rather than as bad bytes.
	CRC    uint32
	HasCRC bool

	// Attribs is the attribute word the archive records for the item and HasAttribs
	// says whether it records one. The word is passed through as it stands: the
	// low 16 bits are DOS attributes and the high 16 a Unix mode, behind
	// UnixExtension. Reading it is the layer above's business.
	Attribs    uint32
	HasAttribs bool
}

// Archive is an open 7z archive.
//
// An Archive is not safe for concurrent use: reading an item moves the decoder's
// shared block buffer. Different archives are independent, so a caller that wants
// parallelism opens one per goroutine.
type Archive struct {
	handle lzma.Handle
	file   *os.File
	once   sync.Once
}

// errClosed is what a use after Close reports, rather than a nil handle reaching
// the decoder and dereferencing it.
var errClosed = errors.New("the archive is closed")

// Open reads the archive at path, allowing the decoder to hold at most
// memoryBudget bytes at any moment.
//
// Everything the decoder takes counts against that budget, including the fixed
// buffer it streams the file through, so a budget below lzma.InBufSize opens
// nothing. The descriptor never leaves the package.
func Open(path string, memoryBudget uint64) (*Archive, error) {
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

// Len is the number of items, directories included. It is zero once the archive
// is closed.
func (a *Archive) Len() int {
	if a.handle == nil {
		return 0
	}
	return lzma.NumItems(a.handle)
}

// Entry reads the metadata of item i.
func (a *Archive) Entry(i int) (Entry, error) {
	if a.handle == nil {
		return Entry{}, errClosed
	}
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
	entry.Attribs, entry.HasAttribs = lzma.ItemAttribs(a.handle, i)
	return entry, nil
}

// LargestBlock is the size of the biggest decoded block the archive asks for, read
// from its metadata before anything is decoded.
func (a *Archive) LargestBlock() uint64 {
	if a.handle == nil {
		return 0
	}
	return lzma.LargestBlock(a.handle)
}

// WriteEntry writes item i's contents to w.
//
// The bytes live in the block the decoder keeps, so nothing that outlives the call
// is handed out — which is why this writes rather than returning a slice: the
// buffer's lifetime is the decoder's, and a caller holding on to it is a bug the
// type system cannot catch.
//
// A corrupt item never reaches the write: the decoder verifies the item's checksum
// while decoding it, so lzma.Extract fails first. The check below repeats that
// verification as a guard against a bug in the decoder rather than against a
// corrupt archive, which is why no archive can be built that reaches it.
func (a *Archive) WriteEntry(i int, w io.Writer) error {
	if a.handle == nil {
		return errClosed
	}
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
		// Unreachable with a correct decoder, which is the point of keeping it.
		return fmt.Errorf("item %d: checksum mismatch", i)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("item %d: %w", i, err)
	}
	return nil
}

// Held is the number of bytes the decoder currently holds. It is zero once the
// archive is closed.
func (a *Archive) Held() uint64 {
	if a.handle == nil {
		return 0
	}
	return lzma.Held(a.handle)
}

// Close releases the decoder and the file, and is safe to call twice.
//
// It reports a decoder that did not hand everything back: that is a bug, in this
// package or in the decoder, and silence would hide it. Using the archive
// afterwards is refused rather than passed to the decoder.
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
