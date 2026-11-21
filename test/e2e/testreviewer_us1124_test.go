// US-112.4 AC-9 in a sandbox: the built binary, its every call put to the hook
// through the plugin launcher first, in a real git repository `sdlc init` set
// up. The project manager staffs the test reviewer; the freeze is refused with no
// review, with a block and with a stale approve, and taken after an approve of
// the tests as they are; a new test file then makes that approve stale. The
// hook refuses the test author recording the reviewer's approve itself
// (THREATS.md, spoofing).

package e2e

import (
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// trFreezeRefused is a freeze of the session under test refused with the code,
// naming the test reviewer and saying what to do next.
func (sb sandbox) trFreezeRefused(t *testing.T, code, says string) {
	t.Helper()
	r := sb.does(t, sb.tree(), "sdet", "", "freeze")
	if r.code != 1 || r.stdout != "" {
		t.Errorf("AC-9 freeze: exit %d, stdout %q; want a refusal\n%s", r.code, r.stdout, r.stderr)
	}
	for _, want := range []string{code, "test-reviewer", says, "fix "} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("AC-9 the refused freeze does not say %q:\n%s", want, r.stderr)
		}
	}
}

func TestAC9TheFreezeWaitsForTheStaffedTestReviewerInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	sb.works(t, tree, "", "", "gate", "dor", "pass", "--note", "the criteria are testable")
	sb.works(t, tree, "researcher", sandboxAnalysis, "artifact", "write", "analysis")
	sb.works(t, tree, "researcher", sandboxThreats, "artifact", "write", "threats")
	sb.works(t, tree, "project-manager", staffingDocument(t), "staffing", "set")
	sb.works(t, tree, "", "", "gate", "analysis", "pass", "--security-sensitive=false")

	var atGate []string
	for _, a := range sb.shows(t).AtGate {
		atGate = append(atGate, a.Agent)
	}
	if !strings.Contains(strings.Join(atGate, ","), "test-reviewer") {
		t.Fatalf("AC-9 staffing show does not delegate Gate 3 to the test reviewer: %v", atGate)
	}

	sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)
	sb.works(t, tree, "sdet", sandboxTestPlan, "artifact", "write", "test_plan")
	sb.trFreezeRefused(t, "SDLC-E0029", "has not reviewed")

	// The test author cannot record the approve it is waiting for.
	sb.refusesAs(t, "sdet", "sdlc review add tests_frozen test-reviewer approve", "review-is-recorded-by-its-reviewer")
	sb.refusesAs(t, "", "sdlc review add tests_frozen test-reviewer approve", "review-is-recorded-by-its-reviewer")

	sb.works(t, tree, "test-reviewer", "# Test review\n", "review", "add", "tests_frozen", "test-reviewer", "block",
		"--important", "1", "--note", "AC-1 has no failing test")
	sb.trFreezeRefused(t, "SDLC-E0030", "AC-1 has no failing test")

	sb.works(t, tree, "test-reviewer", "# Test review\n", "review", "add", "tests_frozen", "test-reviewer", "approve")
	sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest+"\n// corrected after the approve\n")
	sb.trFreezeRefused(t, "SDLC-E0029", "changed since")

	sb.works(t, tree, "test-reviewer", "# Test review\n", "review", "add", "tests_frozen", "test-reviewer", "approve")
	if out := sb.normalise(sb.works(t, tree, "sdet", "", "freeze")); out != "US-001  1 test file frozen\n  invoice_test.go\n" {
		t.Errorf("AC-9 the freeze after the approve printed %q", out)
	}
	sb.works(t, tree, "", "", "gate", "tests_frozen", "pass", "--note", "1 criterion, 1 failing test")

	// A person lifts the freeze; a new test file makes the approve stale.
	if r := sb.person(t, tree, "unfreeze", "--reason", "AC-1 needs a second case"); r.code != 0 {
		t.Fatalf("the person's unfreeze: exit %d\n%s", r.code, r.stderr)
	}
	sb.writesFile(t, "sdet", "invoice_more_test.go", sandboxTest)
	list := sb.works(t, tree, "", "", "review", "list", "--gate", "tests_frozen")
	if !strings.Contains(list, "test-reviewer") || !strings.Contains(list, "stale") {
		t.Errorf("AC-9 review list does not call the approve stale once a test file was added:\n%s", list)
	}
	sb.trFreezeRefused(t, "SDLC-E0029", "changed since")
}

// fixtureReviewers is the roster's reviewers of a gate as a project made by
// project or storyRepository has them: projectManagerOff disables the test
// reviewer there (decision D1), and a disabled reviewer is refused its review.
func fixtureReviewers(gate model.Gate) []model.Reviewer {
	var out []model.Reviewer
	for _, r := range model.ReviewersFor(gate) {
		if r.Role != "test-reviewer" {
			out = append(out, r)
		}
	}
	return out
}
