package cli

// US-112 AC-18 to AC-22: `sdlc trace` splits a story's time and cost from its
// gate record and its trace.jsonl, and `--all` puts every story side by side.
// The records here are written by hand at fixed times, so every figure can be
// worked out on paper; AC-10's diff record is made by the real commit gate.

import (
	"encoding/json"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// t0 is when every story here was started.
var t0 = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return t0.Add(d) }

func ms(d time.Duration) string { return at(d).Format("2006-01-02T15:04:05.000Z") }

// traceStories puts stories with these ids in the backlog, shaped like the
// example `sdlc init` wrote.
func traceStories(t *testing.T, root string, ids ...string) {
	t.Helper()
	path := filepath.Join(root, "user_stories.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var backlog map[string]any
	if err := json.Unmarshal(raw, &backlog); err != nil {
		t.Fatal(err)
	}
	stories, _ := backlog["stories"].([]any)
	if len(stories) == 0 {
		t.Fatal("the backlog init wrote holds no story to copy")
	}
	example, _ := stories[0].(map[string]any)
	for _, id := range ids {
		s := maps.Clone(example)
		s["id"], s["title"], s["status"] = id, "Story "+id, "done"
		stories = append(stories, s)
	}
	backlog["stories"] = stories
	out, err := json.MarshalIndent(backlog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "user_stories.json", string(out)+"\n")
}

// gateRecord builds a record as the loop's commands leave it.
type gateRecord struct {
	r     *model.Record
	extra []map[string]any // agent events, which the model may not name yet
}

func newGateRecord(story string) *gateRecord {
	r := model.NewRecord(story, t0)
	r.Append("loop_start", "", t0)
	return &gateRecord{r: r}
}

func (g *gateRecord) pass(gate model.Gate, d time.Duration) *gateRecord {
	g.r.SetGate(gate, model.GatePass, "", at(d))
	g.r.Append(model.EventGate, model.GateEvent(gate, model.GatePass), at(d))
	return g
}

func (g *gateRecord) fail(gate model.Gate, d time.Duration) *gateRecord {
	g.r.SetGate(gate, model.GateFail, "", at(d))
	g.r.Append(model.EventGate, model.GateEvent(gate, model.GateFail), at(d))
	return g
}

func (g *gateRecord) event(kind, message string, d time.Duration) *gateRecord {
	g.r.Append(kind, message, at(d))
	return g
}

func (g *gateRecord) escalate(kind string, d, answered time.Duration) *gateRecord {
	g.r.Escalate(kind, "a question", "", at(d))
	g.r.Append("escalate", kind+": a question", at(d))
	g.r.Append("loop_end", "", at(d))
	g.r.Decide(true, "carry on", "", at(answered))
	g.r.Append(model.EventApproval, "approved", at(answered))
	return g
}

func (g *gateRecord) review(gate model.Gate, role string, v model.Verdict, round int, d time.Duration) *gateRecord {
	g.r.Reviews = append(g.r.Reviews, model.Review{At: model.Timestamp(at(d)), Gate: gate, Role: role, Verdict: v, Round: round})
	g.r.Append(model.EventReview, model.ReviewEvent(gate, role, v), at(d))
	return g
}

func (g *gateRecord) agent(typ, id string, from, to time.Duration) *gateRecord {
	g.extra = append(g.extra, map[string]any{"at": model.Timestamp(at(from)), "type": "agent_start", "agent_type": typ, "agent_id": id, "message": ""})
	if to >= 0 {
		g.extra = append(g.extra, map[string]any{"at": model.Timestamp(at(to)), "type": "agent_end", "agent_type": typ, "agent_id": id, "message": ""})
	}
	return g
}

// save writes the record, its agent events merged into its history by time.
func (g *gateRecord) save(t *testing.T, root string) {
	t.Helper()
	raw, err := json.Marshal(g.r)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	events, _ := doc["events"].([]any)
	for _, e := range g.extra {
		events = append(events, e)
	}
	stamp := func(e any) string { s, _ := e.(map[string]any)["at"].(string); return s }
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && stamp(events[j]) < stamp(events[j-1]); j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
	doc["events"] = events
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, ".sdlc/stories/"+g.r.Story+"/"+model.RecordFile, string(out))
}

func traceLines(t *testing.T, root, story string, lines ...string) {
	t.Helper()
	writeFile(t, root, ".sdlc/stories/"+story+"/trace.jsonl", strings.Join(lines, "\n")+"\n")
}

// record is one trace line, from its fields.
func record(t *testing.T, kind string, d time.Duration, fields map[string]any) string {
	t.Helper()
	m := map[string]any{"v": 1, "at": ms(d), "kind": kind}
	maps.Copy(m, fields)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func tok(input, read, written, output int) map[string]any {
	return map[string]any{"input": input, "cache_read": read, "cache_written": written, "output": output}
}

// The document `sdlc trace --json` prints, as far as these tests read it.
// A figure that is not recorded is null, which a pointer tells from zero.
type traceSpan struct {
	Gate        string   `json:"gate"`
	Wall        *float64 `json:"wall"`
	Agents      *float64 `json:"agents"`
	Person      *float64 `json:"person"`
	Between     *float64 `json:"between"`
	Unaccounted *float64 `json:"unaccounted"`
}

type traceTokens struct {
	Input        float64 `json:"input"`
	CacheRead    float64 `json:"cache_read"`
	CacheWritten float64 `json:"cache_written"`
	Output       float64 `json:"output"`
}

type traceAgentType struct {
	AgentType      string                 `json:"agent_type"`
	Runs           float64                `json:"runs"`
	Time           float64                `json:"time"`
	Models         map[string]traceTokens `json:"models"`
	Responses      float64                `json:"responses"`
	PeakContext    float64                `json:"peak_context"`
	CacheReadRatio *float64               `json:"cache_read_ratio"`
	Tools          map[string]float64     `json:"tools"`
	Asked          []struct {
		Model  string `json:"model"`
		Effort string `json:"effort"`
	} `json:"asked"`
	ObservedModels  []string `json:"observed_models"`
	ObservedEfforts []string `json:"observed_efforts"`
	Done            float64  `json:"done"`
	Blocked         float64  `json:"blocked"`
}

type traceDoc struct {
	Story        string      `json:"story"`
	Traced       bool        `json:"traced"`
	SkippedLines float64     `json:"skipped_lines"`
	Truncated    bool        `json:"truncated"`
	TruncatedAt  string      `json:"truncated_at"`
	Total        traceSpan   `json:"total"`
	Gates        []traceSpan `json:"gates"`
	Unfinished   []struct {
		AgentType string `json:"agent_type"`
		AgentID   string `json:"agent_id"`
		Start     string `json:"start"`
	} `json:"unfinished"`
	AgentTypes []traceAgentType `json:"agent_types"`
	Main       []struct {
		Gate   string                 `json:"gate"`
		Models map[string]traceTokens `json:"models"`
	} `json:"main"`
	Shell []struct {
		Class   string  `json:"class"`
		Count   float64 `json:"count"`
		Total   float64 `json:"total"`
		Longest float64 `json:"longest"`
	} `json:"shell"`
	Tests *struct {
		Count    float64 `json:"count"`
		Time     float64 `json:"time"`
		Passes   float64 `json:"passes"`
		Failures float64 `json:"failures"`
	} `json:"tests"`
	Refusals []struct {
		Rule      string  `json:"rule"`
		AgentType string  `json:"agent_type"`
		Code      string  `json:"code"`
		Count     float64 `json:"count"`
	} `json:"refusals"`
	Diff *struct {
		Files       float64 `json:"files"`
		Added       float64 `json:"added"`
		Removed     float64 `json:"removed"`
		TestFiles   float64 `json:"test_files"`
		TestAdded   float64 `json:"test_added"`
		TestRemoved float64 `json:"test_removed"`
	} `json:"diff"`
	Reviews []struct {
		Gate   string  `json:"gate"`
		Rounds float64 `json:"rounds"`
		Blocks float64 `json:"blocks"`
	} `json:"reviews"`
}

func (d traceDoc) gate(t *testing.T, g model.Gate) traceSpan {
	t.Helper()
	for _, s := range d.Gates {
		if s.Gate == string(g) {
			return s
		}
	}
	t.Fatalf("no row for %s among %+v", g, d.Gates)
	return traceSpan{}
}

func (d traceDoc) agentType(t *testing.T, typ string) traceAgentType {
	t.Helper()
	for _, a := range d.AgentTypes {
		if a.AgentType == typ {
			return a
		}
	}
	t.Fatalf("no row for %s among %+v", typ, d.AgentTypes)
	return traceAgentType{}
}

func seconds(t *testing.T, what string, got *float64, want time.Duration) {
	t.Helper()
	if got == nil {
		t.Errorf("%s is not recorded, want %v", what, want)
		return
	}
	if math.Abs(*got-want.Seconds()) > 0.001 {
		t.Errorf("%s = %vs, want %vs", what, *got, want.Seconds())
	}
}

// tracedStory is T-1, worked for four hours, as the comments in it add up.
func tracedStory(t *testing.T) {
	t.Helper()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-1", "T-9")
	newGateRecord("T-1").
		pass(model.GateDoR, 10*time.Minute).
		// Working towards analysis: two agents, 12m-30m and 20m-40m, 28m between them.
		agent("sdlc:researcher", "a1", 12*time.Minute, 30*time.Minute).
		agent("Explore", "a2", 20*time.Minute, 40*time.Minute).
		pass(model.GateAnalysis, 45*time.Minute).
		// Working towards tests_frozen: a question open for two hours, ten
		// minutes until the next iteration, then an agent for thirty.
		escalate("spec_unclear", 50*time.Minute, 2*time.Hour+50*time.Minute).
		event("loop_start", "", 3*time.Hour).
		agent("sdlc:sdet", "a3", 3*time.Hour+5*time.Minute, 3*time.Hour+35*time.Minute).
		// Never ended: listed as unfinished and left out.
		agent("sdlc:verifier", "a4", 3*time.Hour+40*time.Minute, -1).
		review(model.GateTestsFrozen, "verifier", model.VerdictBlock, 1, 3*time.Hour+50*time.Minute).
		review(model.GateTestsFrozen, "verifier", model.VerdictApprove, 2, 3*time.Hour+55*time.Minute).
		pass(model.GateTestsFrozen, 4*time.Hour).
		save(t, root)
	traceLines(t, root, "T-1",
		record(t, "turn", 20*time.Minute, map[string]any{"prompt_id": "p1", "models": map[string]any{"m-main": tok(1000, 0, 0, 10)}, "responses": 2}),
		record(t, "agent_run", 30*time.Minute, map[string]any{
			"agent_type": "sdlc:researcher", "agent_id": "a1", "start": ms(12 * time.Minute), "end": ms(30 * time.Minute),
			"effort": "high", "asked_model": "opus", "asked_effort": "high",
			"models": map[string]any{"m-opus": tok(100, 300, 100, 50)}, "responses": 4, "peak_context": 400,
			"tools": map[string]any{"Read": 3}, "status": "done",
		}),
		`{"v":1,"at":"2026-10-01T10:31:00.000Z","kind":"agent_ru`,
		record(t, "agent_run", 40*time.Minute, map[string]any{
			"agent_type": "Explore", "agent_id": "a2", "start": ms(20 * time.Minute), "end": ms(40 * time.Minute),
			"models": map[string]any{"m-haiku": tok(10, 0, 0, 5)}, "responses": 1, "peak_context": 10, "tools": map[string]any{"Grep": 1},
		}),
		record(t, "a_kind_from_a_newer_sdlc", 41*time.Minute, map[string]any{"x": 1}),
		record(t, "shell", 42*time.Minute, map[string]any{"class": "git", "program": "git", "duration_ms": 10}),
		record(t, "shell", 43*time.Minute, map[string]any{"class": "git", "program": "git", "duration_ms": 30}),
		record(t, "refusal", 44*time.Minute, map[string]any{"rule": "loop-state-through-the-tool", "tool": "Bash", "code": "SDLC-E0081"}),
		record(t, "turn", 3*time.Hour+10*time.Minute, map[string]any{"prompt_id": "p2", "models": map[string]any{"m-main": tok(500, 0, 0, 5)}, "responses": 1}),
		record(t, "shell", 3*time.Hour+20*time.Minute, map[string]any{"agent_type": "sdlc:sdet", "class": "test", "program": "go", "duration_ms": 1000, "outcome": "pass"}),
		record(t, "shell", 3*time.Hour+25*time.Minute, map[string]any{"agent_type": "sdlc:sdet", "class": "test", "program": "go", "duration_ms": 3000, "outcome": "fail", "exit_code": 1}),
		record(t, "refusal", 3*time.Hour+26*time.Minute, map[string]any{"rule": "gate-record-is-written-by-the-tool", "agent_type": "sdlc:sdet", "tool": "Write"}),
		record(t, "refusal", 3*time.Hour+27*time.Minute, map[string]any{"rule": "gate-record-is-written-by-the-tool", "agent_type": "sdlc:sdet", "tool": "Edit"}),
		record(t, "agent_run", 3*time.Hour+35*time.Minute, map[string]any{
			"agent_type": "sdlc:sdet", "agent_id": "a3", "start": ms(3*time.Hour + 5*time.Minute), "end": ms(3*time.Hour + 35*time.Minute),
			"effort": "medium", "asked_model": "sonnet", "asked_effort": "high",
			"models": map[string]any{"m-sonnet": tok(200, 600, 200, 100)}, "responses": 6, "peak_context": 900,
			"tools": map[string]any{"Write": 2, "Bash": 4}, "status": "blocked",
		}),
		record(t, "diff", 4*time.Hour, map[string]any{"files": 5, "added": 100, "removed": 20, "test_files": 2, "test_added": 60, "test_removed": 0}),
	)
}

func TestAC18AC20TraceSplitsAStorysTimeIntoAgentsAPersonIterationsAndTheRest(t *testing.T) {
	t.Parallel()
	tracedStory(t)
	r := run(t, "trace", "T-1", "--json")
	if r.code != 0 {
		t.Fatalf("AC-18 sdlc trace exited %d:\n%s", r.code, r.stderr)
	}
	d := decode[traceDoc](t, r)
	if d.Story != "T-1" || !d.Traced {
		t.Errorf("AC-20 story %q, traced %v", d.Story, d.Traced)
	}
	seconds(t, "AC-18 agents' time, the union of the runs", d.Total.Agents, 58*time.Minute)
	seconds(t, "AC-18 waiting on a person", d.Total.Person, 2*time.Hour)
	seconds(t, "AC-18 time between iterations", d.Total.Between, 10*time.Minute)
	if d.Total.Wall == nil || d.Total.Unaccounted == nil {
		t.Fatalf("AC-18 the wall time or the rest is not recorded: %+v", d.Total)
	}
	if rest := *d.Total.Wall - (58*time.Minute + 2*time.Hour + 10*time.Minute).Seconds(); math.Abs(*d.Total.Unaccounted-rest) > 0.001 {
		t.Errorf("AC-18 the rest is %v, want the wall time less the three others, %v", *d.Total.Unaccounted, rest)
	}
	// Charged to the gate the story was working towards at the time.
	seconds(t, "AC-18 analysis's agents' time", d.gate(t, model.GateAnalysis).Agents, 28*time.Minute)
	seconds(t, "AC-18 tests_frozen's agents' time", d.gate(t, model.GateTestsFrozen).Agents, 30*time.Minute)
	seconds(t, "AC-18 tests_frozen's wait on a person", d.gate(t, model.GateTestsFrozen).Person, 2*time.Hour)
	seconds(t, "AC-18 tests_frozen's time between iterations", d.gate(t, model.GateTestsFrozen).Between, 10*time.Minute)
	seconds(t, "AC-18 dor's agents' time", d.gate(t, model.GateDoR).Agents, 0)
	seconds(t, "AC-18 analysis's wait on a person", d.gate(t, model.GateAnalysis).Person, 0)
	seconds(t, "AC-18 dor's wall time", d.gate(t, model.GateDoR).Wall, 10*time.Minute)
	seconds(t, "AC-18 analysis's wall time", d.gate(t, model.GateAnalysis).Wall, 35*time.Minute)
	if len(d.Gates) == 0 || d.Gates[0].Gate != string(model.GateDoR) {
		t.Errorf("AC-18 the gates are not in the loop's order: %+v", d.Gates)
	}
	if len(d.Unfinished) != 1 || d.Unfinished[0].AgentID != "a4" || d.Unfinished[0].AgentType != "sdlc:verifier" {
		t.Errorf("AC-21 the run that never ended is not listed as unfinished: %+v", d.Unfinished)
	}
	if d.SkippedLines != 2 || d.Truncated {
		t.Errorf("AC-20 skipped %v lines and truncated %v, want 2 and false", d.SkippedLines, d.Truncated)
	}
}

func TestAC18TraceSplitsTheCostByAgentType(t *testing.T) {
	t.Parallel()
	tracedStory(t)
	d := decode[traceDoc](t, mustRun(t, "trace", "T-1", "--json"))
	res := d.agentType(t, "sdlc:researcher")
	if res.Runs != 1 || math.Abs(res.Time-(18*time.Minute).Seconds()) > 0.001 {
		t.Errorf("AC-18 the researcher's runs %v and time %v, want 1 and 1080", res.Runs, res.Time)
	}
	if got := res.Models["m-opus"]; got.Input != 100 || got.CacheRead != 300 || got.CacheWritten != 100 || got.Output != 50 {
		t.Errorf("AC-18 the researcher's tokens = %+v", res.Models)
	}
	if res.Responses != 4 || res.PeakContext != 400 || res.Tools["Read"] != 3 {
		t.Errorf("AC-18 the researcher's responses %v, peak %v, tools %v", res.Responses, res.PeakContext, res.Tools)
	}
	// 300 over 100 + 300 + 100.
	if res.CacheReadRatio == nil || math.Abs(*res.CacheReadRatio-0.6) > 0.0001 {
		t.Errorf("AC-3 AC-18 the researcher's cache-read ratio = %v, want 0.6", res.CacheReadRatio)
	}
	if len(res.Asked) != 1 || res.Asked[0].Model != "opus" || res.Asked[0].Effort != "high" {
		t.Errorf("AC-18 asked for = %+v", res.Asked)
	}
	if len(res.ObservedModels) != 1 || res.ObservedModels[0] != "m-opus" || len(res.ObservedEfforts) != 1 || res.ObservedEfforts[0] != "high" {
		t.Errorf("AC-18 observed models %v and efforts %v", res.ObservedModels, res.ObservedEfforts)
	}
	if res.Done != 1 || res.Blocked != 0 {
		t.Errorf("AC-18 the researcher's hand-backs: %v done, %v blocked", res.Done, res.Blocked)
	}
	if sdet := d.agentType(t, "sdlc:sdet"); sdet.Blocked != 1 || sdet.Done != 0 || sdet.Asked[0].Model != "sonnet" {
		t.Errorf("AC-18 the sdet's row = %+v", sdet)
	}
	if explore := d.agentType(t, "Explore"); explore.Done != 0 || explore.Blocked != 0 || explore.Models["m-haiku"].Output != 5 {
		t.Errorf("AC-18 Explore's row = %+v", explore)
	}
	main := map[string]traceTokens{}
	for _, m := range d.Main {
		main[m.Gate] = m.Models["m-main"]
	}
	if main["analysis"].Input != 1000 || main["tests_frozen"].Input != 500 {
		t.Errorf("AC-18 the main conversation's tokens per gate = %+v", d.Main)
	}
}

func TestAC18TraceSumsShellCommandsTestRunsRefusalsTheDiffAndReviews(t *testing.T) {
	t.Parallel()
	tracedStory(t)
	d := decode[traceDoc](t, mustRun(t, "trace", "T-1", "--json"))
	classes := map[string][3]float64{}
	for _, s := range d.Shell {
		classes[s.Class] = [3]float64{s.Count, s.Total, s.Longest}
	}
	if classes["git"] != [3]float64{2, 0.04, 0.03} || classes["test"] != [3]float64{2, 4, 3} {
		t.Errorf("AC-18 shell commands per class (count, total s, longest s) = %v", classes)
	}
	if d.Tests == nil || d.Tests.Count != 2 || d.Tests.Time != 4 || d.Tests.Passes != 1 || d.Tests.Failures != 1 {
		t.Errorf("AC-18 test runs = %+v, want 2 runs, 4 s, 1 pass and 1 failure", d.Tests)
	}
	found := false
	for _, r := range d.Refusals {
		if r.Rule == "gate-record-is-written-by-the-tool" && r.AgentType == "sdlc:sdet" && r.Count == 2 {
			found = true
		}
		if r.Rule == "loop-state-through-the-tool" && (r.Code != "SDLC-E0081" || r.AgentType != "" || r.Count != 1) {
			t.Errorf("AC-18 the main conversation's refusal = %+v", r)
		}
	}
	if !found {
		t.Errorf("AC-18 refusals by rule, agent type and code = %+v", d.Refusals)
	}
	if d.Diff == nil || d.Diff.Files != 5 || d.Diff.Added != 100 || d.Diff.Removed != 20 || d.Diff.TestFiles != 2 || d.Diff.TestAdded != 60 {
		t.Errorf("AC-18 the diff = %+v", d.Diff)
	}
	var rounds, blocks float64
	for _, r := range d.Reviews {
		if r.Gate == string(model.GateTestsFrozen) {
			rounds, blocks = r.Rounds, r.Blocks
		}
	}
	if rounds != 2 || blocks != 1 {
		t.Errorf("AC-18 tests_frozen's reviews = %v rounds and %v blocks, want 2 and 1: %+v", rounds, blocks, d.Reviews)
	}
}

// AC-18 and AC-20: the same figures for a person to read, and exit 0; JSON
// has nothing else on stdout.
func TestAC18AC20TraceSaysTheSameInWordsAndPrintsOnlyJSONWhenAsked(t *testing.T) {
	t.Parallel()
	tracedStory(t)
	r := mustRun(t, "trace", "T-1")
	for _, want := range []string{"T-1", "dor", "analysis", "tests_frozen", "sdlc:researcher", "Explore", "sdlc:sdet",
		"m-opus", "gate-record-is-written-by-the-tool", "SDLC-E0081", "unfinished", "skipped"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("AC-18 the output does not mention %q:\n%s", want, r.stdout)
		}
	}
	if strings.Contains(r.stdout, "predates tracing") {
		t.Errorf("AC-21 a traced story is said to predate tracing:\n%s", r.stdout)
	}
	j := mustRun(t, "trace", "T-1", "--json")
	var v any
	dec := json.NewDecoder(strings.NewReader(j.stdout))
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("AC-20 --json printed something that is not JSON: %v\n%s", err, j.stdout)
	}
	if dec.More() {
		t.Errorf("AC-20 --json printed more than one document:\n%s", j.stdout)
	}
}

// AC-21: a story from before tracing still shows its wall time, waiting and
// time between iterations; everything else is not recorded, never zero.
func TestAC21AStoryWithNoTraceShowsWhatTheGateRecordHoldsAndNothingInvented(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-1")
	newGateRecord("T-1").
		pass(model.GateDoR, 10*time.Minute).
		escalate("spec_unclear", 20*time.Minute, 80*time.Minute).
		event("loop_start", "", 90*time.Minute).
		pass(model.GateAnalysis, 2*time.Hour).
		save(t, root)
	recordPath := filepath.Join(root, ".sdlc", "stories", "T-1", model.RecordFile)
	before, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	d := decode[traceDoc](t, mustRun(t, "trace", "T-1", "--json"))
	if d.Traced {
		t.Error("AC-21 a story with no trace is said to have one")
	}
	seconds(t, "AC-21 waiting on a person", d.Total.Person, time.Hour)
	seconds(t, "AC-21 time between iterations", d.Total.Between, 10*time.Minute)
	if d.Total.Wall == nil {
		t.Error("AC-21 the wall time is not shown")
	}
	if d.Total.Agents != nil || d.Total.Unaccounted != nil {
		t.Errorf("AC-21 agents' time %v and the rest %v are given, want not recorded", d.Total.Agents, d.Total.Unaccounted)
	}
	if d.AgentTypes != nil || d.Tests != nil || d.Diff != nil || d.Shell != nil || d.Refusals != nil || d.Main != nil {
		t.Errorf("AC-21 recorded measures are given for a story with no trace: %+v", d)
	}
	text := mustRun(t, "trace", "T-1").stdout
	if strings.Count(text, "predates tracing") != 1 {
		t.Errorf("AC-21 the output does not say once that the story predates tracing:\n%s", text)
	}
	if !strings.Contains(text, "not recorded") {
		t.Errorf("AC-21 the output does not say what is not recorded:\n%s", text)
	}
	after, _ := os.ReadFile(recordPath)
	if string(before) != string(after) {
		t.Error("AC-21 reading the story wrote to its record")
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "stories", "T-1", "trace.jsonl")); err == nil {
		t.Error("AC-21 a trace was invented for a story that had none")
	}
}

// AC-16 and AC-20: a truncated trace is said to be one.
func TestAC16AC20ATruncatedTraceIsSaidToBeMissingRecords(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-1")
	newGateRecord("T-1").pass(model.GateDoR, 10*time.Minute).save(t, root)
	traceLines(t, root, "T-1",
		record(t, "shell", time.Minute, map[string]any{"class": "git", "program": "git", "duration_ms": 10}),
		record(t, "truncated", 2*time.Minute, map[string]any{"size": 4194300}),
	)
	d := decode[traceDoc](t, mustRun(t, "trace", "T-1", "--json"))
	if !d.Truncated || d.TruncatedAt != ms(2*time.Minute) {
		t.Errorf("AC-20 truncated %v at %q, want true at %s", d.Truncated, d.TruncatedAt, ms(2*time.Minute))
	}
	if text := mustRun(t, "trace", "T-1").stdout; !strings.Contains(text, "missing") {
		t.Errorf("AC-16 the output does not say that records after the limit are missing:\n%s", text)
	}
}

// AC-22: a story the backlog does not hold, or one with no record yet, is
// refused with the code sdlc gives for an id it cannot use (ANALYSIS R4).
func TestAC22TraceRefusesAStoryItHasNoRecordOf(t *testing.T) {
	t.Parallel()
	tracedStory(t)
	for _, id := range []string{"NOPE-1", "T-9"} {
		r := run(t, "trace", id)
		if r.code == 0 {
			t.Errorf("AC-22 sdlc trace %s exited 0", id)
		}
		if r.stdout != "" {
			t.Errorf("AC-22 sdlc trace %s printed on stdout: %q", id, r.stdout)
		}
		for _, want := range []string{"SDLC-E0009", id, "sdlc story list", "started"} {
			if !strings.Contains(r.stderr, want) {
				t.Errorf("AC-22 the refusal of %s does not say %q:\n%s", id, want, r.stderr)
			}
		}
		if j := run(t, "trace", id, "--json"); j.code == 0 || strings.Contains(j.stdout, `"story"`) {
			t.Errorf("AC-22 sdlc trace %s --json gave a trace: %q", id, j.stdout)
		}
	}
	// THREATS E4: an id is checked before it becomes a path.
	if r := run(t, "trace", "../../etc"); r.code == 0 || r.stdout != "" {
		t.Errorf("an id that is a path was read: exit %d, %q", r.code, r.stdout)
	}
}

// AC-19 and AC-20: one row per story with a record, medians of the time and
// token columns over the stories that have them, and the share that passed
// Gates 6 and 7 first time.
func TestAC19TraceAllPutsEveryStorySideBySide(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-2", "T-3", "T-4")
	walk := func(id string, end time.Duration, blockAt model.Gate) *gateRecord {
		g := newGateRecord(id)
		for i, gate := range model.Gates {
			d := time.Duration(i+1) * time.Minute
			if gate == blockAt {
				g.review(gate, "verifier", model.VerdictBlock, 1, d)
			}
			if gate == model.GateRetro {
				continue
			}
			g.pass(gate, d)
		}
		g.pass(model.GateRetro, end-time.Minute)
		g.event("landed", "", end)
		return g
	}
	// T-2: ten hours, one escalation, one unfreeze, clean through 6 and 7.
	walk("T-2", 10*time.Hour, "").escalate("frozen_test_wrong", 30*time.Minute, 90*time.Minute).
		event("unfreeze", "a person lifted the freeze", 95*time.Minute).save(t, root)
	// T-3: two hours, from before tracing.
	walk("T-3", 2*time.Hour, "").save(t, root)
	// T-4: six hours, a block at the verifier's review.
	walk("T-4", 6*time.Hour, model.GateVerifierReview).fail(model.GateVerification, 6*time.Hour-30*time.Second).save(t, root)
	traceLines(t, root, "T-2", record(t, "agent_run", time.Hour, map[string]any{
		"agent_type": "sdlc:sdet", "agent_id": "a", "models": map[string]any{"m": tok(100, 200, 300, 400)}, "responses": 1, "peak_context": 600,
	}))
	traceLines(t, root, "T-4", record(t, "turn", time.Hour, map[string]any{"models": map[string]any{"m": tok(1000, 1000, 500, 500)}, "responses": 1}))

	r := run(t, "trace", "--all", "--json")
	if r.code != 0 {
		t.Fatalf("AC-19 sdlc trace --all exited %d:\n%s", r.code, r.stderr)
	}
	var doc struct {
		Stories []struct {
			Story             string             `json:"story"`
			Start             string             `json:"start"`
			End               string             `json:"end"`
			Wall              *float64           `json:"wall"`
			Agents            *float64           `json:"agents"`
			Person            *float64           `json:"person"`
			Escalations       map[string]float64 `json:"escalations"`
			Unfreezes         float64            `json:"unfreezes"`
			ReviewRounds      map[string]float64 `json:"review_rounds"`
			FirstPass         bool               `json:"first_pass"`
			DesignToEnd       *float64           `json:"design_to_end"`
			FailedAfterCommit bool               `json:"failed_after_commit"`
			Tokens            *float64           `json:"tokens"`
			PeakContext       *float64           `json:"peak_context"`
			TestTime          *float64           `json:"test_time"`
			Refusals          *float64           `json:"refusals"`
			DiffFiles         *float64           `json:"diff_files"`
			DiffLines         *float64           `json:"diff_lines"`
			SkippedLines      float64            `json:"skipped_lines"`
			Truncated         bool               `json:"truncated"`
		} `json:"stories"`
		Medians        map[string]*float64 `json:"medians"`
		FirstPassShare float64             `json:"first_pass_share"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Fatalf("AC-20 --all --json is not one JSON document: %v\n%s", err, r.stdout)
	}
	rows := map[string]int{}
	for i, s := range doc.Stories {
		rows[s.Story] = i
	}
	for _, id := range []string{"T-2", "T-3", "T-4"} {
		if _, ok := rows[id]; !ok {
			t.Fatalf("AC-19 no row for %s: %+v", id, doc.Stories)
		}
	}
	t2, t3, t4 := doc.Stories[rows["T-2"]], doc.Stories[rows["T-3"]], doc.Stories[rows["T-4"]]
	seconds(t, "AC-19 T-2's wall time", t2.Wall, 10*time.Hour)
	seconds(t, "AC-19 T-3's wall time", t3.Wall, 2*time.Hour)
	seconds(t, "AC-19 T-2's wait on a person", t2.Person, time.Hour)
	if t2.Start != model.Timestamp(t0) && t2.Start != ms(0) {
		t.Errorf("AC-19 T-2 started %q, want %s", t2.Start, model.Timestamp(t0))
	}
	if t2.End != model.Timestamp(at(10*time.Hour)) && t2.End != ms(10*time.Hour) {
		t.Errorf("AC-19 T-2 ended %q, want the landing at %s", t2.End, model.Timestamp(at(10*time.Hour)))
	}
	if t2.Escalations["frozen_test_wrong"] != 1 || t2.Unfreezes != 1 {
		t.Errorf("AC-19 T-2's escalations %v and unfreezes %v", t2.Escalations, t2.Unfreezes)
	}
	if !t2.FirstPass || !t3.FirstPass || t4.FirstPass {
		t.Errorf("AC-19 first-pass success: T-2 %v, T-3 %v, T-4 %v; want true, true, false", t2.FirstPass, t3.FirstPass, t4.FirstPass)
	}
	if t4.ReviewRounds[string(model.GateVerifierReview)] != 1 {
		t.Errorf("AC-19 T-4's review rounds per gate = %v", t4.ReviewRounds)
	}
	if t2.DesignToEnd == nil || t2.FailedAfterCommit {
		t.Errorf("AC-19 T-2's time from design review to the end %v, failed after commit %v", t2.DesignToEnd, t2.FailedAfterCommit)
	}
	if !t4.FailedAfterCommit {
		t.Error("AC-19 T-4 failed a gate after its commit gate passed, and the row does not say so")
	}
	if t2.Tokens == nil || *t2.Tokens != 1000 || t4.Tokens == nil || *t4.Tokens != 3000 {
		t.Errorf("AC-19 tokens in all: T-2 %v, T-4 %v; want 1000 and 3000", t2.Tokens, t4.Tokens)
	}
	if t3.Tokens != nil || t3.PeakContext != nil || t3.TestTime != nil || t3.Refusals != nil || t3.DiffFiles != nil || t3.DiffLines != nil || t3.Agents != nil {
		t.Errorf("AC-19 a story with no trace shows recorded columns: %+v", t3)
	}
	// The tokens' median leaves T-3 out: of 1000 and 3000, not of 0, 1000 and 3000.
	if m := doc.Medians["tokens"]; m == nil || *m != 2000 {
		t.Errorf("AC-19 the median of tokens = %v, want 2000", m)
	}
	seconds(t, "AC-19 the median wall time", doc.Medians["wall"], 6*time.Hour)
	if math.Abs(doc.FirstPassShare-2.0/3.0) > 0.001 {
		t.Errorf("AC-19 the share of first-pass stories = %v, want 2/3", doc.FirstPassShare)
	}
	text := mustRun(t, "trace", "--all").stdout
	for _, want := range []string{"T-2", "T-3", "T-4", "not recorded", "median"} {
		if !strings.Contains(text, want) {
			t.Errorf("AC-19 sdlc trace --all does not show %q:\n%s", want, text)
		}
	}
}

// AC-10: the commit gate measures the change, in all and for the frozen
// tests, and names no file.
func TestAC10TheCommitGateRecordsTheSizeOfTheChange(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	reach(t, root, model.GateImplementation)
	// The code is written at its gate, so the reviews after it are of it.
	writeFile(t, root, "internal/invoice.go", "package internal\n\nfunc Invoice() {}\n")
	for _, gate := range model.Gates[slices.Index(model.Gates, model.GateImplementation):slices.Index(model.Gates, model.GateCommit)] {
		satisfy(t, root, gate)
		mustRun(t, "gate", string(gate), "pass", "--note", "reached by the test")
	}
	satisfy(t, root, model.GateCommit)
	mustRun(t, "gate", "commit", "pass", "--note", "measured")

	story := decode[statusPayload](t, mustRun(t, "status", "--json")).Active
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", story, "trace.jsonl"))
	if err != nil {
		t.Fatalf("AC-10 the commit gate wrote no trace: %v", err)
	}
	var diffs []map[string]any
	for line := range strings.SplitSeq(strings.TrimSuffix(string(raw), "\n"), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["kind"] == "diff" {
			diffs = append(diffs, m)
		}
	}
	if len(diffs) != 1 {
		t.Fatalf("AC-10 want one diff record, found %d in:\n%s", len(diffs), raw)
	}
	d := diffs[0]
	num := func(k string) float64 { n, _ := d[k].(float64); return n }
	// The frozen test is three lines, the code three more.
	if num("test_files") != 1 || num("test_added") != 3 || num("test_removed") != 0 {
		t.Errorf("AC-10 the frozen tests' share = %v", d)
	}
	if num("files") < 2 || num("added") < 6 {
		t.Errorf("AC-10 the change in all = %v, want at least the test and the code", d)
	}
	if strings.Contains(string(raw), "invoice") || strings.Contains(string(raw), "internal/") {
		t.Errorf("AC-10 a file name was written: %s", raw)
	}
}

// AC-26: `sdlc init` ignores every story's trace and nothing else of the
// loop's, so telemetry stays on the machine that made it.
func TestAC26InitIgnoresTheTraceAndKeepsWhatTheProjectAlreadyIgnores(t *testing.T) {
	t.Parallel()
	root := gitProject(t)
	writeFile(t, root, ".gitignore", "/build/\n")
	mustRun(t, "init")
	ignored := func(rel string) bool {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", "check-ignore", "-q", "--no-index", rel)
		cmd.Dir = root
		return cmd.Run() == nil
	}
	for _, rel := range []string{".sdlc/stories/US-001/trace.jsonl", ".sdlc/stories/A-1/trace.jsonl", "build/x"} {
		if !ignored(rel) {
			t.Errorf("AC-26 %s is not ignored after sdlc init", rel)
		}
	}
	for _, rel := range []string{".sdlc/stories/US-001/gate-record.json", ".sdlc/stories/US-001/ANALYSIS.md", ".sdlc/config.json", "user_stories.json", "trace.jsonl"} {
		if ignored(rel) {
			t.Errorf("AC-26 init ignored %s, which is the project's decision", rel)
		}
	}
	// Run again with --force, as an update: still ignored, and said once.
	mustRun(t, "init", "--force")
	if !ignored(".sdlc/stories/US-001/trace.jsonl") {
		t.Error("AC-26 the trace is not ignored after init --force")
	}
	count := 0
	for _, rel := range []string{".gitignore", ".sdlc/.gitignore", ".sdlc/stories/.gitignore"} {
		raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		count += strings.Count(string(raw), "trace.jsonl")
	}
	if count != 1 {
		t.Errorf("AC-26 the ignore rule for the trace is written %d times across the ignore files, want once", count)
	}
}

// AC-26: this repository ignores the trace too.
func TestAC26ThisRepositoryIgnoresTheTrace(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		if l := strings.TrimSpace(line); !strings.HasPrefix(l, "#") && strings.HasSuffix(l, ".sdlc/stories/*/trace.jsonl") {
			return
		}
	}
	t.Error("AC-26 .gitignore has no line for .sdlc/stories/*/trace.jsonl")
}

// AC-25: what the docs say of the trace.
func TestAC25TheDocsDescribeTheTrace(t *testing.T) {
	t.Parallel()
	commands := page(t, "commands.md")
	for _, want := range []string{"sdlc trace <ID>", "sdlc trace --all", "--json", "predates tracing", "median"} {
		if !strings.Contains(commands, want) {
			t.Errorf("AC-25 docs/commands.md does not say %q", want)
		}
	}
	loop := page(t, "the-loop.md")
	start := strings.Index(loop, "## Where everything lives")
	if start < 0 {
		t.Fatal(`docs/the-loop.md has no "Where everything lives"`)
	}
	where := loop[start:]
	if next := strings.Index(where[3:], "\n## "); next >= 0 {
		where = where[:next+3]
	}
	for _, want := range []string{"trace.jsonl", "agent_start", "agent_end", "agent_run", "turn", "refusal", "shell", "diff",
		"notification", "api_error", "compaction", "truncated", "4 MiB"} {
		if !strings.Contains(where, want) {
			t.Errorf("AC-25 docs/the-loop.md's \"Where everything lives\" does not name %q", want)
		}
	}
	config := page(t, "configuration.md")
	if !strings.Contains(loop+config, "asked_model") || !strings.Contains(loop+config, "asked_effort") {
		t.Error("AC-25 neither docs/the-loop.md nor docs/configuration.md says what asked_model and asked_effort mean")
	}
	enforcement := page(t, "enforcement.md")
	for _, want := range []string{"trace.jsonl", "`trace-is-written-by-the-tool`", "sdlc trace"} {
		if !strings.Contains(enforcement, want) {
			t.Errorf("AC-25 docs/enforcement.md does not say %q", want)
		}
	}
}
