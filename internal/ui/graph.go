package ui

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/goccy/go-graphviz"

	"github.com/vnasardinov/bazelex/internal/quotient"
)

const (
	defaultDepth = 1
	maxDepth     = 10
	// maxGraphNodes bounds the drawing. Graphviz's layout time grows steeply
	// with the number of edges (130 packages and 1000 edges took 90 seconds on a
	// sample package in a large workspace), and the picture is unreadable long before that.
	maxGraphNodes = 60
)

// by selects which quotient the page draws: the package graph, or its SCC
// condensation, whose nodes are dependency cycles. They are the quotient names
// the database uses.
const (
	byPackage = "package"
	bySCC     = "scc"
)

//go:embed graph.html
var graphHTML string

var graphTmpl = template.Must(template.New("graph").Parse(graphHTML))

// dotSpec is what dot draws: a graph, the nodes of it to show, and how to mark
// them. A zero field means the default: labels fall back to the node names, no
// node is colored, and the nodes are not grouped into layers.
type dotSpec struct {
	graph   *quotient.Graph
	nodes   []string
	centers []string          // filled gold
	cycle   []string          // filled red, and their mutual edges red
	labels  map[string]string // node -> label; a missing node uses its name
	layers  [][]string        // rank=same groups, in order; nil draws none
	link    func(node string) string
}

// dot returns the DOT source for the subgraph of s.graph induced by s.nodes.
// s.layers, when set, groups the nodes into rank=same subgraphs, so the drawing
// is the topological layering those groups describe. Each node links to
// s.link(node).
func dot(s dotSpec) string {
	in := map[string]bool{}
	for _, n := range s.nodes {
		in[n] = true
	}
	var b strings.Builder
	b.WriteString("digraph {\n  rankdir=LR;\n  nslimit=1; mclimit=0.2;\n  node [shape=box, style=filled, fillcolor=white, fontname=Courier, fontsize=10];\n")
	for _, layer := range s.layers {
		var group []string
		for _, n := range layer {
			if in[n] {
				group = append(group, dotQuote(n))
			}
		}
		if len(group) > 0 {
			fmt.Fprintf(&b, "  { rank=same; %s; }\n", strings.Join(group, "; "))
		}
	}
	for _, n := range s.nodes {
		attrs := fmt.Sprintf("label=%s, URL=%s, tooltip=%s", dotQuote(s.label(n)), dotQuote(s.link(n)), dotQuote(s.label(n)))
		switch {
		case slices.Contains(s.centers, n):
			attrs += ", fillcolor=gold"
		case slices.Contains(s.cycle, n):
			attrs += ", fillcolor=lightsalmon"
		}
		fmt.Fprintf(&b, "  %s [%s];\n", dotQuote(n), attrs)
	}
	for _, from := range s.nodes {
		for _, d := range s.graph.Deps(from) {
			if !in[d.To] {
				continue
			}
			// Edge labels would each become a layout node and make the layout
			// several times slower, so the weight is the line width and tooltip.
			attrs := fmt.Sprintf("penwidth=%.1f, tooltip=%s", 1+math.Log10(float64(d.Weight)), dotQuote(fmt.Sprintf("%s -> %s: %d dependencies", s.label(from), s.label(d.To), d.Weight)))
			if slices.Contains(s.cycle, from) && slices.Contains(s.cycle, d.To) {
				attrs += ", color=red"
			}
			fmt.Fprintf(&b, "  %s -> %s [%s];\n", dotQuote(from), dotQuote(d.To), attrs)
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// label returns the label of node: the one s.labels gives, or its own name.
func (s dotSpec) label(node string) string {
	if l, ok := s.labels[node]; ok {
		return l
	}
	return pkgName(node)
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

// renderSVG lays out the DOT source and returns the SVG element, without the
// XML prolog and doctype, so that it can be embedded in a page or saved as is.
//
// Creating a Graphviz instance compiles the WASM module, which is slow and
// memory-hungry, so the server creates one on first use and keeps it. An
// instance is not safe for concurrent use, so renders take turns.
func (s *server) renderSVG(ctx context.Context, src string) (string, error) {
	s.gvMu.Lock()
	defer s.gvMu.Unlock()
	if s.gv == nil {
		// Not ctx: the instance outlives this request.
		g, err := graphviz.New(context.Background())
		if err != nil {
			return "", err
		}
		s.gv = g
	}
	g := s.gv
	gr, err := graphviz.ParseBytes([]byte(src))
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
	return svg, nil
}

// graphKey is the input of a drawing, with depth already clamped. The database
// is read-only, so equal keys always give the same drawing.
type graphKey struct {
	pkg   string
	depth int
	cycle bool   // draw the whole dependency cycle of pkg as the center
	by    string // the quotient to draw: byPackage or bySCC
}

// params returns the key as URL query parameters.
func (k graphKey) params() string {
	p := url.Values{"pkg": {k.pkg}, "depth": {strconv.Itoa(k.depth)}}
	if k.by == bySCC {
		p.Set("by", bySCC)
	}
	if k.cycle {
		p.Set("cycle", "1")
	}
	return p.Encode()
}

// withPkg returns k centered on pkg, keeping the rest of the request.
func (k graphKey) withPkg(pkg string) graphKey {
	k.pkg = pkg
	return k
}

// switched returns k with the other quotient selected: the package graph
// centered on pkg's dependency cycle, or the component graph centered on pkg's
// component.
func (k graphKey) switched() graphKey {
	if k.by == bySCC {
		return graphKey{pkg: k.pkg, depth: k.depth, cycle: true, by: byPackage}
	}
	return graphKey{pkg: k.pkg, depth: k.depth, by: bySCC}
}

// graphURL returns the link to the graph page for k.
func graphURL(k graphKey) string {
	return "graph?" + k.params()
}

// parseGraphKey reads the graph parameters of r. ok is false if there is no pkg.
func parseGraphKey(r *http.Request) (k graphKey, ok bool) {
	q := r.URL.Query()
	k = graphKey{pkg: strings.TrimSpace(q.Get("pkg")), depth: defaultDepth, by: byPackage}
	if q.Get("by") == bySCC {
		// The condensation is acyclic, so there is no cycle to center on.
		k.by = bySCC
	} else {
		k.cycle = q.Get("cycle") != ""
	}
	if n, err := strconv.Atoi(q.Get("depth")); err == nil && n >= 0 {
		k.depth = min(n, maxDepth)
	}
	return k, q.Has("pkg")
}

// drawing is a laid-out graph. It is immutable once cached.
type drawing struct {
	dot, svg  string
	notice    string
	nodes     int
	cycleSize int // packages in the centered dependency cycle (or component)
}

// maxCachedDrawings bounds the cache. A dense drawing of maxGraphNodes packages
// is about 360 KB (60 KB of DOT and 300 KB of SVG, measured on a reasonably large example project),
// so this is at most about 25 MB.
const maxCachedDrawings = 64

// drawingCache holds the most recent drawings, evicting the oldest first. The
// zero value is ready to use.
type drawingCache struct {
	mu    sync.Mutex
	m     map[graphKey]*drawing
	order []graphKey // insertion order
}

func (c *drawingCache) get(k graphKey) *drawing {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[k]
}

func (c *drawingCache) put(k graphKey, d *drawing) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[graphKey]*drawing{}
	}
	if _, ok := c.m[k]; !ok {
		if len(c.order) == maxCachedDrawings {
			delete(c.m, c.order[0])
			c.order = c.order[1:]
		}
		c.order = append(c.order, k)
	}
	c.m[k] = d
}

// nothingToDrawError says that there is no drawing for a package: it is not in
// the graph at all, or it is a node with no dependencies across packages.
type nothingToDrawError string

func (e nothingToDrawError) Error() string {
	return fmt.Sprintf("Package %q has no dependencies on or from other packages.", string(e))
}

// draw returns the drawing for k, laying it out only if it is not cached.
// Failures are not cached.
func (s *server) draw(ctx context.Context, k graphKey) (*drawing, error) {
	if d := s.drawings.get(k); d != nil {
		return d, nil
	}
	d := &drawing{}
	var err error
	if k.by == bySCC {
		err = s.drawComponents(ctx, k, d)
	} else {
		err = s.drawPackages(ctx, k, d)
	}
	if err != nil {
		return nil, err
	}
	if d.svg, err = s.renderSVG(ctx, d.dot); err != nil {
		return nil, err
	}
	s.drawings.put(k, d)
	return d, nil
}

// drawPackages fills d with the drawing of k over the package quotient: the
// neighborhood of k.pkg out to k.depth, or of its whole dependency cycle when
// k.cycle is set.
func (s *server) drawPackages(ctx context.Context, k graphKey, d *drawing) error {
	g, err := s.eng.PackageGraph(ctx)
	if err != nil {
		return err
	}
	if !g.HasEdges(k.pkg) {
		return nothingToDrawError(k.pkg)
	}
	centers := []string{k.pkg}
	cycle := g.Component(k.pkg)
	d.cycleSize = len(cycle)
	if k.cycle {
		centers = cycle
	}
	nodes, truncated := g.Neighborhood(centers, k.depth, maxGraphNodes)
	d.nodes = len(nodes)
	if truncated {
		d.notice = fmt.Sprintf("Showing the %d packages nearest the center; lower the depth to see everything within it.", maxGraphNodes)
	}
	if len(cycle) < 2 {
		cycle = nil // a lone package is not a cycle
	}
	d.dot = dot(dotSpec{graph: g, nodes: nodes, centers: centers, cycle: cycle, link: func(p string) string { return graphURL(k.withPkg(p)) }})
	return nil
}

// drawComponents fills d with the drawing of k over the SCC condensation: the
// neighborhood of the component k.pkg belongs to, out to k.depth, drawn in
// topological layers. A component is a dependency cycle, named after its
// smallest member, and holds one or more packages.
func (s *server) drawComponents(ctx context.Context, k graphKey, d *drawing) error {
	g, component, err := s.eng.CondensedGraph(ctx)
	if err != nil {
		return err
	}
	center, ok := component[k.pkg]
	if !ok {
		return nothingToDrawError(k.pkg)
	}
	sizes := map[string]int{}
	for _, c := range component {
		sizes[c]++
	}
	d.cycleSize = sizes[center]
	nodes, truncated := g.Neighborhood([]string{center}, k.depth, maxGraphNodes)
	d.nodes = len(nodes)
	if truncated {
		d.notice = fmt.Sprintf("Showing the %d components nearest the center; lower the depth to see everything within it.", maxGraphNodes)
	}
	labels := make(map[string]string, len(nodes))
	for _, n := range nodes {
		labels[n] = componentLabel(n, sizes[n])
	}
	d.dot = dot(dotSpec{
		graph:   g,
		nodes:   nodes,
		centers: []string{center},
		labels:  labels,
		layers:  layeredNodes(g, nodes),
		link:    func(n string) string { return graphURL(k.withPkg(n)) },
	})
	return nil
}

// componentLabel names a node of the condensation: the package the component is
// named after, and how many packages it holds when it is a real cycle.
func componentLabel(name string, size int) string {
	if size > 1 {
		return fmt.Sprintf("%s (%d)", pkgName(name), size)
	}
	return pkgName(name)
}

// layeredNodes groups the drawn nodes by their layer in g's topological
// layering, in layer order, so that dot can draw each layer as a rank=same
// group.
func layeredNodes(g *quotient.Graph, nodes []string) [][]string {
	drawn := map[string]bool{}
	for _, n := range nodes {
		drawn[n] = true
	}
	var layers [][]string
	for _, layer := range quotient.Layers(g) {
		var group []string
		for _, n := range layer {
			if drawn[n] {
				group = append(group, n)
			}
		}
		if len(group) > 0 {
			layers = append(layers, group)
		}
	}
	return layers
}

// graphView is what graph.html renders.
type graphView struct {
	DB        string
	Pkg       string
	HasPkg    bool
	Depth     int
	Cycle     bool
	SCC       bool // the component graph, not the package graph
	MaxDepth  int
	Err       string
	Notice    string
	SVG       template.HTML
	Nodes     int
	CycleSize int
	DotURL    string // download links for the drawing
	SVGURL    string
	SwitchURL string // the same drawing in the other quotient
}

func (s *server) graph(w http.ResponseWriter, r *http.Request) {
	k, hasPkg := parseGraphKey(r)
	v := graphView{DB: s.dbName, Pkg: k.pkg, HasPkg: hasPkg, Depth: k.depth, Cycle: k.cycle, SCC: k.by == bySCC, MaxDepth: maxDepth}
	if hasPkg {
		if d, err := s.draw(r.Context(), k); err != nil {
			v.Err = err.Error()
		} else {
			v.Notice, v.Nodes, v.CycleSize = d.notice, d.nodes, d.cycleSize
			// The SVG is Graphviz's own output, and every string it embeds is escaped.
			v.SVG = template.HTML(d.svg)
			v.DotURL, v.SVGURL = "graph.dot?"+k.params(), "graph.svg?"+k.params()
			v.SwitchURL = graphURL(k.switched())
		}
	}
	if err := graphTmpl.Execute(w, v); err != nil {
		log.Printf("render: %v", err)
	}
}

// graphFile serves one part of the drawing for the request's parameters as a
// download named after them, with ext as its extension.
func (s *server) graphFile(w http.ResponseWriter, r *http.Request, ext, contentType string, part func(*drawing) string) {
	k, ok := parseGraphKey(r)
	if !ok {
		http.Error(w, "missing pkg parameter", http.StatusBadRequest)
		return
	}
	d, err := s.draw(r.Context(), k)
	var noGraph nothingToDrawError
	switch {
	case errors.As(err, &noGraph):
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	case err != nil:
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": k.filename(ext)}))
	io.WriteString(w, part(d))
}

var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// filename returns a file name for the drawing of k, such as
// "graph-comp_core-depth1-cycle.dot" or "graph-scc-comp_core-depth1.svg".
func (k graphKey) filename(ext string) string {
	pkg := unsafeFilenameChars.ReplaceAllString(k.pkg, "_")
	if pkg == "" {
		pkg = "root"
	}
	name := "graph"
	if k.by == bySCC {
		name += "-" + bySCC
	}
	name += fmt.Sprintf("-%s-depth%d", pkg, k.depth)
	if k.cycle {
		name += "-cycle"
	}
	return name + "." + ext
}
