package trace

// US-112, design review round 2 (red team, V8): one list of keys for every
// kind passes TestAC12TheWriterRefusesAnyKeyNotOnItsListNotOnlyTheOnesNamed,
// whose keys are on no kind's list. A shell record holding `rule`, or a
// refusal holding `program`, is a field the story never named for that kind,
// so the list is each kind's own.

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// kindFields is each kind's fields as TestAC12EveryFieldTheStoryNamesIsOnTheWritersList
// writes them, with a value of the kind each takes.
func kindFields() map[string]Record {
	tokens := map[string]any{"input": 10, "cache_read": 20, "cache_written": 30, "output": 40}
	return map[string]Record{
		"agent_run": {
			"agent_type": "sdlc:verifier", "agent_id": "a1",
			"start": "2026-10-28T10:00:00.000Z", "end": "2026-10-28T10:05:00.000Z",
			"effort": "high", "asked_model": "opus", "asked_effort": "high", "permission_mode": "acceptEdits",
			"models": map[string]any{"claude-opus-x": tokens}, "responses": 3, "peak_context": 60,
			"tools": map[string]any{"Read": 2}, "status": "done",
		},
		"turn": {
			"prompt_id": "p-1", "effort": "medium", "models": map[string]any{"claude-sonnet-x": tokens},
			"responses": 2, "peak_context": 60, "tools": map[string]any{"Agent": 1}, "background_tasks": map[string]any{"subagent": 1},
		},
		"refusal":      {"rule": "gate-record-is-written-by-the-tool", "agent_type": "sdlc:implementer", "tool": "Write", "code": "SDLC-E0081"},
		"shell":        {"agent_type": "sdlc:sdet", "duration_ms": 1234, "background": true, "class": "test", "program": "go", "outcome": "fail", "exit_code": 1},
		"diff":         {"files": 3, "added": 40, "removed": 2, "test_files": 1, "test_added": 20, "test_removed": 0},
		"notification": {"agent_type": "sdlc:sdet", "notification_type": "permission_prompt"},
		"api_error":    {"agent_type": "sdlc:sdet", "error": "rate_limit"},
		"compaction":   {"agent_type": "sdlc:sdet", "trigger": "auto"},
		"truncated":    {"size": 4194000},
	}
}

// AC-12, V8: each kind takes its own fields and refuses every other kind's.
func TestAC12EachKindRefusesAFieldThatOnlyAnotherKindHas(t *testing.T) {
	t.Parallel()
	fields := kindFields()
	kinds := slices.Sorted(maps.Keys(fields))
	for _, kind := range kinds {
		own := fields[kind]
		foreign := Record{}
		for _, other := range kinds {
			for key, value := range fields[other] {
				if _, ok := own[key]; !ok {
					foreign[key] = value
				}
			}
		}
		for _, key := range slices.Sorted(maps.Keys(foreign)) {
			t.Run("AC-12 "+kind+" with "+key, func(t *testing.T) {
				t.Parallel()
				dir := storyDir(t)
				if err := Append(dir, Record{"kind": kind, key: foreign[key]}); err == nil {
					t.Errorf("a %s record holding %q, a field of other kinds only, was written", kind, key)
				}
				if raw, err := os.ReadFile(filepath.Join(dir, traceName)); err == nil {
					t.Errorf("a refused record left a trace: %q", raw)
				}
			})
		}
	}
	// The same records, each holding only its own fields, are written: the
	// refusals above are the lists', not every record's. A truncated record
	// is the writer's own, so it is not asked of a caller.
	for _, kind := range kinds {
		if kind == "truncated" {
			continue
		}
		r := maps.Clone(fields[kind])
		r["kind"] = kind
		if err := Append(storyDir(t), r); err != nil {
			t.Errorf("AC-12 a %s record holding only its own fields was refused: %v", kind, err)
		}
	}
}
