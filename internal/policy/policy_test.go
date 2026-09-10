package policy

import (
	"strings"
	"testing"
)

// fixture pairs one rule with a request it must refuse and a request it must
// let through. Both halves matter: a rule with only a positive case can be
// satisfied by refusing everything.
type fixture struct {
	denies  Request
	permits Request
}

// fixtures is keyed by rule id, and a test asserts that this set and the rule
// set are the same. Adding a rule without a fixture fails; removing a rule and
// leaving its fixture fails too.
var fixtures = map[string]fixture{
	"write-outside-repository": {
		denies:  Request{Tool: "Write", Path: "/etc/hosts", Outside: true, Story: "A-1"},
		permits: Request{Tool: "Write", Agent: "sdlc-sdet", Path: "internal/x_test.go", Story: "A-1"},
	},
	"write-protected-path": {
		denies:  Request{Tool: "Edit", Agent: "sdlc-researcher", Path: ".sdlc/state/active", Story: "A-1"},
		permits: Request{Tool: "Edit", Agent: "sdlc-researcher", Path: ".sdlc/stories/A-1/ANALYSIS.md", Story: "A-1"},
	},
	"researcher-writes-analysis-only": {
		denies:  Request{Tool: "Write", Agent: "sdlc-researcher", Path: "internal/billing/invoice.go", Story: "A-1"},
		permits: Request{Tool: "Write", Agent: "sdlc-researcher", Path: "CODEMAP.md", Story: "A-1"},
	},
	"orchestrator-delegates": {
		denies:  Request{Tool: "Write", Agent: "", Path: "internal/billing/invoice.go", Story: "A-1"},
		permits: Request{Tool: "Write", Agent: "", Path: ".sdlc/stories/A-1/notes.md", Story: "A-1"},
	},
}

func TestEveryRuleHasBothFixtures(t *testing.T) {
	ids := map[string]bool{}
	for _, r := range Rules {
		if ids[r.ID] {
			t.Errorf("%s is registered twice", r.ID)
		}
		ids[r.ID] = true
		if _, ok := fixtures[r.ID]; !ok {
			t.Errorf("%s has no fixture; a rule nobody tested is a rule nobody trusts", r.ID)
		}
	}
	for id := range fixtures {
		if !ids[id] {
			t.Errorf("there is a fixture for %s but no such rule", id)
		}
	}
}

// A denial that explains nothing makes an assistant retry, work around the
// hook, or give up. The route is what prevents that, so it is not optional.
func TestEveryRuleNamesASanctionedRoute(t *testing.T) {
	for _, r := range Rules {
		if strings.TrimSpace(r.Route) == "" {
			t.Errorf("%s has no route; a rule that cannot say what to do instead "+
				"will be worked around", r.ID)
		}
	}
}

func TestEachFixtureDeniesAndPermitsWhatItSays(t *testing.T) {
	for id, f := range fixtures {
		t.Run(id, func(t *testing.T) {
			got := Evaluate(f.denies)
			if got.Allowed {
				t.Fatalf("the denying fixture was allowed: %+v", f.denies)
			}
			if got.Rule != id {
				t.Errorf("refused by %s, want %s (%s)", got.Rule, id, got.Reason)
			}
			if got.Reason == "" || got.Route == "" {
				t.Errorf("verdict = %+v", got)
			}

			if permitted := Evaluate(f.permits); !permitted.Allowed {
				t.Errorf("the permitting fixture was refused by %s: %s",
					permitted.Rule, permitted.Reason)
			}
		})
	}
}

func TestMessageCarriesTheRuleTheRouteAndTheID(t *testing.T) {
	v := Evaluate(Request{Tool: "Write", Path: "internal/x.go", Story: "A-1"})
	msg := v.Message()
	if !strings.Contains(msg, "Instead:") {
		t.Errorf("message does not offer a route: %q", msg)
	}
	if !strings.Contains(msg, "[orchestrator-delegates]") {
		t.Errorf("message does not name the rule: %q", msg)
	}
	if Allowed.Message() != "" {
		t.Error("an allowed verdict produced a message")
	}
}

// The loop governs writing. Reading, running and everything else is somebody
// else's rule to make.
func TestOnlyWritingToolsAreGoverned(t *testing.T) {
	for _, tool := range []string{"Read", "Bash", "Grep", "Glob", "WebFetch", ""} {
		got := Evaluate(Request{Tool: tool, Path: ".sdlc/state/active", Story: "A-1"})
		if !got.Allowed {
			t.Errorf("%s was governed by %s", tool, got.Rule)
		}
	}
	for _, tool := range []string{"Write", "Edit", "MultiEdit", "NotebookEdit"} {
		if Evaluate(Request{Tool: tool, Path: ".sdlc/state/active", Story: "A-1"}).Allowed {
			t.Errorf("%s was not governed", tool)
		}
	}
}

func TestProtectedPathsAreProtectedFromEveryone(t *testing.T) {
	for _, path := range []string{
		".git/config", ".git/hooks/pre-commit",
		".claude/settings.json",
		"CLAUDE.md",
		".sdlc/config.json",
		".sdlc/state/active", ".sdlc/state/tests.lock",
		".sdlc/claude-progress.json",
	} {
		for _, agent := range []string{"", "sdlc-researcher", "sdlc-implementer", "sdlc-sdet"} {
			got := Evaluate(Request{Tool: "Write", Agent: agent, Path: path, Story: "A-1"})
			if got.Allowed {
				t.Errorf("%s was writable by %q", path, agent)
			}
		}
	}
}

// A path that merely starts with the same letters is a different path.
func TestProtectionIsDirectoryAwareNotPrefixAware(t *testing.T) {
	for _, path := range []string{".gitignore", ".claude-code-version", "CLAUDE.md.bak", ".sdlc-notes"} {
		got := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: path, Story: "A-1"})
		if !got.Allowed {
			t.Errorf("%s was refused by %s, but it is not a protected path", path, got.Rule)
		}
	}
}

func TestAnAgentWithNoRuleOfItsOwnIsNotGoverned(t *testing.T) {
	got := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: "internal/billing/invoice.go", Story: "A-1"})
	if !got.Allowed {
		t.Errorf("sdlc-implementer was refused by %s: %s", got.Rule, got.Reason)
	}
}

// The story directory is per story, so a researcher on A-1 must not write into
// another story's analysis.
func TestResearcherIsScopedToItsOwnStory(t *testing.T) {
	got := Evaluate(Request{
		Tool: "Write", Agent: "sdlc-researcher",
		Path: ".sdlc/stories/B-2/ANALYSIS.md", Story: "A-1",
	})
	if got.Allowed {
		t.Error("the researcher wrote into another story's directory")
	}
}

func TestAnEmptyPathIsNotADecision(t *testing.T) {
	if !Evaluate(Request{Tool: "Write", Path: "", Story: "A-1"}).Allowed {
		t.Error("a tool call with no path was refused")
	}
}
