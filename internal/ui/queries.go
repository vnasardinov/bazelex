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
	SQL       string
}

// selfContained reports whether the query needs no user input.
func (q Query) selfContained() bool { return len(q.Params) == 0 }

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
		Description: "The longest dependency chain from any top-level rule down to each rule. The largest value is the length of the longest chain in the graph; if it equals the depth limit (100), the graph probably has a cycle.",
		SQL: `WITH RECURSIVE
  dep(f, t) AS (
    SELECT DISTINCT e.from_id, e.to_id FROM edge e
    JOIN target f ON f.id = e.from_id AND f.kind = 'rule'
    JOIN target t ON t.id = e.to_id AND t.kind = 'rule'
    WHERE e.attr IN ('deps', 'embed')
  ),
  roots(id) AS (SELECT f FROM dep EXCEPT SELECT t FROM dep),
  walk(id, depth) AS (
    SELECT id, 0 FROM roots
    UNION
    SELECT dep.t, walk.depth + 1 FROM dep JOIN walk ON dep.f = walk.id
    WHERE walk.depth < 100
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
		SQL: `WITH RECURSIVE
  dep(f, t) AS (
    SELECT DISTINCT e.from_id, e.to_id FROM edge e
    JOIN target f ON f.id = e.from_id AND f.kind = 'rule'
    JOIN target t ON t.id = e.to_id AND t.kind = 'rule'
    WHERE e.attr IN ('deps', 'embed')
  ),
  closure(root, id) AS (
    SELECT id, id FROM target WHERE kind = 'rule'
    UNION
    SELECT closure.root, dep.t FROM dep JOIN closure ON dep.f = closure.id
  )
SELECT t.label, COUNT(*) - 1 AS transitive_deps
FROM closure JOIN target t ON t.id = closure.root
GROUP BY t.label ORDER BY transitive_deps DESC;`,
	},
	{
		ID:          "transitive-deps",
		Title:       "Transitive dependencies",
		Description: "Everything a target depends on, directly or indirectly, via deps edges.",
		Params:      []Param{{"target", "Target", "//foo:bar"}},
		SQL: `WITH RECURSIVE r(id) AS (
  SELECT id FROM target WHERE label = :target
  UNION
  SELECT e.to_id FROM edge e JOIN r ON e.from_id = r.id
  WHERE e.attr = 'deps' AND e.to_id IS NOT NULL
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
}
