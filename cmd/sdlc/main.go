// Command sdlc is the delivery loop's command-line interface.
//
// This is the pre-release skeleton. The verbs land over the phases described in
// CHANGELOG.md; until then the binary reports its own version and says so.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is overwritten at release time by the build. Before the first tag it
// is the pre-release marker, which several messages read to explain themselves.
var version = "0.0.0-dev"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) > 0 && (args[0] == "version" || args[0] == "--version" || args[0] == "-v") {
		_, err := fmt.Fprintln(out, version)
		return err
	}
	_, err := fmt.Fprintf(out, "sdlc %s (pre-release)\n\n"+
		"No verbs are implemented yet. Watch the repository for the first release:\n"+
		"  https://github.com/bbsnly/sdlc\n", version)
	return err
}
