// A word xargs is fed from elsewhere on the line, in the sandbox: put to the
// built hook through the plugin launcher, from the story's worktree, in the
// session holding the story and in another session of the project. Read
// without its `~` or its `$`, `echo ~/.claude/settings.json | xargs rm` took
// the person's configuration away from either session, and `echo
// .sd''lc/state/active | xargs rm` the loop's record from the story's.
// internal/shellpolicy holds the table of spellings; these are the binary's own
// answers to a few of them.

package e2e

import (
	"strings"
	"testing"
)

// handedRule is the rule the hook refuses a Bash command of session, run from
// dir, under, or "" where it lets it through.
func (sb sandbox) handedRule(t *testing.T, session, dir, command string) string {
	t.Helper()
	input := map[string]any{"command": command}
	stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(session, dir, "", "Bash", input))
	if code != 0 {
		t.Fatalf("the hook exited %d for %s, so its reply is never read:\n%s", code, describeInput("Bash", input), stderr)
	}
	if stdout == carryOn {
		return ""
	}
	var r refusal
	oneLine(t, stdout, &r)
	reason := r.HookSpecificOutput.PermissionDecisionReason
	if r.HookSpecificOutput.PermissionDecision != "deny" || !strings.Contains(reason, "Instead: ") {
		t.Fatalf("the hook's reply to %s is neither a refusal with a route nor a pass: %q", describeInput("Bash", input), stdout)
	}
	open := strings.LastIndex(reason, "[")
	if open < 0 || !strings.HasSuffix(reason, "]") {
		t.Fatalf("the refusal of %s names no rule: %q", describeInput("Bash", input), reason)
	}
	return reason[open+1 : len(reason)-1]
}

func TestAWordXargsIsFedIsReadAsTheShellHandsItOverInTheSandbox(t *testing.T) {
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	if t.Failed() {
		t.Fatal("the story was not started, so nothing after this can be run")
	}
	const other = "a-session-that-holds-no-story"
	for _, row := range []struct {
		command, story, other string
	}{
		{"echo ~/.claude/settings.json | xargs rm", "claude-config-is-the-persons", "claude-config-is-the-persons"},
		{"echo $HOME/.claude/settings.json | xargs rm -f", "protected-path-through-the-tool", "claude-config-is-the-persons"},
		{"echo ~/.claude | xargs -I{} cp -r /tmp/evil/. {}", "claude-config-is-the-persons", "claude-config-is-the-persons"},
		{`echo "$HOME"/.agents | xargs rm -rf`, "agents-config-is-the-persons", "agents-config-is-the-persons"},
		{"echo .sd''lc/state/active | xargs rm", "loop-state-through-the-tool", ""},
		{"echo ~/notes.txt | xargs rm", "", ""},
	} {
		if got := sb.handedRule(t, sandboxSession, tree, row.command); got != row.story {
			t.Errorf("in the story's session, %s: %q; want %q", row.command, got, row.story)
		}
		if got := sb.handedRule(t, other, tree, row.command); got != row.other {
			t.Errorf("in another session, %s: %q; want %q", row.command, got, row.other)
		}
	}
}
