// US-017, the coverage command the loop tells an agent to run and the hook it
// runs under: the command `sdlc init` writes is one the hook permits (AC-1),
// running it leaves nothing for the commit gate to refuse (AC-2, AC-4), and
// `sdlc doctor` says so when a configured command is one the hook would refuse
// (AC-3). The built binary, the shipped launcher and a real repository; the
// unit tests in internal/scaffold and internal/cli hold the same behaviour
// without a build.

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
	"strings"
	"testing"
)

// commandOf is commands.<key> of dir's configuration.
func commandOf(t *testing.T, dir, key string) string {
	t.Helper()
	var cfg struct {
		Commands map[string]string `json:"commands"`
	}
	readJSON(t, filepath.Join(dir, ".sdlc", "config.json"), &cfg)
	return cfg.Commands[key]
}

// printed runs the tool in dir and returns what it printed on standard output
// and its exit code, without failing the test on a non-zero exit.
func printed(t *testing.T, binary, dir string, args ...string) (stdout string, code int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_SESSION_ID="+claudeSession)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if exit, ok := errors.AsType[*exec.ExitError](err); ok {
		return out.String(), exit.ExitCode()
	}
	if err != nil {
		t.Fatalf("sdlc %s: %v", strings.Join(args, " "), err)
	}
	return out.String(), 0
}

// ruleIn is the rule the hook names in a denial: the word in brackets at the
// end of its reason.
var ruleIn = regexp.MustCompile(`\[([a-z-]+)\]\s*$`)

// AC-1: the command exactly as the configuration `sdlc init` wrote holds it,
// put to the hook while a story is being worked on. It passes today and is a
// guard for the command that replaces it.
func TestAC1TheHookAllowsTheCoverageCommandInitWrote(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX launcher does not run here; internal/plugin holds sdlc-hook.cmd to the same answers")
	}
	binary := build(t)
	root := project(t, binary)
	runTool(t, binary, root, "start")

	command := commandOf(t, root, "coverage")
	if command == "" {
		t.Fatal("init wrote no coverage command for a Go project")
	}
	for _, agent := range []string{"", "sdlc:implementer", "sdlc:verifier"} {
		got := parse(t, launcher(t, binary, root, shell(root, agent, command)))
		if got.HookSpecificOutput.PermissionDecision == "deny" {
			t.Errorf("the hook refuses commands.coverage as init wrote it (agent %q): %s",
				agent, got.HookSpecificOutput.PermissionDecisionReason)
		}
	}
}

// AC-3: a configured command the hook refuses is a problem in `sdlc doctor`,
// with the command's key, the rule the hook names for it, and a fix; and it is
// the same command and the same rule the hook gives an agent that runs it.
func TestAC3SdlcDoctorReportsAConfiguredCommandTheHookWouldRefuse(t *testing.T) {
	binary := build(t)
	root := project(t, binary)

	// Before: what init wrote is nothing the hook objects to.
	before, _ := printed(t, binary, root, "doctor")
	if strings.Contains(before, "-through-the-tool") {
		t.Errorf("doctor says the hook refuses a command init wrote:\n%s", before)
	}

	const refused = "rm -f .sdlc/state/cover.out"
	setConfig(t, root, "commands", "tidy_up", refused)

	out, code := printed(t, binary, root, "doctor")
	if code != 1 {
		t.Errorf("sdlc doctor exited %d with a command the hook refuses configured, want 1:\n%s", code, out)
	}
	for _, want := range []string{"tidy_up", "loop-state-through-the-tool", "fix:"} {
		if !strings.Contains(out, want) {
			t.Errorf("sdlc doctor does not say %q:\n%s", want, out)
		}
	}

	raw, _ := printed(t, binary, root, "doctor", "--json")
	var payload struct {
		OK     bool `json:"ok"`
		Checks []struct {
			Name   string `json:"name"`
			State  string `json:"state"`
			Detail string `json:"detail"`
			Fix    string `json:"fix"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("sdlc doctor --json is not JSON: %v\n%s", err, raw)
	}
	var found bool
	for _, c := range payload.Checks {
		if c.State == "problem" && strings.Contains(c.Detail+c.Fix, "loop-state-through-the-tool") &&
			strings.Contains(c.Detail+c.Fix, "tidy_up") && strings.TrimSpace(c.Fix) != "" {
			found = true
		}
	}
	if payload.OK || !found {
		t.Errorf("sdlc doctor --json carries no problem naming tidy_up and its rule, with a fix: %s", raw)
	}

	if runtime.GOOS == "windows" {
		t.Log("the POSIX launcher does not run here, so the hook is not asked; internal/plugin holds sdlc-hook.cmd to the same answers")
		return
	}
	runTool(t, binary, root, "start")
	got := parse(t, launcher(t, binary, root, shell(root, "", refused))).HookSpecificOutput
	if got.PermissionDecision != "deny" {
		t.Fatalf("the hook allows %q, which doctor reports as refused", refused)
	}
	m := ruleIn.FindStringSubmatch(got.PermissionDecisionReason)
	if m == nil {
		t.Fatalf("the hook's refusal names no rule: %q", got.PermissionDecisionReason)
	}
	if !strings.Contains(out, m[1]) {
		t.Errorf("the hook refuses %q under %s, which doctor does not name:\n%s", refused, m[1], out)
	}
}

// AC-4 (and AC-2): a story's commit has been made, the coverage command is run
// as an agent runs it, and the commit gate still passes. Run before the fix,
// the Go command left .sdlc/state/cover.out, which the gate refused as
// uncommitted work (SDLC-E0032).
func TestAC4RunningTheCoverageCommandAfterTheCommitDoesNotStopTheCommitGate(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to run the coverage command with; sdlc runs it through Git's sh on Windows")
	}
	binary := build(t)
	root := project(t, binary)
	for name, body := range map[string]string{
		"main.go":           "package main\n\nfunc main() {}\n",
		"calc/calc.go":      "package calc\n\nfunc Used() int { return 1 }\n\nfunc Unused() int { return 2 }\n",
		"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestUsed(t *testing.T) {\n\tif Used() != 1 {\n\t\tt.Fatal(\"Used\")\n\t}\n}\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git(t, root, "add", "-A")
	git(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "a package to cover")

	runTool(t, binary, root, "start")
	walk(t, binary, root)
	git(t, root, "add", "-A")
	git(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "US-001")

	// The measurement an agent takes before it records the commit: the command
	// as the configuration holds it, in the project, with a temp directory of
	// the test's own.
	command := commandOf(t, root, "coverage")
	run := exec.CommandContext(t.Context(), sh, "-c", command)
	run.Dir = root
	tmp := t.TempDir()
	run.Env = append(os.Environ(), "TMPDIR="+tmp, "TMP="+tmp, "TEMP="+tmp)
	var stdout, stderr bytes.Buffer
	run.Stdout, run.Stderr = &stdout, &stderr
	if err := run.Run(); err != nil {
		t.Fatalf("%q failed: %v\n%s", command, err, stderr.String())
	}
	if all := regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`).FindAllString(stdout.String(), -1); len(all) == 0 || all[len(all)-1] != "50.0" {
		t.Errorf("the last number on stdout is not 50.0, one statement of two: %q", stdout.String())
	}

	if status := gitOut(t, root, "status", "--porcelain"); strings.TrimSpace(status) != "" {
		t.Errorf("the coverage command left uncommitted work:\n%s", status)
	}
	out, err := tool(t, binary, root, "gate", "commit", "pass", "--note", "on trunk")
	if err != nil || strings.Contains(out, "SDLC-E0032") {
		t.Errorf("the commit gate refuses after the coverage command ran: %v\n%s", err, out)
	}
}
