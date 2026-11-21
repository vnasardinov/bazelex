# TODO

Beacon candidates for the `internal/engine` package (`Config`/`Engine`,
`Query`/`QueryPage`/`RuleLabels`/`Format`/`IsNumber`, the Datalog evaluation, and
the quotient readers), found by scanning the package for places where a reader
has to work out what the code does. A beacon is a name, idiom or shape
that lets a reader confirm intent without reading every line. Nothing here
changes behavior; each entry is a rewrite candidate. Files are named relative to
`internal/engine/`.

`quotient-TODO.md` holds the earlier scan of `internal/quotient` and the two
`internal/ui/graph*` files it cites. This file owns every `internal/engine/`
file, so the anchors and the entries below stay in one place. That scan found no
candidates outstanding in `internal/engine/quotient.go`, and this one found none
new in `quotient_test.go`; the shadowing `sql` parameter only showed up by
reading the package as a whole, so it was new.

## Revision anchors

Each entry's line numbers were verified at the "scanned" revision shown
below; "now" is the revision they were last re-checked against. If a file's two
hashes differ, re-check that file's entries.

| file | blob hash scanned | blob hash now |
| --- | --- | --- |
| `internal/engine/engine.go` | `120d4096b53cd93c15869a7de662234887a0cab5` | `89b169d7172c7c6cf565d5c2ae646d9f50db4922` |
| `internal/engine/datalog.go` | `df9a600c83fef0433fc1f94491a1b5cdd56c2742` | `b2303952e64e5fa177640db357b0c2e355543436` |
| `internal/engine/quotient.go` | `480cbcb736d449a766d88891b49fdbd53dddda3e` | `a168577ccf34d5e718cfd6faa861eb3b07fad805` |
| `internal/engine/engine_test.go` | `3bac5f07782bae3a846335945a2f9f3ee6946915` | `8ca3ed3d299c64c2acb734df0dd339a0f4dea718` |
| `internal/engine/quotient_test.go` | `4c8e8f96848aef356f8e828354f199bd84e43e96` | `840fec713608991852f4a20b5ec04a2c8568914e` |

The scan ran at commit `0fcc082a4cfa89e0fedd77debb4c2ed59409228d`. A "now" hash
that differs from its scanned hash has been re-checked since, and its entries'
line numbers are current for it. The `now` column was last refreshed after the
quotient tables moved to `internal/graphdb`: `quotient.go` reads them back
instead of deriving them, and the two test fixtures write the rows the engine
reads. The entries below are current for that revision.

## Beacon candidates

None outstanding.

## Checked, not a candidate

-   `engine.go:112` — `QueryPage` skips `offset` rows in Go instead of adding
    `LIMIT`/`OFFSET` to the SQL. That is deliberate: `query` is the caller's
    arbitrary statement, which may carry its own `LIMIT`, so rewriting it would
    change the caller's query, and the probe row (engine.go:118) is how
    `hasMore` is known without reading the rest. The doc comment and
    `TestQueryPage` pin the behavior.

-   `engine.go:158` — `RuleLabels` keeps a row only when the scan gave a
    `string`, although `Table` (engine.go:79) says text may also be `[]byte`.
    Not raised: the only driver is `modernc.org/sqlite` (README.md:234), which
    returns TEXT as `string`, and `TestRuleLabels` asserts the whole list.

-   `engine.go:152` — `kind = 'rule'` is a bare literal. Not raised: the kind
    vocabulary belongs to the schema, which documents it
    (internal/graphdb/schema.go:12) and uses it in the `rule_dep` view; the
    engine has one query, and a Go constant would be a second source of truth.

-   `engine.go:137` — `scanInto[T]` is the package's only generic function. Not
    raised: it folds the identical bodies of the old `scanRow` and `scanStrings`
    into one, and its comment says outright that T buys clarity, not safety,
    since `rows.Scan` takes `...any` and cannot check T at compile time.

-   `datalog.go:270` — `datalogColumns` drops the variables whose names start
    with `_`. Not raised: that is Mangle's anonymous-variable spelling, used in
    the shipped example (`target(X, "rule", _)`, internal/ui/datalog.go:44), and
    the `argN` fallback column name is what it means.

## Deferred

-   `datalog.go:233` — `Derived` is the store's fact count minus every loaded
    fact, including the atoms of a predicate the program redefines, which are
    never seeded (datalog.go:208). So it undercounts for such a program, and the
    CLI and the page show the number (internal/cli/format.go:69,
    internal/ui/datalog.html:42). `DatalogResult` now documents the rule;
    counting only the seeded atoms would fix it, but that changes a displayed
    number, so it is a behavior change and gets its own commit, not a beacon
    rewrite.