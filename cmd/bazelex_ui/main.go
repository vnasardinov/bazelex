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
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

var (
	httpAddr = flag.String("http", ":6060", "address to listen on")
	dbPath   = flag.String("db", "bazelex.sqlite", "SQLite database written by bazelex")
)

const (
	rowLimit     = 20
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
	log.Fatal(http.ListenAndServe(*httpAddr, newServer(db, *dbPath)))
}

type server struct {
	db     *sql.DB
	dbName string
}

func newServer(db *sql.DB, dbName string) http.Handler {
	s := &server{db: db, dbName: dbName}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.index)
	return mux
}

// queryView is what the template renders for one query.
type queryView struct {
	Query
	Values    map[string]string // current input values, by param name
	Ran       bool
	Err       string
	Columns   []string
	Rows      [][]string
	Truncated bool
	Limit     int
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
	page := pageData{DB: s.dbName, Limit: rowLimit, Labels: s.ruleLabels(r.Context())}
	requested := map[string]bool{}
	for _, id := range r.URL.Query()["run"] {
		requested[id] = true
	}
	for _, q := range queries {
		v := queryView{Query: q, Values: map[string]string{}, Limit: rowLimit}
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
			s.run(r.Context(), &v)
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

// run executes v's query and fills in the result fields.
func (s *server) run(ctx context.Context, v *queryView) {
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
	for rows.Next() {
		if len(v.Rows) == rowLimit {
			v.Truncated = true
			return
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
