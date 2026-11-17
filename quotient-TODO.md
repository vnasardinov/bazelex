# TODO

Beacon candidates for the `internal/quotient` package (`Graph`, `Partition`,
`ComponentIndex`/`Condense`), found by scanning the package for places where a
reader has to work out what the code does. A beacon is a name, idiom or shape
that lets a reader confirm intent without reading every line. Nothing here
changes behavior; each entry is a rewrite candidate. Files are named relative to
`internal/quotient/` unless a full path is given.

## Revision anchors

Each candidate's line numbers were verified at the "scanned" revision shown
below; "now" is the revision they were last re-checked against. If a file's two
hashes differ, re-check that file's entries.

| file | blob hash scanned | blob hash now |
| --- | --- | --- |
| `internal/quotient/graph.go` | `08997bc4f0b34448e8fe7bf04a8945cc8ac28b74` | `1b6cad1f1b03e79acb56e3de642b4d32c6645504` |
| `internal/quotient/partition.go` | `c218903cbfa3c61325e0b917ecd86c8446168e56` | `28ecc8dad5ede1d4508174a67a6085ebaa18ded0` |
| `internal/quotient/scc.go` | `2a3fa70488ab2ffb5530387fca5b030392eb30ec` | `109ffe5f3add40e3cbd4c7d98d012aa59e1fde8f` |
| `internal/quotient/quotient_test.go` | `55ba946cf213b6205f27d282ba6d70b3a5fcae9b` | `07ace06a94f6e0f26b44a86a35702630e1f4f8da` |
| `internal/engine/quotient.go` | `cce7015316bc53d541bbef46c9451983db149b59` | `cce7015316bc53d541bbef46c9451983db149b59` |
| `internal/engine/quotient_test.go` | `e3b175311cf0bb550b76911e2da96a6946737571` | `4c8e8f96848aef356f8e828354f199bd84e43e96` |
| `internal/engine/datalog.go` | `f01cac896f8cd1a1a230dd36da8f13ae398da41e` | `df9a600c83fef0433fc1f94491a1b5cdd56c2742` |
| `internal/ui/graph.go` | `3d6798a686f3e8d0cccf08ad1b4273ef3c8dd61d` | `da7a4fe41821133efced7e48a6ec0500a71adeb3` |

`graph.go`, `partition.go`, `scc.go` and the call sites of the renames have all
moved as their candidates landed, but the moves were renames and same-line
edits, so the recorded line numbers still hold. If a file's hash has changed
since the "now" column was written, re-check that file's entries;
`internal/engine/quotient.go` is untouched.

## Beacon candidates

-   `partition.go:14` (`Of(g, part)`) — the verb is a preposition, so the doc has
    to restate the job ("returns the quotient of g by part") and callers read
    `quotient.Of(g, ...)`. Rename to `Collapse` (or `ByPartition`). The call
    site says what happens to the graph, and `Condense` keeps reading as the
    special case it is.

-   `partition.go:32-34` (`Condense`) — the partition is an inline closure over
    the `ComponentIndex` map, and `internal/engine/quotient.go:49-50` hand-rolls
    the same closure because it needs the map too. Give the map a named method
    (e.g. `func (ci ComponentIndex) Partition(node string) string`, with
    `ComponentIndex` as a named type) and use it in both places. `Condense` then
    becomes `Of(g, index.Partition)` and the engine reuses the beacon instead of
    duplicating it.

-   `internal/ui/graph.go:218` (found while renaming `Has`) — the page asks
    whether a package exists with `g.HasEdges(k.pkg)`, so a package that is a
    node but has no cross-package edge (like the fixture in
    `internal/engine/quotient_test.go:58-70`) is reported as missing. Give
    `Graph` a membership accessor (`HasNode`) and use it here. The call site then
    asks the question it means, and the UI stops hiding edge-less packages; the
    result changes for those packages, so it wants its own test.
