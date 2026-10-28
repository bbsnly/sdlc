package hook

// US-108 AC-6 and AC-7 through the hook, the rows the researcher reproduced in
// ANALYSIS.md section 2.8, with HOME pointed at a temp dir so the person's
// Claude config has a known place. The unit tests in internal/policy and
// internal/shellpolicy hold the rules; these hold the hook wiring that reads
// HOME, decides whether the project takes part, and reaches those rules from a
// session that holds the story and from a second session of the same project.
//
// Only the reproduced rows are here. Decision 1 on the story makes the refusal
// reach every session in a participating project; a project that does not take
// part keeps the writes, which is the control each case ends with.

import (
	"path/filepath"
	"testing"
)

// claudeHome points HOME (and USERPROFILE, for Windows) at a temp dir and
// returns the person's Claude config directory inside it.
func claudeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return filepath.Join(home, ".claude")
}

// claudeConfigTargets are the kinds of file the criterion names, each a path
// under the person's Claude config directory.
func claudeConfigTargets(config string) map[string]string {
	return map[string]string{
		"settings":       filepath.Join(config, "settings.json"),
		"settings.local": filepath.Join(config, "settings.local.json"),
		"skills":         filepath.Join(config, "skills", "x", "SKILL.md"),
		"agents":         filepath.Join(config, "agents", "reviewer.md"),
		"commands":       filepath.Join(config, "commands", "approve.md"),
		"hooks":          filepath.Join(config, "hooks", "hooks.json"),
		"plugins":        filepath.Join(config, "plugins", "sdlc", "bin", "sdlc-hook"),
	}
}

// AC-7: a file-tool write to the person's Claude config is refused, from the
// session holding the story and from a second session of the same project.
func TestAC7AFileWriteToThePersonsClaudeConfigIsRefused(t *testing.T) {
	config := claudeHome(t)
	root := loopProject(t)
	for kind, path := range claudeConfigTargets(config) {
		for _, tool := range []string{"Write", "Edit", "NotebookEdit"} {
			for _, session := range []string{working, "session-b"} {
				if !denied(call(t, inSession(event(root, tool, "", path), session), noEnv)) {
					t.Errorf("%s to the %s file of the person's config, from session %q, went through", tool, kind, session)
				}
			}
		}
	}
	// A project that does not take part keeps the write.
	plain := t.TempDir()
	if !allows(call(t, event(plain, "Write", "", filepath.Join(config, "settings.json")), noEnv)) {
		t.Error("a write to the person's config was refused in a project that does not take part")
	}
}

// AC-7: a shell write to the person's Claude config is refused, from both
// sessions, and kept in a project that does not take part.
func TestAC7AShellWriteToThePersonsClaudeConfigIsRefused(t *testing.T) {
	config := claudeHome(t)
	root := loopProject(t)
	commands := []string{
		"echo x > ~/.claude/settings.json",
		"echo x > " + filepath.Join(config, "settings.json"),
		"cp /etc/hosts ~/.claude/skills/x/SKILL.md",
		"tee -a ~/.claude/plugins/x.json < /etc/hosts",
	}
	for _, cmd := range commands {
		for _, session := range []string{working, "session-b"} {
			if !denied(call(t, inSession(command(root, "", cmd), session), noEnv)) {
				t.Errorf("from session %q, the shell wrote the person's config: %s", session, cmd)
			}
		}
	}
	plain := t.TempDir()
	if !allows(call(t, command(plain, "", commands[0]), noEnv)) {
		t.Error("a shell write to the person's config was refused in a project that does not take part")
	}
}

// AC-6: a command that starts a Claude Code session which would expand one of
// a person's slash commands is refused, from both sessions, and kept in a
// project that does not take part.
func TestAC6ANestedSessionThatWouldExpandAPersonsCommandIsRefusedThroughTheHook(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	commands := []string{
		`claude -p "/sdlc:approve US-001"`,
		`echo /sdlc:unfreeze --reason x | claude -p`,
		`npx @anthropic-ai/claude-code -p "/sdlc:ack --through HEAD"`,
	}
	for _, cmd := range commands {
		for _, session := range []string{working, "session-b"} {
			if !denied(call(t, inSession(command(root, "", cmd), session), noEnv)) {
				t.Errorf("from session %q, a nested session that would expand a person's command went through: %s", session, cmd)
			}
		}
	}
	plain := t.TempDir()
	if !allows(call(t, command(plain, "", commands[0]), noEnv)) {
		t.Error("a nested session was refused in a project that does not take part")
	}
}

// allows reports a reply that let the call through with nothing said.
func allows(r reply) bool {
	return !denied(r) && r.SystemMessage == ""
}
