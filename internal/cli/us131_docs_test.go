package cli

import (
	"regexp"
	"strings"
	"testing"
)

// us131Section is the `## ` section of a docs page whose heading starts so, with
// its white space folded, to the next `## `.
func us131Section(t *testing.T, opening string) string {
	t.Helper()
	for _, part := range regexp.MustCompile(`(?m)^## `).Split(page(t, "commands.md"), -1) {
		if strings.HasPrefix(part, opening) {
			return strings.Join(strings.Fields(part), " ")
		}
	}
	t.Fatalf("docs/commands.md has no section headed %q", "## "+opening)
	return ""
}

// US-131 AC-10: docs/commands.md describes `sdlc decide`, its flags, what it
// prints and how it exits, the `decision` event, and the product_decision reason.
func TestAC10TheCommandsPageDescribesDecideTheEventAndTheReason(t *testing.T) {
	t.Parallel()
	decide := us131Section(t, "`sdlc decide")
	for _, word := range []string{"--question", "--option", "--chosen", "--reason", "--by", "SDLC-E0094", "exit", "`decision`", "DECISIONS.md", "sdlc log", "sdlc ack"} {
		if !strings.Contains(decide, word) {
			t.Errorf("AC-10 the `sdlc decide` section does not say %s", word)
		}
	}
	if escalate := us131Section(t, "`sdlc escalate`"); !strings.Contains(escalate, "product_decision") {
		t.Error("AC-10 the `sdlc escalate` section does not name product_decision")
	}
	if status := us131Section(t, "`sdlc status`"); !strings.Contains(status, "decisions_unread") {
		t.Error("AC-9 the `sdlc status` section does not name decisions_unread")
	}
	if trace := us131Section(t, "`sdlc trace`"); !regexp.MustCompile(`decisions:? \d`).MatchString(trace) {
		t.Error("AC-9 the `sdlc trace` section does not show a story's decisions on its second line")
	}
	if review := us131Section(t, "`sdlc review add"); !strings.Contains(review, "-arbiter") {
		t.Error("AC-6 the `sdlc review add` section does not say a reviewer's limit sends the gate to <role>-arbiter")
	}
}
