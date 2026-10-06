package exitcode

import (
	"errors"
	"fmt"
	"testing"
)

// coded is a stand-in CodedError, so this package's tests do not depend on
// internal/errs.
type coded struct{ code int }

func (c coded) Error() string { return fmt.Sprintf("coded(%d)", c.code) }

func (c coded) ExitCode() int { return c.code }

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil is success", nil, OK},
		{"uses the reported code", coded{Relocate}, Relocate},
		{"foreign error is internal", errors.New("boom"), Internal},
		{"wrapped error keeps its code", fmt.Errorf("outer: %w", coded{Network}), Network},
		{"wrapped twice keeps its code", fmt.Errorf("a: %w", fmt.Errorf("b: %w", coded{NotFound})), NotFound},
		{"a code of zero is treated as unclassified", coded{OK}, Internal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.err); got != tt.want {
				t.Fatalf("Classify(%v) = %d, want %d", tt.err, got, tt.want)
			}
		})
	}
}

// TestPublishedValues pins the numbers against literals: changing one of the
// constants is a contract change, and this is where that shows up.
func TestPublishedValues(t *testing.T) {
	published := []struct {
		name string
		got  int
		want int
	}{
		{"OK", OK, 0},
		{"Internal", Internal, 1},
		{"Usage", Usage, 2},
		{"NotFound", NotFound, 3},
		{"Network", Network, 4},
		{"Integrity", Integrity, 5},
		{"Filesystem", Filesystem, 6},
		{"Relocate", Relocate, 7},
		{"Config", Config, 8},
		{"Auth", Auth, 9},
		{"Interrupted", Interrupted, 130},
	}

	seen := make(map[int]string, len(published))
	for _, p := range published {
		if p.got != p.want {
			t.Errorf("%s = %d, want %d", p.name, p.got, p.want)
		}
		if prev, dup := seen[p.got]; dup {
			t.Errorf("%s and %s share exit code %d", prev, p.name, p.got)
		}
		seen[p.got] = p.name
	}
}
