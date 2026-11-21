package graphdb

// Schema is the SQLite schema written by bazelex.
//
// Edges point at to_label as written in the query output. to_id is filled in
// after loading and stays NULL when the destination is not part of the loaded
// graph (for example, targets in external repositories).
const Schema = `
CREATE TABLE target (
  id         INTEGER PRIMARY KEY,
  label      TEXT NOT NULL UNIQUE,
  kind       TEXT NOT NULL, -- rule, source_file, generated_file, package_group, environment_group
  package    TEXT NOT NULL, -- e.g. "foo/bar"; empty for the root package
  name       TEXT NOT NULL,
  rule_class TEXT,          -- rules only
  location   TEXT
);

CREATE TABLE edge (
  from_id   INTEGER NOT NULL REFERENCES target(id),
  to_label  TEXT NOT NULL,
  to_id     INTEGER REFERENCES target(id),
  attr      TEXT NOT NULL,    -- attribute name, e.g. deps, srcs, data, visibility
                              -- "<implicit>": rule input not tied to an attribute
                              -- "<generating_rule>": generated file -> its rule
                              -- "<select_condition>": config_setting used as a select() key
  in_select INTEGER NOT NULL, -- 1 if the value comes from a select() branch
  nodep     INTEGER NOT NULL  -- 1 if the attribute does not create a build dependency
);

-- Non-label attribute values of rules, one row per value. Lists are exploded
-- (tags = ["a", "b"] gives two rows), dicts become "key=value" rows, booleans
-- are "true"/"false" and tristates are "yes"/"no"/"auto". Only attributes set
-- explicitly in the BUILD file are stored, so a missing row means "default":
-- no testonly row means testonly = False. The exception is visibility, which
-- is always stored, with package default_visibility already applied. Label
-- values of visibility also appear as edges. select() values are not stored.
CREATE TABLE attribute (
  target_id INTEGER NOT NULL REFERENCES target(id),
  name      TEXT NOT NULL, -- e.g. testonly, tags, size, visibility
  value     TEXT NOT NULL
);

CREATE INDEX attribute_target ON attribute(target_id);
CREATE INDEX attribute_name_value ON attribute(name, value);
CREATE INDEX edge_from ON edge(from_id);
CREATE INDEX edge_to_id ON edge(to_id);
CREATE INDEX edge_to_label ON edge(to_label);
CREATE INDEX target_package ON target(package);

-- Quotient graphs of the target graph, written by bazelex after loading, so
-- that they can be explored with SQL instead of re-derived per query (a SQL
-- condensation would need a quadratic reachability). The quotient column says
-- which quotient a row belongs to: "package" is the rule graph collapsed by
-- package, and "scc" is that graph condensed by its strongly connected
-- components, which merges each package dependency cycle into one node and is
-- always a DAG. The tables mirror the Datalog facts qnode/2, qedge/4 and
-- qmember/3 one for one.
CREATE TABLE qnode (
  quotient TEXT NOT NULL, -- package, scc
  node     TEXT NOT NULL  -- a package, or a component named after its smallest member
);

CREATE TABLE qedge (
  quotient  TEXT NOT NULL,
  from_node TEXT NOT NULL,
  to_node   TEXT NOT NULL,
  weight    INTEGER NOT NULL -- number of underlying rule dependencies
);

-- Which package collapses into which component. Only the "scc" quotient has
-- members here: the package quotient's members are rules, which target holds.
CREATE TABLE qmember (
  quotient TEXT NOT NULL,
  node     TEXT NOT NULL, -- the component
  member   TEXT NOT NULL  -- a package in it
);

-- No indexes: unlike edge and target, these tables stay small (the package
-- graph of a 26k-target repo is about 18k edges), so a scan is instant and an
-- index on qedge would cost more than the table it indexes.

-- Rule-to-rule dependencies: the building block of the recursive queries.
-- Only deps and embed edges count (go_binary embeds its library), and both
-- ends must be rules in the loaded graph.
CREATE VIEW rule_dep(from_id, to_id) AS
  SELECT DISTINCT e.from_id, e.to_id FROM edge e
  JOIN target f ON f.id = e.from_id AND f.kind = 'rule'
  JOIN target t ON t.id = e.to_id AND t.kind = 'rule'
  WHERE e.attr IN ('deps', 'embed');
`
