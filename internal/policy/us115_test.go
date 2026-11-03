package policy

import (
	"strings"
	"testing"
)

const rpRule = "review-policy-is-not-edited"

// The person's Q1. The policy says what every reviewer reports, and the reviewers
// read the worktree's copy, so a story's implementer rewriting it weakened the
// review of its own work. It is where the configuration says it is, so it is
// keyed on the configured path as the backlog is, and held the way the disk folds
// case. A person changes it by hand.
func TestQ1TheReviewPolicyIsNotEditedByAnyAgent(t *testing.T) {
	t.Parallel()
	for _, agent := range []string{
		"", "sdlc:researcher", "sdlc:sdet", "sdlc:implementer", "sdlc:bookkeeper",
		"sdlc:architect", "sdlc:code-reviewer", "general-purpose",
	} {
		for _, tool := range []string{"Write", "Edit"} {
			for policy, paths := range map[string][]string{
				"REVIEW.md":      {"REVIEW.md", "review.md", "Review.MD"},
				"docs/policy.md": {"docs/policy.md", "Docs/POLICY.md"},
			} {
				for _, path := range paths {
					got := Evaluate(Request{Tool: tool, Agent: agent, Path: path, Story: "A-1", ReviewPolicy: policy})
					if got.Allowed || got.Rule != rpRule {
						t.Errorf("%q using %s on %s with the policy at %s: rule %q, allowed %v",
							agent, tool, path, policy, got.Rule, got.Allowed)
					}
					if !strings.Contains(got.Route, "by hand") {
						t.Errorf("the route does not say a person changes it by hand: %q", got.Route)
					}
				}
			}
		}
	}

	// Moved, the old name is an ordinary file, and a sibling is not the policy. The
	// hook always names a path; no path named is no rule, as for the backlog.
	for _, r := range []Request{
		{Tool: "Write", Agent: "sdlc:implementer", Path: "REVIEW.md", Story: "A-1", ReviewPolicy: "docs/policy.md"},
		{Tool: "Write", Agent: "sdlc:implementer", Path: "REVIEW.md", Story: "A-1"},
		{Tool: "Write", Agent: "sdlc:implementer", Path: "docs/REVIEW.md", Story: "A-1", ReviewPolicy: "REVIEW.md"},
		{Tool: "Write", Agent: "sdlc:implementer", Path: "REVIEW.md.txt", Story: "A-1", ReviewPolicy: "REVIEW.md"},
		{Tool: "Write", Agent: "sdlc:implementer", Path: "REVIEWS.md", Story: "A-1", ReviewPolicy: "REVIEW.md"},
		// Every reviewer is told to read it.
		{Tool: "Read", Agent: "sdlc:code-reviewer", Path: "REVIEW.md", Story: "A-1", ReviewPolicy: "REVIEW.md"},
	} {
		if got := Evaluate(r); !got.Allowed {
			t.Errorf("%s by %s with the policy at %q was refused by %s", r.Path, r.Tool, r.ReviewPolicy, got.Rule)
		}
	}
}
