package relocate

import (
	"context"
	"path/filepath"

	"github.com/nekrozis/qtogo/internal/model"
)

// ModernDesktopRelocator covers Qt 5.14 and newer on the desktop, which relocates
// from text alone: a qt.conf naming a relative prefix, and the downstream build
// files corrected where they carry the build prefix. No binary is touched, because
// measured, these versions need no binary change (ADR-011 decision 4).
type ModernDesktopRelocator struct{}

// Capability covers desktop from the floor onward, on every host.
func (*ModernDesktopRelocator) Capability() Capability {
	return Capability{
		Name:        "modern-desktop",
		Kinds:       []model.Kind{model.KindDesktop},
		Since:       &desktopFloor,
		Relocatable: true,
	}
}

// Ready reports that the primitives are here.
func (*ModernDesktopRelocator) Ready() bool { return true }

// Relocate corrects the tree in place.
//
// The order is fixed so a report reads the same way every time, and every step is
// idempotent and skips a file it does not find: a newer tree that no longer emits
// one just results in fewer changes, never a failure.
func (r *ModernDesktopRelocator) Relocate(ctx context.Context, tree Tree, target model.Target) (Report, error) {
	root := tree.Root()
	report := Report{Policy: r.Capability().Name, Relocatable: true}

	steps := []func(context.Context, string, model.Target, *Report) error{
		writeQtConf,
		rewriteLicense,
		rewritePkgConfig,
		rewritePrl,
		removeLibtool,
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return report, cancelled(ctx.Err())
		}
		if err := step(ctx, root, target, &report); err != nil {
			return report, err
		}
	}
	return report, nil
}

// record notes a file that was touched.
func record(report *Report, rel, action string) {
	report.Changes = append(report.Changes, Change{Path: filepath.ToSlash(rel), Action: action})
}

// skipped notes a file that was looked for and not found.
func skipped(report *Report, rel, reason string) {
	report.Skipped = append(report.Skipped, Skip{Path: filepath.ToSlash(rel), Reason: reason})
}
