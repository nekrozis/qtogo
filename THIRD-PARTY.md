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
absent, and the manifest test fails if anything outside that list appears.
