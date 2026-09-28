// Checkouts: every worktree git knows of one repository works one backlog, one
// record of each story and one claim on it (US-029.1).
//
// Before this story each checkout was a project of its own to sdlc: its own
// backlog, its own gate records, its own lock. A second Claude Code session in
// a worktree made by hand was handed the story the first was working, a status
// changed in one checkout was never seen in the other, and the hook held a
// session only in the checkout it started in, so the records it guards were
// out of its reach from the next directory along.
//
// The worktrees here are made with `git worktree add`, as a person makes one:
// making them is US-029.2's. Each is on a branch of its own, and its own
// .sdlc/config.json names that branch as trunk, because each checkout keeps
// reading its own configuration and a new story starts only on trunk.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
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
)

// The Claude Code sessions the tests hold stories in. One session holds one
// story, so a story held in each of two checkouts needs two.
const (
	sessionMain     = "e2e-main-session"
	sessionWorktree = "e2e-worktree-session"
	sessionThird    = "e2e-third-session"
	sessionUnheld   = "e2e-unheld-session"
)

// checkout is one working tree of a repository, and the Claude Code session its
// commands run in. No session is a person at a terminal.
type checkout struct {
	binary  string
	dir     string
	session string
}

// as is the same checkout, used from another session, or by a person with "".
func (c checkout) as(session string) checkout {
	c.session = session
	return c
}

func (c checkout) command(t *testing.T, stdin string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), c.binary, args...)
	cmd.Dir = c.dir
	cmd.Env = os.Environ()
	if c.session != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CODE_SESSION_ID="+c.session)
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	return cmd
}

// outcome runs a command and returns each stream and the exit code.
func (c checkout) outcome(t *testing.T, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := c.command(t, stdin, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("sdlc %s could not be run: %v", strings.Join(args, " "), err)
	}
	return out.String(), errOut.String(), code
}

// run is a command that has to work. It returns what it printed on stdout.
func (c checkout) run(t *testing.T, args ...string) string {
	t.Helper()
	return c.runWith(t, "", args...)
}

func (c checkout) runWith(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	stdout, stderr, code := c.outcome(t, stdin, args...)
	if code != 0 {
		t.Fatalf("sdlc %s in %s exited %d:\n%s%s", strings.Join(args, " "), c.dir, code, stdout, stderr)
	}
	return stdout
}

// try is a command that may fail: both streams together, and the exit code.
func (c checkout) try(t *testing.T, args ...string) (string, int) {
	t.Helper()
	return c.tryWith(t, "", args...)
}

func (c checkout) tryWith(t *testing.T, stdin string, args ...string) (string, int) {
	t.Helper()
	stdout, stderr, code := c.outcome(t, stdin, args...)
	return stdout + stderr, code
}

// document runs a command with --json and returns what it printed, parsed.
func (c checkout) document(t *testing.T, args ...string) map[string]any {
	t.Helper()
	var doc map[string]any
	out := c.run(t, append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("sdlc %s --json printed something that is not JSON: %v\n%s", strings.Join(args, " "), err, out)
	}
	return doc
}

// started is the story `sdlc start` began or resumed here.
func (c checkout) started(t *testing.T, args ...string) string {
	t.Helper()
	id, _ := c.document(t, append([]string{"start"}, args...)...)["story"].(string)
	return id
}

// analysed takes the story this checkout works through the two gates before
// the freeze.
func (c checkout) analysed(t *testing.T) {
	t.Helper()
	c.run(t, "gate", "dor", "pass", "--note", "criteria are testable")
	c.runWith(t, "# Analysis\n", "artifact", "write", "analysis")
	c.runWith(t, "# Threats\n", "artifact", "write", "threats")
	c.run(t, "gate", "analysis", "pass", "--security-sensitive=false")
}

// frozen writes an acceptance test in this checkout and freezes it at Gate 3.
func (c checkout) frozen(t *testing.T, test string) {
	t.Helper()
	c.analysed(t)
	if err := os.WriteFile(filepath.Join(c.dir, test), []byte("package main\n\n// AC-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c.runWith(t, "# Test plan\n", "artifact", "write", "test_plan")
	c.run(t, "freeze")
	c.run(t, "gate", "tests_frozen", "pass", "--note", "1 criterion, 1 failing test")
}

func (c checkout) reviewed(t *testing.T, gate string, roles ...string) {
	t.Helper()
	for _, role := range roles {
		c.runWith(t, "# "+role+"\n", "review", "add", gate, role, "approve")
	}
}

// walked takes the story from the start up to, but not including, its commit.
func (c checkout) walked(t *testing.T, test string) {
	t.Helper()
	c.frozen(t, test)
	c.runWith(t, "# Plan\n", "artifact", "write", "plan")
	c.run(t, "gate", "plan", "pass", "--note", "one step")
	c.reviewed(t, "design_review", "architect", "red-team", "security", "perf", "human-advocate")
	c.run(t, "gate", "design_review", "pass", "--note", "architect approved")
	c.run(t, "gate", "implementation", "pass", "--note", "frozen tests green")
	c.runWith(t, "# Verification\n", "artifact", "write", "verification")
	c.run(t, "gate", "verification", "pass", "--note", "all commands green")
	c.reviewed(t, "verifier_review", "verifier")
	c.run(t, "gate", "verifier_review", "pass", "--note", "no gaming found")
	c.reviewed(t, "code_review", "code-reviewer", "security", "perf", "human-advocate")
	c.run(t, "gate", "code_review", "pass", "--note", "reviewer approved")
}

// committed commits everything in the checkout and passes the commit gate.
func (c checkout) committed(t *testing.T, message string) {
	t.Helper()
	commitEverything(t, c.dir, message)
	c.run(t, "gate", "commit", "pass", "--note", "on trunk")
}

func (c checkout) retro(t *testing.T) {
	t.Helper()
	c.runWith(t, "# Retro\n", "artifact", "write", "retro")
	c.run(t, "gate", "retro", "pass", "--note", "no deviations")
}

func (c checkout) backlog() string { return filepath.Join(c.dir, "user_stories.json") }

func (c checkout) record(id string) string {
	return filepath.Join(c.dir, ".sdlc", "stories", id, "gate-record.json")
}

// spec is a story of a backlog made for a test: the example story sdlc init
// writes, under another id.
type spec struct {
	id        string
	priority  int
	dependsOn []string
}

// repository is a project with the stories given, committed, and nothing
// started: its main checkout, used by a person.
func repository(t *testing.T, binary string, stories ...spec) checkout {
	t.Helper()
	root := project(t, binary)
	if len(stories) > 0 {
		setStories(t, filepath.Join(root, "user_stories.json"), stories...)
		commitEverything(t, root, "the backlog")
	}
	return checkout{binary: binary, dir: root}
}

func setStories(t *testing.T, path string, stories ...spec) {
	t.Helper()
	var backlog map[string]any
	readJSON(t, path, &backlog)
	list, _ := backlog["stories"].([]any)
	if len(list) == 0 {
		t.Fatalf("%s has no story to copy", path)
	}
	template, _ := list[0].(map[string]any)
	out := make([]any, 0, len(stories))
	for _, s := range stories {
		next := maps.Clone(template)
		next["id"] = s.id
		next["title"] = "Story " + s.id
		next["priority"] = s.priority
		next["status"] = "ready"
		delete(next, "depends_on")
		if len(s.dependsOn) > 0 {
			next["depends_on"] = s.dependsOn
		}
		out = append(out, next)
	}
	backlog["stories"] = out
	writeJSON(t, path, backlog)
}

// setStatus is a person editing a story's status in the backlog by hand.
func setStatus(t *testing.T, path, id, status string) {
	t.Helper()
	var backlog map[string]any
	readJSON(t, path, &backlog)
	for _, s := range backlog["stories"].([]any) {
		if story := s.(map[string]any); story["id"] == id {
			story["status"] = status
		}
	}
	writeJSON(t, path, backlog)
}

// statuses is each story's status in the backlog at path.
func statuses(t *testing.T, path string) map[string]string {
	t.Helper()
	var backlog struct {
		Stories []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"stories"`
	}
	readJSON(t, path, &backlog)
	out := map[string]string{}
	for _, s := range backlog.Stories {
		out[s.ID] = s.Status
	}
	return out
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return string(raw)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func commitEverything(t *testing.T, dir, message string) {
	t.Helper()
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit", "--quiet", "--allow-empty", "-m", message)
}

// tempDir is a directory for the test, by the name the operating system
// resolves it to, so that a path sdlc prints can be compared with it.
func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

// worktree adds a linked worktree of main on a branch of its own, and makes
// that branch the worktree's trunk in its own configuration.
func worktree(t *testing.T, main checkout, name string) checkout {
	t.Helper()
	dir := filepath.Join(tempDir(t), name)
	git(t, main.dir, "worktree", "add", "--quiet", "-b", name, dir)
	if exists(filepath.Join(dir, ".sdlc", "config.json")) {
		trunkIs(t, dir, name)
	}
	return checkout{binary: main.binary, dir: dir}
}

func trunkIs(t *testing.T, dir, branch string) {
	t.Helper()
	path := filepath.Join(dir, ".sdlc", "config.json")
	var cfg map[string]any
	readJSON(t, path, &cfg)
	section, _ := cfg["git"].(map[string]any)
	if section == nil {
		section = map[string]any{}
	}
	section["trunk_branch"] = branch
	cfg["git"] = section
	writeJSON(t, path, cfg)
}

// files is every regular file under root, by slash-separated path relative to
// it, with its content. Git's object store is left out: it only ever grows, and
// nothing the loop keeps is in it. A root that does not exist has no files.
func files(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() && d.Name() == "objects" && filepath.Base(filepath.Dir(p)) == ".git" {
			return filepath.SkipDir
		}
		if !d.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// loopState is what the loop keeps in a checkout: everything under .sdlc/,
// and the backlog.
func loopState(t *testing.T, c checkout) map[string]string {
	t.Helper()
	state := files(t, filepath.Join(c.dir, ".sdlc"))
	if exists(c.backlog()) {
		state["<backlog>"] = readFile(t, c.backlog())
	}
	return state
}

// changedNaming is every file new or changed between two listings whose
// content names path: the record of a claim, which is the one thing the loop
// stores that names a checkout.
func changedNaming(before, after map[string]string, path string) []string {
	var out []string
	for name, body := range after {
		if old, ok := before[name]; ok && old == body {
			continue
		}
		if names(body, path) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// names is whether text names the path, as it is or as sdlc quotes it.
func names(text, path string) bool {
	quoted := strconv.Quote(path)
	return strings.Contains(text, path) || strings.Contains(text, quoted[1:len(quoted)-1])
}

// namesHolder is whether a JSON document ties the story to the checkout at
// path: an object that has the story's id among its values and names that path
// and none of the others, or an entry keyed by the id that names the path.
func namesHolder(doc any, id, path string, others ...string) bool {
	switch v := doc.(type) {
	case map[string]any:
		hasID := false
		for k, x := range v {
			if s, ok := x.(string); ok && s == id {
				hasID = true
			}
			if k == id && mentions(x, path) {
				return true
			}
			if namesHolder(x, id, path, others...) {
				return true
			}
		}
		if !hasID || !mentions(v, path) {
			return false
		}
		for _, other := range others {
			if mentions(v, other) {
				return false
			}
		}
		return true
	case []any:
		for _, x := range v {
			if namesHolder(x, id, path, others...) {
				return true
			}
		}
	}
	return false
}

// mentions is whether any string in a JSON value names the path.
func mentions(doc any, path string) bool {
	switch v := doc.(type) {
	case string:
		return v == path || filepath.Clean(v) == path || names(v, path)
	case map[string]any:
		for _, x := range v {
			if mentions(x, path) {
				return true
			}
		}
	case []any:
		for _, x := range v {
			if mentions(x, path) {
				return true
			}
		}
	}
	return false
}

var codePattern = regexp.MustCompile(`SDLC-E(\d{4})`)

// newCode is the code a refusal carries where it is one this story added:
// every code up to SDLC-E0049 was registered before it, for other conditions.
func newCode(t *testing.T, out string) string {
	t.Helper()
	for _, m := range codePattern.FindAllStringSubmatch(out, -1) {
		if n, _ := strconv.Atoi(m[1]); n > 49 {
			return m[0]
		}
	}
	t.Errorf("the refusal carries no code of its own; every code up to SDLC-E0049 means something else:\n%s", out)
	return ""
}

// fixLine is the line of a rendered refusal that says what to do.
func fixLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, "  fix  "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// ask puts a tool call to the hook through the shipped launcher, as Claude Code
// does for a session whose project is dir.
func ask(t *testing.T, binary, session, dir, tool string, input map[string]any) reply {
	t.Helper()
	return parse(t, launch(t, binary, dir, "PreToolUse", map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      session,
		"tool_name":       tool,
		"cwd":             dir,
		"tool_input":      input,
	}))
}

// stopHook is Claude Code telling the hook that the session is ending its turn.
func stopHook(t *testing.T, binary, session, dir string) string {
	t.Helper()
	return launch(t, binary, dir, "Stop", map[string]any{
		"hook_event_name":  "Stop",
		"session_id":       session,
		"cwd":              dir,
		"stop_hook_active": false,
	})
}

func launch(t *testing.T, binary, dir, event string, payload map[string]any) string {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "plugin", "bin", "sdlc-hook"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), script, event)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "SDLC_BIN="+binary, "CLAUDE_PROJECT_DIR="+dir)
	cmd.Stdin = bytes.NewReader(raw)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the launcher failed: %v\n%s", err, stderr.String())
	}
	return stdout.String()
}

func denied(r reply) bool { return r.HookSpecificOutput.PermissionDecision == "deny" }

func skipWithoutTheLauncher(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX launcher does not run here; plugin/bin/sdlc-hook.cmd is its counterpart")
	}
}

// ---------------------------------------------------------------- AC-1

// A worktree of a repository whose backlog is committed gets a copy of it, and
// one whose backlog is ignored gets none. Either way the backlog and the
// records are the main checkout's, and the copy git put in the worktree is
// neither read nor written (D5).
func TestAC1ACommandInALinkedWorktreeUsesTheMainCheckoutsBacklogAndRecords(t *testing.T) {
	binary := build(t)

	for _, layout := range []struct {
		name    string
		ignored bool
	}{
		{"AC-1 where the backlog is committed", false},
		{"AC-1 where the backlog and the records are ignored", true},
	} {
		t.Run(layout.name, func(t *testing.T) {
			main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
			if layout.ignored {
				writeFile(t, filepath.Join(main.dir, ".gitignore"), "/user_stories.json\n/.sdlc/stories/\n/.sdlc/state/\n")
				git(t, main.dir, "rm", "--cached", "--quiet", "user_stories.json")
				commitEverything(t, main.dir, "the backlog is the loop's")
			}
			wt := worktree(t, main, "wt").as(sessionWorktree)

			// The main checkout's backlog moves on after the worktree was made,
			// and the worktree's own copy says something else.
			setStories(t, main.backlog(), spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2},
				spec{id: "US-003", priority: 3})
			if !layout.ignored {
				raw := strings.Replace(readFile(t, wt.backlog()), `"Story US-001"`, `"A copy nobody reads"`, 1)
				writeFile(t, wt.backlog(), raw)
			}
			copyBefore := readFile(t, wt.backlog())

			listed := wt.document(t, "story", "list")
			rows, _ := listed["stories"].([]any)
			var ids []string
			for _, r := range rows {
				row, _ := r.(map[string]any)
				id, _ := row["id"].(string)
				ids = append(ids, id)
				if id == "US-001" && row["title"] != "Story US-001" {
					t.Errorf("story list in the worktree read the worktree's copy: US-001 is titled %q", row["title"])
				}
			}
			if !slices.Equal(ids, []string{"US-001", "US-002", "US-003"}) {
				t.Errorf("story list in the worktree lists %v, want the main checkout's US-001, US-002, US-003", ids)
			}

			if got := wt.started(t); got != "US-001" {
				t.Fatalf("start in the worktree began %q, want US-001", got)
			}
			if got := statuses(t, main.backlog())["US-001"]; got != "in_progress" {
				t.Errorf("the main checkout's backlog has US-001 %q after start in the worktree, want in_progress", got)
			}

			wt.analysed(t)
			wt.run(t, "cost", "add", "--usd", "1.50")
			for _, doc := range []string{"gate-record.json", "ANALYSIS.md", "THREATS.md"} {
				if !exists(filepath.Join(main.dir, ".sdlc", "stories", "US-001", doc)) {
					t.Errorf("%s was not written to the main checkout's .sdlc/stories/US-001", doc)
				}
			}
			if exists(filepath.Join(wt.dir, ".sdlc", "stories")) {
				t.Errorf("the worktree has a .sdlc/stories of its own: %v", slices.Collect(maps.Keys(files(t, filepath.Join(wt.dir, ".sdlc", "stories")))))
			}
			gates, _ := wt.document(t, "status")["gates"].(map[string]any)
			if gates["dor"] != "pass" || gates["analysis"] != "pass" {
				t.Errorf("status in the worktree shows gates %v, want dor and analysis passed", gates)
			}

			// A record git checked out in the worktree, saying more than the
			// real one, is never read and never touched.
			var planted map[string]any
			readJSON(t, main.record("US-001"), &planted)
			if recorded, ok := planted["gates"].(map[string]any); ok {
				recorded["tests_frozen"] = recorded["analysis"]
			}
			raw, err := json.MarshalIndent(planted, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, wt.record("US-001"), string(raw)+"\n")
			plantedBody := readFile(t, wt.record("US-001"))
			gates, _ = wt.document(t, "status")["gates"].(map[string]any)
			if gates["tests_frozen"] == "pass" {
				t.Error("status in the worktree read the record git put there, not the main checkout's")
			}

			wt.run(t, "escalate", "spec_unclear", "--message", "which greeting?")
			if got := statuses(t, main.backlog())["US-001"]; got != "awaiting_human" {
				t.Errorf("escalating in the worktree left US-001 %q in the main checkout's backlog, want awaiting_human", got)
			}
			wt.as("").run(t, "approve", "US-001")
			if got := statuses(t, main.backlog())["US-001"]; got != "in_progress" {
				t.Errorf("approving in the worktree left US-001 %q in the main checkout's backlog, want in_progress", got)
			}
			wt.as("").run(t, "stop")

			if got := readFile(t, wt.backlog()); got != copyBefore {
				t.Errorf("the worktree's own backlog was written to:\n%s", got)
			}
			if got := files(t, filepath.Join(wt.dir, ".sdlc", "stories")); len(got) != 1 || got["US-001/gate-record.json"] != plantedBody {
				t.Errorf("the worktree's .sdlc/stories holds %v, want only the record git put there, untouched",
					slices.Collect(maps.Keys(got)))
			}
		})
	}

	t.Run("AC-1 init in a worktree puts the starter backlog in the main checkout", func(t *testing.T) {
		root := tempDir(t)
		git(t, root, "init", "--quiet", "--initial-branch=main")
		writeFile(t, filepath.Join(root, "go.mod"), "module example.com/demo\n\ngo 1.26\n")
		commitEverything(t, root, "a project without the loop")
		wt := worktree(t, checkout{binary: binary, dir: root}, "wt")

		wt.as("").run(t, "init")
		if !exists(filepath.Join(root, "user_stories.json")) {
			t.Error("init in the worktree wrote no backlog in the main checkout")
		}
		if exists(wt.backlog()) {
			t.Error("init in the worktree wrote a backlog in the worktree")
		}
	})
}

// ---------------------------------------------------------------- AC-2

func TestAC2StartInAnotherCheckoutSkipsAStoryHeldElsewhere(t *testing.T) {
	binary := build(t)

	t.Run("AC-2 a story under way in the main checkout is neither resumed nor picked", func(t *testing.T) {
		main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
		wt := worktree(t, main, "wt").as(sessionWorktree)
		if got := main.as(sessionMain).started(t); got != "US-001" {
			t.Fatalf("the main checkout started %q, want US-001", got)
		}

		doc := wt.document(t, "start")
		if doc["story"] != "US-002" || doc["resume"] != false {
			t.Errorf("start in the worktree gave %v (resume %v), want US-002 begun afresh", doc["story"], doc["resume"])
		}
		if got := statuses(t, main.backlog()); got["US-001"] != "in_progress" || got["US-002"] != "in_progress" {
			t.Errorf("the backlog is %v, want both stories in_progress", got)
		}
	})

	t.Run("AC-2 a story held elsewhere that waits on a person is skipped", func(t *testing.T) {
		main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
		wt := worktree(t, main, "wt").as(sessionWorktree)
		main.as(sessionMain).run(t, "start")
		main.as(sessionMain).run(t, "escalate", "spec_unclear", "--message", "which greeting?")

		if got := wt.started(t); got != "US-002" {
			t.Errorf("start in the worktree gave %q, want US-002: US-001 waits on a person in the checkout that holds it", got)
		}
	})

	t.Run("AC-2 a story put down part-way stays held by its checkout", func(t *testing.T) {
		main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
		wt := worktree(t, main, "wt").as(sessionWorktree)
		main.as(sessionMain).run(t, "start")
		main.as("").run(t, "stop")

		if got := wt.started(t); got != "US-002" {
			t.Errorf("start in the worktree gave %q, want US-002: stopping part-way leaves the claim in place", got)
		}
	})

	t.Run("AC-2 a story waiting on a held story waits as if the held story were missing", func(t *testing.T) {
		main := repository(t, binary, spec{id: "US-001", priority: 1},
			spec{id: "US-002", priority: 2, dependsOn: []string{"US-001"}}, spec{id: "US-003", priority: 3})
		wt := worktree(t, main, "wt").as(sessionWorktree)
		main.as(sessionMain).run(t, "start")

		if got := wt.started(t); got != "US-003" {
			t.Errorf("start in the worktree gave %q, want US-003", got)
		}
	})

	t.Run("AC-2 with only the held story left there is nothing to start", func(t *testing.T) {
		main := repository(t, binary, spec{id: "US-001", priority: 1})
		wt := worktree(t, main, "wt").as(sessionWorktree)
		main.as(sessionMain).run(t, "start")
		before := loopState(t, main)

		out, code := wt.try(t, "start")
		if code == 0 || !strings.Contains(out, "SDLC-E0010") {
			t.Errorf("start in the worktree exited %d, want the refusal an empty backlog gets (SDLC-E0010):\n%s", code, out)
		}
		if after := loopState(t, main); !maps.Equal(before, after) {
			t.Error("the refused start changed the main checkout's loop state")
		}
	})
}

// ---------------------------------------------------------------- AC-3

func TestAC3NamingAStoryAnotherCheckoutHoldsIsRefusedAndChangesNothing(t *testing.T) {
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	wt := worktree(t, main, "wt").as(sessionWorktree)
	main.as(sessionMain).run(t, "start")

	// refused runs a command in the worktree that has to be refused with the
	// code start gets, naming the main checkout, and leave both checkouts as
	// they were.
	var code string
	refused := func(t *testing.T, c checkout, stdin string, args ...string) {
		t.Helper()
		beforeMain, beforeWorktree := loopState(t, main), loopState(t, wt)
		out, exit := c.tryWith(t, stdin, args...)
		if exit == 0 {
			t.Fatalf("sdlc %s in the worktree went through:\n%s", strings.Join(args, " "), out)
		}
		got := newCode(t, out)
		switch {
		case code == "":
			code = got
		case got != code:
			t.Errorf("sdlc %s refused with %s, want %s, the code start refuses with", strings.Join(args, " "), got, code)
		}
		if !names(out, main.dir) {
			t.Errorf("the refusal does not name the checkout that holds the story, %s:\n%s", main.dir, out)
		}
		if fixLine(out) == "" {
			t.Errorf("the refusal does not say what to do:\n%s", out)
		}
		if after := loopState(t, main); !maps.Equal(beforeMain, after) {
			t.Error("the refusal changed the main checkout's backlog or records")
		}
		if after := loopState(t, wt); !maps.Equal(beforeWorktree, after) {
			t.Error("the refusal changed the worktree's loop state")
		}
	}

	t.Run("AC-3 start names the checkout that holds the story", func(t *testing.T) {
		refused(t, wt, "", "start", "US-001")
	})

	// D7: whatever writes a story's record or status is measured on the tree
	// of the checkout it runs in, so it runs where the story is held.
	t.Run("AC-3 a gate for the story is refused the same way", func(t *testing.T) {
		refused(t, wt, "", "gate", "dor", "pass", "--story", "US-001", "--note", "from elsewhere")
	})
	t.Run("AC-3 a document for the story is refused the same way", func(t *testing.T) {
		refused(t, wt, "# Analysis\n", "artifact", "write", "analysis", "--story", "US-001")
	})
	t.Run("AC-3 a review of the story is refused the same way", func(t *testing.T) {
		refused(t, wt, "# architect\n", "review", "add", "design_review", "architect", "approve", "--story", "US-001")
	})
	t.Run("AC-3 a cost for the story is refused the same way", func(t *testing.T) {
		refused(t, wt, "", "cost", "add", "--usd", "1.00", "--story", "US-001")
	})

	// D7: one session holds one story.
	t.Run("AC-3 the session holding a story in the main checkout cannot start another in the worktree", func(t *testing.T) {
		refused(t, wt.as(sessionMain), "", "start")
	})

	// D4: an .sdlc/state/active git checked out from somebody else's work.
	t.Run("AC-3 an active in the worktree naming the held story is refused", func(t *testing.T) {
		writeFile(t, filepath.Join(wt.dir, ".sdlc", "state", "active"), "US-001\n")
		defer func() { _ = os.Remove(filepath.Join(wt.dir, ".sdlc", "state", "active")) }()
		refused(t, wt, "", "start")
	})

	t.Run("AC-3 approve for a story waiting in another checkout is refused the same way", func(t *testing.T) {
		main.as(sessionMain).run(t, "escalate", "spec_unclear", "--message", "which greeting?")
		refused(t, wt.as(""), "", "approve", "US-001")
	})
}

// ---------------------------------------------------------------- AC-4

func TestAC4StartsAtTheSameMomentInThreeCheckoutsNeverShareAStory(t *testing.T) {
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2},
		spec{id: "US-003", priority: 3})
	checkouts := []checkout{
		main.as(sessionMain),
		worktree(t, main, "wt-b").as(sessionWorktree),
		worktree(t, main, "wt-c").as(sessionThird),
	}
	copies := map[string]string{}
	for _, c := range checkouts[1:] {
		copies[c.dir] = readFile(t, c.backlog())
	}

	type result struct {
		stdout, stderr string
		err            error
	}
	results := make([]result, len(checkouts))
	cmds := make([]*exec.Cmd, len(checkouts))
	outs := make([]*bytes.Buffer, len(checkouts))
	errs := make([]*bytes.Buffer, len(checkouts))
	for i, c := range checkouts {
		cmds[i] = c.command(t, "", "start", "--json")
		outs[i], errs[i] = &bytes.Buffer{}, &bytes.Buffer{}
		cmds[i].Stdout, cmds[i].Stderr = outs[i], errs[i]
	}
	gun := make(chan struct{})
	var wg sync.WaitGroup
	for i := range cmds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gun
			err := cmds[i].Run()
			results[i] = result{stdout: outs[i].String(), stderr: errs[i].String(), err: err}
		}()
	}
	close(gun)
	wg.Wait()

	given := map[string]string{}
	for i, r := range results {
		if r.err != nil {
			t.Errorf("start in %s failed: %v\n%s", checkouts[i].dir, r.err, r.stderr)
			continue
		}
		var doc struct {
			Story string `json:"story"`
		}
		if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
			t.Fatalf("start printed something that is not JSON: %v\n%s", err, r.stdout)
		}
		if other, taken := given[doc.Story]; taken {
			t.Errorf("%s was given to both %s and %s", doc.Story, other, checkouts[i].dir)
		}
		given[doc.Story] = checkouts[i].dir
	}
	want := map[string]string{"US-001": "in_progress", "US-002": "in_progress", "US-003": "in_progress"}
	if got := statuses(t, main.backlog()); !maps.Equal(got, want) {
		t.Errorf("the backlog holds %v after three starts, want every one of them: %v", got, want)
	}
	for dir, body := range copies {
		if readFile(t, filepath.Join(dir, "user_stories.json")) != body {
			t.Errorf("start wrote the worktree's own copy of the backlog in %s", dir)
		}
	}
}

// ---------------------------------------------------------------- AC-5

// perCheckout is what AC-5 keeps apart: a checkout's freeze, the session bound
// to it and the Stop guard's count.
func perCheckout(t *testing.T, c checkout) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range []string{"tests.lock", "session", "stop-blocks.json"} {
		out[name] = readFile(t, filepath.Join(c.dir, ".sdlc", "state", name))
	}
	return out
}

func TestAC5WhatOneCheckoutDoesLeavesTheOthersFreezeSessionAndStopCount(t *testing.T) {
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	wt := worktree(t, main, "wt")
	m, w := main.as(sessionMain), wt.as(sessionWorktree)
	m.run(t, "start", "US-001")
	w.run(t, "start", "US-002")
	m.frozen(t, "us_001_test.go")
	w.frozen(t, "us_002_test.go")

	// The two stories are held in one repository: one backlog says so, and one
	// place keeps both records.
	if got := statuses(t, main.backlog()); got["US-001"] != "in_progress" || got["US-002"] != "in_progress" {
		t.Fatalf("precondition: the main checkout's backlog is %v, want both stories in_progress", got)
	}
	if !exists(main.record("US-002")) {
		t.Fatal("precondition: the worktree's story has no record in the main checkout")
	}

	unchanged := func(t *testing.T, what string, other checkout, act func()) {
		t.Helper()
		before := perCheckout(t, other)
		act()
		if after := perCheckout(t, other); !maps.Equal(before, after) {
			for name := range before {
				if before[name] != after[name] {
					t.Errorf("%s changed %s in %s:\nbefore %q\nafter  %q", what, name, other.dir, before[name], after[name])
				}
			}
		}
	}

	for _, pair := range []struct {
		name        string
		here, there checkout
		test        string
	}{
		{"in the worktree", w, m, "us_002_test.go"},
		{"in the main checkout", m, w, "us_001_test.go"},
	} {
		t.Run("AC-5 unfreeze and freeze "+pair.name, func(t *testing.T) {
			unchanged(t, "unfreeze "+pair.name, pair.there, func() {
				pair.here.as("").run(t, "unfreeze", "--reason", "the test has to change")
				if exists(filepath.Join(pair.here.dir, ".sdlc", "state", "tests.lock")) {
					t.Errorf("unfreeze left the freeze in %s", pair.here.dir)
				}
			})
			unchanged(t, "freeze "+pair.name, pair.there, func() {
				pair.here.run(t, "freeze")
			})
		})

		t.Run("AC-5 the Stop hook "+pair.name, func(t *testing.T) {
			skipWithoutTheLauncher(t)
			unchanged(t, "the Stop hook "+pair.name, pair.there, func() {
				before := readFile(t, filepath.Join(pair.here.dir, ".sdlc", "state", "stop-blocks.json"))
				stopHook(t, binary, pair.here.session, pair.here.dir)
				if readFile(t, filepath.Join(pair.here.dir, ".sdlc", "state", "stop-blocks.json")) == before {
					t.Errorf("the Stop hook did not count the stop in %s", pair.here.dir)
				}
			})
		})
	}

	t.Run("AC-5 stop in the worktree", func(t *testing.T) {
		unchanged(t, "stop in the worktree", m, func() {
			w.as("").run(t, "stop")
		})
	})
}

// ---------------------------------------------------------------- AC-6

func TestAC6StatusStoryListAndDoctorReportEveryStoryAndWhoHoldsIt(t *testing.T) {
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2},
		spec{id: "US-003", priority: 3})
	wt := worktree(t, main, "wt")
	main.as(sessionMain).run(t, "start", "US-001")
	wt.as(sessionWorktree).run(t, "start", "US-002")

	for _, c := range []struct {
		name string
		at   checkout
	}{
		{"from the main checkout", main.as(sessionMain)},
		{"from the worktree", wt.as(sessionWorktree)},
	} {
		t.Run("AC-6 status "+c.name, func(t *testing.T) {
			doc := c.at.document(t, "status")
			counts, _ := doc["backlog"].(map[string]any)
			if counts["in_progress"] != 2.0 || counts["ready"] != 1.0 {
				t.Errorf("status counts %v, want the repository's 2 in_progress and 1 ready", counts)
			}
			if !namesHolder(doc, "US-001", main.dir, wt.dir) {
				t.Errorf("status --json does not tie US-001 to %s", main.dir)
			}
			if !namesHolder(doc, "US-002", wt.dir, main.dir) {
				t.Errorf("status --json does not tie US-002 to %s", wt.dir)
			}
			if text := c.at.run(t, "status"); !names(text, main.dir) || !names(text, wt.dir) {
				t.Errorf("status does not name the checkouts holding US-001 and US-002:\n%s", text)
			}
		})

		t.Run("AC-6 story list "+c.name, func(t *testing.T) {
			doc := c.at.document(t, "story", "list")
			got := map[string]string{}
			rows, _ := doc["stories"].([]any)
			for _, r := range rows {
				row, _ := r.(map[string]any)
				id, _ := row["id"].(string)
				got[id], _ = row["status"].(string)
				if id == "US-003" && (mentions(row, main.dir) || mentions(row, wt.dir)) {
					t.Errorf("story list names a checkout for US-003, which nobody holds: %v", row)
				}
			}
			want := map[string]string{"US-001": "in_progress", "US-002": "in_progress", "US-003": "ready"}
			if !maps.Equal(got, want) {
				t.Errorf("story list shows %v, want %v", got, want)
			}
			if !namesHolder(doc, "US-001", main.dir, wt.dir) || !namesHolder(doc, "US-002", wt.dir, main.dir) {
				t.Errorf("story list --json does not tie each held story to its checkout:\n%v", doc)
			}
			if text := c.at.run(t, "story", "list"); !names(text, main.dir) || !names(text, wt.dir) {
				t.Errorf("story list does not name the checkouts holding US-001 and US-002:\n%s", text)
			}
		})

		t.Run("AC-6 doctor "+c.name, func(t *testing.T) {
			out, _ := c.at.try(t, "doctor", "--json")
			var doc struct {
				Checks []check `json:"checks"`
			}
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("doctor --json: %v\n%s", err, out)
			}
			state := checkNamed(doc.Checks, "loop state")
			if !names(state.Detail, main.dir) || !names(state.Detail, wt.dir) {
				t.Errorf("doctor's loop state does not name the checkouts holding US-001 and US-002: %q", state.Detail)
			}
		})
	}

	t.Run("AC-6 doctor in the worktree reads every record in the main checkout", func(t *testing.T) {
		writeFile(t, main.record("US-003"), "{ not a record")
		out, _ := wt.as(sessionWorktree).try(t, "doctor", "--json")
		var doc struct {
			Checks []check `json:"checks"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("doctor --json: %v\n%s", err, out)
		}
		if state := checkNamed(doc.Checks, "loop state"); state.State != "problem" || !strings.Contains(state.Detail, "US-003") {
			t.Errorf("doctor in the worktree does not report the main checkout's broken US-003 record: %+v", state)
		}
	})
}

type check struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

func checkNamed(checks []check, name string) check {
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	return check{}
}

func TestAC6LogInAnyCheckoutListsAStoryCommittedInAnotherAndWhoHoldsIt(t *testing.T) {
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1})
	wt := worktree(t, main, "wt").as(sessionWorktree)
	wt.run(t, "start")
	wt.walked(t, "us_001_test.go")
	wt.committed(t, "US-001")

	for _, c := range []checkout{main, wt.as("")} {
		var logged struct {
			Entries []map[string]any `json:"entries"`
		}
		if err := json.Unmarshal([]byte(c.run(t, "log", "--all", "--json")), &logged); err != nil {
			t.Fatal(err)
		}
		var entry map[string]any
		for _, e := range logged.Entries {
			if e["story"] == "US-001" {
				entry = e
			}
		}
		if entry == nil {
			t.Errorf("sdlc log in %s does not list US-001, committed in the worktree: %v", c.dir, logged.Entries)
			continue
		}
		if !mentions(entry, wt.dir) {
			t.Errorf("sdlc log --json in %s does not name %s as holding US-001: %v", c.dir, wt.dir, entry)
		}
		if text := c.run(t, "log", "--all"); !names(text, wt.dir) {
			t.Errorf("sdlc log in %s does not name the checkout holding US-001:\n%s", c.dir, text)
		}
	}
}

// ---------------------------------------------------------------- AC-7

// route is one way a session can write a file, as a tool call to the hook.
type route struct {
	name  string
	tool  string
	input func(main, held, other string) map[string]any
}

func bash(format func(main, held, other string) string) func(main, held, other string) map[string]any {
	return func(main, held, other string) map[string]any {
		return map[string]any{"command": format(main, held, other)}
	}
}

func file(path func(main, held, other string) string) func(main, held, other string) map[string]any {
	return func(main, held, other string) map[string]any {
		return map[string]any{"file_path": path(main, held, other), "content": "{}\n"}
	}
}

func backlogAt(main, _, _ string) string { return filepath.Join(main, "user_stories.json") }
func recordAt(main, held, _ string) string {
	return filepath.Join(main, ".sdlc", "stories", held, "gate-record.json")
}
func documentAt(main, held, _ string) string {
	return filepath.Join(main, ".sdlc", "stories", held, "ANALYSIS.md")
}

// routes are the ways a session held to a story in a single checkout is refused
// a write to its backlog and its records today, spelled with absolute paths
// into the main checkout.
var routes = []route{
	{"Write the backlog", "Write", file(backlogAt)},
	{"Edit the backlog", "Edit", func(main, held, other string) map[string]any {
		return map[string]any{"file_path": backlogAt(main, held, other), "old_string": "ready", "new_string": "done"}
	}},
	{"Write the story's gate record", "Write", file(recordAt)},
	{"Write a gate document", "Write", file(documentAt)},
	{"Write another story's gate record", "Write", file(func(main, held, other string) string {
		return recordAt(main, other, held)
	})},
	{"redirect into the backlog", "Bash", bash(func(main, held, other string) string {
		return "echo '{}' > " + backlogAt(main, held, other)
	})},
	{"append to the gate record", "Bash", bash(func(main, held, other string) string {
		return "echo x >> " + recordAt(main, held, other)
	})},
	{"edit the backlog in place with sed", "Bash", bash(func(main, held, other string) string {
		return "sed -i.bak 's/ready/done/' " + backlogAt(main, held, other)
	})},
	{"delete the gate record", "Bash", bash(func(main, held, other string) string {
		return "rm -f " + recordAt(main, held, other)
	})},
	{"delete every record", "Bash", bash(func(main, _, _ string) string {
		return "rm -rf " + filepath.Join(main, ".sdlc", "stories")
	})},
	{"copy over the backlog", "Bash", bash(func(main, held, other string) string {
		return "cp /dev/null " + backlogAt(main, held, other)
	})},
	{"move the backlog away", "Bash", bash(func(main, held, other string) string {
		return "mv " + backlogAt(main, held, other) + " " + filepath.Join(main, "old.json")
	})},
	{"tee a gate document", "Bash", bash(func(main, held, other string) string {
		return "echo pass | tee " + documentAt(main, held, other)
	})},
	{"truncate the backlog", "Bash", bash(func(main, held, other string) string {
		return "truncate -s 0 " + backlogAt(main, held, other)
	})},
	{"restore the backlog with git -C", "Bash", bash(func(main, _, _ string) string {
		return "git -C " + main + " checkout -- user_stories.json"
	})},
	{"restore the backlog with --git-dir and --work-tree", "Bash", bash(func(main, _, _ string) string {
		return "git --git-dir=" + filepath.Join(main, ".git") + " --work-tree=" + main + " checkout -- user_stories.json"
	})},
	{"change directory and redirect", "Bash", bash(func(main, _, _ string) string {
		return "cd " + main + " && echo '{}' > user_stories.json"
	})},
	{"a subshell in the main checkout", "Bash", bash(func(main, held, _ string) string {
		return "(cd " + main + " && rm -f .sdlc/stories/" + held + "/gate-record.json)"
	})},
}

func TestAC7TheHookRefusesAHeldSessionTheMainCheckoutsBacklogRecordsAndClaim(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	wt := worktree(t, main, "wt")

	before := files(t, main.dir)
	main.as(sessionMain).run(t, "start", "US-001")
	mid := files(t, main.dir)
	wt.as(sessionWorktree).run(t, "start", "US-002")
	after := files(t, main.dir)
	claims := append(changedNaming(before, mid, main.dir), changedNaming(mid, after, wt.dir)...)

	for _, r := range routes {
		t.Run("AC-7 "+r.name, func(t *testing.T) {
			// What a single checkout refuses today is the measure.
			if got := ask(t, binary, sessionMain, main.dir, r.tool, r.input(main.dir, "US-001", "US-002")); !denied(got) {
				t.Fatalf("precondition: a session held in a single checkout may %s today", r.name)
			}
			if got := ask(t, binary, sessionWorktree, wt.dir, r.tool, r.input(main.dir, "US-002", "US-001")); !denied(got) {
				t.Errorf("the session held in the worktree may %s in the main checkout", r.name)
			}
			// The session's project is the main checkout, where it holds nothing:
			// it opened there and started its story in the worktree.
			if got := ask(t, binary, sessionWorktree, main.dir, r.tool, r.input(main.dir, "US-002", "US-001")); !denied(got) {
				t.Errorf("the session held in the worktree may %s from inside the main checkout", r.name)
			}
		})
	}

	t.Run("AC-7 the claim", func(t *testing.T) {
		if len(claims) == 0 {
			t.Fatalf("no file in the main checkout names the checkout holding a story: there is no record of a claim to protect")
		}
		for _, name := range claims {
			path := filepath.Join(main.dir, filepath.FromSlash(name))
			for _, s := range []struct{ session, dir string }{
				{sessionMain, main.dir}, {sessionWorktree, wt.dir}, {sessionWorktree, main.dir},
			} {
				for _, call := range []struct {
					tool  string
					input map[string]any
				}{
					{"Write", map[string]any{"file_path": path, "content": "{}\n"}},
					{"Bash", map[string]any{"command": "rm -f " + path}},
					{"Bash", map[string]any{"command": "echo '{}' > " + path}},
				} {
					if got := ask(t, binary, s.session, s.dir, call.tool, call.input); !denied(got) {
						t.Errorf("%s, from %s, may %s %v", s.session, s.dir, call.tool, call.input)
					}
				}
			}
		}
	})

	// D5: the tool never reads the worktree's copy, and a held session still
	// may not edit it, because the edit would be committed on the story's branch.
	t.Run("AC-7 the worktree's own copy of the backlog stays refused", func(t *testing.T) {
		for _, call := range []struct {
			tool  string
			input map[string]any
		}{
			{"Write", map[string]any{"file_path": filepath.Join(wt.dir, "user_stories.json"), "content": "{}\n"}},
			{"Bash", map[string]any{"command": "echo '{}' > user_stories.json"}},
		} {
			if got := ask(t, binary, sessionWorktree, wt.dir, call.tool, call.input); !denied(got) {
				t.Errorf("the session held in the worktree may %s %v", call.tool, call.input)
			}
		}
	})
}

// ---------------------------------------------------------------- AC-8

// namedCommands is every sdlc command a text quotes, in "..." or `...`, as the
// arguments a person would type: a placeholder for a value becomes a reason,
// and <ID> or ID the story.
func namedCommands(text, id string) [][]string {
	var out [][]string
	for _, delim := range []byte{'"', '`'} {
		rest := text
		for {
			i := strings.Index(rest, string(delim)+"sdlc ")
			if i < 0 {
				break
			}
			body := rest[i+len("sdlc ")+1:]
			end := closingDelimiter(body, delim)
			out = append(out, commandArgs(body[:end], id))
			if end >= len(body) {
				break
			}
			rest = body[end+1:]
		}
	}
	return out
}

func closingDelimiter(s string, delim byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return i
		}
		if s[i] == delim && (delim != '"' || i == 0 || s[i-1] != '\\') {
			return i
		}
	}
	return len(s)
}

func commandArgs(line, id string) []string {
	line = strings.ReplaceAll(line, `\"`, `"`)
	var args []string
	for {
		line = strings.TrimLeft(line, " ")
		if line == "" {
			return args
		}
		var word string
		if q := line[0]; q == '"' || q == '\'' {
			end := strings.IndexByte(line[1:], q)
			if end < 0 {
				word, line = line[1:], ""
			} else {
				word, line = line[1:1+end], line[2+end:]
			}
		} else {
			end := strings.IndexByte(line, ' ')
			if end < 0 {
				word, line = line, ""
			} else {
				word, line = line[:end], line[end:]
			}
		}
		switch {
		case word == "..." || word == "…" || word == "":
			word = "the checkout that held it is gone"
		case word == "ID" || (strings.HasPrefix(word, "<") && strings.HasSuffix(word, ">")):
			word = id
		}
		args = append(args, word)
	}
}

func shellLine(args []string) string {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = a
		if strings.ContainsAny(a, " \"'") {
			quoted[i] = strconv.Quote(a)
		}
	}
	return "sdlc " + strings.Join(quoted, " ")
}

func TestAC8AClaimWhoseCheckoutIsGoneIsRefusedUntilAPersonReleasesIt(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2},
		spec{id: "US-003", priority: 3})
	gone := worktree(t, main, "gone").as(sessionWorktree)
	other := worktree(t, main, "other").as(sessionThird)
	gone.run(t, "start", "US-001")
	other.run(t, "start", "US-002")
	if err := os.RemoveAll(gone.dir); err != nil {
		t.Fatal(err)
	}

	here := main.as(sessionMain)
	before := loopState(t, main)
	out, code := here.try(t, "start", "US-001")
	if code == 0 {
		t.Fatalf("start went through on a story whose checkout is gone:\n%s", out)
	}
	if !names(out, gone.dir) {
		t.Errorf("the refusal does not name the missing checkout %s:\n%s", gone.dir, out)
	}
	if after := loopState(t, main); !maps.Equal(before, after) {
		t.Error("the refused start changed the main checkout's loop state")
	}

	var candidates [][]string
	for _, args := range namedCommands(out, "US-001") {
		if len(args) == 0 || slices.Contains([]string{"start", "status", "doctor", "log", "help", "version"}, args[0]) ||
			(args[0] == "story" && len(args) > 1 && args[1] == "list") {
			continue
		}
		candidates = append(candidates, args)
	}
	if len(candidates) == 0 {
		t.Fatalf("the refusal names no command that releases the claim:\n%s", out)
	}

	// The release command is a person's: the hook refuses it to a session that
	// holds nothing, and to one that holds a story, as it refuses sdlc ack.
	var release [][]string
	for _, args := range candidates {
		line := shellLine(args)
		unheld := ask(t, binary, sessionUnheld, main.dir, "Bash", map[string]any{"command": line})
		held := ask(t, binary, sessionThird, other.dir, "Bash", map[string]any{"command": line})
		if denied(unheld) && denied(held) {
			release = append(release, args)
		}
	}
	if len(release) == 0 {
		t.Fatalf("the hook lets a model-driven session run every command the refusal names: %v", candidates)
	}

	for _, args := range release {
		if out, code := main.as("").try(t, args...); code != 0 {
			t.Fatalf("a person's %q failed:\n%s", shellLine(args), out)
		}
	}
	if got := here.started(t, "US-001"); got != "US-001" {
		t.Errorf("after a person released the claim, start in the main checkout gave %q, want US-001", got)
	}
}

// ---------------------------------------------------------------- AC-9

// bareWorktree is a linked worktree of a bare repository, cloned from a project
// with the loop set up in it when withLoop is set.
func bareWorktree(t *testing.T, binary string, withLoop bool) checkout {
	t.Helper()
	src := tempDir(t)
	git(t, src, "init", "--quiet", "--initial-branch=main")
	writeFile(t, filepath.Join(src, "go.mod"), "module example.com/demo\n\ngo 1.26\n")
	if withLoop {
		checkout{binary: binary, dir: src}.run(t, "init")
	}
	commitEverything(t, src, "the project")

	bare := filepath.Join(tempDir(t), "repo.git")
	git(t, src, "clone", "--quiet", "--bare", src, bare)
	wt := filepath.Join(tempDir(t), "wt")
	git(t, bare, "worktree", "add", "--quiet", "-b", "wt", wt, "main")
	return checkout{binary: binary, dir: wt}
}

func TestAC9ARepositoryWithNoMainCheckoutIsRefusedWithACodeOfItsOwn(t *testing.T) {
	binary := build(t)
	wt := bareWorktree(t, binary, true).as(sessionWorktree)

	var code string
	refusedBare := func(t *testing.T, c checkout, args ...string) string {
		t.Helper()
		out, exit := c.try(t, args...)
		if exit == 0 {
			t.Errorf("sdlc %s went through with no main checkout to keep the backlog:\n%s", strings.Join(args, " "), out)
			return out
		}
		got := newCode(t, out)
		switch {
		case code == "":
			code = got
		case got != code:
			t.Errorf("sdlc %s refused with %s, want %s, the same code for the same condition", strings.Join(args, " "), got, code)
		}
		if !strings.Contains(strings.ToLower(out), "bare") || fixLine(out) == "" {
			t.Errorf("the refusal does not say the repository is bare and what to do:\n%s", out)
		}
		return out
	}

	for _, args := range [][]string{
		{"status"},
		{"story", "list"},
		{"start"},
		{"start", "US-001"},
		{"gate", "dor", "pass", "--story", "US-001", "--note", "anywhere"},
		{"approve", "US-001"},
	} {
		t.Run("AC-9 "+strings.Join(args, " "), func(t *testing.T) {
			refusedBare(t, wt, args...)
			if exists(filepath.Join(wt.dir, ".sdlc", "stories")) {
				t.Error("a record was written in the worktree")
			}
		})
	}

	t.Run("AC-9 init", func(t *testing.T) {
		fresh := bareWorktree(t, binary, false)
		refusedBare(t, fresh.as(""), "init")
		if exists(fresh.backlog()) || exists(filepath.Join(fresh.dir, ".sdlc", "config.json")) {
			t.Error("init wrote the loop's files into a worktree of a bare repository")
		}
	})

	// D6: stop is the way out of a broken state.
	t.Run("AC-9 stop still ends the iteration and says the backlog is out of reach", func(t *testing.T) {
		writeFile(t, filepath.Join(wt.dir, ".sdlc", "state", "active"), "US-001\n")
		writeFile(t, filepath.Join(wt.dir, ".sdlc", "state", "session"), sessionWorktree+"\n")
		out, exit := wt.as("").try(t, "stop")
		if exit != 0 {
			t.Fatalf("stop exited %d:\n%s", exit, out)
		}
		if exists(filepath.Join(wt.dir, ".sdlc", "state", "active")) {
			t.Error("stop left .sdlc/state/active behind")
		}
		if exists(filepath.Join(wt.dir, ".sdlc", "stories")) {
			t.Error("stop wrote a record in the worktree")
		}
		if !strings.Contains(out, "backlog") {
			t.Errorf("stop does not say the backlog could not be reached:\n%s", out)
		}
	})

	t.Run("AC-9 doctor reports it as a failing check", func(t *testing.T) {
		out, exit := wt.try(t, "doctor", "--json")
		var doc struct {
			Checks []check `json:"checks"`
		}
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Fatalf("doctor --json: %v\n%s", err, out)
		}
		reported := false
		for _, c := range doc.Checks {
			if c.State == "problem" && strings.Contains(strings.ToLower(c.Detail+" "+c.Fix), "bare") {
				reported = true
			}
		}
		if exit == 0 || !reported {
			t.Errorf("doctor exited %d and reported no problem naming the bare repository: %+v", exit, doc.Checks)
		}
	})

	// D6 and C18: the hook fails open over it, and the commit stays refused.
	t.Run("AC-9 the hook still answers, and still refuses the commit", func(t *testing.T) {
		skipWithoutTheLauncher(t)
		writeFile(t, filepath.Join(wt.dir, ".sdlc", "state", "active"), "US-001\n")
		writeFile(t, filepath.Join(wt.dir, ".sdlc", "state", "session"), sessionWorktree+"\n")
		if got := ask(t, binary, sessionWorktree, wt.dir, "Bash", map[string]any{"command": "ls"}); !got.Continue || denied(got) {
			t.Errorf("the hook refused a harmless call: %+v", got)
		} else if code != "" && strings.Contains(got.HookSpecificOutput.PermissionDecisionReason, code) {
			t.Errorf("the hook answered with %s", code)
		}
		if got := ask(t, binary, sessionWorktree, wt.dir, "Bash", map[string]any{"command": `git commit -m "done"`}); !denied(got) {
			t.Error("a held session may commit where the main checkout cannot be found")
		}
	})

	t.Run("AC-9 a worktree of a repository with a separate git directory", func(t *testing.T) {
		main := tempDir(t)
		gitDir := filepath.Join(tempDir(t), "gitdir")
		git(t, tempDir(t), "init", "--quiet", "--initial-branch=main", "--separate-git-dir="+gitDir, main)
		writeFile(t, filepath.Join(main, "go.mod"), "module example.com/demo\n\ngo 1.26\n")
		checkout{binary: binary, dir: main}.run(t, "init")
		commitEverything(t, main, "the project")
		if out, exit := (checkout{binary: binary, dir: main}).try(t, "status"); exit != 0 {
			t.Fatalf("precondition: the main checkout of a separate git directory does not work:\n%s", out)
		}
		linked := worktree(t, checkout{binary: binary, dir: main}, "wt").as(sessionWorktree)

		for _, args := range [][]string{{"status"}, {"start"}} {
			out := refusedBare(t, linked, args...)
			if !strings.Contains(strings.ToLower(out), "separate") {
				t.Errorf("the refusal does not cover a separate git directory:\n%s", out)
			}
		}
		if out, exit := (checkout{binary: binary, dir: main}).try(t, "status"); exit != 0 {
			t.Errorf("the main checkout of a separate git directory stopped working:\n%s", out)
		}
	})

	t.Run("AC-9 the code is not the one for a story held elsewhere", func(t *testing.T) {
		if code == "" {
			refusedBare(t, wt, "status")
		}
		main := repository(t, binary, spec{id: "US-001", priority: 1})
		other := worktree(t, main, "wt").as(sessionThird)
		main.as(sessionMain).run(t, "start")
		out, _ := other.try(t, "start", "US-001")
		if held := newCode(t, out); held != "" && held == code {
			t.Errorf("a bare repository and a story held elsewhere share %s", code)
		}
	})
}

// ---------------------------------------------------------------- AC-10

// normalised replaces what differs between two runs of the same commands: the
// project's path, the time, and commit and tree names.
func normalised(text, root string) string {
	text = strings.ReplaceAll(text, root, "<root>")
	text = timePattern.ReplaceAllString(text, "<time>")
	return hexPattern.ReplaceAllString(text, "<sha>")
}

var (
	timePattern = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})`)
	hexPattern  = regexp.MustCompile(`\b[0-9a-f]{40,64}\b`)
)

// A single checkout is where every project is today, and nothing here may move
// for it: what each command prints, its exit code, and what it leaves under
// .sdlc/ apart from the record of the claim. The outputs are those of the
// released tool at 64e7b4d.
func TestAC10ASingleCheckoutPrintsAndLeavesWhatTheReleasedToolDoes(t *testing.T) {
	binary := build(t)
	root := project(t, binary)
	addSecondStory(t, root)
	commitEverything(t, root, "a second story")
	c := checkout{binary: binary, dir: root, session: claudeSession}
	var backlog struct {
		Stories []struct {
			Title string `json:"title"`
		} `json:"stories"`
	}
	readJSON(t, filepath.Join(root, "user_stories.json"), &backlog)
	title := backlog.Stories[0].Title

	type step struct {
		as     string
		args   []string
		stdout string
		stderr string
		code   int
	}
	steps := []step{
		{claudeSession, []string{"status"}, ac10StatusFresh(title), "", 0},
		{claudeSession, []string{"story", "list"}, ac10StoryListFresh(title), "", 0},
		{claudeSession, []string{"start"}, ac10Start("Started", title), "", 0},
		{claudeSession, []string{"status"}, ac10StatusStarted(title), "", 0},
		{claudeSession, []string{"story", "list"}, ac10StoryListStarted(title), "", 0},
		{claudeSession, []string{"start", "US-002"}, "", ac10AlreadyRunning, 1},
		{"", []string{"stop"}, "Stopped the iteration on US-001. Nothing was recorded; `sdlc start` picks it up again.\n", "", 0},
		{claudeSession, []string{"start"}, ac10Start("Resumed", title), "", 0},
	}
	for i, s := range steps {
		stdout, stderr, code := c.as(s.as).outcome(t, "", s.args...)
		stdout, stderr = normalised(stdout, root), normalised(stderr, root)
		if stdout != s.stdout || stderr != s.stderr || code != s.code {
			t.Errorf("step %d, sdlc %s:\ngot  exit %d\nstdout %q\nstderr %q\nwant exit %d\nstdout %q\nstderr %q",
				i+1, strings.Join(s.args, " "), code, stdout, stderr, s.code, s.stdout, s.stderr)
		}
	}

	// What the loop leaves under .sdlc/: the files it left before, with the
	// content they had, and at most the claim, which names this checkout.
	want := map[string]string{
		"state/active":  "US-001\n",
		"state/session": claudeSession + "\n",
	}
	got := files(t, filepath.Join(root, ".sdlc"))
	for name, body := range want {
		if got[name] != body {
			t.Errorf(".sdlc/%s = %q, want %q", name, got[name], body)
		}
	}
	if _, ok := got["stories/US-001/gate-record.json"]; !ok {
		t.Error("start left no gate record for US-001")
	}
	committed := map[string]bool{}
	for _, name := range strings.Fields(gitOut(t, root, "ls-files", ".sdlc")) {
		committed[strings.TrimPrefix(name, ".sdlc/")] = true
	}
	for name, body := range got {
		if committed[name] || name == "stories/US-001/gate-record.json" {
			continue
		}
		if _, ok := want[name]; ok {
			continue
		}
		if !names(body, root) {
			t.Errorf(".sdlc/%s is new, and is not the record of a claim on this checkout:\n%s", name, body)
		}
	}
}

func ac10StatusFresh(title string) string {
	return "No iteration running.\n\n  backlog  2 ready\n  next up  US-001  " + title +
		"\n\nA person runs `sdlc start`, or types /sdlc:next in Claude Code, to begin.\n"
}

func ac10StoryListFresh(title string) string {
	return "> US-001  ready   1  " + title + "\n  US-002  ready   2  The story after the first one\n\n" +
		"> is what `sdlc start` would pick.\n"
}

func ac10StoryListStarted(title string) string {
	return "> US-001  in_progress   1  " + title + "\n  US-002  ready         2  The story after the first one\n\n" +
		"> is what `sdlc start` would pick.\n"
}

func ac10Start(verb, title string) string {
	return verb + " US-001  " + title + "\n\n" + ac10Gates +
		"\nNext: a person types /sdlc:next in Claude Code to work the story.\n"
}

func ac10StatusStarted(title string) string {
	return "US-001  in progress  " + title + "\n\n" + ac10Gates +
		"\n  tests  not frozen\n  cost   $0.00 of $60.00 (0%)\n\n  next   dor\n\n  backlog  1 ready · 1 in progress\n\n" +
		"A person types /sdlc:next in Claude Code to carry on, or runs `sdlc stop` to put it down.\n"
}

const (
	ac10Gates          = "  no gates recorded yet\n"
	ac10AlreadyRunning = "sdlc: an iteration is already running on \"US-001\"\n\n" +
		"  why  the loop works one story at a time, which is what keeps a diff small enough to review honestly\n" +
		"  fix  finish the current story, or run \"sdlc stop\" to end the iteration without recording a result\n\n" +
		"  SDLC-E0012  https://github.com/bbsnly/sdlc/blob/main/docs/troubleshooting.md#sdlc-e0012\n"
)

// C6: where the loop's state and records are ignored, as they are in this
// repository, a story committed with only its own work passes the commit gate.
// A record of the claim that git reported as untracked would turn that into
// SDLC-E0032.
func TestAC10TheCommitGateOfASingleCheckoutIsNotRefusedOverTheClaim(t *testing.T) {
	binary := build(t)
	root := project(t, binary)
	writeFile(t, filepath.Join(root, ".gitignore"), "/.sdlc/state/\n/.sdlc/stories/\n")
	commitEverything(t, root, "the loop's state is the machine's")
	c := checkout{binary: binary, dir: root, session: claudeSession}

	c.run(t, "start")
	c.walked(t, "invoice_test.go")
	git(t, root, "add", "invoice_test.go")
	git(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-a", "-m", "US-001")
	if out, code := c.try(t, "gate", "commit", "pass", "--note", "on trunk"); code != 0 {
		t.Errorf("the commit gate refused a story committed with its own work:\n%s", out)
	}
}

// D4: an iteration that was running when this release was installed has no
// claim. It carries on as it did, and it keeps its story from every other
// checkout from then on.
func TestAC10AnIterationRunningAtUpgradeCarriesOnAndKeepsItsStory(t *testing.T) {
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	// What the released tool leaves after `sdlc start`, and nothing more.
	setStatus(t, main.backlog(), "US-001", "in_progress")
	writeFile(t, filepath.Join(main.dir, ".sdlc", "state", "active"), "US-001\n")
	writeFile(t, filepath.Join(main.dir, ".sdlc", "state", "session"), sessionMain+"\n")

	doc := main.as(sessionMain).document(t, "start")
	if doc["story"] != "US-001" || doc["resume"] != true {
		t.Fatalf("start in the upgraded checkout gave %v (resume %v), want US-001 resumed", doc["story"], doc["resume"])
	}

	wt := worktree(t, main, "wt").as(sessionWorktree)
	if got := wt.started(t); got != "US-002" {
		t.Errorf("start in a new worktree gave %q, want US-002: the upgraded checkout holds US-001", got)
	}
}

// D10: an .sdlc/state/active naming a story nobody claims, whose status a
// person changed by hand, is resumed as today in a single checkout and ignored
// where the repository has a linked worktree.
func TestAC10AnActiveAPersonSetAsideIsResumedOnlyInASingleCheckout(t *testing.T) {
	binary := build(t)
	for _, layout := range []struct {
		name   string
		linked bool
		want   string
	}{
		{"AC-10 in a single checkout it is resumed as today", false, "US-001"},
		{"AC-10 with a linked worktree it is ignored and start picks afresh", true, "US-002"},
	} {
		t.Run(layout.name, func(t *testing.T) {
			main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
			setStatus(t, main.backlog(), "US-001", "blocked")
			writeFile(t, filepath.Join(main.dir, ".sdlc", "state", "active"), "US-001\n")
			writeFile(t, filepath.Join(main.dir, ".sdlc", "state", "session"), sessionMain+"\n")
			if layout.linked {
				worktree(t, main, "wt")
			}
			if got := main.as(sessionMain).started(t); got != layout.want {
				t.Errorf("start gave %q, want %s", got, layout.want)
			}
		})
	}
}

// D10: a finished story keeps today's handling in every layout. Between the
// retro and `sdlc stop` start refuses with "run sdlc stop", and stop ends the
// iteration and lifts the finished story's freeze, on the story's one record.
func TestAC10AFinishedStoryInAWorktreeIsPutDownWithSdlcStop(t *testing.T) {
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1})
	wt := worktree(t, main, "wt").as(sessionWorktree)
	wt.run(t, "start")
	wt.walked(t, "us_001_test.go")
	wt.committed(t, "US-001")
	wt.retro(t)

	out, code := wt.try(t, "start")
	if code == 0 || !strings.Contains(out, "SDLC-E0033") || !strings.Contains(out, "sdlc stop") {
		t.Errorf("start between the retro and stop exited %d, want SDLC-E0033 sending you to sdlc stop:\n%s", code, out)
	}
	stopped := wt.as("").run(t, "stop")
	if !strings.Contains(stopped, "Every gate passed") {
		t.Errorf("stop did not end a finished iteration:\n%s", stopped)
	}
	if exists(filepath.Join(wt.dir, ".sdlc", "state", "tests.lock")) {
		t.Error("stop left the finished story's freeze in the worktree")
	}
	var record struct {
		Events []struct {
			Type string `json:"type"`
		} `json:"events"`
	}
	readJSON(t, main.record("US-001"), &record)
	if n := len(record.Events); n == 0 || record.Events[n-1].Type != "unfreeze" {
		t.Errorf("the story's record in the main checkout does not end with the freeze lifted: %+v", record.Events)
	}
	if got := statuses(t, main.backlog())["US-001"]; got != "done" {
		t.Errorf("the main checkout's backlog has US-001 %q, want done", got)
	}
}
