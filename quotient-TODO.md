# TODO

Beacon candidates for the `internal/quotient` package (`Graph`, `Partition`,
`ComponentIndex`/`Condense`), found by scanning the package for places where a
reader has to work out what the code does. A beacon is a name, idiom or shape
that lets a reader confirm intent without reading every line. Nothing here
changes behavior; each entry is a rewrite candidate. Files are named relative to
`internal/quotient/` unless a full path is given.

## Revision anchors

Every line number below was verified against, and is pinned to, these revisions
(git blob hashes at commit `a5441f0` "engine: add a quotient package and SCC
condensation"). The working tree matches them exactly at the time of writing
(`git hash-object` equals `git rev-parse HEAD:<file>` for each), so a mismatch
means the file moved and the line numbers need re-checking.

Scanned package:

| file | blob hash | lines |
| --- | --- | --- |
| `internal/quotient/graph.go` | `08997bc4f0b34448e8fe7bf04a8945cc8ac28b74` | 183 |
| `internal/quotient/partition.go` | `c218903cbfa3c61325e0b917ecd86c8446168e56` | 35 |
| `internal/quotient/scc.go` | `2a3fa70488ab2ffb5530387fca5b030392eb30ec` | 102 |

Supporting call sites the entries refer to:

| file | blob hash |
| --- | --- |
| `internal/quotient/quotient_test.go` | `55ba946cf213b6205f27d282ba6d70b3a5fcae9b` |
| `internal/engine/quotient.go` | `cce7015316bc53d541bbef46c9451983db149b59` |
| `internal/engine/quotient_test.go` | `e3b175311cf0bb550b76911e2da96a6946737571` |
| `internal/engine/datalog.go` | `f01cac896f8cd1a1a230dd36da8f13ae398da41e` |
| `internal/ui/graph.go` | `3d6798a686f3e8d0cccf08ad1b4273ef3c8dd61d` |

## Beacon candidates

-   `graph.go:80` (`Dep.N`) — the field's meaning ("the edge's weight", per the
    doc on line 77) is only in the comment, and read sites say `d.N`
    (`graph.go:91` builds it; `partition.go:21`, `internal/engine/datalog.go:158`,
    `internal/ui/graph.go:67` twice) while tests write `Dep{N: 1}`
    (`quotient_test.go:50,101,104,116,119`,
    `internal/engine/quotient_test.go:72-75,94`). Rename to `Weight`. The reader then
    knows the value's meaning at every use site, not just at the declaration.

-   `graph.go:61` (`Has`) — the name promises "is a node", but the body reports
    having an edge in either direction, so `AddNode`-only nodes answer false.
    Rename to `HasEdges` (or make it cover `g.nodes`). Either way the answer to
    "does this graph contain x?" stops depending on which method the reader
    picked.

-   `graph.go:43-58` (`Add`) — the reader has to decode the anonymous struct
    literal `{{g.out, from, to}, {g.in, to, from}}`, whose fields `from`/`to`
    shadow the parameters. Extract `func (g *Graph) addDirected(adj
    map[string]map[string]int, from, to string, weight int)` and call it twice
    (`g.out, from, to` and `g.in, to, from`). The two directions then state
    themselves instead of being inferred from a swapped tuple.

-   `graph.go:104-111` (`successors`) vs `graph.go:85-95` (`Deps`) — two accessors
    for the same list, differing in return type (`[]string` / `[]Dep`) and sort
    call (`slices.Sort` / `slices.SortFunc`). Define one in terms of the other
    and name the cheap one for what it is (e.g. `successorNames`). The reader can
    then rely on one pattern instead of checking which flavor a call site needs.

-   `graph.go:114` (`reach(adj, node)`) — `adj` is vague where the map is the
    graph's out- or in-adjacency, chosen by the caller (`reach(g.in, node)`).
    Rename to `reachable(adjacency, node)`, and consider returning a named set
    type (`map[string]bool`) so `Component`'s two calls read as a set
    intersection. The direction being walked becomes visible at the call site.

-   `graph.go:49,134,136,171` — "both directions" is spelled three different
    ways: the struct literal in `Add`, two `reach` calls in `Component`, and the
    slice `[]map[string]map[string]int{g.out, g.in}` in `Neighborhood`. Add one
    named helper (e.g. `func (g *Graph) adjacency() []map[string]map[string]int`
    or `neighbors(n)` returning both directions) and use it in all three. Each
    site then reads as "walk the graph undirected" rather than as map plumbing.

-   `scc.go:26,37` — two stacks are in play under near-synonymous names: `stack`
    is Tarjan's component stack, `work` is the DFS frame stack. Rename to
    `compStack` and `frames` (or `dfs`). The reader can then tell at a glance
    which pop belongs to which algorithm without re-reading the comments.

-   `scc.go:17-21` (frame field `next int`) — the comment has to explain that
    `next` is "how far we have walked"; the field is an index into `succ`. Name
    it `succIx` (or `cursor`). The comment can then go away, and `f.next++`
    reads as advancing through successors.

-   `scc.go:27` (`counter`) — vague, where the maps document it as "discovery
    order". Rename to `nextIndex` (or `clock`). The one name then carries
    Tarjan's discovery-index invariant, so the reader stops tracking which of
    `index`/`low`/`counter` moves.

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

-   `scc.go:91` (`byName`) — the name does not say the values are component
    members grouped by component name. Rename to `membersByName`. The grouping
    that `Components` performs is then visible without reading the append.
