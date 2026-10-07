# Third-party code

Code in this repository that was written by someone else, where it came from, and
what its licence asks of us. Test fixtures taken from another project are recorded
here too, since they carry the same obligations.

## LZMA SDK (ANSI-C decoder)

| | |
| --- | --- |
| Source | <https://www.7-zip.org/sdk.html> — `lzma2604.7z` from the 7-Zip 26.04 release |
| Version | 26.04, recorded in `internal/lzma/VERSION` |
| Licence | public domain, by Igor Pavlov; PPMd carries Dmitry Shkarin's public-domain code |
| Location | `internal/lzma/` |
| Purpose | decoding 7z archives, which is what the Qt repositories ship |

The files listed in `internal/lzma/manifest.txt` are the upstream decoder, byte for
byte. The other files in that directory are ours: `VERSION`, `LICENSE` and the
manifest record the provenance, `glue.c` and `glue.h` are the bridge that makes the
C callable from Go, and the tests keep all of it in step. Only the ANSI-C decoder
is taken — the SDK's C++ implementation, the encoders, `Asm/` and `Util/` are
absent. The manifest test fails if anything beyond the list appears in that
directory, and a check in CI fails if a `CPP/7zip` path appears anywhere in the
tree.

## Updating the vendored sources

`go run ./tools/vendor/lzma` compares the vendored files with an unpacked SDK,
copies a new version over them, and shows what changed between two upstream
releases. It reads a directory the maintainer already has:

```
go run ./tools/vendor/lzma verify <sdk-dir>            # do the vendored files match upstream?
go run ./tools/vendor/lzma diff <old-sdk> <new-sdk>    # what would an upgrade bring?
go run ./tools/vendor/lzma update 26.05 <sdk-dir>      # take it
```

`update` refuses to run unless the directory it copies from holds every file
`manifest.txt` names, so a partial or wrong directory cannot quietly become the
vendored sources. It does not check the vendored files against upstream first — an
update exists precisely because the two differ. It then rewrites `VERSION`. After
it, run the tests, read the diff, and add to `manifest.txt` anything the tool
reports as newly reachable from the taken files.

**Nothing is downloaded, by the tool or by the build.** A vendored source file is
part of the repository, and a build that fetched one would depend on a release
archive still being online — which is not a property this project controls. For the
same reason the tool is not part of CI: the ordinary check that the directory and
the manifest agree is a Go test, which needs no upstream copy.
