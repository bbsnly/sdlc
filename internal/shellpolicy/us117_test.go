package shellpolicy

import (
	"slices"
	"testing"
)

// US-117 AC-2 and T1/S3 of its threats. --finding takes a free-text value, and
// reviewRole skips a flag's value only when ValueFlags lists the flag: otherwise
// the value sits among the positionals and the gate and role are not found as a
// pair, or a word of the value is paired with the role instead. Cobra reads each
// command below as a review by the code reviewer, whatever the value says.
func TestAC2AFindingCannotMoveARoleOutOfTheRuleThatKeepsAReviewItsReviewersOwn(t *testing.T) {
	t.Parallel()
	if !ValueFlags["--finding"] {
		t.Error("ValueFlags does not list --finding")
	}
	// The three closings take no value: listed, the word after one would be
	// skipped as its value.
	for _, flag := range []string{"--fixed", "--declined", "--referred"} {
		if ValueFlags[flag] {
			t.Errorf("ValueFlags lists %s, which takes no value", flag)
		}
	}
	const rule = "review-is-recorded-by-its-reviewer"
	reviewer := State{CommitReady: true, Agent: "code-reviewer"}
	for _, c := range []struct {
		command string
		// others are the agents that may not record it.
		others []string
	}{
		// Between the gate and the role: none of these is refused today.
		{"sdlc review add code_review --finding x code-reviewer approve", []string{"implementer", ""}},
		{`sdlc review add code_review --finding "nit a.go: the name" code-reviewer approve`, []string{"implementer"}},
		{"sdlc review add code_review --finding a --finding b code-reviewer approve", []string{"implementer"}},
		{"sdlc review add code_review --nits 1 --finding x code-reviewer approve", []string{"implementer"}},
		// The value is a gate, so the gate the command names is not paired.
		{"sdlc review add code_review --finding design_review code-reviewer approve", []string{"implementer"}},
		// The value is a role: read as a positional, it pairs with the gate as the
		// agent's own review, and the code reviewer's verdict is recorded.
		{"sdlc review add code_review --finding security code-reviewer approve", []string{"security", "implementer"}},
		{"sdlc review add code_review --finding perf code-reviewer block --important 1", []string{"perf"}},
		// Before the subcommand: the value read as the subcommand hid the review.
		{"sdlc --finding x review add code_review code-reviewer approve", []string{"implementer"}},
		// Spellings the released rule already refuses, which have to stay refused.
		{"sdlc review add code_review --finding= code-reviewer approve", []string{"implementer"}},
		{"sdlc review add code_review --finding=security code-reviewer approve", []string{"security"}},
		{"sdlc review add --finding x code_review code-reviewer approve", []string{"implementer"}},
		{"sdlc review add code_review --finding -x code-reviewer approve", []string{"implementer"}},
		{"sdlc review add code_review code-reviewer approve --finding x", []string{"implementer"}},
	} {
		t.Run("AC-2 "+c.command, func(t *testing.T) {
			t.Parallel()
			for _, agent := range c.others {
				refused(t, c.command, State{CommitReady: true, Agent: agent}, rule)
			}
			allowed(t, c.command, reviewer)
		})
	}
}

// --finding came after the rules that read sdlc's subcommand, as the counts did:
// read only as a flag with a value, the word after it would stop being read as
// the subcommand, and a person's command spelled with it in front would stop
// being refused. Each row is refused today, and has to stay so.
func TestAC2AFindingFlagInFrontOfAPersonsCommandIsReadBothWays(t *testing.T) {
	t.Parallel()
	if !flagReadBothWays("--finding") {
		t.Error("flagReadBothWays does not read --finding both ways")
	}
	for command, rule := range map[string]string{
		"sdlc --finding approve US-001":      "approval-is-a-human-decision",
		"sdlc --finding unfreeze --reason x": "unfreeze-is-a-human-decision",
		"sdlc --finding ack --through HEAD":  "acknowledgement-is-a-human-decision",
		"sdlc --finding release US-001":      "release-is-a-human-decision",
		"sdlc --finding stop":                "stop-is-a-human-decision",
		"sdlc --finding=x approve US-001":    "approval-is-a-human-decision",
	} {
		t.Run("AC-2 "+command, func(t *testing.T) {
			t.Parallel()
			refused(t, command, State{CommitReady: true, Agent: "implementer"}, rule)
		})
	}
}

// Decision 5: any agent may close a finding, and the main conversation, so no
// rule refuses it; what makes a decline stand is the raising reviewer's fresh
// approval and the verifier. It is not a person's decision either.
func TestAC3AnyAgentMayCloseAFinding(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		"sdlc finding close code_review code-reviewer F-1 --fixed",
		`sdlc finding close design_review architect F-2 --declined --reason "step 4 covers it"`,
		`sdlc finding close code_review security F-3 --referred --reason "outside the story"`,
		"sdlc finding close code_review code-reviewer F-1 --declined --reason=x",
	} {
		for _, agent := range []string{"implementer", "", "code-reviewer", "architect", "verifier"} {
			allowed(t, command, State{CommitReady: true, Agent: agent})
		}
		for _, waits := range []bool{true, false} {
			if f, ok := HumanDecisions(command, false, func(string) bool { return waits }); ok {
				t.Errorf("HumanDecisions(%q) refused it under %s", command, f.Rule)
			}
		}
	}
}

// E2 of the threats: `finding` is one of sdlc's commands, so the project
// manager's rule reads it behind a program it does not know, as it reads the
// others; and the project manager, who decides who takes part, closes nothing.
func TestAC3TheProjectManagerClosesNoFinding(t *testing.T) {
	t.Parallel()
	if !slices.Contains(sdlcCommands, "finding") {
		t.Error("sdlcCommands does not name finding")
	}
	manager := State{CommitWhy: "analysis has not passed", Agent: "project-manager"}
	for _, command := range []string{
		"sdlc finding close code_review code-reviewer F-1 --fixed",
		`sdlc finding close design_review architect F-1 --declined --reason "x"`,
		"sdlc --json finding close code_review code-reviewer F-1 --fixed",
		"npx @bbsnly/sdlc finding close code_review code-reviewer F-1 --fixed",
		"mytool sdlc finding close code_review code-reviewer F-1 --fixed",
	} {
		t.Run("AC-3 "+command, func(t *testing.T) {
			t.Parallel()
			refused(t, command, manager, managerRule)
		})
	}
}
