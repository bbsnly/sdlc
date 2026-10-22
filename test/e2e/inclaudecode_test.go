package e2e

// US-108 AC-15 and AC-17, end to end, with Claude Code's markers set
// explicitly in each child's environment (the sandbox's own carries none).
//
// AC-15: the built binary refuses a person's command run with any of the
// markers, as from a model's Bash, from anything it starts, or from a `!`
// command typed in a session, and the story goes on waiting. A slash command
// typed through the plugin's launcher, whose hook carries the markers a real
// hook does, still makes the decision. AC-17: the same line expanded in a
// session started from another session's tool call, whose hook inherits
// AI_AGENT, records nothing.
//
// parallel is left to internal/cli, whose fake claude it starts: here it
// would find the developer's own on PATH.

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// inClaudeCodeCode is AC-15's refusal.
const inClaudeCodeCode = "SDLC-E0079"

// bashMarkers is what Claude Code 2.1.302 sets for a model's Bash tool
// (ANALYSIS 7.1), the session id aside.
var bashMarkers = []string{
	"CLAUDECODE=1",
	"CLAUDE_CODE_CHILD_SESSION=1",
	"CLAUDE_CODE_SESSION_ATTENDED=1",
	"CLAUDE_CODE_ENTRYPOINT=cli",
	"CLAUDE_PID=4242",
	"AI_AGENT=claude-code_2-1-302_agent",
}

// realHookMarkers is what it sets for a hook: the same, but AI_AGENT, which is
// a model's tools' alone.
var realHookMarkers = []string{
	"CLAUDECODE=1",
	"CLAUDE_CODE_CHILD_SESSION=1",
	"CLAUDE_CODE_SESSION_ATTENDED=1",
	"CLAUDE_CODE_ENTRYPOINT=claude-desktop",
	"CLAUDE_PID=4242",
}

// insideClaudeCode runs the sandbox's sdlc in dir in session, with markers
// added to its environment.
func (sb sandbox) insideClaudeCode(t *testing.T, session, dir string, markers []string, args ...string) result {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), sb.sdlc, args...)
	cmd.Dir = dir
	cmd.Env = append(sb.env(t, session), markers...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	code := 0
	if err := cmd.Run(); err != nil {
		exit, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			t.Fatalf("sdlc %s could not be run: %v", strings.Join(args, " "), err)
		}
		code = exit.ExitCode()
	}
	return result{out.String(), errOut.String(), code}
}

// expandsInClaudeCode is expands with markers added to the hook's environment.
func (sb sandbox) expandsInClaudeCode(t *testing.T, session, dir string, markers []string, payload map[string]any) expanded {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	name, args := sb.sdlc, []string{"hook", "UserPromptExpansion"}
	if runtime.GOOS != "windows" {
		name, args = filepath.Join(moduleRoot(t), "plugin", "bin", "sdlc-hook"), []string{"UserPromptExpansion"}
	}
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = dir
	cmd.Env = append(append(sb.env(t, session), "CLAUDE_PROJECT_DIR="+sb.repo), markers...)
	cmd.Stdin = bytes.NewReader(raw)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("the hook did not answer UserPromptExpansion (%v):\n%s", err, errOut.String())
	}
	var e expanded
	dec := json.NewDecoder(strings.NewReader(out.String()))
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("the hook's stdout is not one JSON object: %v\n%q", err, out.String())
	}
	if rest := strings.TrimSpace(out.String()[dec.InputOffset():]); rest != "" {
		t.Fatalf("the hook printed more than one JSON object: %q", out.String())
	}
	e.raw = out.String()
	return e
}

// loopRecord is what a person's command could change: the loop's files, the
// story's worktree with them, and the claims in git's directory.
func loopRecord(t *testing.T, repo string) map[string]string {
	t.Helper()
	out := files(t, filepath.Join(repo, ".sdlc"))
	for k, v := range files(t, filepath.Join(repo, ".git", "sdlc")) {
		out[".git/sdlc/"+k] = v
	}
	out["<backlog>"] = files(t, repo)["user_stories.json"]
	return out
}

func TestAC15APersonsCommandRunWithClaudeCodesMarkersIsRefusedByTheBinaryAndTheSlashCommandStillDecides(t *testing.T) {
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	sb.works(t, tree, "", "", "escalate", "spec_unclear", "--message", "is a zero total refused too?")
	if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
		t.Fatalf("the story is %q after the escalation, want awaiting_human", got["US-001"])
	}
	before := loopRecord(t, main)

	t.Run("AC-15 approve with each marker, and with a model's Bash's", func(t *testing.T) {
		rows := map[string]struct {
			session string
			markers []string
		}{
			"a model's Bash": {sandboxSession, bashMarkers},
			// The five markers ANALYSIS constraint 29 names, each alone; the
			// session id is the sandbox's own way of setting it.
			"the session id alone":            {sandboxOther, nil},
			"CLAUDECODE alone":                {"", []string{"CLAUDECODE=1"}},
			"CLAUDE_CODE_CHILD_SESSION alone": {"", []string{"CLAUDE_CODE_CHILD_SESSION=1"}},
			"CLAUDE_PID alone":                {"", []string{"CLAUDE_PID=4242"}},
			"AI_AGENT alone":                  {"", []string{"AI_AGENT=claude-code_2-1-302_agent"}},
		}
		for name, row := range rows {
			r := sb.insideClaudeCode(t, row.session, tree, row.markers, "approve", "US-001")
			if r.code == 0 || !strings.Contains(r.stderr, inClaudeCodeCode) {
				t.Errorf("sdlc approve with %s: exit %d, want %s\n%s%s", name, r.code, inClaudeCodeCode, r.stdout, r.stderr)
			}
			if fix := fixLine(r.stderr); !strings.Contains(fix, "/sdlc:approve") || !strings.Contains(fix, "outside Claude Code") {
				t.Errorf("sdlc approve with %s: the fix names neither /sdlc:approve nor a terminal outside Claude Code:\n%s", name, r.stderr)
			}
		}
	})

	t.Run("AC-15 each other person's command with CLAUDECODE alone", func(t *testing.T) {
		for _, args := range [][]string{
			{"unfreeze", "--reason", "the test was wrong"},
			{"ack", "--through", "HEAD"},
			{"remap", "US-001"},
			{"release", "US-001", "--reason", "moved to a new session"},
			{"staffing", "assign", "security", "--model", "opus", "--reason", "mine"},
		} {
			r := sb.insideClaudeCode(t, "", main, []string{"CLAUDECODE=1"}, args...)
			if r.code == 0 || !strings.Contains(r.stderr, inClaudeCodeCode) {
				t.Errorf("sdlc %s with CLAUDECODE=1: exit %d, want %s\n%s%s",
					strings.Join(args, " "), r.code, inClaudeCodeCode, r.stdout, r.stderr)
			}
		}
	})

	if !maps.Equal(before, loopRecord(t, main)) {
		t.Fatal("a person's command refused inside Claude Code changed the record")
	}

	t.Run("AC-17 /sdlc:approve in a session started from a model's Bash records nothing", func(t *testing.T) {
		e := sb.expandsInClaudeCode(t, sandboxOther, tree, append(append([]string{}, realHookMarkers...), "AI_AGENT=claude-code_2-1-302_agent"),
			typedLine(sandboxOther, tree, "sdlc:approve", "US-001"))
		if !e.blocked() || !strings.Contains(e.shown(), "/sdlc:approve did not run") {
			t.Errorf("the person is not told that /sdlc:approve did not run: %q", e.raw)
		}
		if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
			t.Fatalf("a nested session's /sdlc:approve answered the escalation: the story is %q", got["US-001"])
		}
		if !maps.Equal(before, loopRecord(t, main)) {
			t.Error("a nested session's /sdlc:approve changed the record")
		}
	})

	t.Run("AC-15 /sdlc:approve typed, with the markers a real hook carries, decides", func(t *testing.T) {
		e := sb.expandsInClaudeCode(t, sandboxOther, tree, realHookMarkers, typedLine(sandboxOther, tree, "sdlc:approve", "US-001"))
		sb.showsWhatTheTerminalPrints(t, e,
			"US-001  approved (spec_unclear)\n\nA person runs `sdlc start US-001`, or types /sdlc:next in Claude Code, to carry on.\n")
		if got := storiesOf(t, main); got["US-001"] != "in_progress" {
			t.Fatalf("the story is %q after /sdlc:approve, want in_progress", got["US-001"])
		}
	})

	// AC-8: the terminal route, outside Claude Code, still works.
	if r := sb.person(t, main, "ack", "--through", "HEAD"); r.code != 0 {
		t.Errorf("sdlc ack from a terminal outside Claude Code: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
}
