package ui

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/goccy/go-graphviz"
)

const (
	defaultDepth = 1
	maxDepth     = 10
	// maxGraphNodes bounds the drawing. Graphviz's layout time grows steeply
	// with the number of edges (130 packages and 1000 edges took 90 seconds on a
	// sample package in a large workspace), and the picture is unreadable long before that.
	maxGraphNodes = 60
)

//go:embed graph.html
var graphHTML string

var graphTmpl = template.Must(template.New("graph").Parse(graphHTML))

// pkgGraph is the quotient of the rule graph by package: one node per package,
// and an edge a -> b weighted by the number of rule dependencies from a rule in
// a to a rule in b. Dependencies within a package are dropped.
type pkgGraph struct {
	out, in map[string]map[string]int
}

func newPkgGraph() *pkgGraph {
	return &pkgGraph{out: map[string]map[string]int{}, in: map[string]map[string]int{}}
}

func (g *pkgGraph) add(from, to string, n int) {
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

func (g *pkgGraph) has(pkg string) bool {
	_, o := g.out[pkg]
	_, i := g.in[pkg]
	return o || i
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

// component returns the packages in pkg's dependency cycle: those that reach
// pkg and are reached from it. It is just {pkg} if pkg is in no cycle.
func (g *pkgGraph) component(pkg string) []string {
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

// neighborhood returns the packages within depth edges of any of centers,
// following edges in either direction. Packages are added nearest first, in
// name order within a distance, until limit is reached; truncated says whether
// that left some out.
func (g *pkgGraph) neighborhood(centers []string, depth, limit int) (nodes []string, truncated bool) {
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

// dot returns the DOT source for the subgraph induced by nodes. centers are
// filled gold, and members of the cycle red. Each node links to graph(pkg).
func (g *pkgGraph) dot(nodes, centers, cycle []string, graph func(pkg string) string) string {
	in := map[string]bool{}
	for _, p := range nodes {
		in[p] = true
	}
	var b strings.Builder
	b.WriteString("digraph {\n  rankdir=LR;\n  nslimit=1; mclimit=0.2;\n  node [shape=box, style=filled, fillcolor=white, fontname=Courier, fontsize=10];\n")
	for _, p := range nodes {
		attrs := fmt.Sprintf("label=%s, URL=%s, tooltip=%s", dotQuote(pkgName(p)), dotQuote(graph(p)), dotQuote(pkgName(p)))
		switch {
		case slices.Contains(centers, p):
			attrs += ", fillcolor=gold"
		case slices.Contains(cycle, p):
			attrs += ", fillcolor=lightsalmon"
		}
		fmt.Fprintf(&b, "  %s [%s];\n", dotQuote(p), attrs)
	}
	for _, from := range nodes {
		tos := make([]string, 0, len(g.out[from]))
		for to := range g.out[from] {
			if in[to] {
				tos = append(tos, to)
			}
		}
		slices.Sort(tos)
		for _, to := range tos {
			// Edge labels would each become a layout node and make the layout
			// several times slower, so the weight is the line width and tooltip.
			n := g.out[from][to]
			attrs := fmt.Sprintf("penwidth=%.1f, tooltip=%s", 1+math.Log10(float64(n)), dotQuote(fmt.Sprintf("%s -> %s: %d dependencies", pkgName(from), pkgName(to), n)))
			if slices.Contains(cycle, from) && slices.Contains(cycle, to) {
				attrs += ", color=red"
			}
			fmt.Fprintf(&b, "  %s -> %s [%s];\n", dotQuote(from), dotQuote(to), attrs)
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// pkgName is how a package is shown; the root package has an empty name.
func pkgName(p string) string {
	if p == "" {
		return "//"
	}
	return p
}

// dotQuote returns s as a DOT quoted string.
func dotQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// renderSVG lays out the DOT source and returns the SVG as an element that can
// be embedded in a page, without the XML prolog and doctype.
func renderSVG(ctx context.Context, dot string) (template.HTML, error) {
	g, err := graphviz.New(ctx)
	if err != nil {
		return "", err
	}
	defer g.Close()
	gr, err := graphviz.ParseBytes([]byte(dot))
	if err != nil {
		return "", err
	}
	defer gr.Close()
	var buf bytes.Buffer
	if err := g.Render(ctx, gr, graphviz.SVG, &buf); err != nil {
		return "", err
	}
	svg := buf.String()
	if i := strings.Index(svg, "<svg"); i >= 0 {
		svg = svg[i:]
	}
	// The SVG is Graphviz's own output, and every string it embeds is escaped.
	return template.HTML(svg), nil
}

// loadPkgGraph reads the package quotient graph from the database. The
// database is read-only, so the result is kept for later requests.
func (s *server) loadPkgGraph(ctx context.Context) (*pkgGraph, error) {
	s.pkgMu.Lock()
	defer s.pkgMu.Unlock()
	if s.pkgs != nil {
		return s.pkgs, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT f.package, t.package, COUNT(*)
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
	s.pkgs = g
	return g, nil
}

// graphView is what graph.html renders.
type graphView struct {
	DB        string
	Pkg       string
	HasPkg    bool
	Depth     int
	Cycle     bool // draw the whole dependency cycle of Pkg as the center
	MaxDepth  int
	Err       string
	Notice    string
	SVG       template.HTML
	Nodes     int
	CycleSize int
}

// graphURL returns the link to the graph page centered on pkg.
func graphURL(pkg string, depth int, cycle bool) string {
	p := url.Values{"pkg": {pkg}, "depth": {strconv.Itoa(depth)}}
	if cycle {
		p.Set("cycle", "1")
	}
	return "graph?" + p.Encode()
}

func (s *server) graph(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v := graphView{DB: s.dbName, Pkg: strings.TrimSpace(q.Get("pkg")), HasPkg: q.Has("pkg"),
		Depth: defaultDepth, Cycle: q.Get("cycle") != "", MaxDepth: maxDepth}
	if n, err := strconv.Atoi(q.Get("depth")); err == nil && n >= 0 {
		v.Depth = min(n, maxDepth)
	}
	if v.HasPkg {
		s.fillGraph(r.Context(), &v)
	}
	if err := graphTmpl.Execute(w, v); err != nil {
		log.Printf("render: %v", err)
	}
}

// fillGraph runs the graph for v's inputs and sets its output fields.
func (s *server) fillGraph(ctx context.Context, v *graphView) {
	g, err := s.loadPkgGraph(ctx)
	if err != nil {
		v.Err = err.Error()
		return
	}
	if !g.has(v.Pkg) {
		v.Err = fmt.Sprintf("Package %q has no dependencies on or from other packages.", v.Pkg)
		return
	}
	centers := []string{v.Pkg}
	cycle := g.component(v.Pkg)
	v.CycleSize = len(cycle)
	if v.Cycle {
		centers = cycle
	}
	nodes, truncated := g.neighborhood(centers, v.Depth, maxGraphNodes)
	v.Nodes = len(nodes)
	if truncated {
		v.Notice = fmt.Sprintf("Showing the %d packages nearest the center; lower the depth to see everything within it.", maxGraphNodes)
	}
	if len(cycle) < 2 {
		cycle = nil // a lone package is not a cycle
	}
	dot := g.dot(nodes, centers, cycle, func(p string) string { return graphURL(p, v.Depth, v.Cycle) })
	if v.SVG, err = renderSVG(ctx, dot); err != nil {
		v.Err = err.Error()
	}
}
