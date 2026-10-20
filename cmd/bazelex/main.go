// Command bazelex dumps the static target graph of a Bazel workspace, as
// reported by `bazel query`, into a SQLite database for later SQL querying.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	_ "modernc.org/sqlite"

	"github.com/vnasardinov/bazelex/internal/graphdb"
)

var (
	dbPath    = flag.String("db", "bazelex.sqlite", "path of the SQLite database to create; it is replaced if it exists")
	buildTool = flag.String("bazel", "bazel", "bazel binary to run")
	fromStdin = flag.Bool("stdin", false, "read `query --output=streamed_proto` output from stdin instead of running bazel")
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: bazelex [flags] [target patterns...]\n\nPatterns default to //...:all-targets, which includes source and generated files.\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if err := run(flag.Args()); err != nil {
		log.Fatal(err)
	}
}

func run(patterns []string) error {
	// Under `bazel run` the process starts in the runfiles tree, where bazel
	// refuses to run and a relative -db would end up. Go back to where the
	// user invoked the command.
	if dir := os.Getenv("BUILD_WORKING_DIRECTORY"); dir != "" {
		if err := os.Chdir(dir); err != nil {
			return err
		}
	}

	var in io.Reader = os.Stdin
	var cmd *exec.Cmd
	if !*fromStdin {
		if len(patterns) == 0 {
			patterns = []string{"//...:all-targets"}
		}
		args := append([]string{"query", "--output=streamed_proto", "--keep_going"}, patterns...)
		cmd = exec.Command(*buildTool, args...)
		cmd.Stderr = os.Stderr
		out, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		in = out
		log.Printf("running: %s %v", *buildTool, args)
		if err := cmd.Start(); err != nil {
			return err
		}
	}

	if err := os.Remove(*dbPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec(graphdb.Schema); err != nil {
		return err
	}

	n, err := graphdb.Load(db, in)
	if err != nil {
		return err
	}
	if cmd != nil {
		// With --keep_going, bazel exits 3 on partial failures. The graph is
		// still usable, so report it rather than failing, unless nothing loaded.
		if err := cmd.Wait(); err != nil {
			if n == 0 {
				return fmt.Errorf("bazel query: %w", err)
			}
			log.Printf("warning: bazel query: %v", err)
		}
	}
	log.Printf("loaded %d targets into %s", n, *dbPath)
	return nil
}
