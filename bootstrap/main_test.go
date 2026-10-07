package main

import (
	"context"
	"errors"
	"runtime"
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
		got, err := cCompiler(tt.goos, tt.goarch)
		if err != nil {
			t.Errorf("cCompiler(%s/%s) = %v", tt.goos, tt.goarch, err)
			continue
		}
		if got != tt.want {
			t.Errorf("cCompiler(%s/%s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
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
		if _, err := cCompiler(tt.goos, tt.goarch); err == nil {
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

	env, err := buildEnv(true)
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

	want, err := cCompiler(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	env, err := buildEnv(false)
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
		"bare -o":                           {"build", "-o"},
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
