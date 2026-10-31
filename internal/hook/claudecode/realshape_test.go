package claudecode

// US-112 AC-3, G10 (design review round 1, red team): a transcript Claude
// Code writes holds more than the four counts. Its usage block carries
// strings (service_tier, inference_geo) and objects (cache_creation,
// server_tool_use), and its lines include kinds the reader has no use for. A
// reader that decodes the usage block strictly, or stops at a line of a kind
// it does not know, reads no real transcript at all. The shape here is the
// one the red team saw in this project's transcripts, with the numbers
// masked; only the four counts must be numbers.

import (
	"context"
	"encoding/json"
	"maps"
	"testing"
)

// realUsage is a usage block as Claude Code writes it.
func realUsage(input, cacheRead, cacheWritten, output int64) map[string]any {
	u := usage(input, cacheRead, cacheWritten, output)
	u["service_tier"] = "standard"
	u["inference_geo"] = "eu"
	u["cache_creation"] = map[string]any{"ephemeral_5m_input_tokens": cacheWritten, "ephemeral_1h_input_tokens": 0}
	u["server_tool_use"] = map[string]any{"web_search_requests": 0, "web_fetch_requests": 0}
	return u
}

// line is one transcript line of any kind, with the fields Claude Code puts
// on every line.
func line(fields map[string]any) string {
	base := map[string]any{
		"parentUuid": "p", "isSidechain": true, "userType": "external", "cwd": "/Users/dev/MARKER",
		"sessionId": "s", "version": "2.1.309", "gitBranch": "story/US-1", "uuid": "u",
		"timestamp": "2026-10-28T10:00:00.000Z",
	}
	maps.Copy(base, fields)
	raw, err := json.Marshal(base)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func TestAC3ATranscriptAsClaudeCodeWritesItIsRead(t *testing.T) {
	t.Parallel()
	message := func(id string, u map[string]any, content ...map[string]any) map[string]any {
		return map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": opus, "usage": u, "content": content,
			"stop_reason": "tool_use", "stop_sequence": nil, "container": nil,
		}
	}
	path := transcript(t,
		line(map[string]any{"type": "summary", "summary": "MARKER", "leafUuid": "l"}),
		line(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": "MARKER-prompt"}}),
		line(map[string]any{"type": "assistant", "requestId": "req_1", "message": message("msg_1", realUsage(10, 100, 5, 7),
			map[string]any{"type": "thinking", "thinking": "MARKER", "signature": "sig"})}),
		line(map[string]any{"type": "assistant", "requestId": "req_1", "message": message("msg_1", realUsage(10, 100, 5, 7),
			map[string]any{"type": "tool_use", "id": "tu_1", "name": "Read", "input": map[string]any{"file_path": "/Users/dev/MARKER"}})}),
		line(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": []map[string]any{
			{"type": "tool_result", "tool_use_id": "tu_1", "content": "MARKER"},
		}}, "toolUseResult": map[string]any{"stdout": "MARKER"}}),
		line(map[string]any{"type": "system", "subtype": "informational", "content": "MARKER", "level": "info"}),
		line(map[string]any{"type": "attachment", "attachment": map[string]any{"type": "MARKER"}}),
		line(map[string]any{"type": "file-history-snapshot", "messageId": "m", "snapshot": map[string]any{"trackedFileBackups": map[string]any{}}}),
		line(map[string]any{"type": "assistant", "requestId": "req_2", "message": message("msg_2", realUsage(3, 300, 40, 11),
			map[string]any{"type": "tool_use", "id": "tu_2", "name": "Bash", "input": map[string]any{"command": "MARKER"}})}),
	)
	u, _, err := ReadTranscript(context.Background(), path, 0)
	if err != nil {
		t.Fatalf("AC-3 a transcript as Claude Code writes it was not read: %v", err)
	}
	if u.Responses != 2 {
		t.Errorf("AC-3 responses = %d, want 2", u.Responses)
	}
	if got := u.Models[opus]; got.Input != 13 || got.CacheRead != 400 || got.CacheWritten != 45 || got.Output != 18 {
		t.Errorf("AC-3 tokens = %+v, want input 13, cache read 400, cache written 45, output 18", got)
	}
	if u.PeakContext != 343 {
		t.Errorf("AC-3 peak context = %d, want 343", u.PeakContext)
	}
	if u.Tools["Read"] != 1 || u.Tools["Bash"] != 1 || len(u.Tools) != 2 {
		t.Errorf("AC-3 tool calls = %v, want Read 1 and Bash 1", u.Tools)
	}
}
