package engine

import (
	"context"
	"database/sql"
	"strconv"
	"sync"

	"github.com/vnasardinov/bazelex/internal/quotient"
)

// graphCache holds the quotient graphs of the build graph, built once from the
// read-only database. pkg is the base: the rule graph collapsed by package.
// condensed is its SCC condensation, and component records which component each
// package landed in.
type graphCache struct {
	mu        sync.Mutex
	pkg       *quotient.Graph
	condensed *quotient.Graph
	component map[string]string
}

// PackageGraph reads the package quotient graph from the database: one node per
// package, and an edge a -> b weighted by the number of rule dependencies from
// a rule in a to a rule in b. Dependencies within a package are dropped, and a
// package with no cross-package edge is still a node. The database is
// read-only, so the result is kept and returned for later calls.
func (e *Engine) PackageGraph(ctx context.Context) (*quotient.Graph, error) {
	e.graphs.mu.Lock()
	defer e.graphs.mu.Unlock()
	return e.packageGraph(ctx)
}

// CondensedGraph returns the SCC condensation of the package graph together
// with the component name of each package. Bazel forbids cycles among targets
// but not among packages, so the package graph can be cyclic; the condensation
// merges each dependency cycle into one node and is always a DAG. Like the
// package graph, it is built once and returned for later calls.
func (e *Engine) CondensedGraph(ctx context.Context) (*quotient.Graph, map[string]string, error) {
	e.graphs.mu.Lock()
	defer e.graphs.mu.Unlock()
	if e.graphs.condensed != nil {
		return e.graphs.condensed, e.graphs.component, nil
	}
	g, err := e.packageGraph(ctx)
	if err != nil {
		return nil, nil, err
	}
	component := quotient.ComponentIndex(g)
	e.graphs.component = component
	e.graphs.condensed = quotient.Collapse(g, component.Partition)
	return e.graphs.condensed, e.graphs.component, nil
}

// packageGraph loads the package quotient graph, or returns the cached one. The
// caller must hold e.graphs.mu.
func (e *Engine) packageGraph(ctx context.Context) (*quotient.Graph, error) {
	if e.graphs.pkg != nil {
		return e.graphs.pkg, nil
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	g := quotient.New()
	// Every package is a node, so that a package with no cross-package
	// dependency still becomes a node of the condensation.
	if err := eachRow(ctx, e.db, `SELECT DISTINCT package FROM target`, func(v []string) {
		g.AddNode(v[0])
	}); err != nil {
		return nil, err
	}
	if err := eachRow(ctx, e.db, `SELECT f.package, t.package, COUNT(*)
FROM rule_dep d
JOIN target f ON f.id = d.from_id
JOIN target t ON t.id = d.to_id
WHERE f.package <> t.package
GROUP BY f.package, t.package`, func(v []string) {
		n, _ := strconv.Atoi(v[2])
		g.Add(v[0], v[1], n)
	}); err != nil {
		return nil, err
	}
	e.graphs.pkg = g
	return g, nil
}

// eachRow runs the statement and calls f with each row, whose values are
// scanned as strings: the callers read names and small counts, never a blob.
func eachRow(ctx context.Context, db *sql.DB, query string, f func([]string)) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	for rows.Next() {
		vals, err := scanInto[string](rows, len(cols))
		if err != nil {
			return err
		}
		f(vals)
	}
	return rows.Err()
}
