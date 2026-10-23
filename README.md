# bazelex

Dumps the static target graph reported by `bazel query` into a SQLite database,
so it can be explored with SQL (for example with the `sqlite3` CLI).

See [Building](#building), [Running on a Bazel project](#running-on-a-bazel-project)
and [Web UI](#web-ui) below.

Targets in external repositories are not loaded. Edges that point to them keep
their `to_label` but have a NULL `to_id`.

## Building

You need [Bazelisk](https://github.com/bazelbuild/bazelisk) (or Bazel at the
version in `.bazelversion`). The Go toolchain is managed by Bazel, so you don't
need a local Go installation.

```sh
git clone https://github.com/vnasardinov/bazelex
cd bazelex
bazel build -c opt //cmd/bazelex
```

The binary is `bazel-bin/cmd/bazelex/bazelex_/bazelex`. To put it on your
`PATH`:

```sh
install -m 755 bazel-bin/cmd/bazelex/bazelex_/bazelex ~/.local/bin/bazelex
```

## Running on a Bazel project

`cd` into any directory inside the Bazel workspace you want to analyze and run
the binary there:

```sh
cd /path/to/your/project
/path/to/bazelex/bazel-bin/cmd/bazelex/bazelex_/bazelex --db graph.sqlite
```

(or just `bazelex --db graph.sqlite` if it is on your `PATH`). It prints
`loaded N targets into graph.sqlite` when it is done. Then explore the result:

```sh
sqlite3 graph.sqlite
```

Notes:

* `bazel` must be on your `PATH` (use `--bazel` to name a different binary), and
  the project must be able to load with `bazel query`. The first run may start a
  Bazel server and fetch external repositories.
* A relative `--db` path is created in the current directory. An existing file
  is replaced.
* By default it queries `//...:all-targets`. Pass target patterns as arguments
  to narrow it, for example `bazelex --db graph.sqlite //foo/...:all-targets`.
  Patterns are used as given, so without the `:all-targets` suffix you get rules
  only, and edges to source files stay unresolved.
* To reuse an existing query result instead, run
  `bazel query --output=streamed_proto //...:all-targets | bazelex -stdin`.
* `bazel run //cmd/bazelex` only works against bazelex's own workspace, because
  `bazel run` is always tied to the workspace it is invoked from. To analyze
  another project, use the built binary as shown above.
* Add the database file to that project's `.gitignore`, or write it outside the
  working tree, so it isn't committed by accident.

Example: running it over the [buildtools](https://github.com/bazel-contrib/buildtools)
repository with `--db buildtools.sqlite` loaded 545 targets in about 5 seconds
on a cold Bazel server.

## Web UI

`bazelex_ui` serves a single page that runs the [example queries](#example-queries)
and shows the results 20 rows at a time (see `--rows`), with Previous and Next
links for longer results. The page has two sections:

* Self-contained queries need no input: most depended-on targets, depth of
  every rule, heaviest targets, choke points, rules tagged `manual`, and public
  rules used only in their own package. All but heaviest targets run when the page loads.
  Heaviest targets is expensive, so it only runs when you click its "Run this
  query" link. Each label in the depth results links to the longest dependency
  chain to that rule.
* Parameterized queries need a target label: transitive dependencies, impact of
  changing a target, the shortest dependency path between two targets, and the
  longest dependency chain to a target. The longest chain is the one whose
  length is the target's depth in "Depth of every rule", from a top-level rule
  down to the target. Ties are broken the same way every time, so only one
  chain is shown.

```sh
bazel run //cmd/bazelex_ui -- --http=:6060 --db=graph.sqlite
```

Then open <http://localhost:6060>. Notes:

* `--db` is a database written by `bazelex`. A relative path is resolved
  against the directory you ran the command from. `--http` defaults to `:6060`.
  `--rows` sets how many result rows are shown at once (default 20).
* The `--` before the flags is required. Without it Bazel tries to parse them
  as its own options.
* For a parameterized query, fill in its inputs and press Run. The inputs autocomplete from the
  rule labels in the database, and their values are kept in the URL.
* The database is opened read-only, and each query is limited to 120 seconds
  (change it with `--timeout`, for example `--timeout=5m`), because the recursive
  queries can be slow on large graphs.
* The UI never runs Bazel, so it can serve a database of any project. Run it
  from a bazelex checkout and point `--db` at the file, or use the built binary
  (`bazel build //cmd/bazelex_ui`, then `bazel-bin/cmd/bazelex_ui/bazelex_ui_/bazelex_ui`).
* A table of contents with a link to every query stays in view on the right
  while you scroll. In a narrow window it moves to the top of the page.
* The page is plain HTML rendered by Go templates, with no JavaScript.

## Layout

* `cmd/bazelex/`: the command-line entry point (flags, running `bazel query`).
* `cmd/bazelex_ui/`: the web UI entry point (flags, opening the database).
* `internal/graphdb/`: the SQLite schema and the loader that turns a stream of
  query `Target` messages into rows. This is where the tests live.
* `internal/ui/`: the web UI itself: the canned queries, the HTTP handler and the
  page template.
* Bazel's query proto bindings (`build_proto`) come from the published
  [buildtools](https://github.com/bazel-contrib/buildtools) module
  (`github.com/bazel-contrib/buildtools/v10`). `MODULE.bazel` overrides how
  gazelle builds that module so it uses the checked-in `build.gen.pb.go` as plain
  Go instead of compiling protoc from source.

## Development

```sh
bazel test //...
bazel run //:gazelle                          # after adding/removing Go files or imports
bazel run @io_bazel_rules_go//go -- mod tidy  # after changing Go dependencies
```

## Design

bazelex is a standalone tool rather than a new Bazel command. It runs
`bazel query --output=streamed_proto --keep_going //...:all-targets` (or reads
that output from stdin) and loads the result into SQLite. It uses the pure-Go
`modernc.org/sqlite` driver, which avoids cgo and keeps the Bazel build simple.
Only `query` (the full static graph) is supported; `cquery` is not.

Plain `//...` in `query` returns only rules, so the default pattern is
`//...:all-targets`. This also loads source and generated files, which lets
`srcs` edges resolve.

## What it writes

* `target`: one row per target, with `label`, `kind` (rule, source file,
  generated file, and so on), `package`, `name`, `rule_class` and `location`.
* `edge`: one row per dependency, with `from_id`, `to_label`, `to_id`, `attr`,
  `in_select` and `nodep`.
  * It covers every label-typed attribute, including `select()` branches.
  * `to_id` stays NULL for labels outside the loaded graph, which means targets
    in external repos.
  * Three special `attr` values mark edges that don't come from a normal
    attribute: `<implicit>`, `<generating_rule>` and `<select_condition>`.
* `attribute`: one row per non-label attribute value of a rule, as
  `(target_id, name, value)`: `tags`, `size`, `testonly`, `visibility` and so on.
  * Lists become one row per element, dicts become `key=value` rows.
  * Only attributes set explicitly in the BUILD file are stored, so a missing
    row means the default. `visibility` is always stored, with the package's
    `default_visibility` applied. It is also still in `edge`.
  * `select()` values are not stored.
* `schema.go` documents the columns.

## Results on the buildtools repo

* It loaded 545 targets (179 rules, 326 source files, 40 generated files) in
  about 1.5 seconds.
* Queries tried: most depended-on targets and transitive deps via a recursive
  CTE. `//build:build` was the most depended-on rule, with 15 dependents.
* 987 of 1,596 edges had a NULL `to_id`. All the ones inspected pointed at
  external repos such as `@bazel_tools`, as expected.

## Things to know

* **`visibility` edges.** Visibility is a label-list attribute, so it produces
  edges to `//visibility:public` and similar. They are kept because `attr` lets
  you filter them out, and the example queries filter on `attr = 'deps'`.
* **`embed` vs `deps`.** `go_binary` uses `embed` for its library, so filtering
  on `deps` alone misses it. For example, `//buildifier:buildifier` has no
  `deps` edges.
* **Dependency changes.** `go.mod`, `go.sum` and `MODULE.bazel` include sqlite
  and its transitive dependencies.
* **Attribute defaults.** `attribute` has no rows for defaulted values, so
  "not testonly" means "no `testonly = true` row". Attributes set through
  `select()` are missing too.

## Schema

See [`internal/graphdb/schema.go`](internal/graphdb/schema.go). Three tables: `target` (one row per rule, source
file, generated file, ...), `attribute` (non-label attribute values) and `edge` (one row per dependency, tagged with the
attribute it came from and whether it came from a `select()` branch). A view, `rule_dep`, selects the
rule-to-rule `deps`/`embed` edges that the recursive queries below build on.

## Example queries

Most depended-on targets, ignoring visibility and data edges:

```sql
SELECT t.label, COUNT(*) AS n
FROM edge e JOIN target t ON t.id = e.to_id
WHERE e.attr = 'deps'
GROUP BY t.label ORDER BY n DESC LIMIT 10;
```

Transitive `deps` and `embed` of a target, using a recursive CTE:

```sql
WITH RECURSIVE r(id) AS (
  SELECT id FROM target WHERE label = '//foo:bar'
  UNION
  SELECT e.to_id FROM edge e JOIN r ON e.from_id = r.id
  WHERE e.attr IN ('deps', 'embed') AND e.to_id IS NOT NULL
)
SELECT t.label FROM r JOIN target t ON t.id = r.id;
```

Rules tagged `manual`, and rules that are public but only used inside their
own package:

```sql
SELECT t.label FROM target t JOIN attribute a ON a.target_id = t.id
WHERE a.name = 'tags' AND a.value = 'manual';

SELECT t.label FROM target t
WHERE t.kind = 'rule'
  AND EXISTS (SELECT 1 FROM attribute a WHERE a.target_id = t.id
              AND a.name = 'visibility' AND a.value = '//visibility:public')
  AND NOT EXISTS (SELECT 1 FROM edge e JOIN target f ON f.id = e.from_id
                  WHERE e.to_id = t.id AND e.attr <> 'visibility'
                  AND f.package <> t.package);
```

### More recursive queries

These are built on the `rule_dep(from_id, to_id)` view: rule-to-rule dependency
edges, with `attr` limited to `deps` and `embed`. The view is defined in the
schema. Query `edge` directly instead (as the shortest-path query does) if you
want `data` as well for runtime dependencies. Leave `visibility` and the
implicit `$...`/`:...` attributes out. Cycles can't make these run forever: the
queries that track paths have a depth limit, the depth query is bounded by the
number of rules, and the others use `UNION`, which drops duplicate rows. See
[the depth query](#more-recursive-queries) for why cycles are possible at all.

Direct and transitive impact of changing a target, that is everything that
(transitively) depends on it:

```sql
WITH RECURSIVE r(id) AS (
  SELECT id FROM target WHERE label = '//foo:bar'
  UNION
  SELECT e.from_id FROM edge e JOIN r ON e.to_id = r.id
  WHERE e.attr IN ('deps', 'embed')
)
SELECT t.label FROM r JOIN target t ON t.id = r.id
WHERE t.kind = 'rule' AND t.label <> '//foo:bar'
ORDER BY t.label;
```

Shortest dependency path from one target to another:

```sql
WITH RECURSIVE
  dep(f, t) AS (
    SELECT DISTINCT from_id, to_id FROM edge
    WHERE attr IN ('deps', 'embed') AND to_id IS NOT NULL
  ),
  walk(id, depth, path) AS (
    SELECT id, 0, label FROM target WHERE label = '//foo:bar'
    UNION ALL
    SELECT dep.t, walk.depth + 1, walk.path || ' -> ' || t.label
    FROM dep
    JOIN walk ON dep.f = walk.id
    JOIN target t ON t.id = dep.t
    WHERE walk.depth < 20
  )
SELECT depth, path FROM walk JOIN target t ON t.id = walk.id
WHERE t.label = '//baz:qux'
ORDER BY depth LIMIT 1;
```

This enumerates every path from the start target up to the depth limit, so it
can be slow from a target near the top of a large, wide graph. Lower the limit
if it is.

Depth of every rule: the length of the longest dependency chain from any
top-level rule (one that nothing depends on) down to it. The largest value is
the length of the longest chain in the whole graph:

```sql
WITH RECURSIVE
  roots(id) AS (SELECT from_id FROM rule_dep EXCEPT SELECT to_id FROM rule_dep),
  walk(id, depth) AS (
    SELECT id, 0 FROM roots
    UNION
    SELECT d.to_id, walk.depth + 1 FROM rule_dep d JOIN walk ON d.from_id = walk.id
    WHERE walk.depth < (SELECT COUNT(*) FROM target WHERE kind = 'rule')
  )
SELECT t.label, MAX(walk.depth) AS depth
FROM walk JOIN target t ON t.id = walk.id
GROUP BY t.label ORDER BY depth DESC LIMIT 10;
```

This tracks `(target, depth)` pairs instead of whole paths, so it stays small
even when the number of distinct paths is huge. Very deep chains are normal in
large projects (TensorFlow has rules more than 100 levels down), so the walk has
no fixed depth limit. It is bounded by the number of rules instead: a chain
through N rules has at most N-1 edges, so real chains are never cut short. If
the top depth reaches the number of rules, the graph has a cycle among these
edges.

Bazel forbids dependency cycles, so a cycle should not normally show up, but
the graph in the database is not quite the graph Bazel builds:

* `bazel query` reports the static graph. Every branch of every `select()` is
  included, whereas Bazel checks for cycles in the graph of one configuration,
  where only one branch is taken. Two branches that never apply together can
  form a loop that no build ever sees.
* With `--keep_going`, `bazel query` reports a dependency cycle as an error but
  still prints the targets it found, so a broken workspace can be loaded.
* With `-stdin`, bazelex loads whatever stream it is given, which Bazel has not
  vetted.

Dropping edges, as happens with a narrowed pattern or with external repos, can
hide a cycle but never creates one. The bound is there so that a cycle ends the
query with a recognizable result instead of running forever.

Heaviest targets: how many rules each one pulls in transitively. This is the
"how much does building this cost" number, and it is the most expensive query
here, because it computes the full closure from every rule:

```sql
WITH RECURSIVE
  closure(root, id) AS (
    SELECT id, id FROM target WHERE kind = 'rule'
    UNION
    SELECT closure.root, d.to_id FROM rule_dep d JOIN closure ON d.from_id = closure.id
  )
SELECT t.label, COUNT(*) - 1 AS transitive_deps
FROM closure JOIN target t ON t.id = closure.root
GROUP BY t.label ORDER BY transitive_deps DESC LIMIT 10;
```

Choke points: rules with both many direct dependents and many direct
dependencies, ranked by the product of the two. This is a cheap stand-in for
betweenness (see [docs/choke-points.md](docs/choke-points.md) for better
measures):

```sql
WITH
  fan_in(id, n) AS (SELECT to_id, COUNT(*) FROM rule_dep GROUP BY to_id),
  fan_out(id, n) AS (SELECT from_id, COUNT(*) FROM rule_dep GROUP BY from_id)
SELECT t.label, fan_in.n AS fan_in, fan_out.n AS fan_out,
       fan_in.n * fan_out.n AS score
FROM fan_in JOIN fan_out USING (id) JOIN target t ON t.id = fan_in.id
ORDER BY score DESC, t.label LIMIT 10;
```
