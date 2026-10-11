// US-067: the sandbox, end to end.
//
// A sandbox is a throwaway directory holding a new git repository with sdlc
// initialised and one ready story, the sdlc binary built from this checkout,
// and room for everything the two write. It is made by one program,
// internal/tools/sandbox, which `./task sandbox` runs; the tests here build
// that program and run it as a person would, so the documented command and the
// tested path are the same one.
//
// The word is used elsewhere for what sdlc is not: docs/enforcement.md says
// "this is a discipline tool, not a sandbox". Nothing here confines anything.
//
// What the helpers below bind, and the implementation has to provide:
//
//	sandbox [-C <checkout>] create   exit 0, nothing on stderr, and on stdout
//	                                 lines of a word and a path: `sandbox` the
//	                                 directory, `repository` the git repository
//	                                 in it, `sdlc` the binary built from the
//	                                 checkout in it
//	sandbox [-C <checkout>] remove <dir>   exit 0, and <dir> is gone
//
// The directory is made directly under the system temp directory, as the
// program's own environment names it, and is called sdlc-sandbox-<something>.
//
// The person-only steps (approve, unfreeze, ack, release, and a stop before the
// gates have passed) are no word of that program. They are run here, by the
// test process, in a sandbox this process made a moment before, and with no
// Claude Code session in the command's environment; the session under test is
// an id the tests hand to the binary and to the hook, and every call it makes
// is put to the hook first.

package e2e

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// sandboxSession is the session under test. sandboxOther is a second one, which
// holds no story.
const (
	sandboxSession = "sandbox-session-under-test"
	sandboxOther   = "sandbox-another-session"
)

// sandboxPrefix is what the name of every sandbox directory starts with.
const sandboxPrefix = "sdlc-sandbox-"

// sandbox is one sandbox the program made, and the directories its own run was
// given for a temp directory and a home.
type sandbox struct {
	// program is the sandbox program, built from this checkout.
	program string
	// dir, repo and sdlc are what create printed.
	dir, repo, sdlc string
	// temp is the temp directory the program ran with, which holds dir. home is
	// the home directory it ran with, which holds nothing.
	temp, home string
	// printed is create's stdout, as it was.
	printed string
}

// moduleRoot is this checkout, which the tests run two directories below.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// sandboxProgram builds internal/tools/sandbox, the program `./task sandbox`
// runs with `go run`. It is built rather than run with `go run` so that its own
// exit code and streams are what a test sees.
func sandboxProgram(t *testing.T) string {
	t.Helper()
	name := "sandbox"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", out, "./internal/tools/sandbox")
	cmd.Dir = moduleRoot(t)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the sandbox program, internal/tools/sandbox, does not build: %v\n%s", err, combined)
	}
	return out
}

// goEnv is one of go's own settings, as the test process has it. The build
// cache and the module cache are the two places the sandbox program is let
// write outside its directory, because a build with neither starts from
// nothing and fetches from the network; they are named to it outright, since
// its home is not the developer's.
func goEnv(t *testing.T, name string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}

// platformEnv is what a child needs of the machine to run at all on Windows,
// and nothing anywhere else.
func platformEnv() []string {
	var env []string
	for _, name := range []string{"SystemRoot", "SystemDrive", "ComSpec", "PATHEXT", "WINDIR"} {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// homeEnv points every variable a home, a cache or a temp directory is found by
// at home and temp, on all three platforms.
func homeEnv(home, temp string) []string {
	return []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"APPDATA=" + filepath.Join(home, "AppData", "Roaming"),
		"TMPDIR=" + temp,
		"TMP=" + temp,
		"TEMP=" + temp,
	}
}

// programEnv is the environment the sandbox program is run in: the machine's
// PATH, go's caches where they are, GOPROXY off so that a build that wanted the
// network fails instead, and a home and a temp directory of the test's own.
// Nothing else of the test's environment reaches it.
func programEnv(t *testing.T, home, temp string, extra ...string) []string {
	t.Helper()
	env := append(platformEnv(),
		"PATH="+os.Getenv("PATH"),
		"GOCACHE="+goEnv(t, "GOCACHE"),
		"GOMODCACHE="+goEnv(t, "GOMODCACHE"),
		"GOPATH="+goEnv(t, "GOPATH"),
		"GOPROXY=off",
	)
	env = append(env, homeEnv(home, temp)...)
	return append(env, extra...)
}

// runProgram runs the sandbox program in this checkout, as `./task` does, and
// returns each stream and the exit code.
func runProgram(t *testing.T, program string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	return runProgramIn(t, moduleRoot(t), program, env, args...)
}

func runProgramIn(t *testing.T, dir, program string, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), program, args...)
	cmd.Dir = dir
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("sandbox %s could not be run: %v", strings.Join(args, " "), err)
	}
	return out.String(), errOut.String(), code
}

// printedPaths reads what create printed: a word, white space, a path. A word
// printed twice is an output nobody can read a path out of.
func printedPaths(t *testing.T, stdout string) map[string]string {
	t.Helper()
	paths := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(stdout, "\n"), "\n") {
		word, rest, ok := strings.Cut(strings.TrimRight(line, "\r"), " ")
		if !ok {
			continue
		}
		if _, twice := paths[word]; twice {
			t.Fatalf("create printed %q twice:\n%s", word, stdout)
		}
		paths[word] = strings.TrimSpace(rest)
	}
	return paths
}

// scratch is a directory of the test's own with links resolved, so that a path
// a program reports by its real name compares equal to one built here.
func scratch(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if actual, err := filepath.EvalSymlinks(dir); err == nil {
		dir = actual
	}
	return dir
}

// newSandbox builds the program and has it make a sandbox, under a temp
// directory and with a home that are the test's own.
func newSandbox(t *testing.T) sandbox {
	t.Helper()
	return newSandboxWith(t, sandboxProgram(t))
}

func newSandboxWith(t *testing.T, program string, extra ...string) sandbox {
	t.Helper()
	sb := sandbox{program: program, temp: scratch(t), home: scratch(t)}
	stdout, stderr, code := runProgram(t, program, programEnv(t, sb.home, sb.temp, extra...), "create")
	if code != 0 || stderr != "" {
		t.Fatalf("sandbox create exited %d:\nstdout %q\nstderr %q", code, stdout, stderr)
	}
	sb.printed = stdout
	paths := printedPaths(t, stdout)
	sb.dir, sb.repo, sb.sdlc = paths["sandbox"], paths["repository"], paths["sdlc"]
	for word, path := range map[string]string{"sandbox": sb.dir, "repository": sb.repo, "sdlc": sb.sdlc} {
		if path == "" || !filepath.IsAbs(path) {
			t.Fatalf("create printed no absolute path after %q:\n%s", word, stdout)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("create printed %s for %q, and it is not there: %v", path, word, err)
		}
	}
	return sb
}

// removed has the program remove the sandbox, which has to work.
func (sb sandbox) removed(t *testing.T) {
	t.Helper()
	stdout, stderr, code := runProgram(t, sb.program, programEnv(t, sb.home, sb.temp), "remove", sb.dir)
	if code != 0 || stderr != "" {
		t.Fatalf("sandbox remove %s exited %d:\nstdout %q\nstderr %q", sb.dir, code, stdout, stderr)
	}
}

// own is a directory in the sandbox for what its commands write outside the
// repository: made here where the program did not make it already.
func (sb sandbox) own(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(sb.dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// harnessGit is the one configuration of git's the sandbox's commands read
// besides the repository's own: no maintenance in the background, whose lock
// came and went under a listing (internal/testenv has the story), and somebody
// to commit as, since the home these commands run with has no identity in it.
const harnessGit = "[maintenance]\n\tauto = false\n[gc]\n\tauto = 0\n[user]\n\tname = Sandbox\n\temail = sandbox@example.com\n"

// env is the environment of every command run in the sandbox: its own sdlc
// first on PATH and named by SDLC_BIN, so that `sdlc doctor` reports that one
// and not one the developer installed; a home, a cache and a temp directory
// inside the sandbox, which is where the lock every writing command takes and
// git's scratch directories then go; no configuration of git's but the
// repository's own and harnessGit; and the session given, or none, which is a
// person at a terminal. SDLC_ENFORCE is never set: a run that passed with
// enforcement off would have shown nothing.
func (sb sandbox) env(t *testing.T, session string) []string {
	t.Helper()
	home := sb.own(t, "home")
	config := filepath.Join(home, "harness.gitconfig")
	if err := os.WriteFile(config, []byte(harnessGit), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(platformEnv(),
		"PATH="+filepath.Dir(sb.sdlc)+string(os.PathListSeparator)+os.Getenv("PATH"),
		"SDLC_BIN="+sb.sdlc,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+config,
		"GIT_TERMINAL_PROMPT=0",
	)
	env = append(env, homeEnv(home, sb.own(t, "tmp"))...)
	if session != "" {
		env = append(env, "CLAUDE_CODE_SESSION_ID="+session)
	}
	return env
}

// outcome runs the sandbox's sdlc in dir, in a session or as a person with "",
// and returns each stream and the exit code. Standard input is closed unless a
// document is given, so a command that waited for one ends instead.
func (sb sandbox) outcome(t *testing.T, session, dir, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), sb.sdlc, args...)
	cmd.Dir = dir
	cmd.Env = sb.env(t, session)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
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
	if code == 0 {
		noteRun(args...)
	}
	return out.String(), errOut.String(), code
}

// git runs git in the sandbox with the sandbox's environment, and returns what
// it printed and whether it worked.
func (sb sandbox) git(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Env = sb.env(t, "")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (sb sandbox) mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := sb.git(t, dir, args...)
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

// tree is where the story is worked: the worktree `sdlc start` makes for it,
// since a project `sdlc init` set up has story worktrees on.
func (sb sandbox) tree() string {
	return filepath.Join(sb.repo, ".sdlc", "worktrees", "US-001")
}

// hook hands the sandbox's sdlc one hook event, with the payload on standard
// input as Claude Code sends it, and returns each stream and the exit code.
//
// On Linux and macOS it goes through plugin/bin/sdlc-hook, the launcher
// hooks.json names. On Windows that script does not run, so the binary is given
// the event directly, which is what the launcher does once it has found it;
// internal/plugin holds sdlc-hook.cmd to the same answers. Nothing here skips
// on Windows.
func (sb sandbox) hook(t *testing.T, event string, payload map[string]any) (stdout, stderr string, code int) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return sb.hookRaw(t, event, string(raw), runtime.GOOS != "windows")
}

func (sb sandbox) hookRaw(t *testing.T, event, payload string, throughLauncher bool) (stdout, stderr string, code int) {
	t.Helper()
	name, args := sb.sdlc, []string{"hook", event}
	if throughLauncher {
		name, args = filepath.Join(moduleRoot(t), "plugin", "bin", "sdlc-hook"), []string{event}
	}
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Dir = sb.repo
	cmd.Env = append(sb.env(t, ""), "CLAUDE_PROJECT_DIR="+sb.repo)
	cmd.Stdin = strings.NewReader(payload)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	runErr := cmd.Run()
	var exit *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exit):
		code = exit.ExitCode()
	default:
		t.Fatalf("the hook could not be run: %v", runErr)
	}
	if code == 0 && out.Len() > 0 {
		noteHook()
	}
	return out.String(), errOut.String(), code
}

// toolCall is the JSON Claude Code sends before a tool call of the session
// given, made from dir.
func toolCall(session, dir, agent, tool string, input map[string]any) map[string]any {
	call := map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      session,
		"cwd":             dir,
		"tool_name":       tool,
		"tool_input":      input,
	}
	if agent != "" {
		call["agent_type"] = "sdlc:" + agent
	}
	return call
}

// carryOn is the whole of what the hook prints for a call it has nothing to say
// about: one line, and no field but continue.
const carryOn = "{\"continue\":true}\n"

// refusal is what the hook prints for a tool call it refuses, read strictly: a
// field this does not name, or anything after the one line, fails the test.
// The struct parse reads everywhere else in this package takes any JSON at all
// and reads a missing `continue` as false, which checks nothing.
type refusal struct {
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

// oneLine holds a hook's stdout to one line of JSON and a newline, and decodes
// it into v with every field accounted for.
func oneLine(t *testing.T, stdout string, v any) {
	t.Helper()
	if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("the hook printed something other than one line: %q", stdout)
	}
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("the hook's stdout is not the JSON it is documented to print: %v\n%q", err, stdout)
	}
}

// allows puts a tool call of the session under test to the hook, which has to
// let it through and say nothing else.
func (sb sandbox) allows(t *testing.T, dir, agent, tool string, input map[string]any) {
	t.Helper()
	stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, dir, agent, tool, input))
	if stdout != carryOn || code != 0 {
		t.Fatalf("the hook did not let %s through: exit %d\nstdout %q\nstderr %q",
			describeInput(tool, input), code, stdout, stderr)
	}
}

// bashTool is the tool name Claude Code gives a shell command.
const bashTool = "Bash"

// refuses puts a Bash call of a session to the hook, which has to refuse it
// under the rule given, with a route.
func (sb sandbox) refuses(t *testing.T, session, dir string, input map[string]any, rule string) {
	t.Helper()
	stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(session, dir, "", bashTool, input))
	if code != 0 {
		t.Fatalf("the hook exited %d for %s, so its reply is never read:\n%s", code, describeInput(bashTool, input), stderr)
	}
	if stdout == carryOn {
		t.Fatalf("the hook let session %s run %s", session, describeInput(bashTool, input))
	}
	var r refusal
	oneLine(t, stdout, &r)
	out := r.HookSpecificOutput
	if out.HookEventName != "PreToolUse" || out.PermissionDecision != "deny" {
		t.Fatalf("the hook's reply to %s is not a refusal of a tool call: %q", describeInput(bashTool, input), stdout)
	}
	if !strings.Contains(out.PermissionDecisionReason, "["+rule+"]") {
		t.Errorf("%s was refused under another rule than %s:\n%s", describeInput(bashTool, input), rule, out.PermissionDecisionReason)
	}
	if !strings.Contains(out.PermissionDecisionReason, "Instead:") {
		t.Errorf("the refusal of %s offers no route:\n%s", describeInput(bashTool, input), out.PermissionDecisionReason)
	}
}

// result is what one command printed and how it ended.
type result struct {
	stdout, stderr string
	code           int
}

// does is a command of the session under test: its words are put to the hook
// first, as a Bash call made from dir, and then it is run there.
func (sb sandbox) does(t *testing.T, dir, agent, stdin string, args ...string) result {
	t.Helper()
	sb.allows(t, dir, agent, "Bash", map[string]any{"command": "sdlc " + strings.Join(args, " ")})
	stdout, stderr, code := sb.outcome(t, sandboxSession, dir, stdin, args...)
	return result{stdout, stderr, code}
}

// person is a person-only step: run by this process, in the sandbox it made,
// with no session in the command's environment, and never put to the hook,
// because no tool call is made. The directory comes from the sandbox value and
// from nowhere else.
func (sb sandbox) person(t *testing.T, dir string, args ...string) result {
	t.Helper()
	if !strings.HasPrefix(dir, sb.dir+string(filepath.Separator)) {
		t.Fatalf("a person-only step was aimed at %s, which is not in the sandbox %s", dir, sb.dir)
	}
	stdout, stderr, code := sb.outcome(t, "", dir, "", args...)
	return result{stdout, stderr, code}
}

// normalise replaces what differs between two runs: where the sandbox, its
// repository, its sdlc and the story's worktree are, the time, and the names
// of commits and trees. Each path is replaced as it is, as %q prints it, and
// slash-separated, which is how sdlc spells a path inside a message (AC-3 of
// US-050): on Windows `sdlc start` names the worktree C:/..., which the native
// spelling alone never matched.
func (sb sandbox) normalise(text string) string {
	for _, p := range []struct{ path, name string }{
		{sb.tree(), "<worktree>"},
		{sb.repo, "<repository>"},
		{sb.sdlc, "<sdlc>"},
		{sb.dir, "<sandbox>"},
	} {
		quoted := strconv.Quote(p.path)
		text = strings.ReplaceAll(text, quoted[1:len(quoted)-1], p.name)
		text = strings.ReplaceAll(text, p.path, p.name)
		text = strings.ReplaceAll(text, filepath.ToSlash(p.path), p.name)
	}
	text = timePattern.ReplaceAllString(text, "<time>")
	return shaPattern.ReplaceAllString(text, "<sha>")
}

// shaPattern is a commit or a tree as sdlc prints one: in full, or its first
// twelve characters.
var shaPattern = regexp.MustCompile(`\b[0-9a-f]{40,64}\b|\b[0-9a-f]{12}\b`)

// writesFile is a file the session under test writes in the story's worktree,
// the hook asked first.
func (sb sandbox) writesFile(t *testing.T, agent, rel, body string) {
	t.Helper()
	path := filepath.Join(sb.tree(), filepath.FromSlash(rel))
	sb.allows(t, sb.tree(), agent, "Write", map[string]any{"file_path": path, "content": body})
	writeFile(t, path, body)
}
