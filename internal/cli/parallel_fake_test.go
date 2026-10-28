package cli

// The fake `claude` US-058's tests start sessions with, and what reads back
// what it saw.
//
// No real claude and no network: a `claude` script first on PATH hands over to
// this test binary again (TestMain sees PLFAKE_CHILD and runs plChildMain),
// which does what the plan says for the story named in its prompt. That is
// what a session does with /sdlc:next <ID>: it runs `sdlc start <ID>` from the
// directory it was started in, under a session id of its own, and the loop's
// own code answers. So "each session holds one story in its own worktree" is
// the loop's real answer, not the test's.
//
// The script is the same for every test, and TestMain puts it on PATH once
// (plShareFakeClaude): the launcher starts its sessions from the process's
// environment, and a test cannot change that without t.Setenv, which Go
// refuses in a parallel test. What differs from one test to the next, the plan
// and the log of what ran, sits beside the main checkout the sessions are
// started in, where a session finds it from its own directory.
//
// Helpers carry a pl prefix, for parallel.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/store"
)

// plChildEnv is set by the fake claude script, and by nothing else, to have
// the test binary behave as the session.
const plChildEnv = "PLFAKE_CHILD"

// plBehaviour is what the fake session does for one story: the steps, in
// order, and the exit status it ends with.
//
//	"start"    sdlc start <ID> --json from the directory it was started in
//	"escalate" sdlc escalate in the story's worktree, with Message
type plBehaviour struct {
	Actions []string `json:"actions"`
	Exit    int      `json:"exit"`
	Message string   `json:"message"`
}

// plCall is what one fake session saw.
type plCall struct {
	Story string            `json:"story"`
	Args  []string          `json:"args"`
	Cwd   string            `json:"cwd"`
	Env   map[string]string `json:"env"`
	// Failed is what an sdlc step the session ran printed, when it was refused.
	Failed string `json:"failed"`
}

// plSessionOf is the Claude Code session id the fake session works its story
// in: its own, so that no session holds two stories.
func plSessionOf(id string) string { return "plfake-" + id }

// plSeen is the environment a session reports on, because a launcher must not
// add to it, hide from it, or hand a parent session down.
var plSeen = []string{store.SessionEnv, "SDLC_ENFORCE", "SDLC_BIN", "CLAUDE_PROJECT_DIR", "PLFAKE_SENTINEL"}

// plChildMain is the fake session. It returns the exit status to end with.
func plChildMain() int {
	args := os.Args[1:]
	call := plCall{Args: args, Env: map[string]string{}}
	for _, a := range args {
		if id, ok := strings.CutPrefix(a, "/sdlc:next "); ok {
			call.Story = id
		}
	}
	call.Cwd, _ = os.Getwd()
	dir := plDirOf(call.Cwd)
	for _, name := range plSeen {
		if v, ok := os.LookupEnv(name); ok {
			call.Env[name] = v
		}
	}
	plAppend(filepath.Join(dir, "events.log"), "start "+call.Story)
	defer plAppend(filepath.Join(dir, "events.log"), "end "+call.Story)

	var plan map[string]plBehaviour
	if raw, err := os.ReadFile(filepath.Join(dir, "plan.json")); err == nil {
		_ = json.Unmarshal(raw, &plan)
	}
	b := plan[call.Story]
	getenv := func(name string) string {
		if name == store.SessionEnv {
			return plSessionOf(call.Story)
		}
		return os.Getenv(name)
	}
	step := func(at string, args ...string) {
		var out, errb strings.Builder
		if code := ExecuteIn(at, getenv, args, strings.NewReader(""), &out, &errb); code != 0 {
			call.Failed += fmt.Sprintf("sdlc %s exited %d: %s%s\n", strings.Join(args, " "), code, out.String(), errb.String())
		}
	}
	for _, action := range b.Actions {
		switch action {
		case "start":
			step(call.Cwd, "start", call.Story, "--json")
		case "escalate":
			message := b.Message
			if message == "" {
				message = "needs a person"
			}
			step(filepath.Join(call.Cwd, ".sdlc", "worktrees", call.Story),
				"escalate", "pre_commit_approval", "--message", message)
		}
	}
	raw, err := json.Marshal(call)
	if err != nil {
		panic(err)
	}
	name := fmt.Sprintf("%s-%d.json", strings.ReplaceAll(call.Story, "/", "_"), os.Getpid())
	_ = os.MkdirAll(filepath.Join(dir, "calls"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "calls", name), raw, 0o644)
	return b.Exit
}

// plAppend adds one line to a file, in one write, so that sessions running
// together cannot tear each other's lines.
func plAppend(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line + "\n")
}

// plDirOf is where the fake claude keeps a test's plan and what it logs, for
// the main checkout the sessions are started in: beside it, in the directory
// the test's own t.TempDir made, so that it goes with the test and no other
// test's sessions can read it. A session finds it from its working directory,
// which the launcher sets to the main checkout; both ends resolve symbolic
// links, because a temporary directory is one on macOS.
func plDirOf(main string) string {
	if resolved, err := filepath.EvalSymlinks(main); err == nil {
		main = resolved
	}
	return main + ".plfake"
}

// plShareFakeClaude puts the fake claude on PATH for the whole run, and
// returns what removes it. Every test's sessions are this one script, which
// hands over to the test binary as plChildEnv says.
//
// Windows has none: the fake is a POSIX shell script, and plInstall skips
// there.
func plShareFakeClaude() (cleanup func(), err error) {
	if runtime.GOOS == "windows" {
		return func() {}, nil
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	bin, err := os.MkdirTemp("", "sdlc-plfake-")
	if err != nil {
		return nil, err
	}
	cleanup = func() { _ = os.RemoveAll(bin) }
	script := "#!/bin/sh\n" + plChildEnv + "=1 exec \"$PLFAKE_BIN\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		cleanup()
		return nil, err
	}
	for name, value := range map[string]string{
		"PATH":            bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"PLFAKE_BIN":      self,
		"PLFAKE_SENTINEL": "carried",
	} {
		if err := os.Setenv(name, value); err != nil {
			cleanup()
			return nil, err
		}
	}
	return cleanup, nil
}

// plRig is the fake claude, set up for one test.
type plRig struct {
	t   *testing.T
	dir string
}

// plInstall gives the fake claude the plan for a test whose sessions are
// started in main. With shimGit it also puts a git that logs what it was asked
// before it runs the real one first on PATH, which is the process's: a test
// that asks for it is not parallel.
func plInstall(t *testing.T, main string, plan map[string]plBehaviour, shimGit bool) *plRig {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake claude is a POSIX shell script; the launcher's Windows path runs under Git Bash and is not run here")
	}
	dir := plDirOf(main)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plan.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if shimGit {
		gitPath, err := exec.LookPath("git")
		if err != nil {
			t.Fatal(err)
		}
		bin := t.TempDir()
		shim := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$PLFAKE_DIR/git.log\"\nexec '" + gitPath + "' \"$@\"\n"
		if err := os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		t.Setenv("PLFAKE_DIR", dir)
	}
	return &plRig{t: t, dir: dir}
}

// calls is every session the fake started, by story.
func (r *plRig) calls() map[string]plCall {
	r.t.Helper()
	files, _ := filepath.Glob(filepath.Join(r.dir, "calls", "*.json"))
	out := map[string]plCall{}
	for _, f := range files {
		var c plCall
		raw, err := os.ReadFile(f)
		if err != nil {
			r.t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			r.t.Fatalf("%s: %v", f, err)
		}
		if _, twice := out[c.Story]; twice {
			r.t.Errorf("two sessions were started for %q", c.Story)
		}
		out[c.Story] = c
	}
	return out
}

// started is the stories a session was started for, as a sorted list.
func (r *plRig) started() []string {
	var ids []string
	for id := range r.calls() {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// peak is the most sessions the log shows running at once.
func (r *plRig) peak() int {
	raw, _ := os.ReadFile(filepath.Join(r.dir, "events.log"))
	running, peak := 0, 0
	for line := range strings.Lines(string(raw)) {
		switch {
		case strings.HasPrefix(line, "start "):
			running++
		case strings.HasPrefix(line, "end "):
			running--
		}
		peak = max(peak, running)
	}
	return peak
}

// gitCommands is every git command the shim logged.
func (r *plRig) gitCommands() []string {
	raw, _ := os.ReadFile(filepath.Join(r.dir, "git.log"))
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}
