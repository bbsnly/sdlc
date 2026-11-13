package hook

// US-129 AC-8: each hook event, run in this process as cmd/sdlc runs it
// (RunHosted), against a story held in its worktree in the middle of its walk
// (every gate before the implementation passed, the freeze recorded) whose
// gate record is 190 KB, allocates no more bytes than it did at the story's
// base, and all of them together at most 60% of what they did there,
// measured with runtime.MemStats.
//
// Each measured call gets a project of its own, built off the measure, so
// that what one call writes (the record's events, the stop guard's count) is
// not what the next one reads; one call first warms the process up. Not
// parallel: MemStats counts every goroutine of the process.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/trace"
)

// us129Runs is how many calls each figure is the least of, each measured cold
// (us129Bytes). Cold, one call's figure moved by tens of bytes between calls,
// and the least of three is what the code itself costs, as init-time and
// hook-time take theirs.
const us129Runs = 3

// us129RecordSize is the size of the record the calls read: a real one
// (US-109's) was 190 KB when the story was written.
const us129RecordSize = 190 << 10

// us129MidWalk is swCheckouts with A-1's record through the design review,
// padded with the agent runs a long story records to us129RecordSize.
func us129MidWalk(t *testing.T) swLayout {
	t.Helper()
	l := swCheckouts(t)
	at := time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)
	record := model.NewRecord("A-1", at)
	for _, g := range model.GateImplementation.Before() {
		record.SetGate(g, model.GatePass, "worked by the session", at)
	}
	var raw []byte
	for i := 0; len(raw) < us129RecordSize; i++ {
		when := at.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		id := fmt.Sprintf("agent-%04d", i)
		record.Events = append(record.Events,
			model.Event{At: when, Type: model.EventAgentStart, Message: "sdlc:researcher started", AgentType: "sdlc:researcher", AgentID: id},
			model.Event{At: when, Type: model.EventAgentEnd, Message: "sdlc:researcher finished", AgentType: "sdlc:researcher", AgentID: id})
		if i%16 == 0 {
			var err error
			if raw, err = json.MarshalIndent(record, "", "  "); err != nil {
				t.Fatal(err)
			}
		}
	}
	write(t, l.main, ".sdlc/stories/A-1/gate-record.json", string(raw)+"\n")
	// An hour old, as a record is between two of a session's calls.
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(l.main, ".sdlc", "stories", "A-1", "gate-record.json"), old, old); err != nil {
		t.Fatal(err)
	}
	return l
}

// us129Event is one hook event and the payload Claude Code sends with it.
type us129Event struct {
	name, event string
	payload     func(l swLayout) map[string]any
	answer      string // "deny" or "" for a call let through
}

func us129Events() []us129Event {
	tool := func(name string, input map[string]any) func(l swLayout) map[string]any {
		return func(swLayout) map[string]any {
			return map[string]any{"tool_name": name, "agent_type": "sdlc:implementer", "tool_input": input}
		}
	}
	return []us129Event{
		{"PreToolUse Bash", "PreToolUse", tool("Bash", map[string]any{"command": "go test ./... 2>&1 | tail -n 40"}), ""},
		{"PreToolUse Write", "PreToolUse", func(l swLayout) map[string]any {
			return map[string]any{"tool_name": "Write", "agent_type": "sdlc:implementer",
				"tool_input": map[string]any{"file_path": filepath.Join(l.wt, "invoice.go"), "content": "package invoice\n"}}
		}, ""},
		{"PreToolUse Edit", "PreToolUse", func(l swLayout) map[string]any {
			return map[string]any{"tool_name": "Edit", "agent_type": "sdlc:implementer",
				"tool_input": map[string]any{"file_path": filepath.Join(l.wt, "invoice.go"), "old_string": "package", "new_string": "package"}}
		}, ""},
		{"PostToolUse", "PostToolUse", func(swLayout) map[string]any {
			return map[string]any{"tool_name": "Bash", "duration_ms": 2000,
				"tool_input":    map[string]any{"command": "go test ./..."},
				"tool_response": map[string]any{"stdout": "ok", "stderr": "", "interrupted": false}}
		}, ""},
		{"SubagentStart", "SubagentStart", func(swLayout) map[string]any {
			return map[string]any{"agent_type": "sdlc:implementer", "agent_id": "us129"}
		}, ""},
		{"SubagentStop", "SubagentStop", func(swLayout) map[string]any {
			return map[string]any{"agent_type": "sdlc:implementer", "agent_id": "us129", "stop_hook_active": false,
				"last_assistant_message": `{"status":"done"}`}
		}, ""},
		{"Stop", "Stop", func(swLayout) map[string]any { return map[string]any{"stop_hook_active": false} }, ""},
		{"SessionStart", "SessionStart", func(swLayout) map[string]any { return map[string]any{"source": "startup"} }, ""},
	}
}

func us129NoTranscripts(context.Context, string, int64) (trace.Usage, int64, error) {
	return trace.Usage{}, 0, nil
}

// us129Call is one call of e on a project of its own, and what it answered.
func us129Call(t *testing.T, e us129Event, l swLayout) string {
	t.Helper()
	payload := e.payload(l)
	payload["hook_event_name"], payload["session_id"], payload["cwd"] = e.event, swHeld, l.wt
	stdin := mustMarshal(payload)
	getenv := env(map[string]string{"CLAUDE_PROJECT_DIR": l.wt})
	var out bytes.Buffer
	if code := RunHosted([]string{e.event}, strings.NewReader(stdin), &out, io.Discard, getenv,
		Host{Transcripts: us129NoTranscripts}); code != 0 {
		t.Fatalf("%s exited %d", e.name, code)
	}
	return out.String()
}

// us129Bytes is the fewest bytes any of us129Runs calls of e allocated.
func us129Bytes(t *testing.T, e us129Event) uint64 {
	t.Helper()
	us129Call(t, e, us129MidWalk(t))
	least := ^uint64(0)
	for range us129Runs {
		l := us129MidWalk(t)
		// Cold, as a hook process starts: two collections empty sync.Pool,
		// its victim cache with it, and none runs during the call, so what
		// the JSON packages pool is allocated anew each time and never only
		// sometimes. A collection during the call emptied the pools part of
		// the way through, and a subagent event allocated 1.0 MB on one run
		// and 1.9 MB on the next.
		runtime.GC()
		runtime.GC()
		gc := debug.SetGCPercent(-1)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		out := us129Call(t, e, l)
		runtime.ReadMemStats(&after)
		debug.SetGCPercent(gc)
		least = min(least, after.TotalAlloc-before.TotalAlloc)
		var r reply
		if strings.TrimSpace(out) != "" {
			if err := json.Unmarshal([]byte(out), &r); err != nil {
				t.Fatalf("%s: stdout is not JSON: %v (%q)", e.name, err, out)
			}
		}
		if got := r.HookSpecificOutput.PermissionDecision; got != e.answer && (e.answer != "" || got != "allow") {
			t.Fatalf("%s answered %q (%s), want the call %s: the fixture is not the one AC-8 measures",
				e.name, got, r.HookSpecificOutput.PermissionDecisionReason, map[string]string{"": "let through", "deny": "refused"}[e.answer])
		}
	}
	return least
}

// us129BaseBytes is what each event allocated at 37e3ac8, by the operating
// system it was measured on (Gate 3): the most any of twenty runs gave, each
// the least of us129Runs cold calls. They spread by 0.04% at most.
var us129BaseBytes = map[string]map[string]uint64{
	"darwin": {
		"PreToolUse Bash": 1146664, "PreToolUse Write": 172720, "PreToolUse Edit": 172736, "PostToolUse": 101896,
		"SubagentStart": 1901032, "SubagentStop": 1921848, "Stop": 740272, "SessionStart": 7824,
	},
	"linux": {
		"PreToolUse Bash": 1098088, "PreToolUse Write": 119528, "PreToolUse Edit": 119544, "PostToolUse": 82304,
		"SubagentStart": 1887568, "SubagentStop": 1899216, "Stop": 676328, "SessionStart": 4864,
	},
}

// us129Noise is how far an event's figure may sit above its base, for what
// moved between runs of one tree: 1%, twenty times the spread seen.
const us129Noise = 100 // one part in a hundred

// us129WithoutRace is the row run again in a test binary built without the
// race detector. Under it sync.Pool drops a quarter of what it is handed, at
// random, and one event's figure ran from 1.92 to 2.62 MB over twenty runs of
// one tree: no figure measured there is the code's. ./task test runs this
// package under it, so the row measures in a build of its own, as the
// hook-time leg does.
func us129WithoutRace(t *testing.T) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", "test", "-count=1", "-v",
		"-run", "^TestAC8EachHookEventAllocatesNoMoreThanAtTheBaseAndAllOfThemAtMostSixtyPercent$", ".")
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("AC-8 without the race detector:\n%s", out)
	}
	t.Logf("AC-8 without the race detector:\n%s", out)
}

func TestAC8EachHookEventAllocatesNoMoreThanAtTheBaseAndAllOfThemAtMostSixtyPercent(t *testing.T) {
	if us129Race {
		us129WithoutRace(t)
		return
	}
	bases, ok := us129BaseBytes[runtime.GOOS]
	if !ok {
		t.Skipf("AC-8's base was measured on macOS and Linux; %s allocates otherwise in its file and process calls, and has no base", runtime.GOOS)
	}
	var got, base uint64
	for _, e := range us129Events() {
		n := us129Bytes(t, e)
		got += n
		want, ok := bases[e.name]
		base += want
		// The figures the retro records, with -v.
		t.Logf("%q: %d (base %d)", e.name, n, want)
		switch {
		case !ok:
			t.Errorf("AC-8 no base figure for %s", e.name)
		case n > want+want/us129Noise:
			t.Errorf("AC-8 %s allocated %d bytes a call, more than the %d it allocated at the base", e.name, n, want)
		}
	}
	if got*10 > base*6 {
		t.Errorf("AC-8 the hook events allocated %d bytes in all, want at most 60%% of the %d they allocated at the base, %d",
			got, base, base*6/10)
	}
}
