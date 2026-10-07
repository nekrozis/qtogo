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

The build driver is a Go program, so it needs nothing beyond the Go toolchain and the
same command works everywhere:

```sh
go run ./bootstrap build        # writes bin/qtogo
go run ./bootstrap check        # gofmt check, go vet, tests, golangci-lint
go run ./bootstrap test -race   # tests with the race detector
```

The extractor is C, so the driver names the compiler cgo needs —
[zig](https://ziglang.org/) on every platform — and always builds for the host. `-race`
needs tsan, which zig does not provide, so it uses the host's own compiler.

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
