// Package discovery walks a Qt online repository.
//
// It composes two layers: internal/repository reads a listing or an Updates.xml
// out of bytes, and internal/transport fetches those bytes. Discovery decides
// which bytes to ask for — the directory segment a target lives under, the version
// directories it holds, and, for one version, the leaf that carries its metadata.
//
// The rules are ADR-008's. A version directory's shape is probed from its listing
// in a fixed order (flat, nested, arch-split) and never inferred from the version
// number, because the same version is nested on one host and split on another.
// Updates.xml is the authority on what a version holds; a directory name is only a
// candidate. And a shape nobody has seen is an error, not a guess.
package discovery

import (
	"context"
	"path"
	"strings"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/repository"
	"github.com/nekrozis/qtogo/internal/transport"
)

// Root is the repository path every walk starts from.
const Root = "online/qtsdkrepository"

// Fetcher is what discovery needs from the network: one small object, by
// repository-relative path. *transport.Client is one.
type Fetcher interface {
	Get(ctx context.Context, path string) (transport.Document, error)
}

// Discover walks a repository through a Fetcher. It is safe for concurrent use.
type Discover struct {
	fetch Fetcher
}

// New returns a Discover over fetch.
func New(fetch Fetcher) *Discover { return &Discover{fetch: fetch} }

// Segment returns the directory segment a host and target live under.
//
// Only the desktop targets are covered so far, and each follows its host. A
// segment is an observed fact, added with a sample, never derived from a version
// (ADR-008 decision 5); where a target has lived under more than one segment, the
// table grows a candidate list and the walk tries them in order.
func Segment(host model.Host, kind model.Kind) (string, error) {
	if kind != model.KindDesktop {
		return "", errs.New(exitcode.Usage, errs.CodeNotEnabled, errs.PhaseResolve,
			"this build does not know where the %s target lives yet", kind)
	}
	switch host {
	case model.HostWindows:
		return "windows_x86", nil
	case model.HostLinux:
		return "linux_x64", nil
	case model.HostMac:
		return "mac_x64", nil
	default:
		return "", errs.New(exitcode.Usage, errs.CodeNotEnabled, errs.PhaseResolve,
			"this build does not know where the %s host lives yet", host)
	}
}

// Version is a version directory a repository holds.
type Version struct {
	Directory repository.VersionDirectory
	Path      string // repository-relative path of the version directory
}

// Versions lists the version directories under a path — normally a target's
// directory. A child that is not a directory, or does not parse as a version
// directory, is skipped: a listing holds other things too.
func (d *Discover) Versions(ctx context.Context, dir string) ([]Version, error) {
	entries, err := d.list(ctx, dir)
	if err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		decoded, err := repository.ParseVersionDirectory(e.Name)
		if err != nil {
			continue
		}
		versions = append(versions, Version{Directory: decoded, Path: path.Join(dir, e.Name)})
	}
	if len(versions) == 0 {
		return nil, errs.New(exitcode.NotFound, errs.CodeVersionNotFound, errs.PhaseResolve,
			"%s holds no version directories", dir)
	}
	return versions, nil
}

// Leaf is a directory that carries an Updates.xml.
type Leaf struct {
	Path string // repository-relative path of the directory holding Updates.xml
	Ext  string // architecture extension, or "" when the leaf carries every architecture
}

// Leaves probes a version directory and returns the leaf, or the leaves, that
// carry its metadata. The shapes are tried in the order ADR-008 fixes: the version
// directory's own Updates.xml (flat), then a same-named child (nested), then
// children named "<version>_<extension>" (arch-split, one leaf per architecture).
func (d *Discover) Leaves(ctx context.Context, version string) ([]Leaf, error) {
	entries, err := d.list(ctx, version)
	if err != nil {
		return nil, err
	}
	token := path.Base(version)

	if holds(entries, "Updates.xml", false) {
		return []Leaf{{Path: version}}, nil
	}
	if holds(entries, token, true) {
		return []Leaf{{Path: path.Join(version, token)}}, nil
	}
	prefix := token + "_"
	leaves := make([]Leaf, 0, len(entries))
	for _, e := range entries {
		if e.IsDir && strings.HasPrefix(e.Name, prefix) {
			leaves = append(leaves, Leaf{Path: path.Join(version, e.Name), Ext: strings.TrimPrefix(e.Name, prefix)})
		}
	}
	if len(leaves) > 0 {
		return leaves, nil
	}
	return nil, errs.New(exitcode.NotFound, errs.CodeVersionNotFound, errs.PhaseResolve,
		"%s is in no layout this build recognises", version)
}

// Metadata reads a leaf's Updates.xml, the authority on what the version holds.
func (d *Discover) Metadata(ctx context.Context, leaf Leaf) ([]repository.PackageUpdate, repository.Metadata, error) {
	doc, err := d.fetch.Get(ctx, path.Join(leaf.Path, "Updates.xml"))
	if err != nil {
		return nil, repository.Metadata{}, err
	}
	return repository.ParseUpdatesXML(doc.URL, doc.Body)
}

// list fetches a directory's page and reads it as a listing.
func (d *Discover) list(ctx context.Context, dir string) ([]repository.IndexEntry, error) {
	doc, err := d.fetch.Get(ctx, dir)
	if err != nil {
		return nil, err
	}
	return repository.ParseIndex(doc.URL, doc.Body)
}

// holds reports whether entries contain a child named name of the wanted kind.
func holds(entries []repository.IndexEntry, name string, dir bool) bool {
	for _, e := range entries {
		if e.Name == name && e.IsDir == dir {
			return true
		}
	}
	return false
}
