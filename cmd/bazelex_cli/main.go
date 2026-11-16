// Command bazelex_cli explores a database written by bazelex from the command
// line: run a custom SQL query, or evaluate a Mangle (Datalog) program over the
// build graph. It is the command-line front-end to the same exploration engine
// as bazelex_ui.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/vnasardinov/bazelex/internal/cli"
	"github.com/vnasardinov/bazelex/internal/engine"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "sql":
		err = runSQL(os.Args[2:])
	case "datalog":
		err = runDatalog(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q; want sql or datalog", os.Args[1])
	}
	if err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: bazelex_cli [command] [flags] [input]

Explore a database written by bazelex.

Commands:
  sql      run a SQL query and print its result
  datalog  evaluate a Mangle (Datalog) program over the build graph

Run "bazelex_cli sql -h" or "bazelex_cli datalog -h" for the flags of each.
`)
}

// options are the flags every command shares.
type options struct {
	db       string
	format   string
	timeout  time.Duration
	limit    int
	maxFacts int
}

// addCommon registers the shared flags on fs and returns where they land.
func addCommon(fs *flag.FlagSet) *options {
	o := &options{}
	fs.StringVar(&o.db, "db", "bazelex.sqlite", "SQLite database written by bazelex")
	fs.StringVar(&o.format, "format", "md", "output format: md, json or csv")
	fs.DurationVar(&o.timeout, "timeout", engine.DefaultTimeout, "limit on how long the input may run")
	fs.IntVar(&o.limit, "limit", 0, "print at most this many rows per table (0 means all)")
	return o
}

// open opens the database read-only. Under `bazel run` the process starts in
// the runfiles tree, where a relative -db would be wrong, so it first returns to
// where the user typed the command.
func (o *options) open() (*sql.DB, error) {
	if dir := os.Getenv("BUILD_WORKING_DIRECTORY"); dir != "" {
		if err := os.Chdir(dir); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(o.db); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+o.db+"?mode=ro")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// readInput returns the query or program named by args: no arguments or "-"
// reads stdin, "@file" reads that file, and anything else is joined by spaces,
// so an unquoted SQL query works.
func readInput(args []string) (string, error) {
	if len(args) == 0 || (len(args) == 1 && args[0] == "-") {
		b, err := io.ReadAll(os.Stdin)
		return string(b), err
	}
	if len(args) == 1 && strings.HasPrefix(args[0], "@") {
		b, err := os.ReadFile(args[0][1:])
		return string(b), err
	}
	return strings.Join(args, " "), nil
}

func runSQL(args []string) error {
	fs := flag.NewFlagSet("sql", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "usage: bazelex_cli sql [flags] [query]\n\n"+
			"Run a SQL query against the database and print its result. With no query, or\n"+
			"with -, the query is read from stdin; @file reads it from that file. Flags come\n"+
			"before the query.\n\n")
		fs.PrintDefaults()
	}
	o := addCommon(fs)
	fs.Parse(args)

	query, err := readInput(fs.Args())
	if err != nil {
		return err
	}
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("empty query: pass it as an argument, - for stdin, or @file")
	}
	format, err := cli.ParseFormat(o.format)
	if err != nil {
		return err
	}
	db, err := o.open()
	if err != nil {
		return err
	}
	defer db.Close()
	eng, err := engine.Open(db, engine.Config{Timeout: o.timeout})
	if err != nil {
		return err
	}

	// A limit keeps a large result out of memory; 0 reads all of it.
	t, _, err := eng.QueryPage(context.Background(), 0, o.limit, query)
	if err != nil {
		return err
	}
	return cli.WriteSQL(os.Stdout, t, format)
}

func runDatalog(args []string) error {
	fs := flag.NewFlagSet("datalog", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), "usage: bazelex_cli datalog [flags] [program]\n\n"+
			"Evaluate a Mangle (Datalog) program over the build graph and print the\n"+
			"relations it derives. With no program, or with -, the program is read from\n"+
			"stdin; @file reads it from that file. Flags come before the program.\n\n")
		fs.PrintDefaults()
	}
	o := addCommon(fs)
	fs.IntVar(&o.maxFacts, "max-facts", engine.DefaultMaxFacts, "stop the program after it derives this many facts")
	fs.Parse(args)

	program, err := readInput(fs.Args())
	if err != nil {
		return err
	}
	if strings.TrimSpace(program) == "" {
		return fmt.Errorf("empty program: pass it as an argument, - for stdin, or @file")
	}
	format, err := cli.ParseFormat(o.format)
	if err != nil {
		return err
	}
	db, err := o.open()
	if err != nil {
		return err
	}
	defer db.Close()
	eng, err := engine.Open(db, engine.Config{Timeout: o.timeout, MaxFacts: o.maxFacts})
	if err != nil {
		return err
	}

	res, err := eng.Datalog(context.Background(), program)
	if err != nil {
		return err
	}
	cli.LimitDatalog(res, o.limit)
	return cli.WriteDatalog(os.Stdout, res, format)
}
