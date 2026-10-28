package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// US-089 through the hook, which is the primary barrier: a model-driven
// session cannot run `sdlc remap` in any form, whether or not a story is being
// worked on, from the session working it or from another, as it cannot
// `sdlc approve`. The repair chooses between two stories' words for the map,
// or throws one story's away, and that is a person's. The rule is
// shellpolicy's remap-is-a-human-decision, reached by both entry points
// (Inspect while a story is worked, HumanDecisions otherwise).

var remapCommands = []string{
	"sdlc remap US-001",
	"sdlc remap US-001 --replace",
	"sdlc remap US-001 --drop",
	"sdlc remap --replace US-001",
	"sdlc remap --drop US-002",
	"sdlc remap US-404 --drop",
	"sdlc --json remap US-001 --drop",
	"env sdlc remap US-001 --replace",
	"bash -c 'sdlc remap US-001 --drop'",
	"go test ./... && sdlc remap US-001",
}

func remapRefused(t *testing.T, who, cmd string, r reply) {
	t.Helper()
	if !denied(r) {
		t.Errorf("%s ran %q", who, cmd)
		return
	}
	if reason := r.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(reason, "remap-is-a-human-decision") {
		t.Errorf("%q was refused by something other than the rule: %s", cmd, reason)
	}
}

func TestAC6TheSessionWorkingAStoryIsRefusedRemapInEveryForm(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	for _, cmd := range remapCommands {
		remapRefused(t, "the session working the story", cmd, call(t, command(root, "", cmd), noEnv))
		remapRefused(t, "the bookkeeper", cmd, call(t, command(root, "bookkeeper", cmd), noEnv))
	}
}

func TestAC6AnotherSessionOfTheProjectIsRefusedRemapWhileAStoryIsUnderWay(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	for _, cmd := range remapCommands {
		remapRefused(t, "another session, while a story was under way,", cmd, call(t, inSession(command(root, "", cmd), "session-b"), noEnv))
	}
}

// The state the repair is for: the story has landed, so nothing is being
// worked and nothing waits for a decision. Remap is refused all the same.
func TestAC6ASessionIsRefusedRemapWithNoStoryBeingWorkedOn(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range remapCommands {
		remapRefused(t, "a session, with no story being worked on,", cmd, call(t, inSession(command(root, "", cmd), "session-b"), noEnv))
	}
	// Nor with every escalation answered.
	escalated(t, root, true)
	for _, cmd := range remapCommands {
		remapRefused(t, "a session, with every escalation answered,", cmd, call(t, inSession(command(root, "", cmd), "session-b"), noEnv))
	}
}

// The bookkeeper's own command stores the sections and decides nothing: it
// goes through, as does what only names the repair.
func TestAC1StoringSectionsAndNamingTheRepairAreNotRefusedWhereTheRepairIs(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	remapRefused(t, "the bookkeeper", "sdlc remap US-001", call(t, command(root, "bookkeeper", "sdlc remap US-001"), noEnv))
	for _, cmd := range []string{
		"sdlc codemap write",
		"sdlc codemap write --json",
		"sdlc codemap write <<'SDLC_DOCUMENT'\n<!-- sdlc:add -->\n## refund.go\n\nKeeps refunds.\nSDLC_DOCUMENT",
		"echo remap",
	} {
		for _, agent := range []string{"", "bookkeeper"} {
			if r := call(t, command(root, agent, cmd), noEnv); denied(r) {
				t.Errorf("%q was refused to %q while a story is worked: %s", cmd, agent, r.HookSpecificOutput.PermissionDecisionReason)
			}
		}
	}
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"sdlc codemap write", "echo remap", "sdlc status --json"} {
		if denied(call(t, inSession(command(root, "", cmd), "session-b"), noEnv)) {
			t.Errorf("%q was refused with no story being worked on", cmd)
		}
	}
}
