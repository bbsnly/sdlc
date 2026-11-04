package policy

import (
	"strings"
	"testing"
)

// US-117 AC-2 and its design review's I1 (architect): at the design and code
// reviews a count above 0 is refused unless each finding is listed with
// --finding, so a reviewer the hook turns back with a `sdlc review add` to run
// is given the flag as well, or the command it copies is refused next.
func TestAC2EveryRouteThatGivesAReviewAddCommandNamesTheFindingFlag(t *testing.T) {
	t.Parallel()
	given := 0
	for _, rule := range Rules {
		if !strings.Contains(rule.Route, "sdlc review add") {
			continue
		}
		given++
		if !strings.Contains(rule.Route, "--finding") {
			t.Errorf("AC-2 the route of %s gives `sdlc review add` without --finding: %s", rule.ID, rule.Route)
		}
	}
	// The two rules that send a reviewer to the command, so that a route reworded
	// away from it does not leave this test checking nothing.
	for _, id := range []string{"reviewer-reviews", "review-is-written-by-the-tool"} {
		got := Evaluate(fixtures[id].denies)
		if got.Rule != id {
			t.Fatalf("the fixture for %s is refused by %s", id, got.Rule)
		}
		if !strings.Contains(got.Route, "sdlc review add") || !strings.Contains(got.Route, "--finding") {
			t.Errorf("AC-2 a reviewer refused by %s is not told to record its review with --finding: %s", id, got.Route)
		}
	}
	if given < 2 {
		t.Errorf("only %d routes give `sdlc review add`", given)
	}
}
