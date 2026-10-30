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
    binaries or tests. `buildtools` has 74. This is a lead, not an
    answer: libraries are meant to be consumed from outside the
    workspace.
-   **Shared sources.** Files listed in `srcs` of more than one rule.
    `buildtools` has 26, and some are legitimate.
-   **External dependency exposure.** Group edges with a NULL `to_id` by
    repo and count the dependent rules. In `buildtools`, `@bazel_tools`
    has 9.
-   **`select()` usage.** Which targets use `select`, and which
    `config_setting` is the most common key. The data is already in
    `edge` (`in_select` and the `<select_condition>` attr).
-   **Choke points, more accurate.** The web UI ranks targets by the
    product of fan-in and fan-out. See
    [docs/choke-points.md](docs/choke-points.md) for better measures
    (ancestors times descendants, dominators, exact betweenness).
-   **Snapshot diffs.** `ATTACH` two databases built from different
    commits and diff the edges. Needs no code.

## Smaller improvements

-   **Implicit-attribute edges.** In `buildtools` they dominate the edge
    table: attributes such as `$go_context_data` and `:xcode_config`
    each contribute 78 edges. They carry their own attribute names, so
    the `<implicit>` special value rarely fires. Document this, and
    consider an `implicit` column so queries don't need to filter on
    name prefixes.

## Web UI

-   **Parallel cheap queries.** Run them in a bounded pool of
    goroutines, with the limit set by a flag (default 5). SQLite allows
    concurrent readers, each on its own connection, so `database/sql`'s
    pool works as long as the database is opened read-only (or in WAL
    mode).

-   **Graph view.** Collapse the target graph by package and draw the
    quotient graph. If that is still too large, let the user pick a
    package and draw its neighbors out to a chosen depth.

    See also [Quotienting](#Quotienting) below.

-   In `internal/ui/index.html`, if two refreshes for the same section
    overlap (the user clicks twice quickly), nothing here cancels the
    first. Whichever response arrives last wins, and the earlier
    request's `.finally` may remove the CSS class `loading` while the
    later one is still in flight. For this use it's probably acceptable,
    but an `AbortController` would tighten it up.

## Code comprehension

Rewrites that would make the code easier to read, mostly by adding or
sharpening beacons (in Ruven Brooks's sense: recognizable names, idioms
and shapes that let a reader confirm what code does without reading
every line).

-   **`queryView` state** (`internal/ui/server.go`). The state of a
    query is spread across `Ran`, `Skipped`, `Err` and an empty `Rows`,
    so the template has to know which combinations are possible.
    Consider a single `State` field that the template switches on.

## Quotienting

Quotienting by package works because packages are a human-chosen
partition that mostly matches ownership and intent. The general move is
to pick a partition of the targets that matches the question you're
asking, then draw the quotient. Here are the partitions I think pay off
most.

### By ownership and organization

-   **By owning team or CODEOWNERS entry.** This shows which teams
    depend on which, and where a change will need cross-team review.
    Edges between teams are a map of coordination cost, and a team with
    huge fan-in is de facto a platform team whether or not it's staffed
    like one.
-   **By top-level directory or product area.** This is a coarser
    package quotient. It's often the best first view of a monorepo,
    because a flat package-level graph has thousands of nodes.
-   **By language or toolchain** (C++, Java, Python, Go, proto, JS).
    This shows the cross-language seams: where FFI, codegen, or
    generated bindings glue ecosystems together. Those seams are usually
    the most fragile and the slowest to build.

### By target kind or role

-   **By rule type** (library, binary, test, proto, genrule, container
    image). This shows how much of the graph is generated or packaging
    machinery, and which libraries sit under many binaries.
-   **By visibility or layer.** Quotienting by declared layer (core,
    services, apps, tools) lets you check whether the graph actually
    respects the architecture you think you have. Back-edges between
    layers become obvious, and you can draw the layer graph as a DAG and
    see what violates it.
-   **By test versus non-test.** Collapsing tests and non-tests
    separately shows what a test run drags in and how much of the
    production graph is covered by test targets.

### By graph structure (computed rather than declared)

-   **Strongly connected components.** Bazel forbids target cycles but
    not package-level cycles. The package quotient can contain cycles
    even when the target graph is a DAG, and collapsing SCCs shows
    exactly where those hidden cycles are. This is one of the most
    useful quotients because the condensation is always a DAG, so you
    get a clean topological layering for free.
-   **By transitive-dependency signature.** Group targets with identical
    (or nearly identical) transitive closures. These are targets that
    are effectively interchangeable from the build's point of view,
    which flags candidates for merging or for a shared dependency to be
    split out.
-   **By reverse-dependency signature.** Group targets with the same set
    of dependents. These are targets that always get used together,
    which hints at a package that should be one library or one API.
-   **By depth or rank.** Collapse by longest-path distance from the
    leaves (or roots). This gives a layered picture and shows how deep
    the critical path is.
-   **Modularity-based clusters (e.g., Louvain).** Let community
    detection find the "real" modules and compare them against the
    declared package boundaries. Mismatches show where the directory
    structure lies about the architecture.

### By external factors

-   **By third-party or external repository.** Collapse everything in
    `@maven//`, `@npm//`, `@com_google_protobuf//` and so on to one node
    each. This shows your exposure to each external dependency, which is
    useful for upgrade planning and for security audits.
-   **By change frequency or churn.** Join in version-control history
    and group hot targets versus cold ones. Edges from hot to cold nodes
    are cheap; cold nodes that depend on hot ones are where cache
    invalidation and rebuild cost come from.
-   **By build cost or cache behavior.** Group by build-time bucket, or
    by cache hit rate. Weighting the quotient by cumulative action time
    shows which cluster dominates your CI wall time.
-   **By release or deployment unit.** Collapse to "what ships in each
    binary or image." The quotient then answers "if I change this
    library, which deployables are affected?"

### Query-centered quotients

These aren't partitions of the whole graph but are useful in the same
way:

-   **Relative to a focal target.** Collapse everything into "the
    target," "its direct deps," "its transitive deps," "its direct
    rdeps," "its transitive rdeps," and "unrelated." This gives a small,
    constant-size picture for any target.
-   **Relative to a diff.** Partition into changed targets, impacted
    targets, and unaffected targets. This is a natural view for code
    review and for CI test selection.

### Making quotient graphs more informative

A few design choices matter as much as the partition:

-   **Weight the edges.** The number of underlying target edges, or the
    number of distinct symbols or headers crossed, distinguishes a thin
    interface from a sprawling one.
-   **Size the nodes** by target count, lines of code, or build time.
-   **Show the "leakiness" of a partition.** Report what fraction of
    edges cross partition boundaries. A good partition has most edges
    inside nodes.
-   **Support drill-down.** The most useful tool lets you expand one
    node of the quotient into its members while keeping everything else
    collapsed, which is a partial quotient. Combined with multiple
    partitions, you can switch lenses on the same graph.
-   **Compare two quotients.** Overlaying the team quotient on the
    package quotient, for example, shows where ownership and structure
    disagree.

If I had to pick three to build first, I'd choose SCC condensation of
the package graph (for hidden cycles and a topological layering), the
focal-target view (for day-to-day "what does this touch" questions), and
an ownership quotient (for the organizational view). The rest tend to be
worth it once those are in people's hands and they start asking more
specific questions.
