// Package extract writes an archive's entries into a destination directory.
//
// The destination is a root the caller owns: it has to exist already, be a
// directory, and be empty. The archive does not get to choose where its bytes go;
// no name is trusted, and every name is checked against that root before any
// content is written.
//
// It creates regular files, directories and symlinks, and nothing else -- never a
// device, fifo or socket, and never through a symlink that is somehow already
// there. That last promise is what the empty destination buys: with nothing in the
// tree, there is no link to follow, so no O_NOFOLLOW dance is needed.
//
// A symlink is recreated where the platform can always make one. Windows is not
// such a platform -- whether it will depends on a machine setting -- so a link
// entry is refused there, rather than turning one archive into different trees on
// different machines.
//
// A failed extraction leaves the destination part-written. That is deliberate: the
// destination is the caller's private staging area, which a caller discards on
// failure rather than repairing.
package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/lzma"
	"github.com/nekrozis/qtogo/internal/sevenzip"
)

// Limits bound one extraction. Every field has to be positive: a bound a caller
// forgot to set is a mistake this package refuses to guess at, because guessing
// means letting an archive decide how much of the disk it takes.
type Limits struct {
	// MaxEntries is the most entries the archive may hold.
	MaxEntries int
	// MaxBytes is the most decoded data the archive may hold in total.
	MaxBytes uint64
	// MaxEntry is the most decoded data one entry may hold.
	MaxEntry uint64
	// MaxBlock is the most data one solid block may decode to -- the memory the
	// decoder needs at once, known from the metadata before any of it is decoded.
	MaxBlock uint64
}

// Report is what an extraction produced.
type Report struct {
	Files int
	Dirs  int
	Links int
	Bytes uint64
}

// item is one entry, resolved: where it goes and what it is.
type item struct {
	index int
	name  string // the archive's name, kept for messages
	path  string
	kind  kind
	mode  os.FileMode
	size  uint64
}

// All writes every entry of a into dest.
//
// dest must be an existing, empty directory that the caller owns; anything else is
// refused, and nothing is written.
func All(ctx context.Context, a *sevenzip.Archive, dest string, limits Limits) (Report, error) {
	if err := limits.check(); err != nil {
		return Report{}, err
	}
	root, err := destinationRoot(dest)
	if err != nil {
		return Report{}, err
	}
	items, err := scan(a, root, limits)
	if err != nil {
		return Report{}, err
	}
	return write(ctx, a, root, items)
}

// check rejects a limit a caller left unset. A zero or negative bound would make
// the check it guards meaningless, so it is refused rather than read as "no
// bound".
func (l Limits) check() error {
	if l.MaxEntries <= 0 {
		return unsetLimit("MaxEntries")
	}
	fields := []struct {
		name  string
		value uint64
	}{
		{"MaxBytes", l.MaxBytes},
		{"MaxEntry", l.MaxEntry},
		{"MaxBlock", l.MaxBlock},
	}
	for _, f := range fields {
		if f.value == 0 {
			return unsetLimit(f.name)
		}
	}
	return nil
}

// unsetLimit reports a bound a caller left at its zero value.
func unsetLimit(name string) error {
	return errs.New(exitcode.Internal, errs.CodeUnclassified, errs.PhaseExtract,
		"the %s limit was never set, and a limit of zero is not a bound", name)
}

// destinationRoot insists on a directory the caller owns and has not put anything
// in yet, and returns its absolute path.
func destinationRoot(dest string) (string, error) {
	root, err := filepath.Abs(dest)
	if err != nil {
		return "", failed("resolving the destination", err)
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", failed("reading the destination", err)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return "", refused("the destination %s is a symlink, and its target is not ours to write into", root)
	case !info.IsDir():
		return "", refused("the destination %s is not a directory", root)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", failed("reading the destination", err)
	}
	if len(entries) > 0 {
		return "", refused("the destination %s is not empty", root)
	}
	return root, nil
}

// scan reads the archive's metadata, holds it against the limits, and resolves
// every name -- all before a byte of content is decoded or written. A bomb is
// turned away here, where the cost is reading a header rather than writing
// gigabytes.
func scan(a *sevenzip.Archive, root string, limits Limits) ([]item, error) {
	count := a.Len()
	if count > limits.MaxEntries {
		return nil, overLimit("the archive holds %d entries, above the %d entry limit", count, limits.MaxEntries)
	}
	if block := a.LargestBlock(); block > limits.MaxBlock {
		return nil, overLimit("one solid block holds %d bytes, above the %d byte block limit", block, limits.MaxBlock)
	}

	items := make([]item, 0, count)
	var total uint64
	for i := 0; i < count; i++ {
		entry, err := a.Entry(i)
		if err != nil {
			return nil, undecodable(err, "reading the metadata of entry %d", i)
		}
		path, err := target(root, entry.Name)
		if err != nil {
			return nil, unsafeEntry(entry.Name, err)
		}
		k, mode := classify(entry)
		if k == kindOther {
			return nil, unsafeEntry(entry.Name, errors.New("it is not a regular file, a directory or a symlink"))
		}
		// A Windows-target archive holds no links anyway, and whether Windows would
		// make one depends on a machine setting -- so the same archive would give
		// different trees on different machines. Refusing here is the metadata saying
		// so before a byte has been written.
		if k == kindLink && runtime.GOOS == "windows" {
			return nil, unsafeEntry(entry.Name,
				errors.New("the archive holds a symlink, and Windows will not create one the same way on every machine"))
		}

		it := item{index: i, name: entry.Name, path: path, kind: k, mode: mode}
		// A link's bytes are its target, and they are decoded into memory rather
		// than streamed, so they are bounded like a file's.
		if k == kindFile || k == kindLink {
			if entry.Size > limits.MaxEntry {
				return nil, overLimit("the entry %q holds %d bytes, above the %d byte entry limit",
					it.name, entry.Size, limits.MaxEntry)
			}
			// Written as a subtraction, so a size that would overflow the running
			// total reads as "too much" rather than as room to spare.
			it.size = entry.Size
			if it.size > limits.MaxBytes-total {
				return nil, overLimit("the archive holds more than %d bytes", limits.MaxBytes)
			}
			total += it.size
		}
		items = append(items, it)
	}
	return items, nil
}

// write creates the entries scan resolved. Directories and files go first and
// links last: created earlier, a link the archive made could be the path a later
// entry is written through, and the destination is the one place nothing is
// followed.
func write(ctx context.Context, a *sevenzip.Archive, root string, items []item) (Report, error) {
	var (
		report Report
		dirs   []item
		links  []item
	)

	for _, it := range items {
		if err := ctx.Err(); err != nil {
			return Report{}, cancelled(err)
		}
		switch it.kind {
		case kindDir:
			// Created closed, then opened up to what the archive asks for at the end:
			// the mode the archive names is not applied while its children are still
			// to come.
			if err := os.MkdirAll(it.path, 0o750); err != nil {
				return Report{}, failed("creating the directory "+it.name, err)
			}
			dirs = append(dirs, it)
			report.Dirs++
		case kindFile:
			written, err := writeFile(a, it)
			if err != nil {
				return Report{}, err
			}
			report.Files++
			report.Bytes += written
		case kindLink:
			links = append(links, it)
		}
	}

	for _, it := range links {
		if err := ctx.Err(); err != nil {
			return Report{}, cancelled(err)
		}
		written, err := writeLink(a, it, root)
		if err != nil {
			return Report{}, err
		}
		report.Links++
		report.Bytes += written
	}

	// Directory modes go on once every child exists: a directory the archive marks
	// unwritable would otherwise turn away the children still to come.
	for _, it := range dirs {
		if err := os.Chmod(it.path, it.mode); err != nil {
			return Report{}, failed("setting the mode of the directory "+it.name, err)
		}
	}
	return report, nil
}

// writeFile creates one regular file and puts the entry's bytes in it. O_EXCL is
// the point: an entry must not land on top of one already there, whether the
// archive names it twice or a name resolves onto a path another entry took.
func writeFile(a *sevenzip.Archive, it item) (uint64, error) {
	// A parent the archive names is set to what it asked for once everything below
	// it exists; one it does not name simply keeps the 0750 it is created with.
	if err := os.MkdirAll(filepath.Dir(it.path), 0o750); err != nil {
		return 0, failed("creating the directory for "+it.name, err)
	}
	// Created closed: the mode the archive asks for is put on below, so the file
	// is never readable for longer than the write takes.
	f, err := os.OpenFile(it.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, failed("creating "+it.name, err)
	}

	counted := &countWriter{w: f}
	writeErr := a.WriteEntry(it.index, counted)
	closeErr := f.Close()
	if writeErr != nil {
		return 0, undecodable(writeErr, "decoding %s", it.name)
	}
	if closeErr != nil {
		return 0, failed("writing "+it.name, closeErr)
	}
	if counted.n != it.size {
		return 0, undecodable(errors.New("the bytes decoded are not the size the archive records"), "decoding %s", it.name)
	}
	if err := os.Chmod(it.path, it.mode); err != nil {
		return 0, failed("setting the mode of "+it.name, err)
	}
	return counted.n, nil
}

// writeLink decodes one symlink entry and creates the link.
//
// The target is the entry's content, so where the link points is only known once
// it has been decoded -- which is why links are made in a pass of their own. A
// target that leaves the destination is refused here, with the files already
// written; the caller throws the destination away rather than repairing it.
func writeLink(a *sevenzip.Archive, it item, root string) (uint64, error) {
	var target bytes.Buffer
	counted := &countWriter{w: &target}
	if err := a.WriteEntry(it.index, counted); err != nil {
		return 0, undecodable(err, "reading the target of %s", it.name)
	}
	if counted.n != it.size {
		return 0, undecodable(errors.New("the target is not the size the archive records"), "reading %s", it.name)
	}
	if err := checkLink(root, it.path, target.String()); err != nil {
		return 0, unsafeEntry(it.name, err)
	}
	if err := os.MkdirAll(filepath.Dir(it.path), 0o750); err != nil {
		return 0, failed("creating the directory for "+it.name, err)
	}
	// os.Symlink refuses a path that is already there, so a link cannot land on
	// top of something another entry put down first.
	if err := os.Symlink(target.String(), it.path); err != nil {
		return 0, failed("creating the link "+it.name, err)
	}
	return counted.n, nil
}

// countWriter counts what is written through it, so the bytes that arrived can be
// held against the size the archive promised.
type countWriter struct {
	w io.Writer
	n uint64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 {
		c.n += uint64(n)
	}
	return n, err
}

// unsafeEntry reports a name or a type the destination must not be asked to hold.
func unsafeEntry(name string, reason error) error {
	return errs.Wrap(exitcode.Integrity, errs.CodeUnsafeArchive, errs.PhaseExtract, reason,
		"the entry %q cannot be written safely: %v", name, reason)
}

// overLimit reports an archive that does not fit the limits it was given.
func overLimit(format string, args ...any) error {
	return errs.New(exitcode.Integrity, errs.CodeExtractLimit, errs.PhaseExtract, format, args...)
}

// refused reports a destination the extractor will not write into.
func refused(format string, args ...any) error {
	return errs.New(exitcode.Filesystem, errs.CodeExtractFailed, errs.PhaseExtract, format, args...)
}

// failed reports a filesystem failure, naming the disk-full case as itself.
func failed(action string, cause error) error {
	code := errs.CodeExtractFailed
	if errors.Is(cause, syscall.ENOSPC) {
		code = errs.CodeDiskFull
	}
	return errs.Wrap(exitcode.Filesystem, code, errs.PhaseExtract, cause, "%s: %v", action, cause)
}

// undecodable reports an entry the decoder could not read out. Running out of the
// memory budget is the one case that is a limit rather than damage.
func undecodable(cause error, format string, args ...any) error {
	code := errs.CodeExtractDecode
	if errors.Is(cause, lzma.ErrMemory) {
		code = errs.CodeExtractLimit
	}
	return errs.Wrap(exitcode.Integrity, code, errs.PhaseExtract, cause, "%s: %v", fmt.Sprintf(format, args...), cause)
}

// cancelled reports a context that ended between entries.
func cancelled(cause error) error {
	return errs.Wrap(exitcode.Interrupted, errs.CodeInterrupted, errs.PhaseExtract, cause,
		"the extraction was cancelled: %v", cause)
}
