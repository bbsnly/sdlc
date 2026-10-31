package hook

// US-112: what the hook records about a story as it runs, and that recording
// never changes an answer. The two agent events go on the gate record (AC-2);
// everything else is a line of the story's trace.jsonl. The transcript reader
// is the host's, handed in from cmd/sdlc; here it is a fake whose counts and
// failures the tests choose, and Run, which is handed none, is the hook with
// no reader at all (AC-4).

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/trace"
)

// trCarryOn is the whole answer to an event the hook has nothing to say about.
const trCarryOn = "{\"continue\":true}\n"

// trProject is a project held by the session working, on story A-1, which has
// passed Gate 1 and has a gate record.
func trProject(t *testing.T) string {
	t.Helper()
	root := loopProject(t)
	recordGates(t, root, model.GateDoR)
	return root
}

// trStaffed writes A-1's gate record with the staffing given.
func trStaffed(t *testing.T, root string, s *model.Staffing) {
	t.Helper()
	record := model.NewRecord("A-1", time.Now())
	record.SetGate(model.GateDoR, model.GatePass, "", time.Now())
	record.Staffing = s
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))
}

// trFake is the host's transcript reader, as the tests want it to answer.
type trFake struct {
	mu     sync.Mutex
	calls  []trRead
	usage  trace.Usage
	err    error
	panics bool
	// next is where each read says it stopped, counted up from here.
	next int64
}

type trRead struct {
	path string
	from int64
}

func (f *trFake) read(_ context.Context, path string, from int64) (trace.Usage, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, trRead{path, from})
	if f.panics {
		panic("the transcript reader fell over")
	}
	if f.err != nil {
		return trace.Usage{}, 0, f.err
	}
	f.next += 1000
	return f.usage, f.next, nil
}

func (f *trFake) reads() []trRead {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// trUsage is what the fake reports: two models, three responses.
func trUsage() trace.Usage {
	return trace.Usage{
		Models: map[string]trace.Tokens{
			"model-a": {Input: 10, CacheRead: 100, CacheWritten: 5, Output: 7},
			"model-b": {Input: 1, CacheRead: 2, CacheWritten: 3, Output: 4},
		},
		Responses:   3,
		PeakContext: 115,
		Tools:       map[string]int{"Read": 2, "Bash": 1},
	}
}

// trPayload is an event of the session working, from root, with fields on
// top.
func trPayload(root, event string, fields map[string]any) string {
	p := map[string]any{"hook_event_name": event, "session_id": working, "cwd": root}
	for k, v := range fields {
		if v == nil {
			delete(p, k)
			continue
		}
		p[k] = v
	}
	return mustMarshal(p)
}

// trSend hands the hook one event as Claude Code would and returns what it
// printed. With a fake, it is the hook cmd/sdlc builds, with a transcript
// reader; without one, it is Run, which has none.
func trSend(t *testing.T, event, payload string, getenv func(string) string, fake *trFake) string {
	t.Helper()
	var out bytes.Buffer
	var code int
	if fake != nil {
		code = RunHosted([]string{event}, strings.NewReader(payload), &out, io.Discard, getenv, Host{Transcripts: fake.read})
	} else {
		code = Run([]string{event}, strings.NewReader(payload), &out, io.Discard, getenv)
	}
	if code != 0 {
		t.Fatalf("the hook exited %d on %s", code, event)
	}
	return out.String()
}

func trTracePath(root string) string {
	return filepath.Join(root, ".sdlc", "stories", "A-1", "trace.jsonl")
}

// trRecords is every line of A-1's trace, decoded; none where there is no
// trace.
func trRecords(t *testing.T, root string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(trTracePath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("a trace line is not JSON: %v\n%s", err, line)
		}
		out = append(out, m)
	}
	return out
}

func trOfKind(records []map[string]any, kind string) []map[string]any {
	var out []map[string]any
	for _, r := range records {
		if r["kind"] == kind {
			out = append(out, r)
		}
	}
	return out
}

// trOne is the one record of kind, failing where there is not exactly one.
func trOne(t *testing.T, root, kind string) map[string]any {
	t.Helper()
	all := trRecords(t, root)
	got := trOfKind(all, kind)
	if len(got) != 1 {
		t.Fatalf("want one %s record in the trace, found %d among %v", kind, len(got), all)
	}
	return got[0]
}

// trEvents is the gate record's events, as raw JSON objects, so a field the
// model does not know yet is still seen.
func trEvents(t *testing.T, root string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "A-1", model.RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r.Events
}

func trEventsOfType(t *testing.T, root, typ string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, e := range trEvents(t, root) {
		if e["type"] == typ {
			out = append(out, e)
		}
	}
	return out
}

// trAbsent holds a record to AC-4's "absent, never zero".
func trAbsent(t *testing.T, r map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if v, ok := r[k]; ok {
			t.Errorf("%s is %v on a %s record, want it absent", k, v, r["kind"])
		}
	}
}

var trInstant = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

func trTime(t *testing.T, r map[string]any, key string) time.Time {
	t.Helper()
	s, _ := r[key].(string)
	if !trInstant.MatchString(s) {
		t.Fatalf("%s on a %s record is %q, want RFC 3339 in UTC to the millisecond", key, r["kind"], s)
	}
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func trNumber(r map[string]any, key string) float64 {
	n, _ := r[key].(float64)
	return n
}

// trSubagent starts and stops one subagent, and returns what the stop printed.
func trSubagent(t *testing.T, root, agentType, id string, stop map[string]any, getenv func(string) string, fake *trFake) string {
	t.Helper()
	start := trSend(t, "SubagentStart", trPayload(root, "SubagentStart", map[string]any{"agent_type": agentType, "agent_id": id}), getenv, fake)
	if start != trCarryOn {
		t.Errorf("SubagentStart was answered %q, want %q", start, trCarryOn)
	}
	fields := map[string]any{
		"agent_type": agentType, "agent_id": id, "stop_hook_active": false,
		"agent_transcript_path":  filepath.Join(root, "transcripts", id+".jsonl"),
		"last_assistant_message": "Done.",
	}
	maps.Copy(fields, stop)
	return trSend(t, "SubagentStop", trPayload(root, "SubagentStop", fields), getenv, fake)
}

// ------------------------------------------------------------------ AC-2

func TestAC2ASubagentsStartAndEndAreEventsOnTheGateRecordWithItsTypeAndID(t *testing.T) {
	t.Parallel()
	for _, agentType := range []string{"sdlc:verifier", "Explore"} {
		root := trProject(t)
		if got := trSubagent(t, root, agentType, "a1b2", nil, noEnv, nil); got != trCarryOn {
			t.Errorf("AC-2 SubagentStop of %s was answered %q, want %q", agentType, got, trCarryOn)
		}
		for _, typ := range []string{"agent_start", "agent_end"} {
			events := trEventsOfType(t, root, typ)
			if len(events) != 1 {
				t.Fatalf("AC-2 %s: %d %s events on the gate record, want 1: %v", agentType, len(events), typ, trEvents(t, root))
			}
			e := events[0]
			if e["agent_type"] != agentType || e["agent_id"] != "a1b2" {
				t.Errorf("AC-2 %s holds agent_type %v and agent_id %v, want %q and a1b2", typ, e["agent_type"], e["agent_id"], agentType)
			}
			if at, _ := e["at"].(string); at == "" {
				t.Errorf("AC-2 %s has no time", typ)
			}
			for k, v := range e {
				if !slices.Contains([]string{"at", "type", "agent_type", "agent_id", "message"}, k) {
					t.Errorf("AC-2 %s holds %s = %v, and nothing else is kept", typ, k, v)
				}
			}
			if m, _ := e["message"].(string); m != "" {
				t.Errorf("AC-2 %s holds a message: %q", typ, m)
			}
		}
	}
}

// ------------------------------------------------------------------ AC-3

func TestAC3AnAgentRunHoldsWhatTheStopAndTheTranscriptSay(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	trStaffed(t, root, &model.Staffing{
		At:     "2026-10-28T10:00:00Z",
		Agents: []model.Staffed{{Agent: "verifier", Needed: true, AskedModel: "opus", AskedEffort: "high"}},
	})
	fake := &trFake{usage: trUsage()}
	before := time.Now().UTC().Add(-2 * time.Second)
	got := trSubagent(t, root, "sdlc:verifier", "a1", map[string]any{
		"effort": map[string]any{"level": "medium"}, "permission_mode": "acceptEdits",
	}, noEnv, fake)
	if got != trCarryOn {
		t.Errorf("SubagentStop was answered %q", got)
	}
	r := trOne(t, root, "agent_run")
	for k, want := range map[string]any{
		"agent_type": "sdlc:verifier", "agent_id": "a1", "effort": "medium", "permission_mode": "acceptEdits",
		"asked_model": "opus", "asked_effort": "high",
	} {
		if r[k] != want {
			t.Errorf("AC-3 %s = %v, want %v", k, r[k], want)
		}
	}
	start, end := trTime(t, r, "start"), trTime(t, r, "end")
	if start.Before(before.Truncate(time.Second)) || end.Before(start) || end.After(time.Now().UTC().Add(time.Second)) {
		t.Errorf("AC-3 the run went from %v to %v, which is not when it ran", start, end)
	}
	models, _ := r["models"].(map[string]any)
	a, _ := models["model-a"].(map[string]any)
	if trNumber(a, "input") != 10 || trNumber(a, "cache_read") != 100 || trNumber(a, "cache_written") != 5 || trNumber(a, "output") != 7 {
		t.Errorf("AC-3 model-a's tokens = %v", a)
	}
	if b, _ := models["model-b"].(map[string]any); trNumber(b, "output") != 4 || len(models) != 2 {
		t.Errorf("AC-3 models = %v, want model-a and model-b under their own names", models)
	}
	if trNumber(r, "responses") != 3 || trNumber(r, "peak_context") != 115 {
		t.Errorf("AC-3 responses %v and peak context %v, want 3 and 115", r["responses"], r["peak_context"])
	}
	if tools, _ := r["tools"].(map[string]any); trNumber(tools, "Read") != 2 || trNumber(tools, "Bash") != 1 {
		t.Errorf("AC-3 tool calls = %v", r["tools"])
	}
	// The ratio is computed when it is shown, from the stored counts.
	trAbsent(t, r, "cache_read_ratio", "cache_ratio")
	reads := fake.reads()
	if len(reads) != 1 || reads[0].path != filepath.Join(root, "transcripts", "a1.jsonl") || reads[0].from != 0 {
		t.Errorf("AC-3 the subagent's transcript was read as %+v, want once, whole, from the stop's agent_transcript_path", reads)
	}
}

// AC-3: what was asked for is the staffing's, a person's assignment included,
// and absent where the staffing names none.
func TestAC3TheModelAndEffortAskedForComeFromTheStaffingOrAreAbsent(t *testing.T) {
	t.Parallel()
	t.Run("AC-3 a person's assignment with no plan row", func(t *testing.T) {
		t.Parallel()
		root := trProject(t)
		trStaffed(t, root, &model.Staffing{Assigned: []model.Assigned{{Agent: "verifier", Model: "sonnet"}}})
		trSubagent(t, root, "sdlc:verifier", "a1", nil, noEnv, nil)
		r := trOne(t, root, "agent_run")
		if r["asked_model"] != "sonnet" {
			t.Errorf("asked_model = %v, want the person's sonnet", r["asked_model"])
		}
		trAbsent(t, r, "asked_effort")
	})
	t.Run("AC-3 no staffing at all", func(t *testing.T) {
		t.Parallel()
		root := trProject(t)
		trSubagent(t, root, "sdlc:verifier", "a1", nil, noEnv, nil)
		trAbsent(t, trOne(t, root, "agent_run"), "asked_model", "asked_effort")
	})
	t.Run("AC-3 an agent the plugin does not ship", func(t *testing.T) {
		t.Parallel()
		root := trProject(t)
		trStaffed(t, root, &model.Staffing{
			At:     "2026-10-28T10:00:00Z",
			Agents: []model.Staffed{{Agent: "verifier", Needed: true, AskedModel: "opus", AskedEffort: "high"}},
		})
		trSubagent(t, root, "Explore", "a1", nil, noEnv, nil)
		r := trOne(t, root, "agent_run")
		if r["agent_type"] != "Explore" {
			t.Errorf("agent_type = %v, want Explore as the payload named it", r["agent_type"])
		}
		trAbsent(t, r, "asked_model", "asked_effort", "effort", "permission_mode")
	})
}

// ------------------------------------------------------------------ AC-4

// AC-4: no transcript counts, and the run is recorded all the same, with the
// counts absent and never zero.
func TestAC4AnAgentRunWhoseTranscriptCannotBeReadHasNoCountsAndIsStillRecorded(t *testing.T) {
	transcriptFields := []string{"models", "responses", "peak_context", "tools"}
	t.Run("AC-4 no reader", func(t *testing.T) {
		root := trProject(t)
		trSubagent(t, root, "sdlc:verifier", "a1", nil, noEnv, nil)
		r := trOne(t, root, "agent_run")
		trAbsent(t, r, transcriptFields...)
		if r["agent_id"] != "a1" {
			t.Errorf("the run was not recorded whole: %v", r)
		}
	})
	t.Run("AC-4 the reader fails", func(t *testing.T) {
		log := debugLog(t)
		root := trProject(t)
		fake := &trFake{err: errors.New("not in the shape the hook reads")}
		if got := trSubagent(t, root, "sdlc:verifier", "a1", nil, noEnv, fake); got != trCarryOn {
			t.Errorf("SubagentStop was answered %q", got)
		}
		trAbsent(t, trOne(t, root, "agent_run"), transcriptFields...)
		if len(linesHolding(log.String(), "transcript")) == 0 {
			t.Errorf("AC-4 the debug log does not say why the counts are missing:\n%s", log)
		}
		if strings.Contains(log.String(), filepath.Join(root, "transcripts")) {
			t.Errorf("AC-4 the debug log holds the transcript's path:\n%s", log)
		}
	})
	t.Run("AC-4 no transcript path in the payload", func(t *testing.T) {
		root := trProject(t)
		fake := &trFake{usage: trUsage()}
		trSubagent(t, root, "sdlc:verifier", "a1", map[string]any{"agent_transcript_path": nil}, noEnv, fake)
		trAbsent(t, trOne(t, root, "agent_run"), transcriptFields...)
		if len(fake.reads()) != 0 {
			t.Errorf("a transcript was read with no path to it: %+v", fake.reads())
		}
	})
}

// ------------------------------------------------------------------ AC-5

func TestAC5TheHandBackStatusIsReadOnlyFromAJSONObjectsStatusKey(t *testing.T) {
	t.Parallel()
	for message, want := range map[string]string{
		`{"status":"done","summary":"MARKER"}`:  "done",
		`{"status":"blocked","blocked_on":"x"}`: "blocked",
		"\n  {\"status\": \"done\"}  \n":        "done",
		`status: done`:                          "",
		`{"status":"finished"}`:                 "",
		`{"status":"done"} and then some text`:  "",
		`Here it is: {"status":"done"}`:         "",
		`{"result":"done"}`:                     "",
		`[{"status":"done"}]`:                   "",
		`{"status":true}`:                       "",
		"":                                      "",
	} {
		root := trProject(t)
		trSubagent(t, root, "sdlc:verifier", "a1", map[string]any{"last_assistant_message": message}, noEnv, nil)
		r := trOne(t, root, "agent_run")
		if want == "" {
			if v, ok := r["status"]; ok {
				t.Errorf("AC-5 a hand-back of %q gave status %v, want none", message, v)
			}
			continue
		}
		if r["status"] != want {
			t.Errorf("AC-5 a hand-back of %q gave status %v, want %s", message, r["status"], want)
		}
	}
}

// AC-5 and ANALYSIS R2: an agent that hands back through the SubagentHandback
// tool delivers its report there, and its last message is only closing text.
func TestAC5AHandBackThroughTheToolGivesTheRunItsStatus(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	trSend(t, "SubagentStart", trPayload(root, "SubagentStart", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "a1"}), noEnv, nil)
	got := trSend(t, "PostToolUse", trPayload(root, "PostToolUse", map[string]any{
		"agent_type": "sdlc:verifier", "agent_id": "a1", "tool_name": "SubagentHandback",
		"tool_input":    map[string]any{"message": `{"status":"blocked","why":"MARKER"}`},
		"tool_response": map[string]any{"ok": true},
	}), noEnv, nil)
	if got != trCarryOn {
		t.Errorf("PostToolUse on SubagentHandback was answered %q, want %q", got, trCarryOn)
	}
	trSend(t, "SubagentStop", trPayload(root, "SubagentStop", map[string]any{
		"agent_type": "sdlc:verifier", "agent_id": "a1", "last_assistant_message": "Handed back.",
	}), noEnv, nil)
	runs := trOfKind(trRecords(t, root), "agent_run")
	if len(runs) != 1 || runs[0]["status"] != "blocked" {
		t.Errorf("AC-5 the run's status is not the one handed back through the tool: %v", runs)
	}
	raw, _ := os.ReadFile(trTracePath(root))
	if strings.Contains(string(raw), "MARKER") {
		t.Errorf("AC-5 the rest of the hand-back was kept: %s", raw)
	}
}

// ------------------------------------------------------------------ AC-6

func trStopPayload(root string, fields map[string]any) string {
	base := map[string]any{"stop_hook_active": false, "transcript_path": filepath.Join(root, "transcripts", "main.jsonl")}
	maps.Copy(base, fields)
	return trPayload(root, "Stop", base)
}

func TestAC6AStopRecordsATurnOfTheMainConversation(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	fake := &trFake{usage: trUsage()}
	trSend(t, "Stop", trStopPayload(root, map[string]any{
		"prompt_id": "p-1", "effort": map[string]any{"level": "high"},
		"last_assistant_message": "MARKER",
		"background_tasks": []map[string]any{
			{"id": "b1", "type": "local_bash", "status": "running", "description": "MARKER", "command": "MARKER"},
			{"id": "b2", "type": "local_bash", "status": "running", "description": "MARKER", "command": "MARKER"},
			{"id": "b3", "type": "remote_agent", "status": "running", "description": "MARKER"},
		},
	}), noEnv, fake)
	r := trOne(t, root, "turn")
	trTime(t, r, "at")
	if r["prompt_id"] != "p-1" || r["effort"] != "high" {
		t.Errorf("AC-6 prompt_id %v and effort %v, want p-1 and high", r["prompt_id"], r["effort"])
	}
	if trNumber(r, "responses") != 3 || trNumber(r, "peak_context") != 115 {
		t.Errorf("AC-6 responses %v, peak context %v", r["responses"], r["peak_context"])
	}
	models, _ := r["models"].(map[string]any)
	if a, _ := models["model-a"].(map[string]any); trNumber(a, "cache_read") != 100 {
		t.Errorf("AC-6 the turn's tokens by model = %v", r["models"])
	}
	if tools, _ := r["tools"].(map[string]any); trNumber(tools, "Read") != 2 {
		t.Errorf("AC-6 the turn's tool calls = %v", r["tools"])
	}
	if bg, _ := r["background_tasks"].(map[string]any); trNumber(bg, "local_bash") != 2 || trNumber(bg, "remote_agent") != 1 || len(bg) != 2 {
		t.Errorf("AC-6 background tasks by type = %v, want local_bash 2 and remote_agent 1", r["background_tasks"])
	}
	raw, _ := os.ReadFile(trTracePath(root))
	if strings.Contains(string(raw), "MARKER") {
		t.Errorf("AC-6 a task's command or description, or the last message, was kept: %s", raw)
	}
	if reads := fake.reads(); len(reads) != 1 || reads[0].path != filepath.Join(root, "transcripts", "main.jsonl") {
		t.Errorf("AC-6 the main transcript was read as %+v", reads)
	}
}

// AC-6: each turn counts from where the previous turn's record stopped.
func TestAC6EachTurnCountsFromWhereThePreviousTurnStopped(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	fake := &trFake{usage: trUsage()}
	for range 3 {
		trSend(t, "Stop", trStopPayload(root, map[string]any{"stop_hook_active": true}), noEnv, fake)
	}
	reads := fake.reads()
	if len(reads) != 3 {
		t.Fatalf("AC-6 three stops read the transcript %d times", len(reads))
	}
	if reads[0].from != 0 || reads[1].from != 1000 || reads[2].from != 2000 {
		t.Errorf("AC-6 the turns read from %d, %d and %d; want 0 and then where each read stopped, 1000 and 2000",
			reads[0].from, reads[1].from, reads[2].from)
	}
	if n := len(trOfKind(trRecords(t, root), "turn")); n != 3 {
		t.Errorf("AC-6 three stops made %d turn records", n)
	}
}

// AC-6 and AC-13: the answer to Stop is the one it gives today, recorded or
// not, and whether the recording worked or failed.
func TestAC6AC13TheAnswerToAStopIsTheSameWhateverTheRecording(t *testing.T) {
	t.Parallel()
	answers := map[string]string{}
	for name, setup := range map[string]func(root string) *trFake{
		"no reader": func(string) *trFake { return nil },
		"a reader":  func(string) *trFake { return &trFake{usage: trUsage()} },
		"a reader that fails": func(string) *trFake {
			return &trFake{err: errors.New("unreadable")}
		},
		"a reader that panics": func(string) *trFake { return &trFake{panics: true} },
		"a trace that cannot be written": func(root string) *trFake {
			if err := os.MkdirAll(trTracePath(root), 0o755); err != nil {
				t.Fatal(err)
			}
			return &trFake{usage: trUsage()}
		},
	} {
		root := trProject(t)
		fake := setup(root)
		got := trSend(t, "Stop", trStopPayload(root, map[string]any{"prompt_id": "p-1"}), noEnv, fake)
		// The root is in the reason; take it out so projects compare.
		answers[name] = strings.ReplaceAll(got, root, "<root>")
	}
	want := answers["no reader"]
	var r stopOutcome
	if err := json.Unmarshal([]byte(want), &r); err != nil || r.Decision != "block" {
		t.Fatalf("the stop mid-story was not sent back as it is today: %q", want)
	}
	for name, got := range answers {
		if got != want {
			t.Errorf("AC-13 with %s the stop was answered\n%s\nwant\n%s", name, got, want)
		}
	}
}

// ------------------------------------------------------------------ AC-7

func TestAC7ARefusedFileWriteIsRecordedByRuleAndToolAndNothingElse(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	if r := call(t, event(root, "Write", "", filepath.Join(root, ".sdlc/stories/A-1/gate-record.json")), noEnv); !denied(r) {
		t.Fatal("the main conversation wrote the gate record")
	}
	if r := call(t, event(root, "Edit", "sdlc:implementer", filepath.Join(root, ".sdlc/stories/A-1/gate-record.json")), noEnv); !denied(r) {
		t.Fatal("the implementer edited the gate record")
	}
	refusals := trOfKind(trRecords(t, root), "refusal")
	if len(refusals) != 2 {
		t.Fatalf("AC-7 two refusals made %d records: %v", len(refusals), trRecords(t, root))
	}
	main, agent := refusals[0], refusals[1]
	if main["rule"] != "gate-record-is-written-by-the-tool" || main["tool"] != "Write" {
		t.Errorf("AC-7 the first refusal is recorded as %v", main)
	}
	trAbsent(t, main, "agent_type", "code")
	if agent["agent_type"] != "sdlc:implementer" || agent["tool"] != "Edit" {
		t.Errorf("AC-7 the implementer's refusal is recorded as %v", agent)
	}
	trTime(t, main, "at")
	raw, _ := os.ReadFile(trTracePath(root))
	if strings.Contains(string(raw), "gate-record.json") || strings.Contains(string(raw), root) {
		t.Errorf("AC-7 the refused path was kept: %s", raw)
	}
}

func TestAC7ARefusedCommandKeepsNoWordOfTheCommandOrTheReason(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	r := call(t, command(root, "sdlc-implementer", "rm .sdlc/state/tests.lock # MARKER"), noEnv)
	if !denied(r) {
		t.Fatal("the implementer removed the freeze")
	}
	rec := trOne(t, root, "refusal")
	if rec["rule"] != "loop-state-through-the-tool" || rec["tool"] != "Bash" || rec["agent_type"] != "sdlc-implementer" {
		t.Errorf("AC-7 the refusal is recorded as %v", rec)
	}
	raw, _ := os.ReadFile(trTracePath(root))
	for _, word := range []string{"MARKER", "tests.lock", "rm ", "Instead"} {
		if strings.Contains(string(raw), word) {
			t.Errorf("AC-7 the record holds %q from the command or the reason: %s", word, raw)
		}
	}
	// An allowed call is no refusal.
	call(t, command(root, "sdlc-implementer", "go build ./..."), noEnv)
	if n := len(trOfKind(trRecords(t, root), "refusal")); n != 1 {
		t.Errorf("AC-7 an allowed call was recorded as a refusal: %d refusals", n)
	}
}

// AC-7: the SDLC-E code, where the refusal carries one.
func TestAC7ARefusalWithACodeRecordsTheCode(t *testing.T) {
	t.Parallel()
	root := lbProject(t)
	recordGates(t, root, model.GateDoR)
	word := lbAtOneName(t, root, "lnk", lbPastTheLookup(65), "elsewhere", "notes.txt")
	if reason := lbAsk(t, root, "touch "+word); !strings.Contains(reason, lbWordCode) {
		t.Fatalf("the long chain was not refused under %s: %q", lbWordCode, reason)
	}
	if rec := trOne(t, root, "refusal"); rec["code"] != lbWordCode {
		t.Errorf("AC-7 the refusal's code is %v, want %s", rec["code"], lbWordCode)
	}
}

// AC-7: with no story held there is nowhere to write, and nothing is.
func TestAC7ARefusalWithNoStoryHeldWritesNothing(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	if r := call(t, command(root, "", "sdlc ack --through HEAD"), noEnv); !denied(r) {
		t.Fatal("a session acknowledged the log with no story held")
	}
	trNothingWritten(t, root)
}

// trNothingWritten is AC-15's "nothing anywhere": no trace in any story.
func trNothingWritten(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "trace.jsonl" {
			t.Errorf("a trace was written: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// ------------------------------------------------------------------ AC-8, AC-9

func trShell(root, event, tool, cmd string, fields map[string]any) string {
	base := map[string]any{
		"tool_name":  tool,
		"tool_input": map[string]any{"command": cmd, "description": "MARKER", "run_in_background": false},
	}
	if event == "PostToolUse" {
		base["tool_response"] = map[string]any{"stdout": "MARKER", "stderr": "", "interrupted": false}
	}
	maps.Copy(base, fields)
	return trPayload(root, event, base)
}

func TestAC8AC9ATestRunThatSucceededIsRecordedWithItsClassProgramDurationAndOutcome(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	got := trSend(t, "PostToolUse", trShell(root, "PostToolUse", "Bash", "cd -- '"+root+"' && go test ./...", map[string]any{"duration_ms": 1234}), noEnv, nil)
	if got != trCarryOn {
		t.Errorf("PostToolUse on Bash was answered %q", got)
	}
	r := trOne(t, root, "shell")
	trTime(t, r, "at")
	for k, want := range map[string]any{"class": "test", "program": "go", "outcome": "pass", "background": false} {
		if r[k] != want {
			t.Errorf("AC-8 %s = %v, want %v", k, r[k], want)
		}
	}
	if trNumber(r, "duration_ms") != 1234 {
		t.Errorf("AC-8 duration_ms = %v, want 1234 from the payload", r["duration_ms"])
	}
	trAbsent(t, r, "agent_type", "exit_code")
}

func TestAC8AShellRecordKeepsTheAgentTheBackgroundAndNoDurationItWasNotGiven(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	trSend(t, "PostToolUse", trShell(root, "PostToolUse", "Bash", "git status", map[string]any{
		"agent_type": "sdlc:sdet",
		"tool_input": map[string]any{"command": "git status", "run_in_background": true},
	}), noEnv, nil)
	r := trOne(t, root, "shell")
	if r["agent_type"] != "sdlc:sdet" || r["background"] != true || r["class"] != "git" || r["program"] != "git" {
		t.Errorf("AC-8 the record is %v", r)
	}
	// AC-8: absent when the payload gives none, never zero; AC-9: outcome only
	// for a test run.
	trAbsent(t, r, "duration_ms", "outcome", "exit_code")
}

func TestAC8TheClassComesFromTheProjectsOwnCommandsToo(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	write(t, root, ".sdlc/config.json", `{"version":1,"commands":{"test":"./task test","lint":"./task lint","build":"./task build"}}`)
	for cmd, class := range map[string]string{"./task test": "test", "./task lint": "lint", "./task build": "build", "./task check": "other"} {
		trSend(t, "PostToolUse", trShell(root, "PostToolUse", "Bash", cmd, map[string]any{"duration_ms": 5}), noEnv, nil)
		records := trOfKind(trRecords(t, root), "shell")
		if len(records) == 0 {
			t.Fatalf("AC-8 %q made no shell record", cmd)
		}
		if last := records[len(records)-1]; last["class"] != class || last["program"] != "task" {
			t.Errorf("AC-8 %q is %v, want class %s, program task", cmd, last, class)
		}
	}
}

func TestAC8APowerShellCommandAndOneThatCannotBeReadAreRecorded(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	trSend(t, "PostToolUse", trShell(root, "PostToolUse", "PowerShell", `Set-Location C:\work; C:\Go\bin\go.exe test ./...`, map[string]any{"duration_ms": 9}), noEnv, nil)
	trSend(t, "PostToolUse", trShell(root, "PostToolUse", "Bash", `echo "unterminated MARKER`, map[string]any{"duration_ms": 9}), noEnv, nil)
	records := trOfKind(trRecords(t, root), "shell")
	if len(records) != 2 {
		t.Fatalf("AC-8 two commands made %d shell records", len(records))
	}
	if records[0]["class"] != "test" || records[0]["program"] != "go" {
		t.Errorf("AC-8 AC-23 the PowerShell test run is %v", records[0])
	}
	if records[1]["class"] != "other" {
		t.Errorf("AC-8 a command that cannot be read is %v, want class other", records[1])
	}
	trAbsent(t, records[1], "program", "outcome")
}

func TestAC9AFailedOrInterruptedTestRunIsRecordedWithItsOutcomeAndNoOtherText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		fields map[string]any
		want   string
		code   float64
	}{
		{"an exit code", map[string]any{"error": "Exit code 1\nFAIL\tMARKER\t0.1s", "duration_ms": 50}, "fail", 1},
		{"a signal's exit code", map[string]any{"error": "Exit code 137\nMARKER", "duration_ms": 50}, "fail", 137},
		{"an interrupt", map[string]any{"error": "Interrupted MARKER", "is_interrupt": true}, "interrupted", 0},
		{"a shell that could not start", map[string]any{"error": "MARKER: spawn failed"}, "unknown", 0},
		{"an exit code later in the text", map[string]any{"error": "MARKER\nExit code 2"}, "unknown", 0},
	} {
		t.Run("AC-9 "+tc.name, func(t *testing.T) {
			t.Parallel()
			root := trProject(t)
			got := trSend(t, "PostToolUseFailure", trShell(root, "PostToolUseFailure", "Bash", "go test ./...", tc.fields), noEnv, nil)
			if got != trCarryOn {
				t.Errorf("PostToolUseFailure was answered %q, want %q", got, trCarryOn)
			}
			r := trOne(t, root, "shell")
			if r["class"] != "test" || r["outcome"] != tc.want {
				t.Errorf("AC-9 the record is %v, want class test, outcome %s", r, tc.want)
			}
			if tc.code != 0 {
				if trNumber(r, "exit_code") != tc.code {
					t.Errorf("AC-9 exit_code = %v, want %v", r["exit_code"], tc.code)
				}
			} else {
				trAbsent(t, r, "exit_code")
			}
			raw, _ := os.ReadFile(trTracePath(root))
			if strings.Contains(string(raw), "MARKER") || strings.Contains(string(raw), "Exit code") {
				t.Errorf("AC-9 the error text was kept: %s", raw)
			}
		})
	}
}

// AC-9: outcome is a test run's alone.
func TestAC9AFailedCommandThatIsNotATestRunHasNoOutcome(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	trSend(t, "PostToolUseFailure", trShell(root, "PostToolUseFailure", "Bash", "git push", map[string]any{"error": "Exit code 1"}), noEnv, nil)
	r := trOne(t, root, "shell")
	if r["class"] != "git" {
		t.Errorf("the record is %v", r)
	}
	trAbsent(t, r, "outcome", "exit_code")
}

// THREATS E2: a PostToolUseFailure is never judged as a PreToolUse. Before
// it was routed, a failed command the rules refuse came back as a deny.
func TestAC9AFailedCommandIsNeverAnsweredAsARefusal(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	for _, cmd := range []string{"rm .sdlc/state/tests.lock", "sdlc approve A-1", "echo x > CLAUDE.md"} {
		if got := trSend(t, "PostToolUseFailure", trShell(root, "PostToolUseFailure", "Bash", cmd, map[string]any{"error": "Exit code 1"}), noEnv, nil); got != trCarryOn {
			t.Errorf("a failed %q was answered %q, want %q", cmd, got, trCarryOn)
		}
	}
	idle := trProject(t)
	if err := os.Remove(filepath.Join(idle, ".sdlc", "state", "active")); err != nil {
		t.Fatal(err)
	}
	if got := trSend(t, "PostToolUseFailure", trShell(idle, "PostToolUseFailure", "Bash", "sdlc ack --through HEAD", map[string]any{"error": "Exit code 1"}), noEnv, nil); got != trCarryOn {
		t.Errorf("a failed person's decision with no story held was answered %q, want %q", got, trCarryOn)
	}
	if n := len(trOfKind(trRecords(t, root), "shell")); n != 3 {
		t.Errorf("three failed commands made %d shell records", n)
	}
}

// ------------------------------------------------------------------ AC-11

func TestAC11NotificationsAPIErrorsAndCompactionsAreRecordedByTheirTypeAlone(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	for _, e := range []struct {
		event  string
		fields map[string]any
	}{
		{"Notification", map[string]any{"notification_type": "permission_prompt", "message": "MARKER", "title": "MARKER"}},
		{"Notification", map[string]any{"notification_type": "idle_prompt", "message": "MARKER", "agent_type": "sdlc:sdet"}},
		{"Notification", map[string]any{"notification_type": "auth_success", "message": "MARKER"}},
		{"StopFailure", map[string]any{"error": "rate_limit", "error_details": "MARKER", "last_assistant_message": "MARKER"}},
		{"StopFailure", map[string]any{"error": "MARKER free text where a type should be"}},
		{"PostCompact", map[string]any{"trigger": "auto", "compact_summary": "MARKER", "custom_instructions": "MARKER"}},
	} {
		if got := trSend(t, e.event, trPayload(root, e.event, e.fields), noEnv, nil); got != trCarryOn {
			t.Errorf("%s was answered %q, want %q", e.event, got, trCarryOn)
		}
	}
	records := trRecords(t, root)
	notes := trOfKind(records, "notification")
	if len(notes) != 2 || notes[0]["notification_type"] != "permission_prompt" || notes[1]["notification_type"] != "idle_prompt" {
		t.Errorf("AC-11 notifications = %v, want the permission prompt and the idle prompt only", notes)
	} else {
		trAbsent(t, notes[0], "agent_type")
		if notes[1]["agent_type"] != "sdlc:sdet" {
			t.Errorf("AC-11 the subagent's notification lost its agent type: %v", notes[1])
		}
	}
	errs := trOfKind(records, "api_error")
	if len(errs) != 2 || errs[0]["error"] != "rate_limit" {
		t.Errorf("AC-11 API errors = %v", errs)
	} else {
		// THREATS I1: a value outside the documented set is not copied.
		trAbsent(t, errs[1], "error")
	}
	if c := trOfKind(records, "compaction"); len(c) != 1 || c[0]["trigger"] != "auto" {
		t.Errorf("AC-11 compactions = %v", c)
	}
	for _, r := range records {
		trTime(t, r, "at")
	}
	raw, _ := os.ReadFile(trTracePath(root))
	if strings.Contains(string(raw), "MARKER") {
		t.Errorf("AC-11 a message, a detail or a summary was kept: %s", raw)
	}
}

// ------------------------------------------------------------------ AC-13

// AC-13 and THREATS E1: a refused call is refused, and an allowed one allowed,
// whether or not its record can be written.
func TestAC13ARefusalIsAnsweredTheSameWhenItsRecordCannotBeWritten(t *testing.T) {
	t.Parallel()
	answer := func(root, cmd string) string {
		var out bytes.Buffer
		Run([]string{"PreToolUse"}, strings.NewReader(command(root, "sdlc-implementer", cmd)), &out, io.Discard, noEnv)
		return strings.ReplaceAll(out.String(), root, "<root>")
	}
	healthy, broken := trProject(t), trProject(t)
	if err := os.MkdirAll(trTracePath(broken), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"rm .sdlc/state/tests.lock", "go build ./..."} {
		want, got := answer(healthy, cmd), answer(broken, cmd)
		if got != want {
			t.Errorf("AC-13 %q was answered\n%s\nwith the trace unwritable, and\n%s\nwith it writable", cmd, got, want)
		}
	}
	if !strings.Contains(answer(broken, "rm .sdlc/state/tests.lock"), `"deny"`) {
		t.Error("AC-13 a refusal whose record could not be written was let through")
	}
	if n := len(trOfKind(trRecords(t, healthy), "refusal")); n != 1 {
		t.Errorf("the healthy project recorded %d refusals, so the comparison shows nothing", n)
	}
}

// AC-13: a panic or a failure while recording a subagent leaves the answer as
// it is today.
func TestAC13ARecordingThatPanicsOrFailsStillAnswersCarryOn(t *testing.T) {
	t.Parallel()
	for name, fake := range map[string]*trFake{
		"a panic":   {panics: true},
		"a failure": {err: errors.New("unreadable")},
	} {
		root := trProject(t)
		if got := trSubagent(t, root, "sdlc:verifier", "a1", nil, noEnv, fake); got != trCarryOn {
			t.Errorf("AC-13 with %s in recording, SubagentStop was answered %q", name, got)
		}
		if len(trEventsOfType(t, root, "agent_start")) != 1 {
			t.Errorf("AC-13 with %s at the stop, the start was not recorded either", name)
		}
	}
	root := trProject(t)
	for _, payload := range []string{
		trPayload(root, "SubagentStop", map[string]any{"agent_type": "sdlc:verifier"}),
		trPayload(root, "SubagentStart", map[string]any{"agent_id": 7}),
		trPayload(root, "PostToolUse", map[string]any{"tool_name": "Bash", "tool_input": "not an object"}),
		trPayload(root, "Notification", map[string]any{"notification_type": []int{1}}),
		`{"hook_event_name":"SubagentStop",`,
	} {
		for _, event := range []string{"SubagentStart", "SubagentStop", "PostToolUse", "Notification", "PostToolUseFailure", "StopFailure", "PostCompact"} {
			if got := trSend(t, event, payload, noEnv, &trFake{panics: true}); got != trCarryOn {
				t.Errorf("AC-13 %s with a payload missing what recording needs was answered %q", event, got)
			}
		}
	}
}

// AC-13: the lock not got. SubagentStart waits for the store's lock like any
// command that writes the gate record, and where it cannot have it, writes
// nothing and answers as today.
func TestAC1AC13AnAgentEventWhoseLockCannotBeTakenWritesNothingAndTheTraceNeedsNoLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the cache directory is found differently on Windows")
	}
	root := trProject(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	cacheBase := filepath.Join(home, "cache")
	if runtime.GOOS == "darwin" {
		cacheBase = filepath.Join(home, "Library", "Caches")
	} else {
		t.Setenv("XDG_CACHE_HOME", cacheBase)
	}
	if err := os.MkdirAll(cacheBase, 0o755); err != nil {
		t.Fatal(err)
	}
	// A plain file named "sdlc" blocks the lock directory beneath it.
	if err := os.WriteFile(filepath.Join(cacheBase, "sdlc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "A-1", model.RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := trSubagent(t, root, "sdlc:verifier", "a1", nil, noEnv, nil); got != trCarryOn {
		t.Errorf("AC-13 SubagentStop without the lock was answered %q", got)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "A-1", model.RecordFile))
	if !bytes.Equal(before, after) {
		t.Error("AC-13 the gate record was written without the lock")
	}
	// AC-1: recording to the trace takes no store lock, so a lock nobody can
	// have does not stop it.
	trSend(t, "PostToolUse", trShell(root, "PostToolUse", "Bash", "go test ./...", map[string]any{"duration_ms": 1}), noEnv, nil)
	trOne(t, root, "shell")
}

// ------------------------------------------------------------------ AC-14

// AC-14: a shell command's record, and a refusal's, never wait on a
// transcript.
func TestAC14AShellCommandsRecordReadsNoTranscript(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	fake := &trFake{usage: trUsage()}
	trSend(t, "PostToolUse", trShell(root, "PostToolUse", "Bash", "go test ./...", map[string]any{"duration_ms": 1, "transcript_path": filepath.Join(root, "t.jsonl")}), noEnv, fake)
	trSend(t, "PostToolUseFailure", trShell(root, "PostToolUseFailure", "Bash", "go test ./...", map[string]any{"error": "Exit code 1", "transcript_path": filepath.Join(root, "t.jsonl")}), noEnv, fake)
	trSend(t, "PreToolUse", trPayload(root, "PreToolUse", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "rm .sdlc/state/active"}, "transcript_path": filepath.Join(root, "t.jsonl")}), noEnv, fake)
	if n := len(trOfKind(trRecords(t, root), "shell")); n != 2 {
		t.Errorf("two shell commands made %d records", n)
	}
	if n := len(trOfKind(trRecords(t, root), "refusal")); n != 1 {
		t.Errorf("one refusal made %d records", n)
	}
	if reads := fake.reads(); len(reads) != 0 {
		t.Errorf("AC-14 recording a shell command or a refusal read a transcript: %+v", reads)
	}
}

// ------------------------------------------------------------------ AC-15

// AC-15: in a session the story is not held in, with no story, or in a
// project that does not take part, no event writes anything, and each is
// answered as an event the hook has no business with.
func TestAC15WhereNoStoryIsHeldByTheSessionNothingIsWritten(t *testing.T) {
	t.Parallel()
	events := func(root, session string) map[string]string {
		p := func(event string, fields map[string]any) string {
			fields["session_id"] = session
			return trPayload(root, event, fields)
		}
		return map[string]string{
			"SubagentStart": p("SubagentStart", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "a1"}),
			"SubagentStop": p("SubagentStop", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "a1",
				"agent_transcript_path": filepath.Join(root, "t.jsonl"), "last_assistant_message": `{"status":"done"}`}),
			"PostToolUse":        p("PostToolUse", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "go test ./..."}, "duration_ms": 3}),
			"PostToolUseFailure": p("PostToolUseFailure", map[string]any{"tool_name": "Bash", "tool_input": map[string]any{"command": "go test ./..."}, "error": "Exit code 1"}),
			"Notification":       p("Notification", map[string]any{"notification_type": "permission_prompt"}),
			"StopFailure":        p("StopFailure", map[string]any{"error": "rate_limit"}),
			"PostCompact":        p("PostCompact", map[string]any{"trigger": "auto"}),
		}
	}
	for name, setup := range map[string]func(t *testing.T) (root, session string){
		"another session": func(t *testing.T) (string, string) { t.Helper(); return trProject(t), "session-b" },
		"no story": func(t *testing.T) (string, string) {
			t.Helper()
			root := trProject(t)
			if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
				t.Fatal(err)
			}
			return root, working
		},
		"a project that does not take part": func(t *testing.T) (string, string) {
			t.Helper()
			root := trProject(t)
			if err := os.Remove(filepath.Join(root, ".sdlc", "config.json")); err != nil {
				t.Fatal(err)
			}
			return root, working
		},
	} {
		t.Run("AC-15 "+name, func(t *testing.T) {
			t.Parallel()
			root, session := setup(t)
			record := filepath.Join(root, ".sdlc", "stories", "A-1", model.RecordFile)
			before, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			fake := &trFake{usage: trUsage()}
			for event, payload := range events(root, session) {
				if got := trSend(t, event, payload, noEnv, fake); got != trCarryOn {
					t.Errorf("AC-15 %s was answered %q, want %q", event, got, trCarryOn)
				}
			}
			trNothingWritten(t, root)
			if after, _ := os.ReadFile(record); !bytes.Equal(before, after) {
				t.Error("AC-15 the gate record was written")
			}
			if len(fake.reads()) != 0 {
				t.Errorf("AC-15 a transcript was read: %+v", fake.reads())
			}
		})
	}
	// The same events from the session that holds the story do write, so the
	// absence above is the rule's and not recording's being off.
	root := trProject(t)
	for event, payload := range events(root, working) {
		trSend(t, event, payload, noEnv, nil)
	}
	if len(trRecords(t, root)) < 6 {
		t.Errorf("AC-15 the session holding the story recorded %d lines for seven events", len(trRecords(t, root)))
	}
}

// ------------------------------------------------------------------ AC-16

func TestAC16APastLimitTraceTakesOneTruncatedRecordAndAnswersAreUnchanged(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	line := `{"v":1,"at":"2026-10-28T10:00:00.000Z","kind":"compaction","trigger":"auto"}` + "\n"
	limit := 4<<20 - 10
	full := strings.Repeat(line, limit/len(line))
	if rest := limit - len(full); rest > 0 {
		full += strings.Repeat("x", rest-1) + "\n"
	}
	write(t, root, ".sdlc/stories/A-1/trace.jsonl", full)

	trSend(t, "PostToolUse", trShell(root, "PostToolUse", "Bash", "go test ./...", map[string]any{"duration_ms": 1}), noEnv, nil)
	raw, err := os.ReadFile(trTracePath(root))
	if err != nil {
		t.Fatal(err)
	}
	tail := strings.TrimSuffix(string(raw[len(full):]), "\n")
	if strings.Count(tail, "\n") != 0 || !strings.Contains(tail, `"truncated"`) {
		t.Fatalf("AC-16 past the limit the trace took %q, want one truncated record", tail)
	}
	size := len(raw)
	if r := call(t, command(root, "sdlc-implementer", "rm .sdlc/state/tests.lock"), noEnv); !denied(r) {
		t.Error("AC-16 a refusal past the limit was let through")
	}
	trSubagent(t, root, "sdlc:verifier", "a1", nil, noEnv, nil)
	if again, _ := os.ReadFile(trTracePath(root)); len(again) != size {
		t.Errorf("AC-16 records were appended after the truncated record: %d bytes, then %d", size, len(again))
	}
	if len(trEventsOfType(t, root, "agent_start")) != 1 {
		t.Error("AC-16 a full trace stopped the gate record's agent events")
	}
}

// ------------------------------------------------------------------ AC-17

func TestAC17TheHookRefusesAWriteToTheTraceAndLetsAReadThrough(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	write(t, root, ".sdlc/stories/A-1/trace.jsonl", "")
	write(t, root, ".sdlc/stories/A-1/x/.keep", "")
	write(t, root, ".sdlc/stories/B-2/trace.jsonl", "")
	for _, path := range []string{".sdlc/stories/A-1/trace.jsonl", ".sdlc/stories/B-2/trace.jsonl"} {
		r := call(t, event(root, "Write", "sdlc:verifier", filepath.Join(root, path)), noEnv)
		if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "[trace-is-written-by-the-tool]") {
			t.Errorf("AC-17 a Write to %s was answered %+v", path, r.HookSpecificOutput)
		}
	}
	link := filepath.Join(root, ".sdlc", "stories", "A-1", "notes")
	linked := os.Symlink("trace.jsonl", link) == nil
	for _, cmd := range []string{
		"echo x > .sdlc/stories/A-1/x/../trace.jsonl",
		"echo x > .sdlc/stories/A-1/../A-1/trace.jsonl",
		"cd .sdlc/stories/A-1/x && echo x >> ../trace.jsonl",
	} {
		r := call(t, command(root, "", cmd), noEnv)
		if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "[trace-is-written-by-the-tool]") {
			t.Errorf("AC-17 %q was answered %+v", cmd, r.HookSpecificOutput)
		}
	}
	if linked {
		if r := call(t, command(root, "", "echo x >> .sdlc/stories/A-1/notes"), noEnv); !denied(r) {
			t.Error("AC-17 a write through a link to the trace was let through")
		}
	}
	for _, cmd := range []string{"cat .sdlc/stories/A-1/trace.jsonl", "tail -n 5 .sdlc/stories/B-2/trace.jsonl"} {
		if r := call(t, command(root, "sdlc:verifier", cmd), noEnv); denied(r) {
			t.Errorf("AC-17 reading the trace with %q was refused: %s", cmd, r.HookSpecificOutput.PermissionDecisionReason)
		}
	}
}

// THREATS T2: a link planted at the trace's name is not appended through by
// the hook, and the refusal it was recording still stands.
func TestAC17ALinkAtTheTracesNameIsNotWrittenThroughByTheHook(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	victim := filepath.Join(root, "internal", "frozen_test.go")
	write(t, root, "internal/frozen_test.go", "package internal\n")
	if err := os.Symlink(victim, trTracePath(root)); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	if r := call(t, command(root, "sdlc-implementer", "rm .sdlc/state/tests.lock"), noEnv); !denied(r) {
		t.Error("a refusal was let through because its record could not be written")
	}
	trSend(t, "PostToolUse", trShell(root, "PostToolUse", "Bash", "go test ./...", map[string]any{"duration_ms": 1}), noEnv, nil)
	trSubagent(t, root, "sdlc:verifier", "a1", nil, noEnv, nil)
	if raw, _ := os.ReadFile(victim); string(raw) != "package internal\n" {
		t.Errorf("T2 the file the link points at was written: %q", raw)
	}
	if len(trEventsOfType(t, root, "agent_start")) != 1 {
		t.Error("the agent's start was not recorded on the gate record, so the test shows nothing")
	}
}

// ------------------------------------------------------------------ THREATS

// THREATS T5 and ANALYSIS R1: the stop guard's count is of stops with nothing
// done between them. A subagent starting and stopping is recorded on the gate
// record, and is not progress: counted as progress, a session that kept
// starting subagents would never be handed to a person.
func TestAC2AgentEventsDoNotRestartTheStopCount(t *testing.T) {
	root := storyUnderWay(t)
	for i := 1; i <= 3; i++ {
		trSubagent(t, root, "sdlc:verifier", "a"+string(rune('0'+i)), nil, noEnv, nil)
		if r := stop(t, root, false, noEnv); r.Decision != "block" {
			t.Fatalf("stop %d went through before the limit", i)
		}
	}
	if n := len(trEventsOfType(t, root, "agent_start")); n != 3 {
		t.Fatalf("AC-2 three subagents left %d agent_start events, so the count was not put to the test", n)
	}
	trSubagent(t, root, "sdlc:verifier", "a4", nil, noEnv, nil)
	if r := stop(t, root, false, noEnv); !strings.Contains(r.SystemMessage, "sdlc approve A-1") {
		t.Errorf("T5 subagents starting and stopping between stops kept the story from a person: %+v", r)
	}
}

// THREATS S1, after the person's decision of 2026-10-29 (spec_unclear,
// option A). Rounds 2 and 3 had the hook record nothing where AI_AGENT was
// set, as Claude Code set it for a model's tools alone. Claude Code 2.1.309 in
// Claude Desktop puts AI_AGENT into the hook's own environment, on a prompt a
// person typed, so that hook recorded nothing in Desktop at all. For
// recording, the hook no longer asks AI_AGENT: every event it records is
// recorded whatever the variable holds, the real value among them, on the gate
// record and in the trace, and a refusal is recorded under its rule. S1 rests
// on the shell rules (forgedevent_test.go, oneline_test.go and
// hiddenbehind_test.go in shellpolicy).
//
// The slash route keeps its own check: with the same environment, a person's
// /sdlc:approve is still not run, and says so (fail closed; the person uses a
// `!` line). A hook that stopped asking AI_AGENT anywhere fails that half.
//
// G9 the other way round: the values are several, and the environment is
// AI_AGENT alone or Claude Desktop's hook with it, so a check that skipped
// recording for one value, or for a hook with Claude Code's markers, fails.
func TestAC2AC15AnEventIsRecordedWhateverAIAgentTheHooksEnvironmentHolds(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"claude-code_2-1-309_agent", "claude-code", "x", "0", "false"} {
		for _, desktop := range []bool{false, true} {
			shape := "AI_AGENT alone"
			if desktop {
				shape = "Claude Desktop's hook"
			}
			t.Run("AC-2 AC-15 "+shape+" AI_AGENT="+value, func(t *testing.T) {
				t.Parallel()
				root := trProject(t)
				getenv := env(map[string]string{"AI_AGENT": value})
				if desktop {
					getenv = realHookEnv(root, working, map[string]string{"AI_AGENT": value})
				}
				fake := &trFake{usage: trUsage()}
				if got := trSubagent(t, root, "sdlc:verifier", "a1", map[string]any{"last_assistant_message": `{"status":"done"}`}, getenv, fake); got != trCarryOn {
					t.Errorf("SubagentStop was answered %q", got)
				}
				for _, event := range []string{"PostToolUse", "PostToolUseFailure"} {
					trSend(t, event, trShell(root, event, "Bash", "go test ./...", map[string]any{"duration_ms": 1, "error": "Exit code 1"}), getenv, fake)
				}
				for _, event := range []string{"Notification", "StopFailure", "PostCompact"} {
					trSend(t, event, trPayload(root, event, map[string]any{"notification_type": "idle_prompt", "error": "rate_limit", "trigger": "auto"}), getenv, fake)
				}
				trSend(t, "Stop", trStopPayload(root, map[string]any{"prompt_id": "p-1"}), getenv, fake)
				if r := call(t, command(root, "sdlc-implementer", "rm .sdlc/state/tests.lock"), getenv); !denied(r) {
					t.Error("S1 a refusal was let through with AI_AGENT set")
				}

				for _, typ := range []string{"agent_start", "agent_end"} {
					if n := len(trEventsOfType(t, root, typ)); n != 1 {
						t.Errorf("AC-2 %d %s events on the gate record with AI_AGENT=%s, want 1", n, typ, value)
					}
				}
				records := trRecords(t, root)
				for kind, want := range map[string]int{
					"agent_run": 1, "shell": 2, "notification": 1, "api_error": 1, "compaction": 1, "turn": 1, "refusal": 1,
				} {
					if n := len(trOfKind(records, kind)); n != want {
						t.Errorf("AC-2 AC-15 %d %s records in the trace with AI_AGENT=%s, want %d", n, kind, value, want)
					}
				}
				if runs := trOfKind(records, "agent_run"); len(runs) == 1 && runs[0]["status"] != "done" {
					t.Errorf("AC-5 the run's hand-back status = %v, want done", runs[0]["status"])
				}
				if refusals := trOfKind(records, "refusal"); len(refusals) == 1 && refusals[0]["rule"] != "loop-state-through-the-tool" {
					t.Errorf("AC-7 the refusal was recorded under %v", refusals[0]["rule"])
				}
				if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "trace-read.json")); err != nil {
					t.Errorf("AC-6 the stop left no turn mark: %v", err)
				}

				// The slash route still refuses the person's command here.
				calls := 0
				r := expand(t, typed(root, working, "sdlc:approve", "A-1"), getenv, counted(&calls))
				if calls != 0 || !r.blocked() || !strings.Contains(r.shown(), "/sdlc:approve did not run") {
					t.Errorf("/sdlc:approve with AI_AGENT=%s ran %d commands and showed %q; the slash route's check was dropped with the recording's",
						value, calls, r.shown())
				}
			})
		}
	}
}
