package hook_test

// US-112 AC-12: payloads and a transcript whose messages, commands, paths and
// errors carry a marker, and the marker nowhere under .sdlc/. It runs the hook
// as cmd/sdlc builds it, with Claude Code's transcript reader, so it is in a
// package of its own: the host's package may import the hook's. The records
// are looked for first: with recording off, a search for the marker would find
// nothing too.

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/hook"
	"github.com/bbsnly/sdlc/internal/hook/claudecode"
	"github.com/bbsnly/sdlc/internal/model"
)

const markerSession = "session-a"

func markerWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func markerJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAC12NoMarkerFromAPayloadATranscriptAPathOrTheEnvironmentReachesTheLoopsFiles(t *testing.T) {
	t.Parallel()
	const marker = "QZXMARKERQZX"
	base := filepath.Join(t.TempDir(), marker+"-dir")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(base); err == nil {
		base = resolved
	}
	root := filepath.Join(base, "project")
	markerWrite(t, root, ".sdlc/config.json", `{"version":1}`)
	markerWrite(t, root, ".sdlc/state/active", "A-1\n")
	markerWrite(t, root, ".sdlc/state/session", markerSession+"\n")
	markerWrite(t, root, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress"}]}`+"\n")
	record := model.NewRecord("A-1", time.Now())
	record.SetGate(model.GateDoR, model.GatePass, "", time.Now())
	markerWrite(t, root, ".sdlc/stories/A-1/gate-record.json", markerJSON(t, record))

	transcript := func(id, tool string) string {
		return markerJSON(t, map[string]any{
			"type": "assistant", "timestamp": "2026-10-28T10:00:00.000Z", "cwd": root,
			"message": map[string]any{
				"id": id, "model": "model-" + id, "role": "assistant",
				"usage": map[string]any{"input_tokens": 1, "cache_read_input_tokens": 2, "cache_creation_input_tokens": 3, "output_tokens": 4},
				"content": []map[string]any{
					{"type": "text", "text": marker + " response"},
					{"type": "tool_use", "id": "tu-" + id, "name": tool, "input": map[string]any{"command": marker, "file_path": root}},
				},
			},
		}) + "\n" + markerJSON(t, map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": marker + " prompt"}}) + "\n"
	}
	agentTranscript := filepath.Join(root, "transcripts", "agent-"+marker+".jsonl")
	mainTranscript := filepath.Join(root, "transcripts", "main-"+marker+".jsonl")
	markerWrite(t, root, "transcripts/agent-"+marker+".jsonl", transcript("a", "Read"))
	markerWrite(t, root, "transcripts/main-"+marker+".jsonl", transcript("m", "Agent"))
	vars := map[string]string{"USER": marker, "LANG": marker, "SHELL": "/bin/" + marker}
	getenv := func(k string) string { return vars[k] }

	send := func(event string, fields map[string]any) {
		t.Helper()
		p := map[string]any{"hook_event_name": event, "session_id": markerSession, "cwd": root, "transcript_path": mainTranscript}
		maps.Copy(p, fields)
		var out bytes.Buffer
		if code := hook.RunHosted([]string{event}, strings.NewReader(markerJSON(t, p)), &out, io.Discard, getenv,
			hook.Host{Transcripts: claudecode.ReadTranscript}); code != 0 {
			t.Fatalf("%s exited %d", event, code)
		}
	}
	agent := "a1"
	send("SubagentStart", map[string]any{"agent_type": "sdlc:verifier", "agent_id": agent})
	send("PostToolUse", map[string]any{"agent_type": "sdlc:verifier", "agent_id": agent, "tool_name": "SubagentHandback",
		"tool_input": map[string]any{"message": `{"status":"done","detail":"` + marker + `"}`}})
	send("PreToolUse", map[string]any{"agent_type": "sdlc:verifier", "tool_name": "Write",
		"tool_input": map[string]any{"file_path": filepath.Join(root, ".sdlc", "stories", "A-1", "gate-record.json"), "content": marker}})
	send("PreToolUse", map[string]any{"agent_type": "sdlc:verifier", "tool_name": "Bash",
		"tool_input": map[string]any{"command": "rm .sdlc/state/tests.lock " + marker, "description": marker}})
	send("PostToolUse", map[string]any{"agent_type": "sdlc:verifier", "tool_name": "Bash", "duration_ms": 3,
		"tool_input":    map[string]any{"command": marker + "=1 go test ./" + marker + "/...", "description": marker},
		"tool_response": map[string]any{"stdout": marker, "stderr": marker}})
	send("PostToolUseFailure", map[string]any{"agent_type": "sdlc:verifier", "tool_name": "Bash", "duration_ms": 3,
		"tool_input": map[string]any{"command": "cd " + root + " && go test -run " + marker}, "error": "Exit code 1\n" + marker})
	send("Notification", map[string]any{"notification_type": "permission_prompt", "message": marker, "title": marker})
	send("StopFailure", map[string]any{"error": "server_error", "error_details": marker, "last_assistant_message": marker})
	send("PostCompact", map[string]any{"trigger": "manual", "compact_summary": marker, "custom_instructions": marker})
	send("SubagentStop", map[string]any{"agent_type": "sdlc:verifier", "agent_id": agent,
		"agent_transcript_path": agentTranscript, "last_assistant_message": marker})
	send("Stop", map[string]any{"prompt_id": "p-1", "stop_hook_active": true, "last_assistant_message": marker,
		"background_tasks": []map[string]any{{"id": "b", "type": "local_bash", "command": marker, "description": marker}}})

	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "A-1", "trace.jsonl"))
	if err != nil {
		t.Fatalf("AC-12 nothing was recorded, so finding no marker would prove nothing: %v", err)
	}
	kinds := map[string]int{}
	var run map[string]any
	for line := range strings.SplitSeq(strings.TrimSuffix(string(raw), "\n"), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("a trace line is not JSON: %s", line)
		}
		kind, _ := m["kind"].(string)
		kinds[kind]++
		if kind == "agent_run" {
			run = m
		}
	}
	for _, kind := range []string{"agent_run", "turn", "refusal", "shell", "notification", "api_error", "compaction"} {
		if kinds[kind] == 0 {
			t.Errorf("AC-12 no %s record was written, so finding no marker there would prove nothing", kind)
		}
	}
	if n, _ := run["responses"].(float64); n != 1 {
		t.Errorf("AC-12 the agent's transcript was not read through the host's reader: %v", run)
	}
	if run["status"] != "done" {
		t.Errorf("AC-12 the hand-back's status was not kept: %v", run)
	}
	gate, _ := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "A-1", model.RecordFile))
	if !strings.Contains(string(gate), `"agent_start"`) || !strings.Contains(string(gate), `"agent_end"`) {
		t.Error("AC-12 the agent events were not written to the gate record")
	}
	err = filepath.WalkDir(filepath.Join(root, ".sdlc"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), marker) {
			t.Errorf("AC-12 %s holds the marker:\n%s", path, body)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
