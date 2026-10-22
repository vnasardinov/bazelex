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
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

const (
	queryTimeout = 10 * time.Second
	maxLabels    = 20000
)

//go:embed index.html
var indexHTML string

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

type server struct {
	db       *sql.DB
	dbName   string
	pageRows int // rows shown per page
}

// NewServer returns a handler serving the page for db. dbName is shown in the
// page header, and pageRows is the maximum number of result rows shown at once.
func NewServer(db *sql.DB, dbName string, pageRows int) http.Handler {
	s := &server{db: db, dbName: dbName, pageRows: pageRows}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	return mux
}

// cell is one value in a result row. URL is set when it should be a link, and
// NewTab when the link should open in a new browser tab.
type cell struct {
	Text   string
	URL    string
	NewTab bool
}

// queryView is what the template renders for one query.
type queryView struct {
	Query
	Values  map[string]string // current input values, by param name
	Ran     bool
	Err     string
	Columns []string
	Rows    [][]cell
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
	page := pageData{DB: s.dbName, Limit: s.pageRows, Labels: s.ruleLabels(r.Context())}
	requested := map[string]bool{}
	for _, id := range r.URL.Query()["run"] {
		requested[id] = true
	}
	for _, q := range queries {
		v := queryView{Query: q, Values: map[string]string{}, Limit: s.pageRows, Page: 1}
		if n, err := strconv.Atoi(r.URL.Query().Get(q.ID + ".page")); err == nil && n > 1 {
			v.Page = n
		}
		complete := true
		for _, p := range q.Params {
			val := strings.TrimSpace(r.URL.Query().Get(q.ID + "." + p.Name))
			v.Values[p.Name] = val
			complete = complete && val != ""
		}
		switch {
		case !complete:
		case q.Expensive && !requested[q.ID]:
			v.Skipped = true
		default:
			hasNext := s.run(r.Context(), &v)
			v.addRowLinks(r.URL.Query())
			v.FirstRow = (v.Page-1)*s.pageRows + 1
			v.LastRow = v.FirstRow + len(v.Rows) - 1
			if v.Page > 1 {
				v.PrevURL = pageURL(r.URL.Query(), q.ID, v.Page-1)
			}
			if hasNext {
				v.NextURL = pageURL(r.URL.Query(), q.ID, v.Page+1)
			}
		}
		if q.selfContained() {
			page.SelfContained = append(page.SelfContained, v)
		} else {
			page.Parameterized = append(page.Parameterized, v)
		}
	}
	if err := indexTmpl.Execute(w, page); err != nil {
		log.Printf("render: %v", err)
	}
}

// addRowLinks turns the values of v's RowLink column into links.
func (v *queryView) addRowLinks(params url.Values) {
	if v.RowLink == nil {
		return
	}
	col := slices.Index(v.Columns, v.RowLink.Column)
	if col < 0 {
		return
	}
	if v.RowLink.NewTab {
		p := url.Values{}
		for k, vs := range params {
			p[k] = vs
		}
		p.Del("run")
		params = p
	}
	for _, row := range v.Rows {
		row[col].URL = linkURL(params, v.RowLink.ToQuery, v.RowLink.ToParam, row[col].Text)
		row[col].NewTab = v.RowLink.NewTab
	}
}

// linkURL returns a link that opens query id with its parameter set to value,
// at its first page, keeping the rest of the current URL's parameters.
func linkURL(params url.Values, id, param, value string) string {
	p := url.Values{}
	for k, vs := range params {
		p[k] = vs
	}
	p.Set(id+"."+param, value)
	p.Del(id + ".page")
	return "?" + p.Encode() + "#" + id
}

// pageURL returns a link to the given page of query id that keeps the rest of
// the current URL's parameters.
func pageURL(params url.Values, id string, page int) string {
	p := url.Values{}
	for k, vs := range params {
		p[k] = vs
	}
	if page > 1 {
		p.Set(id+".page", strconv.Itoa(page))
	} else {
		p.Del(id + ".page")
	}
	return "?" + p.Encode() + "#" + id
}

// run executes v's query and fills in the result fields with the rows of page
// v.Page. It reports whether there are more rows after that page.
func (s *server) run(ctx context.Context, v *queryView) (hasNext bool) {
	v.Ran = true
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	args := make([]any, 0, len(v.Params))
	for _, p := range v.Params {
		args = append(args, sql.Named(p.Name, v.Values[p.Name]))
	}
	rows, err := s.db.QueryContext(ctx, v.SQL, args...)
	if err != nil {
		v.Err = err.Error()
		return
	}
	defer rows.Close()
	if v.Columns, err = rows.Columns(); err != nil {
		v.Err = err.Error()
		return
	}
	skip := (v.Page - 1) * s.pageRows
	for rows.Next() {
		if skip > 0 {
			skip--
			continue
		}
		if len(v.Rows) == s.pageRows {
			return true
		}
		vals := make([]any, len(v.Columns))
		ptrs := make([]any, len(vals))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			v.Err = err.Error()
			return
		}
		row := make([]cell, len(vals))
		for i, x := range vals {
			switch x := x.(type) {
			case nil:
				row[i].Text = "NULL"
			case []byte:
				row[i].Text = string(x)
			default:
				row[i].Text = fmt.Sprint(x)
			}
		}
		v.Rows = append(v.Rows, row)
	}
	if err := rows.Err(); err != nil {
		v.Err = err.Error()
	}
	return false
}

// ruleLabels returns rule labels for input autocompletion. Failure only costs
// the autocompletion, so it is logged rather than reported.
func (s *server) ruleLabels(ctx context.Context) []string {
	rows, err := s.db.QueryContext(ctx,
		`SELECT label FROM target WHERE kind = 'rule' ORDER BY label LIMIT ?`, maxLabels)
	if err != nil {
		log.Printf("labels: %v", err)
		return nil
	}
	defer rows.Close()
	var labels []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			log.Printf("labels: %v", err)
			break
		}
		labels = append(labels, l)
	}
	return labels
}
