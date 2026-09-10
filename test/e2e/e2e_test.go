// Package e2e drives the shipped plugin the way Claude Code does: the launcher
// script from plugin/bin, the real binary, a real repository on disk.
//
// Everything else in the test suite checks one piece. This checks that the
// pieces are connected -- which is the failure that unit tests structurally
// cannot see, and the one a user meets first.
package e2e

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// build compiles the binary once for the whole package.
func build(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "sdlc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)

	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", out, "../../cmd/sdlc")
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building sdlc: %v\n%s", err, combined)
	}
	return out
}

// project makes a real Git repository with the tool set up in it.
func project(t *testing.T, binary string) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	git(t, root, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTool(t, binary, root, "init")
	return root
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runTool(t *testing.T, binary, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), binary, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("sdlc %s: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.String()
}

// launcher runs plugin/bin/sdlc-hook exactly as the hooks.json entry does.
func launcher(t *testing.T, binary, root, payload string) string {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "plugin", "bin", "sdlc-hook"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), script, "PreToolUse")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "SDLC_BIN="+binary, "CLAUDE_PROJECT_DIR="+root)
	cmd.Stdin = strings.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the launcher failed: %v\n%s", err, stderr.String())
	}
	return stdout.String()
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
		PermissionDecision       string `json:"permissionDecision"`
		PermissionDecisionReason string `json:"permissionDecisionReason"`
	} `json:"hookSpecificOutput"`
}

func parse(t *testing.T, out string) reply {
	t.Helper()
	var r reply
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("the launcher wrote something that is not JSON: %v\n%q", err, out)
	}
	return r
}

// TestAStoryReachesTheAnalysisGate walks the path a user walks: set the project
// up, start a story, record the gates the shipped skill records.
func TestAStoryReachesTheAnalysisGate(t *testing.T) {
	binary := build(t)
	root := project(t, binary)

	var status struct {
		Next *struct{ Story string } `json:"next"`
	}
	if err := json.Unmarshal([]byte(runTool(t, binary, root, "status", "--json")), &status); err != nil {
		t.Fatal(err)
	}
	if status.Next == nil || status.Next.Story != "US-001" {
		t.Fatalf("a fresh project did not offer the example story: %+v", status.Next)
	}

	runTool(t, binary, root, "start")
	runTool(t, binary, root, "gate", "dor", "pass", "--note", "criteria are testable")
	runTool(t, binary, root, "gate", "analysis", "pass", "--note", "not security sensitive")

	var after struct {
		Active string            `json:"active"`
		Gates  map[string]string `json:"gates"`
	}
	if err := json.Unmarshal([]byte(runTool(t, binary, root, "status", "--json")), &after); err != nil {
		t.Fatal(err)
	}
	if after.Active != "US-001" || after.Gates["dor"] != "pass" || after.Gates["analysis"] != "pass" {
		t.Fatalf("status = %+v", after)
	}

	// The record is a file in the repository, which is what makes it reviewable.
	record := filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json")
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("no gate record on disk: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("the gate record is not valid JSON:\n%s", raw)
	}
	if !strings.Contains(string(raw), "criteria are testable") {
		t.Error("the note did not reach the record")
	}
}

// TestTheShippedHookEnforcesThroughItsOwnLauncher is the connection the unit
// tests cannot see: hooks.json names a script, the script finds the binary, the
// binary applies the policy, and Claude Code gets an answer it understands.
func TestTheShippedHookEnforcesThroughItsOwnLauncher(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX launcher does not run here; plugin/bin/sdlc-hook.cmd is its counterpart")
	}
	binary := build(t)
	root := project(t, binary)
	runTool(t, binary, root, "start")

	t.Run("the analysis agent may write its analysis", func(t *testing.T) {
		got := parse(t, launcher(t, binary, root,
			event(root, "Write", "sdlc:researcher", ".sdlc/stories/US-001/ANALYSIS.md")))
		if got.HookSpecificOutput.PermissionDecision == "deny" {
			t.Errorf("refused: %s", got.HookSpecificOutput.PermissionDecisionReason)
		}
	})

	t.Run("the analysis agent may not write code", func(t *testing.T) {
		got := parse(t, launcher(t, binary, root,
			event(root, "Write", "sdlc:researcher", "internal/billing/invoice.go")))
		if got.HookSpecificOutput.PermissionDecision != "deny" {
			t.Fatal("the analysis agent wrote production code")
		}
		reason := got.HookSpecificOutput.PermissionDecisionReason
		if !strings.Contains(reason, "Instead:") {
			t.Errorf("the denial offers no route: %q", reason)
		}
	})

	t.Run("nobody may rewrite the loop's own record", func(t *testing.T) {
		got := parse(t, launcher(t, binary, root,
			event(root, "Edit", "sdlc:researcher", ".sdlc/state/active")))
		if got.HookSpecificOutput.PermissionDecision != "deny" {
			t.Fatal("loop state was writable during an iteration")
		}
	})

	t.Run("a project with no iteration running is untouched", func(t *testing.T) {
		runTool(t, binary, root, "stop")
		got := parse(t, launcher(t, binary, root,
			event(root, "Write", "", "internal/billing/invoice.go")))
		if got.HookSpecificOutput.PermissionDecision == "deny" {
			t.Error("a write was refused with no story being worked on")
		}
	})
}

// A missing binary must not block the session, and must say why.
func TestTheLauncherWithoutABinaryAllowsAndExplains(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX launcher does not run here")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "plugin", "bin", "sdlc-hook"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	cmd := exec.CommandContext(t.Context(), script, "PreToolUse")
	cmd.Dir = root
	// An empty PATH and no SDLC_BIN: there is no binary to find.
	cmd.Env = []string{"PATH=" + filepath.Join(root, "nothing-here")}
	cmd.Stdin = strings.NewReader(event(root, "Write", "", "x.go"))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("the launcher exited non-zero with no binary: %v\n%s", err, stderr.String())
	}
	if got := parse(t, stdout.String()); !got.Continue {
		t.Error("a missing binary blocked the tool call")
	}
	for _, want := range []string{"not found", "why", "fix"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr is missing %q:\n%s", want, stderr.String())
		}
	}
}

// TestTheAnalysisCannotBeWrittenByTheConversationItself covers the hole a live
// run found: the main conversation wrote Gate 2's analysis itself and every
// rule allowed it, because the orchestrator may write under .sdlc/ and the
// analysis lives under .sdlc/. Every gate after that would have been reviewing
// work shaped by the reasoning it was supposed to be independent of.
func TestTheAnalysisCannotBeWrittenByTheConversationItself(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX launcher does not run here")
	}
	binary := build(t)
	root := project(t, binary)
	runTool(t, binary, root, "start")

	analysis := ".sdlc/stories/US-001/ANALYSIS.md"

	got := parse(t, launcher(t, binary, root, event(root, "Write", "", analysis)))
	if got.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatal("the conversation wrote the analysis itself")
	}
	if !strings.Contains(got.HookSpecificOutput.PermissionDecisionReason, "fresh context") {
		t.Errorf("the denial does not say why it matters: %q",
			got.HookSpecificOutput.PermissionDecisionReason)
	}

	allowed := parse(t, launcher(t, binary, root, event(root, "Write", "sdlc:researcher", analysis)))
	if allowed.HookSpecificOutput.PermissionDecision == "deny" {
		t.Errorf("the analysis agent could not write its own analysis: %s",
			allowed.HookSpecificOutput.PermissionDecisionReason)
	}
}
