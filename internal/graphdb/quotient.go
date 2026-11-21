package graphdb

import (
	"database/sql"

	"github.com/vnasardinov/bazelex/internal/quotient"
)

// The quotient names the qnode, qedge and qmember tables are keyed by, as the
// schema's quotient column documents. They are the vocabulary SQL queries and
// Datalog programs share: a query writes qedge.quotient = 'scc', a program
// writes qedge("scc", ...).
const (
	packageQuotient = "package"
	sccQuotient     = "scc"
)

// insertQuotients writes the quotient graphs of the loaded graph into the
// qnode, qedge and qmember tables. It runs after every target and edge is
// written, because rule_dep and each target's package are only complete then.
//
// The quotients are the package graph, the rule graph collapsed by package,
// and its SCC condensation. Both are small next to the target graph, and
// writing them down spares SQL users the quadratic reachability a condensation
// would otherwise need per query.
func insertQuotients(tx *sql.Tx) error {
	pkg, err := packageGraph(tx)
	if err != nil {
		return err
	}
	component := quotient.ComponentIndex(pkg)
	cond := quotient.Collapse(pkg, component.Partition)

	node, err := tx.Prepare(`INSERT INTO qnode (quotient, node) VALUES (?, ?)`)
	if err != nil {
		return err
	}
	defer node.Close()
	edge, err := tx.Prepare(`INSERT INTO qedge (quotient, from_node, to_node, weight) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer edge.Close()
	member, err := tx.Prepare(`INSERT INTO qmember (quotient, node, member) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer member.Close()

	for _, q := range []struct {
		name  string
		graph *quotient.Graph
	}{{packageQuotient, pkg}, {sccQuotient, cond}} {
		for _, n := range q.graph.Nodes() {
			if _, err := node.Exec(q.name, n); err != nil {
				return err
			}
		}
		for _, from := range q.graph.Nodes() {
			for _, d := range q.graph.Deps(from) {
				if _, err := edge.Exec(q.name, from, d.To, d.Weight); err != nil {
					return err
				}
			}
		}
	}
	// Packages come sorted, so the members are written deterministically.
	for _, p := range pkg.Nodes() {
		if _, err := member.Exec(sccQuotient, component[p], p); err != nil {
			return err
		}
	}
	return nil
}

// packageGraph reads the package graph from the loaded tables: one node per
// package, and an edge a -> b weighted by the number of rule dependencies from
// a rule in a to a rule in b. Dependencies within a package are dropped, and a
// package with no cross-package edge is still a node, so that it becomes one in
// the condensation too.
func packageGraph(tx *sql.Tx) (*quotient.Graph, error) {
	g := quotient.New()
	if err := func() error {
		rows, err := tx.Query(`SELECT DISTINCT package FROM target`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var pkg string
			if err := rows.Scan(&pkg); err != nil {
				return err
			}
			g.AddNode(pkg)
		}
		return rows.Err()
	}(); err != nil {
		return nil, err
	}
	if err := func() error {
		rows, err := tx.Query(`SELECT f.package, t.package, COUNT(*)
FROM rule_dep d
JOIN target f ON f.id = d.from_id
JOIN target t ON t.id = d.to_id
WHERE f.package <> t.package
GROUP BY f.package, t.package`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var from, to string
			var n int
			if err := rows.Scan(&from, &to, &n); err != nil {
				return err
			}
			g.Add(from, to, n)
		}
		return rows.Err()
	}(); err != nil {
		return nil, err
	}
	return g, nil
}
