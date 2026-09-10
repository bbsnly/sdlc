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
		permits: Request{Tool: "Write", Agent: "sdlc-sdet", Path: "internal/x_test.go", Story: "A-1", Tests: Tests{IsTest: true}},
	},
	"write-protected-path": {
		denies:  Request{Tool: "Edit", Agent: "sdlc-researcher", Path: ".sdlc/state/active", Story: "A-1"},
		permits: Request{Tool: "Edit", Agent: "sdlc-researcher", Path: ".sdlc/stories/A-1/notes.md", Story: "A-1"},
	},
	"frozen-test-is-not-edited": {
		denies: Request{Tool: "Edit", Agent: "sdlc-implementer", Path: "internal/x_test.go", Story: "A-1",
			Tests: Tests{IsTest: true, Frozen: true, Locked: true}},
		permits: Request{Tool: "Edit", Agent: "sdlc-implementer", Path: "internal/x.go", Story: "A-1",
			Tests: Tests{Frozen: true}},
	},
	"no-new-test-after-the-freeze": {
		denies: Request{Tool: "Write", Agent: "sdlc-sdet", Path: "internal/new_test.go", Story: "A-1",
			Tests: Tests{IsTest: true, Frozen: true}},
		permits: Request{Tool: "Write", Agent: "sdlc-sdet", Path: "internal/new_test.go", Story: "A-1",
			Tests: Tests{IsTest: true, Frozen: true, AllowNew: true}},
	},
	"implementer-does-not-write-tests": {
		denies: Request{Tool: "Write", Agent: "sdlc-implementer", Path: "internal/x_test.go", Story: "A-1",
			Tests: Tests{IsTest: true}},
		permits: Request{Tool: "Write", Agent: "sdlc-implementer", Path: "internal/x.go", Story: "A-1"},
	},
	"sdet-writes-tests-only": {
		denies:  Request{Tool: "Write", Agent: "sdlc-sdet", Path: "internal/billing/invoice.go", Story: "A-1"},
		permits: Request{Tool: "Write", Agent: "sdlc-sdet", Path: "internal/billing/invoice_test.go", Story: "A-1", Tests: Tests{IsTest: true}},
	},
	"researcher-writes-analysis-only": {
		denies:  Request{Tool: "Write", Agent: "sdlc-researcher", Path: "internal/billing/invoice.go", Story: "A-1"},
		permits: Request{Tool: "Write", Agent: "sdlc-researcher", Path: "CODEMAP.md", Story: "A-1"},
	},
	"gate-record-is-written-by-the-tool": {
		denies:  Request{Tool: "Edit", Agent: "", Path: ".sdlc/stories/A-1/gate-record.json", Story: "A-1"},
		permits: Request{Tool: "Edit", Agent: "", Path: ".sdlc/stories/A-1/reviews/gate-record.json", Story: "A-1"},
	},
	"gate-artifact-is-written-by-the-tool": {
		denies:  Request{Tool: "Write", Agent: "sdlc:researcher", Path: ".sdlc/stories/A-1/ANALYSIS.md", Story: "A-1"},
		permits: Request{Tool: "Write", Agent: "sdlc:researcher", Path: ".sdlc/stories/A-1/notes.md", Story: "A-1"},
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

// The same agent arrives under a different name depending on how it was
// installed. A rule that matched only one spelling would stop enforcing the
// moment the install method changed.
func TestTheResearcherIsRecognisedUnderEverySpelling(t *testing.T) {
	for _, name := range []string{"sdlc:researcher", "sdlc-researcher", "researcher"} {
		got := Evaluate(Request{Tool: "Write", Agent: name, Path: "internal/x.go", Story: "A-1"})
		if got.Allowed {
			t.Errorf("%q was not recognised as the analysis agent", name)
		}
		if got.Rule != "researcher-writes-analysis-only" {
			t.Errorf("%q was refused by %s", name, got.Rule)
		}
	}
}

// "sdlc:" alone must not be read as the main conversation, and an agent whose
// name merely starts with the same letters is a different agent.
func TestNormalizeAgentDoesNotOverreach(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "",
		"sdlc:sdet":         "sdet",
		"sdlc-implementer":  "implementer",
		"sdlcsomething":     "sdlcsomething",
		"other:researcher":  "other:researcher",
		"  sdlc:verifier  ": "verifier",
	} {
		if got := NormalizeAgent(in); got != want {
			t.Errorf("NormalizeAgent(%q) = %q, want %q", in, got, want)
		}
	}
}

// The hole this closes was found by running the loop for real: the main
// conversation wrote the analysis itself and every rule allowed it, because the
// orchestrator may write under .sdlc/ and the analysis lives under .sdlc/.
//
// The rule refuses the researcher too. That is deliberate and it is not a
// weaker rule: a gate artifact is loop state, and loop state goes through the
// tool. It also happens to be the only shape that works, because Claude Code
// refuses a subagent's Write when the filename reads like a report.
func TestAGateArtifactIsWrittenByTheToolAndNobodyElse(t *testing.T) {
	for _, artifact := range []string{"ANALYSIS.md", "THREATS.md"} {
		path := ".sdlc/stories/A-1/" + artifact

		for _, agent := range []string{"", "sdlc:researcher", "sdlc:sdet", "sdlc-implementer", "somebody-else"} {
			got := Evaluate(Request{Tool: "Write", Agent: agent, Path: path, Story: "A-1"})
			if got.Allowed {
				t.Errorf("%s was written in place by %q", artifact, agent)
			}
			if got.Rule != "gate-artifact-is-written-by-the-tool" {
				t.Errorf("%q writing %s was refused by %s, want the artifact rule",
					agent, artifact, got.Rule)
			}
			if !strings.Contains(got.Route, "sdlc artifact write") {
				t.Errorf("the denial does not name the sanctioned route: %q", got.Route)
			}
		}
	}
}

// The gate record is what every later gate reads to find out what happened. An
// assistant that can edit it can make it say something that did not.
func TestTheGateRecordIsNobodysToEdit(t *testing.T) {
	for _, agent := range []string{"", "sdlc:researcher", "sdlc-implementer", "sdlc:verifier"} {
		got := Evaluate(Request{
			Tool: "Write", Agent: agent,
			Path: ".sdlc/stories/A-1/gate-record.json", Story: "A-1",
		})
		if got.Allowed {
			t.Errorf("%q edited the gate record", agent)
			continue
		}
		if !strings.Contains(got.Route, "sdlc gate") {
			t.Errorf("the denial does not name the route: %q", got.Route)
		}
	}
}

// The rule is about the story's own artifacts, not about the directory. Other
// bookkeeping under it stays open, and a file of the same name somewhere else
// is a different file.
func TestTheArtifactRuleIsNarrow(t *testing.T) {
	for _, path := range []string{
		".sdlc/stories/A-1/notes.md",
		".sdlc/stories/A-1/reviews/ANALYSIS.md",
		".sdlc/lessons.md",
	} {
		if got := Evaluate(Request{Tool: "Write", Agent: "", Path: path, Story: "A-1"}); !got.Allowed {
			t.Errorf("%s was refused by %s: %s", path, got.Rule, got.Reason)
		}
	}
	// A file of the same name outside the story directory is a different file.
	// It is still governed -- by the rule that governs code, not this one.
	got := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: "docs/ANALYSIS.md", Story: "A-1"})
	if !got.Allowed {
		t.Errorf("docs/ANALYSIS.md was refused by %s", got.Rule)
	}
}

func TestAnEmptyPathIsNotADecision(t *testing.T) {
	if !Evaluate(Request{Tool: "Write", Path: "", Story: "A-1"}).Allowed {
		t.Error("a tool call with no path was refused")
	}
}

// ------------------------------------------------------------------ the freeze

// The freeze is the loop's central claim, so it holds against everyone. An
// exception for the agent that wrote the tests would be an exception for the
// agent most able to argue for one.
func TestAFrozenTestIsFrozenForEverybody(t *testing.T) {
	frozen := Tests{IsTest: true, Frozen: true, Locked: true}
	for _, agent := range []string{"", "sdlc:sdet", "sdlc:implementer", "sdlc:verifier", "somebody"} {
		got := Evaluate(Request{
			Tool: "Edit", Agent: agent, Path: "internal/x_test.go", Story: "A-1", Tests: frozen,
		})
		if got.Allowed {
			t.Errorf("%q edited a frozen test", agent)
			continue
		}
		if !strings.Contains(got.Route, "sdlc unfreeze") {
			t.Errorf("the denial does not name the way out: %q", got.Route)
		}
	}
}

// Before the freeze, writing tests is the whole job of the gate that writes
// them. A rule that fired early would make the loop impossible to run.
func TestBeforeTheFreezeTheTestAuthorWritesTests(t *testing.T) {
	got := Evaluate(Request{
		Tool: "Write", Agent: "sdlc:sdet", Path: "internal/x_test.go", Story: "A-1",
		Tests: Tests{IsTest: true},
	})
	if !got.Allowed {
		t.Errorf("the test author was refused by %s: %s", got.Rule, got.Reason)
	}
}

// Production code is untouched by any of this. The freeze is about tests, and a
// rule that reached past them would stop the implementation gate dead.
func TestTheFreezeDoesNotReachProductionCode(t *testing.T) {
	got := Evaluate(Request{
		Tool: "Edit", Agent: "sdlc:implementer", Path: "internal/billing/invoice.go", Story: "A-1",
		Tests: Tests{Frozen: true},
	})
	if !got.Allowed {
		t.Errorf("the implementer was refused by %s: %s", got.Rule, got.Reason)
	}
}

// A project can decide that new test files are allowed after the freeze. It is
// a real loosening -- a test written now can be written to pass -- so the rule
// has to actually read the setting rather than assume the strict answer.
//
// The setting is about the agent that writes tests. It does not let the
// implementer write one: that rule is not a setting, and a test asserting it
// here is how it stays that way.
func TestANewTestAfterTheFreezeFollowsTheProjectsSetting(t *testing.T) {
	req := Request{Tool: "Write", Agent: "sdlc:sdet", Path: "internal/extra_test.go",
		Story: "A-1", Tests: Tests{IsTest: true, Frozen: true}}

	if Evaluate(req).Allowed {
		t.Error("a new test file was added after the freeze")
	}
	req.Tests.AllowNew = true
	if got := Evaluate(req); !got.Allowed {
		t.Errorf("the project allows new test files, but %s refused: %s", got.Rule, got.Reason)
	}

	req.Agent = "sdlc:implementer"
	if got := Evaluate(req); got.Allowed {
		t.Error("the setting let the implementer write a test")
	} else if got.Rule != "implementer-does-not-write-tests" {
		t.Errorf("refused by %s, want implementer-does-not-write-tests", got.Rule)
	}
}
