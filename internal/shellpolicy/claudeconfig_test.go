package shellpolicy

// US-108 AC-6 and AC-7, the shell rows the researcher reproduced in
// ANALYSIS.md section 2.8. Two behaviours this package decides:
//
//   - AC-6: a command that starts a Claude Code session of its own, in a way
//     that would expand one of a person's slash commands, is refused as
//     story-stays-in-its-session. Inspect already refuses it for the session
//     holding the story; HumanDecisions is the reading for a second session of
//     the same project, and must refuse it there too.
//   - AC-7: a command that writes under the person's Claude Code configuration
//     (~/.claude, or the same path spelled absolutely) is refused as
//     claude-config-is-the-persons. The caller says which words are under that
//     directory through State.ClaudeConfig, because only it knows where the
//     home is.
//
// Only the reproduced rows are here. Further spellings are red-team's and
// security's to name at Gate 4.

import "testing"

// claudeConfigState is ready with the project's own paths resolved, the user's
// home outside it, and the words under ~/.claude named for the caller.
func claudeConfigState() State {
	const home = "/home/alice"
	under := func(word string) bool {
		c := clean(word)
		return c == "~/.claude" || hasDir(c, "~/.claude") ||
			c == home+"/.claude" || hasDir(c, home+"/.claude")
	}
	s := ready
	s.Resolve = func(word string) string {
		c := clean(word)
		if isAbsolute(c) || c == "~" || hasTilde(c) {
			return ""
		}
		return c
	}
	s.ClaudeConfig = under
	return s
}

// hasDir reports whether path is dir or a path inside it.
func hasDir(path, dir string) bool {
	return len(path) > len(dir) && path[:len(dir)] == dir && path[len(dir)] == '/'
}

// hasTilde reports whether a word starts at the user's home, ~ followed by a
// separator.
func hasTilde(word string) bool {
	return len(word) > 1 && word[0] == '~' && (word[1] == '/' || word[1] == '\\')
}

// AC-6: the session holding the story is refused a nested session, as it is
// today, and so is a second session of the same project, which reads through
// HumanDecisions.
func TestAC6ANestedSessionThatWouldExpandAPersonsCommandIsRefused(t *testing.T) {
	const rule = "story-stays-in-its-session"
	for _, command := range []string{
		`claude -p "/sdlc:approve US-001"`,
		`echo /sdlc:unfreeze --reason x | claude -p`,
		`npx @anthropic-ai/claude-code -p "/sdlc:ack --through HEAD"`,
	} {
		refused(t, command, ready, rule)
		f, ok := HumanDecisions(command, false, func(string) bool { return false })
		if !ok || f.Rule != rule {
			t.Errorf("HumanDecisions(%q) = %+v, %v; want it refused as %s to a second session", command, f, ok, rule)
		}
		if ok && f.Route == "" {
			t.Errorf("the refusal of %q says nothing to do next", command)
		}
	}
}

// AC-7: a command that writes under the person's Claude config is refused.
func TestAC7AShellWriteUnderThePersonsClaudeConfigIsRefused(t *testing.T) {
	const rule = "claude-config-is-the-persons"
	s := claudeConfigState()
	for _, command := range []string{
		"echo x > ~/.claude/settings.json",
		"echo x > /home/alice/.claude/settings.json",
		"cp /etc/hosts ~/.claude/skills/x/SKILL.md",
		"tee -a ~/.claude/plugins/x.json < /etc/hosts",
	} {
		f := refused(t, command, s, rule)
		if f.Route == "" {
			t.Errorf("the refusal of %q says nothing to do next", command)
		}
	}
	// A write to the project's own files is untouched, so the rule has not
	// started refusing every write.
	allowed(t, "echo x > internal/app.go", s)
}
