package policy

import (
	"strings"
	"testing"
)

// The main conversation writes files that are neither the story's code nor its
// tests: a commit message, a note for the person. orchestrator-delegates
// refuses them anywhere outside .sdlc/, and said only to delegate, which no
// agent's gate covers; the way found was a shell redirect, which writes code
// as readily. What the rule refuses has not changed. Its route now names the
// place such a file was always allowed: the story's own directory.

const delegatesRule = "orchestrator-delegates"

// routeOffersTheShell names the word by which a route points at the shell.
func routeOffersTheShell(route string) (string, bool) {
	lower := strings.ToLower(route)
	for _, word := range []string{"shell", "bash", "redirect", "printf", "heredoc", "here-doc", "cat >", "echo ", "tee "} {
		if strings.Contains(lower, word) {
			return word, true
		}
	}
	return "", false
}

func TestAC4TheRouteOfOrchestratorDelegatesNamesTheStorysOwnDirectory(t *testing.T) {
	for _, story := range []string{"A-1", "B-7", "US-104"} {
		for _, agent := range []string{"", "general-purpose"} {
			for _, path := range []string{"commit-msg.txt", "notes/handoff.md", "internal/billing/invoice.go", "docs/usage.md"} {
				for _, tool := range []string{"Write", "Edit", "MultiEdit"} {
					t.Run("AC-4 "+story+" "+agent+" "+tool+" "+path, func(t *testing.T) {
						got := Evaluate(Request{Tool: tool, Agent: agent, Path: path, Story: story, Backlog: "user_stories.json"})
						if got.Allowed || got.Rule != delegatesRule {
							t.Fatalf("%q writing %s: allowed = %v under %q, want a refusal under %s", agent, path, got.Allowed, got.Rule, delegatesRule)
						}
						if want := ".sdlc/stories/" + story; !strings.Contains(got.Route, want) {
							t.Errorf("the route does not name %s, where a file that is not the story's work goes: %s", want, got.Route)
						}
						if !strings.Contains(got.Route, "delegate") {
							t.Errorf("the route no longer says to delegate the story's work: %s", got.Route)
						}
						if word, offers := routeOffersTheShell(got.Route); offers {
							t.Errorf("the route offers the shell (%q) as the way to write the file: %s", word, got.Route)
						}
					})
				}
			}
		}
	}
}

// The directory the route names is one the rules let the main conversation
// write to, and what the loop keeps there is still refused by its own rule.
func TestAC4TheDirectoryTheRouteNamesIsAllowedAndWhatTheLoopKeepsThereIsNot(t *testing.T) {
	for _, story := range []string{"A-1", "B-7"} {
		dir := ".sdlc/stories/" + story + "/"
		for _, name := range []string{"commit-msg.txt", "notes/handoff.md", "pr-body.md"} {
			t.Run("AC-4 "+dir+name, func(t *testing.T) {
				if got := Evaluate(Request{Tool: "Write", Path: dir + name, Story: story, Backlog: "user_stories.json"}); !got.Allowed {
					t.Errorf("the main conversation writing %s was refused under %s: %s", dir+name, got.Rule, got.Reason)
				}
			})
		}
		for name, rule := range map[string]string{
			"gate-record.json": "gate-record-is-written-by-the-tool",
			"ANALYSIS.md":      "gate-artifact-is-written-by-the-tool",
		} {
			t.Run("AC-4 AC-5 "+dir+name, func(t *testing.T) {
				got := Evaluate(Request{Tool: "Write", Path: dir + name, Story: story, Backlog: "user_stories.json"})
				if got.Allowed || got.Rule != rule {
					t.Errorf("the main conversation writing %s: allowed = %v under %q, want a refusal under %s", dir+name, got.Allowed, got.Rule, rule)
				}
			})
		}
	}
}

// What the rule refuses is as it was: the main conversation and an agent the
// loop does not know, writing anything outside .sdlc/ and the code map.
func TestAC4AC5OrchestratorDelegatesStillRefusesWhatItRefused(t *testing.T) {
	for _, agent := range []string{"", "general-purpose", "my-own-agent"} {
		for _, path := range []string{"commit-msg.txt", "internal/billing/invoice.go", "internal/billing/invoice_test.go", "README.md", "docs/usage.md"} {
			t.Run("AC-4 AC-5 "+agent+" "+path, func(t *testing.T) {
				got := Evaluate(Request{Tool: "Write", Agent: agent, Path: path, Story: "A-1", Backlog: "user_stories.json"})
				if got.Allowed || got.Rule != delegatesRule {
					t.Errorf("%q writing %s: allowed = %v under %q, want a refusal under %s", agent, path, got.Allowed, got.Rule, delegatesRule)
				}
			})
		}
	}
}
