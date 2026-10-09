// Package relocate corrects an extracted Qt tree so it works from wherever it was
// installed.
//
// A Qt tree is built to run from one prefix — /home/qt/work/install on Linux,
// c:/Users/qt/work/install on Windows — and the files that record it have to be
// corrected after extraction. Which files need correcting depends on the version,
// the host and the target, so relocation is a strategy layer: a table of
// Relocators, tried in order, whose first matching Capability wins (ADR-011).
//
// The work happens on a staging tree, before anything is published, and every
// value the primitives write is relative or symbolic — never the install path. That
// is what makes two installs of the same request byte-identical wherever they land
// (the ZAP rule, ADR-011 decision 4).
package relocate

import (
	"context"

	"github.com/nekrozis/qtogo/internal/model"
)

// Capability states what a Relocator covers.
//
// The version range is half-open: Since is inclusive, Until exclusive, and a nil
// bound is unbounded on that side. That is the same convention as model.VersionRange
// and for the same reason — rules like "5.14 and newer" read as a single bound
// rather than an off-by-one.
type Capability struct {
	// Name is the policy's name, for reports and error text.
	Name string
	// Hosts and Kinds are the segments the policy addresses. A nil slice means
	// every host, or every kind.
	Hosts []model.Host
	Kinds []model.Kind
	// Since and Until bound the versions, Since inclusive and Until exclusive.
	Since *model.Version
	Until *model.Version
	// Relocatable is whether a result the policy produces is usable from any
	// path. False means the tree is anchored to where it was installed — the
	// binary-slot case — and a caller must warn and record it.
	Relocatable bool
}

// Supports reports whether the policy covers a host, kind and version.
func (c Capability) Supports(host model.Host, kind model.Kind, version model.Version) bool {
	if !containsHost(c.Hosts, host) || !containsKind(c.Kinds, kind) {
		return false
	}
	if c.Since != nil && version.Compare(*c.Since) < 0 {
		return false
	}
	if c.Until != nil && version.Compare(*c.Until) >= 0 {
		return false
	}
	return true
}

// Tree is the directory relocation runs on: the staging root, which holds the
// extracted Qt tree.
type Tree interface {
	// Root is the directory the tree lives under. Every path a policy touches is
	// below it, and a policy never writes outside it.
	Root() string
}

// Dir is a Tree that is a plain directory.
type Dir string

// Root returns the directory.
func (d Dir) Root() string { return string(d) }

// Relocator corrects one class of tree. Its Relocate must be idempotent — running
// it twice leaves the tree as one run did — cancellable, and confined to the tree.
type Relocator interface {
	// Capability states what this policy covers.
	Capability() Capability
	// Relocate corrects tree in place for target.
	Relocate(ctx context.Context, tree Tree, target model.Target) (Report, error)
	// Ready reports whether Relocate is implemented. A policy whose primitives
	// have not landed carries a Capability — so the selector can name it and
	// describe its coverage — but is not ready, and a caller checks this before
	// downloading rather than after (ADR-011 decision 2).
	Ready() bool
}

// Change is one file a policy touched, so a report can say what happened without
// the reader diffing trees.
type Change struct {
	// Path is relative to the tree root, slash-separated: a report carries no
	// absolute path (ADR-011 decision 6).
	Path string `json:"path"`
	// Action is what was done: "created", "rewrote" or "removed".
	Action string `json:"action"`
}

// Skip is a file a policy looked for and did not find, which is not a failure: a
// newer Qt tree simply does not carry every file an older one did.
type Skip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Report is what one relocation did.
type Report struct {
	// Policy is the capability name that ran.
	Policy string `json:"policy"`
	// Relocatable is whether the result works from any path.
	Relocatable bool `json:"relocatable"`
	// Changes and Skips are what the policy touched and what it looked for and did
	// not find, in the order they happened.
	Changes []Change `json:"changes,omitempty"`
	Skipped []Skip   `json:"skipped,omitempty"`
}

// containsHost reports whether hosts lists h, an empty list meaning every host.
func containsHost(hosts []model.Host, h model.Host) bool {
	if len(hosts) == 0 {
		return true
	}
	for _, x := range hosts {
		if x == h {
			return true
		}
	}
	return false
}

// containsKind reports whether kinds lists k, an empty list meaning every kind.
func containsKind(kinds []model.Kind, k model.Kind) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}
