# TODO

Ideas for further queries and schema work, with an eye towards what a
developer might want to know about a large Bazel project's build graph.
The figures are from running bazelex over the
[buildtools](https://github.com/bazel-contrib/buildtools) repository
(545 targets as of Oct 2026).

## Queries to add

-   **Fan-out and fan-in.** Direct dependency counts per target. This is
    plain `GROUP BY`, no recursion. With `deps` and `embed` edges,
    `//edit:edit` has the most direct deps (10) and `//build:build` the
    most direct dependents (16). The counts change with which `attr`
    values you include.
-   **Dead-code candidates.** Rules with no dependents that aren't
    binaries or tests. ``buildtools`` has 74. This is a lead, not an answer:
    libraries are meant to be consumed from outside the workspace.
-   **Shared sources.** Files listed in `srcs` of more than one rule.
    `buildtools` has 26, and some are legitimate.
-   **External dependency exposure.** Group edges with a NULL `to_id` by
    repo and count the dependent rules. In `buildtools`, `@bazel_tools`
    has 9.
-   **`select()` usage.** Which targets use `select`, and which
    `config_setting` is the most common key. The data is already in
    `edge` (`in_select` and the `<select_condition>` attr).
-   **Choke points, more accurate.** The web UI ranks targets by the
    product of fan-in and fan-out. See [docs/choke-points.md](docs/choke-points.md)
    for better measures (ancestors times descendants, dominators, exact
    betweenness).
-   **Snapshot diffs.** `ATTACH` two databases built from different
    commits and diff the edges. Needs no code.

## Smaller improvements

-   **Implicit-attribute edges.** In `buildtools` they dominate the edge
    table: attributes such as `$go_context_data` and `:xcode_config`
    each contribute 78 edges. They carry their own attribute names, so
    the `<implicit>` special value rarely fires. Document this, and
    consider an `implicit` column so queries don't need to filter on
    name prefixes.

## Code comprehension

Candidates for rewrites that make the code easier to read, mostly by
adding or sharpening beacons (in Ruven Brooks's sense: recognizable
names, idioms and shapes that let a reader confirm what code does without
reading every line). Listed in the order I'd tackle them.

-   **`queryView` state** (`internal/ui/server.go`). The state of a
    query is spread across `Ran`, `Skipped`, `Err` and an empty `Rows`,
    so the template has to know which combinations are possible.
    Consider a single `State` field that the template switches on.
-   **`ruleAttrs`** (`internal/graphdb/load.go`). The skip condition
    `name == "name" || (name != "visibility" && !explicit)` is a
    double negative. Name it, e.g.
    `keep := name == "visibility" || explicit`. The "select() values are
    skipped" rule holds only because no case handles selectors, so state
    it where the switch is. Comment why `LABEL_LIST` is handled for
    `visibility` alone.
