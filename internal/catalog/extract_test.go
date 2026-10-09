package catalog

import (
	"strings"
	"testing"

	"github.com/nekrozis/qtogo/internal/repository"
)

func TestExtractPairsReadsDestinationsAndArchivesInOrder(t *testing.T) {
	pkg := repository.PackageUpdate{
		Operations: []repository.Operation{
			{Name: "Extract", Arguments: []string{"@TargetDir@/6.8.0/msvc2022_64", "qtbase.7z"}},
			{Name: "Extract", Arguments: []string{"@TargetDir@/6.8.0/msvc2022_64", "qtsvg.7z"}},
		},
	}

	pairs := extractPairs(pkg)

	if len(pairs) != 2 {
		t.Fatalf("pairs = %+v, want two", pairs)
	}
	for i, want := range []string{"qtbase.7z", "qtsvg.7z"} {
		if pairs[i].archive != want || pairs[i].dest != "6.8.0/msvc2022_64" {
			t.Errorf("pair %d = %+v, want %q at 6.8.0/msvc2022_64", i, pairs[i], want)
		}
	}
}

// A destination without the template prefix is kept as written.
func TestExtractPairsKeepsAPlainDestination(t *testing.T) {
	pkg := repository.PackageUpdate{
		Operations: []repository.Operation{
			{Name: "Extract", Arguments: []string{"plain/dir", "qtbase.7z"}},
		},
	}

	pairs := extractPairs(pkg)
	if len(pairs) != 1 || pairs[0].dest != "plain/dir" {
		t.Errorf("pairs = %+v, want the destination kept", pairs)
	}
}

// An argument with no partner is ignored rather than making a half pair.
func TestExtractPairsIgnoresATrailingArgument(t *testing.T) {
	pkg := repository.PackageUpdate{
		Operations: []repository.Operation{
			{Name: "Extract", Arguments: []string{"@TargetDir@/6.8.0/msvc2022_64", "qtbase.7z", "@TargetDir@/leftover"}},
		},
	}

	pairs := extractPairs(pkg)
	if len(pairs) != 1 || pairs[0].archive != "qtbase.7z" {
		t.Errorf("pairs = %+v, want the complete pair and nothing from the leftover", pairs)
	}
}

// Without Extract the downloads land at the root.
func TestExtractPairsFallsBackToTheRoot(t *testing.T) {
	pkg := repository.PackageUpdate{Downloadables: []string{"qtbase.7z", "qtsvg.7z"}}

	pairs := extractPairs(pkg)
	if len(pairs) != 2 {
		t.Fatalf("pairs = %+v, want the two downloads", pairs)
	}
	for _, p := range pairs {
		if p.dest != "" {
			t.Errorf("pair %+v has a destination, want the root", p)
		}
	}
}

// Only Extract operations contribute arguments.
func TestExtractArgsReadsOnlyExtract(t *testing.T) {
	pkg := repository.PackageUpdate{
		Operations: []repository.Operation{
			{Name: "License", Arguments: []string{"@TargetDir@/ignored"}},
			{Name: "Extract", Arguments: []string{"@TargetDir@/dir", "a.7z"}},
		},
	}

	args := extractArgs(pkg)
	if strings.Join(args, ",") != "@TargetDir@/dir,a.7z" {
		t.Errorf("args = %v, want only the Extract arguments", args)
	}
}

// FuzzExtractPairs pins the pairing: a pair is never half-formed, no argument is
// used twice, and the count is the number of complete pairs.
func FuzzExtractPairs(f *testing.F) {
	f.Add("@TargetDir@/6.8.0/msvc2022_64", "qtbase.7z")
	f.Add("@TargetDir@/dir", "a.7z")
	f.Add("plain", "b.7z")

	f.Fuzz(func(t *testing.T, dest, archive string) {
		pkg := repository.PackageUpdate{
			Operations: []repository.Operation{{Name: "Extract", Arguments: []string{dest, archive}}},
		}

		pairs := extractPairs(pkg)
		if len(pairs) != 1 {
			t.Fatalf("extractPairs of one pair = %d pairs, want 1", len(pairs))
		}
		if pairs[0].archive != archive {
			t.Errorf("archive = %q, want %q", pairs[0].archive, archive)
		}
		if want := strings.TrimPrefix(dest, targetDirPrefix); pairs[0].dest != want {
			t.Errorf("dest = %q, want %q", pairs[0].dest, want)
		}
	})
}
