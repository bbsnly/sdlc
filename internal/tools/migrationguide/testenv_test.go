package main

import (
	"os"
	"testing"

	"github.com/bbsnly/sdlc/internal/testenv"
)

// migrationguideHelperEnv lets a test re-exec this binary as a plain
// migrationguide process: main's own os.Args, streams and exit code are
// otherwise never run, because calling main() directly would end the test
// binary at its first os.Exit.
const migrationguideHelperEnv = "MIGRATIONGUIDE_TEST_HELPER"

// The tests build repositories and run git in them, so they run in the
// environment testenv gives: no global git configuration, no GIT_ variable
// from a hook, and no repository found above the temp directory.
func TestMain(m *testing.M) {
	if os.Getenv(migrationguideHelperEnv) == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(testenv.Run(m))
}
