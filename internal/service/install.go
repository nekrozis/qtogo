package service

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"

	"github.com/nekrozis/qtogo/internal/buildinfo"
	"github.com/nekrozis/qtogo/internal/catalog"
	"github.com/nekrozis/qtogo/internal/discovery"
	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/extract"
	"github.com/nekrozis/qtogo/internal/filesystem"
	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/relocate"
	"github.com/nekrozis/qtogo/internal/sevenzip"
	"github.com/nekrozis/qtogo/internal/transport"
)

// Defaults bound one installation. They are deliberately generous — a Qt package
// is large — but finite: an archive that cannot be extracted within them is
// refused rather than allowed to exhaust the disk or the process.
const (
	// DefaultMemoryBudget is the most the decoder may hold at once, for one solid
	// block. Qt's archives are solid, so this is the working-set ceiling.
	//
	// It has to clear the largest block a real archive carries: a Qt 6.8.0 desktop
	// package (qtdeclarative) holds one of about 1.5 GiB, so a smaller default
	// would refuse a flagship install before decoding anything. Four gigabytes
	// clears the real maximum with room for a larger one, and remains a bound
	// rather than "no limit". --memory-budget raises or lowers it.
	DefaultMemoryBudget = 4 << 30 // 4 GiB
	// DefaultMaxEntries, DefaultMaxBytes and DefaultMaxEntry bound one archive.
	DefaultMaxEntries = 1_000_000
	DefaultMaxBytes   = 32 << 30 // 32 GiB
	DefaultMaxEntry   = 8 << 30  // 8 GiB
)

// InstallOptions are the choices an install makes beyond the request itself.
type InstallOptions struct {
	// OutputDir is the directory the tree is published into. Empty means the
	// working directory, matching the reference tool.
	OutputDir string
	// Overwrite removes an existing destination first. Without it an existing one
	// is a failure.
	Overwrite bool
	// DryRun stops after planning: nothing is downloaded, extracted or published.
	DryRun bool
	// MemoryBudget overrides DefaultMemoryBudget when non-zero.
	MemoryBudget uint64
}

// Installed is the outcome of an installation that ran.
type Installed struct {
	// Plan is what was decided, the same document plan install-qt shows.
	Plan catalog.Plan `json:"plan"`
	// Path is the directory the tree was published into.
	Path string `json:"path"`
	// Policy is the relocation policy that ran.
	Policy string `json:"policy"`
	// Relocatable is whether the tree works from any path.
	Relocatable bool `json:"relocatable"`
	// Replaced is whether an existing destination was overwritten.
	Replaced bool `json:"replaced,omitempty"`
}

// InstallQt builds an installation plan and, unless DryRun is set, carries it out:
// download each archive, verify it, extract it where it belongs, relocate the
// staged tree, and publish it with a manifest (ADR-011).
//
// The relocation capability check happens before the first download, so a target
// this build cannot relocate fails in a second rather than after gigabytes have
// been fetched.
func (s *Service) InstallQt(ctx context.Context, host model.Host, kind model.Kind,
	version model.Version, arch string, modules []string, opts InstallOptions) (Installed, error) {

	plan, target, err := s.planFor(ctx, host, kind, version, arch, modules)
	if err != nil {
		return Installed{}, err
	}

	// The capability check comes before any download: a version this build cannot
	// relocate must not cost a gigabyte to discover (ADR-011 decision 2).
	if err := s.relocator.Check(target); err != nil {
		return Installed{}, err
	}
	if opts.DryRun {
		return Installed{Plan: plan}, nil
	}

	// --outputdir is the base directory and may already exist — a user points at
	// their Qt directory. The tree lives inside it at the path the archives carry,
	// so it is the tree, not the base, that must be new or explicitly replaced.
	base := opts.OutputDir
	if base == "" {
		base, err = os.Getwd()
		if err != nil {
			return Installed{}, fsFailed("reading the working directory", err)
		}
	}
	if err := os.MkdirAll(base, 0o750); err != nil {
		return Installed{}, fsFailed("creating "+base, err)
	}
	stage, err := filesystem.NewStage(filepath.Join(base, ".staging"))
	if err != nil {
		return Installed{}, err
	}
	// Every failure below discards the staging tree, so a partial install never
	// survives as a directory that looks finished.
	defer func() { _ = stage.Discard() }()

	// Downloads and per-archive extraction both happen under the staging tree, on
	// the destination's own volume: the extraction merge is a rename, and a rename
	// across volumes fails. They are discarded with the staging tree, so an
	// install that fails leaves nothing behind — and the archives are not kept,
	// which is the reference tool's behaviour without --keep.
	work, scratch := stage.Path("archives"), stage.Path("scratch")
	for _, dir := range []string{work, scratch} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return Installed{}, fsFailed("creating "+dir, err)
		}
	}

	records, err := s.fetchAndExtract(ctx, plan, work, scratch, stage.Dir(), opts.MemoryBudget)
	if err != nil {
		return Installed{}, err
	}

	if err := ctx.Err(); err != nil {
		return Installed{}, cancelledInstall(err)
	}

	// The tree is inside the staging directory, at the version/arch path the
	// archives established; it is corrected and given its manifest there, before
	// anything is published.
	tree, err := findTree(stage.Dir())
	if err != nil {
		return Installed{}, err
	}
	treeRel, err := filepath.Rel(stage.Dir(), tree)
	if err != nil {
		return Installed{}, fsFailed("locating the tree", err)
	}
	report, err := s.relocator.Relocate(ctx, filesystem.Dir(tree), target)
	if err != nil {
		return Installed{}, err
	}

	manifest, err := marshalManifest(plan, target, report, records)
	if err != nil {
		return Installed{}, err
	}
	if err := filesystem.WriteManifest(tree, manifest); err != nil {
		return Installed{}, err
	}

	// Only the tree is published, and it lands at the same relative path inside the
	// base directory; the staging directory around it is discarded. The tree is
	// the unit the reference tool refuses to overwrite, and the one --overwrite
	// replaces.
	dest := filepath.Join(base, treeRel)
	published, err := filesystem.PublishTree(tree, dest, opts.Overwrite)
	if err != nil {
		return Installed{}, err
	}

	return Installed{
		Plan:        plan,
		Path:        published.Path,
		Policy:      report.Policy,
		Relocatable: report.Relocatable,
		Replaced:    published.Replaced,
	}, nil
}

// versionFromPlan re-parses the plan's rendered release, so a target built from a
// plan carries the version a person reads rather than the directory token.
func versionFromPlan(plan catalog.Plan) (model.Version, error) {
	release, err := model.ParseVersion(plan.Version)
	if err != nil {
		return model.Version{}, errs.Wrap(exitcode.Internal, errs.CodeUnclassified, errs.PhaseResolve, err,
			"the plan's version %q is not a version", plan.Version)
	}
	return release, nil
}

// planFor walks the repository and builds the plan, returning the relocation
// target alongside it.
func (s *Service) planFor(ctx context.Context, host model.Host, kind model.Kind,
	version model.Version, arch string, modules []string) (catalog.Plan, model.Target, error) {

	segment, err := discovery.Segment(host, kind)
	if err != nil {
		return catalog.Plan{}, model.Target{}, err
	}
	target := path.Join(discovery.Root, segment, string(kind))

	found, err := s.discover.Versions(ctx, target)
	if err != nil {
		return catalog.Plan{}, model.Target{}, err
	}
	directory, err := matchVersion(found, version)
	if err != nil {
		return catalog.Plan{}, model.Target{}, err
	}

	leaves, err := s.discover.Leaves(ctx, directory.Path)
	if err != nil {
		return catalog.Plan{}, model.Target{}, err
	}
	if arch == "" && len(leaves) > 1 {
		return catalog.Plan{}, model.Target{}, errs.New(exitcode.NotFound, errs.CodePackageNotFound,
			errs.PhaseResolve, "%s has more than one architecture; name one with <arch>",
			directory.Directory.Version.Dotted())
	}

	for _, leaf := range leaves {
		packages, _, err := s.discover.Metadata(ctx, leaf)
		if err != nil {
			return catalog.Plan{}, model.Target{}, err
		}
		plan, err := catalog.Build(packages, leaf.Path, catalog.Request{
			Version: directory.Directory.Version,
			Arch:    arch,
			Modules: modules,
		})
		if err == nil {
			// The target carries the release, not the directory token: the token
			// ("5120") is how the repository spells it, and a message that names
			// the version has to read the way a person asked for it ("5.12.0").
			release, err := versionFromPlan(plan)
			if err != nil {
				return catalog.Plan{}, model.Target{}, err
			}
			return plan, model.Target{
				Host: host, Kind: kind,
				Version: release,
				Arch:    plan.Arch,
			}, nil
		}
		// Keep the first leaf's failure and try the next; a split layout's leaves
		// only answer for their own architecture.
		if leaf == leaves[len(leaves)-1] {
			return catalog.Plan{}, model.Target{}, err
		}
	}
	return catalog.Plan{}, model.Target{}, errs.New(exitcode.NotFound, errs.CodePackageNotFound,
		errs.PhaseResolve, "%s has no metadata this build can read", directory.Path)
}

// fetchAndExtract downloads every archive the plan names and extracts it to where
// its install path says, under root. It returns one record per archive.
//
// work holds the downloaded archives and scratch holds each archive's extraction
// directory; both live under the staging tree, so no cross-volume move is ever
// needed — see extractArchive. budget is the request's --memory-budget, or zero
// for the default.
func (s *Service) fetchAndExtract(ctx context.Context, plan catalog.Plan, work, scratch, root string, budget uint64) ([]filesystem.Record, error) {
	if budget == 0 {
		budget = s.memoryBudget
	}
	if budget == 0 {
		budget = DefaultMemoryBudget
	}
	limits := extract.Limits{
		MaxEntries: DefaultMaxEntries,
		MaxBytes:   DefaultMaxBytes,
		MaxEntry:   DefaultMaxEntry,
		MaxBlock:   budget,
	}

	records := make([]filesystem.Record, 0, len(plan.Archives))
	for _, archive := range plan.Archives {
		if err := ctx.Err(); err != nil {
			return nil, cancelledInstall(err)
		}
		got, err := s.fetch.Download(ctx, archive.URL, work)
		if err != nil {
			return nil, err
		}
		if err := extractArchive(ctx, got.Path, filepath.Join(root, filepath.FromSlash(archive.InstallPath)), scratch, limits); err != nil {
			return nil, err
		}
		records = append(records, filesystem.Record{
			Name:        archive.Name,
			Package:     archive.Package,
			Digest:      got.Digest,
			InstallPath: archive.InstallPath,
		})
	}
	return records, nil
}

// extractArchive opens one archive and merges its contents into dest.
//
// The extractor insists on an empty destination — that is what makes a name it
// refuses unable to reach anything already there (ADR-005). But archives share a
// destination: a package records one archive per file group, and the archives of a
// Qt tree nest <version>/<arch>/ and land in the same place. So each archive is
// extracted into a directory of its own and its entries are then moved into place,
// rather than handing the shared destination to the extractor.
//
// That working directory is created under scratch — which lives on the staging
// tree's volume — because merging is a rename, and a rename only works within one
// volume. On the OS temp directory it would fail with EXDEV wherever the two
// volumes differ, which on Windows is any install onto a second drive.
func extractArchive(ctx context.Context, path, dest, scratch string, limits extract.Limits) error {
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return fsFailed("creating "+dest, err)
	}
	staging, err := os.MkdirTemp(scratch, "qtogo-extract-")
	if err != nil {
		return fsFailed("creating an extraction directory", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	a, err := sevenzip.Open(path, limits.MaxBlock)
	if err != nil {
		return errs.Wrap(exitcode.Integrity, errs.CodeExtractFailed, errs.PhaseExtract, err,
			"opening %s: %v", filepath.Base(path), err)
	}
	defer func() { _ = a.Close() }()

	if _, err := extract.All(ctx, a, staging, limits); err != nil {
		return err
	}
	return mergeTrees(staging, dest)
}

// mergeTrees moves everything under from into dest, keeping whatever is already
// there.
//
// A name that is already present in dest is the archive naming a file twice, or
// two archives claiming the same path; either way the later one is refused rather
// than silently overwriting, which is the same "an entry never lands on another"
// rule the extractor applies within one archive.
func mergeTrees(from, dest string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return fsFailed("reading the extracted tree", err)
	}
	for _, e := range entries {
		src := filepath.Join(from, e.Name())
		dst := filepath.Join(dest, e.Name())
		if err := mergeEntry(src, dst); err != nil {
			return err
		}
	}
	return nil
}

// mergeEntry moves one top-level entry, descending into directories so a merge
// into a directory already holding files from another archive works.
func mergeEntry(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fsFailed("reading "+src, err)
	}
	if !info.IsDir() {
		if _, err := os.Lstat(dst); err == nil {
			return refused("the archive names %s, which an earlier archive already put there", dst)
		}
		if err := os.Rename(src, dst); err != nil {
			return fsFailed("moving "+src+" into place", err)
		}
		return nil
	}

	if existing, err := os.Lstat(dst); err == nil && !existing.IsDir() {
		return refused("%s is a file, and the archive has a directory there", dst)
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return fsFailed("creating "+dst, err)
	}
	children, err := os.ReadDir(src)
	if err != nil {
		return fsFailed("reading "+src, err)
	}
	for _, child := range children {
		if err := mergeEntry(filepath.Join(src, child.Name()), filepath.Join(dst, child.Name())); err != nil {
			return err
		}
	}
	return os.Remove(src)
}

// marshalManifest builds the document an installed tree carries. Every path in it
// is relative, so two installs of the same request are byte-identical wherever
// they land (ADR-011 decision 6).
func marshalManifest(plan catalog.Plan, target model.Target,
	report relocate.Report, records []filesystem.Record) ([]byte, error) {

	m := filesystem.Manifest{
		Program:     buildinfo.ProgramName,
		Version:     buildinfo.Version,
		Policy:      report.Policy,
		Relocatable: report.Relocatable,
		Archives:    records,
		Request: filesystem.Request{
			Host:    string(target.Host),
			Target:  string(target.Kind),
			Version: plan.Version,
			Arch:    plan.Arch,
		},
	}
	// The modules a plan selected, minus the base, are what the request asked for
	// beyond Qt itself.
	if len(plan.Packages) > 1 {
		m.Request.Modules = plan.Packages[1:]
	}

	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, errs.Wrap(exitcode.Internal, errs.CodeUnclassified, errs.PhasePublish, err,
			"building the manifest")
	}
	return append(body, '\n'), nil
}

// findTree locates the Qt tree under the staging directory.
//
// The destination of an install is the tree itself, wherever the archives put it:
// a Qt 5 archive nests <version>/<arch>/ inside itself, while a Qt 6 archive's
// Extract operation names that path. So the tree is found rather than assumed, by
// looking for the marker every Qt tree carries — qmake in bin — with a bounded
// walk from the root.
func findTree(root string) (string, error) {
	found, err := walkForQmake(root, 0)
	if err != nil {
		return "", err
	}
	if found != "" {
		return found, nil
	}
	return "", errs.New(exitcode.Filesystem, errs.CodeExtractFailed, errs.PhaseExtract,
		"no Qt tree was found under %s: the archives did not carry a bin/qmake", root)
}

// treeDepth bounds the search: version and architecture are at most two levels
// below the base directory.
const treeDepth = 3

// walkForQmake descends from dir looking for a directory holding bin/qmake.
func walkForQmake(dir string, depth int) (string, error) {
	for _, exe := range []string{"qmake", "qmake.exe"} {
		if info, err := os.Stat(filepath.Join(dir, "bin", exe)); err == nil && !info.IsDir() {
			return dir, nil
		}
	}
	if depth >= treeDepth {
		return "", nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fsFailed("reading "+dir, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		found, err := walkForQmake(filepath.Join(dir, e.Name()), depth+1)
		if err != nil {
			return "", err
		}
		if found != "" {
			return found, nil
		}
	}
	return "", nil
}

// fsFailed reports a filesystem failure during an install.
func fsFailed(action string, cause error) error {
	return errs.Wrap(exitcode.Filesystem, errs.CodeFilesystem, errs.PhasePublish, cause,
		"%s: %v", action, cause)
}

// refused reports an archive whose entries collide with what is already extracted.
func refused(format string, args ...any) error {
	return errs.New(exitcode.Integrity, errs.CodeUnsafeArchive, errs.PhaseExtract, format, args...)
}

// cancelledInstall reports a context that ended during an install.
func cancelledInstall(cause error) error {
	return errs.Wrap(exitcode.Interrupted, errs.CodeInterrupted, errs.PhaseRun, cause,
		"the installation was cancelled: %v", cause)
}

// downloader is the part of the transport a Service installs through, so a test
// can substitute one.
type downloader interface {
	Download(ctx context.Context, remote, destDir string) (transport.Downloaded, error)
}
