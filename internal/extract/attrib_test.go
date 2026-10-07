package extract

import (
	"os"
	"testing"

	"github.com/nekrozis/qtogo/internal/sevenzip"
)

// unix packs a mode the way an archive records it: the mode in the high half of
// the attribute word, behind the extension bit.
func unix(mode uint32) uint32 {
	return sevenzip.UnixExtension | mode<<16
}

func TestClassifyReadsAModeOnlyBehindTheExtensionBit(t *testing.T) {
	const sIFCHR = 0x2000

	tests := []struct {
		name     string
		entry    sevenzip.Entry
		wantKind kind
		wantMode os.FileMode
	}{
		{
			"a Unix regular file",
			sevenzip.Entry{Attribs: unix(sIFREG | 0o644), HasAttribs: true},
			kindFile, 0o644,
		},
		{
			"an executable keeps its bits but not setuid",
			sevenzip.Entry{Attribs: unix(sIFREG | 0o4755), HasAttribs: true},
			kindFile, 0o755,
		},
		{
			"a Unix directory",
			sevenzip.Entry{Attribs: unix(sIFDIR | 0o775), HasAttribs: true},
			kindDir, 0o775,
		},
		{
			"a Unix symlink",
			sevenzip.Entry{Attribs: unix(sIFLNK | 0o777), HasAttribs: true},
			kindLink, 0,
		},
		{
			"a Unix character device",
			sevenzip.Entry{Attribs: unix(sIFCHR | 0o666), HasAttribs: true},
			kindOther, 0,
		},
		{
			"DOS attributes with no extension bit stay a plain file",
			sevenzip.Entry{Attribs: 0x20, HasAttribs: true},
			kindFile, 0o644,
		},
		{
			"a directory the archive records no attributes for",
			sevenzip.Entry{IsDir: true},
			kindDir, 0o755,
		},
		{
			"a file the archive records no attributes for",
			sevenzip.Entry{},
			kindFile, 0o644,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKind, gotMode := classify(tt.entry)
			if gotKind != tt.wantKind {
				t.Errorf("kind = %d, want %d", gotKind, tt.wantKind)
			}
			if gotMode != tt.wantMode {
				t.Errorf("mode = %v, want %v", gotMode, tt.wantMode)
			}
		})
	}
}
