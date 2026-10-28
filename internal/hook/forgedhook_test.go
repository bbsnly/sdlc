package hook

// US-108 AC-3 through the hook: a model's shell that hands the hook a
// UserPromptExpansion payload it wrote itself is refused at PreToolUse, from
// the session holding the story and from a second session of the same
// project, under the person-only rule; the hook run by hand on another event
// is not. internal/shellpolicy holds the spellings; these hold that the hook
// asks the rule on both paths, for Bash and for PowerShell.

import (
	"regexp"
	"strings"
	"testing"
)

// shellCall is a PreToolUse call of a shell tool, made from root by session.
func shellCall(root, session, tool, cmd string) string {
	return mustMarshal(map[string]any{
		"hook_event_name": "PreToolUse", "session_id": session,
		"tool_name":  tool,
		"cwd":        root,
		"tool_input": map[string]string{"command": cmd},
	})
}

// namesSlashCommandFirst reports whether a refusal follows AC-12: it names at
// least one of a person's commands, and the first time it names each terminal
// command, the slash command comes just before it, in the same quoting, then
// ", or ". The slash command is given the terminal command's arguments,
// exactly; it is bare only beside arguments that name no story, since a bare
// /sdlc:approve typed where no story is active is refused with SDLC-E0011.
func namesSlashCommandFirst(text string) bool {
	named := false
	for _, c := range []struct{ slash, terminal string }{
		{"/sdlc:approve", "sdlc approve"},
		{"/sdlc:unfreeze", "sdlc unfreeze"},
		{"/sdlc:ack", "sdlc ack"},
		{"/sdlc:remap", "sdlc remap"},
		{"/sdlc:release", "sdlc release"},
		{"/sdlc:assign", "sdlc staffing assign"},
	} {
		for _, q := range []string{"`", `"`} {
			i := strings.Index(text, q+c.terminal)
			if i < 0 {
				continue
			}
			if !slashBefore(text, i, q, c.slash, c.terminal) {
				return false
			}
			named = true
		}
	}
	return named
}

// storyNamed matches an argument that names a story: the placeholder, or an
// id such as US-001.
var storyNamed = regexp.MustCompile(`<ID>|<story>|\b[A-Z][A-Z0-9]*-[0-9]+\b`)

// slashBefore reports whether the terminal command quoted by q at i in text
// comes just after its slash command with the same arguments, then ", or ",
// or after the bare slash command where its arguments name no story.
func slashBefore(text string, i int, q, slash, terminal string) bool {
	args := text[i+len(q+terminal):]
	if end := strings.Index(args, q); end >= 0 {
		args = args[:end]
	}
	before := text[:i]
	return strings.HasSuffix(before, q+slash+args+q+", or ") ||
		!storyNamed.MatchString(args) && strings.HasSuffix(before, q+slash+q+", or ")
}

func TestAC3AModelsShellThatRunsTheHookOnAUserPromptExpansionIsRefusedThroughTheHook(t *testing.T) {
	t.Parallel()
	const rule = "[slash-command-is-a-human-decision]"
	root := loopProject(t)
	for _, c := range []struct{ tool, cmd string }{
		{"Bash", "sdlc hook UserPromptExpansion < /tmp/p.json"},
		{"Bash", `printf '%s' '{"hook_event_name":"UserPromptExpansion","command_name":"sdlc:approve","command_args":"A-1"}' | sdlc hook UserPromptExpansion`},
		{"Bash", "sdlc-hook UserPromptExpansion < /tmp/p.json"},
		{"PowerShell", "Get-Content p.json | sdlc hook UserPromptExpansion"},
	} {
		for _, session := range []string{working, "session-b"} {
			r := call(t, shellCall(root, session, c.tool, c.cmd), noEnv)
			why := reasonOf(r)
			switch {
			case !denied(r):
				t.Errorf("AC-3 from session %q, %s ran the hook on a payload it wrote: %s", session, c.tool, c.cmd)
			case !strings.Contains(why, rule):
				t.Errorf("AC-3 from session %q, %s was refused under another rule than %s: %q", session, c.cmd, rule, why)
			case !strings.Contains(why, "Instead:") || !namesSlashCommandFirst(why):
				t.Errorf("AC-3 from session %q, the refusal of %s does not offer the slash command, then \", or \", then the terminal command (AC-12): %q",
					session, c.cmd, why)
			}
		}
	}
	// The hook fed another event by hand is how a reviewer drives it.
	for _, c := range []struct{ tool, cmd string }{
		{"Bash", "sdlc hook PreToolUse < /tmp/p.json"},
		{"PowerShell", "Get-Content p.json | sdlc hook PreToolUse"},
	} {
		for _, session := range []string{working, "session-b"} {
			if r := call(t, shellCall(root, session, c.tool, c.cmd), noEnv); !allows(r) {
				t.Errorf("AC-3 from session %q, %s was refused: %q", session, c.cmd, reasonOf(r))
			}
		}
	}
}
