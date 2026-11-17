# TODO

Beacon candidates for the `internal/quotient` package (`Graph`,
`Partition`/`Collapse`, `ComponentIndex`/`ComponentNames`/`Components`,
`Condense`), found by scanning the package for places where a reader has to work
out what the code does. A beacon is a name, idiom or shape that lets a reader
confirm intent without reading every line. Nothing here changes behavior; each
entry is a rewrite candidate. Files are named relative to `internal/quotient/`
unless a full path is given.

## Revision anchors

Each candidate's line numbers were verified at the "scanned" revision shown
below; "now" is the revision they were last re-checked against. If a file's two
hashes differ, re-check that file's entries.

| file | blob hash scanned | blob hash now |
| --- | --- | --- |
| `internal/quotient/graph.go` | `08997bc4f0b34448e8fe7bf04a8945cc8ac28b74` | `b4068c162ee732cde64db6bf1b7fa485afed3c27` |
| `internal/quotient/partition.go` | `c218903cbfa3c61325e0b917ecd86c8446168e56` | `6c372301c8a25a619b681b936d3514b015a0eac5` |
| `internal/quotient/scc.go` | `2a3fa70488ab2ffb5530387fca5b030392eb30ec` | `c41601982da9b186a83b246fcfc5d5e27d98e440` |
| `internal/quotient/quotient_test.go` | `55ba946cf213b6205f27d282ba6d70b3a5fcae9b` | `07969c278e88d22c7984e455e61ffddb991db12a` |
| `internal/engine/quotient.go` | `cce7015316bc53d541bbef46c9451983db149b59` | `480cbcb736d449a766d88891b49fdbd53dddda3e` |
| `internal/engine/quotient_test.go` | `e3b175311cf0bb550b76911e2da96a6946737571` | `4c8e8f96848aef356f8e828354f199bd84e43e96` |
| `internal/engine/datalog.go` | `f01cac896f8cd1a1a230dd36da8f13ae398da41e` | `df9a600c83fef0433fc1f94491a1b5cdd56c2742` |
| `internal/ui/graph.go` | `3d6798a686f3e8d0cccf08ad1b4273ef3c8dd61d` | `1f7655703e4a136fdecee00250189dee096af309` |
| `internal/ui/graph_test.go` | `e9788fa805af21975260fe5090fc594bf846fbb6` | `e9788fa805af21975260fe5090fc594bf846fbb6` |

`internal/engine/quotient_test.go`, `internal/engine/datalog.go` and
`internal/ui/graph_test.go` are untouched since the scan; everything else has
moved as its candidates landed, but the moves were renames and same-line edits,
so the recorded line numbers still hold. Re-check a file's entries if its hash
has changed since this table was written.

## Beacon candidates

None outstanding: the twelve candidates found in the scan are landed, and so is
the one the scan turned up in the UI (`noPackageError` → `nothingToDrawError`).
The sections below record what is left to know.

## Checked, not a candidate

-   `internal/ui/graph.go:218` — the page asks whether a package exists and
    reads false for a package that is a node with no cross-package edge. That is
    what `nothingToDrawError` documents and its message states ("has no
    dependencies on or from other packages"), and `internal/ui/graph_test.go:48`
    asserts it, so it is intended: a package with nothing to draw is reported
    rather than drawn. Earlier noted as a bug while renaming `Has`; not one.

-   The one message covers both causes (a package that is not in the graph and
    one with no cross-package edges). Splitting it into two is a wording choice
    for whoever reads the page, not a beacon, so it is left as it is.
