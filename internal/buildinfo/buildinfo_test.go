package buildinfo

import (
	"runtime"
	"testing"
)

// stamp sets the injected variables for one test and restores them afterwards,
// so test order never matters.
func stamp(t *testing.T, version, commit, date string) {
	t.Helper()

	oldVersion, oldCommit, oldDate := Version, Commit, Date
	t.Cleanup(func() {
		Version, Commit, Date = oldVersion, oldCommit, oldDate
	})

	Version, Commit, Date = version, commit, date
}

func TestStringDevelopment(t *testing.T) {
	stamp(t, DefaultVersion, "", "")

	const want = "qtogo 0.1.0"
	if got := String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringFullyStamped(t *testing.T) {
	stamp(t, "1.2.3", "abc123def456", "2026-10-06T09:00:00Z")

	got := String()
	want := "qtogo 1.2.3 (commit abc123def456, built 2026-10-06T09:00:00Z, " + runtime.Version() + ")"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringPartialStamps(t *testing.T) {
	tests := []struct {
		name   string
		commit string
		date   string
		want   string
	}{
		{
			name:   "commit only",
			commit: "deadbeef",
			want:   "qtogo 0.1.0 (commit deadbeef, " + runtime.Version() + ")",
		},
		{
			name: "date only",
			date: "2026-10-06T09:00:00Z",
			want: "qtogo 0.1.0 (built 2026-10-06T09:00:00Z, " + runtime.Version() + ")",
		},
		{
			name: "neither",
			want: "qtogo 0.1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stamp(t, DefaultVersion, tt.commit, tt.date)

			if got := String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}
