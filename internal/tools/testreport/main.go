// Command testreport prints what no test through the built binary runs: the
// commands and the gate record keys, and then the number of tests in each
// package as e2e, unit or other. It reads the command tree and the test files
// and nothing else, and it is a report, not a check: the exit is 0 however long
// the list is. The check is the guard in test/e2e.
//
// `./task test-report` runs it.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/bbsnly/sdlc/internal/testscan"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run exits 0 with the report on stdout, 1 for a directory it cannot read, and
// 2 for words it does not take.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("testreport", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("C", ".", "the module to read")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		fmt.Fprintln(stderr, "testreport: takes only -C <dir>, the module to read")
		return 2
	}
	if err := testscan.Report(*dir, stdout); err != nil {
		fmt.Fprintf(stderr, "testreport: %v\n", err)
		return 1
	}
	return 0
}
