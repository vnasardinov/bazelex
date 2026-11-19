# TODO

Beacon candidates for the `internal/engine` package (`Config`/`Engine`,
`Query`/`QueryPage`/`RuleLabels`/`Format`/`IsNumber`, the Datalog evaluation, and
the package-quotient loaders), found by scanning the package for places where a
reader has to work out what the code does. A beacon is a name, idiom or shape
that lets a reader confirm intent without reading every line. Nothing here
changes behavior; each entry is a rewrite candidate. Files are named relative to
`internal/engine/`.

`quotient-TODO.md` holds the earlier scan of `internal/quotient`; it also anchors
`internal/engine/quotient.go` and `quotient_test.go`, and records no candidates
outstanding there. This scan found nothing new in `quotient_test.go`, and the two
entries below that cite `quotient.go` (the scan loop and the shadowing `sql`
parameter) only showed up by reading the package as a whole, so they are new.
Keep both files' anchors current when either lands.

## Revision anchors

Each candidate's line numbers were verified at the "scanned" revision shown
below; "now" is the revision they were last re-checked against. If a file's two
hashes differ, re-check that file's entries.

| file | blob hash scanned | blob hash now |
| --- | --- | --- |
| `internal/engine/engine.go` | `120d4096b53cd93c15869a7de662234887a0cab5` | `120d4096b53cd93c15869a7de662234887a0cab5` |
| `internal/engine/datalog.go` | `df9a600c83fef0433fc1f94491a1b5cdd56c2742` | `df9a600c83fef0433fc1f94491a1b5cdd56c2742` |
| `internal/engine/quotient.go` | `480cbcb736d449a766d88891b49fdbd53dddda3e` | `480cbcb736d449a766d88891b49fdbd53dddda3e` |
| `internal/engine/engine_test.go` | `3bac5f07782bae3a846335945a2f9f3ee6946915` | `3bac5f07782bae3a846335945a2f9f3ee6946915` |
| `internal/engine/quotient_test.go` | `4c8e8f96848aef356f8e828354f199bd84e43e96` | `4c8e8f96848aef356f8e828354f199bd84e43e96` |

The scan ran at commit `0fcc082a4cfa89e0fedd77debb4c2ed59409228d`, which every
"now" hash above matched.

## Beacon candidates

-   `datalog.go:230` — the rows are ordered by a hand-rolled loop that compares
    each column and returns 0 after assuming the rows have equal lengths. Write
    `slices.SortFunc(rel.Rows, slices.Compare)`: same ordering, but the call says
    "lexicographic" instead of making the reader audit the loop. `datalog.go:210`
    is the same shape for predicate symbols; `cmp.Or(strings.Compare(a.Symbol,
    b.Symbol), cmp.Compare(a.Arity, b.Arity))` names the two sort keys and their
    order.

-   `datalog.go:172` — `evalDatalog(..., limit int)` is the only place the fact
    budget is called a limit; `DefaultMaxFacts`, `Config.MaxFacts` and
    `Engine.maxFacts` all call it the max facts, and only Mangle's
    `engine.WithCreatedFactLimit` calls it a limit. Rename the parameter
    `maxFacts`, so the budget can be followed from the flag to the evaluation
    without translating.

-   `engine.go:46` — `Config.Validate` applies the defaults just to check them
    and throws them away, and `Open` (engine.go:75) applies them again. A reader
    has to check that the two copies agree. Have one method return the filled
    config, e.g. `func (c Config) normalized() (Config, error)`, and let
    `Validate` and `Open` both be callers of it.

-   `engine.go:17` — `DefaultTimeout`'s doc says it bounds "each query and
    Datalog evaluation", but `evalDatalog` (datalog.go:172) takes no context:
    only queries are timed (engine.go:99), including the fact load
    (datalog.go:79), and Mangle's `EvalProgram` takes no context either. Say that
    it bounds each query and the Datalog fact load, or the reader goes looking
    for an evaluation deadline that does not exist.

-   `engine.go:134`, `datalog.go:110`, `quotient.go:99` — the same scan idiom
    (`vals`, `ptrs`, `rows.Scan(ptrs...)`) is written three times, twice into
    `[]string` and once into `[]any`. A reader comparing them has to diff the
    loops to learn that only the value type differs. Extract `scanStrings(rows,
    n)` for the two string callers and keep `scanRow` for Table, or fold both
    into one small generic.

-   `datalog.go:81` — the fact-loading table is an anonymous struct whose `arity`
    field restates the SELECT's column count and the indices its `atom` closure
    reads, so the reader has to verify 3, 2, 3 against both. Name the type
    (`factQuery`) and size the scan by `len(rows.Columns())`, leaving the
    closure's indices as the only place the shape of a row appears.

-   `datalog.go:216` — `DatalogResult.EDB` and `Derived` have no doc, and
    `Derived` is `EstimateFactCount() - len(edb)` although the atoms of a
    predicate the program redefines were never seeded (datalog.go:188). So the
    number is neither "derived facts" nor an estimate of them, and the CLI and
    the page show it (internal/cli/format.go:69, internal/ui/datalog.html:42).
    Document both fields and the counting rule; subtracting only the seeded
    atoms would be the matching fix, but it changes the number for a program
    that redefines a loaded predicate.

-   `engine_test.go:18`, `quotient_test.go:20` — the two fixtures repeat the same
    open-create-schema-insert-close-reopen-read-only-`Open` dance, and `testDB`
    returns a path although its name reads like a `*sql.DB`. One
    `fixtureEngine(t, stmts ...string) *Engine` would leave each fixture as just
    its rows, so the one-package and cycle fixtures differ only in what matters.

-   `datalog.go:152`, `datalog.go:164` — the quotient names `"package"` and
    `"scc"` are the programs' public vocabulary (the examples write
    `qedge("scc", ...)`), but they sit as bare literals next to `cond`, which the
    rest of the package calls `condensed`. Named constants (`packageQuotient`,
    `sccQuotient`) would show the contract without matching strings against the
    doc comment at datalog.go:29.

-   `engine.go:89`, `engine.go:98`, `quotient.go:88` — the query parameter is
    named `sql`, so inside these functions the `database/sql` package that gives
    the file `*sql.DB` and `*sql.Rows` is shadowed. Rename it `query`: the
    signature then reads like the SQL it is, and a reader can reach for `sql.`
    inside the function without a type error.

-   `engine_test.go:140` — `TestFormat` ends by asserting `IsNumber`
    (engine_test.go:155), so a reader looking for IsNumber's coverage by test
    name finds none. Split `TestIsNumber` out, or name the test after both.

## Checked, not a candidate

-   `engine.go:112` — `QueryPage` skips `offset` rows in Go instead of adding
    `LIMIT`/`OFFSET` to the SQL. That is deliberate: `sql` is the caller's
    arbitrary statement, which may carry its own `LIMIT`, so rewriting it would
    change the caller's query, and the probe row (engine.go:118) is how
    `hasMore` is known without reading the rest. The doc comment and
    `TestQueryPage` pin the behavior.

-   `engine.go:154` — `RuleLabels` keeps a row only when the scan gave a
    `string`, although `Table` (engine.go:79) says text may also be `[]byte`.
    Not raised: the only driver is `modernc.org/sqlite` (README.md:234), which
    returns TEXT as `string`, and `TestRuleLabels` asserts the whole list.

-   `engine.go:148` — `kind = 'rule'` is a bare literal. Not raised: the kind
    vocabulary belongs to the schema, which documents it
    (internal/graphdb/schema.go:12) and uses it in the `rule_dep` view; the
    engine has one query, and a Go constant would be a second source of truth.

-   `datalog.go:260` — `datalogColumns` drops the variables whose names start
    with `_`. Not raised: that is Mangle's anonymous-variable spelling, used in
    the shipped example (`target(X, "rule", _)`, internal/ui/datalog.go:44), and
    the `argN` fallback column name is what it means.