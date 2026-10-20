# bazelex

Dumps the static target graph reported by `bazel query` into a SQLite database,
so it can be explored with SQL (for example with the `sqlite3` CLI).

See [Building](#building) and [Running on a Bazel project](#running-on-a-bazel-project)
below.

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

## Layout

* `cmd/bazelex/`: the command-line entry point (flags, running `bazel query`).
* `internal/graphdb/`: the SQLite schema and the loader that turns a stream of
  query `Target` messages into rows. This is where the tests live.
* `internal/buildproto/`: Bazel's `build.proto` and its checked-in Go bindings,
  vendored from [buildtools](https://github.com/bazel-contrib/buildtools)
  because the module that provides it is not importable from here. The proto is
  copied from Bazel itself, so regenerate the bindings from a newer
  `build.proto` if a Bazel upgrade needs new fields.

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
* **Not modeled yet.** Non-label attributes such as `tags` and `visibility`
  values aren't stored.

## Schema

See [`internal/graphdb/schema.go`](internal/graphdb/schema.go). Two tables: `target` (one row per rule, source
file, generated file, ...) and `edge` (one row per dependency, tagged with the
attribute it came from and whether it came from a `select()` branch).

## Example queries

Most depended-on targets, ignoring visibility and data edges:

```sql
SELECT t.label, COUNT(*) AS n
FROM edge e JOIN target t ON t.id = e.to_id
WHERE e.attr = 'deps'
GROUP BY t.label ORDER BY n DESC LIMIT 10;
```

Transitive `deps` of a target, using a recursive CTE:

```sql
WITH RECURSIVE r(id) AS (
  SELECT id FROM target WHERE label = '//foo:bar'
  UNION
  SELECT e.to_id FROM edge e JOIN r ON e.from_id = r.id
  WHERE e.attr = 'deps' AND e.to_id IS NOT NULL
)
SELECT t.label FROM r JOIN target t ON t.id = r.id;
```
