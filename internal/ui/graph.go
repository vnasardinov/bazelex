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

//go:embed graph.html
var graphHTML string

var graphTmpl = template.Must(template.New("graph").Parse(graphHTML))

// dot returns the DOT source for the subgraph induced by nodes. centers are
// filled gold, and members of the cycle red. Each node links to link(pkg).
func dot(g *quotient.Graph, nodes, centers, cycle []string, link func(pkg string) string) string {
	in := map[string]bool{}
	for _, p := range nodes {
		in[p] = true
	}
	var b strings.Builder
	b.WriteString("digraph {\n  rankdir=LR;\n  nslimit=1; mclimit=0.2;\n  node [shape=box, style=filled, fillcolor=white, fontname=Courier, fontsize=10];\n")
	for _, p := range nodes {
		attrs := fmt.Sprintf("label=%s, URL=%s, tooltip=%s", dotQuote(pkgName(p)), dotQuote(link(p)), dotQuote(pkgName(p)))
		switch {
		case slices.Contains(centers, p):
			attrs += ", fillcolor=gold"
		case slices.Contains(cycle, p):
			attrs += ", fillcolor=lightsalmon"
		}
		fmt.Fprintf(&b, "  %s [%s];\n", dotQuote(p), attrs)
	}
	for _, from := range nodes {
		for _, d := range g.Deps(from) {
			if !in[d.To] {
				continue
			}
			// Edge labels would each become a layout node and make the layout
			// several times slower, so the weight is the line width and tooltip.
			attrs := fmt.Sprintf("penwidth=%.1f, tooltip=%s", 1+math.Log10(float64(d.Weight)), dotQuote(fmt.Sprintf("%s -> %s: %d dependencies", pkgName(from), pkgName(d.To), d.Weight)))
			if slices.Contains(cycle, from) && slices.Contains(cycle, d.To) {
				attrs += ", color=red"
			}
			fmt.Fprintf(&b, "  %s -> %s [%s];\n", dotQuote(from), dotQuote(d.To), attrs)
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

// renderSVG lays out the DOT source and returns the SVG element, without the
// XML prolog and doctype, so that it can be embedded in a page or saved as is.
//
// Creating a Graphviz instance compiles the WASM module, which is slow and
// memory-hungry, so the server creates one on first use and keeps it. An
// instance is not safe for concurrent use, so renders take turns.
func (s *server) renderSVG(ctx context.Context, dot string) (string, error) {
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
	return svg, nil
}

// graphKey is the input of a drawing, with depth already clamped. The database
// is read-only, so equal keys always give the same drawing.
type graphKey struct {
	pkg   string
	depth int
	cycle bool // draw the whole dependency cycle of pkg as the center
}

// query returns the key as URL query parameters.
func (k graphKey) query() string {
	p := url.Values{"pkg": {k.pkg}, "depth": {strconv.Itoa(k.depth)}}
	if k.cycle {
		p.Set("cycle", "1")
	}
	return p.Encode()
}

// graphURL returns the link to the graph page centered on pkg.
func graphURL(pkg string, depth int, cycle bool) string {
	return "graph?" + graphKey{pkg, depth, cycle}.query()
}

// parseGraphKey reads the graph parameters of r. ok is false if there is no pkg.
func parseGraphKey(r *http.Request) (k graphKey, ok bool) {
	q := r.URL.Query()
	k = graphKey{pkg: strings.TrimSpace(q.Get("pkg")), depth: defaultDepth, cycle: q.Get("cycle") != ""}
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
	cycleSize int
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
	g, err := s.eng.PackageGraph(ctx)
	if err != nil {
		return nil, err
	}
	if !g.HasEdges(k.pkg) {
		return nil, nothingToDrawError(k.pkg)
	}
	d := &drawing{}
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
	d.dot = dot(g, nodes, centers, cycle, func(p string) string { return graphURL(p, k.depth, k.cycle) })
	if d.svg, err = s.renderSVG(ctx, d.dot); err != nil {
		return nil, err
	}
	s.drawings.put(k, d)
	return d, nil
}

// graphView is what graph.html renders.
type graphView struct {
	DB        string
	Pkg       string
	HasPkg    bool
	Depth     int
	Cycle     bool
	MaxDepth  int
	Err       string
	Notice    string
	SVG       template.HTML
	Nodes     int
	CycleSize int
	DotURL    string // download links for the drawing
	SVGURL    string
}

func (s *server) graph(w http.ResponseWriter, r *http.Request) {
	k, hasPkg := parseGraphKey(r)
	v := graphView{DB: s.dbName, Pkg: k.pkg, HasPkg: hasPkg, Depth: k.depth, Cycle: k.cycle, MaxDepth: maxDepth}
	if hasPkg {
		if d, err := s.draw(r.Context(), k); err != nil {
			v.Err = err.Error()
		} else {
			v.Notice, v.Nodes, v.CycleSize = d.notice, d.nodes, d.cycleSize
			// The SVG is Graphviz's own output, and every string it embeds is escaped.
			v.SVG = template.HTML(d.svg)
			v.DotURL, v.SVGURL = "graph.dot?"+k.query(), "graph.svg?"+k.query()
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
// "graph-comp_core-depth1-cycle.dot".
func (k graphKey) filename(ext string) string {
	pkg := unsafeFilenameChars.ReplaceAllString(k.pkg, "_")
	if pkg == "" {
		pkg = "root"
	}
	name := fmt.Sprintf("graph-%s-depth%d", pkg, k.depth)
	if k.cycle {
		name += "-cycle"
	}
	return name + "." + ext
}
