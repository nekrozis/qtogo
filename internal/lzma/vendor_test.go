package lzma

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ours are the files in this directory that did not come from the SDK: two record
// the provenance, two are the bridge, and two are these tests.
var ours = []string{
	"VERSION",
	"LICENSE",
	"manifest.txt",
	"cgo.go",
	"cgo_test.go",
	"vendor_test.go",
	"glue.c",
	"glue.h",
}

// manifestEntry is one line of manifest.txt: the file and the digest it had when it
// was taken from upstream.
type manifestEntry struct {
	name   string
	digest string
}

// manifestEntries reads manifest.txt, ignoring comments and blank lines.
//
// The tool in tools/vendor/lzma parses the same format, deliberately without
// sharing code: it must run on a machine with no C toolchain, so it cannot import
// this package.
func manifestEntries(t *testing.T) []manifestEntry {
	t.Helper()

	body, err := os.ReadFile("manifest.txt")
	if err != nil {
		t.Fatal(err)
	}

	var entries []manifestEntry
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("manifest line %q is not \"name  sha256\"", line)
		}
		entries = append(entries, manifestEntry{name: fields[0], digest: fields[1]})
	}
	if len(entries) == 0 {
		t.Fatal("manifest.txt lists nothing")
	}
	return entries
}

// The directory and the manifest have to agree: a file added or dropped upstream,
// or a stray file left behind, fails here and has to be accounted for.
func TestDirectoryMatchesTheManifest(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, entry := range entries {
		if entry.IsDir() {
			t.Errorf("%s is a directory; the vendored sources are flat", entry.Name())
			continue
		}
		got = append(got, entry.Name())
	}
	sort.Strings(got)

	want := append([]string{}, ours...)
	for _, entry := range manifestEntries(t) {
		want = append(want, entry.name)
	}
	sort.Strings(want)

	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("directory holds\n  %s\nwant\n  %s", strings.Join(got, " "), strings.Join(want, " "))
	}
}

// The digests are what makes this checkable without an upstream copy: a vendored
// file that changed under us fails here, which is why they are in the manifest
// rather than only in the maintainer's tool.
func TestVendoredFilesMatchTheirDigests(t *testing.T) {
	for _, entry := range manifestEntries(t) {
		body, err := os.ReadFile(entry.name)
		if err != nil {
			t.Errorf("%s: %v", entry.name, err)
			continue
		}
		sum := sha256.Sum256(body)
		if got := hex.EncodeToString(sum[:]); got != entry.digest {
			t.Errorf("%s: digest is %s, manifest says %s", entry.name, got, entry.digest)
		}
	}
}

func TestVersionIsRecorded(t *testing.T) {
	body, err := os.ReadFile("VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(body)); got != "26.04" {
		t.Errorf("VERSION = %q, want 26.04", got)
	}
}

func TestLicenceIsRecorded(t *testing.T) {
	body, err := os.ReadFile("LICENSE")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"public domain", "Igor Pavlov"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("LICENSE does not mention %q", want)
		}
	}
}

// Nothing from the SDK outside C/ may be vendored: the C++ implementation is a
// different licence and a different size of commitment.
func TestOnlyCSourcesAreVendored(t *testing.T) {
	for _, entry := range manifestEntries(t) {
		switch filepath.Ext(entry.name) {
		case ".c", ".h":
		default:
			t.Errorf("%s is not a C source", entry.name)
		}
		if filepath.Base(entry.name) != entry.name {
			t.Errorf("%s names a path outside the flat C directory", entry.name)
		}
	}
}
