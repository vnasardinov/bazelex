// Command bazelex_ui serves a single web page that runs the example queries
// from the README against a database written by bazelex.
package main

import (
	"database/sql"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	_ "modernc.org/sqlite"

	"github.com/vnasardinov/bazelex/internal/ui"
)

var (
	httpAddr = flag.String("http", ":6060", "address to listen on")
	dbPath   = flag.String("db", "bazelex.sqlite", "SQLite database written by bazelex")
	pageRows = flag.Int("rows", 20, "maximum number of result rows to show at once; longer results are paginated")
	timeout  = flag.Duration("timeout", 120*time.Second, "limit on how long each query may run")
)

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
	if *timeout <= 0 {
		log.Fatalf("-timeout must be positive, got %v", *timeout)
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
	log.Fatal(http.ListenAndServe(*httpAddr, ui.NewServer(db, *dbPath, *pageRows, *timeout)))
}
