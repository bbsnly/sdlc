package main

import (
	"os"
	"testing"

	"github.com/bbsnly/sdlc/internal/testenv"
)

// linkcheckHelperEnv lets a test re-exec this binary as a plain linkcheck
// process: main's own flag parsing, mode dispatch and exit codes are otherwise
// never run, because calling main() directly would end the test binary at its
// first os.Exit.
const linkcheckHelperEnv = "LINKCHECK_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(linkcheckHelperEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(testenv.Run(m))
}
