// This package is outside `./...`: the `vendor` path element makes the Go tool skip
// it, which is what keeps a maintainer tool out of the ordinary build and CI. Its
// tests therefore run when the package is named:
//
//	go test ./tools/vendor/lzma
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// upstreamVersion is the only parsing in the tool: an unpacked SDK has no VERSION
// file -- that one is ours -- so its version comes out of its own documentation. It
// answers "" rather than failing, because the version is a convenience and the byte
// comparison is the check.
func TestUpstreamVersionReadsTheSDKDocumentation(t *testing.T) {
	root := t.TempDir()
	writeDoc := func(dir, body string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "lzma-sdk.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeDoc(filepath.Join(root, "DOC"), "LZMA SDK 26.04\n--------------\n\nLZMA SDK provides...\n")

	// The caller may hand over the SDK or its C directory.
	for why, dir := range map[string]string{
		"the SDK root":    root,
		"its C directory": filepath.Join(root, "C"),
	} {
		if got := upstreamVersion(dir); got != "26.04" {
			t.Errorf("upstreamVersion(%s) = %q, want 26.04", why, got)
		}
	}

	noDoc := t.TempDir()
	if got := upstreamVersion(noDoc); got != "" {
		t.Errorf("upstreamVersion of a directory with no documentation = %q, want \"\"", got)
	}

	otherDoc := t.TempDir()
	writeDoc(filepath.Join(otherDoc, "DOC"), "something else entirely\n")
	if got := upstreamVersion(otherDoc); got != "" {
		t.Errorf("upstreamVersion of a documentation file in another shape = %q, want \"\"", got)
	}
}
