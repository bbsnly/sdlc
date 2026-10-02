// Story worktrees: `sdlc start` gives each new story a worktree of its own at
// .sdlc/worktrees/<ID>, on a branch story/<ID> cut from trunk, and the loop
// works the story there (US-029.2).
//
// Before this story start held a story in the checkout it ran in, on whatever
// that checkout had checked out: a second story could not begin while the
// first was open, and the loop worked in the person's own checkout. These
// drive what a person meets: the real binary, the shipped launcher in front of
// every tool call, and a repository on disk whose main checkout is looked at
// before and after.
//
// The switch is git.worktrees in .sdlc/config.json, a boolean, and absent
// means on. project turns it off, because the tests built on it hold start to
// what the released tool does in one checkout; the fixtures here leave it out,
// as the configuration of a project that never heard of it does.
package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

	"github.com/bbsnly/sdlc/internal/model"
)

// The Claude Code sessions stories are worked in here. One session holds one
// story, so two stories at once need two.
const (
	sessionFirst  = "e2e-first-story-session"
	sessionSecond = "e2e-second-story-session"
	sessionOther  = "e2e-other-session"
)

// ---------------------------------------------------------------- fixtures

// setConfig sets one key of a section of dir's .sdlc/config.json, or removes
// it where value is nil.
func setConfig(t *testing.T, dir, section, key string, value any) {
	t.Helper()
	path := filepath.Join(dir, ".sdlc", "config.json")
	var cfg map[string]any
	readJSON(t, path, &cfg)
	keys, _ := cfg[section].(map[string]any)
	if keys == nil {
		keys = map[string]any{}
	}
	if value == nil {
		delete(keys, key)
	} else {
		keys[key] = value
	}
	cfg[section] = keys
	writeJSON(t, path, cfg)
}

// worktreesOff turns story worktrees off in dir's configuration, so that start
// holds a story in the checkout it runs in, as the released tool does.
func worktreesOff(t *testing.T, dir string) {
	t.Helper()
	setConfig(t, dir, "git", "worktrees", false)
}

// storyRepository is repository as a project has it that never heard of story
// worktrees: no git.worktrees in its configuration, which is on.
func storyRepository(t *testing.T, binary string, stories ...spec) checkout {
	t.Helper()
	main := repository(t, binary, stories...)
	setConfig(t, main.dir, "git", "worktrees", nil)
	commitEverything(t, main.dir, "the configuration as it was before story worktrees")
	return main
}

// storyPath is where the worktree of story id goes: inside the main checkout,
// at .sdlc/worktrees/<ID> (D3).
func storyPath(main checkout, id string) string {
	return filepath.Join(main.dir, ".sdlc", "worktrees", id)
}

// storyTree is a story `sdlc start` began or resumed, and the worktree it is
// worked in, used from the session start ran in.
type storyTree struct {
	checkout
	story, branch string
	// pathField and branchField are the fields of start --json that hold the
	// worktree's path and the branch's name: the runbook reads them by name.
	pathField, branchField string
	doc                    map[string]any
}

// startIn runs `sdlc start --json` from a checkout and holds what it printed to
// AC-1: fields naming the story's worktree, at .sdlc/worktrees/<ID> of the
// main checkout, and its branch, story/<ID>. A start that makes no worktree
// fails the test here.
func startIn(t *testing.T, main, from checkout, args ...string) storyTree {
	t.Helper()
	doc := from.document(t, append([]string{"start"}, args...)...)
	id, _ := doc["story"].(string)
	if id == "" {
		t.Fatalf("start --json in %s names no story: %v", from.dir, doc)
	}
	path, branch := storyPath(main, id), "story/"+id
	w := storyTree{
		checkout:    checkout{binary: from.binary, dir: path, session: from.session},
		story:       id,
		branch:      branch,
		doc:         doc,
		pathField:   fieldHolding(doc, func(v string) bool { return samePath(v, path) }),
		branchField: fieldHolding(doc, func(v string) bool { return v == branch }),
	}
	if w.pathField == "" || w.branchField == "" {
		t.Fatalf("start --json in %s gave %s, and names no worktree at %s on %s:\n%v", from.dir, id, path, branch, doc)
	}
	return w
}

// fieldHolding is the name of the field of a JSON document, at its top or one
// level down, whose string value matches: "worktree", or "worktree.path".
func fieldHolding(doc map[string]any, match func(string) bool) string {
	for _, k := range slices.Sorted(maps.Keys(doc)) {
		switch v := doc[k].(type) {
		case string:
			if match(v) {
				return k
			}
		case map[string]any:
			if inner := fieldHolding(v, match); inner != "" && !strings.Contains(inner, ".") {
				return k + "." + inner
			}
		}
	}
	return ""
}

// resolved is a path by the name the operating system resolves it to, where
// it exists.
func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func samePath(a, b string) bool {
	return a != "" && b != "" && resolved(a) == resolved(b)
}

// gitRaw runs git in dir and returns what it printed as it printed it: git
// status starts a line with a space.
func gitRaw(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return string(out)
}

// worktreesOf is every checkout git lists for the repository at dir, by
// resolved path, with the branch each has checked out, "" for none.
func worktreesOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	at := ""
	for _, line := range strings.Split(gitRaw(t, dir, "worktree", "list", "--porcelain"), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			at = resolved(strings.TrimPrefix(line, "worktree "))
			out[at] = ""
		case strings.HasPrefix(line, "branch refs/heads/"):
			out[at] = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	return out
}

// branchesOf is every branch of the repository at dir, with its commit.
func branchesOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	listed := gitRaw(t, dir, "for-each-ref", "--format=%(refname:short) %(objectname)", "refs/heads")
	for _, line := range strings.Split(listed, "\n") {
		if name, sha, ok := strings.Cut(line, " "); ok {
			out[name] = sha
		}
	}
	return out
}

// cutFromTrunk holds a story's worktree to AC-1 on disk: git lists it with its
// branch checked out, and the branch is on tip, where trunk was when start ran.
func cutFromTrunk(t *testing.T, main checkout, w storyTree, tip string) {
	t.Helper()
	if got, listed := worktreesOf(t, main.dir)[resolved(w.dir)]; !listed || got != w.branch {
		t.Errorf("git lists %s (%v) on %q, want it a worktree on %s", w.dir, listed, got, w.branch)
	}
	if got := branchesOf(t, main.dir)[w.branch]; got != tip {
		t.Errorf("%s is on %q, want trunk's tip when start ran, %s", w.branch, got, tip)
	}
	if got := gitOut(t, w.dir, "rev-parse", "--abbrev-ref", "HEAD"); got != w.branch {
		t.Errorf("the worktree has %q checked out, want %s", got, w.branch)
	}
}

// recordFields is as much of a story's gate record as these tests read.
type recordFields struct {
	Base       string `json:"base"`
	CommitBase string `json:"commit_base"`
	Gates      map[string]struct {
		Status string `json:"status"`
	} `json:"gates"`
}

func recordOf(t *testing.T, main checkout, id string) recordFields {
	t.Helper()
	var r recordFields
	readJSON(t, main.record(id), &r)
	return r
}

// commitPaths commits only the paths named, as a person does who leaves the
// rest of their checkout alone.
func commitPaths(t *testing.T, dir, message string, paths ...string) string {
	t.Helper()
	git(t, dir, append([]string{"add", "--"}, paths...)...)
	return commitAs(t, dir, "commit", "--quiet", "-m", message)
}

// withOrigin gives main a remote, origin, in a bare repository of its own, and
// has start ask it about trunk (git.remote). It returns origin's path.
func withOrigin(t *testing.T, main checkout) string {
	t.Helper()
	origin := filepath.Join(tempDir(t), "origin.git")
	git(t, main.dir, "init", "--quiet", "--bare", "--initial-branch=main", origin)
	setConfig(t, main.dir, "git", "remote", true)
	commitEverything(t, main.dir, "start asks origin about trunk")
	git(t, main.dir, "remote", "add", "origin", origin)
	git(t, main.dir, "push", "--quiet", "origin", "main")
	return origin
}

// originMovesOn puts a commit on origin's trunk that nobody has fetched.
func originMovesOn(t *testing.T, origin string) {
	t.Helper()
	other := filepath.Join(tempDir(t), "other")
	git(t, filepath.Dir(other), "clone", "--quiet", origin, other)
	writeFile(t, filepath.Join(other, "upstream.go"), "package main\n")
	commitEverything(t, other, "somebody else's commit")
	git(t, other, "push", "--quiet", "origin", "main")
}

// throughCodeReview takes a story frozen at Gate 3 on through code review,
// with its implementation written.
func (c checkout) throughCodeReview(t *testing.T) {
	t.Helper()
	c.runWith(t, "# Plan\n", "artifact", "write", "plan")
	c.run(t, "gate", "plan", "pass", "--note", "one step")
	c.reviewed(t, "design_review", "architect", "red-team", "security", "perf", "human-advocate")
	c.run(t, "gate", "design_review", "pass", "--note", "architect approved")
	writeFile(t, filepath.Join(c.dir, "invoice.go"), "package main\n\nfunc Invoice() {}\n")
	c.run(t, "gate", "implementation", "pass", "--note", "frozen tests green")
	c.runWith(t, "# Verification\n", "artifact", "write", "verification")
	c.run(t, "gate", "verification", "pass", "--note", "all commands green")
	c.reviewed(t, "verifier_review", "verifier")
	c.run(t, "gate", "verifier_review", "pass", "--note", "no gaming found")
	c.reviewed(t, "code_review", "code-reviewer", "security", "perf", "human-advocate")
	c.run(t, "gate", "code_review", "pass", "--note", "reviewer approved")
}

// refusedWith holds a command that had to be refused to the code given, and to
// a fix a person can act on.
func refusedWith(t *testing.T, out string, failed bool, code string) {
	t.Helper()
	if !failed {
		t.Fatalf("it went through, where %s was wanted:\n%s", code, out)
	}
	if !strings.Contains(out, code) {
		t.Errorf("the refusal is not %s:\n%s", code, out)
	}
	if fixLine(out) == "" {
		t.Errorf("the refusal does not say what to do:\n%s", out)
	}
}

// difference names each key that is new, gone or changed from before to after.
func difference(before, after map[string]string) string {
	keys := map[string]bool{}
	for k := range before {
		keys[k] = true
	}
	for k := range after {
		keys[k] = true
	}
	var out []string
	for _, k := range slices.Sorted(maps.Keys(keys)) {
		b, wasThere := before[k]
		a, isThere := after[k]
		switch {
		case !wasThere:
			out = append(out, "  new      "+k+"  "+a)
		case !isThere:
			out = append(out, "  gone     "+k)
		case a != b:
			out = append(out, "  changed  "+k)
		}
	}
	return strings.Join(out, "\n")
}

// ---------------------------------------------------------------- footprints

// footprint is what a start that fails leaves no trace in (AC-4): the
// checkouts git knows, the branches, the claims, and the loop's files in the
// main checkout, the statuses in the backlog among them. What is in
// .sdlc/worktrees is compared by name, less anything hidden there.
type footprint struct {
	worktrees, branches, claims, state map[string]string
	nested                             []string
}

func footprintOf(t *testing.T, main checkout) footprint {
	t.Helper()
	state := loopState(t, main)
	maps.DeleteFunc(state, func(name, _ string) bool { return strings.HasPrefix(name, "worktrees/") })
	return footprint{
		worktrees: worktreesOf(t, main.dir),
		branches:  branchesOf(t, main.dir),
		claims:    files(t, filepath.Join(main.dir, ".git", "sdlc", "claims")),
		state:     state,
		nested:    visibleEntries(t, filepath.Join(main.dir, ".sdlc", "worktrees")),
	}
}

// visibleEntries is the names in dir that do not start with a dot, and none
// where dir does not exist.
func visibleEntries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	var out []string
	for _, e := range list {
		if !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out
}

// unchanged reports each way after differs from f.
func (f footprint) unchanged(t *testing.T, after footprint, what string) {
	t.Helper()
	for _, c := range []struct {
		name          string
		before, after map[string]string
	}{
		{"the checkouts git lists", f.worktrees, after.worktrees},
		{"the branches", f.branches, after.branches},
		{"the claims", f.claims, after.claims},
		{"the loop's files in the main checkout", f.state, after.state},
	} {
		if !maps.Equal(c.before, c.after) {
			t.Errorf("%s changed %s:\n%s", what, c.name, difference(c.before, c.after))
		}
	}
	if !slices.Equal(f.nested, after.nested) {
		t.Errorf("%s left .sdlc/worktrees holding %v, where it held %v", what, after.nested, f.nested)
	}
}

// mainView is the main checkout as AC-2 holds it: what git status reports, the
// branch and commit it is on, its index, and every file in it. The loop's
// bookkeeping for the story is left out, and only that: its status and its
// updated time in the backlog, and its directory under .sdlc/stories.
type mainView struct {
	status []string
	head   string
	index  string
	files  map[string]string
}

func viewOf(t *testing.T, main checkout, id string) mainView {
	t.Helper()
	const backlog = "user_stories.json"
	record := ".sdlc/stories/" + id
	var status []string
	for _, line := range strings.Split(gitRaw(t, main.dir, "status", "--porcelain=v1", "--untracked-files=all"), "\n") {
		if len(line) < 4 {
			continue
		}
		if p := strings.Trim(line[3:], `"`); p != backlog && !strings.HasPrefix(p, record+"/") {
			status = append(status, line)
		}
	}
	tree := map[string]string{}
	err := filepath.WalkDir(main.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(main.dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" || rel == ".sdlc/worktrees" || rel == record {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		tree[rel] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	tree[backlog] = withoutBookkeeping(t, tree[backlog], id)
	return mainView{
		status: status,
		head:   gitOut(t, main.dir, "rev-parse", "--symbolic-full-name", "HEAD") + " at " + headOf(t, main.dir),
		index:  gitRaw(t, main.dir, "ls-files", "--stage"),
		files:  tree,
	}
}

// withoutBookkeeping is a backlog less what the loop writes into story id as it
// moves it along, its status and its updated time, encoded again so that two
// backlogs that differ only there read the same.
func withoutBookkeeping(t *testing.T, raw, id string) string {
	t.Helper()
	var backlog map[string]any
	if err := json.Unmarshal([]byte(raw), &backlog); err != nil {
		return raw
	}
	stories, _ := backlog["stories"].([]any)
	for _, s := range stories {
		if story, _ := s.(map[string]any); story != nil && story["id"] == id {
			delete(story, "status")
			delete(story, "updated")
		}
	}
	out, err := json.Marshal(backlog)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// same reports each way the main checkout moved from v to after.
func (v mainView) same(t *testing.T, after mainView, what string) {
	t.Helper()
	if !slices.Equal(v.status, after.status) {
		t.Errorf("after %s, git status in the main checkout reports\n  %s\nwhere before it reported\n  %s",
			what, strings.Join(after.status, "\n  "), strings.Join(v.status, "\n  "))
	}
	if v.head != after.head {
		t.Errorf("%s moved the main checkout from %s to %s", what, v.head, after.head)
	}
	if v.index != after.index {
		t.Errorf("%s changed the main checkout's index:\nbefore\n%s\nafter\n%s", what, v.index, after.index)
	}
	if !maps.Equal(v.files, after.files) {
		t.Errorf("%s changed the main checkout's files:\n%s", what, difference(v.files, after.files))
	}
}

// ---------------------------------------------------------------- the hook

// opening is where a Claude Code session was opened, which Claude Code names in
// CLAUDE_PROJECT_DIR, and where its shell is.
type opening struct {
	name         string
	project, cwd string
}

// openingsOf is each place a session working a story in its worktree can have
// been opened: the main checkout, the main checkout with its shell moved into
// the worktree, and the worktree itself.
func openingsOf(main, tree string) []opening {
	return []opening{
		{"opened in the main checkout", main, main},
		{"opened in the main checkout, its shell in the worktree", main, tree},
		{"opened in the worktree", tree, tree},
	}
}

// in spells a command run in the worktree from this opening's shell: as it is
// where the shell is there, and after a cd into it where it is not, which is
// how the runbook sends a command there.
func (o opening) in(tree, command string) string {
	if samePath(o.cwd, tree) {
		return command
	}
	return "cd " + strconv.Quote(tree) + " && " + command
}

// askAt puts a tool call to the hook through the shipped launcher, from a
// session opened at o, by agent, "" for the conversation.
func askAt(t *testing.T, binary, session string, o opening, agent, tool string, input map[string]any) reply {
	t.Helper()
	payload := map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      session,
		"tool_name":       tool,
		"cwd":             o.cwd,
		"tool_input":      input,
	}
	if agent != "" {
		payload["agent_type"] = "sdlc:" + agent
	}
	return parse(t, launch(t, binary, o.project, "PreToolUse", payload))
}

func writing(path string) map[string]any {
	return map[string]any{"file_path": path, "content": "x\n"}
}

func editing(path string) map[string]any {
	return map[string]any{"file_path": path, "old_string": "AC-1", "new_string": "AC-2"}
}

func running(command string) map[string]any {
	return map[string]any{"command": command}
}

// treeSession is a Claude Code session working a story in the worktree start
// made for it, as the runbook directs: every command runs in the worktree, and
// every file is named by its path there.
type treeSession struct {
	t      *testing.T
	binary string
	id     string
	at     opening
	tree   string
}

func (s *treeSession) here() checkout {
	return checkout{binary: s.binary, dir: s.tree, session: s.id}
}

// do is a tool call the runbook makes, which the hook has to allow.
func (s *treeSession) do(agent, tool string, input map[string]any) {
	s.t.Helper()
	if r := askAt(s.t, s.binary, s.id, s.at, agent, tool, input); denied(r) {
		s.t.Fatalf("%s: the runbook told %s to run %s, and the hook refused it:\n  %s",
			s.at.name, describeAgent(agent), describeInput(tool, input), r.HookSpecificOutput.PermissionDecisionReason)
	}
}

func (s *treeSession) refused(agent, tool string, input map[string]any) {
	s.t.Helper()
	if r := askAt(s.t, s.binary, s.id, s.at, agent, tool, input); !denied(r) {
		s.t.Errorf("%s: %s was allowed to run %s", s.at.name, describeAgent(agent), describeInput(tool, input))
	}
}

func (s *treeSession) run(agent string, args ...string) string {
	s.t.Helper()
	s.do(agent, "Bash", running(s.at.in(s.tree, "sdlc "+strings.Join(args, " "))))
	return s.here().run(s.t, args...)
}

func (s *treeSession) stores(agent, kind, body string) {
	s.t.Helper()
	s.do(agent, "Bash", running(s.at.in(s.tree, "sdlc artifact write "+kind)))
	s.here().runWith(s.t, body, "artifact", "write", kind)
}

func (s *treeSession) writes(agent, rel, body string) {
	s.t.Helper()
	path := filepath.Join(s.tree, filepath.FromSlash(rel))
	s.do(agent, "Write", map[string]any{"file_path": path, "content": body})
	writeFile(s.t, path, body)
}

// nextGate is what the runbook reads to know where the story is.
func (s *treeSession) nextGate() string {
	s.t.Helper()
	var status struct {
		Active   string `json:"active"`
		NextGate string `json:"next_gate"`
	}
	if err := json.Unmarshal([]byte(s.here().run(s.t, "status", "--json")), &status); err != nil {
		s.t.Fatal(err)
	}
	if status.Active == "" {
		return ""
	}
	return status.NextGate
}

// workGate does what the runbook says at one gate, in the worktree, as whoever
// it delegates the gate to, and records the outcome.
func (s *treeSession) workGate(gate model.Gate, story string) {
	s.t.Helper()
	switch gate {
	case model.GateDoR:
	case model.GateAnalysis:
		s.stores("researcher", "analysis", "# Analysis\n\nAC-1 is testable.\n")
		s.stores("researcher", "threats", "# Threats\n\nNothing crosses a trust boundary.\n")
	case model.GateTestsFrozen:
		s.writes("sdet", "invoice_test.go", "package main\n\n// AC-1\n")
		s.stores("sdet", "test_plan", "# Test plan\n\nAC-1 -> TestRejectsZero\n")
		s.run("sdet", "freeze")
	case model.GatePlan:
		// The freeze holds in the worktree as it does in one checkout.
		s.refused("implementer", "Edit", editing(filepath.Join(s.tree, "invoice_test.go")))
		s.stores("implementer", "plan", "# Plan\n\nOne step.\n")
	case model.GateImplementation:
		s.writes("implementer", "invoice.go", "package main\n\nfunc Invoice() {}\n")
	case model.GateVerification:
		s.stores("verifier", "verification", "# Verification\n\nEvery command green.\n")
	case model.GateCommit:
		git(s.t, s.tree, "add", "-A")
		s.do("", "Bash", running(s.at.in(s.tree, "git commit -m "+story)))
		git(s.t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", story)
	case model.GateRetro:
		s.stores("bookkeeper", "retro", "# Retro\n\nNo deviations.\n")
	}
	for _, r := range model.ReviewersFor(gate) {
		if r.WhenDoRAdvocate {
			continue
		}
		s.do(r.Role, "Bash", running(s.at.in(s.tree, "sdlc review add "+string(gate)+" "+r.Role+" approve")))
		s.here().runWith(s.t, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
	}
	s.run("", "gate", string(gate), "pass", "--note", "worked by the session")
}

// ---------------------------------------------------------------- AC-1

func TestAC1StartMakesAWorktreeOnAStoryBranchCutFromTrunkAndSaysWhere(t *testing.T) {
	binary := build(t)

	t.Run("AC-1 from the main checkout on trunk", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
		tip := headOf(t, main.dir)
		w := startIn(t, main, main.as(sessionFirst))
		if w.story != "US-001" || w.doc["ok"] != true || w.doc["resume"] != false || w.doc["session"] != sessionFirst {
			t.Errorf("start --json gave %v, want US-001 begun in %s", w.doc, sessionFirst)
		}
		cutFromTrunk(t, main, w, tip)

		// The story is held in its worktree: its state is there, and the main
		// checkout's own is left alone.
		if got := readFile(t, filepath.Join(w.dir, ".sdlc", "state", "active")); got != "US-001\n" {
			t.Errorf("the worktree's .sdlc/state/active is %q, want US-001", got)
		}
		if got := readFile(t, filepath.Join(w.dir, ".sdlc", "state", "session")); got != sessionFirst+"\n" {
			t.Errorf("the worktree's .sdlc/state/session is %q, want %s", got, sessionFirst)
		}
		for _, name := range []string{"active", "session"} {
			if exists(filepath.Join(main.dir, ".sdlc", "state", name)) {
				t.Errorf("start wrote .sdlc/state/%s in the main checkout", name)
			}
		}
		if claim := readFile(t, filepath.Join(main.dir, ".git", "sdlc", "claims", "US-001.json")); !recorded(claim, w.dir) {
			t.Errorf("the claim on US-001 does not name its worktree %s:\n%s", w.dir, claim)
		}
		if got := statuses(t, main.backlog())["US-001"]; got != "in_progress" {
			t.Errorf("the backlog has US-001 %q, want in_progress", got)
		}
		if base := recordOf(t, main, "US-001").Base; base != tip {
			t.Errorf("the record's base is %q, want the commit the branch was cut from, %s", base, tip)
		}
	})

	t.Run("AC-1 in text, start names the worktree and the branch", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		out := main.as(sessionFirst).run(t, "start")
		if path := storyPath(main, "US-001"); !names(out, path) || !strings.Contains(out, "story/US-001") {
			t.Errorf("start does not print the worktree %s and the branch story/US-001:\n%s", path, out)
		}
	})

	t.Run("AC-1 the branch is cut from the trunk the configuration names, not from where the main checkout is", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		git(t, main.dir, "branch", "-m", "main", "develop")
		setConfig(t, main.dir, "git", "trunk_branch", "develop")
		commitEverything(t, main.dir, "trunk is develop")
		tip := headOf(t, main.dir)
		git(t, main.dir, "switch", "--quiet", "-c", "side")
		writeFile(t, filepath.Join(main.dir, "side.go"), "package main\n")
		commitEverything(t, main.dir, "a person's own work on a branch of their own")

		w := startIn(t, main, main.as(sessionFirst))
		cutFromTrunk(t, main, w, tip)
		if base := recordOf(t, main, "US-001").Base; base != tip {
			t.Errorf("the record's base is %q, want develop's tip %s, not the main checkout's HEAD", base, tip)
		}
	})

	// OQ-4: a new story started from a linked worktree a person made gets a
	// worktree of its own too, in the main checkout.
	t.Run("AC-1 from a linked worktree a person made", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		tip := headOf(t, main.dir)
		hand := filepath.Join(tempDir(t), "hand")
		git(t, main.dir, "worktree", "add", "--quiet", "-b", "hand", hand)

		w := startIn(t, main, checkout{binary: binary, dir: hand, session: sessionFirst})
		cutFromTrunk(t, main, w, tip)
		if exists(filepath.Join(hand, ".sdlc", "worktrees")) || exists(filepath.Join(hand, ".sdlc", "state", "active")) {
			t.Error("start made the story's worktree in, or held the story in, the linked worktree it ran in")
		}
	})

	// D7(6) and T10: a checkout's path is kept in the claim and nowhere else,
	// and start printing it is output, not storage.
	t.Run("AC-1 nothing the loop keeps names where the worktree is", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		w := startIn(t, main, main.as(sessionFirst))
		w.frozen(t, "us_001_test.go")
		w.runWith(t, "# Plan\n", "artifact", "write", "plan")
		w.run(t, "gate", "plan", "pass", "--note", "one step")
		w.reviewed(t, "design_review", "architect")
		w.run(t, "cost", "add", "--usd", "1.00")
		w.run(t, "escalate", "spec_unclear", "--message", "which greeting?")
		w.as("").run(t, "approve", "US-001")

		kept := files(t, filepath.Join(main.dir, ".sdlc", "stories"))
		kept["<backlog>"] = readFile(t, main.backlog())
		for name, body := range files(t, filepath.Join(w.dir, ".sdlc")) {
			kept["<worktree>/.sdlc/"+name] = body
		}
		for _, name := range slices.Sorted(maps.Keys(kept)) {
			for _, p := range []string{main.dir, strings.TrimPrefix(main.dir, "/private")} {
				if recorded(kept[name], p) {
					t.Errorf("%s names the path %s", name, p)
				}
			}
		}
	})
}

// ---------------------------------------------------------------- AC-2

func TestAC2StartLeavesTheMainCheckoutAsItWasApartFromTheLoopsBookkeeping(t *testing.T) {
	binary := build(t)

	for _, c := range []struct {
		name    string
		prepare func(t *testing.T, main checkout)
	}{
		{"AC-2 on trunk with nothing uncommitted", func(*testing.T, checkout) {}},
		{"AC-2 on a branch of a person's own, with work staged, changed and new", func(t *testing.T, main checkout) {
			git(t, main.dir, "switch", "--quiet", "-c", "mine")
			writeFile(t, filepath.Join(main.dir, "staged.go"), "package main\n")
			git(t, main.dir, "add", "staged.go")
			writeFile(t, filepath.Join(main.dir, "go.mod"), readFile(t, filepath.Join(main.dir, "go.mod"))+"// a person's edit\n")
			writeFile(t, filepath.Join(main.dir, "notes", "scratch.txt"), "mine\n")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
			c.prepare(t, main)
			before := viewOf(t, main, "US-001")
			startIn(t, main, main.as(sessionFirst))
			before.same(t, viewOf(t, main, "US-001"), "start")
		})
	}

	// T3: git's own variables, set where start runs, may not turn the worktree
	// start makes into the main checkout.
	t.Run("AC-2 with GIT_DIR, GIT_WORK_TREE and GIT_INDEX_FILE naming the main checkout", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		tip := headOf(t, main.dir)
		before, was := viewOf(t, main, "US-001"), footprintOf(t, main)

		cmd := main.as(sessionFirst).command(t, "", "start", "--json")
		cmd.Env = append(cmd.Env,
			"GIT_DIR="+filepath.Join(main.dir, ".git"),
			"GIT_WORK_TREE="+main.dir,
			"GIT_INDEX_FILE="+filepath.Join(main.dir, ".git", "index"))
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()

		before.same(t, viewOf(t, main, "US-001"), "start with git's variables set")
		if err != nil {
			// Refusing while they are set is one answer (THREATS T3), and then
			// nothing is left behind.
			refusedWith(t, stdout.String()+stderr.String(), true, "SDLC-E")
			was.unchanged(t, footprintOf(t, main), "the refused start")
			return
		}
		w := storyPath(main, "US-001")
		if got := gitOut(t, w, "rev-parse", "--abbrev-ref", "HEAD"); got != "story/US-001" {
			t.Errorf("the worktree has %q checked out, want story/US-001", got)
		}
		if got := headOf(t, w); got != tip {
			t.Errorf("the worktree is on %s, want trunk's tip %s", got, tip)
		}
		if !exists(filepath.Join(w, "go.mod")) {
			t.Error("the worktree has none of trunk's files: the checkout went somewhere else")
		}
	})
}

// ---------------------------------------------------------------- AC-3

func TestAC3AWorktreeStartIsNotRefusedForWhereTheMainCheckoutIsOrWhatIsInIt(t *testing.T) {
	binary := build(t)
	for _, c := range []struct {
		name    string
		prepare func(t *testing.T, main checkout)
	}{
		{"AC-3 the main checkout on another branch, SDLC-E0038 in one checkout", func(t *testing.T, main checkout) {
			git(t, main.dir, "switch", "--quiet", "-c", "mine")
			writeFile(t, filepath.Join(main.dir, "mine.go"), "package main\n")
			commitEverything(t, main.dir, "a person's own commit")
		}},
		{"AC-3 the main checkout with a detached HEAD", func(t *testing.T, main checkout) {
			git(t, main.dir, "switch", "--quiet", "--detach")
		}},
		{"AC-3 uncommitted work in the main checkout, SDLC-E0039 in one checkout", func(t *testing.T, main checkout) {
			writeFile(t, filepath.Join(main.dir, "staged.go"), "package main\n")
			git(t, main.dir, "add", "staged.go")
			writeFile(t, filepath.Join(main.dir, "go.mod"), readFile(t, filepath.Join(main.dir, "go.mod"))+"// a person's edit\n")
			writeFile(t, filepath.Join(main.dir, "stray.go"), "package main\n")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
			tip := headOf(t, main.dir)
			c.prepare(t, main)
			cutFromTrunk(t, main, startIn(t, main, main.as(sessionFirst)), tip)
		})
	}
}

func TestAC3SmokeRunsInTheNewWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the smoke check here records its directory with pwd, which prints a POSIX path under Git for Windows")
	}
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
	seen := tempDir(t)
	setConfig(t, main.dir, "commands", "smoke", "pwd -P > "+strconv.Quote(filepath.Join(seen, "dir"))+
		" && git rev-parse --abbrev-ref HEAD > "+strconv.Quote(filepath.Join(seen, "branch")))
	commitEverything(t, main.dir, "a smoke check that says where it ran")

	w := startIn(t, main, main.as(sessionFirst))
	if got := strings.TrimSpace(readFile(t, filepath.Join(seen, "dir"))); !samePath(got, w.dir) {
		t.Errorf("commands.smoke ran in %q, want the new worktree %s", got, w.dir)
	}
	if got := strings.TrimSpace(readFile(t, filepath.Join(seen, "branch"))); got != w.branch {
		t.Errorf("commands.smoke ran with %q checked out, want %s", got, w.branch)
	}
}

func TestAC3AWorktreeStartIsStillRefusedWhenTrunkIsBehindItsRemote(t *testing.T) {
	binary := build(t)

	behind := func(t *testing.T, prepare func(main checkout)) {
		t.Helper()
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		originMovesOn(t, withOrigin(t, main))
		prepare(main)
		was := footprintOf(t, main)
		out, code := main.as(sessionFirst).try(t, "start")
		refusedWith(t, out, code != 0, "SDLC-E0040")
		was.unchanged(t, footprintOf(t, main), "the start refused over trunk")
	}

	t.Run("AC-3 the main checkout on trunk", func(t *testing.T) {
		behind(t, func(checkout) {})
	})
	// Trunk is behind, and HEAD is not: what is measured is trunk.
	t.Run("AC-3 the main checkout on a branch level with origin", func(t *testing.T) {
		behind(t, func(main checkout) {
			git(t, main.dir, "fetch", "--quiet", "origin")
			git(t, main.dir, "switch", "--quiet", "-c", "level", "origin/main")
		})
	})
	// HEAD is behind, and trunk is not: that is the person's own branch, which
	// AC-3 does not refuse for.
	t.Run("AC-3 trunk level with origin, and the main checkout on a branch behind it", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		origin := withOrigin(t, main)
		git(t, main.dir, "branch", "old")
		originMovesOn(t, origin)
		git(t, main.dir, "pull", "--quiet", "--ff-only", "origin", "main")
		git(t, main.dir, "switch", "--quiet", "old")
		tip := gitOut(t, main.dir, "rev-parse", "refs/heads/main")
		cutFromTrunk(t, main, startIn(t, main, main.as(sessionFirst)), tip)
	})
}

// ---------------------------------------------------------------- AC-4

func TestAC4AWorktreeThatCannotBeMadeIsRefusedAndLeavesNothingBehind(t *testing.T) {
	binary := build(t)

	// T6: a branch by the story's name is a person's, or one a crashed start
	// left, and is neither reused nor reset nor deleted.
	t.Run("AC-4 a branch named for the story is there already", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		git(t, main.dir, "switch", "--quiet", "-c", "story/US-001")
		writeFile(t, filepath.Join(main.dir, "theirs.go"), "package main\n")
		commitEverything(t, main.dir, "a person's own branch, by the name start gives one")
		git(t, main.dir, "switch", "--quiet", "main")

		was := footprintOf(t, main)
		out, code := main.as(sessionFirst).try(t, "start")
		refusedWith(t, out, code != 0, "SDLC-E0053")
		if !strings.Contains(out, "story/US-001") {
			t.Errorf("the refusal does not name the branch in the way:\n%s", out)
		}
		was.unchanged(t, footprintOf(t, main), "the refused start")
	})

	t.Run("AC-4 something is at .sdlc/worktrees/US-001 already", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		notes := filepath.Join(storyPath(main, "US-001"), "notes.txt")
		writeFile(t, notes, "a person's own\n")

		was := footprintOf(t, main)
		out, code := main.as(sessionFirst).try(t, "start")
		refusedWith(t, out, code != 0, "SDLC-E0053")
		if got := readFile(t, notes); got != "a person's own\n" {
			t.Errorf("the refused start changed what was at %s: %q", notes, got)
		}
		was.unchanged(t, footprintOf(t, main), "the refused start")
	})
}

func TestAC4ASmokeCheckThatFailsInTheWorktreeIsRefusedAndLeavesNothingBehind(t *testing.T) {
	binary := build(t)
	// .git is a directory in the main checkout and a file in a linked worktree,
	// so this passes where the released tool runs it and fails in the worktree.
	const passesOnlyInTheMainCheckout = "test -d .git"

	t.Run("AC-4 the first story", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		setConfig(t, main.dir, "commands", "smoke", passesOnlyInTheMainCheckout)
		commitEverything(t, main.dir, "a smoke check a linked worktree fails")

		was := footprintOf(t, main)
		out, code := main.as(sessionFirst).try(t, "start")
		refusedWith(t, out, code != 0, "SDLC-E0041")
		if strings.Contains(out, "SDLC-E0053") {
			t.Errorf("a smoke check that failed is refused as a worktree that could not be made:\n%s", out)
		}
		was.unchanged(t, footprintOf(t, main), "the refused start")
	})

	// What start removes is what it made: the worktree of a story already held
	// stays as it is.
	t.Run("AC-4 a second story, beside a first held in its worktree", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
		first := startIn(t, main, main.as(sessionFirst))
		first.analysed(t)
		setConfig(t, main.dir, "commands", "smoke", passesOnlyInTheMainCheckout)
		commitPaths(t, main.dir, "a smoke check a linked worktree fails", ".sdlc/config.json")

		was, held := footprintOf(t, main), files(t, first.dir)
		out, code := main.as(sessionSecond).try(t, "start")
		refusedWith(t, out, code != 0, "SDLC-E0041")
		was.unchanged(t, footprintOf(t, main), "the refused start")
		if after := files(t, first.dir); !maps.Equal(held, after) {
			t.Errorf("the refused start changed the first story's worktree:\n%s", difference(held, after))
		}
	})
}

// ---------------------------------------------------------------- AC-5

func TestAC5ResumingAStoryPrintsTheSamePathAndBranchAndMakesNothing(t *testing.T) {
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	first := startIn(t, main, main.as(sessionFirst))
	cut := headOf(t, first.dir)

	resumed := func(t *testing.T, from checkout, args ...string) {
		t.Helper()
		was := footprintOf(t, main)
		again := startIn(t, main, from, args...)
		if again.story != first.story || again.doc["resume"] != true {
			t.Errorf("start from %s gave %s (resume %v), want %s resumed", from.dir, again.story, again.doc["resume"], first.story)
		}
		if again.pathField != first.pathField || again.branchField != first.branchField {
			t.Errorf("resuming names the worktree in %q and %q, where starting named it in %q and %q",
				again.pathField, again.branchField, first.pathField, first.branchField)
		}
		after := footprintOf(t, main)
		for _, c := range []struct {
			name          string
			before, after map[string]string
		}{
			{"the checkouts git lists", was.worktrees, after.worktrees},
			{"the branches", was.branches, after.branches},
			{"the claims", was.claims, after.claims},
		} {
			if !maps.Equal(c.before, c.after) {
				t.Errorf("resuming changed %s:\n%s", c.name, difference(c.before, c.after))
			}
		}
		if !slices.Equal(was.nested, after.nested) {
			t.Errorf("resuming left .sdlc/worktrees holding %v, where it held %v", after.nested, was.nested)
		}
		if got := headOf(t, first.dir); got != cut {
			t.Errorf("resuming moved the worktree from %s to %s", cut, got)
		}
	}

	t.Run("AC-5 from the main checkout", func(t *testing.T) {
		resumed(t, main.as(sessionFirst))
	})
	t.Run("AC-5 from the worktree", func(t *testing.T) {
		resumed(t, first.checkout)
	})
	t.Run("AC-5 in text", func(t *testing.T) {
		if out := main.as(sessionFirst).run(t, "start"); !names(out, first.dir) || !strings.Contains(out, first.branch) {
			t.Errorf("resuming does not print the worktree %s and the branch %s:\n%s", first.dir, first.branch, out)
		}
	})
	// OQ-5: another session takes the story over where it is held.
	t.Run("AC-5 by name from the main checkout, in another session", func(t *testing.T) {
		resumed(t, main.as(sessionSecond), "US-001")
		if got := readFile(t, filepath.Join(first.dir, ".sdlc", "state", "session")); got != sessionSecond+"\n" {
			t.Errorf("the worktree's .sdlc/state/session is %q, want the session that took the story over, %s", got, sessionSecond)
		}
	})
	t.Run("AC-5 put down part-way, then from the worktree", func(t *testing.T) {
		first.as("").run(t, "stop")
		resumed(t, first.as(sessionSecond))
	})
	t.Run("AC-5 put down part-way, then by name from the main checkout", func(t *testing.T) {
		first.as("").run(t, "stop")
		resumed(t, main.as(sessionFirst), "US-001")
	})
}

// ---------------------------------------------------------------- AC-6

// The runbook's own walk, from each place a session can be opened: every call
// past the shipped launcher, every command in the worktree.
func TestAC6ASessionWorksItsStoryInItsWorktreeTheWayTheRunbookSaysTo(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)

	for _, c := range []struct {
		name         string
		openedInMain bool
	}{
		{"AC-6 a session opened in the main checkout", true},
		{"AC-6 a session opened in the worktree, of a story a person started in a terminal", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
			cut := headOf(t, main.dir)
			before := viewOf(t, main, "US-001")
			s := &treeSession{t: t, binary: binary, id: sessionFirst}
			if c.openedInMain {
				s.at = opening{"opened in the main checkout", main.dir, main.dir}
				s.do("", "Bash", running("sdlc start --json"))
				s.tree = startIn(t, main, main.as(sessionFirst)).dir
			} else {
				w := startIn(t, main, main.as(""))
				s.tree = w.dir
				s.at = opening{"opened in the worktree", w.dir, w.dir}
				s.run("", "start", "--json")
				if got := readFile(t, filepath.Join(w.dir, ".sdlc", "state", "session")); got != sessionFirst+"\n" {
					t.Fatalf("resuming in the worktree recorded %q as the session, want %s", got, sessionFirst)
				}
			}

			for gate := s.nextGate(); gate != ""; gate = s.nextGate() {
				s.workGate(model.Gate(gate), "US-001")
			}

			record := recordOf(t, main, "US-001")
			for _, gate := range model.Gates {
				if got := record.Gates[string(gate)].Status; got != "pass" {
					t.Errorf("%s is %q on the record in the main checkout, want pass", gate, got)
				}
			}
			if got := revList(t, main.dir, cut, "story/US-001"); len(got) != 1 {
				t.Errorf("story/US-001 holds %d commits past where it was cut, want the story's one", len(got))
			}
			if got := gitOut(t, main.dir, "rev-parse", "refs/heads/main"); got != cut {
				t.Errorf("trunk moved to %s while the story was worked in its worktree", got)
			}
			// Checked before the stop: since US-029.3, the stop of a finished
			// story in its worktree lands it, which moves trunk in the main
			// checkout by design. Working the story is what leaves it alone.
			before.same(t, viewOf(t, main, "US-001"), "working the story in its worktree")
			s.run("", "stop")
		})
	}
}

// What a session is held to in one checkout, asked of the hook for files and
// commands in the story's worktree, from each place the session can be opened.
func TestAC6TheHookHoldsTheSessionInItsWorktreeToWhatItHoldsASingleCheckoutTo(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
	w := startIn(t, main, main.as(sessionFirst))
	openings := openingsOf(main.dir, w.dir)
	in := func(rel string) string { return filepath.Join(w.dir, filepath.FromSlash(rel)) }
	q := strconv.Quote

	type call struct {
		what, agent, tool string
		input             func(o opening) map[string]any
		// says is what a refusal has to name, besides a route.
		says string
	}
	fixed := func(input map[string]any) func(opening) map[string]any {
		return func(opening) map[string]any { return input }
	}
	inTree := func(command string) func(opening) map[string]any {
		return func(o opening) map[string]any { return running(o.in(w.dir, command)) }
	}
	refused := func(t *testing.T, calls ...call) {
		t.Helper()
		for _, o := range openings {
			for _, c := range calls {
				r := askAt(t, binary, sessionFirst, o, c.agent, c.tool, c.input(o))
				reason := r.HookSpecificOutput.PermissionDecisionReason
				switch {
				case !denied(r):
					t.Errorf("%s: %s may %s", o.name, describeAgent(c.agent), c.what)
				case !strings.Contains(reason, c.says) || !strings.Contains(reason, "Instead:"):
					t.Errorf("%s: the refusal to %s does not say %q and what to do instead: %s", o.name, c.what, c.says, reason)
				}
			}
		}
	}
	allowed := func(t *testing.T, calls ...call) {
		t.Helper()
		for _, o := range openings {
			for _, c := range calls {
				if r := askAt(t, binary, sessionFirst, o, c.agent, c.tool, c.input(o)); denied(r) {
					t.Errorf("%s: %s may not %s: %s", o.name, describeAgent(c.agent), c.what, r.HookSpecificOutput.PermissionDecisionReason)
				}
			}
		}
	}

	t.Run("AC-6 before any gate has passed", func(t *testing.T) {
		refused(t,
			call{"commit", "", "Bash", inTree(`git commit -m "done"`), "has not passed"},
			call{"commit with git -C", "", "Bash", fixed(running("git -C " + q(w.dir) + ` commit -m "done"`)), "has not passed"},
			call{"write a gate document in place", "", "Write",
				fixed(writing(filepath.Join(main.dir, ".sdlc", "stories", "US-001", "ANALYSIS.md"))), ""},
			call{"write the worktree's .sdlc/state/active", "researcher", "Write", fixed(writing(in(".sdlc/state/active"))), ""},
			call{"remove the worktree's .sdlc/state/session", "", "Bash", fixed(running("rm -f " + q(in(".sdlc/state/session")))), ""},
			call{"write the worktree's .sdlc/config.json", "", "Write", fixed(writing(in(".sdlc/config.json"))), ""},
			call{"rewrite the worktree's .git", "", "Bash", fixed(running("echo 'gitdir: /elsewhere' > " + q(in(".git")))), ""},
			call{"rewrite where git keeps the worktree", "", "Write",
				fixed(writing(filepath.Join(main.dir, ".git", "worktrees", "US-001", "gitdir"))), ""},
			call{"write production code at the analysis gate", "researcher", "Write", fixed(writing(in("invoice.go"))), ""},
		)
		allowed(t,
			call{"write an acceptance test", "sdet", "Write", fixed(writing(in("invoice_test.go"))), ""},
			call{"store the analysis", "researcher", "Bash", inTree("sdlc artifact write analysis"), ""},
		)
	})

	t.Run("AC-6 the Stop guard sends a stop mid-story back", func(t *testing.T) {
		for _, o := range []opening{openings[0], openings[2]} {
			out := launch(t, binary, o.project, "Stop", map[string]any{
				"hook_event_name":  "Stop",
				"session_id":       sessionFirst,
				"cwd":              o.cwd,
				"stop_hook_active": false,
			})
			var r struct {
				Decision string `json:"decision"`
			}
			if err := json.Unmarshal([]byte(out), &r); err != nil || r.Decision != "block" {
				t.Errorf("%s: the Stop hook let a stop mid-story through: %s", o.name, out)
			}
		}
	})

	w.frozen(t, "invoice_test.go")

	t.Run("AC-6 after the freeze", func(t *testing.T) {
		refused(t,
			call{"edit the frozen test", "implementer", "Edit", fixed(editing(in("invoice_test.go"))), "sdlc unfreeze"},
			call{"write the frozen test", "", "Write", fixed(writing(in("invoice_test.go"))), "sdlc unfreeze"},
			call{"overwrite the frozen test by its path", "implementer", "Bash",
				fixed(running("echo cheat > " + q(in("invoice_test.go")))), ""},
			call{"overwrite the frozen test in the worktree", "implementer", "Bash", inTree("echo cheat > invoice_test.go"), ""},
			call{"write production code as the test author", "sdet", "Write", fixed(writing(in("invoice.go"))), ""},
		)
	})

	w.throughCodeReview(t)

	t.Run("AC-6 with every gate before the commit passed", func(t *testing.T) {
		allowed(t, call{"commit", "", "Bash", inTree(`git commit -m "US-001"`), ""})
	})
}

// What US-029.1 refuses a session held in a linked worktree, in the main
// checkout that keeps the backlog, the records and the claims, holds for a
// session held in its story's worktree.
func TestAC6TheSessionInAStoryWorktreeIsRefusedTheMainCheckoutsBacklogRecordsAndClaims(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	first := startIn(t, main, main.as(sessionFirst))
	second := startIn(t, main, main.as(sessionSecond))
	openings := openingsOf(main.dir, first.dir)

	for _, r := range routes {
		t.Run("AC-6 "+r.name, func(t *testing.T) {
			for _, o := range openings {
				if got := askAt(t, binary, sessionFirst, o, "", r.tool, r.input(main.dir, "US-001", "US-002")); !denied(got) {
					t.Errorf("%s: the session held in its story's worktree may %s", o.name, r.name)
				}
			}
		})
	}

	t.Run("AC-6 the claims", func(t *testing.T) {
		dir := filepath.Join(main.dir, ".git", "sdlc", "claims")
		claims := slices.Sorted(maps.Keys(files(t, dir)))
		if len(claims) != 2 {
			t.Fatalf("precondition: the claims are %v, want one on each story", claims)
		}
		for _, name := range claims {
			path := filepath.Join(dir, filepath.FromSlash(name))
			for _, o := range openings {
				for _, c := range []struct {
					tool  string
					input map[string]any
				}{
					{"Write", writing(path)},
					{"Bash", running("rm -f " + strconv.Quote(path))},
				} {
					if got := askAt(t, binary, sessionFirst, o, "", c.tool, c.input); !denied(got) {
						t.Errorf("%s: the session may %s %v", o.name, c.tool, c.input)
					}
				}
			}
		}
	})

	// D5 of US-029.1: the copy git checked out is never read, and an edit to it
	// would be committed on the story's branch.
	t.Run("AC-6 the worktree's own copy of the backlog", func(t *testing.T) {
		for _, o := range openings {
			for _, c := range []struct {
				tool  string
				input map[string]any
			}{
				{"Write", writing(filepath.Join(first.dir, "user_stories.json"))},
				{"Bash", running(o.in(first.dir, "echo '{}' > user_stories.json"))},
			} {
				if got := askAt(t, binary, sessionFirst, o, "", c.tool, c.input); !denied(got) {
					t.Errorf("%s: the session may %s %v", o.name, c.tool, c.input)
				}
			}
		}
	})

	t.Run("AC-6 the other story's worktree's loop state", func(t *testing.T) {
		for _, o := range openings {
			for _, c := range []struct {
				tool  string
				input map[string]any
			}{
				{"Write", writing(filepath.Join(second.dir, ".sdlc", "state", "session"))},
				{"Bash", running("rm -f " + strconv.Quote(filepath.Join(second.dir, ".sdlc", "state", "active")))},
			} {
				if got := askAt(t, binary, sessionFirst, o, "", c.tool, c.input); !denied(got) {
					t.Errorf("%s: the session may %s %v", o.name, c.tool, c.input)
				}
			}
		}
	})
}

// D4 to D6: a held session may not end or move its hold with git's worktree
// commands or by moving its checkout, may not make a worktree, and may not
// commit in another checkout by any spelling that points git there.
func TestAC6AHeldSessionCannotEndItsHoldOrCommitInAnotherCheckout(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
	w := startIn(t, main, main.as(sessionFirst))
	q := strconv.Quote
	elsewhere := filepath.Join(tempDir(t), "elsewhere")

	for _, o := range openingsOf(main.dir, w.dir) {
		for _, c := range []struct{ what, command string }{
			{"remove its worktree", "git worktree remove --force " + q(w.dir)},
			{"remove its worktree from inside it", o.in(w.dir, "git worktree remove --force .")},
			{"remove its worktree through the main checkout", "git -C " + q(main.dir) + " worktree remove --force " + q(w.dir)},
			{"move its worktree", "git worktree move " + q(w.dir) + " " + q(elsewhere)},
			{"prune worktrees", o.in(w.dir, "git worktree prune")},
			{"move its checkout with mv", "mv " + q(w.dir) + " " + q(elsewhere)},
			{"remove its checkout with rm", "rm -rf " + q(w.dir)},
			{"remove its checkout from the directory above it", o.in(w.dir, "cd .. && rm -rf US-001")},
			{"add a worktree among the records", "git worktree add " + q(filepath.Join(main.dir, ".sdlc", "stories", "x"))},
			{"add a worktree where start puts them", "git worktree add -b mine " + q(filepath.Join(main.dir, ".sdlc", "worktrees", "US-009"))},
		} {
			t.Run("AC-6 "+o.name+", "+c.what, func(t *testing.T) {
				if got := askAt(t, binary, sessionFirst, o, "", "Bash", running(c.command)); !denied(got) {
					t.Errorf("the session held in %s may %s: %s", w.dir, c.what, c.command)
				}
			})
		}
	}

	// With the gates passed the session's own commit goes through, so a commit
	// refused now is refused for where it would land.
	w.frozen(t, "invoice_test.go")
	w.throughCodeReview(t)
	gitDir := filepath.Join(main.dir, ".git")
	for _, o := range openingsOf(main.dir, w.dir)[1:] {
		if got := askAt(t, binary, sessionFirst, o, "", "Bash", running(`git commit -m "US-001"`)); denied(got) {
			t.Fatalf("precondition: %s, the session's own commit is refused: %s", o.name, got.HookSpecificOutput.PermissionDecisionReason)
		}
		for _, c := range []struct{ what, command string }{
			{"git -C", "git -C " + q(main.dir) + ` commit -m "x"`},
			{"cd", "cd " + q(main.dir) + ` && git commit -m "x"`},
			{"--git-dir and --work-tree", "git --git-dir=" + q(gitDir) + " --work-tree=" + q(main.dir) + ` commit -m "x"`},
			{"GIT_DIR and GIT_WORK_TREE", "GIT_DIR=" + q(gitDir) + " GIT_WORK_TREE=" + q(main.dir) + ` git commit -m "x"`},
			{"GIT_WORK_TREE alone", "GIT_WORK_TREE=" + q(main.dir) + ` git commit -m "x"`},
			{"export", "export GIT_DIR=" + q(gitDir) + " GIT_WORK_TREE=" + q(main.dir) + `; git commit -m "x"`},
			{"env", "env GIT_DIR=" + q(gitDir) + " GIT_WORK_TREE=" + q(main.dir) + ` git commit -m "x"`},
			{"core.worktree", "git -c core.worktree=" + q(main.dir) + ` commit -m "x"`},
			{"an add with --work-tree, then a commit", "git --work-tree=" + q(main.dir) + ` add -A && git commit -m "x"`},
		} {
			t.Run("AC-6 "+o.name+", a commit in the main checkout by "+c.what, func(t *testing.T) {
				if got := askAt(t, binary, sessionFirst, o, "", "Bash", running(c.command)); !denied(got) {
					t.Errorf("the session held in %s may commit in the main checkout: %s", w.dir, c.command)
				}
			})
		}
	}
}

// D5 and THREATS T7: a story's worktree is inside the main checkout, so a
// session the main checkout holds reaches it by path, and may not use that to
// edit another story's frozen tests or loop state.
func TestAC6ASessionHeldInTheMainCheckoutMayNotEditAStoryWorktreeInsideIt(t *testing.T) {
	skipWithoutTheLauncher(t)
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	w := startIn(t, main, main.as(sessionFirst))
	w.frozen(t, "us_001_test.go")
	// The main checkout holds a story of its own: a person turned story
	// worktrees off there, with the first story's worktree still inside it.
	worktreesOff(t, main.dir)
	main.as(sessionSecond).run(t, "start")
	if got := readFile(t, filepath.Join(main.dir, ".sdlc", "state", "active")); got != "US-002\n" {
		t.Fatalf("precondition: the main checkout holds %q, want US-002", got)
	}
	here := opening{"opened in the main checkout", main.dir, main.dir}
	test := filepath.Join(w.dir, "us_001_test.go")

	for _, c := range []struct {
		what, agent, tool string
		input             map[string]any
	}{
		{"write the other story's frozen test", "", "Write", writing(test)},
		{"edit the other story's frozen test", "", "Edit", editing(test)},
		{"write the other worktree's .sdlc/state/active", "", "Write", writing(filepath.Join(w.dir, ".sdlc", "state", "active"))},
		{"write the other worktree's freeze", "", "Write", writing(filepath.Join(w.dir, ".sdlc", "state", "tests.lock"))},
		{"write the other worktree's configuration", "", "Write", writing(filepath.Join(w.dir, ".sdlc", "config.json"))},
	} {
		t.Run("AC-6 "+c.what, func(t *testing.T) {
			if got := askAt(t, binary, sessionSecond, here, c.agent, c.tool, c.input); !denied(got) {
				t.Errorf("the session held in the main checkout may %s", c.what)
			}
		})
	}
}

// ---------------------------------------------------------------- AC-7

func TestAC7TrunkMovingTripsNoGateBeforeTheCommitAndTheWorktreeIsWhatIsReviewed(t *testing.T) {
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
	cut := headOf(t, main.dir)
	w := startIn(t, main, main.as(sessionFirst))

	moves := 0
	trunkMoves := func() {
		t.Helper()
		moves++
		name := fmt.Sprintf("trunk_%d.go", moves)
		writeFile(t, filepath.Join(main.dir, name), "package main\n")
		commitPaths(t, main.dir, "a person's commit on trunk", name)
	}

	trunkMoves()
	w.analysed(t)
	trunkMoves()
	writeFile(t, filepath.Join(w.dir, "invoice_test.go"), "package main\n\n// AC-1\n")
	w.runWith(t, "# Test plan\n", "artifact", "write", "test_plan")
	w.run(t, "freeze")
	w.run(t, "gate", "tests_frozen", "pass", "--note", "1 criterion, 1 failing test")
	trunkMoves()
	w.runWith(t, "# Plan\n", "artifact", "write", "plan")
	w.run(t, "gate", "plan", "pass", "--note", "one step")
	w.reviewed(t, "design_review", "architect", "red-team", "security", "perf", "human-advocate")
	// Between the reviews and the gate trunk moves, and a person changes a
	// file in their own checkout: neither is the story's work.
	trunkMoves()
	writeFile(t, filepath.Join(main.dir, "go.mod"), readFile(t, filepath.Join(main.dir, "go.mod"))+"// a person's edit\n")
	w.run(t, "gate", "design_review", "pass", "--note", "architect approved")
	writeFile(t, filepath.Join(w.dir, "invoice.go"), "package main\n\nfunc Invoice() {}\n")
	w.run(t, "gate", "implementation", "pass", "--note", "frozen tests green")
	w.runWith(t, "# Verification\n", "artifact", "write", "verification")
	w.run(t, "gate", "verification", "pass", "--note", "all commands green")
	w.reviewed(t, "verifier_review", "verifier")
	trunkMoves()
	w.run(t, "gate", "verifier_review", "pass", "--note", "no gaming found")
	w.reviewed(t, "code_review", "code-reviewer", "security", "perf", "human-advocate")
	trunkMoves()
	w.run(t, "gate", "code_review", "pass", "--note", "reviewer approved")

	t.Run("AC-7 the record measures from the commit the branch was cut from", func(t *testing.T) {
		if tip := gitOut(t, main.dir, "rev-parse", "refs/heads/main"); tip == cut {
			t.Fatal("precondition: trunk did not move")
		}
		if got := gitOut(t, main.dir, "merge-base", "main", w.branch); got != cut {
			t.Errorf("%s and trunk meet at %s, want where the branch was cut, %s", w.branch, got, cut)
		}
		if r := recordOf(t, main, "US-001"); r.Base != cut || r.CommitBase != cut {
			t.Errorf("the record measures from %q, and its range from %q; want both where the branch was cut, %s",
				r.Base, r.CommitBase, cut)
		}
	})

	t.Run("AC-7 a change in the worktree is what makes a review stale", func(t *testing.T) {
		writeFile(t, filepath.Join(w.dir, "invoice.go"), "package main\n\nfunc Invoice() { panic(1) }\n")
		out, code := w.try(t, "gate", "commit", "pass", "--note", "on the story branch")
		if code == 0 || !strings.Contains(out, "SDLC-E0029") {
			t.Errorf("the commit gate exited %d on code nobody reviewed, want the reviews stale (SDLC-E0029):\n%s", code, out)
		}
	})

	// The reviewers read the story's change in its worktree: against trunk,
	// once trunk moves, it holds every commit trunk gained, reversed.
	t.Run("AC-7 no reviewer is told to read the diff against trunk", func(t *testing.T) {
		for _, agent := range []string{"verifier", "code-reviewer", "security", "perf", "human-advocate"} {
			body := strings.ToLower(readFile(t, filepath.Join("..", "..", "plugin", "agents", agent+".md")))
			for _, stale := range []string{"against the trunk branch", "against trunk"} {
				if strings.Contains(body, stale) {
					t.Errorf("plugin/agents/%s.md tells the reviewer to read the diff %s", agent, stale)
				}
			}
		}
	})
}

// ---------------------------------------------------------------- AC-8

func TestAC8ASecondSessionStartsASecondStoryInASecondWorktree(t *testing.T) {
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	first := startIn(t, main, main.as(sessionFirst))
	first.frozen(t, "us_001_test.go")
	kept := perCheckout(t, first.checkout)

	second := startIn(t, main, main.as(sessionSecond))
	if second.story != "US-002" || samePath(second.dir, first.dir) || second.branch == first.branch {
		t.Fatalf("the second session was given %s in %s on %s, want US-002 in a worktree of its own", second.story, second.dir, second.branch)
	}
	if after := perCheckout(t, first.checkout); !maps.Equal(kept, after) {
		t.Errorf("starting the second story changed the first's freeze, session or stop count:\n%s", difference(kept, after))
	}
	for _, name := range []string{"active", "session", "tests.lock"} {
		if exists(filepath.Join(main.dir, ".sdlc", "state", name)) {
			t.Errorf("a start from the main checkout wrote its .sdlc/state/%s", name)
		}
	}

	t.Run("AC-8 status in the main checkout ties each story to its worktree", func(t *testing.T) {
		doc := main.document(t, "status")
		if !namesHolder(doc, "US-001", first.dir, second.dir) || !namesHolder(doc, "US-002", second.dir, first.dir) {
			t.Errorf("status --json does not tie each story to its worktree:\n%v", doc)
		}
	})

	second.frozen(t, "us_002_test.go")

	t.Run("AC-8 the first story's gates pass as they would alone", func(t *testing.T) {
		first.runWith(t, "# Plan\n", "artifact", "write", "plan")
		first.run(t, "gate", "plan", "pass", "--note", "one step")
		first.reviewed(t, "design_review", "architect", "red-team", "security", "perf", "human-advocate")
		// The second story is worked to its commit meanwhile, which moves its
		// branch and not the first's.
		second.throughCodeReview(t)
		second.committed(t, "US-002")
		first.run(t, "gate", "design_review", "pass", "--note", "architect approved")
	})

	t.Run("AC-8 and fail as they would alone", func(t *testing.T) {
		test := filepath.Join(first.dir, "us_001_test.go")
		frozen := readFile(t, test)
		writeFile(t, test, frozen+"// quietly different\n")
		out, code := first.try(t, "gate", "implementation", "pass", "--note", "frozen tests green")
		if code == 0 || !strings.Contains(out, "SDLC-E0025") || !strings.Contains(out, "us_001_test.go") {
			t.Errorf("the first story's gate exited %d on its changed frozen test, want SDLC-E0025 naming it:\n%s", code, out)
		}
		// The second story is untouched by the first one's.
		second.retro(t)
		writeFile(t, test, frozen)
		first.run(t, "gate", "implementation", "pass", "--note", "frozen tests green")
	})

	// OQ-3: a story put down in its worktree stays held there, and a start
	// from the main checkout passes it over.
	t.Run("AC-8 a third session finds nothing left to start", func(t *testing.T) {
		first.as("").run(t, "stop")
		was := footprintOf(t, main)
		out, code := main.as(sessionOther).try(t, "start")
		refusedWith(t, out, code != 0, "SDLC-E0010")
		was.unchanged(t, footprintOf(t, main), "the refused start")
	})
}

// T6: two starts from the main checkout at once. However the race falls, no
// story and no worktree is given twice, and a start that loses leaves nothing
// of its own behind.
func TestAC8TwoStartsAtTheSameMomentNeverShareAStoryOrAWorktree(t *testing.T) {
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	sessions := []string{sessionFirst, sessionSecond}

	type result struct {
		stdout, stderr bytes.Buffer
		err            error
	}
	results := make([]*result, len(sessions))
	gun := make(chan struct{})
	var wg sync.WaitGroup
	for i, s := range sessions {
		results[i] = &result{}
		cmd := main.as(s).command(t, "", "start", "--json")
		cmd.Stdout, cmd.Stderr = &results[i].stdout, &results[i].stderr
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gun
			results[i].err = cmd.Run()
		}()
	}
	close(gun)
	wg.Wait()

	won := map[string]string{}
	for i, r := range results {
		if r.err != nil {
			refusedWith(t, r.stdout.String()+r.stderr.String(), true, "SDLC-E")
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal(r.stdout.Bytes(), &doc); err != nil {
			t.Fatalf("start printed something that is not JSON: %v\n%s", err, r.stdout.String())
		}
		id, _ := doc["story"].(string)
		if other, taken := won[id]; taken {
			t.Errorf("%s was given to %s and to %s", id, other, sessions[i])
		}
		won[id] = sessions[i]
		path := storyPath(main, id)
		if fieldHolding(doc, func(v string) bool { return samePath(v, path) }) == "" {
			t.Errorf("start in %s gave %s and names no worktree at %s: %v", sessions[i], id, path, doc)
		}
		if got := readFile(t, filepath.Join(path, ".sdlc", "state", "session")); got != sessions[i]+"\n" {
			t.Errorf("%s's worktree records %q as its session, want %s", id, got, sessions[i])
		}
	}
	if len(won) == 0 {
		t.Fatal("neither start began a story")
	}

	trees, branches := map[string]string{resolved(main.dir): "main"}, []string{"main"}
	var claims, nested []string
	for _, id := range slices.Sorted(maps.Keys(won)) {
		trees[resolved(storyPath(main, id))] = "story/" + id
		branches = append(branches, "story/"+id)
		claims = append(claims, id+".json")
		nested = append(nested, id)
	}
	if got := worktreesOf(t, main.dir); !maps.Equal(got, trees) {
		t.Errorf("git lists the checkouts %v, want %v", got, trees)
	}
	if got := slices.Sorted(maps.Keys(branchesOf(t, main.dir))); !slices.Equal(got, slices.Sorted(slices.Values(branches))) {
		t.Errorf("the branches are %v, want %v", got, branches)
	}
	if got := slices.Sorted(maps.Keys(files(t, filepath.Join(main.dir, ".git", "sdlc", "claims")))); !slices.Equal(got, claims) {
		t.Errorf("the claims are %v, want %v", got, claims)
	}
	if got := visibleEntries(t, filepath.Join(main.dir, ".sdlc", "worktrees")); !slices.Equal(got, nested) {
		t.Errorf(".sdlc/worktrees holds %v, want %v", got, nested)
	}
	for id, status := range statuses(t, main.backlog()) {
		if _, begun := won[id]; begun != (status == "in_progress") {
			t.Errorf("the backlog has %s %q, and it was begun: %v", id, status, begun)
		}
	}

	// The session that lost, asked again, begins the other story.
	for _, s := range sessions {
		if slices.Contains(slices.Collect(maps.Values(won)), s) {
			continue
		}
		if w := startIn(t, main, main.as(s)); won[w.story] != "" {
			t.Errorf("%s, starting again, was given %s, which %s holds", s, w.story, won[w.story])
		}
	}
}

// ---------------------------------------------------------------- AC-9

func TestAC9WithStoryWorktreesOffStartDoesWhatTheReleasedToolDoes(t *testing.T) {
	binary := build(t)
	off := func(t *testing.T) checkout {
		t.Helper()
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		worktreesOff(t, main.dir)
		commitEverything(t, main.dir, "story worktrees off")
		return main
	}
	madeNone := func(t *testing.T, main checkout) {
		t.Helper()
		if got := worktreesOf(t, main.dir); len(got) != 1 {
			t.Errorf("git lists the checkouts %v, want the main checkout alone", got)
		}
		for name := range branchesOf(t, main.dir) {
			if strings.HasPrefix(name, "story/") {
				t.Errorf("start made the branch %s", name)
			}
		}
		if got := visibleEntries(t, filepath.Join(main.dir, ".sdlc", "worktrees")); len(got) != 0 {
			t.Errorf("start made %v in .sdlc/worktrees", got)
		}
	}

	t.Run("AC-9 start holds the story in the checkout it runs in", func(t *testing.T) {
		main := off(t)
		doc := main.as(sessionFirst).document(t, "start")
		if doc["story"] != "US-001" || mentions(doc, filepath.Join(main.dir, ".sdlc", "worktrees")) {
			t.Errorf("start --json gave %v, want US-001 and no worktree", doc)
		}
		if got := readFile(t, filepath.Join(main.dir, ".sdlc", "state", "active")); got != "US-001\n" {
			t.Errorf("the main checkout's .sdlc/state/active is %q, want US-001", got)
		}
		madeNone(t, main)
	})

	for _, c := range []struct {
		name, code string
		prepare    func(t *testing.T, main checkout)
	}{
		{"AC-9 SDLC-E0038 on another branch", "SDLC-E0038", func(t *testing.T, main checkout) {
			git(t, main.dir, "switch", "--quiet", "-c", "mine")
		}},
		{"AC-9 SDLC-E0039 for work no story owns", "SDLC-E0039", func(t *testing.T, main checkout) {
			writeFile(t, filepath.Join(main.dir, "stray.go"), "package main\n")
		}},
		{"AC-9 SDLC-E0040 behind origin", "SDLC-E0040", func(t *testing.T, main checkout) {
			originMovesOn(t, withOrigin(t, main))
		}},
		// Run where start runs, the main checkout, where .git is a directory.
		{"AC-9 SDLC-E0041 when smoke fails where start runs", "SDLC-E0041", func(t *testing.T, main checkout) {
			setConfig(t, main.dir, "commands", "smoke", "test ! -d .git")
			commitEverything(t, main.dir, "a smoke check the main checkout fails")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			main := off(t)
			c.prepare(t, main)
			was := footprintOf(t, main)
			out, code := main.as(sessionFirst).try(t, "start")
			refusedWith(t, out, code != 0, c.code)
			was.unchanged(t, footprintOf(t, main), "the refused start")
			madeNone(t, main)
		})
	}
}

func TestAC9WhereTheKeyIsAbsentStoryWorktreesAreOn(t *testing.T) {
	binary := build(t)

	t.Run("AC-9 git.worktrees absent", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		if strings.Contains(readFile(t, filepath.Join(main.dir, ".sdlc", "config.json")), `"worktrees"`) {
			t.Fatal("precondition: the configuration names git.worktrees")
		}
		startIn(t, main, main.as(sessionFirst))
	})

	t.Run("AC-9 git.worktrees true", func(t *testing.T) {
		main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
		setConfig(t, main.dir, "git", "worktrees", true)
		commitEverything(t, main.dir, "story worktrees on, by name")
		startIn(t, main, main.as(sessionFirst))
	})

	// D1: start makes the worktree by default, so the configuration a new
	// project gets has it on.
	t.Run("AC-9 the configuration sdlc init writes", func(t *testing.T) {
		root := tempDir(t)
		git(t, root, "init", "--quiet", "--initial-branch=main")
		writeFile(t, filepath.Join(root, "go.mod"), "module example.com/demo\n\ngo 1.26\n")
		runTool(t, binary, root, "init")
		commitEverything(t, root, "sdlc init, as it writes it")
		main := checkout{binary: binary, dir: root}
		startIn(t, main, main.as(sessionFirst))
	})

	t.Run("AC-9 doctor knows the key", func(t *testing.T) {
		for _, value := range []bool{false, true} {
			main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
			setConfig(t, main.dir, "git", "worktrees", value)
			out, _ := main.try(t, "doctor", "--json")
			var doc struct {
				Checks []check `json:"checks"`
			}
			if err := json.Unmarshal([]byte(out), &doc); err != nil {
				t.Fatalf("doctor --json: %v\n%s", err, out)
			}
			if c := checkNamed(doc.Checks, "configuration"); strings.Contains(c.Detail, "worktrees") {
				t.Errorf("doctor, with git.worktrees %v: %s", value, c.Detail)
			}
		}
	})
}

// ---------------------------------------------------------------- AC-10

func TestAC10TheRunbookTakesTheWorktreeFromStartAndWorksThere(t *testing.T) {
	binary := build(t)
	main := storyRepository(t, binary, spec{id: "US-001", priority: 1})
	w := startIn(t, main, main.as(sessionFirst))
	runbook := readFile(t, filepath.Join("..", "..", "plugin", "skills", "next", "SKILL.md"))
	paragraphs := strings.Split(runbook, "\n\n")

	t.Run("AC-10 it reads the worktree from the field start prints it in", func(t *testing.T) {
		fields := []string{w.pathField}
		if _, leaf, nested := strings.Cut(w.pathField, "."); nested {
			fields = append(fields, leaf)
		}
		if !slices.ContainsFunc(fields, func(f string) bool { return strings.Contains(runbook, "`"+f+"`") }) {
			t.Errorf("the runbook never names %v, where start --json prints the worktree's path", fields)
		}
	})

	t.Run("AC-10 it sends every command there", func(t *testing.T) {
		if !regexp.MustCompile(`\bcd\s+(--\s+)?["'<$]`).MatchString(runbook) {
			t.Error("the runbook never changes directory into the worktree")
		}
	})

	t.Run("AC-10 every agent's brief carries the worktree", func(t *testing.T) {
		if !slices.ContainsFunc(paragraphs, func(p string) bool {
			return strings.Contains(p, "brief") && strings.Contains(p, "worktree")
		}) {
			t.Error("no paragraph of the runbook about an agent's brief mentions the worktree")
		}
	})

	t.Run("AC-10 a person's own commands are sent there too", func(t *testing.T) {
		for _, p := range paragraphs {
			if strings.Contains(p, "sdlc unfreeze") && !strings.Contains(p, "worktree") {
				t.Errorf("the runbook tells a person to run sdlc unfreeze without saying in which checkout:\n%s", p)
			}
		}
	})
}

func TestAC10TheDocsDescribeTheSwitchTheWorktreeAndSDLCE0053(t *testing.T) {
	doc := func(name string) string {
		t.Helper()
		return readFile(t, filepath.Join("..", "..", "docs", name))
	}
	// What the docs said before this story, which is no longer so.
	const stale = "until `sdlc start` makes a worktree itself"

	t.Run("AC-10 configuration.md describes git.worktrees", func(t *testing.T) {
		keys := section(doc("configuration.md"), "## `git`")
		for _, want := range []string{"`worktrees`", ".sdlc/worktrees/", "story/"} {
			if !strings.Contains(keys, want) {
				t.Errorf("the git section of docs/configuration.md does not mention %s:\n%s", want, keys)
			}
		}
	})

	t.Run("AC-10 the-loop.md describes what start does", func(t *testing.T) {
		text := doc("the-loop.md")
		for _, want := range []string{"git.worktrees", ".sdlc/worktrees/", "story/"} {
			if !strings.Contains(text, want) {
				t.Errorf("docs/the-loop.md does not mention %s", want)
			}
		}
		if strings.Contains(strings.ToLower(text), stale) {
			t.Errorf("docs/the-loop.md still says %q", stale)
		}
	})

	t.Run("AC-10 troubleshooting.md describes SDLC-E0053 and when the others apply", func(t *testing.T) {
		text := doc("troubleshooting.md")
		if s := section(text, "### SDLC-E0053"); !strings.Contains(s, "worktree") {
			t.Errorf("docs/troubleshooting.md has no SDLC-E0053 section about the worktree:\n%s", s)
		}
		for _, code := range []string{"SDLC-E0038", "SDLC-E0039"} {
			if s := section(text, "### "+code); !strings.Contains(s, "git.worktrees") {
				t.Errorf("the %s section does not say it applies with git.worktrees off:\n%s", code, s)
			}
		}
		if s := section(text, "### SDLC-E0041"); !strings.Contains(s, "worktree") {
			t.Errorf("the SDLC-E0041 section does not say where commands.smoke runs:\n%s", s)
		}
		if strings.Contains(strings.ToLower(text), stale) {
			t.Errorf("docs/troubleshooting.md still says %q", stale)
		}
	})
}

// section is the part of a Markdown document under heading, down to the next
// heading of the same level or above, and "" where there is no such heading.
// A line in a fenced block is never a heading.
func section(text, heading string) string {
	level := len(heading) - len(strings.TrimLeft(heading, "#"))
	lines := strings.Split(text, "\n")
	start := slices.IndexFunc(lines, func(l string) bool { return l == heading || strings.HasPrefix(l, heading+" ") })
	if start < 0 {
		return ""
	}
	fenced := false
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, "#"))
		if !fenced && n > 0 && n <= level && strings.HasPrefix(line[n:], " ") {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

// ---------------------------------------------------------------- carried from US-029.1

// moveCheckout moves a checkout to another directory, as a person does with mv
// or a file manager, and returns it there.
func moveCheckout(t *testing.T, c checkout) checkout {
	t.Helper()
	to := filepath.Join(tempDir(t), "moved")
	if err := os.Rename(c.dir, to); err != nil {
		t.Fatal(err)
	}
	c.dir = to
	return c
}

// sentences splits a line where one sentence of a message ends and the next
// begins.
var sentences = regexp.MustCompile(`\. |; | — | -- `)

// advisesDiscarding reports whether some sentence of text tells its reader to
// discard work, rather than not to.
func advisesDiscarding(text string) bool {
	for _, line := range strings.Split(strings.ToLower(text), "\n") {
		for _, s := range sentences.Split(line, -1) {
			if strings.Contains(s, "discard") &&
				!strings.Contains(s, "not ") && !strings.Contains(s, "n't ") && !strings.Contains(s, "never ") {
				return true
			}
		}
	}
	return false
}

// D7(6): a single checkout, story worktrees off, moved while its story runs.
// Its claim names where it was, and it goes on holding the story.
func TestACheckoutMovedWhileItsStoryRunsKeepsTheStory(t *testing.T) {
	binary := build(t)
	main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
	m := main.as(sessionMain)
	m.run(t, "start")
	m.frozen(t, "us_001_test.go")
	moved := moveCheckout(t, m)

	stdout, stderr, code := moved.outcome(t, "", "start", "--json")
	var doc map[string]any
	if code != 0 || json.Unmarshal([]byte(stdout), &doc) != nil || doc["story"] != "US-001" || doc["resume"] != true {
		t.Fatalf("start in the moved checkout exited %d, want US-001 resumed:\n%s%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, "US-001") {
		t.Errorf("start in the moved checkout says something about its own story:\n%s", stderr)
	}
	// The freeze moved with the checkout, and the gates after it are held to it.
	moved.runWith(t, "# Plan\n", "artifact", "write", "plan")
	moved.run(t, "gate", "plan", "pass", "--note", "one step")
	claim := readFile(t, filepath.Join(moved.dir, ".git", "sdlc", "claims", "US-001.json"))
	if !recorded(claim, moved.dir) || recorded(claim, main.dir) {
		t.Errorf("the claim on US-001 does not name where the checkout is now, %s:\n%s", moved.dir, claim)
	}
}

// D7(2) and D7(3): a single checkout moved while its story was put down. Its
// claim names where it was, which is no checkout of the repository now, so a
// plain start passes the story over and says so, in words that fit a checkout
// that was moved and that keep the story's own uncommitted work.
func TestACheckoutMovedWhileItsStoryWasStoppedSaysSoAndKeepsTheStorysWork(t *testing.T) {
	binary := build(t)

	t.Run("put down with its work uncommitted", func(t *testing.T) {
		main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
		m := main.as(sessionMain)
		m.run(t, "start")
		m.frozen(t, "us_001_test.go")
		const work = "package main\n\n// US-001's work, not committed yet\n"
		writeFile(t, filepath.Join(main.dir, "invoice.go"), work)
		main.as("").run(t, "stop")
		moved := moveCheckout(t, m)

		_, stderr, code := moved.outcome(t, "", "start")
		for _, want := range []string{"US-001", "moved", "kept", "assistant"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("start in the moved checkout does not say %q about the story it passes over:\n%s", want, stderr)
			}
		}
		// Story worktrees are off, so the next story would begin here, on top
		// of the first one's work, and start refuses it.
		if code == 0 || !strings.Contains(stderr, "SDLC-E0039") {
			t.Errorf("start exited %d, want the next story refused over the uncommitted work, SDLC-E0039:\n%s", code, stderr)
		}
		if advisesDiscarding(stderr) {
			t.Errorf("start tells the person to discard work that is the stopped story's own:\n%s", stderr)
		}
		if got := readFile(t, filepath.Join(moved.dir, "invoice.go")); got != work {
			t.Errorf("the stopped story's work is now %q", got)
		}
		if got := statuses(t, moved.backlog()); got["US-001"] != "in_progress" || got["US-002"] != "ready" {
			t.Errorf("the backlog is %v, want US-001 in_progress and US-002 ready", got)
		}

		t.Run("released there, nothing says its freeze or its work stayed behind", func(t *testing.T) {
			released := moved.as("").run(t, "release", "US-001", "--reason", "this checkout was moved")
			for _, stale := range []string{"does not move", "unfreeze"} {
				if strings.Contains(released, stale) {
					t.Errorf("release in the checkout that moved says %q, and the freeze moved with it:\n%s", stale, released)
				}
			}
			_, stderr, code := moved.outcome(t, "", "start", "US-001")
			if code != 0 {
				t.Fatalf("start US-001 after the release exited %d:\n%s", code, stderr)
			}
			for _, stale := range []string{"stayed there", "is not in this one"} {
				if strings.Contains(stderr, stale) {
					t.Errorf("start after the release says %q, and the work is here:\n%s", stale, stderr)
				}
			}
			moved.runWith(t, "# Plan\n", "artifact", "write", "plan")
			moved.run(t, "gate", "plan", "pass", "--note", "one step")
		})
	})

	t.Run("waiting on a person, the note names sdlc approve", func(t *testing.T) {
		main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
		m := main.as(sessionMain)
		m.run(t, "start")
		m.run(t, "escalate", "spec_unclear", "--message", "which greeting?")
		moved := moveCheckout(t, m)

		_, stderr, _ := moved.outcome(t, "", "start")
		if !strings.Contains(stderr, "moved") || !strings.Contains(stderr, "sdlc approve US-001") {
			t.Errorf("the note on US-001, which waits on a person, does not say the checkout may have moved and name sdlc approve US-001:\n%s", stderr)
		}
	})
}

// D7(1) and OQ-8: a fix a person pastes into a terminal quotes its
// placeholder as they would type it.
func TestTheFixesThatQuoteAReasonOrANotePrintNoBackslashes(t *testing.T) {
	binary := build(t)

	unquoted := func(t *testing.T, out string, failed bool, code, flag string) {
		t.Helper()
		refusedWith(t, out, failed, code)
		fix := fixLine(out)
		if strings.Contains(fix, `\`) {
			t.Errorf("the fix of %s prints a backslash: %s", code, fix)
		}
		if !strings.Contains(fix, flag+` "..."`) {
			t.Errorf("the fix of %s does not quote %s \"...\" as a person types it: %s", code, flag, fix)
		}
	}

	t.Run("SDLC-E0050", func(t *testing.T) {
		main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
		wt := worktree(t, main, "wt").as(sessionWorktree)
		main.as(sessionMain).run(t, "start")
		out, code := wt.try(t, "start", "US-001")
		unquoted(t, out, code != 0, "SDLC-E0050", "--reason")
	})

	t.Run("SDLC-E0051", func(t *testing.T) {
		main := repository(t, binary, spec{id: "US-001", priority: 1}, spec{id: "US-002", priority: 2})
		gone := worktree(t, main, "gone").as(sessionWorktree)
		gone.run(t, "start", "US-001")
		if err := os.RemoveAll(gone.dir); err != nil {
			t.Fatal(err)
		}
		out, code := main.as(sessionMain).try(t, "start", "US-001")
		unquoted(t, out, code != 0, "SDLC-E0051", "--reason")
	})

	t.Run("SDLC-E0049", func(t *testing.T) {
		skipWithoutTheLauncher(t)
		root := project(t, binary)
		runTool(t, binary, root, "start")
		walk(t, binary, root)
		ourCommit(t, binary, root, "US-001")
		first := gitOut(t, root, "rev-list", "--max-parents=0", "HEAD")
		out, err := tool(t, binary, root, "gate", "commit", "pass", "--note", "the work is in "+first[:12])
		unquoted(t, out, err != nil, "SDLC-E0049", "--note")
	})
}
