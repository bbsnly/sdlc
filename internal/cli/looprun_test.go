package cli

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// reach does the real work of every gate before the one named, so that a test
// about one gate starts from a story that genuinely got there.
//
// It is also the loop written out in one place: if a gate grows a requirement
// and this is not updated, every test that walks past it fails, which is the
// reminder that the requirement is real.
func reach(t *testing.T, root string, upTo model.Gate) {
	t.Helper()
	for _, gate := range model.Gates {
		if gate == upTo {
			return
		}
		satisfy(t, root, gate)
		mustRun(t, "gate", string(gate), "pass", "--note", "reached by the test helper")
	}
}

// satisfy does what one gate needs before it can be recorded.
func satisfy(t *testing.T, root string, gate model.Gate) {
	t.Helper()
	switch gate {
	case model.GateAnalysis:
		mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
		mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	case model.GateTestsFrozen:
		writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// AC-1\n")
		mustRunWith(t, "# Test plan\n", "artifact", "write", "test_plan")
		mustRun(t, "freeze")
	case model.GatePlan:
		mustRunWith(t, "# Plan\n", "artifact", "write", "plan")
	case model.GateVerification:
		mustRunWith(t, "# Verification\n", "artifact", "write", "verification")
	case model.GateRetro:
		mustRunWith(t, "# Retro\n", "artifact", "write", "retro")
	case model.GateCommit:
		commitEverything(t, root)
	}
	for _, r := range model.ReviewersFor(gate) {
		mustRunWith(t, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
	}
}

func commitEverything(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "story"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// A story going all the way through is the claim on the front page. Every gate
// here refuses something, and this is the shape of a run where nothing had to
// be refused.
func TestAStoryGoesAllTheWayThrough(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")

	reach(t, root, "")

	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	for _, gate := range model.Gates {
		if status.Gates[string(gate)] != "pass" {
			t.Errorf("%s = %q at the end of the loop", gate, status.Gates[string(gate)])
		}
	}
	if status.Freeze == nil || !status.Freeze.Intact {
		t.Errorf("freeze = %+v", status.Freeze)
	}
}

// The order is not a suggestion. A gate recorded out of order is how a story
// reaches the commit gate having skipped the one that would have stopped it.
func TestAGateCannotBeRecordedOutOfOrder(t *testing.T) {
	gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")

	r := run(t, "gate", "code_review", "pass", "--note", "looks fine")
	if r.code == 0 {
		t.Fatal("a late gate passed on a story that had done none of the earlier ones")
	}
	for _, want := range []string{"SDLC-E0031", "dor", "analysis"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}

	// A gate that failed is not a gate that passed.
	mustRun(t, "gate", "dor", "fail", "--note", "AC-2 is not testable")
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	if r := run(t, "gate", "analysis", "pass"); r.code == 0 {
		t.Error("a gate passed on the back of one that failed")
	}
}

// Recording a failure has to stay possible at every gate, including the ones
// with requirements. A loop that cannot say "this did not work" has nowhere to
// put the truth.
func TestAnyGateCanBeRecordedAsFailedWithNothingDone(t *testing.T) {
	gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")

	for _, gate := range model.Gates {
		mustRun(t, "gate", string(gate), "fail", "--note", "not this time")
	}
}
