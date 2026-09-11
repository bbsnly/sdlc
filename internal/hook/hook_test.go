package hook

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
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
	Continue           bool `json:"continue"`
	HookSpecificOutput struct {
		HookEventName            string `json:"hookEventName"`
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func call(t *testing.T, stdin string, getenv func(string) string) reply {
	t.Helper()
	var out bytes.Buffer
	if code := Run([]string{"PreToolUse"}, strings.NewReader(stdin), &out, getenv); code != 0 {
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
			if code := Run(tc.args, strings.NewReader(tc.stdin), &out, noEnv); code != 0 {
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

func TestOversizedStdinDoesNotHangOrGrowUnbounded(t *testing.T) {
	var out bytes.Buffer
	huge := strings.Repeat("x", 4<<20)
	if code := Run([]string{"PreToolUse"}, strings.NewReader(huge), &out, noEnv); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() > 200 {
		t.Errorf("output should stay small, got %d bytes", out.Len())
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
}

func TestNothingIsEnforcedWhileNoStoryIsBeingWorkedOn(t *testing.T) {
	root := loopProject(t)
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	if denied(call(t, event(root, "Write", "", "internal/x.go"), noEnv)) {
		t.Error("a write was refused with no iteration running")
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

func TestTheMainConversationIsToldToDelegate(t *testing.T) {
	root := loopProject(t)
	r := call(t, event(root, "Edit", "", "internal/billing/invoice.go"), noEnv)

	if !denied(r) {
		t.Fatal("the main conversation edited code during an iteration")
	}
	if !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "sdlc stop") {
		t.Errorf("the denial does not offer the way out: %q",
			r.HookSpecificOutput.PermissionDecisionReason)
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

	if !denied(call(t, event(root, "Write", "", ".sdlc/state/active"), noEnv)) {
		t.Error("loop state became writable because the configuration was broken")
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

// A record that cannot be read is not a reason to stand in front of a commit.
// Blocking work the loop cannot explain is how a tool teaches people to switch
// it off.
func TestAnUnreadableRecordDoesNotBlockACommit(t *testing.T) {
	root := loopProject(t)
	write(t, root, ".sdlc/stories/A-1/gate-record.json", "{not json")

	if denied(call(t, command(root, "", "git commit -m x"), noEnv)) {
		t.Error("a broken record blocked a commit")
	}
}
