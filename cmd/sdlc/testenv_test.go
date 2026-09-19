package main

import (
	"os"
	"testing"

	"github.com/bbsnly/sdlc/internal/testenv"
)

// mainHelperEnv lets a test re-exec this binary as a plain sdlc process: main
// itself -- the real os.Args, real streams, exitOnInterrupt, os.Exit(run(...))
// -- is otherwise never run, because calling it directly would end the test
// binary at its first os.Exit.
const mainHelperEnv = "SDLC_TEST_HELPER_MAIN"

// run's own tests below drive cli.Execute and hook.Run with a session that
// never touches a real git repository, but a subprocess test running main()
// does; TestMain keeps it out of the developer's own git configuration and
// leftover SDLC_/CLAUDE_ variables the same way every other package that runs
// git does.
func TestMain(m *testing.M) {
	if os.Getenv(mainHelperEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(testenv.Run(m))
}
