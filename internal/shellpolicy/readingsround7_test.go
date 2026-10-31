package shellpolicy

// US-112, design review round 7, security's blocking findings:
//
//   - B1: bash, zsh and sh take `--` after eval as the end of its options,
//     and both walks read `--` as the program, so the string after it met no
//     rule: the commit gate, a person's approve, unfreeze and ack, and the
//     path rules;
//   - B2: the program walk stopped reading at the fifth nested script and
//     refused nothing, where the substitution bound refuses;
//   - B3: zsh's short forms whose `}` or `]]` stand among the words emulate
//     drops before its -c, or mapfile before its -C;
//   - the architect's note that movesInAScript fails closed past the cap.
//
// Run in this session under bash and zsh in a scratch directory, with
// `touch` of a new file in place of each command, since a removal under
// `sh -c` was not run: `eval -- 'touch f'`, `builtin eval -- 'touch f'`,
// five nested evals and `bash -c` with four ran it in both shells; `eval --
// 'unset X'` left X unset for what ran next in both; zsh ran the four B3
// spellings and the `>>` form (bash ran none of them). Security's round-7
// review ran the rm forms. Not run: GNU parallel, which is not installed
// here; that row rests on its manual.

import (
	"slices"
	"strings"
	"testing"
)

// markers are the names refuseInsideClaudeCode reads to tell a model's
// session from a person's.
const markers = "CLAUDECODE CLAUDE_CODE_SESSION_ID CLAUDE_CODE_CHILD_SESSION CLAUDE_PID AI_AGENT"

// decisionRefused holds a person's command to a refusal under one of rules,
// from the session holding the story and from a session that holds none.
func decisionRefused(t *testing.T, command string, rules ...string) {
	t.Helper()
	refusedUnder(t, command, notReady, rules...)
	f, ok := HumanDecisions(command, false, aStoryWaits)
	switch {
	case !ok:
		t.Errorf("a second session may run %q", command)
	case !slices.Contains(rules, f.Rule):
		t.Errorf("a second session: %q was refused as %s, want %s (%s)", command, f.Rule, strings.Join(rules, " or "), f.Reason)
	}
}

const (
	approveRule  = "approval-is-a-human-decision"
	unfreezeRule = "unfreeze-is-a-human-decision"
	ackRule      = "acknowledgement-is-a-human-decision"
	traceRemove  = "rm -f .sdlc/stories/A-1/trace.jsonl"
)

// The control for B1: without the `--`, each is refused, so a row below
// that is let through was hidden by the `--`.
func TestAC2AC15AC17EvalsStringWithoutTheEndOfOptionsMeetsEveryRule(t *testing.T) {
	commitMeetsTheGate(t, "eval '"+aCommit+"'")
	decisionRefused(t, "eval 'sdlc approve US-1'", approveRule)
	decisionRefused(t, "eval 'sdlc unfreeze --reason x'", unfreezeRule)
	decisionRefused(t, "eval 'sdlc ack --through HEAD'", ackRule)
	refusedUnder(t, "eval '"+rmLock+"'", ready, lockRule)
	refusedUnder(t, "eval '"+traceRemove+"'", ready, traceRule)
}

// Security's round-7 B1: `eval --` runs the string after it.
func TestAC2AC15AC17EvalsStringAfterTheEndOfItsOptionsMeetsEveryRule(t *testing.T) {
	commitMeetsTheGate(t, "eval -- '"+aCommit+"'")
	commitMeetsTheGate(t, "builtin eval -- '"+aCommit+"'")
	decisionRefused(t, "eval -- 'sdlc approve US-1'", approveRule)
	decisionRefused(t, "eval -- 'sdlc unfreeze --reason x'", unfreezeRule)
	decisionRefused(t, "eval -- 'sdlc ack --through HEAD'", ackRule)
	decisionRefused(t, "eval -- 'unset "+markers+"' && eval -- 'sdlc approve US-1'",
		approveRule, "enforcement-stays-on")
	refusedUnder(t, "eval -- '"+rmLock+"'", ready, lockRule)
	refusedUnder(t, "eval -- '"+traceRemove+"'", ready, traceRule)
	refusedUnder(t, `eval parallel --sshdelay 1 eval -- "`+rmLock+`" ::: 1`, ready, lockRule)
}

// The guard for B1: the fix is not to refuse every `eval --`.
func TestAC17AnOrdinaryEvalAfterTheEndOfItsOptionsIsLeftAlone(t *testing.T) {
	for _, command := range []string{"eval -- 'echo hi'", "eval -- 'go test ./...'"} {
		allowedGlob(t, command, notReady)
		leftAloneInEverySession(t, command, false)
	}
}

// Security's round-7 B2: a script nested past what the program walk reads
// is refused, as too deep to read or under the rule it hides.
func TestAC2AC15AC17AScriptNestedPastWhatTheProgramWalkReadsIsRefused(t *testing.T) {
	five := strings.Repeat("eval ", 5)
	commitMeetsTheGate(t, "eval eval eval eval "+aCommit)
	refusedUnder(t, five+aCommit, notReady, "commit-gate", "git-command-cannot-be-read", tooDeepRule)
	refusedUnder(t, "bash -c 'eval eval eval eval "+aCommit+"'", notReady, "commit-gate", "git-command-cannot-be-read", tooDeepRule)
	decisionRefused(t, five+"sdlc unfreeze --reason x", unfreezeRule, tooDeepRule)
	decisionRefused(t, five+"unset "+markers+" && "+five+"sdlc approve US-1",
		approveRule, "enforcement-stays-on", tooDeepRule)
}

// The guard for B2: four nested evals of a harmless command stay allowed.
func TestAC17FourNestedEvalsOfAHarmlessCommandAreLeftAlone(t *testing.T) {
	command := "eval eval eval eval echo hi"
	allowedGlob(t, command, notReady)
	leftAloneInEverySession(t, command, false)
}

// Security's round-7 B3: zsh's `}` and `]]` among the words emulate drops
// before its -c, and mapfile before its -C.
func TestAC17ZshsShortFormsAmongTheWordsEmulateAndMapfileDropMeetThePathRules(t *testing.T) {
	for _, form := range []struct{ name, line string }{
		{"emulate, always", "{ emulate sh } always { %s -c x }"},
		{"emulate, if [[ && ]]", "if [[ -n a && emulate ]] %s -c x"},
		{"mapfile, always", "{ mapfile sh } always { %s -C x }"},
		{"mapfile, if [[ && ]]", "if [[ -n a && mapfile ]] %s -C x"},
	} {
		t.Run("AC-17 "+form.name, func(t *testing.T) {
			refusedUnder(t, strings.Replace(form.line, "%s", "rm -- .sdlc/state/tests.lock", 1), ready, lockRule)
			refusedUnder(t, strings.Replace(form.line, "%s", "rm -f -- .sdlc/stories/A-1/trace.jsonl", 1), ready, traceRule)
			refusedUnder(t, strings.Replace(form.line, "%s", "echo y >> .sdlc/stories/A-1/trace.jsonl", 1), ready, traceRule)
		})
	}
}

// Architect's round-7 note 1: movesInAScript fails closed on a line the
// program walk stopped reading, and stays false on one it read through.
func TestAC17MovesInAScriptAnswersTrueForALineThatWasNotReadThrough(t *testing.T) {
	for name, line := range map[string]string{
		"past the readings count": strings.Repeat(forkOnce, 17),
		"past the nesting depth":  strings.Repeat("eval ", 5) + "bash -c 'cd /tmp; rm x'",
	} {
		if !movesInAScript(line, false) {
			t.Errorf("AC-17 %s: movesInAScript(%q) = false, want true", name, line)
		}
	}
	if movesInAScript("echo hi", false) {
		t.Error("AC-17 movesInAScript(\"echo hi\") = true, want false")
	}
}
