// Command bazelex_ui serves a single web page that runs the parameterized
// example queries from the README against a database written by bazelex.
package main

import (
	"context"
	"database/sql"
	_ "embed"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	httpAddr = flag.String("http", ":6060", "address to listen on")
	dbPath   = flag.String("db", "bazelex.sqlite", "SQLite database written by bazelex")
	pageRows = flag.Int("rows", 20, "maximum number of result rows to show at once; longer results are paginated")
)

const (
	queryTimeout = 10 * time.Second
	maxLabels    = 20000
)

//go:embed index.html
var indexHTML string

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

func main() {
	flag.Parse()

	// Under `bazel run` the process starts in the runfiles tree; a relative
	// -db is meant relative to where the user typed the command.
	if dir := os.Getenv("BUILD_WORKING_DIRECTORY"); dir != "" {
		if err := os.Chdir(dir); err != nil {
			log.Fatal(err)
		}
	}
	if *pageRows < 1 {
		log.Fatalf("-rows must be at least 1, got %d", *pageRows)
	}
	if _, err := os.Stat(*dbPath); err != nil {
		log.Fatal(err)
	}
	// Read-only: the page only ever runs the canned queries, but there is no
	// reason to let it write.
	db, err := sql.Open("sqlite", "file:"+*dbPath+"?mode=ro")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}

	log.Printf("serving %s on %s", *dbPath, *httpAddr)
	log.Fatal(http.ListenAndServe(*httpAddr, newServer(db, *dbPath, *pageRows)))
}

type server struct {
	db       *sql.DB
	dbName   string
	pageRows int // rows shown per page
}

func newServer(db *sql.DB, dbName string, pageRows int) http.Handler {
	s := &server{db: db, dbName: dbName, pageRows: pageRows}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	return mux
}

// queryView is what the template renders for one query.
type queryView struct {
	Query
	Values  map[string]string // current input values, by param name
	Ran     bool
	Err     string
	Columns []string
	Rows    [][]string
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
		row := make([]string, len(vals))
		for i, x := range vals {
			switch x := x.(type) {
			case nil:
				row[i] = "NULL"
			case []byte:
				row[i] = string(x)
			default:
				row[i] = fmt.Sprint(x)
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
