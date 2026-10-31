package e2e

// US-112 AC-24: the built binary, through the plugin's launcher, in a real git
// repository with the story in its own worktree, is handed the events Claude
// Code sends: a subagent's start and stop with a transcript the test wrote, a
// test run that passed and one that failed, a refused call, a stop, and the
// commit gate. Every record is read back with `sdlc trace --json`, and a write
// to the trace is refused.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAC24TheLoopRecordsAStoryAndSdlcTraceReadsItBackInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")

	answered := func(event string, payload map[string]any) {
		t.Helper()
		payload["hook_event_name"] = event
		payload["session_id"] = sandboxSession
		payload["cwd"] = tree
		stdout, stderr, code := sb.hook(t, event, payload)
		if code != 0 || stdout != carryOn {
			t.Fatalf("AC-24 %s was answered with exit %d\nstdout %q\nstderr %q", event, code, stdout, stderr)
		}
	}

	transcripts := sb.own(t, "transcripts")
	line := func(id, model, tool string) string {
		raw, err := json.Marshal(map[string]any{
			"type": "assistant", "timestamp": "2026-10-28T10:00:00.000Z",
			"message": map[string]any{
				"id": id, "model": model, "role": "assistant",
				"usage":   map[string]any{"input_tokens": 10, "cache_read_input_tokens": 30, "cache_creation_input_tokens": 5, "output_tokens": 7},
				"content": []map[string]any{{"type": "tool_use", "id": "tu-" + id, "name": tool, "input": map[string]any{}}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw) + "\n"
	}
	agentTranscript := filepath.Join(transcripts, "agent.jsonl")
	if err := os.WriteFile(agentTranscript, []byte(line("m1", "e2e-model", "Read")+line("m1", "e2e-model", "Grep")), 0o600); err != nil {
		t.Fatal(err)
	}
	mainTranscript := filepath.Join(transcripts, "main.jsonl")
	if err := os.WriteFile(mainTranscript, []byte(line("m9", "e2e-main", "Agent")), 0o600); err != nil {
		t.Fatal(err)
	}

	answered("SubagentStart", map[string]any{"agent_type": "sdlc:researcher", "agent_id": "e2e-1"})
	answered("SubagentStop", map[string]any{
		"agent_type": "sdlc:researcher", "agent_id": "e2e-1", "stop_hook_active": false,
		"agent_transcript_path": agentTranscript, "last_assistant_message": `{"status":"done"}`,
		"effort": map[string]any{"level": "high"},
	})
	answered("PostToolUse", map[string]any{
		"tool_name": bashTool, "duration_ms": 2000,
		"tool_input":    map[string]any{"command": "cd -- '" + tree + "' && go test ./..."},
		"tool_response": map[string]any{"stdout": "ok", "stderr": "", "interrupted": false},
	})
	answered("PostToolUseFailure", map[string]any{
		"tool_name": bashTool, "duration_ms": 1500,
		"tool_input": map[string]any{"command": "go test ./..."}, "error": "Exit code 1\nFAIL",
	})
	sb.refuses(t, sandboxSession, tree, map[string]any{"command": "sdlc approve US-001"}, "approval-is-a-human-decision")
	stdout, stderr, code := sb.hook(t, "Stop", map[string]any{
		"hook_event_name": "Stop", "session_id": sandboxSession, "cwd": tree, "stop_hook_active": true,
		"prompt_id": "e2e-prompt", "transcript_path": mainTranscript,
	})
	if code != 0 || stdout == "" {
		t.Fatalf("AC-24 Stop exited %d\nstdout %q\nstderr %q", code, stdout, stderr)
	}

	// The commit gate, and every gate on the way to it, for real.
	sb.worksEveryGate(t)

	var doc struct {
		Traced       bool    `json:"traced"`
		SkippedLines float64 `json:"skipped_lines"`
		Truncated    bool    `json:"truncated"`
		AgentTypes   []struct {
			AgentType string                        `json:"agent_type"`
			Runs      float64                       `json:"runs"`
			Responses float64                       `json:"responses"`
			Models    map[string]map[string]float64 `json:"models"`
			Tools     map[string]float64            `json:"tools"`
			Done      float64                       `json:"done"`
		} `json:"agent_types"`
		Main []struct {
			Gate   string                        `json:"gate"`
			Models map[string]map[string]float64 `json:"models"`
		} `json:"main"`
		Tests *struct {
			Count    float64 `json:"count"`
			Passes   float64 `json:"passes"`
			Failures float64 `json:"failures"`
		} `json:"tests"`
		Refusals []struct {
			Rule string `json:"rule"`
		} `json:"refusals"`
		Diff *struct {
			Files float64 `json:"files"`
			Added float64 `json:"added"`
		} `json:"diff"`
	}
	out := sb.works(t, tree, "", "", "trace", "US-001", "--json")
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("AC-24 sdlc trace --json printed something that is not one JSON document: %v\n%s", err, out)
	}
	if !doc.Traced || doc.SkippedLines != 0 || doc.Truncated {
		t.Errorf("AC-24 traced %v, skipped %v, truncated %v", doc.Traced, doc.SkippedLines, doc.Truncated)
	}
	researcher := false
	for _, a := range doc.AgentTypes {
		if a.AgentType == "sdlc:researcher" {
			researcher = true
			if a.Runs != 1 || a.Responses != 1 || a.Models["e2e-model"]["cache_read"] != 30 || a.Tools["Read"] != 1 || a.Tools["Grep"] != 1 || a.Done != 1 {
				t.Errorf("AC-24 the researcher's run read back as %+v", a)
			}
		}
	}
	if !researcher {
		t.Errorf("AC-24 no researcher run was read back: %s", out)
	}
	mainTokens := false
	for _, m := range doc.Main {
		if m.Models["e2e-main"]["input"] == 10 {
			mainTokens = true
		}
	}
	if !mainTokens {
		t.Errorf("AC-24 the stop's turn was not read back: %s", out)
	}
	if doc.Tests == nil || doc.Tests.Count != 2 || doc.Tests.Passes != 1 || doc.Tests.Failures != 1 {
		t.Errorf("AC-24 the test runs read back as %+v", doc.Tests)
	}
	refused := false
	for _, r := range doc.Refusals {
		refused = refused || r.Rule == "approval-is-a-human-decision"
	}
	if !refused {
		t.Errorf("AC-24 the refusal was not read back: %s", out)
	}
	if doc.Diff == nil || doc.Diff.Files < 2 || doc.Diff.Added < 1 {
		t.Errorf("AC-24 the commit gate's diff read back as %+v", doc.Diff)
	}

	// AC-17 and AC-24: the trace is written by sdlc alone.
	trace := filepath.Join(main, ".sdlc", "stories", "US-001", "trace.jsonl")
	if _, err := os.Stat(trace); err != nil {
		t.Fatalf("AC-24 the trace is not in the main checkout's story directory: %v", err)
	}
	sb.refuses(t, sandboxSession, tree, map[string]any{"command": "echo '{}' >> '" + filepath.ToSlash(trace) + "'"}, "trace-is-written-by-the-tool")
	stdout, _, _ = sb.hook(t, "PreToolUse", toolCall(sandboxSession, tree, "verifier", "Write", map[string]any{"file_path": trace, "content": "{}"}))
	var r refusal
	oneLine(t, stdout, &r)
	if r.HookSpecificOutput.PermissionDecision != "deny" {
		t.Errorf("AC-24 a Write to the trace was let through: %q", stdout)
	}
}

// US-112 B1 (design review round 1, security): a model's Bash that runs the
// hook by hand on an event it records, or takes AI_AGENT away first, is
// refused by the built hook, from the session holding the story and from
// another, and nothing reaches the gate record; the hook run on PreToolUse is
// still let through.
func TestAC2AC15AModelsBashThatForgesARecordedEventIsRefusedInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	record := filepath.Join(main, ".sdlc", "stories", "US-001", "gate-record.json")
	before, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	forged := filepath.Join(sb.own(t, "forged"), "p.json")
	raw, err := json.Marshal(map[string]any{"hook_event_name": "SubagentStart", "session_id": sandboxSession, "cwd": tree,
		"agent_type": "sdlc:verifier", "agent_id": "forged"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(forged, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	from := ` < "` + filepath.ToSlash(forged) + `"`
	for _, session := range []string{sandboxSession, sandboxOther} {
		sb.refuses(t, session, tree, map[string]any{"command": "sdlc hook SubagentStart" + from}, "recorded-event-comes-from-the-host")
		sb.refuses(t, session, tree, map[string]any{"command": "env -u AI_AGENT sdlc hook PreToolUse" + from}, "enforcement-stays-on")
	}
	if after, _ := os.ReadFile(record); string(after) != string(before) {
		t.Error("B1 the refusals left an agent event on the gate record")
	}
	sb.allows(t, tree, "", bashTool, map[string]any{"command": "sdlc hook PreToolUse" + from})
}
