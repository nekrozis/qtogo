// Package lzma is qtogo's bridge to the vendored 7-Zip decoder.
//
// The C beside this file is the ANSI-C decoder of the LZMA SDK 26.04, taken
// unchanged: VERSION, LICENSE and manifest.txt record which files, and
// THIRD-PARTY.md at the repository root records where they came from. glue.c and
// glue.h are ours.
//
// The package is deliberately thin — a call and a value, no policy. What an entry
// is called, whether its checksum holds, and what a failure means are for
// internal/sevenzip to decide.
package lzma

/*
#cgo CFLAGS: -DZ7_PPMD_SUPPORT -DZ7_EXTRACT_ONLY
// -DZ7_PPMD_SUPPORT: an archive may carry a PPMd stream. Qt's own archives use
//   LZMA2, but the filter is part of the format and we do not choose what a mirror
//   serves, so the decoder keeps the whole set.
// -DZ7_EXTRACT_ONLY: this links the decoder; the encoder is not vendored.

#include <stdlib.h>

// glue.h is our boundary and names no SDK type; 7zTypes.h is here only so that the
// result codes below can be checked against the decoder's own numbering.
#include "glue.h"
#include "7zTypes.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

// The decoder's result codes. The mapping below is written out in Go so it can be
// read and tested without cgo, and the assertions underneath fail the build if the
// vendored headers ever renumber a code.
const (
	resultOK          = 0
	resultData        = 1
	resultMem         = 2
	resultCRC         = 3
	resultUnsupported = 4
	resultParam       = 5
	resultInputEOF    = 6
	resultOutputEOF   = 7
	resultRead        = 8
	resultWrite       = 9
	resultProgress    = 10
	resultFail        = 11
	resultThread      = 12
	resultArchive     = 16
	resultNoArchive   = 17
)

const (
	_ = uint(resultOK - C.SZ_OK)
	_ = uint(resultData - C.SZ_ERROR_DATA)
	_ = uint(resultMem - C.SZ_ERROR_MEM)
	_ = uint(resultCRC - C.SZ_ERROR_CRC)
	_ = uint(resultUnsupported - C.SZ_ERROR_UNSUPPORTED)
	_ = uint(resultParam - C.SZ_ERROR_PARAM)
	_ = uint(resultInputEOF - C.SZ_ERROR_INPUT_EOF)
	_ = uint(resultOutputEOF - C.SZ_ERROR_OUTPUT_EOF)
	_ = uint(resultRead - C.SZ_ERROR_READ)
	_ = uint(resultWrite - C.SZ_ERROR_WRITE)
	_ = uint(resultProgress - C.SZ_ERROR_PROGRESS)
	_ = uint(resultFail - C.SZ_ERROR_FAIL)
	_ = uint(resultThread - C.SZ_ERROR_THREAD)
	_ = uint(resultArchive - C.SZ_ERROR_ARCHIVE)
	_ = uint(resultNoArchive - C.SZ_ERROR_NO_ARCHIVE)
)

// The failures a caller can act on. The decoder names a dozen more; the ones that
// say nothing about what to do next come back as a plain error carrying the code.
var (
	// ErrMemory says the archive asks for more memory than the budget allowed.
	ErrMemory = errors.New("the memory budget is too small for this archive")
	// ErrNotArchive says the file is not a 7z archive at all.
	ErrNotArchive = errors.New("not a 7z archive")
	// ErrInvalidArchive says the archive's structure does not hold up.
	ErrInvalidArchive = errors.New("invalid archive structure")
	// ErrCorrupt says the data, or the checksum covering it, does not hold up.
	ErrCorrupt = errors.New("corrupt archive data")
	// ErrTruncated says the archive ends before its data does.
	ErrTruncated = errors.New("the archive ends before its data does")
	// ErrUnsupported says a compression method or filter this decoder cannot read.
	ErrUnsupported = errors.New("unsupported compression method or filter")
	// ErrRead says the underlying stream could not be read.
	ErrRead = errors.New("read error")
)

// resultError turns a result code into the failure it means.
func resultError(code uint32) error {
	switch code {
	case resultMem:
		return ErrMemory
	case resultNoArchive:
		return ErrNotArchive
	case resultArchive:
		return ErrInvalidArchive
	case resultCRC, resultData:
		return ErrCorrupt
	case resultInputEOF:
		return ErrTruncated
	case resultUnsupported:
		return ErrUnsupported
	case resultRead:
		return ErrRead
	default:
		return fmt.Errorf("decoder error %d", code)
	}
}

// InBufSize is the read buffer the decoder streams an archive through. It is
// charged to the memory budget like any other allocation, which is why a budget
// smaller than this opens nothing at all.
const InBufSize = uint64(C.QTGO_IN_BUF_SIZE)

// Handle is an open archive, opaque on purpose: the structure behind it belongs to
// glue.c, so nothing here depends on how the decoder is built.
//
// Every function below that takes an index expects one below NumItems: the
// decoder indexes its own arrays with it and does not range-check, so the caller
// checks first. internal/sevenzip does.
type Handle = *C.qtogo_archive

// Init prepares the decoder's tables. It must run before anything else; the layer
// above calls it once.
func Init() {
	C.qtogo_init()
}

// Open reads an archive from an already-open file descriptor, allowing the decoder
// to hold at most budget bytes. The caller keeps ownership of the descriptor.
func Open(fd uintptr, budget uint64) (Handle, error) {
	var result C.int
	handle := C.qtogo_open(C.uintptr_t(fd), C.size_t(budget), &result)
	if handle == nil {
		return nil, resultError(uint32(result))
	}
	return handle, nil
}

// Close releases the archive. It fails if the decoder did not hand back everything
// it took, which would be a bug in the glue or in the decoder.
func Close(h Handle) error {
	if leaked := uint64(C.qtogo_close(h)); leaked != 0 {
		return fmt.Errorf("the decoder kept %d bytes", leaked)
	}
	return nil
}

// Held is the number of bytes the decoder currently holds.
func Held(h Handle) uint64 {
	return uint64(C.qtogo_used(h))
}

// LargestBlock is the biggest decoded block the archive asks for, from its
// metadata.
func LargestBlock(h Handle) uint64 {
	return uint64(C.qtogo_largest_block(h))
}

// NumItems is how many items the archive holds, directories included.
func NumItems(h Handle) int {
	return int(C.qtogo_num_items(h))
}

// IsDir reports whether item index is a directory.
func IsDir(h Handle, index int) bool {
	return C.qtogo_is_dir(h, C.uint32_t(index)) != 0
}

// ItemSize is the decoded size of item index.
func ItemSize(h Handle, index int) uint64 {
	return uint64(C.qtogo_item_size(h, C.uint32_t(index)))
}

// ItemCRC is the checksum the archive records for item index, and whether it
// records one at all.
func ItemCRC(h Handle, index int) (uint32, bool) {
	item := C.uint32_t(index)
	if C.qtogo_item_has_crc(h, item) == 0 {
		return 0, false
	}
	return uint32(C.qtogo_item_crc(h, item)), true
}

// ItemNameLen is the length of item index's name in UTF-16 units, terminator
// included.
func ItemNameLen(h Handle, index int) int {
	return int(C.qtogo_item_name_len(h, C.uint32_t(index)))
}

// ItemName writes item index's name into units, which must be at least
// ItemNameLen units long.
func ItemName(h Handle, index int, units []uint16) {
	if len(units) == 0 {
		return
	}
	C.qtogo_item_name_copy(h, C.uint32_t(index), (*C.uint16_t)(unsafe.Pointer(&units[0])))
}

// Extract decodes item index and points at its bytes inside the block buffer the
// decoder keeps. The bytes stay valid until the next Extract or Close.
func Extract(h Handle, index int) (unsafe.Pointer, uint64, error) {
	var data *C.uint8_t
	var size C.size_t
	if res := C.qtogo_extract(h, C.uint32_t(index), &data, &size); res != C.QTGO_OK {
		return nil, 0, resultError(uint32(res))
	}
	return unsafe.Pointer(data), uint64(size), nil
}
