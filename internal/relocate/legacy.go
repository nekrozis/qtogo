package relocate

import (
	"context"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
)

// LegacyQt5Relocator covers Qt older than 5.14 on the desktop, where the build
// prefix is also written into a fixed-length slot inside qmake and the Qt5 core
// library. Correcting that slot is a separate primitive that lands later (ADR-011
// decision 3); until then the policy exists only so the selector refuses the range
// through the table rather than by a special case.
type LegacyQt5Relocator struct{}

// Capability covers desktop before the floor, on every host.
func (LegacyQt5Relocator) Capability() Capability {
	return Capability{
		Name:  "legacy-qt5-desktop",
		Kinds: []model.Kind{model.KindDesktop},
		Until: &desktopFloor,
	}
}

// Relocate is not implemented in this phase. The refusal is raised by the selector
// before this is reached; reaching it means a policy was placed ahead of the
// selector's check, which is a bug rather than a supported path.
func (LegacyQt5Relocator) Relocate(context.Context, Tree, model.Target) (Report, error) {
	return Report{}, notImplemented("legacy-qt5-desktop",
		"Qt older than 5.14 needs a fixed-length slot rewritten inside qmake and the Qt5 core library")
}

// Ready reports that the primitives have not landed.
func (LegacyQt5Relocator) Ready() bool { return false }

// CrossTargetRelocator covers every non-desktop target: android, ios, winrt and
// wasm, which need extra primitives (target_qt.conf, qdevice.pri, the qmake and
// qtpaths scripts, and a cross CMake toolchain edit for some versions). Those land
// later; the policy exists now so the selector refuses these targets by the table.
type CrossTargetRelocator struct{}

// Capability covers every kind but the desktop, on every host.
func (CrossTargetRelocator) Capability() Capability {
	return Capability{
		Name:  "cross-target",
		Kinds: nonDesktopKinds(),
	}
}

// nonDesktopKinds is the kinds the cross-target policy covers: every recognised
// kind except the desktop.
func nonDesktopKinds() []model.Kind {
	kinds := model.Kinds()
	out := kinds[:0]
	for _, k := range kinds {
		if k != model.KindDesktop {
			out = append(out, k)
		}
	}
	return out
}

// Relocate is not implemented in this phase; see LegacyQt5Relocator.Relocate.
func (CrossTargetRelocator) Relocate(context.Context, Tree, model.Target) (Report, error) {
	return Report{}, notImplemented("cross-target",
		"a cross target needs the target_qt.conf, qdevice.pri and script primitives")
}

// Ready reports that the primitives have not landed.
func (CrossTargetRelocator) Ready() bool { return false }

// notImplemented is the failure a policy raises when its primitives have not
// landed. It carries the same code the selector uses, because from a caller's view
// "this build cannot relocate it yet" is one outcome.
func notImplemented(policy, why string) error {
	return errs.New(exitcode.Relocate, errs.CodeRelocateUnsupported, errs.PhaseRelocate,
		"the %s policy has no implementation yet: %s", policy, why)
}
