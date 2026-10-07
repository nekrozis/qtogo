# Test data

The fixtures here are mostly **hand-written**: small pages built around the
repository model this project supports, carrying only the fields a parser reads
and never a real URL, so that a test is offline by construction and cannot reach
a server.

The exception is `repository/version-directory-names.txt`: one directory name
per line, extracted from captured listing pages because the acceptance criterion
for version-directory encoding (ADR-004) is about the names that exist, which
cannot be invented. It carries names only, never a URL or a page.

```text
repository/   one directory per layout or failure mode the parsers handle,
              plus the captured name list described above
archive/      small 7z archives the extractor reads
upstream/     cases taken from another project, with attribution
```

A `repository/*` directory holds either a directory-listing page (`index.html`)
or repository metadata (`Updates.xml`).

The `archive/*` files are ours too, built with `7za a` (7-Zip 26.04) from files
written for the purpose:

- `plain.7z` — two files under `docs/`, non-solid, with the header stored
  uncompressed (`-mhc=off -ms=off -m0=LZMA2`). The uncompressed header is
  deliberate: it leaves the entry names in the file as plain UTF-16, so a test can
  rewrite one in place — fixing the two header checksums that cover it — and hand
  the extractor an archive that tries to escape the destination directory. That is
  how the path checks get a real archive to refuse rather than a structure built by
  hand.
- `solid.7z` — three files sharing one compressed stream (the default settings),
  which is the shape Qt's own archives have.
- `tree.7z` — a directory entry and an empty file, so both are covered.

Nothing under `archive/` is downloaded, and regenerating them takes only `7za a`
with the switches above.

A case taken from another project belongs under `upstream/`, with its licence,
source and a pinned version recorded in the repository's `THIRD-PARTY` file. Those
cases exist to catch regressions on real-world data; the tests for the repository
model itself must not depend on them.
