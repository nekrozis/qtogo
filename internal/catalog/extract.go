package catalog

import (
	"strings"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/repository"
)

// targetDirPrefix is the template prefix an Extract destination carries; it names
// the directory the user chose, which the parse layer does not know.
const targetDirPrefix = "@TargetDir@/"

// install is one archive and where its contents go, as the metadata states it.
type install struct {
	archive string
	dest    string
}

// extractPairs reads a package's archive-to-destination list.
//
// The Extract arguments come in pairs — a destination and an archive name — and
// the destination has the "@TargetDir@/" prefix removed. When there are no Extract
// operations the package's downloads have no destination of their own and land at
// the root (an empty dest); an old archive carries its version and architecture
// inside itself, so extracting it at the root is what places it correctly.
//
// The pairing is per package, never per version: one repository's metadata has
// some packages with Extract and some without, and the same release differs
// between hosts. A trailing argument with no partner is ignored, matching the
// reference tool's leniency.
func extractPairs(pkg repository.PackageUpdate) []install {
	if args := extractArgs(pkg); len(args) > 0 {
		pairs := make([]install, 0, len(args)/2)
		for i := 0; i+1 < len(args); i += 2 {
			pairs = append(pairs, install{archive: args[i+1], dest: strings.TrimPrefix(args[i], targetDirPrefix)})
		}
		return pairs
	}

	downloads := make([]install, 0, len(pkg.Downloadables))
	for _, name := range pkg.Downloadables {
		downloads = append(downloads, install{archive: name})
	}
	return downloads
}

// extractArgs returns the arguments of every Extract operation, in order. Each
// operation carries one (destination, archive) pair, so the arguments across all
// of them alternate destination, archive, destination, archive.
func extractArgs(pkg repository.PackageUpdate) []string {
	var args []string
	for _, op := range pkg.Operations {
		if op.Name == "Extract" {
			args = append(args, op.Arguments...)
		}
	}
	return args
}

// notFound builds the failure for something the request asked for and the
// repository does not have: a missing version, base package or module is exit 3.
func notFound(format string, args ...any) error {
	return errs.New(exitcode.NotFound, errs.CodePackageNotFound, errs.PhaseResolve, format, args...)
}
