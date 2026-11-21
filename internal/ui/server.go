// Package ui is the web page that runs the example queries from the README
// against a database written by bazelex.
package ui

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"html/template"
	"log"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/goccy/go-graphviz"

	"github.com/vnasardinov/bazelex/internal/engine"
)

// maxLabels caps the label list offered for autocompletion, so that a large
// workspace does not put one <option> per rule into the page.
const maxLabels = 20000

//go:embed index.html
var indexHTML string

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

// server serves one database, configured by the Config NewServer was given.
type server struct {
	eng      *engine.Engine // runs SQL, Datalog and the package graph
	dbName   string
	pageRows int
	parallel int

	gvMu sync.Mutex
	gv   *graphviz.Graphviz // created on first render, never closed

	drawings drawingCache // laid-out graphs, by request
}

// Config configures the page. PageRows, Parallel and Timeout must be positive,
// and DBName names the database in the page header, so the zero value is not
// usable; Validate reports what is wrong with it.
type Config struct {
	DBName   string        // shown in the page header
	PageRows int           // maximum number of result rows shown at once
	Parallel int           // how many queries may run at once
	Timeout  time.Duration // limit on how long each query may run
}

// Validate reports whether c can be used to build a server.
func (c Config) Validate() error {
	switch {
	case c.PageRows < 1:
		return fmt.Errorf("page rows must be at least 1, got %d", c.PageRows)
	case c.Parallel < 1:
		return fmt.Errorf("parallel must be at least 1, got %d", c.Parallel)
	case c.Timeout <= 0:
		return fmt.Errorf("timeout must be positive, got %v", c.Timeout)
	}
	return nil
}

// NewServer returns a handler serving the page for db, as configured by cfg. It
// returns an error if cfg is not usable.
func NewServer(db *sql.DB, cfg Config) (http.Handler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	eng, err := engine.Open(db, engine.Config{Timeout: cfg.Timeout})
	if err != nil {
		return nil, err
	}
	s := &server{eng: eng, dbName: cfg.DBName, pageRows: cfg.PageRows, parallel: cfg.Parallel}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET /q/{id}", s.fragment)
	mux.HandleFunc("GET /graph", s.graph)
	mux.HandleFunc("GET /datalog", s.datalog)
	mux.HandleFunc("GET /graph.dot", func(w http.ResponseWriter, r *http.Request) {
		s.graphFile(w, r, "dot", "text/vnd.graphviz; charset=utf-8", func(d *drawing) string { return d.dot })
	})
	mux.HandleFunc("GET /graph.svg", func(w http.ResponseWriter, r *http.Request) {
		s.graphFile(w, r, "svg", "image/svg+xml", func(d *drawing) string { return d.svg })
	})
	return mux, nil
}

// cell is one value in a result row. URL is set when it should be a link, and
// NewTab when the link should open in a new browser tab. Num is set for numbers,
// which are right-aligned.
type cell struct {
	Text   string
	URL    string
	NewTab bool
	Num    bool
}

// queryView is what the template renders for one query.
type queryView struct {
	Query
	Values  map[string]string // current input values, by param name
	Ran     bool
	Err     string
	Columns []string
	Rows    [][]cell
	NumCols []bool // columns whose every value is a number, for aligning the header
	Limit   int
	// Page is the 1-based page of results shown. PrevURL and NextURL are empty
	// when there is no such page.
	Page     int
	FirstRow int // 1-based numbers of the first and last rows shown
	LastRow  int
	PrevURL  string
	NextURL  string
	// Skipped is set for an expensive query that wasn't requested.
	Skipped bool
	RunURL  string // link that runs a skipped query
}

type pageData struct {
	DB     string
	Limit  int
	Labels []string
	// SelfContained queries need no input; Parameterized ones do.
	SelfContained []queryView
	Parameterized []queryView
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	page := pageData{DB: s.dbName, Limit: s.pageRows, Labels: s.ruleLabels(r.Context())}
	for _, v := range s.views(r.Context(), params) {
		if v.selfContained() {
			page.SelfContained = append(page.SelfContained, v)
		} else {
			page.Parameterized = append(page.Parameterized, v)
		}
	}
	if err := indexTmpl.Execute(w, page); err != nil {
		log.Printf("render: %v", err)
	}
}

// views builds the view of every canned query for the given URL parameters, in
// the order of queries. The cheap ones run in parallel, at most s.parallel at a
// time: SQLite allows concurrent readers, each on its own connection, as long as
// the database is opened read-only. Expensive queries are rare and may be slow,
// so they run afterwards, one at a time, rather than tying up a slot; a skipped
// one runs no SQL at all.
func (s *server) views(ctx context.Context, params url.Values) []queryView {
	views := make([]queryView, len(queries))
	var wg sync.WaitGroup
	sem := make(chan struct{}, s.parallel)
	for i, q := range queries {
		if q.Expensive {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			views[i] = s.view(ctx, q, params)
		}()
	}
	wg.Wait()
	for i, q := range queries {
		if q.Expensive {
			views[i] = s.view(ctx, q, params)
		}
	}
	return views
}

// fragment serves the inner part of one query's section, so that script can
// refresh a single query without reloading the page. It takes the same URL
// parameters as the page.
func (s *server) fragment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	i := slices.IndexFunc(queries, func(q Query) bool { return q.ID == id })
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	v := s.view(r.Context(), queries[i], r.URL.Query())
	if err := indexTmpl.ExecuteTemplate(w, "inner", v); err != nil {
		log.Printf("render: %v", err)
	}
}

// view builds the view of q for the given URL parameters, running the query
// unless it is incomplete or expensive and not requested.
func (s *server) view(ctx context.Context, q Query, params url.Values) queryView {
	v := queryView{Query: q, Values: map[string]string{}, Limit: s.pageRows, Page: 1}
	if n, err := strconv.Atoi(params.Get(q.ID + ".page")); err == nil && n > 1 {
		v.Page = n
	}
	complete := true
	for _, p := range q.Params {
		val := strings.TrimSpace(params.Get(q.ID + "." + p.Name))
		v.Values[p.Name] = val
		complete = complete && val != ""
	}
	switch {
	case !complete:
		// Some input is missing, so show the form and nothing else.
	case q.Expensive && !slices.Contains(params["run"], q.ID):
		v.Skipped = true
		p := maps.Clone(params)
		p.Add("run", q.ID)
		v.RunURL = "?" + p.Encode() + "#" + q.ID
	default:
		hasNext, err := s.run(ctx, &v)
		if err != nil {
			v.Err = err.Error()
		}
		v.addLinks(params)
		v.markNumericColumns()
		v.paginate(params, hasNext)
	}
	return v
}

// paginate sets the row range shown and the links to the neighbouring pages.
// hasNext says whether there are rows after the current page.
func (v *queryView) paginate(params url.Values, hasNext bool) {
	v.FirstRow = (v.Page-1)*v.Limit + 1
	v.LastRow = v.FirstRow + len(v.Rows) - 1
	if v.Page > 1 {
		v.PrevURL = pageURL(params, v.ID, v.Page-1)
	}
	if hasNext {
		v.NextURL = pageURL(params, v.ID, v.Page+1)
	}
}

// markNumericColumns records which columns hold only numbers.
func (v *queryView) markNumericColumns() {
	v.NumCols = make([]bool, len(v.Columns))
	for i := range v.NumCols {
		v.NumCols[i] = v.columnIsNumeric(i)
	}
}

// columnIsNumeric reports whether column i holds a number in every row. A
// column with no rows is not numeric.
func (v *queryView) columnIsNumeric(i int) bool {
	return len(v.Rows) > 0 && !slices.ContainsFunc(v.Rows, func(row []cell) bool { return !row[i].Num })
}

// addLinks turns the values of v's link columns into links: the package graph
// links of GraphColumn and the query links of RowLink.
func (v *queryView) addLinks(params url.Values) {
	v.addGraphLinks()
	v.addQueryLinks(params)
}

// addGraphLinks turns the values of v's GraphColumn into links to the package
// graph page, centered on the package's dependency cycle.
func (v *queryView) addGraphLinks() {
	if v.GraphColumn == "" {
		return
	}
	col := slices.Index(v.Columns, v.GraphColumn)
	if col < 0 {
		return
	}
	for _, row := range v.Rows {
		row[col].URL = graphURL(graphKey{pkg: row[col].Text, depth: defaultDepth, cycle: true, by: byPackage})
		row[col].NewTab = true
	}
}

// addQueryLinks turns the values of v's RowLink column into links that open
// RowLink.ToQuery.
func (v *queryView) addQueryLinks(params url.Values) {
	if v.RowLink == nil {
		return
	}
	col := slices.Index(v.Columns, v.RowLink.Column)
	if col < 0 {
		return
	}
	if v.RowLink.NewTab {
		params = maps.Clone(params)
		delete(params, "run")
	}
	for _, row := range v.Rows {
		row[col].URL = linkURL(params, v.RowLink.ToQuery, v.RowLink.ToParam, row[col].Text)
		row[col].NewTab = v.RowLink.NewTab
	}
}

// linkURL returns a link that opens query id with its parameter set to value,
// at its first page, keeping the rest of the current URL's parameters.
func linkURL(params url.Values, id, param, value string) string {
	p := maps.Clone(params)
	p.Set(id+"."+param, value)
	p.Del(id + ".page")
	return "?" + p.Encode() + "#" + id
}

// pageURL returns a link to the given page of query id that keeps the rest of
// the current URL's parameters.
func pageURL(params url.Values, id string, page int) string {
	p := maps.Clone(params)
	if page > 1 {
		p.Set(id+".page", strconv.Itoa(page))
	} else {
		p.Del(id + ".page")
	}
	return "?" + p.Encode() + "#" + id
}

// run executes v's query and fills in the result fields with the rows of page
// v.Page. It reports whether there are more rows after that page. On error,
// the rows read so far are kept.
func (s *server) run(ctx context.Context, v *queryView) (hasNext bool, err error) {
	v.Ran = true
	args := make([]any, 0, len(v.Params))
	for _, p := range v.Params {
		args = append(args, sql.Named(p.Name, v.Values[p.Name]))
	}
	t, hasNext, err := s.eng.QueryPage(ctx, (v.Page-1)*s.pageRows, s.pageRows, v.SQL, args...)
	if err != nil {
		return false, err
	}
	v.Columns = t.Columns
	for _, r := range t.Rows {
		row := make([]cell, len(r))
		for i, x := range r {
			row[i] = cellFor(x)
		}
		v.Rows = append(v.Rows, row)
	}
	return hasNext, nil
}

// cellFor formats a value scanned from the database. Numbers are marked so
// they are right-aligned.
func cellFor(x any) cell {
	return cell{Text: engine.Format(x), Num: engine.IsNumber(x)}
}

// ruleLabels returns rule labels for input autocompletion. Failure only costs
// the autocompletion, so it is logged rather than reported.
func (s *server) ruleLabels(ctx context.Context) []string {
	labels, err := s.eng.RuleLabels(ctx, maxLabels)
	if err != nil {
		log.Printf("labels: %v", err)
		return nil
	}
	return labels
}
