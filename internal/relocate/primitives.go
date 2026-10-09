package relocate

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
)

// qtConf is the file that makes a Qt tree find itself. The prefix is relative, so
// the file is the same wherever the tree lands (ADR-011 decision 4).
const qtConf = "[Paths]\nPrefix=..\n"

// writeQtConf creates bin/qt.conf. It is written rather than merely created, so a
// second run leaves the same bytes — idempotent by content, not by existence.
func writeQtConf(_ context.Context, root string, _ model.Target, report *Report) error {
	rel := filepath.Join("bin", "qt.conf")
	path := filepath.Join(root, rel)

	existing, readErr := os.ReadFile(path)
	if readErr == nil && string(existing) == qtConf {
		return nil // already correct, nothing to record
	}
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		return failed("reading "+rel, readErr)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return failed("creating "+filepath.Dir(rel), err)
	}
	if err := writeFile(path, []byte(qtConf)); err != nil {
		return failed("writing "+rel, err)
	}
	action := "created"
	if readErr == nil {
		action = "rewrote"
	}
	record(report, rel, action)
	return nil
}

// rewriteLicense sets the two lines qconfig.pri carries about the edition. An
// open-source build must not read as a commercial one, and the licence check must
// not be armed. Only the lines the file already has are changed, so a tree without
// them is left alone.
func rewriteLicense(_ context.Context, root string, _ model.Target, report *Report) error {
	rel := filepath.Join("mkspecs", "qconfig.pri")
	path := filepath.Join(root, rel)

	body, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		skipped(report, rel, "the tree does not carry it")
		return nil
	}
	if err != nil {
		return failed("reading "+rel, err)
	}

	lines := strings.SplitAfter(string(body), "\n")
	changed := false
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "QT_EDITION =") && line != "QT_EDITION = OpenSource\n":
			lines[i] = "QT_EDITION = OpenSource\n"
			changed = true
		case strings.HasPrefix(line, "QT_LICHECK =") && line != "QT_LICHECK =\n":
			lines[i] = "QT_LICHECK =\n"
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := writeFile(path, []byte(strings.Join(lines, ""))); err != nil {
		return failed("writing "+rel, err)
	}
	record(report, rel, "rewrote")
	return nil
}

// rewritePkgConfig points each .pc file at a prefix relative to the file itself.
//
// ${pcfiledir} expands to the directory holding the file, so "${pcfiledir}/../.."
// is the tree root for a file under lib/pkgconfig — a value that is the same
// wherever the tree is installed. The mac -F flag names a lib directory rather than
// the root, so it gets "${pcfiledir}/..", one level less.
func rewritePkgConfig(ctx context.Context, root string, target model.Target, report *Report) error {
	dir := filepath.Join(root, "lib", "pkgconfig")
	return eachFile(ctx, dir, "*.pc", func(path string) error {
		rel, _ := filepath.Rel(root, path)
		body, err := os.ReadFile(path)
		if err != nil {
			return failed("reading "+rel, err)
		}
		text := string(body)

		next := replacePrefixLine(text, "prefix=", treeFromPkgConfig)
		if target.Host == model.HostMac {
			next = replaceFlag(next, "-F", libFromPkgConfig)
		}
		if next == text {
			return nil
		}
		if err := writeFile(path, []byte(next)); err != nil {
			return failed("writing "+rel, err)
		}
		record(report, rel, "rewrote")
		return nil
	}, report)
}

// rewritePrl replaces the build prefix a .prl records with the qmake variable that
// stands for the same directory wherever the tree is.
//
// The sentinel is the build prefix's lib directory, not the prefix alone, because
// $$[QT_INSTALL_LIBS] is itself <prefix>/lib. Substituting the bare prefix would
// leave the path's own /lib behind and produce $$[QT_INSTALL_LIBS]/lib.
func rewritePrl(ctx context.Context, root string, _ model.Target, report *Report) error {
	dir := filepath.Join(root, "lib")
	return eachFile(ctx, dir, "*.prl", func(path string) error {
		rel, _ := filepath.Rel(root, path)
		body, err := os.ReadFile(path)
		if err != nil {
			return failed("reading "+rel, err)
		}
		text := string(body)

		next := replaceBuildPrefix(text)
		if next == text {
			return nil
		}
		if err := writeFile(path, []byte(next)); err != nil {
			return failed("writing "+rel, err)
		}
		record(report, rel, "rewrote")
		return nil
	}, report)
}

// removeLibtool deletes every .la file.
//
// Its only anchor is an absolute libdir, which the byte-identical rule forbids, and
// nothing in a Qt build reads it. Deleting it is the deliberate departure from the
// reference tool that rewrites it in place (ADR-011 decision 4).
func removeLibtool(ctx context.Context, root string, _ model.Target, report *Report) error {
	dir := filepath.Join(root, "lib")
	return eachFile(ctx, dir, "*.la", func(path string) error {
		rel, _ := filepath.Rel(root, path)
		if err := os.Remove(path); err != nil {
			return failed("removing "+rel, err)
		}
		record(report, rel, "removed")
		return nil
	}, report)
}

// eachFile runs fn for every file in dir matching pattern. A directory that is not
// there is a skip, not a failure: a tree that does not carry the file has nothing
// to correct. The context is checked between files, so cancelling a walk over a
// large lib directory stops it rather than waiting for the whole list.
func eachFile(ctx context.Context, dir, pattern string, fn func(string) error, report *Report) error {
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return failed("listing "+dir, err)
	}
	if len(matches) == 0 {
		skipped(report, filepath.Dir(filepath.Join(dir, pattern)), "no matching files")
		return nil
	}
	for _, m := range matches {
		if err := ctx.Err(); err != nil {
			return cancelled(err)
		}
		if err := fn(m); err != nil {
			return err
		}
	}
	return nil
}

// replacePrefixLine rewrites a line that starts with key, keeping the key and
// replacing the value. Only a line already present is changed.
func replacePrefixLine(text, key, value string) string {
	lines := strings.SplitAfter(text, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, key) {
			lines[i] = key + value + "\n"
		}
	}
	return strings.Join(lines, "")
}

// replaceFlag rewrites every occurrence of a flag and the path that follows it to
// value, for the mac-specific -F form. An "=" between the flag and the path is
// kept, since it is part of the spelling.
//
// A flag with nothing after it, or with only whitespace, is left alone: there is
// no path to replace.
func replaceFlag(text, flag, value string) string {
	var b strings.Builder
	for {
		i := strings.Index(text, flag)
		if i < 0 {
			b.WriteString(text)
			return b.String()
		}
		b.WriteString(text[:i+len(flag)])
		rest := text[i+len(flag):]
		if strings.HasPrefix(rest, "=") {
			b.WriteByte('=')
			rest = rest[1:]
		}

		path, tail := cutPath(rest)
		if path == "" {
			// Nothing to replace: keep what follows and carry on.
			text = rest
			continue
		}
		b.WriteString(value)
		text = tail
	}
}

// cutPath splits a leading path token from what follows it. The path ends at the
// first character that cannot be part of an unquoted one; everything up to and
// including the delimiter stays in the tail.
func cutPath(s string) (path, tail string) {
	i := 0
	for i < len(s) && !strings.ContainsRune("\n\t ;", rune(s[i])) {
		i++
	}
	return s[:i], s[i:]
}

// treeFromPkgConfig is the tree root, expressed from inside lib/pkgconfig: two
// levels up from the file.
const treeFromPkgConfig = "${pcfiledir}/../.."

// libFromPkgConfig is the tree's lib directory, one level up — which is what the
// mac -F flag names.
const libFromPkgConfig = "${pcfiledir}/.."

// qtInstallLibs is the qmake variable that resolves, through qt.conf, to the tree's
// own lib directory — which is why the sentinel it replaces includes /lib.
const qtInstallLibs = "$$[QT_INSTALL_LIBS]"

// buildLibDirs are the lib directories Qt was built under, as a .prl records them.
// Each is replaced by qtInstallLibs, which stands for the same directory wherever
// the tree lands.
//
// Order matters: the macOS path is a substring of the Windows one, so the Windows
// forms are replaced first. Replacing the macOS path first would leave the drive
// letter behind as "c:".
//
// The spellings are the ones a real .prl carries: lowercase "c:/" — an uppercase
// "C:/" was searched for and never found — and the Windows form uses backslashes
// after the drive, so both separators appear.
var buildLibDirs = []string{
	`c:\Users\qt\work\install\lib`,
	"c:/Users/qt/work/install/lib",
	"/home/qt/work/install/lib",
	"/Users/qt/work/install/lib",
}

// replaceBuildPrefix rewrites the Qt build prefix wherever a file records it, which
// is the lib directory a .prl names.
func replaceBuildPrefix(text string) string {
	for _, old := range buildLibDirs {
		text = strings.ReplaceAll(text, old, qtInstallLibs)
	}
	return text
}

// writeFile replaces a file's contents, keeping a mode that a Qt build can read.
//
// Written at 0o600 and widened to 0o644, so the file is never briefly readable
// while it is half-written and still ends up a normal build input.
func writeFile(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}

// failed reports a filesystem failure during relocation. It is a filesystem
// outcome (exit 6), not "this target is unsupported": a permission or disk error
// is not a statement about the version, and conflating the two would send a script
// down the wrong path.
func failed(action string, cause error) error {
	return errs.Wrap(exitcode.Filesystem, errs.CodeRelocateFailed, errs.PhaseRelocate, cause,
		"relocation failed: %s: %v", action, cause)
}

// cancelled reports a context that ended during relocation.
func cancelled(cause error) error {
	return errs.Wrap(exitcode.Interrupted, errs.CodeInterrupted, errs.PhaseRelocate, cause,
		"the relocation was cancelled: %v", cause)
}
