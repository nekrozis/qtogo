# Compatibility

qtogo aims to be capability-compatible with the established Python tool that
installs Qt SDKs, not byte-for-byte identical. This page describes the build as
it is **today**; a difference is recorded when the feature it belongs to lands,
not in advance.

## Commands

| Command | State |
| --- | --- |
| `qtogo help [topic]`, `-h`, `--help` | implemented; generated from the command tree |
| `qtogo version`, `--version` | implemented; one line, exit 0 |
| anything else, including `list-qt`, `install-qt` and the `*-official` verbs | absent: `unknown command "<word>"`, exit 2 |

A verb that is not implemented is absent rather than a stub, so a typo and an
unbuilt feature look the same on purpose: neither silently does nothing.

## Output

- stdout carries payloads; stderr carries diagnostics.
- `--json` writes machine-readable documents and is honoured even when the
  command line itself failed to parse, so a script always receives a document.
- Text output is for people: `Error: …` followed by `hint: …` when there is a
  suggestion.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | internal error (unclassified) |
| 2 | command-line usage error |
| 3 | version or package not found |
| 4 | network or mirror error |
| 5 | integrity or security error |
| 6 | extraction or filesystem error |
| 7 | relocation error |
| 8 | configuration error |
| 9 | authentication error |
| 130 | interrupted |

"Not found" (3) and "network" (4) are deliberately separate: a script has to be
able to tell a mistyped version from an unreachable server.

## Version directory names

A repository version directory (`qt6_6110`, `qt5_515_preview`) is decoded by
`repository.ParseVersionDirectory`. Where the reference tool accepts a name, this
build decodes it the same way; the rows below are the deliberate differences.

| Directory name | qtogo | reference tool |
| --- | --- | --- |
| the underscored `qt6_7_*` form (`qt6_7_3_arm64_v8a`) | rejected: not a recognised shape | decodes as `6.7.3` |
| a preview token longer than three digits (`qt6_6120_preview`) | rejected: not a recognised shape | decodes as `6.120.0-preview` |
| a preview token of one digit (`qt6_6_preview`) | rejected: not a recognised shape | crashes on an uncaught `ValueError` |
| a minor above what the corpus spells (`qt6_6810`, which is 6.81.0 or 6.8.10) | rejected: the digits admit two readings | decodes as `6.81.0` |

The first row is a decision, not an oversight: the reference tool special-cases
that spelling, but eight live repository target pages and every captured sample
contain no `qt6_7_`, so it is not decoded on faith. A name that does not fit the
rules is an error, never a guess, which means such a form can be added later with
evidence.

The last row is the same principle applied to a number rather than a shape.
`qt6_6810` splits as minor 81 patch 0 or minor 8 patch 10, and nothing in the digits
chooses between them — `qt5_5152` is ambiguous in exactly the same way and does mean
5.15.2. What separates them is that the repositories spell no Qt 6 minor above 12,
so the 6.81.0 reading is refused. The cost is stated in ADR-004 decision 5: a Qt
release with a new minor is refused, visibly, until that bound is widened.

### Keeping the minor bound current

The bound is not a format rule; it is a snapshot of what the repositories spell, and
it has to move when they spell something new. It lives in
`internal/repository/versiondir.go`, and a test beside it asserts that it matches the
corpus **exactly** — so adding a captured name without moving the bound fails the
build, and so does moving the bound with no name to justify it. The number is
evidence, not a setting.

When upstream spells a new minor:

1. Refresh the corpus from the live listing — the maintainer's probe, kept outside
   this repository with the research notes — and try to decode the names it holds
   that the corpus does not. A name the bound refuses says so:
   `the name "qt6_6130" spells minor 13, above the 12 the corpus shows for Qt 6`.
2. Check whether the name is ambiguous under the split rules. Most are not: a new
   patch, or a minor whose digits can only be split one way, needs no change at all.
3. Add the name to `testdata/repository/version-directory-names.txt`, and move
   `maxMinor` to the highest minor the corpus now spells (`6: 12` becomes `6: 13`).
4. Run the tests. The corpus round trip must still hold, and the corpus-versus-bound
   test names anything else that moved.

Writing a version back to a name (`EncodeVersionDirectory`) differs once: for a
5.x release with a zero patch and a one-digit minor, qtogo drops the patch the way
the repositories do — 5.3.0 encodes as `qt5_53`, or `qt5_53_src_doc_examples` with
that extension — and so on through 5.8.0 — while the reference tool drops it only
for 5.9.0 and would write `qt5_530_src_doc_examples`, a directory that exists
nowhere.
