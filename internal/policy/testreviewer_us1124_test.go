package policy

import "testing"

// US-112.4, THREATS.md (elevation of privilege): the test reviewer is a review
// role under every name the plugin gives it, so it writes nothing but its own
// notes, and above all no test it is reviewing.
func TestAC6TheTestReviewerWritesNoTestUnderAnyOfItsNames(t *testing.T) {
	for _, agent := range []string{"sdlc:test-reviewer", "sdlc-test-reviewer", "test-reviewer"} {
		if !IsLoopRole(agent) {
			t.Errorf("AC-6 %s is not one of the loop's roles", agent)
		}
		for _, r := range []Request{
			{Tool: "Write", Agent: agent, Path: "internal/x_test.go", Story: "A-1", Tests: Tests{IsTest: true}},
			{Tool: "Edit", Agent: agent, Path: "internal/x_test.go", Story: "A-1", Tests: Tests{IsTest: true}},
			{Tool: "Write", Agent: agent, Path: "internal/x.go", Story: "A-1"},
		} {
			if got := Evaluate(r); got.Allowed || got.Rule != "reviewer-reviews" {
				t.Errorf("AC-6 %s %s %s: allowed %v under %q, want refused by reviewer-reviews", agent, r.Tool, r.Path, got.Allowed, got.Rule)
			}
		}
		if got := Evaluate(Request{Tool: "Write", Agent: agent, Path: ".sdlc/stories/A-1/notes.md", Story: "A-1"}); !got.Allowed {
			t.Errorf("AC-6 %s may not write notes in the story's directory: %s", agent, got.Rule)
		}
	}
}
