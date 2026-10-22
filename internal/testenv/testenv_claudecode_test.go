package testenv

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// US-108 AC-15, the person's decision 1: sdlc's person-only commands refuse in
// a process that carries Claude Code's markers, and a test run from a model's
// Bash, or from a terminal Claude Code started, carries them. CLAUDECODE and
// AI_AGENT start with neither SDLC_, CLAUDE_ nor GIT_, so without this the
// tests that run approve as a person passed in CI and failed inside a session.
// The test runs itself again with the markers set.
func TestAC15ClaudeCodesMarkersDoNotReachTheTests(t *testing.T) {
	const name = "TestAC15ClaudeCodesMarkersDoNotReachTheTests"
	if os.Getenv("SDLCTESTENV_MARKERS_CHILD") == "1" {
		for _, v := range []string{
			"CLAUDECODE", "AI_AGENT", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_PID",
			"CLAUDE_CODE_ENTRYPOINT", "SDLC_ENFORCE",
		} {
			if _, ok := os.LookupEnv(v); ok {
				t.Errorf("%s reached the tests", v)
			}
		}
		// A variable that is none of these is left where it is.
		if os.Getenv("SDLCTESTENV_KEPT") != "kept" {
			t.Error("a variable that is no marker of Claude Code's did not reach the tests")
		}
		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+name+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(),
		"SDLCTESTENV_MARKERS_CHILD=1",
		"SDLCTESTENV_KEPT=kept",
		"CLAUDECODE=1",
		"AI_AGENT=claude-code_2-1-302_agent",
		"CLAUDE_CODE_SESSION_ID=session-of-the-run",
		"CLAUDE_CODE_CHILD_SESSION=1",
		"CLAUDE_PID=4242",
		"CLAUDE_CODE_ENTRYPOINT=cli",
		"SDLC_ENFORCE=0",
	)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: "+name) {
		t.Errorf("with Claude Code's markers set (%v):\n%s", err, out)
	}
}
