// Package catalog turns a repository's metadata into an installation plan.
//
// It is the layer between reading a repository and doing anything with what it
// holds: given the packages an Updates.xml lists, the path of the leaf that
// document came from, and a request, it decides which packages the request names,
// which archives they carry, where each archive comes from, and where its
// contents belong.
//
// It reaches no network and no filesystem — internal/discovery supplies the
// packages and the leaf path — so a plan is built from literals and tested without
// fixtures. The rules are ADR-010: selection by package name and architecture, with
// the metadata's dependency fields deliberately not followed.
package catalog

import (
	"fmt"
	"path"
	"strings"

	"github.com/nekrozis/qtogo/internal/model"
	"github.com/nekrozis/qtogo/internal/repository"
)

// Request is what an installation plan is built for.
type Request struct {
	// Version is the version directory's version, whose Raw is the token the
	// repository addresses the version by ("680", "6110", "59"). It is the
	// directory's version, not the release the user typed: the token is the one
	// package names are spelled from.
	Version model.Version
	// Arch is the architecture, e.g. "win64_msvc2022_64". An empty one is resolved
	// from the metadata when exactly one base package exists; otherwise the plan
	// fails rather than guess.
	Arch string
	// Modules are the requested module names, in the order they were asked for.
	Modules []string
}

// Plan is a decided installation: what to fetch and where each archive lands.
type Plan struct {
	// Version is the release rendered for a person ("6.8.0"); Token is the
	// directory spelling the repository used ("680"); Arch is the resolved
	// architecture.
	Version string
	Token   string
	Arch    string
	// Packages are the selected package names: the base first, then the requested
	// modules in the order they were asked for.
	Packages []string
	// Archives are the downloads, in package order and then the order each package
	// lists them.
	Archives []Archive
}

// Archive is one downloadable file, where it comes from and where it belongs.
type Archive struct {
	Name    string `json:"name"`
	Package string `json:"package"`
	URL     string `json:"url"`
	// InstallPath is the destination inside the install root, relative and
	// slash-separated. It is empty when the archive extracts at the root, which is
	// what a package without an Extract operation asks for.
	InstallPath string `json:"installPath,omitempty"`
}

// Build decides the plan for req from the packages a leaf's Updates.xml lists.
// leafPath is the repository-relative path of that leaf, which every archive URL
// is built from.
func Build(packages []repository.PackageUpdate, leafPath string, req Request) (Plan, error) {
	if req.Version.Raw == "" {
		return Plan{}, fmt.Errorf("a plan needs the version directory's version")
	}

	req, err := resolveArch(packages, req)
	if err != nil {
		return Plan{}, err
	}

	base, err := selectBase(packages, req)
	if err != nil {
		return Plan{}, err
	}

	token := req.Version.Raw
	plan := Plan{
		Version:  req.Version.Dotted(),
		Token:    token,
		Arch:     req.Arch,
		Packages: []string{base.Name},
	}
	plan.Archives = archivesOf(base, leafPath)

	for _, module := range req.Modules {
		pkg, err := selectModule(packages, req, module)
		if err != nil {
			return Plan{}, err
		}
		plan.Packages = append(plan.Packages, pkg.Name)
		plan.Archives = append(plan.Archives, archivesOf(pkg, leafPath)...)
	}
	return plan, nil
}

// selectBase picks the base package the request names.
func selectBase(packages []repository.PackageUpdate, req Request) (repository.PackageUpdate, error) {
	candidates := baseNames(req)
	var found []repository.PackageUpdate
	for _, p := range packages {
		if contains(candidates, p.Name) {
			found = append(found, p)
		}
	}

	switch len(found) {
	case 0:
		return repository.PackageUpdate{}, notFound(
			"no base package for %s %s", req.Version.Dotted(), describeArch(req.Arch))
	case 1:
		return found[0], nil
	default:
		return repository.PackageUpdate{}, notFound(
			"more than one base package matches %s; name the architecture with <arch>", req.Version.Dotted())
	}
}

// selectModule picks the package a requested module names.
func selectModule(packages []repository.PackageUpdate, req Request, module string) (repository.PackageUpdate, error) {
	candidates := moduleNames(req, module)
	for _, p := range packages {
		if contains(candidates, p.Name) {
			return p, nil
		}
	}
	return repository.PackageUpdate{}, notFound(
		"no package for module %q of %s %s", module, req.Version.Dotted(), describeArch(req.Arch))
}

// baseNames are the two spellings a base package can take. An empty request arch
// leaves the architecture open, which is how it is inferred: matching against the
// spelled prefix tells Build which architectures exist.
func baseNames(req Request) []string {
	token := req.Version.Raw
	major := req.Version.Major

	if req.Arch == "" {
		return nil // the caller freezes the arch first; see resolveArch
	}
	return []string{
		fmt.Sprintf("qt.qt%d.%s.%s", major, token, req.Arch),
		fmt.Sprintf("qt.%s.%s", token, req.Arch),
	}
}

// moduleNames are the spellings a module can take. The list is the reference
// tool's minus the forms no captured sample shows: the Qt 6.8+ variants, and the
// bare "qt.<token>.<module>.<arch>" basic-prefix form, which is why a module the
// reference tool resolves could be refused here. A form joins the list with a
// fixture (ADR-003's rule and ADR-010's consequence).
func moduleNames(req Request, module string) []string {
	token := req.Version.Raw
	major := req.Version.Major

	return []string{
		fmt.Sprintf("qt.qt%d.%s.%s.%s", major, token, module, req.Arch),
		fmt.Sprintf("qt.qt%d.%s.addons.%s.%s", major, token, module, req.Arch),
		fmt.Sprintf("qt.%s.addons.%s.%s", token, module, req.Arch),
		fmt.Sprintf("extensions.%s.%s.%s", module, token, req.Arch),
	}
}

// resolveArch fills in an omitted architecture from the metadata, but only when
// the packages offer exactly one. Ambiguity is the caller's to resolve by naming
// one; nothing is guessed.
func resolveArch(packages []repository.PackageUpdate, req Request) (Request, error) {
	if req.Arch != "" {
		return req, nil
	}

	prefixes := []string{
		fmt.Sprintf("qt.qt%d.%s.", req.Version.Major, req.Version.Raw),
		fmt.Sprintf("qt.%s.", req.Version.Raw),
	}
	arches := map[string]bool{}
	for _, p := range packages {
		if arch, ok := archAfter(p.Name, prefixes); ok {
			arches[arch] = true
		}
	}

	switch len(arches) {
	case 0:
		return Request{}, notFound("no base package for %s", req.Version.Dotted())
	case 1:
		for arch := range arches {
			req.Arch = arch
		}
		return req, nil
	default:
		return Request{}, notFound("%s offers more than one architecture", req.Version.Dotted()).
			WithSuggestion("name one as <arch>, e.g. win64_msvc2022_64")
	}
}

// archAfter returns the architecture a package name carries after one of the
// prefixes, or ok=false when the name does not start with any of them.
func archAfter(name string, prefixes []string) (string, bool) {
	for _, prefix := range prefixes {
		if rest, ok := strings.CutPrefix(name, prefix); ok && rest != "" && !strings.Contains(rest, ".") {
			return rest, true
		}
	}
	return "", false
}

// archivesOf resolves a package's archives: the file names, their URLs and their
// destinations.
func archivesOf(pkg repository.PackageUpdate, leafPath string) []Archive {
	pairs := extractPairs(pkg)

	archives := make([]Archive, 0, len(pairs))
	for _, p := range pairs {
		archives = append(archives, Archive{
			Name:        p.archive,
			Package:     pkg.Name,
			URL:         path.Join(leafPath, pkg.Name, pkg.Version.Raw+p.archive),
			InstallPath: p.dest,
		})
	}
	return archives
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func describeArch(arch string) string {
	if arch == "" {
		return "(any architecture)"
	}
	return arch
}
