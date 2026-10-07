// Command lzma-vendor maintains the vendored LZMA SDK sources.
//
// It is a maintainer tool, not part of building qtogo. It runs where the
// maintainer already has an unpacked SDK and it never downloads anything: a build
// that fetched a source file would depend on a release archive still being online,
// which is not a property this project controls.
//
// The check that needs no upstream copy — that the vendored files are the ones
// manifest.txt records — is a Go test beside them, so CI runs it. This tool is for
// the work that test cannot do: comparing against upstream, and taking an upgrade.
//
//	go run ./tools/vendor/lzma verify [<sdk-dir>]
//	go run ./tools/vendor/lzma update <version> <sdk-dir>
//	go run ./tools/vendor/lzma diff <old-sdk-dir> <new-sdk-dir>
//
// <sdk-dir> is either an unpacked SDK or its C directory.
//
// It is Go rather than a shell script because the maintainer may be on Windows,
// where make, quoting and PATH behave differently enough that a script would be one
// more thing to get right.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const usage = `usage: lzma-vendor <command> [arguments]

  verify [<sdk-dir>]             check the vendored files against manifest.txt, and
                                 against upstream when a directory is given
  update <version> <sdk-dir>     take a new upstream version: copy the files,
                                 rewrite manifest.txt and VERSION
  diff <old-dir> <new-dir>       show what changed between two upstream copies

<upstream-dir> is an unpacked LZMA SDK, or its C directory. Nothing is downloaded.
`

// manifestHeader is rewritten by update, so the file stays self-describing after a
// version bump.
const manifestHeader = `# Files taken unchanged from the LZMA SDK %s (see VERSION and LICENSE).
#
# The set is what the SDK's own decoder builds -- C/Util/7z/makefile -- plus the two
# CRC sources that makefile pulls in through CPP/7zip/Crc.mak, confirmed by taking
# the transitive #include closure of those files. Nothing from outside C/ is here.
#
# The digest is what makes the set checkable offline: the test beside this file and
# 'lzma-vendor verify' both read it, so a file that changed under us is caught with
# no upstream copy to compare against.
#
# name  sha256
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "verify":
		switch len(os.Args) {
		case 2:
			err = runVerify("")
		case 3:
			err = runVerify(os.Args[2])
		default:
			err = fmt.Errorf("verify takes at most one directory")
		}
	case "update":
		if len(os.Args) != 4 {
			err = fmt.Errorf("update takes a version and a directory")
			break
		}
		err = runUpdate(os.Args[2], os.Args[3])
	case "diff":
		if len(os.Args) != 4 {
			err = fmt.Errorf("diff takes two directories")
			break
		}
		err = runDiff(os.Args[2], os.Args[3])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "lzma-vendor: %v\n", err)
		os.Exit(1)
	}
}

// entry is one line of manifest.txt.
type entry struct {
	name   string
	digest string
}

// vendorDir finds the vendored package whether the tool runs from the module root
// or from anywhere else: the source file knows where it lives.
func vendorDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("cannot locate this source file")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "..")
	return filepath.Join(root, "internal", "lzma"), nil
}

// manifest is the whitelist: the files the vendored package takes from upstream,
// with the digest each had when it was taken. It is the single record of that
// decision, and the test beside the sources reads it too.
func readManifest(dir string) ([]entry, error) {
	body, err := os.ReadFile(filepath.Join(dir, "manifest.txt"))
	if err != nil {
		return nil, err
	}

	var entries []entry
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("manifest line %q is not \"name  sha256\"", line)
		}
		entries = append(entries, entry{name: fields[0], digest: fields[1]})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("manifest.txt lists nothing")
	}
	return entries, nil
}

func digestOf(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func versionOf(dir string) string {
	body, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(body))
}

// cDir accepts either the unpacked SDK or its C directory, because the archive
// unpacks to a tree and the maintainer should not have to remember which.
func cDir(dir string) (string, error) {
	if info, err := os.Stat(filepath.Join(dir, "C")); err == nil && info.IsDir() {
		return filepath.Join(dir, "C"), nil
	}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("%s is not a directory", dir)
}

func runVerify(upstream string) error {
	dir, err := vendorDir()
	if err != nil {
		return err
	}
	entries, err := readManifest(dir)
	if err != nil {
		return err
	}

	fmt.Printf("LZMA SDK version: %s\n\nvendored files:\n", versionOf(dir))

	drifted := 0
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(dir, e.name))
		if err != nil {
			fmt.Printf("  missing:   %s\n", e.name)
			drifted++
			continue
		}
		if got := digestOf(body); got != e.digest {
			fmt.Printf("  changed:   %s\n", e.name)
			drifted++
		}
	}
	fmt.Printf("  %d checked, %d match\n", len(entries), len(entries)-drifted)

	// With a directory to compare against, the upstream bytes are the stronger
	// check; without one the digests are all we have, and they are enough to catch
	// a file that changed under us.
	if upstream != "" {
		upstreamC, err := cDir(upstream)
		if err != nil {
			return err
		}
		fmt.Printf("\nagainst %s:\n", upstreamC)

		differing := 0
		for _, e := range entries {
			want, err := os.ReadFile(filepath.Join(upstreamC, e.name))
			if err != nil {
				fmt.Printf("  missing upstream: %s\n", e.name)
				differing++
				continue
			}
			got, err := os.ReadFile(filepath.Join(dir, e.name))
			if err != nil || !bytes.Equal(got, want) {
				fmt.Printf("  differs:          %s\n", e.name)
				differing++
			}
		}
		fmt.Printf("  %d compared, %d match\n", len(entries), len(entries)-differing)

		if differing > 0 {
			return fmt.Errorf("%d files are not what upstream has", differing)
		}
	}

	if drifted > 0 {
		return fmt.Errorf("%d files are not what manifest.txt records", drifted)
	}
	fmt.Println("\nOK")
	return nil
}

// runUpdate takes a new upstream version.
//
// It deliberately does not check the vendored sources against upstream first: an
// update exists because the two differ. What it does check is that the tree it is
// copying from is the whole SDK — every file the manifest names — so a partial or
// wrong directory cannot quietly become the vendored sources.
func runUpdate(version, upstream string) error {
	dir, err := vendorDir()
	if err != nil {
		return err
	}
	entries, err := readManifest(dir)
	if err != nil {
		return err
	}
	upstreamC, err := cDir(upstream)
	if err != nil {
		return err
	}

	missing := 0
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(upstreamC, e.name)); err != nil {
			fmt.Printf("  not in %s: %s\n", upstreamC, e.name)
			missing++
		}
	}
	if missing > 0 {
		return fmt.Errorf("%s is missing %d of the %d files manifest.txt names; is it the whole SDK?",
			upstreamC, missing, len(entries))
	}

	// What upstream added that the taken files reach: an upgrade that needs a new
	// file has to say so, rather than fail at link time later.
	needed, err := closure(upstreamC, entries)
	if err != nil {
		return err
	}

	copied := make([]entry, 0, len(entries))
	for _, e := range entries {
		body, err := os.ReadFile(filepath.Join(upstreamC, e.name))
		if err != nil {
			return err
		}
		target := filepath.Join(dir, e.name)
		// The SDK ships its sources read-only and a copy of them can carry that bit
		// into the working tree, where it stops the next update.
		if err := os.Chmod(target, 0o644); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			return err
		}
		copied = append(copied, entry{name: e.name, digest: digestOf(body)})
	}
	if err := writeManifest(dir, version, copied); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte(version+"\n"), 0o644); err != nil {
		return err
	}

	fmt.Printf("copied %d files from %s\nversion is now %s\n", len(copied), upstreamC, version)
	if len(needed) > 0 {
		fmt.Printf("\nupstream added files the taken ones reach; add them to manifest.txt and\nrun the command again:\n  %s\n", strings.Join(needed, "\n  "))
	}
	fmt.Println("\nNow run the tests and read the diff before committing.")
	return nil
}

func writeManifest(dir, version string, entries []entry) error {
	var b strings.Builder
	fmt.Fprintf(&b, manifestHeader, version)
	for _, e := range entries {
		fmt.Fprintf(&b, "%s  %s\n", e.name, e.digest)
	}
	return os.WriteFile(filepath.Join(dir, "manifest.txt"), []byte(b.String()), 0o644)
}

func runDiff(oldDir, newDir string) error {
	dir, err := vendorDir()
	if err != nil {
		return err
	}
	entries, err := readManifest(dir)
	if err != nil {
		return err
	}
	oldC, err := cDir(oldDir)
	if err != nil {
		return err
	}
	newC, err := cDir(newDir)
	if err != nil {
		return err
	}

	var changed, gone []string
	for _, e := range entries {
		before, errOld := os.ReadFile(filepath.Join(oldC, e.name))
		after, errNew := os.ReadFile(filepath.Join(newC, e.name))
		switch {
		case errOld != nil && errNew == nil:
			changed = append(changed, e.name+" (new upstream)")
		case errOld == nil && errNew != nil:
			gone = append(gone, e.name)
		case !bytes.Equal(before, after):
			changed = append(changed, e.name)
		}
	}

	fmt.Printf("files the vendored package takes, compared between\n  %s\n  %s\n\n", oldC, newC)
	if len(changed) == 0 {
		fmt.Println("  no change")
	}
	for _, name := range changed {
		fmt.Printf("  changed: %s\n", name)
	}
	for _, name := range gone {
		fmt.Printf("  gone upstream: %s\n", name)
	}

	// The question an upgrade has to answer is not "what does upstream have" — it
	// is "does the decoder now reach a file the manifest does not take".
	needed, err := closure(newC, entries)
	if err != nil {
		return err
	}
	if len(needed) > 0 {
		fmt.Printf("\nreachable from the taken files but not taken (add to manifest.txt):\n  %s\n",
			strings.Join(needed, "\n  "))
	}
	return nil
}

// closure returns the files reachable by #include from the given ones and not
// already among them. It is how the file list was derived in the first place: the
// SDK's decoder builds from a root set, and the closure of that set is what it
// actually needs.
func closure(dir string, taken []entry) ([]string, error) {
	isTaken := map[string]bool{}
	for _, e := range taken {
		isTaken[e.name] = true
	}

	var missing []string
	seen := map[string]bool{}
	queue := make([]string, 0, len(taken))
	for _, e := range taken {
		queue = append(queue, e.name)
	}

	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true

		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		for _, include := range includesOf(body) {
			if isTaken[include] || seen[include] {
				continue
			}
			// An include that is not a file here is either behind a flag this build
			// does not set or upstream's own business; only files that exist are
			// the maintainer's problem.
			if _, err := os.Stat(filepath.Join(dir, include)); err != nil {
				continue
			}
			queue = append(queue, include)
			missing = append(missing, include)
		}
	}

	sort.Strings(missing)
	return missing, nil
}

// includesOf reads the quoted includes of a C source or header.
func includesOf(body []byte) []string {
	var includes []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#include") {
			continue
		}
		start := strings.IndexByte(line, '"')
		if start < 0 {
			continue
		}
		end := strings.IndexByte(line[start+1:], '"')
		if end < 0 {
			continue
		}
		includes = append(includes, line[start+1:start+1+end])
	}
	return includes
}
