// US-079 in the sandbox: a find with operators between its tests, put to the
// built hook as the Bash call Claude Code would send, in a story's worktree
// with a frozen test and the loop's record in it. internal/shellpolicy holds
// the table of forms; these are the hook's own answers to a few of them.

package e2e

import (
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// refusesUnderOneOf puts a Bash command of the session under test to the hook,
// which has to refuse it, with a route, under one of the rules given: from the
// worktree's own directory a find reaches the frozen test and the loop's files
// both, and which the hook names first is its own to say.
func (sb sandbox) refusesUnderOneOf(t *testing.T, dir, command string, rules ...string) string {
	t.Helper()
	input := map[string]any{"command": command}
	stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, dir, "", "Bash", input))
	if code != 0 {
		t.Fatalf("the hook exited %d for %s, so its reply is never read:\n%s", code, describeInput("Bash", input), stderr)
	}
	if stdout == carryOn {
		t.Errorf("the hook let the session run %s", describeInput("Bash", input))
		return ""
	}
	var r refusal
	oneLine(t, stdout, &r)
	out := r.HookSpecificOutput
	if out.HookEventName != "PreToolUse" || out.PermissionDecision != "deny" {
		t.Fatalf("the hook's reply to %s is not a refusal of a tool call: %q", describeInput("Bash", input), stdout)
	}
	named := false
	for _, rule := range rules {
		named = named || strings.Contains(out.PermissionDecisionReason, "["+rule+"]")
	}
	if !named {
		t.Errorf("%s was refused under another rule than %s:\n%s", describeInput("Bash", input),
			strings.Join(rules, " or "), out.PermissionDecisionReason)
	}
	if !strings.Contains(out.PermissionDecisionReason, "Instead:") {
		t.Errorf("the refusal of %s offers no route:\n%s", describeInput("Bash", input), out.PermissionDecisionReason)
	}
	return out.PermissionDecisionReason
}

func TestUS079AFindIsJudgedByWhatItsOperatorsPickInTheSandbox(t *testing.T) {
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GatePlan); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}
	if t.Failed() {
		t.Fatal("the story's tests were not frozen, so nothing after this can be run")
	}
	const state, frozen, protected = "loop-state-through-the-tool", "frozen-test-through-the-tool", "protected-path-through-the-tool"

	t.Run("AC-1 a find that deletes what a negated name test leaves in is refused", func(t *testing.T) {
		sb.refusesUnderOneOf(t, tree, "find . ! -name '*.orig' -delete", state, frozen, protected)
		sb.refusesUnderOneOf(t, tree, "find . -type f -not -name '*.orig' -exec rm {} +", state, frozen, protected)
	})
	t.Run("AC-2 a find with an alternative that picks every file is refused", func(t *testing.T) {
		sb.refusesUnderOneOf(t, tree, "find . -name '*.orig' -o -type f -delete", state, frozen, protected)
	})
	t.Run("AC-3 a find with a word the shell fills in between its tests is refused and told what to do", func(t *testing.T) {
		reason := sb.refusesUnderOneOf(t, tree, `find . -name '*.orig' "$OP" -name '*.bak' -delete`, state, frozen)
		if reason != "" && !strings.Contains(reason, "$OP") {
			t.Errorf("the refusal does not name the word the hook could not read:\n%s", reason)
		}
	})
	t.Run("AC-1 a find handed to a shell as a script is refused for what its operators pick", func(t *testing.T) {
		sb.refusesUnderOneOf(t, tree, `sh -c "find . ! -name '*.orig' -delete"`, state, frozen, protected)
		sb.refusesUnderOneOf(t, tree, `bash -c 'find . -name "*.orig" -o -type f -delete'`, state, frozen, protected)
	})
	t.Run("AC-1 a separator inside a quoted word does not end the find", func(t *testing.T) {
		sb.refusesUnderOneOf(t, tree, "find . ! -name 'x;y' -delete", state, frozen, protected)
	})
	t.Run("AC-3 a find the hook cannot follow is judged to the end of the command past a separator in a quoted word", func(t *testing.T) {
		sb.refusesUnderOneOf(t, tree, "find . -name 'a;b' -delete # don't", state, frozen)
		sb.refusesUnderOneOf(t, tree, `eval "find . -name 'a|b'" -delete`, state, frozen)
	})
	t.Run("AC-4 a find with a descriptor redirect that picks nothing protected is let through", func(t *testing.T) {
		for _, command := range []string{
			"find . -name '*.orig' -delete 2>/dev/null",
			"find . -type f -name '*.pyc' -delete 2>/dev/null || true",
			"find . -name '*.orig' ! -name keep.orig -delete 2>&1",
		} {
			sb.allows(t, tree, "", "Bash", map[string]any{"command": command})
		}
	})
	t.Run("AC-4 a find whose operators pick nothing protected is let through", func(t *testing.T) {
		for _, command := range []string{
			"find . ! -name '*.orig' -print",
			"find . -name '*.orig' ! -name keep.orig -delete",
			"find . -name '*.orig' -o -name '*.bak' -delete",
			`find . \( -name '*.orig' -o -name '*.bak' \) -delete`,
			"find . -name node_modules -prune -o -name '*.pyc' -delete",
		} {
			sb.allows(t, tree, "", "Bash", map[string]any{"command": command})
		}
	})
}
