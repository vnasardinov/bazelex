# TODO

Beacon candidates for the `internal/quotient` package (`Graph`,
`Partition`/`Collapse`, `ComponentIndex`/`ComponentNames`/`Components`,
`Condense`), found by scanning the package for places where a reader has to work
out what the code does. A beacon is a name, idiom or shape that lets a reader
confirm intent without reading every line. Nothing here changes behavior; each
entry is a rewrite candidate. Files are named relative to `internal/quotient/`
unless a full path is given. `engine-TODO.md` owns `internal/engine/`; the rows
here for the two `internal/ui/graph*` files stay because the checked entry below
cites them.

## Revision anchors

Each candidate's line numbers were verified at the "scanned" revision shown
below; "now" is the revision they were last re-checked against. If a file's two
hashes differ, re-check that file's entries.

| file | blob hash scanned | blob hash now |
| --- | --- | --- |
| `internal/quotient/graph.go` | `08997bc4f0b34448e8fe7bf04a8945cc8ac28b74` | `b4068c162ee732cde64db6bf1b7fa485afed3c27` |
| `internal/quotient/partition.go` | `c218903cbfa3c61325e0b917ecd86c8446168e56` | `75fc96c167e7325b0256f01e9cd18aa1091dc506` |
| `internal/quotient/scc.go` | `2a3fa70488ab2ffb5530387fca5b030392eb30ec` | `c41601982da9b186a83b246fcfc5d5e27d98e440` |
| `internal/quotient/quotient_test.go` | `55ba946cf213b6205f27d282ba6d70b3a5fcae9b` | `a0b8e0797631303c6a34a8d6acde848c3a40f750` |
| `internal/ui/graph.go` | `3d6798a686f3e8d0cccf08ad1b4273ef3c8dd61d` | `81c3181bd3918bb78a1967d535d303188dd1faa4` |
| `internal/ui/graph_test.go` | `e9788fa805af21975260fe5090fc594bf846fbb6` | `16364dfc715df411cfd9794c09beea2095432284` |

`internal/ui/graph_test.go` has moved since the scan: its fixtures grew the
quotient tables the engine reads back, which shifted the entry below. Everything
else has moved as its candidates landed, but those moves were renames and
same-line edits, so the recorded line numbers still hold; the `Layers` work
moved `partition.go` and `quotient_test.go`, and `/graph?by=scc` moved
`internal/ui/graph.go`. Re-check a file's entries if its hash has changed since
this table was written.
`internal/engine/` is anchored in `engine-TODO.md`, whose scan took over those
rows; `internal/ui/` as a whole is anchored in `ui-TODO.md`.

## Beacon candidates

None outstanding: the twelve candidates found in the scan are landed, and so is
the one the scan turned up in the UI (`noPackageError` → `nothingToDrawError`).
The sections below record what is left to know.

## Checked, not a candidate

-   `internal/ui/graph.go:307` — the page asks whether a package exists and
    reads false for a package that is a node with no cross-package edge. That is
    what `nothingToDrawError` documents and its message states ("has no
    dependencies on or from other packages"), and `internal/ui/graph_test.go:56`
    asserts it, so it is intended: a package with nothing to draw is reported
    rather than drawn. Earlier noted as a bug while renaming `Has`; not one.

-   The one message covers both causes (a package that is not in the graph and
    one with no cross-package edges). Splitting it into two is a wording choice
    for whoever reads the page, not a beacon, so it is left as it is.
