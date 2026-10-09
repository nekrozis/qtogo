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
  rewrite one in place — fixing the two header checksums that cover it — and so hand
  a path check an archive that tries to escape the destination directory, rather
  than a structure built by hand.
- `solid.7z` — three files sharing one compressed stream (the default settings),
  which is the shape Qt's own archives have.
- `tree.7z` — a directory entry and an empty file, so both are covered.
- `qtree.7z` — a minimal Qt 5 tree: `<version>/<arch>/bin/qmake.exe` (the marker an
  installed tree is found by), a `lib/*.prl` carrying a build prefix, and a
  `mkspecs/qconfig.pri` with the edition lines. It lets an install test run the
  whole chain — extract, locate the tree, relocate — without a real 88 MB download.
- `backslash.7z` — built like `plain.7z`, then rewritten in place so its separators
  are `\`: `sub\nested\f.txt`, and a `..\escape.txt` that tries to leave the
  destination.
- `symlink.7z` — built on Linux, so it carries Unix attributes: `lib/` holds a
  library chained by symlink (`libfoo.so` → `libfoo.so.1` → the real file) and
  `sub/inside-link` points at its sibling. Every target is a plain relative path.
  It also pins the modes — a 0755 directory and 0644 files — which only the
  attribute word can carry.
- `symlink-escape.7z` — the same shape with one link whose target leaves the
  destination (`lib/evil` → `../../outside.txt`), which an extractor has to refuse.

A 7z name normally separates its path segments with `/`, but the format also allows
`\`. The archiver here only writes `/`, so a fixture that pins a backslash name
down has to be built with the same in-place edit and checksum repair.

Nothing under `archive/` is downloaded. The DOS-attribute archives regenerate with
`7za a` and the switches above; `backslash.7z` additionally needs the rename and
checksum repair. The two `symlink*` fixtures need a Unix archiver instead — the
links and the Unix modes only exist there — and were built with `ln -s` and
`7za a -snl` in a Linux container.

A case taken from another project belongs under `upstream/`, with its licence,
source and a pinned version recorded in the repository's `THIRD-PARTY` file. Those
cases exist to catch regressions on real-world data; the tests for the repository
model itself must not depend on them.
