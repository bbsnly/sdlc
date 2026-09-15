package testenv

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(Run(m))
}

// A git hook runs with git's own variables exported, and tests run from one
// inherited them: GIT_INDEX_FILE sent every test repository to the index of
// the commit being made, and GIT_CONFIG_PARAMETERS carried a -c past the empty
// global configuration. The test runs itself again with them set.
func TestGitsOwnVariablesDoNotReachTheTests(t *testing.T) {
	const name = "TestGitsOwnVariablesDoNotReachTheTests"
	if os.Getenv("SDLCTESTENV_CHILD") == "1" {
		for _, v := range []string{
			"GIT_INDEX_FILE", "GIT_DIR", "GIT_WORK_TREE", "GIT_OBJECT_DIRECTORY",
			"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0",
		} {
			if value, ok := os.LookupEnv(v); ok {
				t.Errorf("%s=%s reached the tests", v, value)
			}
		}
		for _, v := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM", "GIT_CEILING_DIRECTORIES"} {
			if os.Getenv(v) == "" {
				t.Errorf("%s is not set for the tests", v)
			}
		}
		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+name+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(),
		"SDLCTESTENV_CHILD=1",
		"GIT_INDEX_FILE=/nowhere/index",
		"GIT_DIR=/nowhere",
		"GIT_WORK_TREE=/nowhere",
		"GIT_OBJECT_DIRECTORY=/nowhere/objects",
		"GIT_CONFIG_PARAMETERS='commit.gpgsign'='true'",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=commit.gpgsign",
		"GIT_CONFIG_VALUE_0=true",
	)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: "+name) {
		t.Errorf("with git's variables set (%v):\n%s", err, out)
	}
}
