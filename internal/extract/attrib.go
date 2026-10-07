package extract

import (
	"os"

	"github.com/nekrozis/qtogo/internal/sevenzip"
)

// kind is what an entry turns out to be once the archive's attribute word has been
// read.
type kind uint8

const (
	kindFile kind = iota
	kindDir
	kindLink
	kindOther // a device, fifo or socket: recognised, never created
)

// The file-type bits of a Unix mode. 7z stores the mode verbatim in the high half
// of the attribute word, so these are the values it carries.
const (
	sIFMT  = 0xF000
	sIFREG = 0x8000
	sIFDIR = 0x4000
	sIFLNK = 0xA000
)

// classify reads an entry's attribute word.
//
// The high half is a mode only when the extension bit is set. A Windows archive
// sets DOS attributes and no mode, so the bit is checked first: read the high half
// regardless and those attribute bits come back as a file type.
func classify(e sevenzip.Entry) (kind, os.FileMode) {
	if e.HasAttribs && e.Attribs&sevenzip.UnixExtension != 0 {
		mode := e.Attribs >> 16
		switch mode & sIFMT {
		case sIFDIR:
			return kindDir, permissions(mode)
		case sIFREG:
			return kindFile, permissions(mode)
		case sIFLNK:
			return kindLink, 0
		default:
			// A device, fifo or socket, or a type this build does not know: either
			// way, not something to create.
			return kindOther, 0
		}
	}
	if e.IsDir {
		return kindDir, 0o755
	}
	return kindFile, 0o644
}

// permissions is the part of a mode an archive may set: the low nine bits, with
// setuid, setgid and sticky dropped. Which bits a file carries is the archive's to
// say; which identities it runs as is not.
func permissions(mode uint32) os.FileMode {
	return os.FileMode(mode & 0o777)
}
