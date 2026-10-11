// US-069 AC-1: `sdlc approve --reject` had no case. It appeared in a payload the
// hook refuses and in the text `sdlc escalate` prints, never as a command run;
// sandbox_loop_test.go runs the approving half of the answer.

package e2e

import (
	"strings"
	"testing"
)

// A person who answers an escalation with a rejection sends the story back to
// work, with the reason on its record, and the session carries on from there.
// The person is this process, in the sandbox it made, with no session in the
// command's environment; the session under test has its attempt put to the hook
// first, which refuses it.
func TestAC1APersonRejectsEscalatedWorkWithAReasonAndTheSessionCarriesOnFromThere(t *testing.T) {
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	sb.works(t, tree, "", "", "escalate", "spec_unclear", "--message", "is a zero total refused too?")
	if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
		t.Fatalf("the story is %q after the escalation, want awaiting_human", got["US-001"])
	}

	sb.refuses(t, sandboxSession, tree, map[string]any{"command": `sdlc approve US-001 --reject "no"`},
		"approval-is-a-human-decision")

	// A rejection says why, because the work goes back to someone who has to
	// know what to change; nothing is decided without it.
	r := sb.person(t, tree, "approve", "US-001", "--reject", " ")
	if r.code == 0 || r.stdout != "" || !strings.Contains(r.stderr, "a rejection needs a reason") {
		t.Errorf("a rejection with no reason: exit %d\nstdout %q\nstderr %q\nwant it refused, saying a rejection needs a reason",
			r.code, r.stdout, r.stderr)
	}
	if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
		t.Fatalf("the story is %q after a rejection with no reason, want it still awaiting_human", got["US-001"])
	}

	r = sb.person(t, tree, "approve", "US-001", "--reject", "a zero total is refused too")
	want := "US-001  rejected (spec_unclear): a zero total is refused too\n\n" +
		"A person runs `sdlc start US-001`, or types /sdlc:next in Claude Code, to carry on.\n"
	if r.stdout != want || r.stderr != "" || r.code != 0 {
		t.Fatalf("approve --reject, by the harness: exit %d\nstdout %q\nstderr %q\nwant stdout %q", r.code, r.stdout, r.stderr, want)
	}
	if got := storiesOf(t, main); got["US-001"] != "in_progress" {
		t.Errorf("the story is %q after the rejection, want in_progress", got["US-001"])
	}
	sb.works(t, tree, "", "", "start")
}
