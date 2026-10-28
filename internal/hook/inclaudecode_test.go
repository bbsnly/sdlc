package hook

// US-108 AC-15 and AC-17 on the slash route.
//
// AC-15: the CLI refuses a person's command in a process that carries Claude
// Code's markers, and the hook carries them too, since Claude Code gives a
// hook the same builder as every other child. A typed slash command runs the
// CLI in the hook's process, so the hook hands it an environment with none of
// them, as it already hands it none of the session's id, and the command
// records as AC-1 says.
//
// AC-17: a session started from another session's tool call inherits that
// tool call's AI_AGENT, and Claude Code sets it for nothing but a model's tool
// (ANALYSIS 7.1 and 7.4, the person's decision 3). So a hook that finds
// AI_AGENT in its own environment is either such a session's, or a model's
// shell running the hook on a payload it wrote (T-12); either way no person
// typed the line, and nothing is recorded.

import (
	"io"
	"maps"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/cli"
)

// hookMarkers is what Claude Code 2.1.302's builder gives every hook besides
// the session id and the project directory (ANALYSIS 7.1), with the plugin's
// own two. AI_AGENT is not among them: Claude Code sets it for a model's tools.
var hookMarkers = map[string]string{
	"CLAUDECODE":                   "1",
	"CLAUDE_CODE_CHILD_SESSION":    "1",
	"CLAUDE_CODE_SESSION_ATTENDED": "1",
	"CLAUDE_PID":                   "4242",
	"CLAUDE_CODE_ENTRYPOINT":       "claude-desktop",
	"CLAUDE_CODE_EXECPATH":         "/opt/claude/versions/2.1.302",
	"CLAUDE_PLUGIN_ROOT":           "/opt/claude/plugins/cache/sdlc/sdlc/0.5.0",
	"CLAUDE_PLUGIN_DATA":           "/opt/claude/plugins/data/sdlc",
}

// aiAgent is the value Claude Code gives AI_AGENT, in its shape.
const aiAgent = "claude-code_2-1-302_agent"

// realHookEnv is the environment Claude Code gives a hook in session, with
// extra on top of it.
func realHookEnv(root, session string, extra map[string]string) func(string) string {
	pairs := map[string]string{"CLAUDE_CODE_SESSION_ID": session, "CLAUDE_PROJECT_DIR": root}
	maps.Copy(pairs, hookMarkers)
	maps.Copy(pairs, extra)
	return env(pairs)
}

// AC-15 with AC-1: with every marker a real hook carries, each command typed
// still makes the terminal's change and shows what the terminal printed. A
// slash route that handed the CLI the hook's own environment would be refused
// with SDLC-E0079 here, where a terminal outside Claude Code is not.
func TestAC15EachPersonsSlashCommandStillRecordsWithTheMarkersARealHookCarries(t *testing.T) {
	t.Parallel()
	for _, c := range personsCases() {
		t.Run(strings.Replace(c.name, "AC-1", "AC-15", 1), func(t *testing.T) {
			byHand := c.project(t)
			stdout, stderr, code := terminal(t, byHand, c.argv...)
			after := alikeFiles(files(t, byHand), byHand)

			typedIn := c.project(t)
			r := expand(t, typed(typedIn, "session-b", c.slash, c.args), realHookEnv(typedIn, "session-b", nil), cli.ExecuteIn)
			if !r.blocked() {
				t.Fatalf("the expansion went on to the model after the hook handled it: %+v", r)
			}
			shown := alike(r.shown(), typedIn)
			if strings.Contains(shown, "SDLC-E0079") {
				t.Fatalf("/%s %s was refused as run inside Claude Code: the slash route handed the CLI the hook's markers:\n%s",
					c.slash, c.args, shown)
			}
			if diff := sameFiles(after, alikeFiles(files(t, typedIn), typedIn)); len(diff) > 0 {
				t.Errorf("/%s %s left the record unlike `sdlc %s` did:\n%s",
					c.slash, c.args, strings.Join(c.argv, " "), strings.Join(diff, "\n"))
			}
			for name, want := range map[string]string{"stdout": stdout, "stderr": stderr} {
				if want = strings.TrimSpace(alike(want, byHand)); want != "" && !strings.Contains(shown, want) {
					t.Errorf("the person was not shown the terminal's %s.\nshown: %q\nwant:  %q", name, shown, want)
				}
			}
			if code != 0 && !strings.Contains(shown, "SDLC-E") {
				t.Errorf("the terminal refused with an SDLC-E code and the person was shown none: %q", shown)
			}
		})
	}
}

// AC-15: the command a slash command runs sees none of Claude Code's markers,
// so the CLI's own check, which reads through the getenv it is handed, finds a
// terminal's environment. What else the hook had (the project's directory,
// the plugin's root) is not the marker check's to take away.
func TestAC15TheCommandASlashCommandRunsIsHandedNoneOfClaudeCodesMarkers(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	seen := map[string]string{}
	calls := 0
	recorder := func(_ string, getenv func(string) string, _ []string, _ io.Reader, stdout, _ io.Writer) int {
		calls++
		for _, name := range []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_PID", "AI_AGENT"} {
			if v := getenv(name); v != "" {
				seen[name] = v
			}
		}
		_, _ = io.WriteString(stdout, "US-001  approved (spec_unclear)\n")
		return 0
	}
	expand(t, typed(root, working, "sdlc:approve", "A-1"), realHookEnv(root, working, nil), recorder)
	if calls != 1 {
		t.Fatalf("the command was run %d times, want once", calls)
	}
	for name, v := range seen {
		t.Errorf("the command was handed %s=%s, which a terminal outside Claude Code does not have", name, v)
	}
}

// AC-17: a person's slash command expanded where the hook carries AI_AGENT
// records nothing, and the person is told that it did not run and why, the
// slash command named first (AC-12). The rows with the other markers are a
// session started from a model's Bash; the one with AI_AGENT alone is a
// model's shell that runs the hook itself.
func TestAC17APersonsSlashCommandExpandedInASessionStartedFromAToolCallRecordsNothingAndSaysWhy(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	for _, c := range []struct{ name, slash, args string }{
		{"approve", "sdlc:approve", "A-1"},
		{"unfreeze", "sdlc:unfreeze", `--reason "x"`},
		{"ack", "sdlc:ack", "--through HEAD"},
		{"remap", "sdlc:remap", "A-1"},
		{"release", "sdlc:release", `A-1 --reason "x"`},
		{"assign", "sdlc:assign", `security --model opus --reason "mine"`},
	} {
		for envName, getenv := range map[string]func(string) string{
			"a nested session's hook":       realHookEnv(root, "session-nested", map[string]string{"AI_AGENT": aiAgent}),
			"a model's shell with AI_AGENT": env(map[string]string{"AI_AGENT": aiAgent, "CLAUDE_PROJECT_DIR": root}),
		} {
			t.Run("AC-17 /"+c.slash+" from "+envName, func(t *testing.T) {
				calls := 0
				recorder := func(string, func(string) string, []string, io.Reader, io.Writer, io.Writer) int {
					calls++
					return 0
				}
				r := expand(t, typed(root, "session-nested", c.slash, c.args), getenv, recorder)
				if calls != 0 {
					t.Fatalf("/%s was run %d times from %s", c.slash, calls, envName)
				}
				if !r.blocked() {
					t.Errorf("the expansion went on to the model, whose skill body is all it reads: %+v", r)
				}
				shown := r.shown()
				slash := "/" + c.slash
				if !strings.Contains(shown, slash+" did not run") {
					t.Errorf("the person is not told that %s did not run:\n%s", slash, shown)
				}
				if !strings.Contains(shown, "another Claude Code session") {
					t.Errorf("the person is not told why: the session was started from another Claude Code session's tool call:\n%s", shown)
				}
				if terminalAt := strings.Index(shown, terminalForm(c.name)); terminalAt >= 0 && terminalAt < strings.Index(shown, slash) {
					t.Errorf("the terminal command comes before %s (AC-12):\n%s", slash, shown)
				}
				if ctx := r.HookSpecificOutput.AdditionalContext; ctx != "" {
					t.Errorf("the hook put text into the model's context: %q", ctx)
				}
			})
		}
	}
}

// AC-17 against a real record: /sdlc:approve in a nested session leaves the
// story waiting, where the same line typed in the person's own session answers
// it.
func TestAC17ANestedSessionsApproveLeavesTheStoryWaitingWhereThePersonsOwnAnswersIt(t *testing.T) {
	t.Parallel()
	nested := waitingProject(t)
	before := alikeFiles(files(t, nested), nested)
	calls := 0
	r := expand(t, typed(nested, "session-nested", "sdlc:approve", "A-1"),
		realHookEnv(nested, "session-nested", map[string]string{"AI_AGENT": aiAgent}), counted(&calls))
	if calls != 0 || !r.blocked() {
		t.Fatalf("a nested session's /sdlc:approve ran %d commands: %+v", calls, r)
	}
	if diff := sameFiles(before, alikeFiles(files(t, nested), nested)); len(diff) > 0 {
		t.Errorf("a nested session's /sdlc:approve changed the record:\n%s", strings.Join(diff, "\n"))
	}

	own := waitingProject(t)
	ownBefore := alikeFiles(files(t, own), own)
	expand(t, typed(own, "session-b", "sdlc:approve", "A-1"), realHookEnv(own, "session-b", nil), counted(&calls))
	if calls != 1 {
		t.Fatalf("the person's own /sdlc:approve ran %d commands, want one", calls)
	}
	if diff := sameFiles(ownBefore, alikeFiles(files(t, own), own)); len(diff) == 0 {
		t.Error("the person's own /sdlc:approve recorded nothing: the control does not hold")
	}
}
