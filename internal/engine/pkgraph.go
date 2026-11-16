package engine

import (
	"cmp"
	"context"
	"slices"
	"sync"
)

// pkgCache holds the package quotient graph, built once from the read-only
// database.
type pkgCache struct {
	mu sync.Mutex
	g  *PkgGraph
}

// PkgGraph is the quotient of the rule graph by package: one node per package,
// and an edge a -> b weighted by the number of rule dependencies from a rule in
// a to a rule in b. Dependencies within a package are dropped.
type PkgGraph struct {
	out, in map[string]map[string]int
}

func newPkgGraph() *PkgGraph {
	return &PkgGraph{out: map[string]map[string]int{}, in: map[string]map[string]int{}}
}

func (g *PkgGraph) add(from, to string, n int) {
	for _, m := range []struct {
		adj      map[string]map[string]int
		from, to string
	}{{g.out, from, to}, {g.in, to, from}} {
		if m.adj[m.from] == nil {
			m.adj[m.from] = map[string]int{}
		}
		m.adj[m.from][m.to] += n
	}
}

// Has reports whether pkg has any dependency across packages.
func (g *PkgGraph) Has(pkg string) bool {
	_, o := g.out[pkg]
	_, i := g.in[pkg]
	return o || i
}

// Dep is one package dependency: the number of rule dependencies from one
// package to another.
type Dep struct {
	To string
	N  int
}

// Deps returns the packages from depends on, in name order, or nil if it
// depends on none.
func (g *PkgGraph) Deps(from string) []Dep {
	deps := make([]Dep, 0, len(g.out[from]))
	for to, n := range g.out[from] {
		deps = append(deps, Dep{To: to, N: n})
	}
	slices.SortFunc(deps, func(a, b Dep) int { return cmp.Compare(a.To, b.To) })
	return deps
}

// reach returns the packages reachable from pkg (including pkg) along adj.
func reach(adj map[string]map[string]int, pkg string) map[string]bool {
	seen := map[string]bool{pkg: true}
	stack := []string{pkg}
	for len(stack) > 0 {
		p := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for q := range adj[p] {
			if !seen[q] {
				seen[q] = true
				stack = append(stack, q)
			}
		}
	}
	return seen
}

// Component returns the packages in pkg's dependency cycle: those that reach
// pkg and are reached from it. It is just {pkg} if pkg is in no cycle.
func (g *PkgGraph) Component(pkg string) []string {
	back := reach(g.in, pkg)
	var comp []string
	for p := range reach(g.out, pkg) {
		if back[p] {
			comp = append(comp, p)
		}
	}
	slices.Sort(comp)
	return comp
}

// Neighborhood returns the packages within depth edges of any of centers,
// following edges in either direction. Packages are added nearest first, in
// name order within a distance, until limit is reached; truncated says whether
// that left some out.
func (g *PkgGraph) Neighborhood(centers []string, depth, limit int) (nodes []string, truncated bool) {
	seen := map[string]bool{}
	frontier := slices.Clone(centers)
	slices.Sort(frontier)
	for d := 0; ; d++ {
		for _, p := range frontier {
			if len(nodes) == limit {
				return nodes, true
			}
			seen[p] = true
			nodes = append(nodes, p)
		}
		if d == depth {
			return nodes, false
		}
		var next []string
		for _, p := range frontier {
			for _, adj := range []map[string]map[string]int{g.out, g.in} {
				for q := range adj[p] {
					if !seen[q] && !slices.Contains(next, q) {
						next = append(next, q)
					}
				}
			}
		}
		if len(next) == 0 {
			return nodes, false
		}
		slices.Sort(next)
		frontier = next
	}
}

// PackageGraph reads the package quotient graph from the database. The database
// is read-only, so the result is kept and returned for later calls.
func (e *Engine) PackageGraph(ctx context.Context) (*PkgGraph, error) {
	e.pkgs.mu.Lock()
	defer e.pkgs.mu.Unlock()
	if e.pkgs.g != nil {
		return e.pkgs.g, nil
	}
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	rows, err := e.db.QueryContext(ctx, `SELECT f.package, t.package, COUNT(*)
FROM rule_dep d
JOIN target f ON f.id = d.from_id
JOIN target t ON t.id = d.to_id
WHERE f.package <> t.package
GROUP BY f.package, t.package`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	g := newPkgGraph()
	for rows.Next() {
		var from, to string
		var n int
		if err := rows.Scan(&from, &to, &n); err != nil {
			return nil, err
		}
		g.add(from, to, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	e.pkgs.g = g
	return g, nil
}
