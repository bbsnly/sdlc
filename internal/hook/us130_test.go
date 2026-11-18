package hook

// US-130: a person's slash commands run when typed in the Code tab of Claude
// Desktop, and a model still cannot get one run.
//
// Until now the slash route refused every person's command where the agent
// variable was set, and Claude Code sets it for every hook it runs in Claude
// Desktop, so /sdlc:approve answered there that it did not run. Gate 2 read
// Claude Code 2.1.321 in Claude Desktop (ANALYSIS 3.1 to 3.3): a hook Claude
// Code runs itself has AI_AGENT=claude-code_<version>_harness and
// CLAUDE_CODE_SESSION_ATTENDED=1, and neither CLAUDE_CODE_EXECPATH nor
// GIT_EDITOR; a model's Bash has AI_AGENT=..._agent, CLAUDE_CODE_EXECPATH and
// GIT_EDITOR=true; the hook of a `claude -p` started from a model's Bash has
// _harness and CLAUDE_CODE_SESSION_ATTENDED=0. The person's decision of
// 2026-11-13 (option B): a typed person's command runs only where all four
// hold, and anything else, a part that cannot be read among it, refuses.
//
// The environments are built from ANALYSIS 3.1's names, with values that are
// no one's (THREATS I2). The rows bind the check where US-108's frozen tests
// already bind the slash route's, at hook.RunWith with the getenv it is
// handed; they name no new function.

import (
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/cli"
)

const (
	// us130HookLabel is the agent variable in a hook Claude Code 2.1.321 runs
	// itself, and us130ToolLabel the same in a model's tools.
	us130HookLabel = "claude-code_2-1-321_harness"
	us130ToolLabel = "claude-code_2-1-321_agent"
	// us130ExecPath is CLAUDE_CODE_EXECPATH in its shape, made up, and odd
	// enough that a refusal that quoted it would be seen.
	us130ExecPath = "/opt/claude-desktop/claude-code/2.1.321/claude-us130-marker"
)

// changedHook is the environment of a Claude Desktop hook (realHookEnv, the
// AC-5 fixture) with change set over it and drop taken out.
func changedHook(root, session string, change map[string]string, drop ...string) func(string) string {
	pairs := map[string]string{"CLAUDE_CODE_SESSION_ID": session, "CLAUDE_PROJECT_DIR": root}
	maps.Copy(pairs, hookMarkers)
	maps.Copy(pairs, change)
	for _, name := range drop {
		delete(pairs, name)
	}
	return env(pairs)
}

// modelsBashIn is a model's Bash as Claude Code 2.1.321 makes it, with entry
// as its entrypoint: a hook's names, but the plugin's and the project's, and a
// model's tool's label, CLAUDE_CODE_EXECPATH and GIT_EDITOR=true. The hook run
// by hand from there has this environment.
func modelsBashIn(entry string) (map[string]string, []string) {
	return map[string]string{
		"AI_AGENT":               us130ToolLabel,
		"CLAUDE_CODE_EXECPATH":   us130ExecPath,
		"GIT_EDITOR":             "true",
		"CLAUDE_CODE_ENTRYPOINT": entry,
	}, []string{"CLAUDE_PLUGIN_ROOT", "CLAUDE_PLUGIN_DATA", "CLAUDE_PROJECT_DIR"}
}

// showsWhatTheTerminalShows holds what a typed command ran to what its
// terminal form did in a project set up the same way: the same record, its
// output and errors shown, and its exit outcome where it failed.
func showsWhatTheTerminalShows(t *testing.T, c personsCase, getenv func(root string) func(string) string) {
	t.Helper()
	byHand := c.project(t)
	stdout, stderr, code := terminal(t, byHand, c.argv...)
	after := alikeFiles(files(t, byHand), byHand)

	typedIn := c.project(t)
	r := expand(t, typed(typedIn, "session-b", c.slash, c.args), getenv(typedIn), cli.ExecuteIn)
	shown := alike(r.shown(), typedIn)
	if strings.Contains(shown, "did not run") || strings.Contains(shown, "is set where this hook runs") {
		t.Fatalf("/%s %s was refused where a person typed it:\n%s", c.slash, c.args, shown)
	}
	if !r.blocked() {
		t.Fatalf("the expansion went on to the model after the hook ran it: %+v", r)
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
	if code != 0 && !strings.Contains(shown, "exit code "+strconv.Itoa(code)) {
		t.Errorf("the terminal form ended with exit code %d and the person was not shown that: %q", code, shown)
	}
	if ctx := r.HookSpecificOutput.AdditionalContext; ctx != "" {
		t.Errorf("the hook put text into the model's context: %q", ctx)
	}
}

// AC-1: each of a person's commands typed in a session they opened in Claude
// Desktop, with the hook's environment as Claude Code 2.1.321 gives it there,
// runs as typed: the record changes as the terminal form changes it, the
// person is shown what that printed and how it ended, and the refusal that
// says the agent variable is set where the hook runs is gone. Today every row
// is refused with it.
func TestAC1APersonsCommandTypedInClaudeDesktopRunsAndShowsWhatTheTerminalFormPrints(t *testing.T) {
	t.Parallel()
	for _, c := range personsCases() {
		t.Run(c.name+" typed in Claude Desktop", func(t *testing.T) {
			t.Parallel()
			showsWhatTheTerminalShows(t, c, func(root string) func(string) string {
				return realHookEnv(root, "session-b", nil)
			})
		})
	}
}

// AC-1 with Q2 and Q-B: the same check holds in Claude Code run from a
// terminal (2.1.312 and later give a hook the same label, with the entrypoint
// cli), for a later version of Claude Code whose label has the same shape,
// and for a person whose own shell exports an editor of theirs. Only
// GIT_EDITOR=true is a model's.
func TestAC1APersonsCommandTypedInClaudeCodeInATerminalOrWithTheirOwnEditorRuns(t *testing.T) {
	t.Parallel()
	approve := personsCases()[0]
	for _, c := range []struct {
		name   string
		change map[string]string
	}{
		{"AC-1 Claude Code 2.1.312 run from a terminal", map[string]string{
			"AI_AGENT": "claude-code_2-1-312_harness", "CLAUDE_CODE_ENTRYPOINT": "cli",
		}},
		{"AC-1 a later Claude Code whose hook's label has the same shape", map[string]string{
			"AI_AGENT": "claude-code_2-1-400_harness",
		}},
		{"AC-1 a person whose own shell exports GIT_EDITOR=vim", map[string]string{
			"AI_AGENT": "claude-code_2-1-312_harness", "CLAUDE_CODE_ENTRYPOINT": "cli", "GIT_EDITOR": "vim",
		}},
		{"AC-1 a person whose own shell exports an editor that waits", map[string]string{
			"GIT_EDITOR": "code --wait",
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			showsWhatTheTerminalShows(t, approve, func(root string) func(string) string {
				return changedHook(root, "session-b", c.change)
			})
		})
	}
}

// aRoute is one way a model could reach the hook with a person's command:
// the hook's environment, as a change to a Claude Desktop hook's, and an edit
// to the payload Claude Code would send for the line typed.
type aRoute struct {
	name   string
	change map[string]string
	drop   []string
	edit   func(p map[string]any)
}

// modelRoutes are AC-2's routes as they reach the hook (ANALYSIS 3.3, THREATS
// S1 and S2). A Skill tool raises no expansion, and the six skills are kept
// from the model (internal/plugin's
// TestAC2EachPersonsSlashCommandIsKeptFromTheModelAndOnlyReportsThatTheActionDidNotRun);
// a payload shaped as typed, in a model's own environment, is the first row.
func modelRoutes() []aRoute {
	desktopBash, bashDrop := modelsBashIn("claude-desktop")
	terminalBash, _ := modelsBashIn("cli")
	harnessed := maps.Clone(desktopBash)
	harnessed["AI_AGENT"] = us130HookLabel
	return []aRoute{
		{name: "AC-2 the hook run by hand from a model's Bash in Claude Desktop", change: desktopBash, drop: bashDrop},
		{name: "AC-2 the hook run by hand from a model's Bash in Claude Code in a terminal", change: terminalBash, drop: bashDrop},
		{name: "AC-2 the hook run by hand with the agent variable set to a hook's label", change: harnessed, drop: bashDrop},
		{name: "AC-2 the hook run by hand with a hook's label and CLAUDE_CODE_EXECPATH gone", change: harnessed,
			drop: append([]string{"CLAUDE_CODE_EXECPATH"}, bashDrop...)},
		{name: "AC-2 the hook run by hand with a hook's label and GIT_EDITOR gone", change: harnessed,
			drop: append([]string{"GIT_EDITOR"}, bashDrop...)},
		{name: "AC-2 a claude -p started from a model's Bash in Claude Desktop, keeping what the Bash had",
			change: map[string]string{"CLAUDE_CODE_SESSION_ATTENDED": "0", "CLAUDE_CODE_EXECPATH": us130ExecPath, "GIT_EDITOR": "true"}},
		{name: "AC-2 a claude -p started from a model's Bash in Claude Desktop, with CLAUDE_CODE_EXECPATH and GIT_EDITOR cleared",
			change: map[string]string{"CLAUDE_CODE_SESSION_ATTENDED": "0"}},
		{name: "AC-2 a claude -p started from a model's Bash in Claude Code in a terminal",
			change: map[string]string{
				"AI_AGENT": "claude-code_2-1-312_harness", "CLAUDE_CODE_ENTRYPOINT": "sdk-cli", "CLAUDE_CODE_SESSION_ATTENDED": "0",
			}},
		{name: "AC-2 an interactive claude started from a model's Bash, keeping what the Bash had",
			change: map[string]string{"CLAUDE_CODE_EXECPATH": us130ExecPath, "GIT_EDITOR": "true"}},
		{name: "AC-2 a subagent's expansion in the person's own session", edit: func(p map[string]any) {
			p["agent_type"], p["agent_id"] = "sdlc:researcher", "a1b2c3"
		}},
	}
}

// personsSix are the six commands that are a person's alone, by skill name,
// with arguments each takes.
var personsSix = []struct{ name, args string }{
	{"approve", "A-1"},
	{"unfreeze", `--reason "x"`},
	{"ack", "--through HEAD"},
	{"remap", "A-1"},
	{"release", `A-1 --reason "x"`},
	{"assign", `security --model opus --reason "mine"`},
}

// AC-2: by every route a model can start, in Claude Desktop or in Claude Code
// in a terminal, none of the six runs, nothing under .sdlc/ changes, and an
// answer the person reads says what they type instead. The control is the
// same line typed in the person's own session in Claude Desktop, which runs
// and changes the record: without it every row passes on a hook that runs
// nothing, as today's does.
func TestAC2NoRouteAModelCanStartRunsAPersonsCommandAndTheAnswerSaysWhatAPersonTypes(t *testing.T) {
	t.Parallel()
	t.Run("AC-2 control: /sdlc:approve typed in the person's own session in Claude Desktop records", func(t *testing.T) {
		t.Parallel()
		root := waitingProject(t)
		before := files(t, root)
		calls := 0
		r := expand(t, typed(root, "session-b", "sdlc:approve", "A-1"), realHookEnv(root, "session-b", nil), counted(&calls))
		if calls != 1 || len(sameFiles(before, files(t, root))) == 0 {
			t.Fatalf("the person's own /sdlc:approve in Claude Desktop ran %d commands and changed nothing:\n%s", calls, r.shown())
		}
	})
	for _, route := range modelRoutes() {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			for _, c := range personsSix {
				root := loopProject(t)
				payload := typed(root, "session-nested", "sdlc:"+c.name, c.args)
				if route.edit != nil {
					route.edit(payload)
				}
				calls := 0
				recorder := func(string, func(string) string, []string, io.Reader, io.Writer, io.Writer) int {
					calls++
					return 0
				}
				r := expand(t, payload, changedHook(root, "session-nested", route.change, route.drop...), recorder)
				if calls != 0 {
					t.Errorf("/sdlc:%s was run %d times", c.name, calls)
				}
				if r.blocked() {
					slash := "/sdlc:" + c.name
					if shown := r.shown(); !strings.Contains(shown, slash+" did not run") || !strings.Contains(shown, terminalForm(c.name)) {
						t.Errorf("the answer does not say that %s did not run and what a person types instead (%s):\n%s",
							slash, terminalForm(c.name), shown)
					}
				}
				if ctx := r.HookSpecificOutput.AdditionalContext; ctx != "" {
					t.Errorf("the hook put text into the model's context: %q", ctx)
				}
			}
			// Against a real record: a waiting story is not answered, and a
			// freeze is not lifted.
			for _, rec := range []struct {
				slash, args string
				project     func(t *testing.T) string
			}{
				{"sdlc:approve", "A-1", waitingProject},
				{"sdlc:unfreeze", `--reason "the test was wrong"`, inGit(func(t *testing.T, root string) {
					t.Helper()
					freeze(t, root, "A-1", "internal/invoice_test.go", "package internal\n")
				})},
			} {
				root := rec.project(t)
				before := files(t, root)
				payload := typed(root, "session-nested", rec.slash, rec.args)
				if route.edit != nil {
					route.edit(payload)
				}
				calls := 0
				expand(t, payload, changedHook(root, "session-nested", route.change, route.drop...), counted(&calls))
				if calls != 0 {
					t.Errorf("/%s was run against the record %d times", rec.slash, calls)
				}
				if diff := sameFiles(before, files(t, root)); len(diff) > 0 {
					t.Errorf("/%s changed the record:\n%s", rec.slash, strings.Join(diff, "\n"))
				}
			}
		})
	}
}

// AC-4: wherever the hook cannot read that a person typed the command -- the
// agent variable missing, blank or of another shape, a version or entrypoint
// that gives the markers otherwise, a marker a hook does not have -- it fails
// closed. The command does not run, and the refusal names the variable it
// could not read, by name and never its value, and says what a person does
// instead: the `!` line in Claude Desktop, or a terminal outside Claude Code.
// /sdlc:init and /sdlc:doctor decide nothing and run whatever the
// environment.
func TestAC4WhereTheHookCannotReadThatAPersonTypedTheCommandItFailsClosedAndSaysWhatToDo(t *testing.T) {
	t.Parallel()
	attended := "CLAUDE_CODE_SESSION_ATTENDED"
	every := []string{
		"AI_AGENT", "CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", attended, "CLAUDE_PID", "CLAUDE_CODE_ENTRYPOINT",
		"CLAUDE_PLUGIN_ROOT", "CLAUDE_PLUGIN_DATA",
	}
	for _, c := range []struct {
		name   string
		change map[string]string
		drop   []string
		names  []string // what may be named as not read; one of them must be
		secret string   // a value that must not be shown
	}{
		{"AC-4 no agent variable, as before Claude Code 2.1.312", nil, []string{"AI_AGENT"}, []string{"AI_AGENT"}, ""},
		{"AC-4 the agent variable blank", map[string]string{"AI_AGENT": ""}, nil, []string{"AI_AGENT"}, ""},
		{"AC-4 a model's tool's label", map[string]string{"AI_AGENT": us130ToolLabel}, nil, []string{"AI_AGENT"}, us130ToolLabel},
		{"AC-4 Claude Code's name with no version or kind", map[string]string{"AI_AGENT": "claude-code"}, nil, []string{"AI_AGENT"}, ""},
		{"AC-4 another host's harness label", map[string]string{"AI_AGENT": "other-agent_1-0-0_harness"}, nil,
			[]string{"AI_AGENT"}, "other-agent_1-0-0_harness"},
		{"AC-4 a label that only starts as a hook's does", map[string]string{"AI_AGENT": us130HookLabel + "es"}, nil,
			[]string{"AI_AGENT"}, us130HookLabel + "es"},
		{"AC-4 a newer Claude Code that labels its hooks another way", map[string]string{"AI_AGENT": "claude-code_3-0-0_hook"}, nil,
			[]string{"AI_AGENT"}, "claude-code_3-0-0_hook"},
		{"AC-4 CLAUDE_CODE_EXECPATH set", map[string]string{"CLAUDE_CODE_EXECPATH": us130ExecPath}, nil,
			[]string{"CLAUDE_CODE_EXECPATH"}, us130ExecPath},
		{"AC-4 GIT_EDITOR=true", map[string]string{"GIT_EDITOR": "true"}, nil, []string{"GIT_EDITOR"}, ""},
		{"AC-4 CLAUDE_CODE_SESSION_ATTENDED missing", nil, []string{attended}, []string{attended}, ""},
		{"AC-4 CLAUDE_CODE_SESSION_ATTENDED=0", map[string]string{attended: "0"}, nil, []string{attended}, ""},
		{"AC-4 CLAUDE_CODE_SESSION_ATTENDED=true", map[string]string{attended: "true"}, nil, []string{attended}, ""},
		{"AC-4 another entrypoint, which Claude Code does not count as attended", map[string]string{
			"CLAUDE_CODE_ENTRYPOINT": "sdk-ts", attended: "0",
		}, nil, []string{attended}, ""},
		{"AC-4 none of Claude Code's markers, as from a clean environment", nil, every, []string{"AI_AGENT", attended}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			for _, p := range personsSix {
				root := loopProject(t)
				calls := 0
				recorder := func(string, func(string) string, []string, io.Reader, io.Writer, io.Writer) int {
					calls++
					return 0
				}
				r := expand(t, typed(root, "session-b", "sdlc:"+p.name, p.args), changedHook(root, "session-b", c.change, c.drop...), recorder)
				slash := "/sdlc:" + p.name
				shown := r.shown()
				switch {
				case calls != 0:
					t.Errorf("%s was run %d times", slash, calls)
				case !r.blocked():
					t.Errorf("%s was let through to the model, which only reads that it did not run: %+v", slash, r)
				case !strings.Contains(shown, slash+" did not run"):
					t.Errorf("the person is not told that %s did not run:\n%s", slash, shown)
				}
				named := false
				for _, name := range c.names {
					named = named || strings.Contains(shown, name)
				}
				if !named {
					t.Errorf("the refusal of %s does not name what it could not read, one of %v:\n%s", slash, c.names, shown)
				}
				if c.secret != "" && strings.Contains(shown, c.secret) {
					t.Errorf("the refusal of %s quotes the variable's value %q:\n%s", slash, c.secret, shown)
				}
				for _, want := range []string{"`!" + terminalForm(p.name), "Claude Desktop", "outside Claude Code"} {
					if !strings.Contains(shown, want) {
						t.Errorf("the refusal of %s does not name the `!` line in Claude Desktop and a terminal outside Claude Code (no %q):\n%s",
							slash, want, shown)
					}
				}
				if ctx := r.HookSpecificOutput.AdditionalContext; ctx != "" {
					t.Errorf("the hook put text into the model's context: %q", ctx)
				}
			}
			for _, name := range []string{"init", "doctor"} {
				root := loopProject(t)
				calls := 0
				recorder := func(string, func(string) string, []string, io.Reader, io.Writer, io.Writer) int {
					calls++
					return 0
				}
				expand(t, typed(root, "session-b", "sdlc:"+name, ""), changedHook(root, "session-b", c.change, c.drop...), recorder)
				if calls != 1 {
					t.Errorf("/sdlc:%s, which decides nothing, ran %d times, want once", name, calls)
				}
			}
			// Against a real record: the waiting story is not answered.
			root := waitingProject(t)
			before := files(t, root)
			calls := 0
			expand(t, typed(root, "session-b", "sdlc:approve", "A-1"), changedHook(root, "session-b", c.change, c.drop...), counted(&calls))
			if diff := sameFiles(before, files(t, root)); calls != 0 || len(diff) > 0 {
				t.Errorf("/sdlc:approve ran %d commands and changed the record:\n%s", calls, strings.Join(diff, "\n"))
			}
		})
	}
}

// AC-3 through the hook: a model's command that takes away, blanks or sets
// what the check reads is refused at PreToolUse, from the session holding the
// story and from a second session, under enforcement-stays-on. Reading them
// is not. internal/shellpolicy holds the spellings; these hold that the hook
// asks the rule on both paths, for Bash and for PowerShell.
func TestAC3ThroughTheHookACommandThatChangesWhatTheCheckReadsIsRefusedInEverySession(t *testing.T) {
	t.Parallel()
	const rule = "[enforcement-stays-on]"
	root := loopProject(t)
	for _, c := range []struct{ tool, cmd string }{
		{"Bash", "unset CLAUDE_CODE_CHILD_SESSION"},
		{"Bash", "env -u CLAUDE_CODE_EXECPATH -u GIT_EDITOR sdlc hook PreToolUse < /tmp/p.json"},
		{"Bash", "export CLAUDE_CODE_ENTRYPOINT=claude-desktop"},
		{"Bash", "CLAUDE_CODE_SESSION_ATTENDED=1 sdlc status"},
		{"Bash", "GIT_EDITOR=vim git log -1"},
		{"PowerShell", "$env:CLAUDE_CODE_EXECPATH = $null; sdlc status"},
		{"PowerShell", "Remove-Item Env:GIT_EDITOR"},
	} {
		for _, session := range []string{working, "session-b"} {
			r := call(t, shellCall(root, session, c.tool, c.cmd), noEnv)
			why := reasonOf(r)
			switch {
			case !denied(r):
				t.Errorf("AC-3 from session %q, %s let through: %s", session, c.tool, c.cmd)
			case !strings.Contains(why, rule):
				t.Errorf("AC-3 from session %q, %s was refused under another rule than %s: %q", session, c.cmd, rule, why)
			case !strings.Contains(why, "Instead:"):
				t.Errorf("AC-3 from session %q, the refusal of %s says nothing to do next: %q", session, c.cmd, why)
			}
		}
	}
	for _, c := range []struct{ tool, cmd string }{
		{"Bash", "printenv GIT_EDITOR"},
		{"Bash", `echo "$CLAUDE_CODE_SESSION_ATTENDED"`},
		{"PowerShell", "echo $env:CLAUDE_CODE_EXECPATH"},
	} {
		for _, session := range []string{working, "session-b"} {
			if r := call(t, shellCall(root, session, c.tool, c.cmd), noEnv); !allows(r) {
				t.Errorf("AC-3 from session %q, %s was refused: %q", session, c.cmd, reasonOf(r))
			}
		}
	}
}

// docText is a file of this repository, its line breaks and the comment marks
// of Go source folded into single spaces, so that a phrase reads the same
// however it was wrapped.
func docText(t *testing.T, rel ...string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, rel...)...))
	if err != nil {
		t.Fatal(err)
	}
	text := regexp.MustCompile(`(?m)^\s*//\s?`).ReplaceAllString(string(raw), " ")
	return strings.Join(strings.Fields(text), " ")
}

// docSection is the part of text from the heading head to the next heading of
// the same level or above.
func docSection(t *testing.T, raw, head string) string {
	t.Helper()
	at := strings.Index(raw, "\n"+head+"\n")
	if at < 0 {
		t.Fatalf("no heading %q", head)
	}
	rest := raw[at+len(head)+2:]
	level := strings.Count(strings.Fields(head)[0], "#")
	for i := 1; i <= level; i++ {
		if end := strings.Index(rest, "\n"+strings.Repeat("#", i)+" "); end >= 0 {
			rest = rest[:end]
		}
	}
	return rest
}

// AC-6: the pages and comments that say a person's slash command does not run
// in Claude Desktop say what is true now, and the pages that describe the
// check say what it reads, the Claude Code version and entrypoint it was seen
// in (AC-3), and what it does not hold.
func TestAC6TheDocsAndCommentsSayWhatTheSlashRouteNowReadsAndHolds(t *testing.T) {
	t.Parallel()
	signal := []string{"harness", "CLAUDE_CODE_EXECPATH", "GIT_EDITOR", "CLAUDE_CODE_SESSION_ATTENDED"}

	t.Run("AC-6 docs/claude-desktop.md types the slash commands in Claude Desktop as in Claude Code", func(t *testing.T) {
		page := docText(t, "docs", "claude-desktop.md")
		for _, stale := range []string{
			"in Claude Desktop, as a `!` line",
			"In Claude Desktop, type a person's command as a `!` line",
			"runs the hook with `AI_AGENT` set, so `/sdlc:approve US-001` answers that it did not run",
		} {
			if strings.Contains(page, stale) {
				t.Errorf("the page still says %q", stale)
			}
		}
		// A sentence that has a person type a slash command in Claude Desktop,
		// and neither sends them to a `!` line nor says it does not run.
		typesThere := false
		for _, s := range regexp.MustCompile(`[.:;]\s`).Split(page, -1) {
			if strings.Contains(s, "Claude Desktop") && strings.Contains(s, "/sdlc:") && strings.Contains(s, "type") &&
				!strings.Contains(s, "`!") && !strings.Contains(s, "did not run") && !strings.Contains(s, "does not run") {
				typesThere = true
			}
		}
		if !typesThere {
			t.Error("no sentence of the page has a person type a slash command in Claude Desktop")
		}
	})

	t.Run("AC-6 docs/enforcement.md describes the check, the version and entrypoint, and what it does not hold", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "enforcement.md"))
		if err != nil {
			t.Fatal(err)
		}
		page := docText(t, "docs", "enforcement.md")
		for _, want := range append([]string{"2.1.321", "claude-desktop", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT"}, signal...) {
			if !strings.Contains(page, want) {
				t.Errorf("the page does not name %s", want)
			}
		}
		for _, stale := range []string{
			"Removing `AI_AGENT` alone is enough for the hook",
			"while a slash command does not run, since the hook there has `AI_AGENT`",
			"a slash command typed that way does not run",
		} {
			if strings.Contains(page, stale) {
				t.Errorf("the page still says %q", stale)
			}
		}
		held := docSection(t, strings.ReplaceAll(string(raw), "\r\n", "\n"), "### Not held")
		notHeld := strings.Join(strings.Fields(held), " ")
		// The nested session that clears both, and the hand-run hook that
		// sets the label and takes both away (THREATS, residuals 2 and 3).
		for _, want := range []string{"CLAUDE_CODE_EXECPATH", "GIT_EDITOR"} {
			if !strings.Contains(notHeld, want) {
				t.Errorf("Not held does not name %s", want)
			}
		}
		// Residual 4: keystrokes typed into Claude Desktop's own prompt.
		keystrokes := false
		for p := range strings.SplitSeq(held, "\n- ") {
			keystrokes = keystrokes || (strings.Contains(p, "osascript") || strings.Contains(p, "keystroke")) &&
				strings.Contains(p, "Claude Desktop")
		}
		if !keystrokes {
			t.Error("Not held has no entry for a slash command typed into Claude Desktop's prompt by keystrokes")
		}
	})

	t.Run("AC-6 docs/troubleshooting.md and SECURITY.md describe the check", func(t *testing.T) {
		for _, rel := range [][]string{{"docs", "troubleshooting.md"}, {"SECURITY.md"}} {
			page := docText(t, rel...)
			for _, want := range signal {
				if !strings.Contains(page, want) {
					t.Errorf("%s does not name %s", filepath.Join(rel...), want)
				}
			}
		}
		if page := docText(t, "docs", "troubleshooting.md"); strings.Contains(page,
			"where the slash command answers that it did not run (Claude Code 2.1.309, seen on 2026-10-29)") {
			t.Error("docs/troubleshooting.md still says the slash command does not run in Claude Desktop")
		}
	})

	t.Run("AC-6 the comments in slash.go and store.go say what is true of the agent variable now", func(t *testing.T) {
		for _, c := range []struct {
			rel   []string
			stale string
		}{
			{[]string{"internal", "hook", "slash.go"}, "so a hook that has it may be a model's shell running the hook on a payload it wrote"},
			{[]string{"internal", "store", "store.go"}, "The slash route reads it and fails closed"},
		} {
			if strings.Contains(docText(t, c.rel...), c.stale) {
				t.Errorf("%s still says %q", filepath.Join(c.rel...), c.stale)
			}
		}
	})
}
