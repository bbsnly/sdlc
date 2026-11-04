package e2e

import (
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// US-117 AC-12. Through the real binary and the plugin's launcher, in a story's
// worktree: the code reviewer lists a nit on the file the story's implementation
// wrote, and the commit gate refuses while it is open. Referring it is refused,
// since the story changes that file, and so is declining it without a reason;
// neither records anything. Closed as fixed, by the implementer, the closing
// keeps the session, and the commit gate passes.
func TestAC12AnOpenCodeReviewFindingHoldsTheCommitGateUntilItIsClosedFixed(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.works(t, sb.repo, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GateCodeReview); gate = sb.nextGate(t) {
		if gate == "" {
			t.Fatal("the story passed its last gate without stopping at the code review")
		}
		sb.worksGate(t, model.Gate(gate))
	}
	tree := sb.tree()
	sb.works(t, tree, "code-reviewer", "# code-reviewer\n\nOne nit.\n",
		"review", "add", "code_review", "code-reviewer", "approve", "--nits", "1",
		"--finding", "nit invoice.go: the name says nothing")
	for _, r := range model.ReviewersFor(model.GateCodeReview) {
		if r.Role != "code-reviewer" {
			sb.works(t, tree, r.Role, "# "+r.Role+"\n", "review", "add", "code_review", r.Role, "approve")
		}
	}
	sb.works(t, tree, "", "", "gate", "code_review", "pass", "--note", "one nit open")
	sb.commits(t)

	r := sb.does(t, tree, "", "", "gate", "commit", "pass", "--note", "the story")
	flat := strings.Join(strings.Fields(r.stderr), " ")
	if r.code == 0 {
		t.Fatalf("AC-12 the commit gate passed with a finding open:\n%s", r.stdout)
	}
	if !strings.Contains(flat, "SDLC-E0092") || !strings.Contains(flat, "code_review code-reviewer F-1") ||
		!strings.Contains(flat, "sdlc finding close") {
		t.Errorf("AC-12 the commit gate's refusal does not name the open finding and how to close it:\n%s", r.stderr)
	}

	before, _ := rpStored(t, sb)
	for _, c := range []struct {
		code string
		args []string
	}{
		{"SDLC-E0091", []string{"--referred", "--reason", "it can wait for another story"}},
		{"SDLC-E0090", []string{"--declined"}},
	} {
		r := sb.does(t, tree, "implementer", "", append([]string{"finding", "close", "code_review", "code-reviewer", "F-1"}, c.args...)...)
		if r.code == 0 || !strings.Contains(r.stderr, c.code) {
			t.Errorf("AC-12 finding close %v: exit %d, want a refusal with %s\n%s%s", c.args, r.code, c.code, r.stdout, r.stderr)
		}
		if after, _ := rpStored(t, sb); after != before {
			t.Errorf("AC-12 finding close %v was refused and changed the record", c.args)
		}
	}

	sb.works(t, tree, "implementer", "", "finding", "close", "code_review", "code-reviewer", "F-1", "--fixed")
	if after, _ := rpStored(t, sb); !strings.Contains(after, sandboxSession) {
		t.Errorf("AC-12 the closing does not keep the session that closed it:\n%s", after)
	}
	sb.works(t, tree, "", "", "gate", "commit", "pass", "--note", "the story")
}

// AC-4 and the design review's I-3 (security) and 1 (red team), in a story's
// worktree, where the record is in the main checkout and the story's diff is
// the worktree's commits: the code reviewer's nit on a file the story has not
// touched is referred, Gate 7 rework then writes that file and the story commits
// it, and the commit gate refuses the referral standing. Closed as fixed, the
// gate passes.
func TestAC4AReferralOfAFileGate7ReworkThenWritesStopsTheCommitGate(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.works(t, sb.repo, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GateCodeReview); gate = sb.nextGate(t) {
		if gate == "" {
			t.Fatal("the story passed its last gate without stopping at the code review")
		}
		sb.worksGate(t, model.Gate(gate))
	}
	tree := sb.tree()
	sb.works(t, tree, "code-reviewer", "# code-reviewer\n\nOne nit.\n",
		"review", "add", "code_review", "code-reviewer", "approve", "--nits", "1",
		"--finding", "nit notes.md: the page says nothing")
	sb.works(t, tree, "implementer", "", "finding", "close", "code_review", "code-reviewer", "F-1",
		"--referred", "--reason", "outside the story, and it cannot wait")

	// Gate 7 rework writes the file, and the verifier and the code reviewers look
	// at it again.
	sb.writesFile(t, "implementer", "notes.md", "# Notes\n\nWhat the invoice does.\n")
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			sb.works(t, tree, r.Role, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
		}
	}
	sb.works(t, tree, "", "", "gate", "code_review", "pass", "--note", "the rework is reviewed")
	sb.commits(t)

	r := sb.does(t, tree, "", "", "gate", "commit", "pass", "--note", "the story")
	if r.code == 0 {
		t.Fatalf("AC-4 the commit gate passed with a referral of a file the story changes:\n%s", r.stdout)
	}
	if flat := strings.Join(strings.Fields(r.stderr), " "); !strings.Contains(flat, "SDLC-E0091") ||
		!strings.Contains(flat, "code_review code-reviewer F-1") {
		t.Errorf("AC-4 the commit gate's refusal does not name the referral of a file the story changes:\n%s", r.stderr)
	}
	sb.works(t, tree, "implementer", "", "finding", "close", "code_review", "code-reviewer", "F-1", "--fixed")
	sb.works(t, tree, "", "", "gate", "commit", "pass", "--note", "the story")
}

// AC-2 and T1 of the threats, through the real hook: a --finding whose value sits
// between the gate and the role does not let another agent record the code
// reviewer's verdict.
func TestAC2TheRealHookKeepsAReviewWithFindingsItsReviewersOwn(t *testing.T) {
	parallel(t)
	sb, _ := rpAtDesignReview(t)
	const rule = "review-is-recorded-by-its-reviewer"
	for _, c := range []struct{ agent, command string }{
		{"implementer", "sdlc review add code_review --finding x code-reviewer approve"},
		{"implementer", `sdlc review add code_review --finding "nit a.go: the name" code-reviewer approve`},
		{"implementer", "sdlc review add code_review --finding design_review code-reviewer approve"},
		{"security", "sdlc review add code_review --finding security code-reviewer approve"},
		{"implementer", "sdlc --finding x review add code_review code-reviewer approve"},
	} {
		sb.refusesAs(t, c.agent, c.command, rule)
	}
}
