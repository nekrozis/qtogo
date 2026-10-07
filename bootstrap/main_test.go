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
	tests := map[string]string{
		"windows": "zig cc",
		"linux":   "cc",
		"darwin":  "cc",
	}
	for goos, want := range tests {
		got, err := cCompiler(goos)
		if err != nil {
			t.Errorf("cCompiler(%q) = %v", goos, err)
			continue
		}
		if got != want {
			t.Errorf("cCompiler(%q) = %q, want %q", goos, got, want)
		}
	}

	// A host the driver has not been taught is refused rather than guessed at.
	if _, err := cCompiler("plan9"); err == nil {
		t.Error("cCompiler(plan9) = nil, want a refusal")
	}
}

// A race build must not use zig, which carries no tsan, and must not inherit one
// either: the empty assignment is what makes that true whatever the shell set.
func TestBuildEnvRaceLeavesTheCompilerAlone(t *testing.T) {
	env, err := buildEnv("linux", true)
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

// A CC inherited from the shell is replaced rather than passed along, so the compiler
// this program chose is the one that runs.
func TestBuildEnvReplacesAnInheritedCompiler(t *testing.T) {
	t.Setenv("CC", "some-other-cc")

	env, err := buildEnv("windows", false)
	if err != nil {
		t.Fatal(err)
	}
	if lastValue(env, "CC") != "zig cc" {
		t.Errorf("CC = %q, want %q", lastValue(env, "CC"), "zig cc")
	}
	for _, entry := range env {
		if entry == "CC=some-other-cc" {
			t.Error("the inherited CC is still in the environment")
		}
	}
}

func TestBuildEnvNamesTheCompiler(t *testing.T) {
	env, err := buildEnv("windows", false)
	if err != nil {
		t.Fatal(err)
	}
	if got := lastValue(env, "CC"); got != "zig cc" {
		t.Errorf("CC = %q, want %q", got, "zig cc")
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
