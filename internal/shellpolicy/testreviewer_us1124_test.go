package shellpolicy

import "testing"

// US-112.4, THREATS.md (spoofing): the test author, or the main conversation,
// recording the test reviewer's approve would freeze its own tests. The rule is
// the roster's, so the new row is covered; these hold it, instances and the
// spellings an instance's name can take included. Tried in bash and zsh, each
// hands sdlc test-reviewer or test-reviewer-2 as one word.
func TestAC1AC3OnlyTheTestReviewerRecordsItsReviewOfTheTests(t *testing.T) {
	commands := []string{
		"sdlc review add tests_frozen test-reviewer approve",
		"sdlc review add tests_frozen Test-Reviewer approve --note ok",
		`sdlc review add --note "fine" tests_frozen test-reviewer approve`,
		"sdlc review add tests_frozen test-reviewer-2 approve",
		`sdlc review add tests_frozen "test-reviewer"-2 approve`,
		"npx @bbsnly/sdlc --json review add tests_frozen test-reviewer approve",
		"sdlc review add --story A-1 tests_frozen test-reviewer approve",
	}
	for _, command := range commands {
		for _, agent := range []string{"", "sdet", "implementer", "researcher", "red-team", "architect", "verifier"} {
			refused(t, command, State{CommitWhy: "code_review has not passed", Agent: agent}, reviewerRule)
		}
		allowed(t, command, State{CommitWhy: "code_review has not passed", Agent: "test-reviewer"})
	}
}
