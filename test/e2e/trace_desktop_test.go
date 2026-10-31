package e2e

// US-112, the person's decision of 2026-10-29 (spec_unclear, option A): Claude
// Code 2.1.309 in Claude Desktop sets AI_AGENT in a hook's own environment.
// The built hook, through the launcher, with the markers a real hook carries
// and that AI_AGENT, still records the story's events, on the gate record and
// in the trace; and the same environment still keeps a person's slash command
// from running, as the slash route has always done with AI_AGENT set.

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAC2AC15AHookThatCarriesAIAgentAsClaudeDesktopsDoesStillRecordsInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	markers := append(append([]string{}, realHookMarkers...), "AI_AGENT=claude-code_2-1-309_agent")
	send := func(event string, payload map[string]any) string {
		t.Helper()
		payload["hook_event_name"], payload["session_id"], payload["cwd"] = event, sandboxSession, tree
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		name, args := sb.sdlc, []string{"hook", event}
		if runtime.GOOS != "windows" {
			name, args = filepath.Join(moduleRoot(t), "plugin", "bin", "sdlc-hook"), []string{event}
		}
		cmd := exec.CommandContext(t.Context(), name, args...)
		cmd.Dir = tree
		cmd.Env = append(append(sb.env(t, sandboxSession), "CLAUDE_PROJECT_DIR="+sb.repo), markers...)
		cmd.Stdin = bytes.NewReader(raw)
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err != nil {
			t.Fatalf("the hook did not answer %s: %v", event, err)
		}
		return out.String()
	}
	send("SubagentStart", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "desktop-1"})
	send("SubagentStop", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "desktop-1", "stop_hook_active": false,
		"last_assistant_message": `{"status":"done"}`})
	send("PostToolUse", map[string]any{"tool_name": bashTool, "duration_ms": 1000,
		"tool_input":    map[string]any{"command": "go test ./..."},
		"tool_response": map[string]any{"stdout": "ok", "stderr": "", "interrupted": false}})
	if out := send("PreToolUse", toolCall(sandboxSession, tree, "", bashTool, map[string]any{"command": "sdlc approve US-001"})); !strings.Contains(out, `"deny"`) {
		t.Fatalf("the approval was let through: %q", out)
	}

	var doc struct {
		AgentTypes []struct {
			AgentType string  `json:"agent_type"`
			Runs      float64 `json:"runs"`
		} `json:"agent_types"`
		Tests *struct {
			Count float64 `json:"count"`
		} `json:"tests"`
		Refusals []struct {
			Rule string `json:"rule"`
		} `json:"refusals"`
	}
	out := sb.works(t, tree, "", "", "trace", "US-001", "--json")
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("sdlc trace --json: %v\n%s", err, out)
	}
	runs := 0.0
	for _, a := range doc.AgentTypes {
		if a.AgentType == "sdlc:verifier" {
			runs = a.Runs
		}
	}
	refused := false
	for _, r := range doc.Refusals {
		refused = refused || r.Rule == "approval-is-a-human-decision"
	}
	if runs != 1 || doc.Tests == nil || doc.Tests.Count != 1 || !refused {
		t.Errorf("AC-2 AC-15 with Claude Desktop's AI_AGENT the hook recorded %v verifier runs, tests %+v, the refusal %v:\n%s",
			runs, doc.Tests, refused, out)
	}

	e := sb.expandsInClaudeCode(t, sandboxSession, tree, markers, typedLine(sandboxSession, tree, "sdlc:approve", "US-001"))
	if !e.blocked() || !strings.Contains(e.shown(), "/sdlc:approve did not run") {
		t.Errorf("the slash route ran /sdlc:approve with AI_AGENT set: %q", e.raw)
	}
}
