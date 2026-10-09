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
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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
  release             write a host archive and its checksum into dist/

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
	case "release":
		return runRelease(ctx, args[1:])
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

	if dir := filepath.Dir(out); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}

	version, commit, date := describedBuild(ctx)
	return buildBinary(ctx, out, version, commit, date)
}

// buildBinary writes the qtogo binary to out, stamped with the build it came from.
func buildBinary(ctx context.Context, out, version, commit, date string) error {
	env, err := buildEnv(false)
	if err != nil {
		return err
	}
	return goRun(ctx, env, "build", "-trimpath", "-buildvcs=false",
		"-ldflags", ldflags(version, commit, date), "-o", out, "./cmd/qtogo")
}

// distDir is where a release archive is written.
const distDir = "dist"

// runRelease writes an archive of this host's binary, with its licence, and a checksum
// beside it.
//
// It is a manual step by design. Nothing publishes, and the version is whatever the
// checkout says, because the parts that decide when a release happens and what it is
// called are still open; this exists so that producing one by hand is the same on every
// platform, not so that it happens on its own.
func runRelease(ctx context.Context, args []string) error {
	if len(args) > 0 {
		return usageError("release takes no arguments")
	}
	version, commit, date := describedBuild(ctx)

	// The binary is built somewhere private first, so that an archive only ever appears
	// under its final name once it is complete.
	staging, err := os.MkdirTemp("", "qtogo-release-")
	if err != nil {
		return err
	}
	// Best effort: a leftover temporary directory is not worth reporting over a build
	// that already succeeded or failed on its own merits.
	defer func() { _ = os.RemoveAll(staging) }()

	binary := filepath.Join(staging, "qtogo"+exeSuffix())
	if err := buildBinary(ctx, binary, version, commit, date); err != nil {
		return err
	}

	if err := os.MkdirAll(distDir, 0o750); err != nil {
		return err
	}
	name := archiveName(version)
	path := filepath.Join(distDir, name)
	// The licence travels with the binary: this is BSD-3-Clause, and redistributing the
	// binary means carrying the notice and the disclaimer.
	if err := writeArchive(path, []string{binary, "LICENSE", "THIRD-PARTY.md"}); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	sum, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path+".sha256", []byte(sum+"  "+name+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Printf("bootstrap: wrote %s and %s.sha256\n", path, path)
	return nil
}

// archiveName is what a release archive is called: the tool, its version, and the
// platform the binary was built for.
func archiveName(version string) string {
	extension := ".tar.gz"
	if runtime.GOOS == "windows" {
		extension = ".zip"
	}
	return strings.Join([]string{"qtogo", version, runtime.GOOS, runtime.GOARCH}, "-") + extension
}

// writeArchive puts the named files into one archive at path, flat, in whichever format
// the name asks for.
//
// It writes under a temporary name and renames when the bytes are whole, so the
// archive appears under its final name only once it is complete and a failure part-way
// leaves nothing to mistake for one.
func writeArchive(path string, files []string) error {
	out, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	// Best effort: after a successful rename there is nothing at the temporary name,
	// and after a failure the temporary file is not worth reporting over the failure.
	defer func() { _ = os.Remove(out.Name()) }()

	if strings.HasSuffix(path, ".zip") {
		err = writeZip(out, files)
	} else {
		err = writeTarGz(out, files)
	}
	// A close error means the last of the bytes never landed, so it counts whenever the
	// writing itself went well.
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(out.Name(), path)
}

func writeTarGz(out io.Writer, files []string) error {
	compressed := gzip.NewWriter(out)
	archive := tar.NewWriter(compressed)
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		header := &tar.Header{
			Name: filepath.Base(file),
			Mode: int64(info.Mode().Perm()),
			Size: int64(len(body)),
		}
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if _, err := archive.Write(body); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return compressed.Close()
}

func writeZip(out io.Writer, files []string) error {
	archive := zip.NewWriter(out)
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		entry, err := archive.Create(filepath.Base(file))
		if err != nil {
			return err
		}
		if _, err := entry.Write(body); err != nil {
			return err
		}
	}
	return archive.Close()
}

// fileSHA256 is the digest written beside an archive, so that whoever downloads it can
// check what they got.
func fileSHA256(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
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

	env, err := buildEnv(race)
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
	env, err := buildEnv(false)
	if err != nil {
		return err
	}
	if err := goRun(ctx, env, "vet", "./..."); err != nil {
		return err
	}
	if err := runTest(ctx, nil); err != nil {
		return err
	}
	return runLint(ctx, env)
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
//
// It runs with the same environment a build does. The linter typechecks the cgo
// packages, so an inherited CGO_ENABLED=0 excludes them from the build and the run
// fails with "build constraints exclude all Go files" -- which is exactly the state of
// a machine that has zig and no system C compiler, the machine this driver is for.
func runLint(ctx context.Context, env []string) error {
	path, err := exec.LookPath("golangci-lint")
	if err != nil {
		return errors.New("golangci-lint is not installed, and `check` runs it; see https://golangci-lint.run/")
	}
	cmd := exec.CommandContext(ctx, path, "run")
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return &commandError{code: exitCodeOf(err)}
	}
	return nil
}

// buildEnv is the environment a build or test runs with: the caller's, with cgo turned
// on and the C compiler named.
// The platform rules live in cCompiler, which takes the host as an argument and can be
// exercised for one the driver is not running on. This only assembles the environment.
func buildEnv(race bool) ([]string, error) {
	env := append(without(os.Environ(), "CGO_ENABLED"), "CGO_ENABLED=1")
	if race {
		// The race detector needs tsan symbols, which zig does not provide, so a race
		// build is left to the compiler the Go toolchain picks for the host. Dropping an
		// inherited CC is what makes that true whatever shell started this.
		return without(env, "CC"), nil
	}
	cc, err := cCompiler(runtime.GOOS, runtime.GOARCH)
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

// glibcFloor is the oldest glibc a published Linux binary may need.
//
// A release has to start on distributions older than the machine that built it, so the
// floor is named here rather than inherited from whatever the build host happens to
// ship. 2.31 is Debian oldoldstable (Bullseye) while Debian 13 (Trixie) is stable, so
// the promise is "Debian oldoldstable and anything newer than it". It is one number for
// every architecture: a per-architecture table would be a second thing to keep current
// for no stated gain.
const glibcFloor = "2.31"

// cCompiler is the C compiler cgo uses on this host, target and all.
//
// Zig is the compiler on every platform, so one toolchain decides what the C sources
// are compiled with rather than each host's own idea of it. On Linux it is also told
// which glibc to build against: without that the binary would link against whatever
// the machine that built it has, and refuse to start on anything older.
func cCompiler(goos, goarch string) (string, error) {
	if goos == "linux" {
		triple, err := linuxTriple(goarch)
		if err != nil {
			return "", err
		}
		return "zig cc -target " + triple + "." + glibcFloor, nil
	}
	switch goos {
	case "windows", "darwin":
		return "zig cc", nil
	default:
		return "", fmt.Errorf("no C compiler is configured for %s", goos)
	}
}

// linuxTriple is the target zig is given for a Linux build, already carrying the
// architecture Go was asked for.
func linuxTriple(goarch string) (string, error) {
	switch goarch {
	case "amd64":
		return "x86_64-linux-gnu", nil
	case "arm64":
		return "aarch64-linux-gnu", nil
	default:
		return "", fmt.Errorf("no glibc target is known for linux/%s", goarch)
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
