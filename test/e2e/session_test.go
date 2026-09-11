// A session, without a session.
//
// Everything else here drives the tool directly. What nobody was checking is
// the thing a user actually runs: an assistant following the runbook, where
// every command and every write goes past the hook first, and a refusal is a
// refusal rather than a thing the test knows to skip.
//
// Running a real Claude Code session in CI would need credentials in the
// repository and would spend money on every commit. This is the other half of
// that trade: a session that is faked precisely where it has to be -- there is
// no model here, and the work each agent does is a fixture -- and real
// everywhere it matters. The plugin's own launcher decides every tool call,
// the agent each call is attributed to is the one the runbook names, and what
// to do next is read out of `sdlc status --json` rather than known in advance.
//
// What this catches that nothing else does: a runbook step the hook refuses,
// an agent whose write scope does not cover what its gate produces, and a
// resume that picks up at the wrong gate.
package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// session is one run of the assistant against one project.
type session struct {
	t      *testing.T
	binary string
	root   string
}

// do is a tool call: the hook is asked first, and a refusal fails the test
// rather than being worked around, which is the whole point of asking.
func (s *session) do(agent, tool string, input map[string]any) {
	s.t.Helper()
	payload := map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       tool,
		"cwd":             s.root,
		"tool_input":      input,
	}
	if agent != "" {
		payload["agent_type"] = "sdlc:" + agent
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		s.t.Fatal(err)
	}
	r := parse(s.t, launcher(s.t, s.binary, s.root, string(raw)))
	if r.HookSpecificOutput.PermissionDecision == "deny" {
		s.t.Fatalf("the runbook told %s to run %s, and the hook refused it:\n  %s",
			describeAgent(agent), describeInput(tool, input),
			r.HookSpecificOutput.PermissionDecisionReason)
	}
}

// refused is do's opposite: a call the loop is supposed to stop. A session that
// only ever did allowed things would pass against a hook that allowed
// everything.
func (s *session) refused(agent, tool string, input map[string]any) string {
	s.t.Helper()
	payload := map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       tool,
		"cwd":             s.root,
		"tool_input":      input,
	}
	if agent != "" {
		payload["agent_type"] = "sdlc:" + agent
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		s.t.Fatal(err)
	}
	r := parse(s.t, launcher(s.t, s.binary, s.root, string(raw)))
	if r.HookSpecificOutput.PermissionDecision != "deny" {
		s.t.Fatalf("%s was allowed to run %s", describeAgent(agent), describeInput(tool, input))
	}
	return r.HookSpecificOutput.PermissionDecisionReason
}

// run is a shell command the runbook gives: past the hook, then actually run.
func (s *session) run(agent string, args ...string) string {
	s.t.Helper()
	s.do(agent, "Bash", map[string]any{"command": "sdlc " + strings.Join(args, " ")})
	return runTool(s.t, s.binary, s.root, args...)
}

// stores is how a gate's documents arrive: through the tool, on standard input.
func (s *session) stores(agent, kind, body string) string {
	s.t.Helper()
	s.do(agent, "Bash", map[string]any{"command": "sdlc artifact write " + kind})
	return runToolWithInput(s.t, s.binary, s.root, body, "artifact", "write", kind)
}

// writes is a file the runbook expects an agent to write itself.
func (s *session) writes(agent, rel, body string) {
	s.t.Helper()
	s.do(agent, "Write", map[string]any{"file_path": rel})
	path := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

// nextGate is what the runbook reads to know where it is. Reading it rather
// than counting gates is the contract: the loop resumes at the first gate that
// has not passed, and the tool works that out so the session does not have to.
func (s *session) nextGate() string {
	s.t.Helper()
	var status struct {
		Active   string `json:"active"`
		NextGate string `json:"next_gate"`
	}
	if err := json.Unmarshal([]byte(runTool(s.t, s.binary, s.root, "status", "--json")), &status); err != nil {
		s.t.Fatal(err)
	}
	if status.Active == "" {
		return ""
	}
	return status.NextGate
}

func describeAgent(agent string) string {
	if agent == "" {
		return "the conversation"
	}
	return "sdlc:" + agent
}

func describeInput(tool string, input map[string]any) string {
	if c, ok := input["command"]; ok {
		return tool + " `" + c.(string) + "`"
	}
	if p, ok := input["file_path"]; ok {
		return tool + " " + p.(string)
	}
	return tool
}

// workGate does what the runbook says to do at one gate, as whoever it says to
// delegate it to, and records the outcome. The gate it is given comes from
// `sdlc status --json`, so the order is the tool's and not this test's.
func (s *session) workGate(gate model.Gate, story string) {
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
		s.stores("implementer", "plan", "# Plan\n\nOne step.\n")
	case model.GateImplementation:
		s.writes("implementer", "invoice.go", "package main\n\nfunc Invoice() {}\n")
	case model.GateVerification:
		s.stores("verifier", "verification", "# Verification\n\nEvery command green.\n")
	case model.GateCommit:
		git(s.t, s.root, "add", "-A")
		git(s.t, s.root, "-c", "user.email=t@example.com", "-c", "user.name=Test",
			"commit", "--quiet", "-m", story)
	case model.GateRetro:
		s.stores("bookkeeper", "retro", "# Retro\n\nNo deviations.\n")
	}
	for _, r := range model.ReviewersFor(gate) {
		s.do(r.Role, "Bash", map[string]any{
			"command": "sdlc review add " + string(gate) + " " + r.Role + " approve",
		})
		runToolWithInput(s.t, s.binary, s.root, "# "+r.Role+"\n",
			"review", "add", string(gate), r.Role, "approve")
	}
	s.run("", "gate", string(gate), "pass", "--note", "worked by the session")
}

// The loop run the way the documentation describes it: one story per session,
// each session starting from nothing but what the tool says.
func TestASessionWorksAStoryTheWayTheRunbookSaysTo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX launcher does not run here")
	}
	binary := build(t)
	root := project(t, binary)
	s := &session{t: t, binary: binary, root: root}

	s.run("", "start")
	for gate := s.nextGate(); gate != ""; gate = s.nextGate() {
		s.workGate(model.Gate(gate), "US-001")
	}
	s.run("", "cost", "add", "--usd", "2.40")
	s.run("", "stop")

	if got := s.nextGate(); got != "" {
		t.Errorf("the story is not finished: next gate is %q", got)
	}
}

// Resuming is the same loop through a different door. A session that stops
// after every gate has to pick up where the last one left off, and it finds out
// by asking rather than by remembering -- which is what makes an interrupted
// story recoverable at all.
func TestEachGateCanBeWorkedByItsOwnSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX launcher does not run here")
	}
	binary := build(t)
	root := project(t, binary)

	first := &session{t: t, binary: binary, root: root}
	first.run("", "start")
	first.run("", "stop")

	var worked []string
	for range model.Gates {
		s := &session{t: t, binary: binary, root: root}
		s.run("", "start")
		gate := s.nextGate()
		if gate == "" {
			s.run("", "stop")
			break
		}
		worked = append(worked, gate)
		s.workGate(model.Gate(gate), "US-001")
		s.run("", "stop")
	}

	if len(worked) != len(model.Gates) {
		t.Errorf("worked %d gates in %d sessions, want %d: %v",
			len(worked), len(model.Gates), len(model.Gates), worked)
	}
	for i, gate := range worked {
		if i < len(model.Gates) && gate != string(model.Gates[i]) {
			t.Errorf("session %d resumed at %q, want %q", i+1, gate, model.Gates[i])
		}
	}
}

// The refusals, from inside a session. Separation of duties is only real if the
// hook enforces it against the agent the runbook actually names, and each of
// these is a step some session will eventually try.
func TestTheRunbookIsRefusedWhereItShouldBe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX launcher does not run here")
	}
	binary := build(t)
	root := project(t, binary)
	s := &session{t: t, binary: binary, root: root}

	s.run("", "start")
	for gate := s.nextGate(); gate != "" && gate != string(model.GatePlan); gate = s.nextGate() {
		s.workGate(model.Gate(gate), "US-001")
	}

	for _, c := range []struct {
		what  string
		agent string
		tool  string
		input map[string]any
	}{
		{"the implementer editing a frozen test",
			"implementer", "Write", map[string]any{"file_path": "invoice_test.go"}},
		{"the implementer editing a frozen test from a shell",
			"implementer", "Bash", map[string]any{"command": "echo cheat > invoice_test.go"}},
		{"the test author writing production code",
			"sdet", "Write", map[string]any{"file_path": "invoice.go"}},
		{"a reviewer writing anything but its review",
			"architect", "Write", map[string]any{"file_path": "invoice.go"}},
		{"anyone writing the gate record by hand",
			"implementer", "Write", map[string]any{"file_path": ".sdlc/stories/US-001/gate-record.json"}},
		{"anyone turning enforcement off",
			"implementer", "Bash", map[string]any{"command": "SDLC_ENFORCE=0 sdlc gate plan pass"}},
		{"committing before the gates are done",
			"", "Bash", map[string]any{"command": "git commit -m done"}},
		{"the conversation writing a gate's document itself",
			"", "Write", map[string]any{"file_path": ".sdlc/stories/US-001/PLAN.md"}},
	} {
		t.Run(c.what, func(t *testing.T) {
			reason := s.refused(c.agent, c.tool, c.input)
			// A refusal that does not say what to do instead is a refusal the
			// session argues with.
			if !strings.Contains(reason, "Instead:") {
				t.Errorf("the refusal offers no route: %q", reason)
			}
		})
	}
}
