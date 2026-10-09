// Package filesystem places a finished tree at its destination.
//
// An installation is built under a staging directory beside the destination, on
// the same volume, and moved into place with one rename once it is complete. The
// destination therefore appears either absent or whole, never half-written, and a
// failure at any point leaves the staging directory removed and the destination
// untouched (ADR-011 decision 5).
//
// The destination is refused by default when it already exists: overwriting is the
// destructive choice and has to be asked for. That is a deliberate departure from
// the reference tool, which unpacks over an existing directory.
package filesystem

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
)

// Result is what publishing produced.
type Result struct {
	// Path is the published directory.
	Path string `json:"path"`
	// Replaced is whether an existing destination was removed first.
	Replaced bool `json:"replaced,omitempty"`
}

// PublishTree moves a finished tree to dest inside an already-existing base
// directory.
//
// Unlike Stage, the base directory may already be there — a user points at their
// Qt directory — so only the tree itself is the unit that must be new, or be
// replaced. dest's parent is created if it is not there, and an existing dest is
// refused unless overwrite is set, since replacing a tree is the destructive
// choice a caller has to ask for (ADR-011 decision 5).
func PublishTree(tree, dest string, overwrite bool) (Result, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return Result{}, failed("resolving the destination", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o750); err != nil {
		return Result{}, failed("creating the destination's parent", err)
	}

	replaced := false
	switch _, statErr := os.Lstat(abs); {
	case statErr == nil && !overwrite:
		return Result{}, exists(abs)
	case statErr == nil:
		if err := os.RemoveAll(abs); err != nil {
			return Result{}, failed("removing the existing "+abs, err)
		}
		replaced = true
	case !errors.Is(statErr, fs.ErrNotExist):
		return Result{}, failed("reading the destination", statErr)
	}

	if err := os.Rename(tree, abs); err != nil {
		return Result{}, failed("moving the tree into place", err)
	}
	return Result{Path: abs, Replaced: replaced}, nil
}

// Dir is a Tree that is a plain directory: the staging tree's own root, or a tree
// found inside it, is what relocation runs on.
type Dir string

// Root returns the directory.
func (d Dir) Root() string { return string(d) }

// Stage is a working tree that becomes the destination or is discarded.
//
// It is created beside the destination — so the final rename stays on one volume,
// which is what makes it atomic — and its name is hidden and unique so a
// concurrent install cannot collide with it.
type Stage struct {
	dir string
}

// NewStage creates a staging directory beside dest.
//
// The destination's parent is created if it is not there — an install into a fresh
// output directory is the ordinary case — but the destination itself is not: the
// staging tree is what becomes it, and a destination that already exists is the
// thing Commit refuses.
func NewStage(dest string) (*Stage, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return nil, failed("resolving the destination", err)
	}
	parent := filepath.Dir(abs)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return nil, failed("creating the destination's parent "+parent, err)
	}

	dir, err := os.MkdirTemp(parent, "."+filepath.Base(abs)+".staging-")
	if err != nil {
		return nil, failed("creating a staging directory", err)
	}
	return &Stage{dir: dir}, nil
}

// Dir is the staging directory: extraction and relocation happen under it.
func (s *Stage) Dir() string { return s.dir }

// Root reads the staging directory's path, so a Stage is a relocate.Tree.
func (s *Stage) Root() string { return s.dir }

// Discard removes the staging directory. It is what a failure path calls so a
// partial tree never survives.
func (s *Stage) Discard() error {
	if s.dir == "" {
		return nil
	}
	dir := s.dir
	s.dir = ""
	if err := os.RemoveAll(dir); err != nil {
		return failed("discarding the staging directory", err)
	}
	return nil
}

// Manifest is the document an installed tree carries.
//
// It holds relative paths only, so two installs of the same request produce the
// same bytes wherever they land (ADR-011 decision 6). It is the diagnostic and
// uninstall basis.
type Manifest struct {
	Program     string   `json:"program"`
	Version     string   `json:"version"`
	Request     Request  `json:"request"`
	Policy      string   `json:"policy"`
	Relocatable bool     `json:"relocatable"`
	Archives    []Record `json:"archives"`
}

// Request is what was asked for, recorded so the manifest says what the tree is.
type Request struct {
	Host    string   `json:"host"`
	Target  string   `json:"target"`
	Version string   `json:"version"`
	Arch    string   `json:"arch"`
	Modules []string `json:"modules,omitempty"`
}

// Record is one archive that was fetched, with the digest it was verified against.
type Record struct {
	// Name is the archive's file name.
	Name string `json:"name"`
	// Package is the repository package it belonged to.
	Package string `json:"package"`
	// Digest is the digest transport verified the bytes against.
	Digest string `json:"digest"`
	// InstallPath is where its contents were placed, relative to the tree root.
	InstallPath string `json:"installPath,omitempty"`
}

// ManifestName is the file an installed tree carries its manifest under.
const ManifestName = "qtogo-manifest.json"

// WriteManifest writes m into the directory a tree was published at.
//
// It is written after the rename, so it describes a tree that is already in place;
// the manifest is a record of an installation, not part of the atomic unit.
func WriteManifest(dest string, body []byte) error {
	if err := os.WriteFile(filepath.Join(dest, ManifestName), body, 0o600); err != nil {
		return failed("writing the manifest", err)
	}
	return nil
}

// exists reports a destination that is already there.
func exists(dest string) error {
	return errs.New(exitcode.Filesystem, errs.CodeFilesystem, errs.PhasePublish,
		"the destination %s already exists", dest).
		WithSuggestion("pass --overwrite to replace it")
}

// failed reports a filesystem failure.
func failed(action string, cause error) error {
	return errs.Wrap(exitcode.Filesystem, errs.CodeFilesystem, errs.PhasePublish, cause,
		"%s: %v", action, cause)
}
