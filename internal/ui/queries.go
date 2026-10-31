package ui

// Param is an input a query needs from the user. Its value is bound to the
// named SQL parameter ":<Name>".
type Param struct {
	Name        string
	Label       string
	Placeholder string
}

// Query is a canned query from the README's "Example queries" section.
type Query struct {
	ID          string
	Title       string
	Description string
	Params      []Param // none for a self-contained query
	// Expensive queries are not run on page load; the user asks for them.
	Expensive bool
	// RowLink, if set, turns a column of the results into links to another query.
	RowLink *RowLink
	// GraphColumn, if set, names a column of package names that become links to
	// the package graph page, centered on the package's dependency cycle.
	GraphColumn string
	SQL         string
}

// RowLink makes each value in Column a link that opens the query ToQuery with
// its parameter ToParam set to that value. With NewTab, the link opens in a new
// browser tab and doesn't carry over the "run" parameter, so the results being
// linked from stay on the page they are on and the new page doesn't re-run an
// expensive query.
type RowLink struct {
	Column  string
	ToQuery string
	ToParam string
	NewTab  bool
}

// selfContained reports whether the query needs no user input.
func (q Query) selfContained() bool { return len(q.Params) == 0 }

// queries is every canned query the page shows. Adding an entry here is all it
// takes to surface a new query: index.html renders each one generically, the
// self-contained ones (no Params) in the first section and the parameterized
// ones in the second. Touch the template or server.go only if a query needs new
// behavior.
var queries = []Query{
	{
		ID:          "most-depended-on",
		Title:       "Most depended-on targets",
		Description: "Targets with the most direct dependents, ignoring visibility and data edges.",
		SQL: `SELECT t.label, COUNT(*) AS n
FROM edge e JOIN target t ON t.id = e.to_id
WHERE e.attr = 'deps'
GROUP BY t.label ORDER BY n DESC;`,
	},
	{
		ID:          "rule-depth",
		Title:       "Depth of every rule",
		Description: "The longest dependency chain from any top-level rule down to each rule. The largest value is the length of the longest chain in the graph; there is no fixed depth limit, but the walk stops at the number of rules, which no chain in an acyclic graph can reach, so a value that large means a cycle.",
		RowLink:     &RowLink{Column: "label", ToQuery: "longest-chain", ToParam: "target"},
		SQL: `WITH RECURSIVE
  roots(id) AS (SELECT from_id FROM rule_dep EXCEPT SELECT to_id FROM rule_dep),
  walk(id, depth) AS (
    SELECT id, 0 FROM roots
    UNION
    SELECT d.to_id, walk.depth + 1 FROM rule_dep d JOIN walk ON d.from_id = walk.id
    WHERE walk.depth < (SELECT COUNT(*) FROM target WHERE kind = 'rule')
  )
SELECT t.label, MAX(walk.depth) AS depth
FROM walk JOIN target t ON t.id = walk.id
GROUP BY t.label ORDER BY depth DESC;`,
	},
	{
		ID:          "heaviest",
		Title:       "Heaviest targets",
		Description: "How many rules each rule pulls in transitively. This computes the full closure from every rule, so it is slow on large graphs.",
		Expensive:   true,
		RowLink:     &RowLink{Column: "label", ToQuery: "transitive-deps", ToParam: "target", NewTab: true},
		SQL: `WITH RECURSIVE
  closure(root, id) AS (
    SELECT id, id FROM target WHERE kind = 'rule'
    UNION
    SELECT closure.root, d.to_id FROM rule_dep d JOIN closure ON d.from_id = closure.id
  )
SELECT t.label, COUNT(*) - 1 AS transitive_deps
FROM closure JOIN target t ON t.id = closure.root
GROUP BY t.label ORDER BY transitive_deps DESC;`,
	},
	{
		ID:          "manual-rules",
		Title:       "Rules tagged manual",
		Description: "Rules with an explicit tags = [\"manual\"], which wildcard patterns such as //... skip.",
		SQL: `SELECT t.label FROM target t JOIN attribute a ON a.target_id = t.id
WHERE a.name = 'tags' AND a.value = 'manual'
ORDER BY t.label;`,
	},
	{
		ID:          "public-package-local",
		Title:       "Public but only used in their own package",
		Description: "Rules visible to //visibility:public that no rule in another package depends on, so their visibility could probably be narrowed. Visibility edges don't count as uses.",
		SQL: `SELECT t.label FROM target t
WHERE t.kind = 'rule'
  AND EXISTS (SELECT 1 FROM attribute a WHERE a.target_id = t.id
              AND a.name = 'visibility' AND a.value = '//visibility:public')
  AND NOT EXISTS (SELECT 1 FROM edge e JOIN target f ON f.id = e.from_id
                  WHERE e.to_id = t.id AND e.attr <> 'visibility'
                  AND f.package <> t.package)
ORDER BY t.label;`,
	},
	{
		ID:          "choke-points",
		Title:       "Choke points",
		Description: "Rules with both many direct dependents (fan-in) and many direct dependencies (fan-out), ranked by their product. A cheap stand-in for betweenness: changes to these rules ripple widely, and they sit between many other rules. Rules with no dependents or no dependencies score 0 and are left out.",
		RowLink:     &RowLink{Column: "label", ToQuery: "impact", ToParam: "target"},
		SQL: `WITH
  fan_in(id, n) AS (SELECT to_id, COUNT(*) FROM rule_dep GROUP BY to_id),
  fan_out(id, n) AS (SELECT from_id, COUNT(*) FROM rule_dep GROUP BY from_id)
SELECT t.label, fan_in.n AS fan_in, fan_out.n AS fan_out,
       fan_in.n * fan_out.n AS score
FROM fan_in JOIN fan_out USING (id) JOIN target t ON t.id = fan_in.id
ORDER BY score DESC, t.label;`,
	},
	{
		ID:          "package-coupling",
		Title:       "Package coupling",
		Description: "Pairs of packages ranked by how many rule-to-rule dependencies cross from one to the other. back_edges counts the opposite direction, so a non-zero value means the two packages depend on each other. Such a pair is listed once, in its heavier direction (alphabetically first on a tie).",
		SQL: `WITH pkg_dep(from_pkg, to_pkg, n) AS (
  SELECT f.package, t.package, COUNT(*)
  FROM rule_dep d
  JOIN target f ON f.id = d.from_id
  JOIN target t ON t.id = d.to_id
  WHERE f.package <> t.package
  GROUP BY f.package, t.package
)
SELECT a.from_pkg, a.to_pkg, a.n AS edges, COALESCE(b.n, 0) AS back_edges
FROM pkg_dep a LEFT JOIN pkg_dep b ON b.from_pkg = a.to_pkg AND b.to_pkg = a.from_pkg
WHERE b.n IS NULL OR a.n > b.n OR (a.n = b.n AND a.from_pkg < a.to_pkg)
ORDER BY edges + back_edges DESC, edges DESC, a.from_pkg, a.to_pkg;`,
	},
	{
		ID:          "package-cycles",
		Title:       "Package dependency cycles",
		Description: "Groups of packages that depend on each other, directly or through other packages. Bazel allows this between packages as long as no rules form a cycle, but it makes the packages impossible to layer. This computes the reachability closure over packages, so it can be slow on large graphs.",
		Expensive:   true,
		GraphColumn: "comp",
		SQL: `WITH RECURSIVE
  pkg_dep(a, b) AS (
    SELECT DISTINCT f.package, t.package
    FROM rule_dep d
    JOIN target f ON f.id = d.from_id
    JOIN target t ON t.id = d.to_id
    WHERE f.package <> t.package
  ),
  reach(a, b) AS (
    SELECT a, b FROM pkg_dep
    UNION
    SELECT reach.a, pkg_dep.b FROM reach JOIN pkg_dep ON pkg_dep.a = reach.b
  ),
  -- A package is in a cycle if it reaches itself. Its component is every
  -- package it reaches and that reaches it back, named by the smallest one.
  member(pkg, comp) AS (
    SELECT self.a, MIN(fwd.b)
    FROM reach self
    JOIN reach fwd ON fwd.a = self.a
    JOIN reach back ON back.a = fwd.b AND back.b = self.a
    WHERE self.b = self.a
    GROUP BY self.a
  )
SELECT comp, COUNT(*) AS packages, GROUP_CONCAT(pkg, ', ') AS members
FROM member GROUP BY comp ORDER BY packages DESC, comp;`,
	},
	{
		ID:          "transitive-deps",
		Title:       "Transitive dependencies",
		Description: "Everything a target depends on, directly or indirectly, via deps and embed edges.",
		Params:      []Param{{"target", "Target", "//foo:bar"}},
		SQL: `WITH RECURSIVE r(id) AS (
  SELECT id FROM target WHERE label = :target
  UNION
  SELECT e.to_id FROM edge e JOIN r ON e.from_id = r.id
  WHERE e.attr IN ('deps', 'embed') AND e.to_id IS NOT NULL
)
SELECT t.label FROM r JOIN target t ON t.id = r.id;`,
	},
	{
		ID:          "impact",
		Title:       "Impact of changing a target",
		Description: "Every rule that directly or transitively depends on the target (deps and embed edges).",
		Params:      []Param{{"target", "Target", "//foo:bar"}},
		SQL: `WITH RECURSIVE r(id) AS (
  SELECT id FROM target WHERE label = :target
  UNION
  SELECT e.from_id FROM edge e JOIN r ON e.to_id = r.id
  WHERE e.attr IN ('deps', 'embed')
)
SELECT t.label FROM r JOIN target t ON t.id = r.id
WHERE t.kind = 'rule' AND t.label <> :target
ORDER BY t.label;`,
	},
	{
		ID:          "shortest-path",
		Title:       "Shortest dependency path",
		Description: "The shortest chain of deps/embed edges from one target to another (depth limit 20).",
		Params: []Param{
			{"from", "From", "//foo:bar"},
			{"to", "To", "//baz:qux"},
		},
		SQL: `WITH RECURSIVE
  dep(f, t) AS (
    SELECT DISTINCT from_id, to_id FROM edge
    WHERE attr IN ('deps', 'embed') AND to_id IS NOT NULL
  ),
  walk(id, depth, path) AS (
    SELECT id, 0, label FROM target WHERE label = :from
    UNION ALL
    SELECT dep.t, walk.depth + 1, walk.path || ' -> ' || t.label
    FROM dep
    JOIN walk ON dep.f = walk.id
    JOIN target t ON t.id = dep.t
    WHERE walk.depth < 20
  )
SELECT depth, path FROM walk JOIN target t ON t.id = walk.id
WHERE t.label = :to
ORDER BY depth LIMIT 1;`,
	},
	{
		ID:          "longest-chain",
		Title:       "Longest dependency chain to a target",
		Description: "The chain of deps/embed edges, from a top-level rule down to the target, whose length is the target's depth in \"Depth of every rule\". Several chains can tie for the longest; this shows one of them.",
		Params:      []Param{{"target", "Target", "//foo:bar"}},
		SQL: `WITH RECURSIVE
  roots(id) AS (SELECT from_id FROM rule_dep EXCEPT SELECT to_id FROM rule_dep),
  walk(id, depth) AS (
    SELECT id, 0 FROM roots
    UNION
    SELECT d.to_id, walk.depth + 1 FROM rule_dep d JOIN walk ON d.from_id = walk.id
    WHERE walk.depth < (SELECT COUNT(*) FROM target WHERE kind = 'rule')
  ),
  depth(id, depth) AS (SELECT id, MAX(depth) FROM walk GROUP BY id),
  -- One parent per rule, one level shallower: following these from the target
  -- gives a single longest chain.
  parent(id, p) AS (
    SELECT d.to_id, MIN(d.from_id) FROM rule_dep d
    JOIN depth df ON df.id = d.from_id
    JOIN depth dt ON dt.id = d.to_id AND df.depth = dt.depth - 1
    GROUP BY d.to_id
  ),
  up(id, depth, len, path) AS (
    SELECT depth.id, depth.depth, depth.depth, t.label
    FROM depth JOIN target t ON t.id = depth.id WHERE t.label = :target
    UNION ALL
    SELECT parent.p, up.depth - 1, up.len, t.label || ' -> ' || up.path
    FROM up JOIN parent ON parent.id = up.id JOIN target t ON t.id = parent.p
  )
SELECT len AS depth, path FROM up WHERE depth = 0;`,
	},
}
