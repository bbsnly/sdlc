// US-108: a person's decisions typed as slash commands.
//
// A person in Claude Desktop has no terminal, so approve, unfreeze, ack, remap,
// release and assign are typed as /sdlc:<name>. Claude Code raises
// UserPromptExpansion for a command a person typed, and never for one the
// model starts with its Skill tool; the hook answers that event by running the
// CLI command itself, so the record changes as it does from a terminal and the
// person reads what the terminal would have printed.
//
// What these tests bind, and the implementation has to provide:
//
//	hook.RunWith(args, stdin, stdout, stderr, getenv, commands) int
//
// is hook.Run with a way to run one sdlc command: commands has the signature
// of cli.ExecuteIn, which cmd/sdlc hands it (hook may not import cli). The
// tests here hand it cli.ExecuteIn too, or a recorder where the test is about
// what the command is handed rather than what it does.
//
// A slash command the hook ran, or refused to run, is answered by stopping the
// expansion: decision "block", or continue false. The skill body (AC-2) says
// the action did not run, so an expansion let through to the model after the
// command ran would tell the person the opposite of what happened.

package hook

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/cli"
)

// commandRunner is the signature of cli.ExecuteIn.
type commandRunner = func(dir string, getenv func(string) string, args []string,
	stdin io.Reader, stdout, stderr io.Writer) int

// expansionReply is every field the hook may answer UserPromptExpansion with.
type expansionReply struct {
	Continue           *bool  `json:"continue"`
	StopReason         string `json:"stopReason"`
	Decision           string `json:"decision"`
	Reason             string `json:"reason"`
	SystemMessage      string `json:"systemMessage"`
	HookSpecificOutput struct {
		HookEventName     string `json:"hookEventName"`
		AdditionalContext string `json:"additionalContext"`
	} `json:"hookSpecificOutput"`
}

// blocked is whether the expansion stops here, so the model never reads it.
func (r expansionReply) blocked() bool {
	return r.Decision == "block" || (r.Continue != nil && !*r.Continue)
}

// shown is what Claude Code shows the person: the reason for a block, the
// reason for stopping, and the system message.
func (r expansionReply) shown() string {
	return strings.Join([]string{r.Reason, r.StopReason, r.SystemMessage}, "\n")
}

// typed is the payload Claude Code sends when a person types /<name> <args> in
// a session whose working directory is root.
func typed(root, session, name, args string) map[string]any {
	prompt := "/" + name
	if args != "" {
		prompt += " " + args
	}
	return map[string]any{
		"hook_event_name": "UserPromptExpansion",
		"session_id":      session,
		"transcript_path": filepath.Join(root, "transcript.jsonl"),
		"cwd":             root,
		"permission_mode": "default",
		"prompt_id":       "prompt-1",
		"expansion_type":  "slash_command",
		"command_name":    name,
		"command_args":    args,
		"command_source":  "plugin",
		"prompt":          prompt,
	}
}

// hookEnv is the environment Claude Code gives a hook in a session: its own
// session id, and the project directory.
func hookEnv(root, session string) func(string) string {
	return env(map[string]string{"CLAUDE_CODE_SESSION_ID": session, "CLAUDE_PROJECT_DIR": root})
}

// expand hands the hook one UserPromptExpansion event and reads its answer,
// which has to be one JSON object and nothing else, with exit 0.
func expand(t *testing.T, payload map[string]any, getenv func(string) string, commands commandRunner) expansionReply {
	t.Helper()
	return expandEvent(t, "UserPromptExpansion", payload, getenv, commands)
}

func expandEvent(t *testing.T, event string, payload map[string]any, getenv func(string) string,
	commands commandRunner) expansionReply {
	t.Helper()
	var out bytes.Buffer
	if code := RunWith([]string{event}, strings.NewReader(mustMarshal(payload)), &out, io.Discard, getenv, commands); code != 0 {
		t.Fatalf("the hook exited %d on %s, so Claude Code reads no answer: %q", code, event, out.String())
	}
	return oneReply(t, out.String())
}

// oneReply decodes a hook's stdout, which has to hold one JSON object and
// nothing after it: plain text on stdout is added to the model's context.
func oneReply(t *testing.T, raw string) expansionReply {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var r expansionReply
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("the hook's stdout is not one JSON object: %v\n%q", err, raw)
	}
	if rest := strings.TrimSpace(raw[dec.InputOffset():]); rest != "" {
		t.Fatalf("the hook printed more than one JSON object: %q", raw)
	}
	return r
}

// terminal runs the command as a person at a terminal does: no session in its
// environment, from dir.
func terminal(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = cli.ExecuteIn(dir, noEnv, args, strings.NewReader(""), &out, &errOut)
	return out.String(), errOut.String(), code
}

// counted is cli.ExecuteIn, counting the commands it is handed.
func counted(calls *int) commandRunner {
	return func(dir string, getenv func(string) string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
		*calls++
		return cli.ExecuteIn(dir, getenv, args, stdin, stdout, stderr)
	}
}

// files is every file under root but git's own, by slash-separated relative
// path, with what it holds: the record a command changes, and anything else it
// leaves behind.
func files(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var (
	momentPattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})?`)
	commitPattern = regexp.MustCompile(`\b[0-9a-f]{40,64}\b|\b[0-9a-f]{7,12}\b`)
)

// alike is text from one project as it would read from another set up the
// same way: the project's place, the moments and the commit names replaced.
// The project's directory name goes too, where it stands as a word of its own:
// `sdlc init` heads CLAUDE.md with it, and t.TempDir names the two projects 001
// and 002. A name inside a longer word, such as US-001, is the project's and
// stays.
func alike(text, root string) string {
	for _, p := range []string{root, filepath.ToSlash(root), strings.ReplaceAll(root, `\`, `\\`)} {
		text = strings.ReplaceAll(text, p, "<root>")
	}
	if name := filepath.Base(root); name != "" && name != "." && name != string(filepath.Separator) {
		alone := regexp.MustCompile(`(^|[^\w-])` + regexp.QuoteMeta(name) + `($|[^\w-])`)
		// Twice, so that two in a row separated by one character are both
		// replaced: the first match takes the character between them.
		for range 2 {
			text = alone.ReplaceAllString(text, "${1}<name>${2}")
		}
	}
	text = momentPattern.ReplaceAllString(text, "<time>")
	return commitPattern.ReplaceAllString(text, "<sha>")
}

func alikeFiles(in map[string]string, root string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = alike(v, root)
	}
	return out
}

// sameFiles reports how two snapshots differ, one line per file.
func sameFiles(a, b map[string]string) []string {
	var diff []string
	for k, v := range a {
		if w, ok := b[k]; !ok {
			diff = append(diff, "only in the first: "+k)
		} else if v != w {
			diff = append(diff, "differs: "+k+"\n--- first\n"+v+"\n--- second\n"+w)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			diff = append(diff, "only in the second: "+k)
		}
	}
	return diff
}

// committed makes root a git repository with everything in it committed, at a
// fixed moment, so that two projects set up the same way have the same commits.
func committed(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"-c", "user.name=Person", "-c", "user.email=person@example.com", "commit", "-q", "-m", "feat: invoices"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_DATE=2026-10-19T10:00:00Z", "GIT_COMMITTER_DATE=2026-10-19T10:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// personsCase is one person-only command, typed and run from a terminal.
type personsCase struct {
	name    string
	project func(t *testing.T) string
	slash   string // the command name Claude Code reports
	args    string // as typed
	argv    []string
	changes bool // whether the terminal run changes the record
}

// inGit is a project made by setUp and committed, as every project the CLI
// works in is a git repository.
func inGit(setUp func(t *testing.T, root string)) func(t *testing.T) string {
	return func(t *testing.T) string {
		t.Helper()
		root := loopProject(t)
		setUp(t, root)
		committed(t, root)
		return root
	}
}

// waitingProject is a project whose story A-1 waits for a person's answer.
func waitingProject(t *testing.T) string {
	t.Helper()
	return inGit(func(t *testing.T, root string) { t.Helper(); escalated(t, root, false) })(t)
}

func personsCases() []personsCase {
	waiting := waitingProject
	frozen := inGit(func(t *testing.T, root string) {
		t.Helper()
		freeze(t, root, "A-1", "internal/invoice_test.go", "package internal\n")
	})
	underWay := inGit(func(*testing.T, string) {})
	staffed := inGit(func(t *testing.T, root string) {
		t.Helper()
		write(t, root, ".sdlc/config.json", staffingOn)
	})
	return []personsCase{
		{"AC-1 approve", waiting, "sdlc:approve", "A-1", []string{"approve", "A-1"}, true},
		{"AC-1 approve with a rejection", waiting, "sdlc:approve", `A-1 --reject "a zero total is refused too"`,
			[]string{"approve", "A-1", "--reject", "a zero total is refused too"}, true},
		{"AC-1 unfreeze", frozen, "sdlc:unfreeze", `--reason "AC-1's test asserted the wrong total"`,
			[]string{"unfreeze", "--reason", "AC-1's test asserted the wrong total"}, true},
		{"AC-1 ack", underWay, "sdlc:ack", "--through HEAD", []string{"ack", "--through", "HEAD"}, true},
		// test/e2e releases a story another checkout holds; here the refusal of
		// one still under way in this checkout.
		{"AC-1 release of a story under way here", underWay, "sdlc:release", `A-1 --reason "moved to a new session"`,
			[]string{"release", "A-1", "--reason", "moved to a new session"}, false},
		{"AC-1 assign", staffed, "sdlc:assign", `security --model opus --effort high --reason "mine"`,
			[]string{"staffing", "assign", "security", "--model", "opus", "--effort", "high", "--reason", "mine"}, true},
		// The refusals: the same SDLC-E code, worded the same, and nothing written.
		{"AC-1 remap of a story with nothing kept", underWay, "sdlc:remap", "A-1", []string{"remap", "A-1"}, false},
		{"AC-1 approve of a story that does not wait", underWay, "sdlc:approve", "A-1", []string{"approve", "A-1"}, false},
		{"AC-1 approve of a story that is not there", underWay, "sdlc:approve", "Z-9", []string{"approve", "Z-9"}, false},
		{"AC-1 unfreeze with nothing frozen", underWay, "sdlc:unfreeze", `--reason "x"`,
			[]string{"unfreeze", "--reason", "x"}, false},
		{"AC-1 ack of a commit that is not there", underWay, "sdlc:ack", "--through 0000000",
			[]string{"ack", "--through", "0000000"}, false},
		{"AC-1 release of a story that is not there", underWay, "sdlc:release", `Z-9 --reason "x"`,
			[]string{"release", "Z-9", "--reason", "x"}, false},
		{"AC-1 assign with the staffing off", underWay, "sdlc:assign", `security --model opus --reason "mine"`,
			[]string{"staffing", "assign", "security", "--model", "opus", "--reason", "mine"}, false},
	}
}

// Typed in a session -- here one that does not hold the story, with its id in
// the hook's environment as Claude Code puts it there -- each command changes
// the record exactly as the terminal command does in a project set up the same
// way, and the person is shown what the terminal printed, SDLC-E code and all.
// A command run with the hook's session in its environment would differ: the
// CLI refuses a session that does not hold the story as SessionElsewhere, and
// refuses `staffing assign` from any session as StaffingAssignInSession.
func TestAC1EachPersonOnlySlashCommandMakesTheTerminalsChangeAndShowsItsOutput(t *testing.T) {
	t.Parallel()
	for _, c := range personsCases() {
		t.Run(c.name, func(t *testing.T) {
			byHand := c.project(t)
			before := alikeFiles(files(t, byHand), byHand)
			stdout, stderr, code := terminal(t, byHand, c.argv...)
			after := alikeFiles(files(t, byHand), byHand)
			if changed := len(sameFiles(before, after)) > 0; changed != c.changes {
				t.Fatalf("the case is not what it says: the terminal run exited %d and changed the record: %v\n%s%s",
					code, changed, stdout, stderr)
			}

			typedIn := c.project(t)
			r := expand(t, typed(typedIn, "session-b", c.slash, c.args), hookEnv(typedIn, "session-b"), cli.ExecuteIn)
			if !r.blocked() {
				t.Fatalf("the expansion went on to the model after the hook handled it: %+v", r)
			}
			if diff := sameFiles(after, alikeFiles(files(t, typedIn), typedIn)); len(diff) > 0 {
				t.Errorf("/%s %s left the record unlike `sdlc %s` did:\n%s",
					c.slash, c.args, strings.Join(c.argv, " "), strings.Join(diff, "\n"))
			}
			shown := alike(r.shown(), typedIn)
			for name, want := range map[string]string{"stdout": stdout, "stderr": stderr} {
				if want = strings.TrimSpace(alike(want, byHand)); want != "" && !strings.Contains(shown, want) {
					t.Errorf("the person was not shown the terminal's %s.\nshown: %q\nwant:  %q", name, shown, want)
				}
			}
			if code != 0 && !strings.Contains(shown, "SDLC-E") {
				t.Errorf("the terminal refused with an SDLC-E code and the person was shown none: %q", shown)
			}
			// What the command printed quotes titles and reasons that anybody
			// could have written; it is for the person, never the model (T-3).
			if ctx := r.HookSpecificOutput.AdditionalContext; ctx != "" {
				t.Errorf("the hook put text into the model's context: %q", ctx)
			}
		})
	}
}

// The command is run from the directory the person typed it in, with no
// session in its environment, as a terminal has none, and with nothing on
// standard input: the payload is the hook's, not the command's.
func TestAC1ASlashCommandIsRunFromTheSessionsDirectoryWithNoSessionAndNothingOnStandardInput(t *testing.T) {
	t.Parallel()
	root := loopProject(t)
	below := filepath.Join(root, "internal")
	if err := os.MkdirAll(below, 0o755); err != nil {
		t.Fatal(err)
	}
	var got struct {
		dir, session, stdin string
		args                []string
		calls               int
	}
	recorder := func(dir string, getenv func(string) string, args []string, stdin io.Reader, stdout, _ io.Writer) int {
		got.calls++
		got.dir, got.session, got.args = dir, getenv("CLAUDE_CODE_SESSION_ID"), args
		in, _ := io.ReadAll(stdin)
		got.stdin = string(in)
		_, _ = io.WriteString(stdout, "US-001  approved (spec_unclear)\n")
		return 0
	}
	r := expand(t, typed(below, working, "sdlc:approve", "A-1"), hookEnv(root, working), recorder)
	if got.calls != 1 {
		t.Fatalf("the command was run %d times, want once", got.calls)
	}
	if got.dir != below {
		t.Errorf("the command ran from %q, want the session's directory %q", got.dir, below)
	}
	if got.session != "" {
		t.Errorf("the command saw the session %q, where a terminal has none", got.session)
	}
	if got.stdin != "" {
		t.Errorf("the command was handed %q on standard input", got.stdin)
	}
	if strings.Join(got.args, "\x00") != "approve\x00A-1" {
		t.Errorf("the command was handed %q, want [approve A-1]", got.args)
	}
	if !r.blocked() || !strings.Contains(r.shown(), "US-001  approved (spec_unclear)") {
		t.Errorf("the person was not shown what the command printed: %+v", r)
	}
}

// AC-10: /sdlc:init and /sdlc:doctor are not a person's decisions, and come
// out as `sdlc init` and `sdlc doctor` do.
func TestAC10InitAndDoctorTypedAsSlashCommandsComeOutAsTheTerminalCommandsDo(t *testing.T) {
	t.Parallel()
	fresh := func(t *testing.T) string {
		t.Helper()
		root := t.TempDir()
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		write(t, root, "go.mod", "module example.com/invoices\n\ngo 1.22\n")
		write(t, root, "invoice.go", "package invoices\n")
		committed(t, root)
		return root
	}
	initialised := func(t *testing.T) string {
		t.Helper()
		root := fresh(t)
		if _, stderr, code := terminal(t, root, "init"); code != 0 {
			t.Fatalf("sdlc init: exit %d\n%s", code, stderr)
		}
		return root
	}
	for _, c := range []struct {
		name    string
		project func(t *testing.T) string
		slash   string
		argv    []string
	}{
		{"AC-10 init in a repository that does not take part yet", fresh, "sdlc:init", []string{"init"}},
		{"AC-10 doctor in a project that takes part", initialised, "sdlc:doctor", []string{"doctor"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			byHand := c.project(t)
			stdout, stderr, _ := terminal(t, byHand, c.argv...)
			want := alikeFiles(files(t, byHand), byHand)

			typedIn := c.project(t)
			r := expand(t, typed(typedIn, "session-b", c.slash, ""), hookEnv(typedIn, "session-b"), cli.ExecuteIn)
			if !r.blocked() {
				t.Fatalf("the expansion went on to the model after the hook ran it: %+v", r)
			}
			if diff := sameFiles(want, alikeFiles(files(t, typedIn), typedIn)); len(diff) > 0 {
				t.Errorf("/%s left the project unlike `sdlc %s` did:\n%s", c.slash, c.argv[0], strings.Join(diff, "\n"))
			}
			shown := alike(r.shown(), typedIn)
			for name, text := range map[string]string{"stdout": stdout, "stderr": stderr} {
				if text = strings.TrimSpace(alike(text, byHand)); text != "" && !strings.Contains(shown, text) {
					t.Errorf("the person was not shown the terminal's %s.\nshown: %q\nwant:  %q", name, shown, text)
				}
			}
		})
	}
}

// AC-3: only this plugin's own commands, typed by a person, record anything.
// The genuine command is the control: without it every row below passes on a
// hook that handles nothing at all.
func TestAC3AnExpansionThatIsNotThisPluginsOwnTypedCommandRecordsNothing(t *testing.T) {
	t.Parallel()
	waiting := waitingProject

	t.Run("AC-3 control: /sdlc:approve from the plugin records the approval", func(t *testing.T) {
		root := waiting(t)
		before := files(t, root)
		calls := 0
		expand(t, typed(root, "session-b", "sdlc:approve", "A-1"), hookEnv(root, "session-b"), counted(&calls))
		if calls != 1 || len(sameFiles(before, files(t, root))) == 0 {
			t.Fatalf("the person's own /sdlc:approve ran %d commands and changed nothing", calls)
		}
	})

	forged := []struct {
		name  string
		event string
		edit  func(p map[string]any)
	}{
		{"AC-3 an MCP prompt", "UserPromptExpansion", func(p map[string]any) { p["expansion_type"] = "mcp_prompt" }},
		{"AC-3 an MCP prompt from an MCP server", "UserPromptExpansion", func(p map[string]any) {
			p["expansion_type"], p["command_source"] = "mcp_prompt", "mcp"
		}},
		{"AC-3 an expansion of no stated type", "UserPromptExpansion", func(p map[string]any) { delete(p, "expansion_type") }},
		{"AC-3 a project command of the same name", "UserPromptExpansion", func(p map[string]any) { p["command_source"] = "project" }},
		{"AC-3 a user command of the same name", "UserPromptExpansion", func(p map[string]any) { p["command_source"] = "user" }},
		{"AC-3 a command of no stated source", "UserPromptExpansion", func(p map[string]any) { delete(p, "command_source") }},
		{"AC-3 another plugin's approve", "UserPromptExpansion", func(p map[string]any) { p["command_name"] = "other:approve" }},
		{"AC-3 a name that only starts like it", "UserPromptExpansion", func(p map[string]any) { p["command_name"] = "sdlc:approve-all" }},
		{"AC-3 a name that only ends like it", "UserPromptExpansion", func(p map[string]any) { p["command_name"] = "notsdlc:approve" }},
		{"AC-3 another of the plugin's skills with an approval in the prompt", "UserPromptExpansion", func(p map[string]any) {
			p["command_name"], p["command_args"] = "sdlc:next", "A-1"
		}},
		{"AC-3 a subagent's expansion", "UserPromptExpansion", func(p map[string]any) { p["agent_type"] = "sdlc:researcher" }},
		{"AC-3 the same words as a prompt submitted", "UserPromptSubmit", func(p map[string]any) {
			p["hook_event_name"] = "UserPromptSubmit"
			for _, k := range []string{"expansion_type", "command_name", "command_args", "command_source"} {
				delete(p, k)
			}
		}},
	}
	for _, f := range forged {
		t.Run(f.name, func(t *testing.T) {
			root := waiting(t)
			before := files(t, root)
			payload := typed(root, "session-b", "sdlc:approve", "A-1")
			f.edit(payload)
			calls := 0
			expandEvent(t, f.event, payload, hookEnv(root, "session-b"), counted(&calls))
			if calls != 0 {
				t.Errorf("the hook ran %d commands for it", calls)
			}
			if diff := sameFiles(before, files(t, root)); len(diff) > 0 {
				t.Errorf("it changed the record:\n%s", strings.Join(diff, "\n"))
			}
		})
	}
}

// AC-4: a hook that fails on one of these commands records nothing, says the
// action did not run and what to do, and leaves the session going: exit 0 and
// one JSON answer, never a crash Claude Code reads as "carry on".
func TestAC4AHookThatFailsOnASlashCommandRecordsNothingAndSaysTheActionDidNotRun(t *testing.T) {
	t.Parallel()
	waiting := waitingProject
	panics := func(string, func(string) string, []string, io.Reader, io.Writer, io.Writer) int {
		panic("forced by the test: the command gave way before it wrote anything")
	}
	for _, c := range []struct {
		name, args string
		commands   commandRunner
		ran        bool
	}{
		{"AC-4 the command gives way", "A-1", panics, true},
		{"AC-4 arguments that cannot be read", `A-1 --reject "the quote is never closed`, cli.ExecuteIn, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := waiting(t)
			before := files(t, root)
			calls := 0
			commands := func(dir string, getenv func(string) string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
				calls++
				return c.commands(dir, getenv, args, stdin, stdout, stderr)
			}
			r := expand(t, typed(root, "session-b", "sdlc:approve", c.args), hookEnv(root, "session-b"), commands)
			if diff := sameFiles(before, files(t, root)); len(diff) > 0 {
				t.Errorf("a failed /sdlc:approve changed the record:\n%s", strings.Join(diff, "\n"))
			}
			if !c.ran && calls != 0 {
				t.Errorf("arguments that could not be read were run anyway, %d times", calls)
			}
			if !r.blocked() {
				t.Errorf("the failure was let through to the model as if nothing happened: %+v", r)
			}
			shown := r.shown()
			if !strings.Contains(shown, "did not run") {
				t.Errorf("the person was not told the action did not run: %q", shown)
			}
			if !strings.Contains(shown, "/sdlc:approve") {
				t.Errorf("the person was not told what to do next, with the command to type: %q", shown)
			}
			if strings.Contains(strings.ToLower(shown), "approved") {
				t.Errorf("the person was told of an approval that was not made: %q", shown)
			}
		})
	}
}

// AC-5: the words a person types reach the command as words. No shell reads
// them: nothing in them is run, expanded or redirected, and a quote groups a
// phrase into one word as it would on a command line.
func TestAC5TheTypedArgumentsReachTheCommandAsWordsAndNeverAShell(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, slash, args string
		want              []string
	}{
		{"AC-5 none", "sdlc:approve", "", []string{"approve"}},
		{"AC-5 a story", "sdlc:approve", "A-1", []string{"approve", "A-1"}},
		{"AC-5 white space around and between", "sdlc:approve", "  A-1 \t ", []string{"approve", "A-1"}},
		{"AC-5 a double-quoted phrase", "sdlc:unfreeze", `--reason "the test was wrong"`,
			[]string{"unfreeze", "--reason", "the test was wrong"}},
		{"AC-5 a single-quoted phrase", "sdlc:approve", `A-1 --reject 'not yet; see AC-2'`,
			[]string{"approve", "A-1", "--reject", "not yet; see AC-2"}},
		{"AC-5 a single quote inside double quotes", "sdlc:unfreeze", `--reason "AC-1's test was wrong"`,
			[]string{"unfreeze", "--reason", "AC-1's test was wrong"}},
		{"AC-5 a command after a semicolon", "sdlc:approve", "A-1; touch pwned", []string{"approve", "A-1;", "touch", "pwned"}},
		{"AC-5 a command after &&", "sdlc:approve", "A-1 && touch pwned", []string{"approve", "A-1", "&&", "touch", "pwned"}},
		{"AC-5 a pipe", "sdlc:ack", "--through HEAD | sh", []string{"ack", "--through", "HEAD", "|", "sh"}},
		{"AC-5 a substitution", "sdlc:approve", "$(touch pwned)", []string{"approve", "$(touch", "pwned)"}},
		{"AC-5 a substitution in double quotes", "sdlc:unfreeze", `--reason "$(touch pwned)"`,
			[]string{"unfreeze", "--reason", "$(touch pwned)"}},
		{"AC-5 backticks", "sdlc:approve", "`touch pwned`", []string{"approve", "`touch", "pwned`"}},
		{"AC-5 a variable", "sdlc:release", `A-1 --reason "$HOME"`, []string{"release", "A-1", "--reason", "$HOME"}},
		{"AC-5 a tilde and a glob", "sdlc:remap", "~ *", []string{"remap", "~", "*"}},
		{"AC-5 a redirection", "sdlc:approve", "A-1 > out.txt", []string{"approve", "A-1", ">", "out.txt"}},
		{"AC-5 assign runs staffing assign", "sdlc:assign", `security --model opus --effort high --reason "mine"`,
			[]string{"staffing", "assign", "security", "--model", "opus", "--effort", "high", "--reason", "mine"}},
		{"AC-5 remap with a flag", "sdlc:remap", "A-1 --replace", []string{"remap", "A-1", "--replace"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := loopProject(t)
			var got [][]string
			recorder := func(_ string, _ func(string) string, args []string, _ io.Reader, _, _ io.Writer) int {
				got = append(got, append([]string(nil), args...))
				return 0
			}
			expand(t, typed(root, "session-b", c.slash, c.args), hookEnv(root, "session-b"), recorder)
			if len(got) != 1 {
				t.Fatalf("/%s %s ran %d commands, want one: %q", c.slash, c.args, len(got), got)
			}
			if strings.Join(got[0], "\x00") != strings.Join(c.want, "\x00") {
				t.Errorf("/%s %s was handed to the command as %q, want %q", c.slash, c.args, got[0], c.want)
			}
		})
	}
}

// AC-5 with the real command: what a shell would have run is not run, and
// nothing is written where a shell would have written it.
func TestAC5ShellWordsTypedAfterASlashCommandRunNothingAndWriteNothing(t *testing.T) {
	t.Parallel()
	for _, args := range []string{
		"A-1; touch pwned",
		"A-1 && touch pwned",
		"$(touch pwned)",
		"`touch pwned`",
		"A-1 > pwned",
		`A-1 --reject "$(touch pwned)"`,
	} {
		t.Run("AC-5 "+args, func(t *testing.T) {
			root := waitingProject(t)
			calls := 0
			r := expand(t, typed(root, "session-b", "sdlc:approve", args), hookEnv(root, "session-b"), counted(&calls))
			if calls != 1 {
				t.Fatalf("/sdlc:approve %s ran %d commands, want one", args, calls)
			}
			// Where the session is, and where the test process is: a shell
			// started by the hook would have run in one of the two.
			for _, dir := range []string{root, "."} {
				if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
					t.Errorf("/sdlc:approve %s wrote pwned in %s: a shell read the words", args, dir)
				}
			}
			if !r.blocked() || strings.TrimSpace(r.shown()) == "" {
				t.Errorf("the person was not shown what the command said of it: %+v", r)
			}
		})
	}
}
