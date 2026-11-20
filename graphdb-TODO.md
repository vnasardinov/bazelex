# TODO

Beacon candidates for the `internal/graphdb` package (`Schema`, `Load` and
the loader's helpers), found by scanning the package for places where a
reader has to work out what the code does. A beacon is a name, idiom or shape
that lets a reader confirm intent without reading every line. Nothing here
changes behavior; each entry is a rewrite candidate. Files are named relative
to `internal/graphdb/`.

`engine-TODO.md` cites `internal/graphdb/schema.go:12` for the `kind`
vocabulary; this file owns the `internal/graphdb/` files, so their anchors and
entries stay here.

## Revision anchors

Each entry's line numbers were verified at the "scanned" revision shown
below; "now" is the revision they were last re-checked against. If a file's two
hashes differ, re-check that file's entries.

| file | blob hash scanned | blob hash now |
| --- | --- | --- |
| `internal/graphdb/load.go` | `0eb0c581e10b1ff5af39262c8fe01126796d759e` | `d0e3f17c3c0a73a70089087bf3bb79918edd1b71` |
| `internal/graphdb/schema.go` | `a242c30a4a20eab8e9da26c78a730dee96974183` | `a242c30a4a20eab8e9da26c78a730dee96974183` |
| `internal/graphdb/load_test.go` | `db15716cbd137cabc460ee5b70560bcc8a405ab3` | `db15716cbd137cabc460ee5b70560bcc8a405ab3` |

The scan ran at commit `c1e35948f23dc7d37ad7e2988656fc4e1141b13f`, and the
working tree matches it, so "scanned" and "now" are the same for every file.

## Beacon candidates

-   `load_test.go:16` — `labelAttr` builds every test attribute, not only label
    ones: it is called for `name` (STRING), `tags` (STRING_LIST) and `size`
    (STRING) as well as `deps`/`srcs`/`visibility` (LABEL_LIST). Rename it
    `newAttr` (the package's `attr` type rules out `attr`): the call sites then
    name the type they pass instead of promising a label.

-   `load_test.go:138`, `load_test.go:178` — `reflect.DeepEqual` where the
    engine and quotient tests use `slices.Equal`; `[]row` and `[][3]string` are
    comparable, so `slices.Equal(got, want)` is the repo's idiom and drops the
    `reflect` import. A reader who knows the pattern confirms the comparison
    without checking that the element type is comparable.

-   `load.go:73` — `bufio.NewReaderSize(r, 1<<20)` is the package's only magic
    number. Give it a name, e.g. `readBufferSize = 1 << 20` with a word on why
    that size, so the reader does not have to decode the shift to learn that
    this is the stream buffer.

-   `load_test.go:86`, `load_test.go:143` — both tests repeat the open
    `:memory:`, `SetMaxOpenConns(1)`, `Exec(Schema)` preamble. A
    `testDB(t) *sql.DB` helper would leave each test as its `Load` call and its
    query, and hold the "each in-memory connection is its own database" note
    once.

## Checked, not a candidate

-   `load.go:29` — `edge.nodep` is the proto's `Nodep` spelling, and the
    schema's column comment (schema.go:28) plus README.md:246 define it as "1
    if the attribute does not create a build dependency". The field mirrors the
    storage, so the name is the beacon.

-   `load.go:152-166` — the `kind` literals ("rule", "source_file", ...) are the
    schema's vocabulary, documented at schema.go:12 and README.md:243, and each
    one is the case for its proto type (`bpb.Target_RULE` -> "rule"), so the
    mapping is already visible. A Go constant would be a second source of
    truth; the engine scan reached the same conclusion for its `kind = 'rule'`
    query (engine-TODO.md).

-   `load.go:230`, `load.go:269` — the `"visibility"` literals are the
    documented exception to "only explicitly set attributes are stored"
    (schema.go:35-36, README.md:256), and both sit in the same function a few
    lines apart, so the repetition marks the exception.

-   `load.go:317` — `splitLabel`'s comment shows only the `//pkg:name` form, but
    `TestSplitLabel` (load_test.go:183) asserts the no-colon form (`//a/b` ->
    `a/b`, `b`) and `//:root`, so the behavior is pinned by a test and the
    example is illustrative rather than the whole contract.

-   `load.go:43` — `Load`'s doc says it returns "the number of targets loaded",
    while on error it returns the partial count from `insertTargets`. The only
    caller checks the error and returns before reading the count
    (cmd/bazelex/main.go:79-82), so the count is used only on success; the
    contract is not misleading for its one caller.

## Deferred

-   `load_test.go:120`, `load_test.go:165` — the row loops stop at `rows.Next()`
    without the usual `rows.Err()` check that `QueryPage` makes (engine.go:129).
    Adding it would change what a mid-iteration driver error does (fail the
    test instead of passing with the rows read so far), so it is a test
    behavior change, not a beacon rewrite.
