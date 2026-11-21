package shellpolicy

import (
	"slices"
	"strings"
	"testing"
)

// US-131. `sdlc decide` is one of sdlc's commands and no person's (T-8 of the
// threats, the settled IQ-5); DECISIONS.md is written by sdlc alone, from the shell
// as from the file tools (T-4, the settled IQ-6). The write rows are the ones the
// trace's own rows try (traceprotect_test.go), with the file's name in their place.

// T-8: the project manager, who runs no sdlc command but the staffing's, records no
// decision, behind a program it does not know as well. The first two rows are
// refused today and stay so; the last two need decide in sdlcCommands.
func TestAC3TheProjectManagerRecordsNoDecision(t *testing.T) {
	t.Parallel()
	if !slices.Contains(sdlcCommands, "decide") {
		t.Error("AC-3 sdlcCommands does not name decide")
	}
	manager := State{CommitWhy: "analysis has not passed", Agent: "project-manager"}
	for _, command := range []string{
		`sdlc decide US-001 --question "q" --option a --option b --chosen a --reason r --by project-manager`,
		`sdlc --json decide US-001 --question q --option a --option b --chosen a --reason r --by researcher`,
		`npx @bbsnly/sdlc decide US-001 --question q --option a --option b --chosen a --reason r --by researcher`,
		`mytool sdlc decide US-001 --question q --option a --option b --chosen a --reason r --by researcher`,
	} {
		t.Run("AC-3 "+command, func(t *testing.T) {
			t.Parallel()
			refused(t, command, manager, managerRule)
		})
	}
}

// IQ-5: no new rule for decide. Any loop agent and the main conversation may run
// it, and it is not a person's decision. The commands are allowed today because
// nothing reads decide as sdlc's; the first check is that something does, so the
// row holds the permission of a command the hook knows.
func TestAC2AnyAgentAndTheMainConversationMayRecordADecision(t *testing.T) {
	t.Parallel()
	if !slices.Contains(sdlcCommands, "decide") {
		t.Fatal("AC-2 sdlcCommands does not name decide, so the hook does not read it as sdlc's command")
	}
	for _, command := range []string{
		`sdlc decide US-001 --question "Which total?" --option gross --option net --chosen net --reason "AC-2 says net" --by researcher`,
		`sdlc decide US-001 --question=q --option=a --option=b --chosen=a --reason=r --by=architect-arbiter`,
	} {
		for _, agent := range []string{"", "researcher", "sdet", "implementer", "architect", "verifier", "bookkeeper"} {
			allowed(t, command, State{CommitReady: true, Agent: agent})
		}
		for _, waits := range []bool{true, false} {
			if f, ok := HumanDecisions(command, false, func(string) bool { return waits }); ok {
				t.Errorf("AC-2 HumanDecisions(%q) refused it under %s", command, f.Rule)
			}
		}
	}
}

// T-8: decide's flags take a value. Read only as a flag's value, the word after
// one would stop being read as the subcommand, and a person's command spelled
// with the flag in front would stop being refused. Each row is refused today,
// and has to stay so once decide is sdlc's command and its flags join ValueFlags.
func TestAC3DecidesFlagsInFrontOfAPersonsCommandStillRefuseIt(t *testing.T) {
	t.Parallel()
	if !slices.Contains(sdlcCommands, "decide") {
		t.Fatal("AC-3 sdlcCommands does not name decide, so its flags are not yet read as its own")
	}
	for _, flag := range []string{"--question", "--option", "--chosen", "--by"} {
		for command, rule := range map[string]string{
			"sdlc " + flag + " approve US-001":      "approval-is-a-human-decision",
			"sdlc " + flag + " unfreeze --reason x": "unfreeze-is-a-human-decision",
			"sdlc " + flag + " ack --through HEAD":  "acknowledgement-is-a-human-decision",
			"sdlc " + flag + " stop":                "stop-is-a-human-decision",
			"sdlc " + flag + "=x approve US-001":    "approval-is-a-human-decision",
		} {
			t.Run("AC-3 "+command, func(t *testing.T) {
				t.Parallel()
				refused(t, command, State{CommitReady: true, Agent: "implementer"}, rule)
			})
		}
	}
}

// T-4: a story's DECISIONS.md is the loop's record of what it decided, and a model
// that could rewrite it could unmake a decision it made. Every way to write it is
// refused under the story files' own rule, whose route names `sdlc decide`.
func TestAC3DecisionsMdCannotBeWrittenThroughTheShell(t *testing.T) {
	for _, command := range traceWrites("DECISIONS.md") {
		f := refused(t, command, ready, ruleLoopState)
		if !strings.Contains(f.Reason+" "+f.Route, "sdlc decide") {
			t.Errorf("AC-3 the refusal of %q does not say that `sdlc decide` records a decision: %+v", command, f)
		}
	}
	refused(t, "echo x > decisions.md", State{CommitReady: true, Dir: ".sdlc/stories/A-1"}, ruleLoopState)
	refused(t, "echo x >> ../stories/A-1/DECISIONS.md", State{CommitReady: true, Dir: ".sdlc/state"}, ruleLoopState)
}

// Reading it stays open, and a file of another name beside it is the story's.
// The write beside the reads is refused first, so the reads are allowed for a
// file the hook protects, not for one it has never heard of.
func TestAC3DecisionsMdCanBeRead(t *testing.T) {
	f := refused(t, "echo x > .sdlc/stories/A-1/DECISIONS.md", ready, ruleLoopState)
	if !strings.Contains(f.Reason+" "+f.Route, "sdlc decide") {
		t.Errorf("AC-3 the refusal of a write to DECISIONS.md does not name `sdlc decide`: %+v", f)
	}
	for _, command := range traceReads("DECISIONS.md") {
		allowed(t, command, ready)
	}
	allowed(t, "echo x > .sdlc/stories/A-1/decisions-notes.md", ready)
	allowed(t, "echo x > .sdlc/stories/A-1/scratch/DECISIONS.md", ready)
}
