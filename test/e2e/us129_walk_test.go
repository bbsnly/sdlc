// US-129 AC-2, AC-3 and AC-6 through the built binary in a sandbox: a story
// worked from `sdlc start` to its landing as the runbook directs, the commands
// no gate runs, every hook event, and a second story stopped and released,
// with a counting git first on PATH for the binary's processes alone.
//
// The counting git logs, for each call, the process that started it ($PPID),
// the directory it runs in, the variables that point git at another index,
// directory or work tree, a checksum of everything on disk in the sandbox, and
// its arguments, and then runs the real git. A read-only query one process asks
// twice, with the same arguments in the same directory and the same variables,
// while the checksum did not move, is one whose answer nothing in between could
// have changed (AC-3). The checksum moves on any write in the sandbox, the
// process's own included, so a query asked again after one is never counted.
// The number of git processes a process starts itself is AC-6's measure; what
// git starts is git's, and is not counted.
//
// The test's own git (fixtures, commits made as the session) runs from its
// own PATH, which has no counting git on it.
//
// Helpers carry a u129 prefix.

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// u129Call is one process the walk started: the binary, or the launcher that
// execs it, which keeps its pid.
type u129Call struct {
	label          string
	pid, code      int
	stdout, stderr string
}

// u129Query is one git process the counting git saw.
type u129Query struct {
	pid, ppid       int
	dir, vars, disk string
	args            []string
}

type u129Walk struct {
	t                *testing.T
	binary, launcher string
	main, tree       string
	env              []string
	log              string
	calls            []u129Call
	// seen counts each label, so that the nth run of one command has a key
	// of its own: "gate dor pass #1".
	seen map[string]int
}

// u129Shared is the one walk the tests of this file read: it takes most of a
// minute, and nothing a test does changes it.
var u129Shared struct {
	once    sync.Once
	walk    *u129Walk
	queries map[int][]u129Query
	// written is what the walk printed and left on disk, read while the
	// sandbox is there: it goes with the first test that ran the walk.
	written string
}

func u129Walked(t *testing.T) (*u129Walk, map[int][]u129Query) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the counting git is a shell script and the launcher a POSIX one; macOS and Linux count AC-3 and AC-6")
	}
	u129Shared.once.Do(func() {
		w := u129NewWalk(t)
		w.walk()
		u129Shared.queries = w.queries()
		u129Shared.written = u129Transcript(w) + u129Formats(w)
		u129Shared.walk = w
	})
	if u129Shared.walk == nil {
		t.Fatal("the walk stopped in the first test of this file that ran it; that test's failure says why")
	}
	return u129Shared.walk, u129Shared.queries
}

func u129Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func u129NewWalk(t *testing.T) *u129Walk {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git on PATH to count")
	}
	binary := build(t)
	main := landRepository(t, binary)
	launcher, err := filepath.Abs(filepath.Join("..", "..", "plugin", "bin", "sdlc-hook"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	// One printf a call, so that two calls at once do not interleave.
	script := "#!/bin/sh\n" +
		"disk=$(cd " + u129Quote(main.dir) + " && { find . -path ./.git/objects -prune -o -type f -print0 | xargs -0 cksum; " +
		"find .git/objects -type f -print; } 2>/dev/null </dev/null | LC_ALL=C sort | cksum)\n" +
		"us=$(printf '\\037')\n" +
		"rec=\"$$$us$PPID$us$(pwd -P)$us${GIT_INDEX_FILE-}|${GIT_DIR-}|${GIT_WORK_TREE-}|${GIT_COMMON_DIR-}$us$disk\"\n" +
		"for a in \"$@\"; do rec=\"$rec$us$a\"; done\n" +
		"printf '%s\\036' \"$rec\" >> " + u129Quote(log) + "\n" +
		"exec " + u129Quote(gitPath) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// PATH holds the counting git and the four programs it runs, and nothing
	// else, so that nothing the binary prints depends on what the machine has
	// installed: doctor names the go and the sdlc it finds on PATH.
	tools := t.TempDir()
	for _, name := range []string{"find", "xargs", "cksum", "sort"} {
		found, err := exec.LookPath(name)
		if err != nil {
			t.Skip("the counting git needs " + name + " on PATH")
		}
		if err := os.Symlink(found, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "PATH=") })
	env = append(env, "PATH="+dir+string(os.PathListSeparator)+tools)
	return &u129Walk{t: t, binary: binary, launcher: launcher, main: main.dir, env: env, log: log, seen: map[string]int{}}
}

// key is the label with its run number.
func (w *u129Walk) key(label string) string {
	w.seen[label]++
	return label + " #" + strconv.Itoa(w.seen[label])
}

// spelt is text with the sandbox's paths named, so that a label reads the same
// in every run.
func (w *u129Walk) spelt(text string) string {
	if w.tree != "" {
		text = strings.ReplaceAll(text, w.tree, "<tree>")
	}
	return strings.ReplaceAll(text, w.main, "<main>")
}

func (w *u129Walk) start(cmd *exec.Cmd, label, stdin string) u129Call {
	w.t.Helper()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	c := u129Call{label: w.key(label), stdout: out.String(), stderr: errOut.String()}
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		c.code = exit.ExitCode()
	default:
		w.t.Fatalf("%s could not be run: %v", label, err)
	}
	c.pid = cmd.Process.Pid
	w.calls = append(w.calls, c)
	return c
}

// sdlc runs one command in dir, from session ("" for a person).
func (w *u129Walk) sdlc(dir, session, stdin string, args ...string) u129Call {
	w.t.Helper()
	cmd := exec.CommandContext(w.t.Context(), w.binary, args...)
	cmd.Dir = dir
	cmd.Env = slices.Clone(w.env)
	if session != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CODE_SESSION_ID="+session)
	}
	c := w.start(cmd, strings.Join(args, " "), stdin)
	if c.code == 0 {
		noteRun(args...)
	}
	return c
}

// works is sdlc for a command that has to work.
func (w *u129Walk) works(dir, session, stdin string, args ...string) u129Call {
	w.t.Helper()
	c := w.sdlc(dir, session, stdin, args...)
	if c.code != 0 {
		w.t.Fatalf("sdlc %s in %s exited %d:\n%s%s", strings.Join(args, " "), dir, c.code, c.stdout, c.stderr)
	}
	return c
}

// hook hands the launcher one event, as hooks.json does, from a session opened
// in project.
func (w *u129Walk) hook(project, event string, payload map[string]any) reply {
	w.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		w.t.Fatal(err)
	}
	cmd := exec.CommandContext(w.t.Context(), w.launcher, event)
	cmd.Dir = project
	cmd.Env = append(slices.Clone(w.env), "SDLC_BIN="+w.binary, "CLAUDE_PROJECT_DIR="+project)
	label := "hook " + event
	if tool, ok := payload["tool_name"].(string); ok {
		label += " " + tool
	}
	if agent, ok := payload["agent_type"].(string); ok {
		label += " [" + agent + "]"
	}
	if input, ok := payload["tool_input"].(map[string]any); ok {
		if v, ok := input["command"].(string); ok {
			label += ": " + w.spelt(v)
		} else if v, ok := input["file_path"].(string); ok {
			label += ": " + w.spelt(v)
		}
	}
	c := w.start(cmd, label, string(raw))
	if c.code != 0 {
		w.t.Fatalf("the launcher exited %d for %s:\n%s", c.code, label, c.stderr)
	}
	noteHook()
	var r reply
	if strings.TrimSpace(c.stdout) != "" {
		if err := json.Unmarshal([]byte(c.stdout), &r); err != nil {
			w.t.Fatalf("%s: the hook wrote something that is not JSON: %v\n%q", label, err, c.stdout)
		}
	}
	return r
}

// ask is a PreToolUse call from the first session, opened in the main checkout.
func (w *u129Walk) ask(session, agent, tool string, input map[string]any) reply {
	w.t.Helper()
	payload := map[string]any{
		"hook_event_name": "PreToolUse", "session_id": session, "tool_name": tool, "cwd": w.main, "tool_input": input,
	}
	if agent != "" {
		payload["agent_type"] = "sdlc:" + agent
	}
	return w.hook(w.main, "PreToolUse", payload)
}

func (w *u129Walk) do(agent, tool string, input map[string]any) {
	w.t.Helper()
	if r := w.ask(sessionFirst, agent, tool, input); denied(r) {
		w.t.Fatalf("the hook refused %s %v: %s", tool, input, r.HookSpecificOutput.PermissionDecisionReason)
	}
}

func (w *u129Walk) inTree(command string) string {
	return "cd " + strconv.Quote(w.tree) + " && " + command
}

// run is a command the runbook runs in the worktree: the hook asked, then the
// command.
func (w *u129Walk) run(agent, stdin string, args ...string) {
	w.t.Helper()
	w.do(agent, "Bash", running(w.inTree("sdlc "+strings.Join(args, " "))))
	w.works(w.tree, sessionFirst, stdin, args...)
}

// tries is run for a command whose exit code is part of what is recorded,
// not a condition of the walk.
func (w *u129Walk) tries(args ...string) {
	w.t.Helper()
	w.do("", "Bash", running(w.inTree("sdlc "+strings.Join(args, " "))))
	w.sdlc(w.tree, sessionFirst, "", args...)
}

func (w *u129Walk) writes(agent, rel, body string) {
	w.t.Helper()
	path := filepath.Join(w.tree, filepath.FromSlash(rel))
	w.do(agent, "Write", map[string]any{"file_path": path, "content": body})
	writeFile(w.t, path, body)
}

func (w *u129Walk) nextGate() string {
	w.t.Helper()
	var status struct {
		Active   string `json:"active"`
		NextGate string `json:"next_gate"`
	}
	if err := json.Unmarshal([]byte(w.works(w.tree, sessionFirst, "", "status", "--json").stdout), &status); err != nil {
		w.t.Fatal(err)
	}
	if status.Active == "" {
		return ""
	}
	return status.NextGate
}

// workGate is treeSession.workGate, through this walk's processes.
func (w *u129Walk) workGate(gate model.Gate) {
	w.t.Helper()
	switch gate {
	case model.GateDoR, model.GateDesignReview, model.GateVerifierReview, model.GateCodeReview:
	case model.GateAnalysis:
		w.run("researcher", "# Analysis\n\nAC-1 is testable.\n", "artifact", "write", "analysis")
		w.run("researcher", "# Threats\n\nNothing crosses a trust boundary.\n", "artifact", "write", "threats")
	case model.GateTestsFrozen:
		w.writes("sdet", "invoice_test.go", "package main\n\n// AC-1\n")
		w.run("sdet", "# Test plan\n\nAC-1 -> TestRejectsZero\n", "artifact", "write", "test_plan")
		w.run("sdet", "", "freeze")
	case model.GatePlan:
		if r := w.ask(sessionFirst, "implementer", "Edit", editing(filepath.Join(w.tree, "invoice_test.go"))); !denied(r) {
			w.t.Fatal("the implementer was let edit the frozen test")
		}
		w.run("implementer", "# Plan\n\nOne step.\n", "artifact", "write", "plan")
	case model.GateImplementation:
		w.writes("implementer", "invoice.go", "package main\n\nfunc Invoice() {}\n")
	case model.GateVerification:
		w.run("verifier", "# Verification\n\nEvery command green.\n", "artifact", "write", "verification")
	case model.GateCommit:
		git(w.t, w.tree, "add", "-A")
		w.do("", "Bash", running(w.inTree("git commit -m US-001")))
		git(w.t, w.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "US-001")
	case model.GateRetro:
		w.run("bookkeeper", "# Retro\n\nNo deviations.\n", "artifact", "write", "retro")
	}
	for _, r := range fixtureReviewers(gate) {
		if r.WhenDoRAdvocate {
			continue
		}
		w.run(r.Role, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
	}
	w.run("", "", "gate", string(gate), "pass", "--note", "worked by the session")
}

func (w *u129Walk) walk() {
	t := w.t
	t.Helper()
	w.do("", "Bash", running("sdlc start --json"))
	w.works(w.main, sessionFirst, "", "start", "--json")
	w.tree = storyPath(checkout{dir: w.main}, "US-001")
	if !exists(w.tree) {
		t.Fatalf("start made no worktree at %s", w.tree)
	}
	for gate := w.nextGate(); gate != ""; gate = w.nextGate() {
		w.workGate(model.Gate(gate))
	}

	// The commands no gate runs, and the hook's other events, on the
	// finished story before it lands.
	w.run("", "", "story", "list")
	w.tries("staffing", "show")
	w.run("", "", "review", "list")
	w.run("", "", "cost")
	w.run("", "", "trace", "US-001")
	w.tries("doctor")
	for _, payload := range []map[string]any{
		{"hook_event_name": "PostToolUse", "tool_name": "Bash", "duration_ms": 2000,
			"tool_input":    map[string]any{"command": w.inTree("go test ./...")},
			"tool_response": map[string]any{"stdout": "ok", "stderr": "", "interrupted": false}},
		{"hook_event_name": "PostToolUse", "tool_name": "Write",
			"tool_input":    map[string]any{"file_path": filepath.Join(w.tree, "notes.md"), "content": "x\n"},
			"tool_response": map[string]any{"filePath": filepath.Join(w.tree, "notes.md"), "success": true}},
		{"hook_event_name": "SubagentStart", "agent_type": "sdlc:bookkeeper", "agent_id": "u129-1"},
		{"hook_event_name": "SubagentStop", "agent_type": "sdlc:bookkeeper", "agent_id": "u129-1",
			"stop_hook_active": false, "last_assistant_message": `{"status":"done"}`},
		{"hook_event_name": "Stop", "stop_hook_active": false},
		{"hook_event_name": "SessionStart", "source": "startup"},
	} {
		payload["session_id"], payload["cwd"] = sessionFirst, w.main
		event, _ := payload["hook_event_name"].(string)
		w.hook(w.main, event, payload)
	}

	// The bookkeeper's CODEMAP.md after the retro, then the landing.
	w.writes("bookkeeper", "CODEMAP.md", "# CODEMAP\n\ninvoice.go keeps invoices.\n")
	w.do("", "Bash", running(w.inTree("git add CODEMAP.md && git commit -m 'docs: map US-001'")))
	git(t, w.tree, "add", "--", "CODEMAP.md")
	git(t, w.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "docs: map US-001")
	tip := headOf(t, w.tree)
	w.run("", "", "stop")
	if got := landTrunk(t, checkout{dir: w.main}); got != tip {
		t.Fatalf("trunk is at %s after the stop, want the story's tip %s", got, tip)
	}

	// A second story, stopped before it is finished and let go by a person.
	w.works(w.main, sessionSecond, "", "start", "--json")
	second := storyPath(checkout{dir: w.main}, "US-002")
	w.tree = second
	w.works(second, sessionSecond, "", "stop")
	w.works(w.main, "", "", "release", "US-002", "--reason", "to be worked in another checkout")
}

// queries is every git process the counting git saw, by the process that
// started it.
func (w *u129Walk) queries() map[int][]u129Query {
	w.t.Helper()
	raw, err := os.ReadFile(w.log)
	if err != nil {
		w.t.Fatal(err)
	}
	out := map[int][]u129Query{}
	for rec := range strings.SplitSeq(string(raw), "\036") {
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\037")
		if len(f) < 5 {
			w.t.Fatalf("the counting git logged %q", rec)
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			w.t.Fatalf("the counting git logged %q", rec)
		}
		ppid, err := strconv.Atoi(f[1])
		if err != nil {
			w.t.Fatalf("the counting git logged %q", rec)
		}
		out[ppid] = append(out[ppid], u129Query{pid: pid, ppid: ppid, dir: f[2], vars: f[3], disk: f[4], args: f[5:]})
	}
	return out
}

// u129Subcommand is git's subcommand and what follows it, past the options
// git takes before it.
func u129Subcommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-C" || a == "-c":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a, args[i+1:]
		}
	}
	return "", nil
}

// u129Sealed is args with the private hooks directory of a sealed call
// (internal/gitx/landing.go) spelt the same for every call. Each call makes a
// directory of its own, empty, so that no hook of the repository runs; a hook
// runs only for a command that writes, so it is no part of a query's answer,
// and two sealed calls asking one thing ask the same query.
func u129Sealed(args []string) []string {
	out := slices.Clone(args)
	for i, a := range out {
		if strings.HasPrefix(a, "core.hooksPath=") && i > 0 && out[i-1] == "-c" {
			out[i] = "core.hooksPath=<private>"
		}
	}
	return out
}

// u129ReadOnly is whether a git command only reads: a query, which AC-3 is
// about, and not a command that writes, whose second run is a second write.
func u129ReadOnly(args []string) bool {
	sub, rest := u129Subcommand(args)
	var positional []string
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
		}
	}
	has := func(flags ...string) bool {
		return slices.ContainsFunc(rest, func(a string) bool {
			return slices.ContainsFunc(flags, func(f string) bool { return a == f || strings.HasPrefix(a, f+"=") })
		})
	}
	switch sub {
	case "rev-parse", "show-ref", "rev-list", "log", "cat-file", "merge-base", "ls-tree", "ls-files",
		"diff-tree", "diff-index", "diff-files", "status", "for-each-ref", "show", "describe", "name-rev",
		"var", "check-ignore", "check-attr", "version", "grep", "cherry", "count-objects", "check-ref-format":
		return true
	case "diff":
		return !has("--output")
	case "symbolic-ref":
		return !has("-d", "--delete") && len(positional) <= 1
	case "config":
		return has("--get", "--get-all", "--get-regexp", "--list", "-l", "--get-urlmatch") ||
			(len(positional) > 0 && (positional[0] == "get" || positional[0] == "list"))
	case "worktree":
		return len(positional) > 0 && positional[0] == "list"
	case "branch":
		return has("--show-current", "--list")
	case "hash-object":
		return !has("-w")
	}
	return false
}

// u129Commands is every command AC-3 names, by the words a label starts
// with, and every hook event.
var u129Commands = []string{
	"start", "status", "story list", "staffing", "artifact write", "review add", "review list", "gate",
	"freeze", "cost", "trace", "stop", "release", "doctor",
	"hook PreToolUse Bash", "hook PreToolUse Write", "hook PreToolUse Edit", "hook PostToolUse",
	"hook SubagentStart", "hook SubagentStop", "hook Stop", "hook SessionStart",
}

// TestAC3TheWalkRanEveryCommandAndHookEventItCounts holds the walk to its
// cover: a walk that stopped running a command would count nothing for it.
func TestAC3TheWalkRanEveryCommandAndHookEventItCounts(t *testing.T) {
	parallel(t)
	w, _ := u129Walked(t)
	for _, want := range u129Commands {
		if !slices.ContainsFunc(w.calls, func(c u129Call) bool {
			return c.label == want || strings.HasPrefix(c.label, want+" ") || strings.HasPrefix(c.label, want+":")
		}) {
			t.Errorf("AC-3 the walk ran no %s", want)
		}
	}
}

func TestAC3NoSdlcProcessAsksGitTheSameReadOnlyQueryTwiceWhileNothingCouldChangeTheAnswer(t *testing.T) {
	parallel(t)
	w, byPID := u129Walked(t)
	for _, c := range w.calls {
		first := map[string]string{}
		told := map[string]bool{}
		for _, q := range byPID[c.pid] {
			if !u129ReadOnly(q.args) {
				continue
			}
			key := q.dir + "\x00" + q.vars + "\x00" + strings.Join(u129Sealed(q.args), "\x00")
			if disk, ok := first[key]; ok && disk == q.disk && !told[key] {
				told[key] = true
				t.Errorf("AC-3 %s asked git %q twice in %s, with nothing on disk changed in between",
					c.label, w.spelt(strings.Join(u129Sealed(q.args), " ")), w.spelt(q.dir))
			}
			first[key] = q.disk
		}
	}
}

// u129GitCounts is the number of git processes each process of the walk
// starts, by its key.
func u129GitCounts(w *u129Walk, byPID map[int][]u129Query) map[string]int {
	counts := map[string]int{}
	for _, c := range w.calls {
		if n := len(byPID[c.pid]); n > 0 {
			counts[c.label] = n
		}
	}
	return counts
}

func TestAC6NoProcessOfTheWalkStartsMoreGitThanAtTheBaseAndAllOfThemAtMostHalf(t *testing.T) {
	parallel(t)
	w, byPID := u129Walked(t)
	counts := u129GitCounts(w, byPID)
	got, base := 0, 0
	for _, n := range u129BaseGitCounts {
		base += n
	}
	for _, c := range w.calls {
		n := counts[c.label]
		got += n
		if n > 0 {
			// The figures the retro records, with -v.
			t.Logf("%q: %d (base %d)", c.label, n, u129BaseGitCounts[c.label])
		}
		if want := u129BaseGitCounts[c.label]; n > want {
			t.Errorf("AC-6 %s started %d git processes, more than the %d it started at the base", c.label, n, want)
		}
	}
	if got*2 > base {
		t.Errorf("AC-6 the walk started %d git processes in all, want at most half the %d it started at the base, %d",
			got, base, base/2)
	}
}

// u129BaseGitCounts is what each process of the walk started at 37e3ac8, the
// story's base, measured by this file on macOS (Gate 3); a process not named
// started none. 185 in all, so the walk may start 92 at most.
var u129BaseGitCounts = map[string]int{
	"start --json #1": 7,
	"gate dor pass --note worked by the session #1":      3,
	"gate analysis pass --note worked by the session #1": 3,
	"freeze #1": 4,
	"gate tests_frozen pass --note worked by the session #1":       5,
	"gate plan pass --note worked by the session #1":               5,
	"gate design_review pass --note worked by the session #1":      3,
	"gate implementation pass --note worked by the session #1":     5,
	"gate verification pass --note worked by the session #1":       5,
	"review add verifier_review verifier approve #1":               5,
	"gate verifier_review pass --note worked by the session #1":    8,
	"review add code_review code-reviewer approve #1":              5,
	"review add code_review security approve #1":                   5,
	"review add code_review perf approve #1":                       5,
	"review add code_review human-advocate approve #1":             5,
	"gate code_review pass --note worked by the session #1":        8,
	`hook PreToolUse Bash: cd "<tree>" && git commit -m US-001 #1`: 7,
	"gate commit pass --note worked by the session #1":             26,
	"gate retro pass --note worked by the session #1":              4,
	"review list #1": 5,
	"doctor #1":      8,
	`hook PreToolUse Bash: cd "<tree>" && git add CODEMAP.md && git commit -m 'docs: map US-001' #1`: 11,
	"stop #1":         36,
	"start --json #2": 7,
}

// u129Transcript is what every process of the walk printed, and its exit
// code, with what changes from one run to the next spelt as a word in angle
// brackets.
func u129Transcript(w *u129Walk) string {
	var b strings.Builder
	for _, c := range w.calls {
		fmt.Fprintf(&b, "=== %s: exit %d\n%s", c.label, c.code, w.normal(c.stdout))
		if c.stderr != "" {
			fmt.Fprintf(&b, "--- stderr\n%s", w.normal(c.stderr))
		}
	}
	return b.String()
}

var (
	u129Hash = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	u129Time = regexp.MustCompile(`\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)`)
	u129Date = regexp.MustCompile(`\b\d{4}-\d\d-\d\d\b`)
	u129Took = regexp.MustCompile(`\b(\d+(\.\d+)?(ns|µs|us|ms|s|m|h))+\b`)
	u129Gap  = regexp.MustCompile(`[ \t]+`)
	// A row of trace's table of time by gate holds durations alone, and
	// which gates have a row depends on how long the walk took to reach them.
	u129TimeRow = regexp.MustCompile(`^ [a-z_ ]+( <took>)+\n?$`)
)

// normal is text with what changes from one run to the next spelt as a word
// in angle brackets: the sandbox's paths, commit and tree hashes, times and
// dates, and how long something took. A line that held a duration has its
// columns closed up, since a wider figure moves them.
func (w *u129Walk) normal(text string) string {
	text = w.spelt(text)
	text = u129Hash.ReplaceAllString(text, "<hash>")
	text = u129Time.ReplaceAllString(text, "<time>")
	text = u129Date.ReplaceAllString(text, "<date>")
	lines := strings.SplitAfter(text, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if u129Took.MatchString(line) {
			line = u129Gap.ReplaceAllString(u129Took.ReplaceAllString(line, "<took>"), " ")
			if u129TimeRow.MatchString(line) {
				continue
			}
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "")
}

// u129Formats is every file the walk left under .sdlc/ and .git/sdlc/ of the
// main checkout, normalised as the transcript is. A story's worktree under
// .sdlc/worktrees is a checkout of the project, not the loop's state.
func u129Formats(w *u129Walk) string {
	var b strings.Builder
	for _, top := range []string{".sdlc", filepath.Join(".git", "sdlc")} {
		err := filepath.WalkDir(filepath.Join(w.main, top), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(w.main, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			switch {
			case d.IsDir() && rel == ".sdlc/worktrees":
				return filepath.SkipDir
			case d.IsDir():
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "=== file %s\n%s\n", rel, w.normal(string(raw)))
			return nil
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			w.t.Fatal(err)
		}
	}
	return b.String()
}

// u129Golden is the walk's transcript and files as the base printed and
// wrote them, captured at 37e3ac8 with U129_WRITE_GOLDEN=1 before the freeze. US-112.4
// added only the test reviewer the fixture disables (decision D1): the line every
// gate and status print of it, its row in review list and doctor, its entry in the
// configuration and the status --json disabled list.
const u129Golden = "testdata/us129_walk.golden"

// u129PauseKey is a JSON key of what US-133 added: pre_commit_pause in status,
// pre_commit_pause_paths in the configuration, and whatever the record or the
// log calls the landing without a pause.
var u129PauseKey = regexp.MustCompile(`"[a-z_]*pause[a-z_]*":\s*`)

// u129WithoutCommitPause is the walk with what US-133 added taken out of both
// sides: US-133 changes what status, the configuration init writes, the record
// and doctor say about the commit's pause on purpose, and us133_test.go and
// internal/cli's commitpause_us133_test.go hold it. In that order: the
// _human_gates comment init writes, whose wording is the template's; every
// value under a key that names the pause, with the comma that joined it to its
// object; every event of the kind US-133 adds; then every line of prose about
// the pause, which in this walk, with no path listed, names it.
func u129WithoutCommitPause(walk string) string {
	walk = regexp.MustCompile(`(?s)\n  "_human_gates": \[\n.*?\n  \],`).ReplaceAllString(walk, "")
	for from := 0; ; {
		loc := u129PauseKey.FindStringIndex(walk[from:])
		if loc == nil {
			break
		}
		start, end := from+loc[0], u129ValueEnd(walk, from+loc[1])
		trimmed := strings.TrimRight(walk[:start], " \t\r\n")
		if strings.HasSuffix(trimmed, ",") {
			start = len(trimmed) - 1
		} else if rest := strings.TrimLeft(walk[end:], " \t\r\n"); strings.HasPrefix(rest, ",") {
			end = len(walk) - len(rest) + 1
			end += len(walk[end:]) - len(strings.TrimLeft(walk[end:], " \t\r\n"))
		}
		walk = walk[:start] + walk[end:]
		from = start
	}
	for _, gone := range []*regexp.Regexp{
		regexp.MustCompile(`\{[^{}]*landed_without_pause[^{}]*\},\s*`),
		regexp.MustCompile(`,\s*\{[^{}]*landed_without_pause[^{}]*\}`),
		regexp.MustCompile(`(?m)^.*landed_without_pause.*\n`),
		// With the blank line before it, where the pause is a block of its own.
		regexp.MustCompile(`(?mi)(^\n)?^[^=\n].*pause.*\n`),
	} {
		walk = gone.ReplaceAllString(walk, "")
	}
	return walk
}

// u129ValueEnd is where the JSON value that starts at from ends: past its
// closing bracket, string or word.
func u129ValueEnd(text string, from int) int {
	depth, inString := 0, false
	for i := from; i < len(text); i++ {
		c := text[i]
		switch {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
			if !inString && depth == 0 {
				return i + 1
			}
		case inString:
		case c == '[' || c == '{':
			depth++
		case c == ']' || c == '}':
			depth--
			if depth == 0 {
				return i + 1
			}
			if depth < 0 {
				return i
			}
		case depth == 0 && (c == ',' || c == '\n'):
			return i
		}
	}
	return len(text)
}

// AC-2: every command of the walk prints what it printed at the base and
// exits as it did, the hook answers each call as it did, and the files under
// .sdlc/ and .git/sdlc/ are written as they were. It passes at the base by
// construction; the base suite, unchanged (AC-1), holds the rest.
func TestAC2TheWalkPrintsExitsAnswersAndWritesWhatItDidAtTheBase(t *testing.T) {
	parallel(t)
	u129Walked(t)
	got := u129Shared.written
	if os.Getenv("U129_WRITE_GOLDEN") != "" {
		writeFile(t, u129Golden, got)
		return
	}
	raw, err := os.ReadFile(u129Golden)
	if err != nil {
		t.Fatal(err)
	}
	got, want := u129WithoutCommitPause(got), u129WithoutCommitPause(string(raw))
	if got == want {
		return
	}
	g, wl := strings.SplitAfter(got, "\n"), strings.SplitAfter(want, "\n")
	for i := range max(len(g), len(wl)) {
		var gl, wantLine string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(wl) {
			wantLine = wl[i]
		}
		if gl != wantLine {
			from := max(0, i-6)
			t.Fatalf("AC-2 the walk's transcript differs from the base's at line %d of %s:\n got %q\nwant %q\nbefore it:\n%s",
				i+1, u129Golden, gl, wantLine, strings.Join(wl[from:i], ""))
		}
	}
}
