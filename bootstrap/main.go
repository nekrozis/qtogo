// Command bootstrap is this project's build driver.
//
// The alternative is a Makefile, and a Makefile needs a POSIX shell: on Windows that
// means Git Bash, or a second set of commands copied out of the README. A Go program
// needs nothing that building this project does not already require, and it handles
// paths, arguments and subprocesses the same way on every platform.
//
//	go run ./bootstrap build        # write bin/qtogo
//	go run ./bootstrap test         # go test ./...
//	go run ./bootstrap test -race   # ... with the race detector
//	go run ./bootstrap check        # gofmt check, go vet, the tests, golangci-lint
//
// It builds for the host only. The C toolchain is chosen for the machine the driver
// runs on, and a target it cannot run is a target it cannot test.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
)

const usage = `usage: bootstrap <command> [arguments]

  build [-o <path>]   build the qtogo binary for this host
  test [-race]        run the unit tests
  check               gofmt check, go vet, the tests, golangci-lint

The commands set up the C toolchain cgo needs, so no shell setup is required.
`

// buildinfoPkg is where the linker writes the build stamp. The driver cannot import it
// and read the names from there: -X takes a package path and a variable name as text.
const buildinfoPkg = "github.com/nekrozis/qtogo/internal/buildinfo"

// errUsage says the command line was wrong, which is the caller's mistake rather than
// a step that failed.
var errUsage = errors.New("usage")

// commandError carries the exit status of a tool that failed, so the driver reports
// what the tool said instead of a generic 1.
type commandError struct {
	code int
}

func (e *commandError) Error() string {
	return fmt.Sprintf("the command exited with status %d", e.code)
}

func main() {
	// The status is returned rather than exited with, so the deferred stop below runs
	// before the process ends.
	os.Exit(realMain())
}

// realMain runs the command and returns the status to leave with.
func realMain() int {
	// An interrupt cancels the tool that is running, so Ctrl-C stops the build rather
	// than leaving a compiler behind it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	err := run(ctx, os.Args[1:])
	switch {
	case err == nil:
		return 0
	case errors.Is(err, errUsage):
		return 2
	}
	fmt.Fprintf(os.Stderr, "bootstrap: %v\n", err)
	var failed *commandError
	if errors.As(err, &failed) {
		return failed.code
	}
	return 1
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError("no command")
	}
	switch args[0] {
	case "build":
		return runBuild(ctx, args[1:])
	case "test":
		return runTest(ctx, args[1:])
	case "check":
		return runCheck(ctx, args[1:])
	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil
	default:
		return usageError("unknown command %q", args[0])
	}
}

func usageError(format string, args ...any) error {
	fmt.Fprintf(os.Stderr, "bootstrap: "+format+"\n\n", args...)
	fmt.Fprint(os.Stderr, usage)
	return errUsage
}

func runBuild(ctx context.Context, args []string) error {
	out := binPath()
	switch len(args) {
	case 0:
	case 2:
		if args[0] != "-o" {
			return usageError("unknown argument %q", args[0])
		}
		out = args[1]
	default:
		return usageError("build takes -o <path> or nothing, not %d arguments", len(args))
	}

	env, err := buildEnv(runtime.GOOS, false)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(out); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}

	version, commit, date := describedBuild(ctx)
	return goRun(ctx, env, "build", "-trimpath", "-buildvcs=false",
		"-ldflags", ldflags(version, commit, date), "-o", out, "./cmd/qtogo")
}

func runTest(ctx context.Context, args []string) error {
	race := false
	for _, arg := range args {
		switch arg {
		case "-race":
			race = true
		default:
			return usageError("unknown argument %q", arg)
		}
	}

	env, err := buildEnv(runtime.GOOS, race)
	if err != nil {
		return err
	}
	testArgs := []string{"test", "-count=1"}
	if race {
		testArgs = append(testArgs, "-race")
	}
	return goRun(ctx, env, append(testArgs, "./...")...)
}

func runCheck(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return usageError("check takes no arguments")
	}
	if err := checkFormatting(ctx); err != nil {
		return err
	}
	env, err := buildEnv(runtime.GOOS, false)
	if err != nil {
		return err
	}
	if err := goRun(ctx, env, "vet", "./..."); err != nil {
		return err
	}
	if err := runTest(ctx, nil); err != nil {
		return err
	}
	return runLint(ctx)
}

// checkFormatting fails on any file gofmt would rewrite, and names them.
func checkFormatting(ctx context.Context) error {
	out, err := exec.CommandContext(ctx, "gofmt", "-l", ".").Output()
	if err != nil {
		return fmt.Errorf("running gofmt: %w", err)
	}
	if names := strings.TrimSpace(string(out)); names != "" {
		return fmt.Errorf("these files are not gofmt-clean:\n%s", names)
	}
	return nil
}

// runLint runs golangci-lint, saying what it is for when it is missing: a bare
// "executable file not found" tells the reader nothing about what to do about it.
func runLint(ctx context.Context) error {
	path, err := exec.LookPath("golangci-lint")
	if err != nil {
		return errors.New("golangci-lint is not installed, and `check` runs it; see https://golangci-lint.run/")
	}
	cmd := exec.CommandContext(ctx, path, "run")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return &commandError{code: exitCodeOf(err)}
	}
	return nil
}

// buildEnv is the environment a build or test runs with: the caller's, with cgo turned
// on and the C compiler named.
func buildEnv(goos string, race bool) ([]string, error) {
	env := append(without(os.Environ(), "CGO_ENABLED"), "CGO_ENABLED=1")
	if race {
		// The race detector needs tsan symbols, which zig does not provide, so a race
		// build is left to the compiler the Go toolchain picks for the host. Dropping an
		// inherited CC is what makes that true whatever shell started this.
		return without(env, "CC"), nil
	}
	cc, err := cCompiler(goos)
	if err != nil {
		return nil, err
	}
	return append(without(env, "CC"), "CC="+cc), nil
}

// without drops any inherited setting of name, so what the driver decides is what the
// toolchain sees, rather than a duplicate whose effect depends on ordering.
func without(env []string, name string) []string {
	kept := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, name+"=") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// cCompiler is the C compiler cgo uses on this host.
//
// cgo builds the archive extractor, so one has to be named. Windows gets zig because it
// ships no C compiler of its own and zig is a single binary; elsewhere the system
// compiler is already installed and is the one the platform's headers belong to.
func cCompiler(goos string) (string, error) {
	switch goos {
	case "windows":
		return "zig cc", nil
	case "linux", "darwin":
		return "cc", nil
	default:
		return "", fmt.Errorf("no C compiler is configured for %s", goos)
	}
}

// goRun runs a go subcommand with the streams attached, and reports the status it
// returned. The command line goes to stderr first, so a log says what ran.
func goRun(ctx context.Context, env []string, args ...string) error {
	fmt.Fprintln(os.Stderr, "bootstrap: go "+strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return &commandError{code: exitCodeOf(err)}
	}
	return nil
}

// exitCodeOf reads the status out of an exec error, defaulting to 1 for the failures
// that never started a process.
func exitCodeOf(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 1
}

// describedBuild reads the version and commit from git, or the values that say "not a
// release" when git cannot answer.
//
// The date is deliberately left empty: a stamp of "now" would make two machines
// building the same commit produce different binaries.
func describedBuild(ctx context.Context) (version, commit, date string) {
	version = strings.TrimPrefix(git(ctx, "describe", "--tags", "--abbrev=0"), "v")
	if version == "" {
		version = "0.1.0-dev"
	}
	commit = git(ctx, "rev-parse", "--short=12", "HEAD")
	if commit == "" {
		commit = "unknown"
	}
	return version, commit, ""
}

// git returns the trimmed output of a git command, or "" when git is absent or has
// nothing to say.
func git(ctx context.Context, args ...string) string {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ldflags is what the linker is told: strip the symbol table, and stamp the build.
func ldflags(version, commit, date string) string {
	return strings.Join([]string{
		"-s", "-w",
		"-X " + buildinfoPkg + ".Version=" + version,
		"-X " + buildinfoPkg + ".Commit=" + commit,
		"-X " + buildinfoPkg + ".Date=" + date,
	}, " ")
}

// binPath is where a build lands when the caller does not say.
func binPath() string {
	return filepath.Join("bin", "qtogo"+exeSuffix())
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
