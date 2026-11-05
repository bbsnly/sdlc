package model

// US-109, unit rows for what the roster answers once the team can switch an
// agent off or change its power to block. cli and hook drive these, but
// coverage counts a package's own tests only.

import (
	"testing"
	"time"
)

// AC-5: the gates whose work is one agent's, which become a person's to do by
// hand where that agent is disabled. The verifier and the code reviewer each
// also own their own review, which waits on that one reviewer.
func TestAC5WorkerAtNamesTheAgentWhoseWorkEachGateIs(t *testing.T) {
	for _, tc := range []struct {
		gate  Gate
		agent string // "" is a gate that is no worker's
	}{
		{GateAnalysis, "researcher"},
		{GateTestsFrozen, "sdet"},
		{GatePlan, "implementer"},
		{GateImplementation, "implementer"},
		{GateVerification, "verifier"},
		{GateVerifierReview, "verifier"},
		{GateCodeReview, "code-reviewer"},
		{GateRetro, "bookkeeper"},
		{GateDoR, ""},
		{GateDesignReview, ""},
		{GateCommit, ""},
		{Gate("no_such_gate"), ""},
	} {
		agent, ok := WorkerAt(tc.gate)
		if agent != tc.agent || ok != (tc.agent != "") {
			t.Errorf("AC-5 WorkerAt(%s) = %q, %v, want %q", tc.gate, agent, ok, tc.agent)
		}
	}
}

// AC-1: the roster's own power, as can_block reads it by default: blocking for
// the architect, security, perf, the verifier and the code reviewer; advisory
// for the red team and the human advocate.
func TestAC1EachReviewerBlocksByDefaultAsTheReleasedToolDoes(t *testing.T) {
	want := map[string]bool{
		"architect": true, "security": true, "perf": true, "verifier": true, "code-reviewer": true,
		"red-team": false, "human-advocate": false,
	}
	for _, r := range Reviewers {
		blocks, known := want[r.Role]
		if !known {
			t.Errorf("AC-1 %s reviews at %s and has no default here", r.Role, r.Gate)
			continue
		}
		if got := r.BlocksByDefault(); got != blocks {
			t.Errorf("AC-1 %s at %s blocks by default = %v, want %v", r.Role, r.Gate, got, blocks)
		}
	}
}

// AC-6: a reviewer's row wherever it first reviews, for the settings that are
// not a gate's, such as can_block.
func TestAC6FindReviewerAnywhereFindsEveryReviewerAndNoWorker(t *testing.T) {
	for _, tc := range []struct {
		agent string
		gate  Gate // "" is no reviewer
	}{
		{"architect", GateDesignReview},
		{"red-team", GateDesignReview},
		{"security", GateDesignReview},
		{"perf", GateDesignReview},
		{"human-advocate", GateDoR},
		{"verifier", GateVerifierReview},
		{"code-reviewer", GateCodeReview},
		{"researcher", ""},
		{"project-manager", ""},
		{"nobody", ""},
		{"", ""},
	} {
		r, ok := FindReviewerAnywhere(tc.agent)
		if ok != (tc.gate != "") || ok && (r.Gate != tc.gate || r.Role != tc.agent) {
			t.Errorf("AC-6 FindReviewerAnywhere(%q) = %s at %s, %v, want the row at %q", tc.agent, r.Role, r.Gate, ok, tc.gate)
		}
	}
}

// AC-6, AC-7: whether a block can stop a gate, under the project's can_block;
// max_rounds counts only blocks that can. can_block false makes any reviewer
// advisory, an instance included; true makes an advisory one block as perf
// does, and gives security no more than the roster does on a story that is not
// sensitive.
func TestAC6AC7CanBlockDecidesWhichBlocksStopAGateAndCount(t *testing.T) {
	off := func(role string) ReviewPolicy { return ReviewPolicy{CanBlock: map[string]bool{role: false}} }
	on := func(role string) ReviewPolicy { return ReviewPolicy{CanBlock: map[string]bool{role: true}} }
	for _, tc := range []struct {
		name                    string
		gate                    Gate
		role                    string
		policy                  ReviewPolicy
		blocks, breach, canStop bool
	}{
		{"AC_6 the architect", GateDesignReview, "architect", ReviewPolicy{}, true, false, true},
		{"AC_6 the architect advisory", GateDesignReview, "architect", off("architect"), false, false, false},
		{"AC_6 the architect set to block", GateDesignReview, "architect", on("architect"), true, false, true},
		{"AC_6 the red team", GateDesignReview, "red-team", ReviewPolicy{}, false, false, false},
		{"AC_6 the red team set to block", GateDesignReview, "red-team", on("red-team"), false, true, true},
		{"AC_6 the human advocate set to block", GateCodeReview, "human-advocate", on("human-advocate"), false, true, true},
		{"AC_6 perf", GateDesignReview, "perf", ReviewPolicy{}, false, true, true},
		{"AC_6 perf advisory", GateDesignReview, "perf", off("perf"), false, false, false},
		{"AC_6 security on a story not sensitive", GateCodeReview, "security", ReviewPolicy{}, false, false, true},
		{"AC_6 security on a sensitive story", GateCodeReview, "security", ReviewPolicy{SecuritySensitive: true}, true, false, true},
		{"AC_6 security set to block, not sensitive", GateCodeReview, "security", on("security"), false, false, true},
		{"AC_6 security advisory on a sensitive story", GateCodeReview, "security",
			ReviewPolicy{SecuritySensitive: true, CanBlock: map[string]bool{"security": false}}, false, false, false},
		{"AC_6 the code reviewer advisory", GateCodeReview, "code-reviewer", off("code-reviewer"), false, false, false},
		{"AC_8 an instance takes its role's can_block", GateCodeReview, "code-reviewer-2", off("code-reviewer"), false, false, false},
		{"AC_8 an instance's own name is no key", GateCodeReview, "code-reviewer-2", off("code-reviewer-2"), true, false, true},
		{"AC_6 another agent's setting", GateCodeReview, "code-reviewer", off("architect"), true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, ok := FindReviewer(tc.gate, tc.role)
			if !ok {
				t.Fatalf("no reviewer %s at %s", tc.role, tc.gate)
			}
			if got := r.Blocks(tc.policy); got != tc.blocks {
				t.Errorf("Blocks = %v, want %v", got, tc.blocks)
			}
			if got := r.OnBreach(tc.policy); got != tc.breach {
				t.Errorf("OnBreach = %v, want %v", got, tc.breach)
			}
			if got := r.CanStop(tc.policy); got != tc.canStop {
				t.Errorf("CanStop = %v, want %v", got, tc.canStop)
			}
			if got, want := r.Stops(tc.policy, VerdictBlock), tc.blocks || tc.breach; got != want {
				t.Errorf("Stops on a block = %v, want %v", got, want)
			}
			if r.Stops(tc.policy, VerdictApprove) {
				t.Error("an approval stops the gate")
			}
		})
	}
}

// AC-6: every gate's output names the agents whose block a person would
// otherwise count on, where the project made them advisory.
func TestAC6SecurityTheVerifierAndTheCodeReviewerAreSaidWhenAdvisory(t *testing.T) {
	for _, agent := range Agents {
		want := agent == "security" || agent == "verifier" || agent == "code-reviewer"
		if got := SaidWhenAdvisory(agent); got != want {
			t.Errorf("AC-6 SaidWhenAdvisory(%s) = %v, want %v", agent, got, want)
		}
	}
	if SaidWhenAdvisory("security-2") || SaidWhenAdvisory("") {
		t.Error("AC-6 SaidWhenAdvisory answers for a name that is not an agent")
	}
}

// AC-9: an answer is written into the history as the released tool wrote it,
// and the counts a person's decision starts again read it back.
func TestAC9AnApprovalIsWrittenAsTheReleasedToolWroteItAndStartsTheCountsAgain(t *testing.T) {
	for _, tc := range []struct{ decision, kind, want string }{
		{DecisionApproved, "pre_commit_approval", "approved (pre_commit_approval)"},
		{DecisionRejected, "review_not_converging", "rejected (review_not_converging)"},
		{DecisionApproved, KindAgentFailed, "approved (agent_failed)"},
		{DecisionApproved, KindWorkByHand, "approved (work_by_hand)"},
	} {
		if got := ApprovalEvent(tc.decision, tc.kind); got != tc.want {
			t.Errorf("AC-9 ApprovalEvent(%s, %s) = %q, want %q", tc.decision, tc.kind, got, tc.want)
		}
	}
	if KindAgentFailed != "agent_failed" || KindWorkByHand != "work_by_hand" {
		t.Errorf("AC-5 AC-9 the hand-over kinds are %q and %q, want agent_failed and work_by_hand", KindAgentFailed, KindWorkByHand)
	}
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	r := NewRecord("US-001", at)
	r.Append(EventAgentFailure, AgentFailureEvent(GateAnalysis, "researcher"), at)
	r.Append(EventApproval, ApprovalEvent(DecisionApproved, KindAgentFailed), at)
	if got := r.SinceDecision(EventAgentFailure, AgentFailureEvent(GateAnalysis, "researcher")); got != 0 {
		t.Errorf("AC-9 the failures before a person's decision still count: %d", got)
	}
}

// AC-9: a failure is counted per agent and per gate, so one agent's failures at
// one gate are not spent by another's, or by its own at another gate.
func TestAC9AFailureIsCountedPerAgentPerGate(t *testing.T) {
	if got := AgentFailureEvent(GateAnalysis, "researcher"); got != "analysis researcher" {
		t.Errorf("AC-9 AgentFailureEvent = %q, want \"analysis researcher\"", got)
	}
	if EventAgentFailure != "agent_failure" {
		t.Errorf("AC-9 the event kind is %q, want agent_failure", EventAgentFailure)
	}
	at := time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)
	r := NewRecord("US-001", at)
	for _, f := range []struct {
		gate  Gate
		agent string
	}{
		{GateDesignReview, "security"}, {GateDesignReview, "security"}, {GateDesignReview, "security-2"},
		{GateCodeReview, "security"}, {GateAnalysis, "researcher"},
	} {
		r.Append(EventAgentFailure, AgentFailureEvent(f.gate, f.agent), at)
	}
	for _, tc := range []struct {
		gate  Gate
		agent string
		want  int
	}{
		{GateDesignReview, "security", 2},
		{GateDesignReview, "security-2", 1},
		{GateCodeReview, "security", 1},
		{GateAnalysis, "researcher", 1},
		{GateAnalysis, "security", 0},
	} {
		if got := r.SinceDecision(EventAgentFailure, AgentFailureEvent(tc.gate, tc.agent)); got != tc.want {
			t.Errorf("AC-9 %s's failures at %s = %d, want %d", tc.agent, tc.gate, got, tc.want)
		}
	}
}

// AC-8: an instance's name reads as its role, and nothing else does.
func TestAC8BaseRoleReadsAnInstanceAsItsRoleAndLeavesEveryOtherNameAlone(t *testing.T) {
	for name, want := range map[string]string{
		"security-2":                    "security",
		"code-reviewer-16":              "code-reviewer",
		"security":                      "security",
		"researcher-2":                  "researcher-2",
		"security-1":                    "security-1",
		"security-99999999999999999999": "security-99999999999999999999",
		"-2":                            "-2",
		"":                              "",
	} {
		if got := BaseRole(name); got != want {
			t.Errorf("AC-8 BaseRole(%q) = %q, want %q", name, got, want)
		}
	}
}
