package hook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/store"
)

// noEnv is a project with nothing set, so a test opts in to what it needs.
func noEnv(string) string { return "" }

func env(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

// loopProject makes a project that takes part in the loop with an iteration
// running on story A-1, which is the only state in which anything is enforced.
func loopProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	write(t, root, ".sdlc/config.json", `{"version":1}`)
	write(t, root, ".sdlc/state/active", "A-1\n")
	// `sdlc start` takes a story from the backlog, so the story being worked on
	// is always in it; the commit gate reads its risk tier there.
	write(t, root, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress"}]}`+"\n")
	return root
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func event(root, tool, agent, path string) string {
	e := map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       tool,
		"cwd":             root,
		"tool_input":      map[string]string{"file_path": path},
	}
	if agent != "" {
		e["agent_type"] = agent
	}
	raw, _ := json.Marshal(e)
	return string(raw)
}

type reply struct {
	Continue           bool   `json:"continue"`
	SystemMessage      string `json:"systemMessage"`
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func call(t *testing.T, stdin string, getenv func(string) string) reply {
	t.Helper()
	var out bytes.Buffer
	if code := Run([]string{"PreToolUse"}, strings.NewReader(stdin), &out, io.Discard, getenv); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r reply
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, out.String())
	}
	return r
}

func denied(r reply) bool { return r.HookSpecificOutput.PermissionDecision == "deny" }

// ------------------------------------------------------------------ shape

func TestRunAlwaysEmitsExactlyOneJSONObject(t *testing.T) {
	for _, tc := range []struct {
		name, stdin string
		args        []string
	}{
		{"no args", "", nil},
		{"event, empty stdin", "", []string{"PreToolUse"}},
		{"event with payload", `{"tool_name":"Bash"}`, []string{"PreToolUse"}},
		{"garbage stdin", "not json at all", []string{"PreToolUse"}},
		{"null payload", "null", []string{"PreToolUse"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := Run(tc.args, strings.NewReader(tc.stdin), &out, io.Discard, noEnv); code != 0 {
				t.Fatalf("exit %d", code)
			}
			var d Decision
			if err := json.Unmarshal(out.Bytes(), &d); err != nil {
				t.Fatalf("stdout is not JSON: %v (%q)", err, out.String())
			}
			if !d.Continue {
				t.Error("a malformed or irrelevant event must never block")
			}
			if lines := strings.Count(strings.TrimSpace(out.String()), "\n"); lines != 0 {
				t.Errorf("stdout is more than one line: %q", out.String())
			}
		})
	}
}

// A call too large to read whole cannot be checked. The hook still answers, and
// small, and says the call went unchecked: in silence, a Write to loop state cut
// off at the bound looked like a call with nothing in it to check.
func TestOversizedStdinIsNotCheckedAndSaysSo(t *testing.T) {
	root := loopProject(t)
	quoted, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	huge := `{"hook_event_name":"PreToolUse","tool_name":"Write","cwd":` + string(quoted) +
		`,"tool_input":{"file_path":".sdlc/state/active","content":"` + strings.Repeat("x", maxPayload) + `"}}`

	var out bytes.Buffer
	if code := Run([]string{"PreToolUse"}, strings.NewReader(huge), &out, io.Discard, noEnv); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() > 400 {
		t.Errorf("output should stay small, got %d bytes", out.Len())
	}
	var r reply
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.SystemMessage, "not checked") {
		t.Errorf("a call too large to read went through without saying it was not checked: %q", out.String())
	}
}

func TestDenyCarriesItsReason(t *testing.T) {
	d := Deny("tests are frozen at Gate 3 -- escalate rather than edit")
	if d.Continue {
		t.Error("Deny must not continue")
	}
	if d.StopReason == "" {
		t.Error("a denial with no reason teaches people to work around the tool")
	}
}

// ------------------------------------------------------------------ scope

// Outside a project that takes part, this does nothing at all.
func TestAProjectWithNoConfigIsNotGoverned(t *testing.T) {
	root := t.TempDir()
	r := call(t, event(root, "Write", "", ".git/config"), noEnv)
	if denied(r) {
		t.Error("a project with no .sdlc/config.json was governed")
	}
	// Not even the decisions the loop keeps for a person: there is no loop here
	// to keep them for.
	if denied(call(t, command(root, "", "sdlc approve A-1"), noEnv)) {
		t.Error("sdlc approve was refused in a project that does not take part")
	}
}

func TestNothingIsEnforcedWhileNoStoryIsBeingWorkedOn(t *testing.T) {
	root := loopProject(t)
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	if denied(call(t, event(root, "Write", "", "internal/x.go"), noEnv)) {
		t.Error("a write was refused with no iteration running")
	}
	if denied(call(t, command(root, "", "git commit -m x"), noEnv)) {
		t.Error("a commit was refused with no iteration running")
	}
	// Except the decisions that are a person's. `sdlc escalate` ends the
	// iteration, so an approval always came while nothing was enforced.
	for _, cmd := range []string{"sdlc approve A-1", "sdlc unfreeze --reason x"} {
		if !denied(call(t, command(root, "sdlc-implementer", cmd), noEnv)) {
			t.Errorf("%q went through because no story was being worked on", cmd)
		}
	}
}

// A session opened in the loop's project can commit in another repository, and
// that commit met this story's gate.
func TestAPathInTheHomeDirectoryIsNotTheProjects(t *testing.T) {
	resolver := pathrules.NewResolver(loopProject(t))
	for _, word := range []string{"~", "~/.claude/skills/check.py", "~someone/.claude/settings.json"} {
		if r := onDisk(resolver, word); r != "" {
			t.Errorf("%s was the project's, as %q", word, r)
		}
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if r := onDisk(resolver, "~/.claude/settings.json"); r != "" {
		t.Errorf("with HOME unset, ~/.claude/settings.json was the project's, as %q", r)
	}
	// Read as a home of "", ~/.claude is /.claude, which is outside every
	// project but one at the root of the disk.
	atRoot := pathrules.NewResolver(filepath.VolumeName(os.TempDir()) + string(filepath.Separator))
	if r := onDisk(atRoot, "~/.claude/settings.json"); r != "" {
		t.Errorf("with HOME unset, ~/.claude/settings.json was a project's at the root, as %q", r)
	}
	for _, word := range []string{"CLAUDE.md", "~CLAUDE.md"} {
		if r := onDisk(resolver, word); r != word {
			t.Errorf("the project's %s was %q", word, r)
		}
	}
}

// ~name is that user's home, and the user running the session may be the one
// named: read as somebody else's, a project under it was not protected.
func TestAUsersNamedHomeIsWhereTheirProjectIs(t *testing.T) {
	me, err := user.Current()
	if err != nil || strings.ContainsAny(me.Username, `\/`) || me.HomeDir == "" {
		t.Skip("no plain user name to write after ~")
	}
	atHome := pathrules.NewResolver(me.HomeDir)
	if r := onDisk(atHome, "~"+me.Username+"/CLAUDE.md"); r != "CLAUDE.md" {
		t.Errorf("~%s/CLAUDE.md in a project at that home was %q", me.Username, r)
	}
}

func TestACommitInAnotherRepositoryIsNotHeldToTheStory(t *testing.T) {
	root := loopProject(t)
	other := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(other); err == nil {
		other = resolved
	}
	raw, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "tool_name": "Bash", "cwd": other,
		"tool_input": map[string]string{"command": "git commit -m x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := call(t, string(raw), env(map[string]string{"CLAUDE_PROJECT_DIR": root})); denied(r) {
		t.Errorf("a commit in another repository was refused: %s", r.HookSpecificOutput.PermissionDecisionReason)
	}
	for _, line := range []string{"git commit -m x", `cd "` + filepath.ToSlash(root) + `" && git commit -m x`} {
		if !denied(call(t, command(root, "", line), noEnv)) {
			t.Errorf("a commit in the project went past its gates: %s", line)
		}
	}
}

// A human who started the session can turn enforcement off. A session cannot
// set this from the inside, which is what makes it an escape hatch rather than
// a hole.
func TestEnforcementCanBeTurnedOffFromTheEnvironment(t *testing.T) {
	root := loopProject(t)
	getenv := env(map[string]string{"SDLC_ENFORCE": "0"})
	if denied(call(t, event(root, "Write", "", ".git/config"), getenv)) {
		t.Error("SDLC_ENFORCE=0 did not turn enforcement off")
	}
}

// A session started below the repository root reports that directory, and
// looking for `.sdlc/config.json` only there found nothing and turned every
// rule off -- silently, which is the worst way for a discipline tool to fail.
// `cd backend && claude` was enough to do it.
func TestASessionStartedBelowTheRootIsStillGoverned(t *testing.T) {
	root := loopProject(t)
	for _, below := range []string{"backend", "backend/services/api"} {
		t.Run(below, func(t *testing.T) {
			deep := filepath.Join(root, filepath.FromSlash(below))
			if err := os.MkdirAll(deep, 0o755); err != nil {
				t.Fatal(err)
			}
			e := event(deep, "Write", "sdlc-researcher", filepath.Join(root, ".sdlc", "state", "active"))
			if !denied(call(t, e, noEnv)) {
				t.Error("loop state was writable from a session started below the root")
			}
		})
	}
}

// The walk up stops at the repository. A project of its own that happens to
// sit inside one taking part in the loop is not governed by it -- though the
// loop's own files still are, wherever the session writing them sits.
func TestTheWalkUpStopsAtTheRepository(t *testing.T) {
	root := loopProject(t)
	inner := filepath.Join(root, "vendor", "other")
	if err := os.MkdirAll(filepath.Join(inner, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := event(inner, "Write", "sdlc-researcher", filepath.Join(inner, "README.md"))
	if denied(call(t, e, noEnv)) {
		t.Error("a separate repository inside the project was governed by it")
	}
	e = event(inner, "Write", "sdlc-researcher", filepath.Join(root, ".sdlc", "state", "active"))
	if !denied(call(t, e, noEnv)) {
		t.Error("the loop's state was writable from a session in a repository inside the project")
	}
}

func TestClaudeProjectDirWinsOverTheReportedDirectory(t *testing.T) {
	root := loopProject(t)
	getenv := env(map[string]string{"CLAUDE_PROJECT_DIR": root})

	// The payload names somewhere else entirely; the environment is authoritative.
	e := event(t.TempDir(), "Write", "", ".git/config")
	if !denied(call(t, e, getenv)) {
		t.Error("CLAUDE_PROJECT_DIR was ignored")
	}
}

// ------------------------------------------------------------------ decisions

func TestAProtectedPathIsRefusedWithARouteAndARuleID(t *testing.T) {
	root := loopProject(t)
	r := call(t, event(root, "Write", "sdlc-researcher", ".sdlc/state/active"), noEnv)

	if !denied(r) {
		t.Fatal("loop state was writable during an iteration")
	}
	reason := r.HookSpecificOutput.PermissionDecisionReason
	for _, want := range []string{"protected", "Instead:", "write-protected-path"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason is missing %q: %q", want, reason)
		}
	}
	if r.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Errorf("hookEventName = %q", r.HookSpecificOutput.HookEventName)
	}
}

// The commit gate reads the story's risk tier from the backlog, and the
// implementer could lower it there to take away the person who approves the
// commit. The backlog is protected wherever the configuration puts it.
func TestTheBacklogIsNotEditedDuringAnIteration(t *testing.T) {
	root := loopProject(t)
	r := call(t, event(root, "Edit", "sdlc-implementer", filepath.Join(root, "user_stories.json")), noEnv)
	if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "backlog-is-not-edited") {
		t.Errorf("the backlog was not refused by its rule: %+v", r.HookSpecificOutput)
	}
	if !denied(call(t, command(root, "sdlc-implementer", `sed -i 's/"high"/"low"/' user_stories.json`), noEnv)) {
		t.Error("the backlog was writable through a shell command")
	}

	write(t, root, ".sdlc/config.json", `{"version":1,"backlog":{"path":"planning/backlog.json"}}`)
	if !denied(call(t, event(root, "Write", "sdlc-implementer", "planning/backlog.json"), noEnv)) {
		t.Error("a backlog the configuration moved was writable")
	}
	if !denied(call(t, command(root, "sdlc-implementer", "echo {} > planning/backlog.json"), noEnv)) {
		t.Error("a backlog the configuration moved was writable through a shell command")
	}
}

// Claude Code gives the hook thirty seconds and lets a call through, without a
// word, when it takes longer. A command naming the same paths in segment after
// segment took that long, checked word by word again for every segment.
func TestALongCommandIsReadWellInsideTheHooksTime(t *testing.T) {
	root := loopProject(t)
	freeze(t, root, "A-1", "internal/x_test.go", "package x\n")
	var paths []string
	for i := range 20 {
		paths = append(paths, fmt.Sprintf("internal/pkg/file%d.go", i))
	}
	long := strings.Repeat("echo "+strings.Join(paths, " ")+" | xargs rm; ", 100)

	start := time.Now()
	r := call(t, command(root, "sdlc-implementer", long), noEnv)
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("a %d KB command took %s", len(long)>>10, took)
	}
	if denied(r) {
		t.Errorf("an ordinary command was refused: %s", r.HookSpecificOutput.PermissionDecisionReason)
	}
}

// A link made to .sdlc, CLAUDE.md or a frozen test names it in letters no rule
// matches, as a short name such as SDLC~1 does on Windows. The shell rules read
// the file a word is on disk.
func TestAShellCommandThroughALinkIsReadAsTheFileItReaches(t *testing.T) {
	root := loopProject(t)
	write(t, root, "CLAUDE.md", "# contract\n")
	freeze(t, root, "A-1", "internal/x_test.go", "package x\n")
	for link, target := range map[string]string{
		"notes":    ".sdlc",
		"brief.md": "CLAUDE.md",
		"alias.go": filepath.Join("internal", "x_test.go"),
	} {
		if err := os.Symlink(filepath.Join(root, target), filepath.Join(root, link)); err != nil {
			t.Skipf("symbolic links cannot be made here: %v", err)
		}
	}
	for _, cmd := range []string{
		"rm notes/state/active",
		"cd notes && rm state/tests.lock",
		"echo x > brief.md",
		"echo cheat > alias.go",
	} {
		if !denied(call(t, command(root, "sdlc-implementer", cmd), noEnv)) {
			t.Errorf("%q went through a link", cmd)
		}
	}
	if denied(call(t, command(root, "sdlc-implementer", "echo x > scratch.txt"), noEnv)) {
		t.Error("an ordinary write was refused")
	}
}

// macOS and Windows fold case. Named with the project's path in capitals, the
// loop's record was outside the project and nobody's to protect, and a commit
// made there was another repository's.
func TestTheProjectSpelledInAnotherCaseIsStillTheProject(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("this filesystem keeps case, so the path in capitals is another one")
	}
	root := loopProject(t)
	upper := filepath.ToSlash(strings.ToUpper(root))
	for _, cmd := range []string{
		"rm " + upper + "/.sdlc/state/active",
		"cd " + upper + " && git commit -m x",
	} {
		if !denied(call(t, command(root, "sdlc-implementer", cmd), noEnv)) {
			t.Errorf("%q went through", cmd)
		}
	}
}

// In PowerShell a backtick escapes the character after it, so the hook reads a
// PowerShell command with that in mind, and a Bash one without it.
func TestAPowerShellCommandIsReadAsPowerShell(t *testing.T) {
	root := loopProject(t)
	raw, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse", "tool_name": "PowerShell", "cwd": root,
		"agent_type": "sdlc-implementer", "tool_input": map[string]string{"command": "Remove-Item CLAUDE`.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r := call(t, string(raw), noEnv); !denied(r) {
		t.Error("CLAUDE.md, escaped with a backtick, was removable from PowerShell")
	}
}

// Claude Code on Windows writes `\repo\CLAUDE.md` to the project's own
// CLAUDE.md, and the hook read it as `repo/CLAUDE.md`, a file nothing protects.
func TestAPathRootedWithoutADriveIsGovernedOnWindows(t *testing.T) {
	root := loopProject(t)
	volume := filepath.VolumeName(root)
	if volume == "" {
		t.Skip("only Windows has a path that is rooted and not absolute")
	}
	r := call(t, event(root, "Write", "sdlc-implementer", root[len(volume):]+`\CLAUDE.md`), noEnv)
	if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "write-protected-path") {
		t.Errorf("CLAUDE.md, named without its drive, was not refused as protected: %+v", r.HookSpecificOutput)
	}
}

// A backlog linked in from outside the repository resolved outside it, and was
// left unprotected: the shell edited it through the link.
func TestABacklogLinkedInFromElsewhereIsStillProtected(t *testing.T) {
	root := loopProject(t)
	elsewhere := filepath.Join(t.TempDir(), "stories.json")
	if err := os.WriteFile(elsewhere, []byte(`{"stories":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(root, "linked.json")); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
	write(t, root, ".sdlc/config.json", `{"version":1,"backlog":{"path":"linked.json"}}`)
	if !denied(call(t, command(root, "sdlc-implementer", "echo x >> linked.json"), noEnv)) {
		t.Error("the backlog was edited through its link")
	}
	if !denied(call(t, event(root, "Edit", "sdlc-implementer", filepath.Join(root, "linked.json")), noEnv)) {
		t.Error("the backlog was edited through its link with a file tool")
	}
}

func TestTheResearcherWorksInItsOwnStoryAndNowhereElse(t *testing.T) {
	root := loopProject(t)

	if denied(call(t, event(root, "Write", "sdlc-researcher", ".sdlc/stories/A-1/notes.md"), noEnv)) {
		t.Error("the researcher could not take notes in its own story directory")
	}
	if !denied(call(t, event(root, "Write", "sdlc-researcher", "internal/billing/invoice.go"), noEnv)) {
		t.Error("the researcher wrote production code")
	}
}

// A gate artifact is loop state, so it goes through the tool no matter who is
// asking -- including the agent whose gate it is.
func TestAGateArtifactIsNotWrittenInPlaceByAnyone(t *testing.T) {
	root := loopProject(t)

	for _, agent := range []string{"", "sdlc-researcher", "sdlc:researcher"} {
		r := call(t, event(root, "Write", agent, ".sdlc/stories/A-1/ANALYSIS.md"), noEnv)
		if !denied(r) {
			t.Errorf("%q wrote the analysis in place", agent)
			continue
		}
		if !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "sdlc artifact write") {
			t.Errorf("the denial does not name the route: %q",
				r.HookSpecificOutput.PermissionDecisionReason)
		}
	}
}

// The denial used to offer `sdlc stop` and taking the work over as the way
// out, and ending the iteration turns every rule off: followed, it committed
// work no gate had passed.
func TestTheMainConversationIsToldToDelegate(t *testing.T) {
	root := loopProject(t)
	r := call(t, event(root, "Edit", "", "internal/billing/invoice.go"), noEnv)

	if !denied(r) {
		t.Fatal("the main conversation edited code during an iteration")
	}
	reason := r.HookSpecificOutput.PermissionDecisionReason
	if !strings.Contains(reason, "delegate") {
		t.Errorf("the denial does not name the route: %q", reason)
	}
	if strings.Contains(reason, "sdlc stop") {
		t.Errorf("the denial sends it to end the iteration: %q", reason)
	}
}

func TestReadingIsNeverRefused(t *testing.T) {
	root := loopProject(t)
	for _, tool := range []string{"Read", "Grep", "Glob", "Bash"} {
		if denied(call(t, event(root, tool, "", ".sdlc/state/active"), noEnv)) {
			t.Errorf("%s was refused; the loop governs writing", tool)
		}
	}
}

// A relative path in the payload is resolved against the project, so a rule
// written as ".git/config" catches it either way.
func TestAnAbsolutePathIsResolvedAgainstTheProject(t *testing.T) {
	root := loopProject(t)
	abs := filepath.Join(root, ".git", "config")
	if !denied(call(t, event(root, "Write", "", abs), noEnv)) {
		t.Error("an absolute path to a protected file was allowed")
	}
}

// The loop governs what it can review, and nothing outside the repository is
// that: a settings file in the home directory is where hooks are turned off. A
// symlink inside the repository leads to the same place by another name.
func TestAWriteOutsideTheRepositoryIsRefused(t *testing.T) {
	root := loopProject(t)
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(elsewhere, ".claude", "settings.json"),
		"link/settings.json",
	} {
		r := call(t, event(root, "Write", "sdlc:implementer", path), noEnv)
		if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "write-outside-repository") {
			t.Errorf("the implementer wrote %s, outside the repository: %+v", path, r)
		}
	}
}

// Claude Code's project directory is not always the loop project: a session can
// be opened above the repository, or add it as another directory. The rules
// still apply to what a tool call does inside it.
func TestTheLoopIsFoundWhereTheToolCallActs(t *testing.T) {
	root := loopProject(t)
	above := filepath.Dir(root)
	session := env(map[string]string{"CLAUDE_PROJECT_DIR": above})

	// A file tool, from a session still sitting above the repository.
	if r := call(t, event(above, "Write", "", filepath.Join(root, ".sdlc", "state", "active")), session); !denied(r) {
		t.Errorf("loop state was writable from a session opened above the repository: %+v", r)
	}
	// A command, run from inside it.
	if r := call(t, command(root, "", "rm .sdlc/state/tests.lock"), session); !denied(r) {
		t.Errorf("the freeze could be removed from a session opened above the repository: %+v", r)
	}

	// A command run from above it, naming what it acts on. A command has no
	// file of its own to find the project from, and each of these was allowed
	// in silence.
	freeze(t, root, "A-1", "internal/invoice_test.go", "package internal\n")
	name := filepath.Base(root)
	for _, c := range []struct{ agent, cmd string }{
		{"sdlc:implementer", "rm " + name + "/internal/invoice_test.go"},
		{"sdlc:implementer", `rm "` + name + `/.sdlc/state/tests.lock"`},
		{"", "echo {} > " + name + "/.sdlc/config.json"},
		{"", "cd " + name + " && sdlc unfreeze --reason x"},
		{"", "git -C " + name + " commit -am wip"},
	} {
		if r := call(t, command(above, c.agent, c.cmd), session); !denied(r) {
			t.Errorf("%q, run from above the repository, was allowed: %+v", c.cmd, r)
		}
	}
	if r := call(t, command(above, "", "ls "+name), session); denied(r) {
		t.Errorf("looking into the repository from above it was refused: %+v", r)
	}

	// An approval is a person's with no story running, from above as well.
	write(t, root, ".sdlc/state/active", "")
	if r := call(t, command(above, "", "cd "+name+" && sdlc approve A-1"), session); !denied(r) {
		t.Errorf("an approval run from above the repository was allowed: %+v", r)
	}
}

func TestANotebookPathIsGovernedToo(t *testing.T) {
	root := loopProject(t)
	e, _ := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "NotebookEdit",
		"cwd":             root,
		"tool_input":      map[string]string{"notebook_path": "analysis.ipynb"},
	})
	if !denied(call(t, string(e), noEnv)) {
		t.Error("a notebook edit by the main conversation was allowed")
	}
}

// An active-story file that has been tampered with must not become part of a
// path. Failing open here is deliberate: the gate record is the real guarantee.
func TestATamperedActiveStoryTurnsEnforcementOffRatherThanBuildingABadPath(t *testing.T) {
	root := loopProject(t)
	write(t, root, ".sdlc/state/active", "../../etc\n")

	if denied(call(t, event(root, "Write", "sdlc-researcher", "internal/x.go"), noEnv)) {
		t.Error("a tampered active file was used to build a rule")
	}
}

// ------------------------------------------------------------------ the freeze

// freeze writes a lock covering one file, with the hash of what is on disk.
func freeze(t *testing.T, root, story, rel, body string) {
	t.Helper()
	write(t, root, rel, body)
	sum := sha256.Sum256([]byte(body))
	write(t, root, ".sdlc/state/tests.lock", `{"schema":"sdlc/tests-lock/1","story":"`+story+
		`","at":"2026-09-10T08:30:00Z","files":{"`+rel+`":"`+hex.EncodeToString(sum[:])+`"}}`)
}

// The freeze has to survive the trip through the hook, or it protects nothing
// where it matters: the rules are only reached with the state the hook worked
// out from the project's own files.
func TestAFrozenTestIsRefusedThroughTheHook(t *testing.T) {
	root := loopProject(t)
	freeze(t, root, "A-1", "internal/invoice_test.go", "package internal\n")

	r := call(t, event(root, "Edit", "sdlc-implementer", "internal/invoice_test.go"), noEnv)
	if !denied(r) {
		t.Fatal("a frozen acceptance test was editable during an iteration")
	}
	reason := r.HookSpecificOutput.PermissionDecisionReason
	for _, want := range []string{"frozen", "sdlc unfreeze", "frozen-test-is-not-edited"} {
		if !strings.Contains(reason, want) {
			t.Errorf("reason is missing %q: %q", want, reason)
		}
	}
}

// The lookups the rules are built from, asked directly. Through the hook the
// sdet may edit tests anyway, so a freeze applied to the wrong story, or a held
// test taken for a new one by its case, passed every test there.
func TestTheFreezeIsReadForItsOwnStoryAndByFoldedName(t *testing.T) {
	root := loopProject(t)
	noWarning := func(string) {}
	freeze(t, root, "OTHER-1", "internal/invoice_test.go", "package internal\n")
	if frozen, isTest, newTest := frozenTests(root, "A-1", noWarning); frozen != nil || isTest != nil || newTest != nil {
		t.Errorf("another story's freeze was applied to A-1: frozen %v", frozen)
	}

	freeze(t, root, "A-1", "Internal/Invoice_test.go", "package internal\n")
	_, _, newTest := frozenTests(root, "A-1", noWarning)
	if newTest == nil {
		t.Fatal("no new-test rule for a project that does not allow new test files")
	}
	if !newTest("internal/refund_test.go") {
		t.Error("a test file the freeze does not hold was not new")
	}
	if newTest("internal/invoice_test.go") {
		t.Error("a test the freeze holds, named in another case, was taken for a new one")
	}
}

// A freeze belongs to one story. Left over from another, it must not lock this
// one's tests -- and must not be quietly ignored either, which is why the CLI
// refuses to pass the gate on it.
func TestAFreezeFromAnotherStoryDoesNotBiteHere(t *testing.T) {
	root := loopProject(t)
	freeze(t, root, "OTHER-9", "internal/invoice_test.go", "package internal\n")

	if denied(call(t, event(root, "Edit", "sdlc-sdet", "internal/invoice_test.go"), noEnv)) {
		t.Error("another story's freeze locked this story's tests")
	}
}

// Before the freeze, the test author writes tests. After it, a new test file is
// the freeze with extra steps.
func TestNewTestFilesAreOnlyRefusedAfterTheFreeze(t *testing.T) {
	root := loopProject(t)
	write(t, root, "internal/invoice_test.go", "package internal\n")

	if denied(call(t, event(root, "Write", "sdlc-sdet", "internal/extra_test.go"), noEnv)) {
		t.Error("the test author could not write a test before the freeze")
	}

	freeze(t, root, "A-1", "internal/invoice_test.go", "package internal\n")
	if !denied(call(t, event(root, "Write", "sdlc-sdet", "internal/extra_test.go"), noEnv)) {
		t.Error("a new test file was added after the freeze")
	}
}

// The project decides what a test file is. A project that names nothing has
// nothing frozen, and the hook must not invent a convention for it.
func TestWhatCountsAsATestComesFromTheProject(t *testing.T) {
	root := loopProject(t)
	write(t, root, ".sdlc/config.json", `{"version":1,"paths":{"tests":{"dirs":[],"file_globs":["*.check.js"]}}}`)
	write(t, root, "internal/invoice_test.go", "package internal\n")

	if denied(call(t, event(root, "Write", "sdlc-implementer", "internal/invoice_test.go"), noEnv)) {
		t.Error("a Go test was protected in a project that only calls *.check.js a test")
	}
	if !denied(call(t, event(root, "Write", "sdlc-implementer", "billing/total.check.js"), noEnv)) {
		t.Error("the project's own convention was not applied")
	}
}

// A configuration that will not parse must not switch off the rules that have
// nothing to do with it. Failing open on everything is how a control quietly
// stops being one.
func TestABrokenConfigurationStillProtectsWhatItCan(t *testing.T) {
	root := loopProject(t)
	write(t, root, ".sdlc/config.json", "{not json")
	write(t, root, "invoice_test.go", "package x\n")
	write(t, root, ".sdlc/state/tests.lock", `{"story":"A-1","files":{"invoice_test.go":"abc"}}`)

	if !denied(call(t, event(root, "Write", "", ".sdlc/state/active"), noEnv)) {
		t.Error("loop state became writable because the configuration was broken")
	}
	// The freeze names its files, so it holds without the configuration. The
	// session itself has no write scope to be refused by, so only the freeze
	// can refuse this.
	if r := call(t, event(root, "Edit", "", "invoice_test.go"), noEnv); !denied(r) ||
		!strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "frozen-test-is-not-edited") {
		t.Error("a frozen test became editable because the configuration was broken")
	}
	if !denied(call(t, command(root, "sdlc:implementer", "echo x > invoice_test.go"), noEnv)) {
		t.Error("a frozen test became writable through the shell because the configuration was broken")
	}
	// What is a test falls back to the default patterns, so the rules that ask
	// still have an answer.
	if !denied(call(t, event(root, "Write", "sdlc:implementer", "billing_test.go"), noEnv)) {
		t.Error("the implementer wrote a test because the configuration was broken")
	}
	if !denied(call(t, command(root, "sdlc:implementer", "echo x > billing_test.go"), noEnv)) {
		t.Error("a new test went in through the shell because the configuration was broken")
	}
}

// Failing open is deliberate. Failing open in silence is not: a hook that has
// decided to enforce nothing is indistinguishable from a hook with nothing to
// enforce. The warning has to be in systemMessage, because stderr from a hook
// that exits 0 goes to Claude Code's debug log and is read by nobody.
func TestTheHookSaysWhenItHasStoppedEnforcing(t *testing.T) {
	corrupt := func(rel string) func(*testing.T, string) {
		return func(t *testing.T, root string) { write(t, root, rel, "{not json") }
	}
	for _, tc := range []struct {
		name  string
		spoil func(*testing.T, string)
		// A file write consults the configuration and the freeze; a shell
		// command consults the freeze alone. Each failure is reached on the
		// path that reads it.
		shell bool
		says  string
	}{
		{
			name:  "a configuration that will not parse",
			spoil: corrupt(".sdlc/config.json"),
			says:  "tests are being recognised by the default patterns",
		},
		{
			name:  "a freeze that will not parse, on a file write",
			spoil: corrupt(".sdlc/state/tests.lock"),
			says:  "every test file is being treated as frozen",
		},
		{
			name:  "a freeze that will not parse, on a shell command",
			spoil: corrupt(".sdlc/state/tests.lock"),
			shell: true,
			says:  "every test file is being treated as frozen",
		},
		{
			name: "a freeze and a configuration that will not parse, on a shell command",
			spoil: func(t *testing.T, root string) {
				corrupt(".sdlc/state/tests.lock")(t, root)
				corrupt(".sdlc/config.json")(t, root)
			},
			shell: true,
			says:  "every file the default patterns call a test is being treated as frozen",
		},
		{
			name: "an iteration file that cannot be read",
			spoil: func(t *testing.T, root string) {
				active := filepath.Join(root, ".sdlc", "state", "active")
				if err := os.Remove(active); err != nil {
					t.Fatal(err)
				}
				// A directory where the file should be: there, and unreadable
				// as a file, on every platform.
				if err := os.MkdirAll(active, 0o755); err != nil {
					t.Fatal(err)
				}
			},
			says: "nothing is being enforced",
		},
		{
			name: "an iteration file that names no story",
			spoil: func(t *testing.T, root string) {
				write(t, root, ".sdlc/state/active", "../elsewhere")
			},
			says: "does not name a story",
		},
		{
			name: "an iteration file naming a story the CLI would refuse",
			spoil: func(t *testing.T, root string) {
				write(t, root, ".sdlc/state/active", "A..1")
			},
			says: "does not name a story",
		},
		{
			name: "an iteration file left empty",
			spoil: func(t *testing.T, root string) {
				write(t, root, ".sdlc/state/active", "")
			},
			says: "does not name a story",
		},
		{
			name: "an iteration file holding only a newline",
			spoil: func(t *testing.T, root string) {
				write(t, root, ".sdlc/state/active", " \n")
			},
			says: "does not name a story",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := loopProject(t)
			tc.spoil(t, root)
			stdin := event(root, "Write", "sdlc:implementer", "invoice.go")
			if tc.shell {
				stdin = command(root, "sdlc:implementer", "echo hello")
			}
			if got := call(t, stdin, noEnv).SystemMessage; !strings.Contains(got, tc.says) {
				t.Errorf("systemMessage does not say enforcement is off: %q", got)
			}
		})
	}
}

// A test file added after the freeze was refused from Write and allowed from a
// redirect, and a test written after the implementation can be written to pass.
func TestANewTestFileIsRefusedThroughTheShellUnlessTheProjectAllowsIt(t *testing.T) {
	root := loopProject(t)
	write(t, root, "invoice_test.go", "package x\n")
	write(t, root, ".sdlc/state/tests.lock", `{"story":"A-1","files":{"invoice_test.go":"abc"}}`)

	r := call(t, command(root, "sdlc:implementer", "echo 'package x' > other_test.go"), noEnv)
	if !denied(r) {
		t.Fatal("a new test file was added through the shell after the freeze")
	}
	if !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "no-new-test-after-the-freeze") {
		t.Errorf("refused for the wrong reason: %q", r.HookSpecificOutput.PermissionDecisionReason)
	}

	// A project that allows new test files allows them to the test author. The
	// implementer does not write tests, whatever the project allows.
	write(t, root, ".sdlc/config.json", `{"version":1,"freeze":{"allow_new_test_files":true}}`)
	if r := call(t, command(root, "sdlc:sdet", "echo 'package x' > other_test.go"), noEnv); denied(r) {
		t.Errorf("a project that allows new test files refused the test author one: %q",
			r.HookSpecificOutput.PermissionDecisionReason)
	}
	r = call(t, command(root, "sdlc:implementer", "echo 'package x' > other_test.go"), noEnv)
	if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "implementer-does-not-write-tests") {
		t.Errorf("the implementer wrote a test through the shell because new test files are allowed: %+v", r)
	}

	// Nor before the freeze, when there is nothing frozen for it to break.
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "tests.lock")); err != nil {
		t.Fatal(err)
	}
	r = call(t, command(root, "sdlc:implementer", "echo 'package x' >> helper_test.go"), noEnv)
	if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "implementer-does-not-write-tests") {
		t.Errorf("the implementer wrote a test through the shell before the freeze: %+v", r)
	}
}

// A freeze that cannot be read is not an absent freeze. Reading it as absent
// made corrupting tests.lock the way to edit a frozen test, silently.
func TestAnUnreadableFreezeStillProtectsTheTests(t *testing.T) {
	root := loopProject(t)
	write(t, root, "invoice_test.go", "package x\n")
	write(t, root, ".sdlc/state/tests.lock", "{not json")

	if !denied(call(t, event(root, "Write", "sdlc:sdet", "invoice_test.go"), noEnv)) {
		t.Error("a test file became editable because the freeze could not be read")
	}
	// And through the shell, which was the half 0ef7050 left open.
	if !denied(call(t, command(root, "sdlc:implementer", "echo cheat > invoice_test.go"), noEnv)) {
		t.Error("a test file became writable through the shell because the freeze could not be read")
	}
	if denied(call(t, command(root, "sdlc:implementer", "echo fine > invoice.go"), noEnv)) {
		t.Error("an unreadable freeze froze a file that is not a test")
	}
	// The implementer is refused any test through the shell, so the freeze is
	// only seen doing it for somebody who may otherwise write one -- with the
	// configuration readable, and without.
	for _, spoiled := range []bool{false, true} {
		if spoiled {
			write(t, root, ".sdlc/config.json", "{not json")
		}
		r := call(t, command(root, "sdlc:sdet", "echo cheat > invoice_test.go"), noEnv)
		if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "frozen-test-through-the-tool") {
			t.Errorf("a test file became writable through the shell because the freeze could not be read "+
				"(configuration unreadable too: %v): %+v", spoiled, r)
		}
	}
}

// The ordinary states are not warnings. Before Gate 3 there is no freeze, and
// a hook that cried wolf on every tool call would be turned off by lunchtime.
func TestTheHookIsQuietWhenNothingIsWrong(t *testing.T) {
	root := loopProject(t)
	for name, stdin := range map[string]string{
		"a file write":    event(root, "Write", "sdlc:implementer", "invoice.go"),
		"a shell command": command(root, "sdlc:implementer", "echo hello"),
	} {
		if got := call(t, stdin, noEnv).SystemMessage; got != "" {
			t.Errorf("%s in a project with no freeze yet produced a warning: %q", name, got)
		}
	}
}

// ------------------------------------------------------------------ the shell

func command(root, agent, cmd string) string {
	e := map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"cwd":             root,
		"tool_input":      map[string]string{"command": cmd},
	}
	if agent != "" {
		e["agent_type"] = agent
	}
	raw, _ := json.Marshal(e)
	return string(raw)
}

// Every other rule in this tool governs the file-writing tools. If the shell is
// not covered, the whole set of them is one `cat >` away from irrelevant.
func TestTheShellCannotWriteWhatTheToolsMayNot(t *testing.T) {
	root := loopProject(t)

	r := call(t, command(root, "sdlc-researcher", "cat x > .sdlc/stories/A-1/ANALYSIS.md"), noEnv)
	if !denied(r) {
		t.Fatal("the loop's record was writable through a shell command")
	}
	for _, want := range []string{"sdlc artifact write", "loop-state-through-the-tool"} {
		if !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, want) {
			t.Errorf("reason is missing %q: %q", want, r.HookSpecificOutput.PermissionDecisionReason)
		}
	}

	if denied(call(t, command(root, "sdlc-researcher", "go test ./... -count=1"), noEnv)) {
		t.Error("an ordinary command was refused")
	}
}

// The reviewer a review names is the one who records it, as the agent prompts
// have it. The implementer recording the code reviewer's approval is refused.
func TestAReviewIsRecordedByTheReviewerItNames(t *testing.T) {
	root := loopProject(t)
	add := "sdlc review add code_review code-reviewer approve --note fine <<'SDLC_DOCUMENT'\n" +
		"looks good\nSDLC_DOCUMENT"
	for _, agent := range []string{"sdlc:implementer", "", "general-purpose", "sdlc:architect"} {
		if !denied(call(t, command(root, agent, add), noEnv)) {
			t.Errorf("%q recorded the code reviewer's review", agent)
		}
	}
	if r := call(t, command(root, "sdlc:code-reviewer", add), noEnv); denied(r) {
		t.Errorf("the code reviewer could not record its own review: %s",
			r.HookSpecificOutput.PermissionDecisionReason)
	}
}

// The Bash tool keeps a cd from one call to the next, and the payload's cwd is
// where it left the session. A relative path is read from there.
func TestAShellCommandIsReadFromTheSessionsDirectory(t *testing.T) {
	root := loopProject(t)
	e := map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"cwd":             filepath.Join(root, ".sdlc", "state"),
		"agent_type":      "sdlc:implementer",
		"tool_input":      map[string]string{"command": "rm tests.lock"},
	}
	raw, _ := json.Marshal(e)
	if !denied(call(t, string(raw), noEnv)) {
		t.Error("the freeze was removed by a relative path from inside .sdlc/state")
	}
}

// Bash is not the only tool that runs a command. Monitor ran one the hook never
// looked at, and so would PowerShell on Windows.
func TestEveryToolThatRunsACommandMeetsTheShellRules(t *testing.T) {
	root := loopProject(t)
	for _, tool := range []string{"Bash", "Monitor", "PowerShell"} {
		e := map[string]any{
			"hook_event_name": "PreToolUse",
			"tool_name":       tool,
			"cwd":             root,
			"agent_type":      "sdlc:implementer",
			"tool_input":      map[string]string{"command": "rm .sdlc/state/tests.lock"},
		}
		raw, _ := json.Marshal(e)
		if !denied(call(t, string(raw), noEnv)) {
			t.Errorf("%s removed the test freeze", tool)
		}
	}
}

// The commit gate is the one the README promises. It stands in front of the
// commit itself, because by the time a commit has happened the gate has nothing
// left to protect.
func TestTheCommitGateStandsInFrontOfGitCommit(t *testing.T) {
	root := loopProject(t)
	write(t, root, ".sdlc/stories/A-1/gate-record.json",
		`{"story":"A-1","gates":{"dor":{"status":"pass","at":"2026-09-10T08:30:00Z"}}}`)

	r := call(t, command(root, "", `git commit -m "done"`), noEnv)
	if !denied(r) {
		t.Fatal("a commit went through with the gates unpassed")
	}
	if !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "analysis has not passed") {
		t.Errorf("the refusal does not say what is missing: %q",
			r.HookSpecificOutput.PermissionDecisionReason)
	}

	// Every gate but the last one before the commit is not every gate.
	gates := map[string]any{}
	for _, g := range model.GateCommit.Before() {
		if g != model.GateCodeReview {
			gates[string(g)] = map[string]string{"status": "pass", "at": "2026-09-10T08:30:00Z"}
		}
	}
	raw, err := json.Marshal(map[string]any{"story": "A-1", "gates": gates})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))
	r = call(t, command(root, "", `git commit -m "done"`), noEnv)
	if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "code_review has not passed") {
		t.Errorf("a commit went through with the code review unpassed: %+v", r)
	}
}

// Claude Code kills a hook that outlasts the timeout hooks.json gives it, and a
// killed hook decides nothing: a commit it was checking goes through unchecked,
// with no message. Each of the hook's own limits has to run out first.
func TestClaudeCodeWaitsLongerThanTheHookDoes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Timeout float64 `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	// The longest each event can take: measuring the tree for a commit, a
	// formatter, and for Stop the ten seconds store.Lock waits for the lock
	// before the tree it measures to hand the story over.
	longest := map[string]time.Duration{
		"PreToolUse":  treeTimeout,
		"PostToolUse": formatTimeout,
		"Stop":        10*time.Second + treeTimeout,
	}
	for event, bound := range longest {
		if len(cfg.Hooks[event]) == 0 {
			t.Errorf("hooks.json has no %s hook", event)
		}
		for _, entry := range cfg.Hooks[event] {
			for _, h := range entry.Hooks {
				if given := time.Duration(h.Timeout * float64(time.Second)); given <= bound {
					t.Errorf("hooks.json gives %s %s, and the hook can take %s", event, given, bound)
				}
			}
		}
	}
}

func TestOnceEveryGateHasPassedTheCommitGoesThrough(t *testing.T) {
	root := loopProject(t)
	gates := map[string]any{}
	for _, g := range model.GateCommit.Before() {
		gates[string(g)] = map[string]string{"status": "pass", "at": "2026-09-10T08:30:00Z"}
	}
	raw, err := json.Marshal(map[string]any{"story": "A-1", "gates": gates})
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))

	if denied(call(t, command(root, "", `git commit -m "done"`), noEnv)) {
		t.Error("the commit gate never opens")
	}
}

// Code edited after it was reviewed went through `git commit` untouched: the
// hook asked only whether the gates had passed. It now asks whether the tree is
// the one the reviews were stamped against, which takes a real repository.
func TestACommitOfWorkChangedSinceItWasReviewedIsRefused(t *testing.T) {
	root := loopProject(t)
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	initRepo := exec.CommandContext(t.Context(), "git", "init", "--quiet")
	initRepo.Dir = root
	if out, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	write(t, root, "invoice.go", "package invoice\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.New(&config.Project{Root: root, Config: cfg}).ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now()
	record := model.NewRecord("A-1", at)
	for _, g := range model.GateCommit.Before() {
		record.SetGate(g, model.GatePass, "", at)
	}
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			record.AddReview(model.Review{Gate: gate, Role: r.Role, Verdict: model.VerdictApprove, Subject: tree}, at)
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))

	if r := call(t, command(root, "", "git commit -m done"), noEnv); denied(r) {
		t.Fatalf("a commit of exactly what was reviewed was refused: %s",
			r.HookSpecificOutput.PermissionDecisionReason)
	}

	write(t, root, "invoice.go", "package invoice\n\nfunc Unreviewed() {}\n")
	r := call(t, command(root, "", "git commit -m done"), noEnv)
	if !denied(r) {
		t.Fatal("code changed after it was reviewed went through to the commit")
	}
	if !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "code-reviewer") {
		t.Errorf("the refusal does not say whose review is stale: %q",
			r.HookSpecificOutput.PermissionDecisionReason)
	}
}

// Waiting for a person puts a story in awaiting_human, and resuming puts it
// back: two writes to the tracked backlog that change nothing a reviewer
// approved. The commit was refused as if the work had changed. A change to the
// story itself still is.
func TestACommitIsNotRefusedBecauseTheStoryWaitedForAPerson(t *testing.T) {
	root := loopProject(t)
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	initRepo := exec.CommandContext(t.Context(), "git", "init", "--quiet")
	initRepo.Dir = root
	if out, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	write(t, root, "invoice.go", "package invoice\n")
	write(t, root, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress"}]}`+"\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(&config.Project{Root: root, Config: cfg})
	tree, err := s.ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now()
	record := model.NewRecord("A-1", at)
	for _, g := range model.GateCommit.Before() {
		record.SetGate(g, model.GatePass, "", at)
	}
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			record.AddReview(model.Review{Gate: gate, Role: r.Role, Verdict: model.VerdictApprove, Subject: tree}, at)
		}
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))

	later := s.WithClock(func() time.Time { return at.Add(time.Hour) })
	for _, status := range []model.Status{model.StatusAwaitingHuman, model.StatusInProgress} {
		if err := later.SetStoryStatus("A-1", status); err != nil {
			t.Fatal(err)
		}
	}
	if r := call(t, command(root, "", "git commit -m done"), noEnv); denied(r) {
		t.Fatalf("a story that waited for a person had its reviews called stale: %s",
			r.HookSpecificOutput.PermissionDecisionReason)
	}

	write(t, root, "user_stories.json",
		`{"stories":[{"id":"A-1","title":"Invoices, and refunds","status":"in_progress"}]}`+"\n")
	if !denied(call(t, command(root, "", "git commit -m done"), noEnv)) {
		t.Error("a story changed after it was reviewed went through to the commit")
	}
}

// Ending the iteration turns every rule off, so an agent that could run
// `sdlc stop` part-way through a story could commit it ungated straight after.
// The runbook's own `sdlc stop`, after the last gate, still goes through.
func TestAStoryIsEndedPartWayOnlyByAPerson(t *testing.T) {
	root := loopProject(t)
	if !denied(call(t, command(root, "", "sdlc stop"), noEnv)) {
		t.Error("sdlc stop went through with the story's gates still to work")
	}

	at := time.Now()
	record := model.NewRecord("A-1", at)
	for _, g := range model.Gates {
		record.SetGate(g, model.GatePass, "", at)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))
	if r := call(t, command(root, "", "sdlc stop"), noEnv); denied(r) {
		t.Errorf("the runbook's last step was refused: %s", r.HookSpecificOutput.PermissionDecisionReason)
	}
}

// A story in a tier the project pauses waits for a person before the commit,
// and the hook asks before it, not only `sdlc gate commit pass` afterwards.
func TestACommitOfAPausedStoryWaitsForAPersonsApproval(t *testing.T) {
	root := loopProject(t)
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	initRepo := exec.CommandContext(t.Context(), "git", "init", "--quiet")
	initRepo.Dir = root
	if out, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	write(t, root, "invoice.go", "package invoice\n")
	write(t, root, "user_stories.json",
		`{"stories":[{"id":"A-1","title":"Refunds","status":"in_progress","risk_tier":"high"}]}`+"\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.New(&config.Project{Root: root, Config: cfg}).ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now()
	record := model.NewRecord("A-1", at)
	for _, g := range model.GateCommit.Before() {
		record.SetGate(g, model.GatePass, "", at)
	}
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			record.AddReview(model.Review{Gate: gate, Role: r.Role, Verdict: model.VerdictApprove, Subject: tree}, at)
		}
	}
	save := func() {
		t.Helper()
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))
	}
	save()

	r := call(t, command(root, "", "git commit -m done"), noEnv)
	if !denied(r) {
		t.Fatal("a high-risk story was committed with nobody asked")
	}
	if reason := r.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(reason, "sdlc escalate") {
		t.Errorf("the refusal does not say how to ask: %q", reason)
	}

	record.Escalate("pre_commit_approval", "ready?", tree, at)
	record.Decide(true, "", tree, at)
	save()
	if r := call(t, command(root, "", "git commit -m done"), noEnv); denied(r) {
		t.Fatalf("the work a person approved was refused: %s", r.HookSpecificOutput.PermissionDecisionReason)
	}

	write(t, root, "user_stories.json",
		`{"stories":[{"id":"B-2","title":"Someone else's","status":"in_progress"}]}`+"\n")
	if !denied(call(t, command(root, "", "git commit -m done"), noEnv)) {
		t.Error("a story whose risk tier cannot be read went through to the commit")
	}
}

// A record that is missing or will not parse is not a story with nothing to
// stand in front of. `sdlc start` always writes one, so either is damage, and
// reading it as "ready" made `rm` on the record the way to a commit no gate had
// passed.
func TestACommitWaitsForARecordThatReads(t *testing.T) {
	for name, spoil := range map[string]func(t *testing.T, root string){
		"missing": func(*testing.T, string) {},
		"corrupt": func(t *testing.T, root string) {
			write(t, root, ".sdlc/stories/A-1/gate-record.json", "{not json")
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := loopProject(t)
			spoil(t, root)
			r := call(t, command(root, "", "git commit -m x"), noEnv)
			if !denied(r) {
				t.Fatal("a commit went through with no readable record of the gates")
			}
			// Blocking work the loop cannot explain is how a tool teaches
			// people to switch it off, so the refusal has to name the file.
			if !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "gate-record.json") {
				t.Errorf("the refusal does not say which file is wrong: %q",
					r.HookSpecificOutput.PermissionDecisionReason)
			}
		})
	}
}
