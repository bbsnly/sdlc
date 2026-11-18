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
// AC-17: a session started from another session's tool call, or a model's
// shell running the hook on a payload it wrote (T-12), is no person typing the
// line, and nothing is recorded. US-130 corrected how these rows model them
// (its ANALYSIS 3.1 to 3.3, Claude Code 2.1.321): every Claude Code process
// gives itself AI_AGENT=claude-code_<version>_harness, so a nested session's
// hook carries that label too, with CLAUDE_CODE_SESSION_ATTENDED=0 where it
// was started with `claude -p`; a model's shell carries _agent,
// CLAUDE_CODE_EXECPATH and GIT_EDITOR=true. The rows here once gave a nested
// session's hook _agent, which no real one has.

import (
	"io"
	"maps"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/cli"
)

// hookMarkers is what Claude Code 2.1.321 gives a hook it runs in Claude
// Desktop for a session a person opened, besides the session id and the
// project directory (US-130 ANALYSIS 3.1, the AC-5 fixture), with the
// plugin's own two. A hook has no CLAUDE_CODE_EXECPATH and no GIT_EDITOR,
// which a model's Bash has, and its AI_AGENT is the harness's label.
var hookMarkers = map[string]string{
	"AI_AGENT":                     "claude-code_2-1-321_harness",
	"CLAUDECODE":                   "1",
	"CLAUDE_CODE_CHILD_SESSION":    "1",
	"CLAUDE_CODE_SESSION_ATTENDED": "1",
	"CLAUDE_PID":                   "4242",
	"CLAUDE_CODE_ENTRYPOINT":       "claude-desktop",
	"CLAUDE_EFFORT":                "high",
	"CLAUDE_PLUGIN_ROOT":           "/opt/claude/plugins/cache/sdlc/sdlc/0.6.1",
	"CLAUDE_PLUGIN_DATA":           "/opt/claude/plugins/data/sdlc",
}

// aiAgent is the value Claude Code gives AI_AGENT in a model's tools, in its
// shape.
const aiAgent = "claude-code_2-1-321_agent"

// nestedHook is the hook of a session started with `claude -p` from a model's
// Bash in Claude Desktop: a harness's label like any hook, and
// CLAUDE_CODE_SESSION_ATTENDED=0, since nobody attends it. Whether it keeps
// the Bash's CLAUDE_CODE_EXECPATH and GIT_EDITOR is not known (US-130
// ANALYSIS 3.3), so it is given both, as the harder case for the CLI, whose
// own check reads neither.
var nestedHook = map[string]string{
	"CLAUDE_CODE_SESSION_ATTENDED": "0",
	"CLAUDE_CODE_EXECPATH":         "/opt/claude/versions/2.1.321",
	"GIT_EDITOR":                   "true",
}

// modelsShell is a model's Bash in Claude Desktop, which runs the hook itself.
func modelsShell(root string) func(string) string {
	return env(map[string]string{
		"AI_AGENT": aiAgent, "CLAUDECODE": "1", "CLAUDE_CODE_CHILD_SESSION": "1", "CLAUDE_CODE_SESSION_ATTENDED": "1",
		"CLAUDE_CODE_ENTRYPOINT": "claude-desktop", "CLAUDE_CODE_EXECPATH": "/opt/claude/versions/2.1.321",
		"GIT_EDITOR": "true", "CLAUDE_PROJECT_DIR": root,
	})
}

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

// AC-17: a person's slash command expanded in a session started from a
// model's Bash, or by a model's shell that runs the hook itself, records
// nothing, and the person is told that it did not run, which variable said so
// (by name), and what to type instead, the slash command named first (AC-12).
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
			"a nested session's hook": realHookEnv(root, "session-nested", nestedHook),
			"a model's shell":         modelsShell(root),
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
				if !strings.Contains(shown, "CLAUDE_CODE_SESSION_ATTENDED") && !strings.Contains(shown, "AI_AGENT") &&
					!strings.Contains(shown, "CLAUDE_CODE_EXECPATH") && !strings.Contains(shown, "GIT_EDITOR") {
					t.Errorf("the person is not told why: no variable the hook read is named:\n%s", shown)
				}
				if !strings.Contains(shown, "`!"+terminalForm(c.name)) || !strings.Contains(shown, "outside Claude Code") {
					t.Errorf("the person is not told what to type instead: the `!` line, or a terminal outside Claude Code:\n%s", shown)
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
		realHookEnv(nested, "session-nested", nestedHook), counted(&calls))
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
