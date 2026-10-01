package sdlcerr

// US-029.3, Gate 7's rework: SDLC-E0055's catalogue fix -- the general one a
// reader who arrives from the code alone gets, for a landing.go call site
// that builds the error with no WithFix of its own -- names the same
// answer-first route as every specific one landing.go does give: a person
// answers the escalation first, with "sdlc approve <ID>" or its --reject, in
// .sdlc/worktrees/<ID>, before the gate command that reopens the gate.

import (
	"strings"
	"testing"
)

func TestE0055sCatalogueFixNamesTheAnswerFirstRouteAndTheWorktree(t *testing.T) {
	fix := New(LandingCheckFailed, "a check failed", "why it failed").Fix
	for _, want := range []string{
		`"sdlc approve <ID>"`,
		"--reject",
		".sdlc/worktrees/<ID>",
		"sdlc gate implementation fail --story <ID>",
		"/sdlc:next",
	} {
		if !strings.Contains(fix, want) {
			t.Errorf("SDLC-E0055's catalogue fix does not name %q:\n%s", want, fix)
		}
	}
}
