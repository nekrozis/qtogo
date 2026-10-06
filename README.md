# qtogo

Installs and manages Qt SDKs from the official online repositories.

It aims to be capability-compatible with the established Python tool that does
the same job, with a deliberately different security and relocation posture.
Every difference is listed in [docs/compatibility.md](docs/compatibility.md).

## Status

Early. This build implements `version` and `help` and nothing else. A command
that is not implemented is absent rather than present-and-failing, so
`qtogo list-qt` reports an unknown command instead of pretending to work.

## Build and test

Linux and macOS:

```sh
make build      # writes bin/qtogo
make check      # gofmt check, go vet, tests, golangci-lint
make test-race  # tests with the race detector
```

Windows, using the Go commands directly:

```powershell
go build ./...
go test ./...
go vet ./...
gofmt -l .      # must print nothing
```

CGO is used for the archive extractor, which on Windows is built with
[zig](https://ziglang.org/)'s C compiler:

```powershell
$env:CGO_ENABLED = '1'
$env:CC = 'zig cc'
go build ./...
```

The race detector needs tsan symbols, which `zig cc` does not provide, so on
Windows `-race` runs in CI rather than locally.

## Using it

```sh
qtogo help                 # the commands this build has
qtogo version              # one line
qtogo version --json       # the same, machine-readable
```

Failures are classified: the exit code says whether the request was wrong, the
version was missing, or the network failed, and `--json` writes the same
classification as a document. The codes are listed in
[docs/compatibility.md](docs/compatibility.md).

## Documentation

- [docs/compatibility.md](docs/compatibility.md) — what this build does, and
  where it differs from the reference implementation.

## Licensing

BSD-3-Clause. See [LICENSE](LICENSE): use it freely, keep the copyright notice
and the disclaimer.

Third-party sources stay out of the repository, with one exception: attributed
offline fixtures used as interoperability regressions.
