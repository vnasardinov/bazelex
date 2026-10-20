package main

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
	Params      []Param
	SQL         string
}

var queries = []Query{
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
