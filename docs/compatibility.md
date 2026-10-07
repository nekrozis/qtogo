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

The first row is a decision, not an oversight: the reference tool special-cases
that spelling, but eight live repository target pages and every captured sample
contain no `qt6_7_`, so it is not decoded on faith. A name that does not fit the
rules is an error, never a guess, which means such a form can be added later with
evidence.
