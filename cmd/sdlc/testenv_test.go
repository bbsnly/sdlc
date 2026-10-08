package main

import (
	"os"
	"strings"
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
	if name, ok := strings.CutPrefix(os.Getenv(mainHelperEnv), panicHelperPrefix); ok {
		os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, panicsReading(name)))
	}
	os.Exit(testenv.Run(m))
}

// panicHelperPrefix, before the name of a variable, re-execs this binary as an
// sdlc process whose environment panics when that variable is read, and with
// "*" when any is. It is run, with the released recover and crash, given a
// real process, real streams and a real exit code, and a panic nothing in the
// released binary has a way to cause: a seam there would be an off switch for
// every rule, since a hook that panics lets the call through (US-067 AC-4).
const panicHelperPrefix = "panic:"

func panicsReading(name string) func(string) string {
	return func(key string) string {
		if name == "*" || key == name {
			panic("forced by the test: the environment could not be read (" + key + ")")
		}
		return os.Getenv(key)
	}
}
