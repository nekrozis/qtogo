// Package service is the layer between the command front end and the repository
// layers.
//
// A command asks a service for a result; the service knows which of repository,
// discovery and transport to use and in what order, so a command reads as what it
// shows rather than as a walk over the network. The front end never reaches past
// this seam.
package service

import (
	"context"
	"path"
	"slices"

	"github.com/nekrozis/qtogo/internal/catalog"
	"github.com/nekrozis/qtogo/internal/discovery"
	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/relocate"
)

// Service drives the repository layers for the commands.
type Service struct {
	discover *discovery.Discover
	// fetch downloads archives for an install. A listing-only Service never uses
	// it, which is why it may be nil.
	fetch downloader
	// relocator selects the policy that corrects an extracted tree.
	relocator *relocate.Selector
	// memoryBudget bounds the decoder during extraction; zero means the default.
	memoryBudget uint64
}

// New returns a Service that reads a repository through fetch, and relocates an
// installed tree with the built-in policies.
func New(fetch discovery.Fetcher) *Service {
	return &Service{discover: discovery.New(fetch), relocator: relocate.NewSelector()}
}

// WithDownloader returns a Service that can also install: it downloads archives
// through d. Without it, InstallQt fails rather than panicking.
func (s *Service) WithDownloader(d downloader) *Service {
	s.fetch = d
	return s
}

// WithMemoryBudget bounds the decoder during extraction, for a caller that wants a
// different ceiling than DefaultMemoryBudget.
func (s *Service) WithMemoryBudget(bytes uint64) *Service {
	s.memoryBudget = bytes
	return s
}

// ListQtVersions returns the Qt versions a host and target offer, oldest first and
// without duplicates.
func (s *Service) ListQtVersions(ctx context.Context, host model.Host, kind model.Kind) ([]model.Version, error) {
	segment, err := discovery.Segment(host, kind)
	if err != nil {
		return nil, err
	}
	found, err := s.discover.Versions(ctx, path.Join(discovery.Root, segment, string(kind)))
	if err != nil {
		return nil, err
	}
	versions := make([]model.Version, 0, len(found))
	for _, v := range found {
		versions = append(versions, v.Directory.Version)
	}
	return order(versions), nil
}

// order sorts versions oldest first and drops one that two directories spell the
// same way, so a target that offers a version under two names lists it once.
func order(versions []model.Version) []model.Version {
	slices.SortFunc(versions, model.Version.Compare)
	return slices.CompactFunc(versions, func(a, b model.Version) bool { return a.Compare(b) == 0 })
}

// PlanInstallQt builds the installation plan for a version, an architecture and a
// set of modules.
//
// It walks the repository the way listing does — the segment, the version
// directories, the leaf — and then hands the leaf's metadata to the catalog. A
// version is matched by its numbers, so a request names a release and not the
// directory token it happens to be spelled with. When a version has several
// leaves (an architecture-split layout) and no architecture was given, the plan
// fails rather than pick one: the choice is the caller's to make.
func (s *Service) PlanInstallQt(ctx context.Context, host model.Host, kind model.Kind,
	version model.Version, arch string, modules []string) (catalog.Plan, error) {

	segment, err := discovery.Segment(host, kind)
	if err != nil {
		return catalog.Plan{}, err
	}
	target := path.Join(discovery.Root, segment, string(kind))

	found, err := s.discover.Versions(ctx, target)
	if err != nil {
		return catalog.Plan{}, err
	}
	directory, err := matchVersion(found, version)
	if err != nil {
		return catalog.Plan{}, err
	}

	leaves, err := s.discover.Leaves(ctx, directory.Path)
	if err != nil {
		return catalog.Plan{}, err
	}
	if arch == "" && len(leaves) > 1 {
		return catalog.Plan{}, errs.New(exitcode.NotFound, errs.CodePackageNotFound, errs.PhaseResolve,
			"%s has more than one architecture; name one with <arch>", directory.Directory.Version.Dotted())
	}

	// A version directory can carry several leaves, so the plan is the first leaf
	// whose metadata answers the request; a leaf without the arch is skipped.
	var firstErr error
	for _, leaf := range leaves {
		packages, _, err := s.discover.Metadata(ctx, leaf)
		if err != nil {
			return catalog.Plan{}, err
		}
		plan, err := catalog.Build(packages, leaf.Path, catalog.Request{
			Version: directory.Directory.Version,
			Arch:    arch,
			Modules: modules,
		})
		if err == nil {
			return plan, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return catalog.Plan{}, firstErr
}

// matchVersion finds the version directory a request names, comparing the numbers
// so "6.8.0" matches the directory spelled "qt6_680".
//
// A version can be spelled by more than one directory: the desktop one carries no
// extension, while a sibling like "qt5_5152_wasm" or "qt5_5152_src_doc_examples"
// decodes to the same numbers with an extension set. The plain directory is the
// desktop one a request without a suffix means, so it wins over those; a request
// that does carry a suffix matches the directory with that same suffix.
func matchVersion(found []discovery.Version, want model.Version) (discovery.Version, error) {
	var plain, suffixed *discovery.Version
	for i := range found {
		v := found[i].Directory.Version
		if v.Major != want.Major || v.Minor != want.Minor || v.Patch != want.Patch {
			continue
		}
		ext := found[i].Directory.Extension
		switch {
		case want.Suffix != "" && v.Suffix == want.Suffix && ext == want.Suffix:
			return found[i], nil
		case ext == "" && v.Suffix == want.Suffix:
			// The desktop directory for this release, and the request named no
			// other spelling: this is the answer.
			plain = &found[i]
		case ext == "" && plain == nil:
			plain = &found[i]
		case ext != "" && suffixed == nil:
			suffixed = &found[i]
		}
	}
	switch {
	case plain != nil:
		return *plain, nil
	case suffixed != nil:
		return *suffixed, nil
	}
	return discovery.Version{}, errs.New(exitcode.NotFound, errs.CodeVersionNotFound, errs.PhaseResolve,
		"the repository does not offer %s", want.Dotted())
}
