package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestLdflagsStampsTheBuild(t *testing.T) {
	got := ldflags("1.2.3", "abcdef012345", "")
	want := strings.Join([]string{
		"-s", "-w",
		"-X " + buildinfoPkg + ".Version=1.2.3",
		"-X " + buildinfoPkg + ".Commit=abcdef012345",
		"-X " + buildinfoPkg + ".Date=",
	}, " ")
	if got != want {
		t.Errorf("ldflags = %q, want %q", got, want)
	}
}

func TestCCompilerNamesOnePerHost(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		// Linux carries the floor, which is why a compiler is named here at all: the
		// binary has to start on distributions older than the machine that built it.
		{"linux", "amd64", "zig cc -target x86_64-linux-gnu." + glibcFloor},
		{"linux", "arm64", "zig cc -target aarch64-linux-gnu." + glibcFloor},
		{"windows", "amd64", "zig cc"},
		{"darwin", "arm64", "zig cc"},
	}
	for _, tt := range tests {
		got, err := cCompiler(tt.goos, tt.goarch, "")
		if err != nil {
			t.Errorf("cCompiler(%s/%s) = %v", tt.goos, tt.goarch, err)
			continue
		}
		if got != tt.want {
			t.Errorf("cCompiler(%s/%s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

// A compiler the caller names is used as it stands: the platform rules, including the
// glibc target, describe zig and do not apply to a compiler we did not choose.
func TestCCompilerUsesTheNamedCompiler(t *testing.T) {
	got, err := cCompiler("linux", "amd64", "gcc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "gcc" {
		t.Errorf("cCompiler with a named compiler = %q, want %q", got, "gcc")
	}
}

// A host or architecture the driver has not been taught is refused rather than guessed
// at: a wrong compiler name is a build that links the wrong thing.
func TestCCompilerRefusesWhatItDoesNotKnow(t *testing.T) {
	tests := []struct{ goos, goarch string }{
		{"plan9", "amd64"},
		{"linux", "riscv64"},
		{"linux", "386"},
	}
	for _, tt := range tests {
		if _, err := cCompiler(tt.goos, tt.goarch, ""); err == nil {
			t.Errorf("cCompiler(%s/%s) = nil, want a refusal", tt.goos, tt.goarch)
		}
	}
}

func TestLinuxTriple(t *testing.T) {
	tests := map[string]string{
		"amd64": "x86_64-linux-gnu",
		"arm64": "aarch64-linux-gnu",
	}
	for goarch, want := range tests {
		got, err := linuxTriple(goarch)
		if err != nil {
			t.Errorf("linuxTriple(%q) = %v", goarch, err)
			continue
		}
		if got != want {
			t.Errorf("linuxTriple(%q) = %q, want %q", goarch, got, want)
		}
	}
	if _, err := linuxTriple("mips"); err == nil {
		t.Error("linuxTriple(mips) = nil, want a refusal")
	}
}

// A race build must not use zig, which carries no tsan, and must not inherit a CC from
// the shell that would put one back.
func TestBuildEnvRaceLeavesTheCompilerAlone(t *testing.T) {
	t.Setenv("CC", "zig cc")

	env, err := buildEnv(true, "")
	if err != nil {
		t.Fatal(err)
	}
	if !hasEnv(env, "CGO_ENABLED=1") {
		t.Error("CGO_ENABLED=1 is missing: cgo is what builds the extractor")
	}
	if lastValue(env, "CC") != "" {
		t.Errorf("a race build set CC=%q, want it unset", lastValue(env, "CC"))
	}
}

// A build runs with the compiler cCompiler names for this host, and a CC inherited from
// the shell is replaced rather than passed along.
func TestBuildEnvNamesTheCompilerForThisHost(t *testing.T) {
	t.Setenv("CC", "some-other-cc")
	stubLookPath(t, nil)

	want, err := cCompiler(runtime.GOOS, runtime.GOARCH, "")
	if err != nil {
		t.Fatal(err)
	}
	env, err := buildEnv(false, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := lastValue(env, "CC"); got != want {
		t.Errorf("CC = %q, want %q", got, want)
	}
	for _, entry := range env {
		if entry == "CC=some-other-cc" {
			t.Error("the inherited CC is still in the environment")
		}
	}
}

// `build -cc` threads the named compiler into the environment cgo reads.
func TestBuildEnvUsesTheNamedCompiler(t *testing.T) {
	stubLookPath(t, nil)

	env, err := buildEnv(false, "gcc")
	if err != nil {
		t.Fatal(err)
	}
	if got := lastValue(env, "CC"); got != "gcc" {
		t.Errorf("CC = %q, want %q", got, "gcc")
	}
}

// A missing compiler is reported as a missing tool, naming it, rather than as a
// compiler error somewhere inside the build.
func TestBuildEnvReportsAMissingCompiler(t *testing.T) {
	stubLookPath(t, errors.New("executable file not found"))

	if _, err := buildEnv(false, ""); err == nil {
		t.Fatal("buildEnv = nil, want an error when the compiler is not installed")
	}
}

// stubLookPath makes a build see the compiler as installed (err nil) or not, without
// the compiler having to be on the machine running the tests.
func stubLookPath(t *testing.T, err error) {
	t.Helper()
	original := lookPath
	lookPath = func(string) (string, error) {
		if err != nil {
			return "", err
		}
		return "/a/compiler", nil
	}
	t.Cleanup(func() { lookPath = original })
}

func TestBinPathIsUnderBin(t *testing.T) {
	got := binPath()
	want := "qtogo"
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got != "bin/"+want && got != `bin\`+want {
		t.Errorf("binPath = %q, want bin/%s", got, want)
	}
}

// The command line is the driver's public surface, so what it refuses is worth
// pinning: a typo is a usage error (2), not a build that quietly does something else.
func TestRunRefusesWhatItDoesNotUnderstand(t *testing.T) {
	tests := map[string][]string{
		"no command":                        {},
		"unknown command":                   {"buidl"},
		"unknown flag":                      {"build", "-race"},
		"-o without a path":                 {"build", "-o"},
		"-cc without a compiler":            {"build", "-cc"},
		"check with args":                   {"check", "extra"},
		"test with a flag it does not know": {"test", "-v"},
	}

	for why, args := range tests {
		if err := run(context.Background(), args); !errors.Is(err, errUsage) {
			t.Errorf("%s: run(%q) = %v, want a usage error", why, args, err)
		}
	}
}

func TestRunAcceptsHelp(t *testing.T) {
	if err := run(context.Background(), []string{"help"}); err != nil {
		t.Errorf("run(help) = %v", err)
	}
}

func TestArchiveNameNamesThePlatform(t *testing.T) {
	extension := ".tar.gz"
	if runtime.GOOS == "windows" {
		extension = ".zip"
	}
	want := "qtogo-1.2.3-" + runtime.GOOS + "-" + runtime.GOARCH + extension
	if got := archiveName("1.2.3"); got != want {
		t.Errorf("archiveName = %q, want %q", got, want)
	}
}

// A release archive holds the files under their own names, whatever directory they came
// from, and the checksum beside it is a sha256 of the bytes on disk.
func TestWriteArchiveAndChecksum(t *testing.T) {
	dir := t.TempDir()
	var sources []string
	for _, name := range []string{"qtogo", "LICENSE"} {
		path := filepath.Join(dir, "src", name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name+" contents"), 0o600); err != nil {
			t.Fatal(err)
		}
		sources = append(sources, path)
	}

	path := filepath.Join(dir, archiveName("1.2.3"))
	if err := writeArchive(path, sources); err != nil {
		t.Fatalf("writeArchive = %v", err)
	}
	if got, want := strings.Join(archiveEntries(t, path), " "), "LICENSE qtogo"; got != want {
		t.Errorf("archive holds %q, want %q", got, want)
	}

	sum, err := fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum) != 64 {
		t.Errorf("checksum = %q, want a sha256 in hex", sum)
	}
}

// A failure part-way leaves no archive under the final name: that name means the
// archive is whole.
func TestWriteArchiveLeavesNothingBehindOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, archiveName("1.2.3"))
	if err := writeArchive(path, []string{filepath.Join(dir, "does-not-exist")}); err == nil {
		t.Fatal("writeArchive with a missing source = nil, want an error")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an archive is at %s after a failed write", path)
	}
}

// archiveEntries lists an archive's contents, in whichever format its name asks for.
func archiveEntries(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var names []string
	if strings.HasSuffix(path, ".zip") {
		info, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}
		archive, err := zip.NewReader(file, info.Size())
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range archive.File {
			names = append(names, entry.Name)
		}
	} else {
		compressed, err := gzip.NewReader(file)
		if err != nil {
			t.Fatal(err)
		}
		defer compressed.Close()
		archive := tar.NewReader(compressed)
		for {
			header, err := archive.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			names = append(names, header.Name)
		}
	}
	sort.Strings(names)
	return names
}

func hasEnv(env []string, entry string) bool {
	for _, e := range env {
		if e == entry {
			return true
		}
	}
	return false
}

// lastValue is what a subprocess would see, since the Go toolchain takes the last of
// a repeated variable.
func lastValue(env []string, name string) string {
	value := ""
	for _, e := range env {
		if got, ok := strings.CutPrefix(e, name+"="); ok {
			value = got
		}
	}
	return value
}
