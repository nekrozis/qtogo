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
upstream/     cases taken from another project, with attribution
```

A `repository/*` directory holds either a directory-listing page (`index.html`)
or repository metadata (`Updates.xml`).

A case taken from another project belongs under `upstream/`, with its licence,
source and a pinned version recorded in the repository's `THIRD-PARTY` file. Those
cases exist to catch regressions on real-world data; the tests for the repository
model itself must not depend on them.
