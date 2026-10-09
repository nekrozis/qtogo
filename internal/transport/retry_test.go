package transport

import (
	"testing"
	"time"
)

func TestCappedGrowsThenHoldsAtMax(t *testing.T) {
	const base, max = 100 * time.Millisecond, 500 * time.Millisecond
	tests := map[int]time.Duration{
		0: 100 * time.Millisecond,
		1: 200 * time.Millisecond,
		2: 400 * time.Millisecond,
		3: 500 * time.Millisecond, // 800, capped
		9: 500 * time.Millisecond,
	}
	for attempt, want := range tests {
		if got := capped(base, max, attempt); got != want {
			t.Errorf("capped(%v, %v, %d) = %v, want %v", base, max, attempt, got, want)
		}
	}
	if got := capped(base, 0, 3); got != 0 {
		t.Errorf("capped with no max = %v, want 0", got)
	}
	if got := capped(0, max, 3); got != 0 {
		t.Errorf("capped with no base = %v, want 0", got)
	}
}

func TestJitteredStaysBetweenHalfAndFull(t *testing.T) {
	const d = 200 * time.Millisecond
	for i := 0; i < 200; i++ {
		if got := jittered(d); got < d/2 || got > d {
			t.Fatalf("jittered(%v) = %v, outside [%v, %v]", d, got, d/2, d)
		}
	}
	if got := jittered(0); got != 0 {
		t.Errorf("jittered(0) = %v, want 0", got)
	}
}
