# TODO

Beacon candidates for the `internal/ui` package (the web front-end: the canned
queries in `queries.go`, the handler and pagination in `server.go`, the package
graph page in `graph.go` and the Datalog page in `datalog.go`), found by scanning
the package for places where a reader has to work out what the code does. A
beacon is a name, idiom or shape that lets a reader confirm intent without
reading every line. Nothing here changes behavior; each entry is a rewrite
candidate. Files are named relative to `internal/ui/`.

`quotient-TODO.md` holds an earlier scan that recorded one `internal/ui/graph*`
entry (the `nothingToDrawError` check below) and anchored those two files. This
file owns every `internal/ui/` file, so their anchors and entries live here now;
the checked entry is carried over so the next reader does not raise it again.

## Revision anchors

Each entry's line numbers were verified at the "scanned" revision shown below;
"now" is the revision they were last re-checked against. If a file's two hashes
differ, re-check that file's entries.

| file | blob hash scanned | blob hash now |
| --- | --- | --- |
| `internal/ui/server.go` | `6c05bb2254577de59f3ba2d488e84799ab730a3e` | `42a7a9d32b6984cec9197c8861ffaabf4def6161` |
| `internal/ui/queries.go` | `c6e692ba9d2563849a647537baad64d23546010d` | `16eaa368c949e198490499e28ece6c0b47ff9ada` |
| `internal/ui/graph.go` | `1f7655703e4a136fdecee00250189dee096af309` | `81c3181bd3918bb78a1967d535d303188dd1faa4` |
| `internal/ui/datalog.go` | `34d6059fba80cdfdfcc263927f8d3ac2008dfacb` | `34d6059fba80cdfdfcc263927f8d3ac2008dfacb` |
| `internal/ui/index.html` | `ca4427e7571d0dc1a745c74706f5f64a86a5c1ad` | `ca4427e7571d0dc1a745c74706f5f64a86a5c1ad` |
| `internal/ui/graph.html` | `46be89c263cc726cd60fe9a8ff8074f268969758` | `f91ca0d454c5c64d231b3d4d66c830e6b2b35657` |
| `internal/ui/datalog.html` | `bc84c4dc71de8529d56a423a133286b71a7a891d` | `bc84c4dc71de8529d56a423a133286b71a7a891d` |
| `internal/ui/server_test.go` | `d16a4c05e7cbd2365eef75a1bfacb7a1a550b51e` | `44c2a9f0f4719571f14d824b9143e0ec6b41e863` |
| `internal/ui/graph_test.go` | `e9788fa805af21975260fe5090fc594bf846fbb6` | `16364dfc715df411cfd9794c09beea2095432284` |
| `internal/ui/datalog_test.go` | `aef9cb5245f6da956f1221fd9505c18ecefc40d6` | `aef9cb5245f6da956f1221fd9505c18ecefc40d6` |

The scan ran at commit `d51a00cfcc38d02a5dbe7eec53f0ba5775166190`. A "now" hash
that differs from its scanned hash has been re-checked since, and its entries'
line numbers are current for it. `graph_test.go` was re-checked after its
fixtures grew the quotient tables the engine reads back, which shifted the three
entries below. `graph.go`, `graph.html`, `graph_test.go` and `server.go` were
re-checked again when `/graph?by=scc` was added, which moved the `graph.go` and
`graph_test.go` lines.

## Beacon candidates

None outstanding: all six candidates are landed. The sections below record
what is left to know.

## Checked, not a candidate

-   `graph.go:67` — `in := map[string]bool{}` as a set is the codebase's idiom
    (`internal/graphdb/load.go:186`, `internal/quotient/graph.go:22,125,170`,
    `internal/quotient/scc.go:34`), so it is the pattern a reader can rely on, not
    a one-off.

-   `graph.go:307` — the page asks whether a package exists and reads false for a
    package that is a node with no cross-package edge. That is what
    `nothingToDrawError` documents and its message states ("has no dependencies on
    or from other packages"), and `graph_test.go:56` asserts it, so it is
    intended: a package with nothing to draw is reported rather than drawn.
    Carried over from `quotient-TODO.md`, which raised it while renaming `Has`.

-   `server.go:162-173` — the goroutines capture `i` and `q` from the range. The
    module's `go 1.26` directive (go.mod:3) guarantees per-iteration loop
    variables, and `views`' doc comment (server.go:152-157) states the two-phase
    scheduling, so there is no stale-capture trap to flag.

-   `graph.go:240-266` (`drawingCache`) — eviction is insertion order, not
    least-recently-used, and `get` (graph.go:246-250) deliberately does not
    refresh the order. The type comment says "evicting the oldest first" and
    `TestDrawingCache` (graph_test.go:193-202) asserts it, so the FIFO policy is
    intended rather than an accidental non-LRU.

-   `queries.go:61,79,115` — `RowLink.ToQuery` names another query's `ID`
    (`"longest-chain"`, `"transitive-deps"`, `"impact"`) as a bare string, with no
    compile-time link, so a typo would silently break the link. The rendered hrefs
    are asserted (server_test.go:138,154,168; graph_test.go:248), so the links are
    pinned behavior; a typed ID is a design change, not a missing beacon.

## Deferred

None outstanding.
