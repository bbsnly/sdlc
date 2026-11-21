package model

// US-112.4: unit rows for the test reviewer's row of the roster. cli, config and
// the hook drive these, but coverage counts a package's own tests only.

import (
	"slices"
	"testing"
	"time"
)

const testReviewer = "test-reviewer"

// AC-1: one row, at tests_frozen, that blocks, for an agent a staffing covers.
// No gate is added: the review is stored under the gate it reviews.
func TestAC1AC9TheTestReviewerHasOneBlockingRowOfTheRosterAtTestsFrozen(t *testing.T) {
	var rows []Reviewer
	for _, r := range Reviewers {
		if r.Role == testReviewer {
			rows = append(rows, r)
		}
	}
	want := Reviewer{Role: testReviewer, Gate: GateTestsFrozen, Blocking: true}
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("AC-1 the roster's rows for the test reviewer are %+v, want only %+v", rows, want)
	}
	if !slices.Contains(Agents, testReviewer) || !IsReviewRole(testReviewer) {
		t.Error("AC-1 the test reviewer is not an agent a staffing covers, or not a reviewer")
	}
	if got := ReviewerNames(GateTestsFrozen); got != testReviewer {
		t.Errorf("AC-1 tests_frozen expects %q, want the test reviewer alone", got)
	}
	if len(Gates) != 11 || !GateTestsFrozen.Valid() {
		t.Errorf("AC-1 the gates are %v: no gate record key is added", Gates)
	}
	if r, ok := FindReviewerAnywhere(testReviewer); !ok || r.Gate != GateTestsFrozen || !r.BlocksByDefault() {
		t.Errorf("AC-1 FindReviewerAnywhere = %+v, %v", r, ok)
	}
	if agent, _ := WorkerAt(GateTestsFrozen); agent != "sdet" {
		t.Errorf("AC-1 the work of tests_frozen is %q's, want the sdet's still", agent)
	}
}

// AC-1 and THREATS.md: the names the review is recorded under, an instance's
// included, and no other gate's. The red team is not given Gate 3 (non-goal).
func TestAC1TheTestReviewerIsFoundOnlyAtTestsFrozenUnderItsNameAndItsInstances(t *testing.T) {
	for _, name := range []string{"test-reviewer", " Test-Reviewer ", "test-reviewer-2"} {
		r, ok := FindReviewer(GateTestsFrozen, name)
		if !ok || BaseRole(r.Role) != testReviewer || !r.Blocking {
			t.Errorf("AC-1 FindReviewer(tests_frozen, %q) = %+v, %v", name, r, ok)
		}
	}
	if role, n, ok := InstanceOf("test-reviewer-3"); !ok || role != testReviewer || n != 3 {
		t.Errorf("AC-1 InstanceOf(test-reviewer-3) = %q, %d, %v", role, n, ok)
	}
	for _, c := range []struct {
		gate Gate
		role string
	}{{GateDesignReview, testReviewer}, {GateCodeReview, testReviewer}, {GateTestsFrozen, "red-team"}, {GateTestsFrozen, "sdet"}} {
		if _, ok := FindReviewer(c.gate, c.role); ok {
			t.Errorf("AC-1 %s reviews at %s", c.role, c.gate)
		}
	}
}

// AC-3, the can_block false case: advisory, its block stops nothing and counts
// towards no hand-over, and it is still expected. Its instance takes its power.
func TestAC3AC9WithCanBlockFalseTheTestReviewerIsAdvisoryAndStillExpected(t *testing.T) {
	off := ReviewPolicy{CanBlock: map[string]bool{testReviewer: false}}
	for _, name := range []string{testReviewer, "test-reviewer-2"} {
		r, ok := FindReviewer(GateTestsFrozen, name)
		if !ok {
			t.Fatalf("AC-3 there is no row for %s at tests_frozen", name)
		}
		if r.Blocks(off) || r.OnBreach(off) || r.Stops(off, VerdictBlock) || r.CanStop(off) {
			t.Errorf("AC-3 %s with can_block false can still stop the freeze", name)
		}
		if !r.Expected(off) {
			t.Errorf("AC-3 %s with can_block false is not waited for", name)
		}
		on := ReviewPolicy{}
		if !r.Blocks(on) || !r.Stops(on, VerdictBlock) || r.Stops(on, VerdictApprove) || r.Stops(on, VerdictNote) {
			t.Errorf("AC-3 %s by default: blocks %v", name, r.Blocks(on))
		}
	}
}

// AC-2: the project manager may leave the test reviewer out, sensitive story or
// not, and a staffing that keeps it, or has no row for it, leaves it in (D1).
func TestAC2AStaffingMayLeaveTheTestReviewerOutAndOnlyARowThatSaysSoDoes(t *testing.T) {
	for _, sensitive := range []bool{false, true} {
		if NeverLeftOut(testReviewer, sensitive) || NeverLeftOut("test-reviewer-2", sensitive) {
			t.Errorf("AC-2 the floor keeps the test reviewer in (sensitive %v)", sensitive)
		}
	}
	out := NewRecord("A-1", time.Now())
	out.Staffing = &Staffing{Agents: []Staffed{{Agent: testReviewer, Needed: false, Reason: "nothing to read"}}}
	if !out.LeftOut(testReviewer) {
		t.Error("AC-2 a row with needed false does not leave the test reviewer out")
	}
	kept := NewRecord("A-1", time.Now())
	kept.Staffing = &Staffing{Agents: []Staffed{{Agent: "perf", Needed: false, Reason: "no budget"}}}
	if kept.LeftOut(testReviewer) || NewRecord("A-1", time.Now()).LeftOut(testReviewer) {
		t.Error("D1 a staffing with no row for the test reviewer, or none at all, leaves it out")
	}
}
