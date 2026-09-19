package model

import (
	"strings"
	"testing"
	"time"
)

// Expected has exactly one condition in the whole roster: the DoR advocate,
// which only runs when the project turns it on. Every other reviewer is
// expected unconditionally.
func TestReviewerExpectedOnlyTheDoRAdvocateIsConditional(t *testing.T) {
	architect := Reviewer{Role: "architect", Gate: GateDesignReview, Blocking: true}
	advocate := Reviewer{Role: "human-advocate", Gate: GateDoR, WhenDoRAdvocate: true}

	if !architect.Expected(ReviewPolicy{}) {
		t.Error("a reviewer with no condition must always be expected")
	}
	if advocate.Expected(ReviewPolicy{DoRAdvocate: false}) {
		t.Error("the DoR advocate is expected without the project asking for it")
	}
	if !advocate.Expected(ReviewPolicy{DoRAdvocate: true}) {
		t.Error("the DoR advocate is not expected once the project asks for it")
	}
}

// Blocks combines what the roster says with what the project chose: the code
// reviewer is the only one a project can loosen, and security only blocks a
// story that is actually sensitive.
func TestReviewerBlocksCombinesTheRosterAndTheProjectsPolicy(t *testing.T) {
	for name, c := range map[string]struct {
		r    Reviewer
		p    ReviewPolicy
		want bool
	}{
		"a plain blocking reviewer always blocks": {
			Reviewer{Blocking: true}, ReviewPolicy{}, true,
		},
		"an advisable reviewer still blocks until the project loosens it": {
			Reviewer{Blocking: true, Advisable: true}, ReviewPolicy{}, true,
		},
		"the project's advisory setting reaches only an advisable reviewer": {
			Reviewer{Blocking: true, Advisable: true}, ReviewPolicy{CodeReviewAdvisory: true}, false,
		},
		"security blocks a story that is sensitive": {
			Reviewer{WhenSecuritySensitive: true}, ReviewPolicy{SecuritySensitive: true}, true,
		},
		"security does not block a story that is not sensitive": {
			Reviewer{WhenSecuritySensitive: true}, ReviewPolicy{SecuritySensitive: false}, false,
		},
		"a non-blocking, non-conditional reviewer never blocks": {
			Reviewer{}, ReviewPolicy{}, false,
		},
	} {
		if got := c.r.Blocks(c.p); got != c.want {
			t.Errorf("%s: Blocks = %v, want %v", name, got, c.want)
		}
	}
}

// Stops is true only for a block from a reviewer that can refuse the gate,
// whether that is because it blocks outright or because it breached a stated
// budget -- perf never has to approve, but a breach still stops the gate.
func TestReviewerStopsOnlyOnABlockThatCounts(t *testing.T) {
	blocking := Reviewer{Blocking: true}
	breach := Reviewer{BlocksOnBreach: true}
	advisory := Reviewer{}

	if !blocking.Stops(ReviewPolicy{}, VerdictBlock) {
		t.Error("a blocking reviewer's block did not stop the gate")
	}
	if blocking.Stops(ReviewPolicy{}, VerdictApprove) {
		t.Error("an approval stopped the gate")
	}
	if !breach.Stops(ReviewPolicy{}, VerdictBlock) {
		t.Error("a breach reviewer's block did not stop the gate, though it never has to approve")
	}
	if advisory.Stops(ReviewPolicy{}, VerdictBlock) {
		t.Error("a purely advisory reviewer's block stopped the gate")
	}
}

// The roster is the one list every lookup is taken from, so a role added
// there is governed everywhere without a second list to keep in step.
func TestReviewerRosterLookups(t *testing.T) {
	if !IsReviewRole("architect") {
		t.Error("architect is on the roster but IsReviewRole missed it")
	}
	if IsReviewRole("implementer") {
		t.Error("implementer is not a reviewer")
	}

	got := ReviewersFor(GateDesignReview)
	if len(got) == 0 {
		t.Fatal("no reviewers found for design_review")
	}
	for _, r := range got {
		if r.Gate != GateDesignReview {
			t.Errorf("%+v does not belong to design_review", r)
		}
	}
	if got := ReviewersFor(GateCommit); got != nil {
		t.Errorf("ReviewersFor(commit) = %+v, want none: nothing reviews the commit gate", got)
	}

	if r, ok := FindReviewer(GateDesignReview, "  ARCHITECT  "); !ok || r.Role != "architect" {
		t.Errorf("FindReviewer did not forgive case and spacing: %+v, %v", r, ok)
	}
	if _, ok := FindReviewer(GateDesignReview, "verifier"); ok {
		t.Error("FindReviewer matched a role that belongs to a different gate")
	}

	names := ReviewerNames(GateDesignReview)
	if !strings.Contains(names, "architect") {
		t.Errorf("ReviewerNames(design_review) = %q, missing architect", names)
	}
	if got := ReviewerNames(GateCommit); got != "no reviewers" {
		t.Errorf("ReviewerNames(commit) = %q, want %q", got, "no reviewers")
	}
}

// ReviewEvent is how a verdict is written into the story's history, and has to
// name the gate, the role and the verdict so that the history reads on its
// own.
func TestReviewEventNamesTheGateRoleAndVerdict(t *testing.T) {
	if got := ReviewEvent(GateDesignReview, "architect", VerdictBlock); got != "design_review architect block" {
		t.Errorf("ReviewEvent = %q", got)
	}
}

// A round is scoped to one role at one gate: two reviewers of the same gate
// must not see each other's history as their own, and a second look by the
// same role must count as round two.
func TestAddReviewNumbersEachRolesRoundsSeparately(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	r := NewRecord("A-1", at)

	if n := r.Round(GateDesignReview, "architect"); n != 0 {
		t.Errorf("Round on a fresh record = %d, want 0", n)
	}
	first := r.AddReview(Review{Gate: GateDesignReview, Role: "architect", Verdict: VerdictBlock}, at)
	if first.Round != 1 || first.At != Timestamp(at) {
		t.Errorf("first review = %+v", first)
	}
	second := r.AddReview(Review{Gate: GateDesignReview, Role: "architect", Verdict: VerdictApprove}, at)
	if second.Round != 2 {
		t.Errorf("second review round = %d, want 2", second.Round)
	}
	other := r.AddReview(Review{Gate: GateDesignReview, Role: "red-team", Verdict: VerdictNote}, at)
	if other.Round != 1 {
		t.Errorf("a different role's round = %d, want 1: it has its own count", other.Round)
	}

	latest, ok := r.LatestReview(GateDesignReview, "architect")
	if !ok || latest.Verdict != VerdictApprove || latest.Round != 2 {
		t.Errorf("LatestReview = %+v, %v, want the second, approving round", latest, ok)
	}
	if _, ok := r.LatestReview(GateCommit, "architect"); ok {
		t.Error("LatestReview found a review for a gate nobody reviewed")
	}
}
