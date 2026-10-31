package hook

// US-112, after design review round 1. The red team named the cheap
// implementations the first set of tests let through (G1, G8, G11 and G12,
// O3 and O4), and security asked for names from a payload or a transcript to
// be bounded (I3) and for a hand-run hook to be refused (B1). Each test here
// fails the cheap implementation its comment names.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/hook/claudecode"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
	"github.com/bbsnly/sdlc/internal/trace"
)

// trSendHosted hands the hook one event with the host given, and returns what
// it printed.
func trSendHosted(t *testing.T, event, payload string, getenv func(string) string, h Host) string {
	t.Helper()
	var out bytes.Buffer
	if code := RunHosted([]string{event}, strings.NewReader(payload), &out, io.Discard, getenv, h); code != 0 {
		t.Fatalf("the hook exited %d on %s", code, event)
	}
	return out.String()
}

// trWithEvents writes A-1's gate record, past Gate 1, with these events
// added to its history as raw objects, so that fields the model does not name
// yet are written all the same.
func trWithEvents(t *testing.T, root string, events ...map[string]any) {
	t.Helper()
	record := model.NewRecord("A-1", time.Now().Add(-3*time.Hour))
	record.SetGate(model.GateDoR, model.GatePass, "", time.Now().Add(-3*time.Hour))
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	all, _ := doc["events"].([]any)
	for _, e := range events {
		all = append(all, e)
	}
	doc["events"] = all
	write(t, root, ".sdlc/stories/A-1/gate-record.json", mustMarshal(doc))
}

// ------------------------------------------------------------------ G1

// AC-3, G1: a run's start is when its agent_start was recorded, not when it
// stopped and not when the hook got round to it. A run whose start the record
// does not hold has no start, never a guessed one.
func TestAC3AnAgentRunStartsWhenItsAgentStartWasRecorded(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	started := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	trWithEvents(t, root,
		map[string]any{"at": model.Timestamp(started.Add(-time.Hour)), "type": "agent_start", "agent_type": "sdlc:verifier", "agent_id": "a0", "message": ""},
		map[string]any{"at": model.Timestamp(started), "type": "agent_start", "agent_type": "sdlc:verifier", "agent_id": "a1", "message": ""},
	)
	for _, id := range []string{"a1", "a7"} {
		trSend(t, "SubagentStop", trPayload(root, "SubagentStop", map[string]any{
			"agent_type": "sdlc:verifier", "agent_id": id, "last_assistant_message": "Done.",
		}), noEnv, nil)
	}
	runs := map[string]map[string]any{}
	for _, r := range trOfKind(trRecords(t, root), "agent_run") {
		id, _ := r["agent_id"].(string)
		runs[id] = r
	}
	if len(runs) != 2 {
		t.Fatalf("AC-3 two stops made runs %v", runs)
	}
	if start := trTime(t, runs["a1"], "start"); !start.Equal(started) {
		t.Errorf("AC-3 a1's run started %v, want %v, when its agent_start was recorded", start, started)
	}
	trTime(t, runs["a1"], "end")
	trAbsent(t, runs["a7"], "start")
	trTime(t, runs["a7"], "end")
}

// ------------------------------------------------------------------ G8

// trTimeField is slog's time attribute, which differs on every line.
var trTimeField = regexp.MustCompile(`time=\S+ `)

// AC-4, G8: the debug log says which of AC-4's four cases kept the counts
// out, through the reader cmd/sdlc hands the hook, and never the path. One
// constant line ("transcript not read") does not say which; the reader's
// own error text, os.Open's for one, holds the path.
func TestAC4TheDebugLogSaysWhichOfTheFourCasesKeptATranscriptsCountsOut(t *testing.T) {
	cases := trUnreadableTranscripts()
	said := map[string]string{}
	for _, c := range cases {
		root := trProject(t)
		dir := filepath.Join(root, "transcripts")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "agent-a1.jsonl")
		c.plant(t, path)
		log := debugLog(t)
		h := Host{Transcripts: claudecode.ReadTranscript}
		trSendHosted(t, "SubagentStart", trPayload(root, "SubagentStart", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "a1"}), noEnv, h)
		if got := trSendHosted(t, "SubagentStop", trPayload(root, "SubagentStop", map[string]any{
			"agent_type": "sdlc:verifier", "agent_id": "a1", "agent_transcript_path": path, "last_assistant_message": "Done.",
		}), noEnv, h); got != trCarryOn {
			t.Errorf("AC-4 %s: SubagentStop was answered %q", c.name, got)
		}
		trAbsent(t, trOne(t, root, "agent_run"), "models", "responses", "peak_context", "tools")
		lines := linesHolding(log.String(), "transcript")
		if len(lines) == 0 {
			t.Errorf("AC-4 %s: the debug log does not say why the counts are missing:\n%s", c.name, log)
			continue
		}
		for _, line := range lines {
			if strings.Contains(line, dir) || strings.Contains(line, "agent-a1.jsonl") || strings.Contains(line, filepath.ToSlash(dir)) {
				t.Errorf("AC-4 %s: the debug log holds the transcript's path: %s", c.name, line)
			}
		}
		said[c.name] = trTimeField.ReplaceAllString(strings.Join(lines, "\n"), "")
	}
	for i, a := range cases {
		for _, b := range cases[i+1:] {
			if said[a.name] != "" && said[a.name] == said[b.name] {
				t.Errorf("AC-4 the debug log says the same for %q and %q, so it does not say which it was:\n%s", a.name, b.name, said[a.name])
			}
		}
	}
}

// ------------------------------------------------------------------ G11, O3

// trFlaky is a reader that fails on the calls it is told to, and otherwise
// says each read stopped 1,000 bytes after the last one that did not.
type trFlaky struct {
	mu    sync.Mutex
	calls []trRead
	fail  map[int]bool // by call, from 1
	next  int64
}

func (f *trFlaky) read(_ context.Context, path string, from int64) (trace.Usage, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, trRead{path, from})
	if f.fail[len(f.calls)] {
		return trace.Usage{}, 0, errors.New("the read timed out")
	}
	f.next += 1000
	return trUsage(), f.next, nil
}

// AC-6, G11 and O3a: a read that fails says it stopped at 0, and a mark set
// from that would count the whole session again at the next stop. The mark
// stays where the last read that worked left it.
func TestAC6AFailedReadLeavesTheTurnMarkWhereItWas(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	fake := &trFlaky{fail: map[int]bool{2: true}}
	for range 3 {
		trSendHosted(t, "Stop", trStopPayload(root, map[string]any{"stop_hook_active": true}), noEnv, Host{Transcripts: fake.read})
	}
	if len(fake.calls) != 3 {
		t.Fatalf("AC-6 three stops read the transcript %d times", len(fake.calls))
	}
	if fake.calls[0].from != 0 || fake.calls[1].from != 1000 || fake.calls[2].from != 1000 {
		t.Errorf("AC-6 the turns read from %d, %d and %d; want 0, then 1000, and 1000 again after the read that failed",
			fake.calls[0].from, fake.calls[1].from, fake.calls[2].from)
	}
}

// AC-6, G11 and O3b: the mark is the transcript's own. A session that stops
// in one transcript, then another, then the first again, reads each from
// where that transcript's last read stopped; one mark for all of them would
// read the first again from 0 and count every turn already recorded.
func TestAC6EachTranscriptKeepsATurnMarkOfItsOwn(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	a, b := filepath.Join(root, "transcripts", "a.jsonl"), filepath.Join(root, "transcripts", "b.jsonl")
	fake := &trFlaky{}
	for _, path := range []string{a, b, a, b} {
		trSendHosted(t, "Stop", trStopPayload(root, map[string]any{"stop_hook_active": true, "transcript_path": path}), noEnv, Host{Transcripts: fake.read})
	}
	want := []trRead{{a, 0}, {b, 0}, {a, 1000}, {b, 2000}}
	if !slices.Equal(fake.calls, want) {
		t.Errorf("AC-6 two transcripts in turn were read as %+v, want %+v", fake.calls, want)
	}
	if n := len(trOfKind(trRecords(t, root), "turn")); n != 4 {
		t.Errorf("AC-6 four stops made %d turn records", n)
	}
}

// ------------------------------------------------------------------ O4

// AC-2 and AC-13, O4: five reviewers ending at once are five SubagentStops
// at once, each loading, adding to and saving the gate record under the
// store's lock. Every agent_end lands, and every run is recorded.
func TestAC2AC13FiveSubagentsStoppingAtOnceEachRecordTheirEnd(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	fake := &trFake{usage: trUsage()}
	ids := []string{"r1", "r2", "r3", "r4", "r5"}
	for _, id := range ids {
		trSend(t, "SubagentStart", trPayload(root, "SubagentStart", map[string]any{"agent_type": "sdlc:security", "agent_id": id}), noEnv, fake)
	}
	var wg sync.WaitGroup
	answers := make(chan string, len(ids))
	for _, id := range ids {
		payload := trPayload(root, "SubagentStop", map[string]any{
			"agent_type": "sdlc:security", "agent_id": id, "last_assistant_message": `{"status":"done"}`,
			"agent_transcript_path": filepath.Join(root, "transcripts", id+".jsonl"),
		})
		wg.Go(func() {
			var out bytes.Buffer
			if code := RunHosted([]string{"SubagentStop"}, strings.NewReader(payload), &out, io.Discard, noEnv, Host{Transcripts: fake.read}); code != 0 {
				answers <- fmt.Sprintf("exit %d", code)
				return
			}
			answers <- out.String()
		})
	}
	wg.Wait()
	close(answers)
	for got := range answers {
		if got != trCarryOn {
			t.Errorf("AC-13 a SubagentStop among five was answered %q", got)
		}
	}
	var ended []string
	for _, e := range trEventsOfType(t, root, "agent_end") {
		id, _ := e["agent_id"].(string)
		ended = append(ended, id)
	}
	slices.Sort(ended)
	if !slices.Equal(ended, ids) {
		t.Errorf("AC-2 five subagents stopping at once left agent_end for %v, want all of %v", ended, ids)
	}
	if n := len(trEventsOfType(t, root, "agent_start")); n != len(ids) {
		t.Errorf("AC-2 the five starts became %d agent_start events: a save lost another's", n)
	}
	if n := len(trOfKind(trRecords(t, root), "agent_run")); n != len(ids) {
		t.Errorf("AC-3 five stops made %d agent_run records", n)
	}
}

// trJumpClock is a wait for the store's lock that runs out at once: each look
// at it is a minute after the last.
type trJumpClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *trJumpClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(time.Minute)
	return c.now
}

func (*trJumpClock) Sleep(time.Duration) {}

// trLockFree reports whether the project's store lock could be had at once.
func trLockFree(root string) bool {
	g, err := store.LockOn(root, "test probe", &trJumpClock{now: time.Now()})
	if err != nil {
		return false
	}
	g.Release()
	return true
}

// trLockProbe is a reader that notes, at each read, whether the store's lock
// was free and how long the read was given.
type trLockProbe struct {
	mu    sync.Mutex
	root  string
	free  []bool
	given []time.Duration
	bound []bool
}

func (p *trLockProbe) read(ctx context.Context, _ string, _ int64) (trace.Usage, int64, error) {
	free := trLockFree(p.root)
	deadline, bounded := ctx.Deadline()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.free = append(p.free, free)
	p.bound = append(p.bound, bounded)
	p.given = append(p.given, time.Until(deadline))
	return trUsage(), 1000, nil
}

// AC-14 and THREATS D3, O4 and the perf review: a transcript is read with
// the store's lock let go, so a read of up to five seconds never holds up the
// other reviewers' agent events, `sdlc gate` or the stop guard; and each read,
// a subagent's and the main conversation's turn alike, is given five seconds
// at most.
func TestAC2AC14ATranscriptIsReadWithTheStoreLockFreeAndForFiveSecondsAtMost(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	held, err := store.Lock(root, "test")
	if err != nil {
		t.Fatal(err)
	}
	seen := !trLockFree(root)
	held.Release()
	if !seen {
		t.Fatal("the probe does not see a lock that is held, so the test shows nothing")
	}
	probe := &trLockProbe{root: root}
	h := Host{Transcripts: probe.read}
	trSendHosted(t, "SubagentStart", trPayload(root, "SubagentStart", map[string]any{"agent_type": "sdlc:verifier", "agent_id": "a1"}), noEnv, h)
	trSendHosted(t, "SubagentStop", trPayload(root, "SubagentStop", map[string]any{
		"agent_type": "sdlc:verifier", "agent_id": "a1", "agent_transcript_path": filepath.Join(root, "transcripts", "a1.jsonl"),
	}), noEnv, h)
	trSendHosted(t, "Stop", trStopPayload(root, nil), noEnv, h)
	if len(probe.free) != 2 {
		t.Fatalf("a SubagentStop and a Stop read %d transcripts, want 2", len(probe.free))
	}
	for i, what := range []string{"the subagent's transcript", "the main conversation's"} {
		if !probe.free[i] {
			t.Errorf("O4 %s was read while the store's lock was held", what)
		}
		if !probe.bound[i] || probe.given[i] > 5*time.Second {
			t.Errorf("AC-14 %s was read with no bound or more than five seconds: bounded %v, %v left", what, probe.bound[i], probe.given[i])
		}
	}
	if len(trEventsOfType(t, root, "agent_end")) != 1 || len(trOfKind(trRecords(t, root), "agent_run")) != 1 {
		t.Error("the run was not recorded, so the test shows nothing")
	}
}

// ------------------------------------------------------------------ G12, E1

// trAnswerProbe is stdout, noting what the story's trace and the turn mark
// held when the answer was first written to it.
type trAnswerProbe struct {
	mu      sync.Mutex
	root    string
	out     bytes.Buffer
	written bool
	trace   string
	marked  bool
}

func (p *trAnswerProbe) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.written {
		p.written = true
		raw, _ := os.ReadFile(trTracePath(p.root))
		p.trace = string(raw)
		_, err := os.Stat(filepath.Join(p.root, ".sdlc", "state", "trace-read.json"))
		p.marked = err == nil
	}
	return p.out.Write(b)
}

// AC-7, AC-13, THREATS E1 and G12: the answer is on stdout before anything is
// recorded. A refusal record written first, by code with no recover of its
// own, would turn a panic in recording into the crash's carry-on: an allowed
// call where the rule said no. Whether that recording has a recover of its
// own is read in the code at Gate 6; this holds the order.
func TestAC7AC13TheAnswerIsOnStdoutBeforeTheRefusalOrTheTurnIsRecorded(t *testing.T) {
	t.Parallel()
	t.Run("AC-7 AC-13 a refusal", func(t *testing.T) {
		t.Parallel()
		root := trProject(t)
		probe := &trAnswerProbe{root: root}
		if code := Run([]string{"PreToolUse"}, strings.NewReader(command(root, "sdlc-implementer", "rm .sdlc/state/tests.lock")), probe, io.Discard, noEnv); code != 0 {
			t.Fatalf("the hook exited %d", code)
		}
		if !strings.Contains(probe.out.String(), `"deny"`) {
			t.Fatalf("the call was not refused: %q", probe.out.String())
		}
		if strings.Contains(probe.trace, `"refusal"`) {
			t.Errorf("G12 the refusal was recorded before it was answered: %s", probe.trace)
		}
		trOne(t, root, "refusal")
	})
	t.Run("AC-6 AC-13 a stop", func(t *testing.T) {
		t.Parallel()
		root := trProject(t)
		probe := &trAnswerProbe{root: root}
		fake := &trFake{usage: trUsage()}
		if code := RunHosted([]string{"Stop"}, strings.NewReader(trStopPayload(root, map[string]any{"prompt_id": "p-1"})), probe, io.Discard, noEnv, Host{Transcripts: fake.read}); code != 0 {
			t.Fatalf("the hook exited %d", code)
		}
		if strings.Contains(probe.trace, `"turn"`) || probe.marked {
			t.Errorf("E1 the turn was recorded, or its mark moved, before the stop was answered: %s", probe.trace)
		}
		trOne(t, root, "turn")
	})
}

// ------------------------------------------------------------------ I3

// trName is what a recorded name may be, as far as these tests pin it: no
// space, no control character, nothing outside ASCII, and at most 128 bytes.
// Whether `/` or `@` is kept in a model's name is the plan's to say: a
// Vertex or OpenRouter model is named with them.
var trName = regexp.MustCompile(`^[A-Za-z0-9:._@/+-]{1,128}$`)

const (
	// trToolCap and trModelCap are the most entries a record's tools and
	// models may have, `other` among them. At those, an agent_run whose every
	// name is 128 bytes is still under AC-1's 4,096.
	trToolCap  = 16
	trModelCap = 4
)

// trWide is a transcript's counts with names as long as may be kept, more of
// them than a record keeps, and names that may not be kept at all.
func trWide() (u trace.Usage, validTools, allTools int, validInput, allInput int64) {
	u = trace.Usage{Models: map[string]trace.Tokens{}, Tools: map[string]int{}, Responses: 50, PeakContext: 9000}
	for i := range 40 {
		name := fmt.Sprintf("t%02d", i) + strings.Repeat("t", 125)
		u.Tools[name] = i + 1
		validTools += i + 1
	}
	for i := range 10 {
		name := fmt.Sprintf("m%02d", i) + strings.Repeat("m", 125)
		u.Models[name] = trace.Tokens{Input: int64(i + 1), CacheRead: 1, CacheWritten: 1, Output: 1}
		validInput += int64(i + 1)
	}
	allTools, allInput = validTools, validInput
	for _, bad := range []string{"Bad Name MARKER", "line\nMARKER", "é-MARKER", strings.Repeat("w", 129), ""} {
		u.Tools[bad] = 1000
		allTools += 1000
		u.Models[bad] = trace.Tokens{Input: 1000}
		allInput += 1000
	}
	return u, validTools, allTools, validInput, allInput
}

// trBounded holds one record's tools and models to I3: few enough, every
// name a name, the rest folded into `other` and not dropped.
func trBounded(t *testing.T, r map[string]any, validTools, allTools int, validInput, allInput int64) {
	t.Helper()
	tools, _ := r["tools"].(map[string]any)
	models, _ := r["models"].(map[string]any)
	if len(tools) == 0 || len(models) == 0 {
		t.Fatalf("I3 a %s record has no tools or no models: %v", r["kind"], r)
	}
	if len(tools) > trToolCap || len(models) > trModelCap {
		t.Errorf("I3 a %s record keeps %d tools and %d models, want at most %d and %d", r["kind"], len(tools), len(models), trToolCap, trModelCap)
	}
	if _, ok := tools["other"]; !ok {
		t.Errorf("I3 the tools past the cap were not folded into other: %v", tools)
	}
	if _, ok := models["other"]; !ok {
		t.Errorf("I3 the models past the cap were not folded into other: %v", models)
	}
	if trNumber(tools, "t39"+strings.Repeat("t", 125)) != 40 {
		t.Errorf("I3 the most-used tool is not kept by its name: %v", tools)
	}
	sum := 0
	for name, n := range tools {
		if name != "other" && !trName.MatchString(name) {
			t.Errorf("I3 a tool name that is not a name was kept: %q", name)
		}
		v, _ := n.(float64)
		sum += int(v)
	}
	if sum != validTools && sum != allTools {
		t.Errorf("I3 the tool calls add up to %d, want %d (or %d with the unnamed folded in): some were dropped", sum, validTools, allTools)
	}
	var input int64
	for name, tok := range models {
		if name != "other" && !trName.MatchString(name) {
			t.Errorf("I3 a model name that is not a name was kept: %q", name)
		}
		m, _ := tok.(map[string]any)
		input += int64(trNumber(m, "input"))
	}
	if input != validInput && input != allInput {
		t.Errorf("I3 the input tokens add up to %d, want %d (or %d with the unnamed folded in): some were dropped", input, validInput, allInput)
	}
}

// trNothingHolds fails where any file under the project's .sdlc holds one of
// these.
func trNothingHolds(t *testing.T, root string, never ...string) {
	t.Helper()
	err := filepath.WalkDir(filepath.Join(root, ".sdlc"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, s := range never {
			if strings.Contains(string(raw), s) {
				t.Errorf("I3 %s holds %q", path, s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// AC-1, AC-12 and THREATS I3: tool and model names come from a transcript,
// written by a model; agent types and ids from a payload. Each is kept only
// where it is a short name, and is absent otherwise. A run that used more
// tools or models than a record keeps has the rest folded into `other` and is
// recorded, never dropped whole for being over 4,096 bytes; its most-used
// tool keeps its name.
func TestAC1AC12NamesFromAPayloadOrATranscriptAreBoundedAndTheRestFoldIntoOther(t *testing.T) {
	t.Parallel()
	t.Run("AC-1 AC-12 a run and a turn with too many and too long names", func(t *testing.T) {
		t.Parallel()
		root := trProject(t)
		usage, validTools, allTools, validInput, allInput := trWide()
		fake := &trFake{usage: usage}
		longType, longID := strings.Repeat("v", 128), strings.Repeat("i", 128)
		trSubagent(t, root, longType, longID, nil, noEnv, fake)
		runs := trOfKind(trRecords(t, root), "agent_run")
		if len(runs) != 1 {
			t.Fatalf("AC-1 a run with many long names made %d agent_run records: it was dropped, not folded", len(runs))
		}
		if runs[0]["agent_type"] != longType || runs[0]["agent_id"] != longID {
			t.Errorf("I3 a 128-byte agent type and id were not kept: %v, %v", runs[0]["agent_type"], runs[0]["agent_id"])
		}
		trBounded(t, runs[0], validTools, allTools, validInput, allInput)
		trSend(t, "Stop", trStopPayload(root, map[string]any{"prompt_id": "p-1"}), noEnv, fake)
		turns := trOfKind(trRecords(t, root), "turn")
		if len(turns) != 1 {
			t.Fatalf("AC-1 a turn with many long names made %d turn records", len(turns))
		}
		trBounded(t, turns[0], validTools, allTools, validInput, allInput)
		trNothingHolds(t, root, "MARKER", strings.Repeat("w", 129))
	})
	t.Run("AC-12 an agent type or id that is not a name", func(t *testing.T) {
		t.Parallel()
		root := trProject(t)
		badTypes := []string{"sdlc:verifier MARKER", "x\nMARKER", strings.Repeat("w", 129), ""}
		for i, typ := range badTypes {
			trSubagent(t, root, typ, fmt.Sprintf("b%d", i), nil, noEnv, nil)
		}
		trSubagent(t, root, "sdlc:verifier", "a1 MARKER", nil, noEnv, nil)
		trSubagent(t, root, "sdlc:verifier", "../../MARKER", nil, noEnv, nil)
		trSend(t, "PostToolUse", trShell(root, "PostToolUse", "Bash", "go test ./...", map[string]any{"agent_type": "sdlc:sdet MARKER", "duration_ms": 1}), noEnv, nil)
		if r := call(t, command(root, "sdlc-implementer\nMARKER", "rm .sdlc/state/tests.lock"), noEnv); !denied(r) {
			t.Fatal("the call was let through")
		}
		trNothingHolds(t, root, "MARKER", strings.Repeat("w", 129))
		starts := trEventsOfType(t, root, "agent_start")
		byID := map[string]map[string]any{}
		for _, e := range starts {
			id, _ := e["agent_id"].(string)
			byID[id] = e
		}
		for i := range badTypes {
			e, ok := byID[fmt.Sprintf("b%d", i)]
			if !ok {
				t.Errorf("I3 a subagent with an agent type that is not a name was not recorded at all: %v", starts)
				continue
			}
			if v, _ := e["agent_type"].(string); v != "" {
				t.Errorf("I3 agent_start keeps the agent type %q", v)
			}
		}
		for _, r := range trRecords(t, root) {
			if v, ok := r["agent_type"]; ok && !trName.MatchString(fmt.Sprint(v)) {
				t.Errorf("I3 a %s record keeps the agent type %q", r["kind"], v)
			}
		}
		if n := len(trOfKind(trRecords(t, root), "shell")); n != 1 {
			t.Errorf("the shell command made %d records", n)
		}
		if n := len(trOfKind(trRecords(t, root), "refusal")); n != 1 {
			t.Errorf("the refusal made %d records", n)
		}
	})
}

// ------------------------------------------------------------------ B1

// AC-2 and AC-15, B1: through the hook itself, a model's Bash that runs the
// hook by hand on an event it records, or that takes AI_AGENT away, is
// refused, from the session holding the story and from another; the hook run
// on PreToolUse is not. The refusal in the session holding the story is
// recorded under its rule.
func TestAC2AC15TheHookRefusesAHandRunOfARecordedEventAndAnAIAgentTakenAway(t *testing.T) {
	t.Parallel()
	root := trProject(t)
	from := func(session, cmd string) string {
		var e map[string]any
		if err := json.Unmarshal([]byte(command(root, "", cmd)), &e); err != nil {
			t.Fatal(err)
		}
		e["session_id"] = session
		return mustMarshal(e)
	}
	for _, session := range []string{working, "session-b"} {
		for cmd, rule := range map[string]string{
			"sdlc hook SubagentStop < /tmp/p.json":                 "recorded-event-comes-from-the-host",
			"sdlc hook Stop < /tmp/p.json":                         "recorded-event-comes-from-the-host",
			"env -u AI_AGENT sdlc hook PreToolUse < /tmp/p.json":   "enforcement-stays-on",
			"AI_AGENT= sdlc hook PreToolUse < /tmp/p.json":         "enforcement-stays-on",
			"unset AI_AGENT; sdlc hook PreToolUse < /tmp/p.json":   "enforcement-stays-on",
			"cat /tmp/p.json | plugin/bin/sdlc-hook SubagentStart": "recorded-event-comes-from-the-host",
		} {
			r := call(t, from(session, cmd), noEnv)
			if !denied(r) || !strings.Contains(r.HookSpecificOutput.PermissionDecisionReason, "["+rule+"]") {
				t.Errorf("B1 %s ran %q and was answered %+v, want a refusal under %s", session, cmd, r.HookSpecificOutput, rule)
			}
		}
		if r := call(t, from(session, "sdlc hook PreToolUse < /tmp/p.json"), noEnv); denied(r) {
			t.Errorf("B1 %s was refused the hook on PreToolUse: %s", session, r.HookSpecificOutput.PermissionDecisionReason)
		}
	}
	found := false
	for _, r := range trOfKind(trRecords(t, root), "refusal") {
		found = found || r["rule"] == "recorded-event-comes-from-the-host"
	}
	if !found {
		t.Error("AC-7 the refusal of a hand-run hook was not recorded")
	}
}
