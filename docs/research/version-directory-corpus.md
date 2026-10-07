# Version directory corpus

The evidence behind the repository version-directory rules. The rules and their
reasoning are in
[ADR-004](https://github.com/nekrozis/qtogo/wiki/ADR-004-Version-directory-encoding);
this page holds the data, so the measurements can be redone without reopening the
decision.

## The sample

`testdata/repository/version-directory-names.txt` — 269 names, one per line, taken
from captured listing pages of the Qt repositories. Names only: the file carries no
URL and no page, which is what keeps the tests offline.

## Method

- Names were read from captured listing pages — one page per host and target — and
  never invented.
- The distribution below was then checked against live pages on the Windows, Linux
  and macOS desktop targets, so no rule rests on a stale capture.
- The acceptance criterion is mechanical: every captured name must decode as a
  version directory and encode back to itself, and the check runs in the unit suite.

## What the sample shows

- **269 names.** 262 decode as version directories; the other 7 are the `qt6_dev`
  channel (`qt6_dev`, `qt6_dev_src_doc_examples`, `qt6_dev_wasm`, and so on), which
  is not a version.
- **Releases of Qt 5 with a one-digit minor and no patch drop the patch.** 5.3.0
  through 5.9.0 appear as `qt5_53` … `qt5_59`, and 5.3.0–5.8.0 appear **only** with
  the `_src_doc_examples` extension — there is no bare `qt5_53` and no `qt5_530` on
  any of the desktop targets.
- **5.10.0, 5.12.0 and 5.15.0 keep every digit** (`qt5_5100`, `qt5_5120`,
  `qt5_5150`). Dropping the patch would write `510`, which reads as the
  one-digit-patch form or collides with the 5.10 preview.
- **Qt 6 releases never drop the patch**: `qt6_600`, `qt6_680`, `qt6_690`,
  `qt6_6110`.
- **A preview carries major and minor only**: `qt6_62_preview`, `qt5_513_preview`.
  The suffix is what separates `qt6_620` from `qt6_62_preview`, so both spellings
  decode to 6.2.0 and only the suffix tells them apart.
- **The underscored Qt 6 form is absent.** `qt6_7_3_arm64_v8a` appears in no
  captured sample and on none of the eight live target pages checked, though the
  reference tool special-cases it.
- **The extension vocabulary drifts**: `qt6_673_msvc2022` sits beside
  `qt6_6110_msvc2022_64` — same host and target, different spelling. An extension
  is therefore taken from the caller verbatim, never derived from a table.
- **Nesting is not stable either**: a version directory may hold the metadata
  itself, a same-named child, or `<version>_<extension>` children. Path shapes are
  discovered, not assumed.
- **The highest minor each major spells**: Qt 5 reaches 15, Qt 6 reaches 12, and the
  largest patch seen is 12 (`qt5_51210`). Those ranges are what the decoder uses to
  settle a token whose digits admit two splits — `qt5_5152` is 5.15.2 rather than
  5.1.52, and `qt6_6810` is refused because 81 is above what Qt 6 spells. The bound
  is a property of the naming scheme, not a list of released versions; ADR-004
  decision 5 carries the reasoning and the cost.

## How the reference tool reads these names

The reference implementation decodes a directory identifier positionally, and says so
in its own comment (`aqt/metadata.py`, `get_semantic_version`) rather than citing a
specification:

> If the version is 3 digits, then major, minor, and patch each get 1 digit. If the
> version is 4 or more digits, then major gets 1 digit, minor gets 2 digits and patch
> gets all the rest. As of May 2021, the version strings at
> https://download.qt.io/online/qtsdkrepository conform to this pattern; they are not
> guaranteed to do so in the future.

So it reads `qt6_6810` as 6.81.0: it never considers the other split, and it has no
rule reserving or bounding a future minor. Its own tests record only names that have
been observed (`51212` → 5.12.12, `600` → 6.0.0, `6_7_3` → 6.7.3). It also accepts the
underscored `6_7_3` form, which this project refuses.

What makes the ambiguity survivable there is where the name is read: that tool builds
the directory it wants from the version it was given, and takes a package's version
from the `Updates.xml` beside it (`<Version>6.11.0-0-202603180535</Version>` inside
`qt6_6110`). Either way the name is an index and the metadata is canonical.

That tool's version knowledge sits mostly in the layout rather than in the names:
about thirty hard-coded thresholds across its source (`>= 6.8.0` for the extra
nesting level, `>= 6.11.0` for the Windows architecture split,
`ANDROID_SHARED_FOLDER_VER = 5.15.2`, and so on). Those measure the layout, not this
corpus, and are noted only to keep the comparison honest: a new nesting level is a
threshold for that tool and a probe for this one.

## Re-measuring

- The mechanical half lives in the repository: append names to the corpus file and
  run `go test ./internal/repository/`. A name the rules do not cover fails
  `versiondir_test.go`, and `FuzzVersionDirectoryRoundTrip` re-checks the
  round trip. Adding a capture is therefore the way to test a rule change.
- The capture half is deliberately out of tree: the probe programs that read live
  listing pages are kept with the project's research notes, and they record names
  only — never a page or a URL.
- If a rule and the live pages disagree, the pages win and the decision record is
  amended. This page exists so that amendment costs a measurement, not an
  argument.
