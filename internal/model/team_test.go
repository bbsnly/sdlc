package model

// US-109: what the team adds to the one rule `cli` and `hook` share for who an
// agent is. A copy of a reviewer is named `security-2`, and three readers have to
// agree about that name: `sdlc review add`, the shell rule that keeps a review the
// reviewer's own, and the staffing's floor. FindReviewer is the one function the
// first two already ask, so an instance's name is held here, through it: a name
// `cli` learnt and the rule did not would let the implementer record
// `code-reviewer-2`'s approval with nothing fired (THREATS.md, T1).
//
// What these bind that the criteria leave to the design: FindReviewer answers for an
// instance's name, and the Role it answers with is the instance's name in its one
// spelling. IsReviewRole does not widen (T2).

import (
	"strings"
	"testing"
	"time"
)

// AC-8 and T1: the name is the role, then a hyphen, then a whole number from 2 up in
// ASCII digits with no leading zero. Case and surrounding space read as they do for a
// role, because a person types them.
func TestAC8FindReviewerReadsAnInstancesNameAndOnlyThatForm(t *testing.T) {
	for _, tc := range []struct {
		gate Gate
		name string
		role string // "" is no reviewer
	}{
		{GateCodeReview, "code-reviewer-2", "code-reviewer-2"},
		{GateCodeReview, "code-reviewer-3", "code-reviewer-3"},
		{GateCodeReview, "code-reviewer-10", "code-reviewer-10"},
		{GateCodeReview, "CODE-REVIEWER-2", "code-reviewer-2"},
		{GateCodeReview, " code-reviewer-2", "code-reviewer-2"},
		{GateCodeReview, "code-reviewer-2 ", "code-reviewer-2"},
		{GateDesignReview, "security-2", "security-2"},
		{GateCodeReview, "security-2", "security-2"},
		{GateVerifierReview, "verifier-2", "verifier-2"},
		{GateDesignReview, "human-advocate-2", "human-advocate-2"},
		{GateDesignReview, "architect-2", "architect-2"},

		// The role itself is the first copy, and is spelled only one way.
		{GateCodeReview, "code-reviewer", "code-reviewer"},
		{GateCodeReview, "code-reviewer-1", ""},
		{GateCodeReview, "code-reviewer-0", ""},
		{GateCodeReview, "code-reviewer-02", ""},
		{GateCodeReview, "code-reviewer--2", ""},
		{GateCodeReview, "code-reviewer-2x", ""},
		{GateCodeReview, "code-reviewer-x2", ""},
		{GateCodeReview, "code-reviewer-", ""},
		{GateCodeReview, "code-reviewer-+2", ""},
		{GateCodeReview, "code-reviewer-٢", ""},
		{GateCodeReview, "code-reviewer-2-2", ""},

		// An instance belongs to the gate its role reviews.
		{GateCodeReview, "architect-2", ""},
		{GateVerifierReview, "security-2", ""},
		{GateCommit, "security-2", ""},
		{GateCodeReview, "researcher-2", ""},
	} {
		r, ok := FindReviewer(tc.gate, tc.name)
		if ok != (tc.role != "") || r.Role != tc.role {
			t.Errorf("AC-8 FindReviewer(%s, %q) = %q, %v, want %q", tc.gate, tc.name, r.Role, ok, tc.role)
		}
	}
}

// AC-8: a copy reviews as its role does. What blocks and what is expected come from
// the roster's row for the role.
func TestAC8AnInstanceBlocksAndIsExpectedAsItsRoleIs(t *testing.T) {
	for _, tc := range []struct {
		gate       Gate
		name, role string
	}{
		{GateCodeReview, "code-reviewer-2", "code-reviewer"},
		{GateCodeReview, "security-3", "security"},
		{GateDesignReview, "perf-2", "perf"},
		{GateDesignReview, "red-team-2", "red-team"},
		{GateDoR, "human-advocate-2", "human-advocate"},
	} {
		copied, ok := FindReviewer(tc.gate, tc.name)
		if !ok {
			t.Errorf("AC-8 FindReviewer(%s, %q) found nobody", tc.gate, tc.name)
			continue
		}
		base, _ := FindReviewer(tc.gate, tc.role)
		for _, p := range []ReviewPolicy{{}, {SecuritySensitive: true}, {DoRAdvocate: true}} {
			if copied.Blocks(p) != base.Blocks(p) || copied.Expected(p) != base.Expected(p) ||
				copied.Stops(p, VerdictBlock) != base.Stops(p, VerdictBlock) {
				t.Errorf("AC-8 %s does not review as %s under %+v", tc.name, tc.role, p)
			}
		}
	}
}

// T2: IsReviewRole takes the host's agent type in IsLoopRole and reviewer-reviews.
// Widened, an agent called security-2 would be a loop role with a reviewer's scope.
func TestAC8AnInstancesNameIsNotARoleAnAgentCanBe(t *testing.T) {
	for _, name := range []string{"security-2", "code-reviewer-2", "verifier-3", "architect-2"} {
		if IsReviewRole(name) {
			t.Errorf("AC-8 IsReviewRole(%q) is true: an agent of that name would be a loop role", name)
		}
	}
	for _, r := range Reviewers {
		if !IsReviewRole(r.Role) {
			t.Errorf("IsReviewRole(%q) is false", r.Role)
		}
	}
}

// T2 and T10: a copy takes the floor of its role. Today every name that is not a role
// is "never left out", so security-2 would be held in by accident on a story that is
// not sensitive, and perf-2 could never be left out at all.
func TestAC8AnInstanceTakesTheFloorOfItsRole(t *testing.T) {
	for _, tc := range []struct {
		agent     string
		sensitive bool
		never     bool
	}{
		{"security-2", true, true},
		{"security-2", false, false},
		{"perf-2", false, false},
		{"perf-2", true, false},
		{"red-team-2", true, false},
		{"human-advocate-3", false, false},
		{"architect-2", false, false},
		{"verifier-2", false, true},
		{"code-reviewer-2", false, true},
	} {
		if got := NeverLeftOut(tc.agent, tc.sensitive); got != tc.never {
			t.Errorf("AC-8 NeverLeftOut(%s, sensitive %v) = %v, want %v", tc.agent, tc.sensitive, got, tc.never)
		}
	}
	r := NewRecord("A-1", time.Date(2026, 11, 2, 8, 30, 0, 0, time.UTC))
	r.Staffing = &Staffing{At: "2026-11-02T08:30:00Z", Agents: []Staffed{
		{Agent: "security-2", Needed: false}, {Agent: "verifier-2", Needed: false}, {Agent: "perf-2", Needed: true},
	}}
	r.SetSecuritySensitive(false)
	for agent, want := range map[string]bool{"security-2": true, "verifier-2": false, "perf-2": false} {
		if got := r.LeftOut(agent); got != want {
			t.Errorf("AC-8 LeftOut(%s) on a story that is not sensitive = %v, want %v", agent, got, want)
		}
	}
	r.SetSecuritySensitive(true)
	if r.LeftOut("security-2") {
		t.Error("AC-10 security-2 is left out of a story recorded as sensitive")
	}
}

// answersCommit asks Approval.AnswersCommit through an interface, so that this package's
// tests compile before the method exists and a test that needs it fails by saying so.
func answersCommit(t *testing.T, a Approval) bool {
	t.Helper()
	asked, ok := any(&a).(interface{ AnswersCommit() bool })
	if !ok {
		t.Fatal("T5 Approval has no AnswersCommit() bool")
	}
	return asked.AnswersCommit()
}

// T5: false only for an approved decision of the two kinds the story adds. A rejection
// sends the work back, the safe direction, and every released kind answers as it did. The
// look-alike is the residual the plan declines (red team H1): the closure is for those two
// exact words, and the docs say so.
func TestAC9OnlyAnApprovedFailureOrByHandHandOverLeavesTheCommitUnanswered(t *testing.T) {
	for _, tc := range []struct {
		decision, kind string
		answers        bool
	}{
		{DecisionApproved, "agent_failed", false},
		{DecisionApproved, "work_by_hand", false},
		{DecisionRejected, "agent_failed", true},
		{DecisionRejected, "work_by_hand", true},
		{DecisionApproved, "pre_commit_approval", true},
		{DecisionRejected, "pre_commit_approval", true},
		{DecisionApproved, "review_not_converging", true},
		{DecisionApproved, "spec_unclear", true},
		{DecisionApproved, "", true},
		{DecisionApproved, "agent-failed", true},
	} {
		if got := answersCommit(t, Approval{Decision: tc.decision, Type: tc.kind}); got != tc.answers {
			t.Errorf("AC-9 AnswersCommit for %s %q = %v, want %v", tc.decision, tc.kind, got, tc.answers)
		}
	}
}

// T5: DecisionFor is what WaitsForApproval and its callers share. It looks past an approved
// failure on the tree to the answer before it, so that answer still stands and a rejection
// still sends the work back, and it holds no answer of its own (red team V3).
func TestAC9TheDecisionOnATreeIsReadPastAnApprovedFailure(t *testing.T) {
	const tree, otherTree = "tree-1", "tree-2"
	approved := func(kind, on string) Approval { return Approval{Decision: DecisionApproved, Type: kind, TreeHash: on} }
	rejected := func(kind, on string) Approval {
		return Approval{Decision: DecisionRejected, Type: kind, TreeHash: on, Reason: "not this"}
	}
	for _, tc := range []struct {
		name      string
		approvals []Approval
		found     bool
		decision  string
		waits     string // what WaitsForApproval says, "" being nothing
	}{
		{"AC_9 a failure alone", []Approval{approved("agent_failed", tree)}, false, "", "its risk tier is"},
		{"AC_5 a by-hand hand-over alone", []Approval{approved("work_by_hand", tree)}, false, "", "its risk tier is"},
		{"AC_9 a real answer, then a failure", []Approval{approved("pre_commit_approval", tree), approved("agent_failed", tree)}, true, DecisionApproved, ""},
		{"AC_9 a failure, then a real answer", []Approval{approved("agent_failed", tree), approved("pre_commit_approval", tree)}, true, DecisionApproved, ""},
		{"AC_9 sent back, then a failure", []Approval{rejected("pre_commit_approval", tree), approved("agent_failed", tree)}, true, DecisionRejected, "sent this work back"},
		{"AC_9 a failure rejected", []Approval{approved("agent_failed", tree), rejected("agent_failed", tree)}, true, DecisionRejected, "sent this work back"},
		{"AC_9 a released kind", []Approval{approved("review_not_converging", tree)}, true, DecisionApproved, ""},
		{"AC_9 an answer on other work and a failure on this", []Approval{approved("pre_commit_approval", otherTree), approved("agent_failed", tree)}, false, "", "the work has changed since"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Record{Approvals: tc.approvals}
			d, ok := r.DecisionFor(tree)
			if ok != tc.found || d.Decision != tc.decision {
				t.Errorf("DecisionFor = %+v, %v, want %q, %v", d, ok, tc.decision, tc.found)
			}
			why := r.WaitsForApproval("high", tree)
			if tc.waits == "" && why != "" || tc.waits != "" && !strings.Contains(why, tc.waits) {
				t.Errorf("WaitsForApproval = %q, want it to say %q", why, tc.waits)
			}
		})
	}
	// The latest decision of any kind still starts the counts again: approving a failure is
	// a person's decision, whatever it does not answer.
	at := time.Date(2026, 11, 2, 8, 30, 0, 0, time.UTC)
	r := NewRecord("A-1", at)
	r.Append("agent_failure", "analysis researcher", at)
	r.Append(EventApproval, "approved (agent_failed)", at)
	if got := r.SinceDecision("agent_failure", "analysis researcher"); got != 0 {
		t.Errorf("AC-9 an approved failure did not start the count again: %d", got)
	}
}

// AC-7: the count of blocks is per name, because each copy records its own review:
// a shared count would have two copies' independent blocks spend one budget.
func TestAC7AnInstancesBlocksAreCountedUnderItsOwnName(t *testing.T) {
	at := time.Date(2026, 11, 2, 8, 30, 0, 0, time.UTC)
	r := NewRecord("A-1", at)
	r.Append(EventReview, ReviewEvent(GateDesignReview, "security", VerdictBlock), at)
	r.Append(EventReview, ReviewEvent(GateDesignReview, "security-2", VerdictBlock), at)
	r.Append(EventReview, ReviewEvent(GateDesignReview, "security-2", VerdictBlock), at)
	if got := r.SinceDecision(EventReview, ReviewEvent(GateDesignReview, "security", VerdictBlock)); got != 1 {
		t.Errorf("AC-7 security's blocks = %d, want 1", got)
	}
	if got := r.SinceDecision(EventReview, ReviewEvent(GateDesignReview, "security-2", VerdictBlock)); got != 2 {
		t.Errorf("AC-7 security-2's blocks = %d, want 2", got)
	}
}
