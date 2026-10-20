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

CREATE INDEX edge_from ON edge(from_id);
CREATE INDEX edge_to_id ON edge(to_id);
CREATE INDEX edge_to_label ON edge(to_label);
CREATE INDEX target_package ON target(package);
`
