package relocate

import (
	"context"
	"fmt"
	"strings"

	"github.com/nekrozis/qtogo/internal/errs"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/model"
)

// desktopFloor is the version at which Qt relocates from bin/qt.conf alone, with
// no binary patch. Below it a fixed-length slot inside qmake and the Qt5 core
// library carries the build prefix, and correcting that is a separate primitive
// (ADR-011 decision 3).
var desktopFloor = model.Version{Major: 5, Minor: 14, Patch: 0}

// policies is the ordered strategy table. The first whose Capability matches runs,
// so the order is the priority: the specific floor policy comes before the broad
// cross-target refusal, and both come before any future general policy.
//
// LegacyQt5Relocator and CrossTargetRelocator have no Relocate yet: they exist so
// that an uncovered request fails through the selector — with a message naming the
// matrix — rather than being special-cased by a caller that might forget to refuse.
var policies = []Relocator{
	&LegacyQt5Relocator{},
	&CrossTargetRelocator{},
	&ModernDesktopRelocator{},
}

// Selector chooses among the policies and answers the pre-download capability
// question.
type Selector struct {
	policies []Relocator
}

// NewSelector returns the selector over the built-in policies.
func NewSelector() *Selector { return &Selector{policies: policies} }

// Select returns the policy that covers the target, or a refusal naming what the
// policies cover. It never returns a policy through which the target is not
// covered: no silent pass is the point (ADR-011 decision 1).
func (s *Selector) Select(host model.Host, kind model.Kind, version model.Version) (Relocator, error) {
	for _, p := range s.policies {
		if p.Capability().Supports(host, kind, version) {
			return p, nil
		}
	}
	return nil, unsupported(host, kind, version, s.policies)
}

// Check reports whether the target can be relocated now, without running
// anything. It is what plan and install call before a download (ADR-011
// decision 2): a covered-but-unready target fails here, in a second, rather than
// after gigabytes have been fetched.
func (s *Selector) Check(target model.Target) error {
	policy, err := s.Select(target.Host, target.Kind, target.Version)
	if err != nil {
		return err
	}
	if !policy.Ready() {
		return notReady(policy, target)
	}
	return nil
}

// notReady reports a target whose policy exists but whose primitives have not
// landed.
func notReady(policy Relocator, target model.Target) error {
	return errs.New(exitcode.Relocate, errs.CodeRelocateUnsupported, errs.PhaseRelocate,
		"this build cannot relocate %s yet: the %s policy has no implementation",
		target.String(), policy.Capability().Name).
		WithSuggestion("installing it would produce a tree that does not run where it lands")
}

// unsupported builds the "no policy covers this" failure, listing what is covered
// so the reader can tell a version this build refuses from a version it does not
// know.
func unsupported(host model.Host, kind model.Kind, version model.Version, policies []Relocator) error {
	lines := make([]string, 0, len(policies))
	for _, p := range policies {
		lines = append(lines, "  "+describe(p.Capability()))
	}
	return errs.New(exitcode.Relocate, errs.CodeRelocateUnsupported, errs.PhaseRelocate,
		"this build cannot relocate %s %s %s yet; covered:\n%s",
		host, kind, version.Dotted(), strings.Join(lines, "\n")).
		WithSuggestion("installing it would produce a tree that does not run where it lands")
}

// describe renders a capability's coverage for the refusal message.
func describe(c Capability) string {
	var b strings.Builder
	b.WriteString(c.Name)
	b.WriteString(": ")

	if len(c.Hosts) == 0 {
		b.WriteString("any host")
	} else {
		b.WriteString("hosts " + joinHosts(c.Hosts))
	}
	b.WriteString(", ")
	if len(c.Kinds) == 0 {
		b.WriteString("any target")
	} else {
		b.WriteString(joinKinds(c.Kinds))
	}
	b.WriteString(", ")

	switch {
	case c.Since != nil && c.Until != nil:
		fmt.Fprintf(&b, "versions %s to %s", c.Since.Dotted(), c.Until.Dotted())
	case c.Since != nil:
		fmt.Fprintf(&b, "versions %s and newer", c.Since.Dotted())
	case c.Until != nil:
		fmt.Fprintf(&b, "versions before %s", c.Until.Dotted())
	default:
		b.WriteString("any version")
	}
	if !c.Relocatable {
		b.WriteString(" (not relocatable)")
	}
	return b.String()
}

func joinHosts(hosts []model.Host) string {
	names := make([]string, len(hosts))
	for i, h := range hosts {
		names[i] = string(h)
	}
	return strings.Join(names, ", ")
}

func joinKinds(kinds []model.Kind) string {
	names := make([]string, len(kinds))
	for i, k := range kinds {
		names[i] = string(k)
	}
	return strings.Join(names, ", ")
}

// Relocate selects the policy for the target and runs it on tree.
func (s *Selector) Relocate(ctx context.Context, tree Tree, target model.Target) (Report, error) {
	policy, err := s.Select(target.Host, target.Kind, target.Version)
	if err != nil {
		return Report{}, err
	}
	if !policy.Ready() {
		return Report{}, notReady(policy, target)
	}
	return policy.Relocate(ctx, tree, target)
}
