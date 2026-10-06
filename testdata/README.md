# Test data

The fixtures here are **hand-written**: small pages built around the repository
model this project supports, carrying only the fields a parser reads and never a
real URL, so that a test is offline by construction and cannot reach a server.

```text
repository/   one directory per layout or failure mode the parsers handle
upstream/     cases taken from another project, with attribution
```

A case taken from another project belongs under `upstream/`, with its licence,
source and a pinned version recorded in the repository's `THIRD-PARTY` file. Those
cases exist to catch regressions on real-world data; the tests for the repository
model itself must not depend on them.
