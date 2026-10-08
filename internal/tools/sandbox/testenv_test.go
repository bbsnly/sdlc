package main

import (
	"os"
	"testing"

	"github.com/bbsnly/sdlc/internal/testenv"
)

// sandboxHelperEnv lets a test re-exec this binary as a plain sandbox process:
// main's own os.Args, streams and exit code are otherwise never run, because
// calling main() directly would end the test binary at its first os.Exit.
const sandboxHelperEnv = "SANDBOX_TEST_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(sandboxHelperEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(testenv.Run(m))
}
