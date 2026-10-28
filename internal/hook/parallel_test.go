package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// US-058 AC-4 through the hook, which is the primary barrier: a model-driven
// session cannot start `sdlc parallel` whether or not a story is being worked
// on, from the session working it or from another, as it cannot `sdlc approve`.
// The rule is shellpolicy's parallel-is-a-human-decision, reached by both
// entry points (Inspect while a story is worked, HumanDecisions otherwise).

var parallelCommands = []string{
	"sdlc parallel US-001 US-002",
	"sdlc parallel --unattended US-001",
	"sdlc --max 2 parallel US-001 US-002",
	"env sdlc parallel US-001",
	"bash -c 'sdlc parallel US-001'",
	"go test ./... && sdlc parallel US-001",
}

func TestAC4TheSessionWorkingAStoryIsRefusedParallel(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	for _, cmd := range parallelCommands {
		r := call(t, command(root, "", cmd), noEnv)
		if !denied(r) {
			t.Errorf("the session working the story ran %q", cmd)
			continue
		}
		if reason := r.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(reason, "parallel-is-a-human-decision") {
			t.Errorf("%q was refused by something other than the rule: %s", cmd, reason)
		}
	}
}

func TestAC4AnotherSessionOfTheProjectIsRefusedParallelWhileAStoryIsUnderWay(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	for _, cmd := range parallelCommands {
		if !denied(call(t, inSession(command(root, "", cmd), "session-b"), noEnv)) {
			t.Errorf("another session ran %q while a story was under way", cmd)
		}
	}
}

// With nothing being worked, nothing waits for a decision, and parallel is
// refused all the same, as the log's acknowledgement is: starting sessions is
// not a decision on a story.
func TestAC4ASessionIsRefusedParallelWithNoStoryBeingWorkedOn(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range parallelCommands {
		r := call(t, inSession(command(root, "", cmd), "session-b"), noEnv)
		if !denied(r) {
			t.Errorf("a session ran %q with no story being worked on", cmd)
			continue
		}
		if reason := r.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(reason, "parallel-is-a-human-decision") {
			t.Errorf("%q was refused by something other than the rule: %s", cmd, reason)
		}
	}
	// Nor with every escalation answered, nor from a session outside the repository.
	escalated(t, root, true)
	for _, cmd := range parallelCommands {
		if !denied(call(t, inSession(command(root, "", cmd), "session-b"), noEnv)) {
			t.Errorf("a session ran %q with every escalation answered", cmd)
		}
	}
}

// What only names the command goes through, and the stories' own commands are
// untouched by the rule.
func TestAC4OrdinaryCommandsAboutParallelAreNotRefused(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{
		"go test -parallel 4 ./...",
		"sdlc status --json",
		"echo parallel",
		`git commit -m "sdlc parallel US-001 works the stories in worktrees"`,
	} {
		if denied(call(t, inSession(command(root, "", cmd), "session-b"), noEnv)) {
			t.Errorf("%q was refused", cmd)
		}
	}
}
