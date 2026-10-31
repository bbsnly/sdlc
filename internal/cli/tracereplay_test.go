package cli

// US-112 AC-18 to AC-20, after design review round 1. The red team named the
// cheap readings the first fixtures let through: text that is the JSON with
// two sentences added (G2), time between iterations taken only after an
// escalation (G3), a run charged whole to the gate in force at its start and
// a reopened gate ignored (G4), the rest worked out as a sum that goes below
// zero where spans overlap (G5), and --all columns left null or first-pass
// success that ignores a review block (G6). The human advocate asked that a
// story still under way not be shown as ended, nor counted in the medians.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// traceOf is one story's --json document, from a fresh project holding only
// the record g.
func traceOf(t *testing.T, g *gateRecord, lines ...string) traceDoc {
	t.Helper()
	root := gitProjectInitialised(t)
	traceStories(t, root, g.r.Story)
	g.save(t, root)
	if len(lines) > 0 {
		traceLines(t, root, g.r.Story, lines...)
	}
	return decode[traceDoc](t, mustRun(t, "trace", g.r.Story, "--json"))
}

// AC-18, G2: the text is for a person: not JSON, with nothing of the JSON
// document in it, a row for each gate with its own figures, in the loop's
// order, and the rest named as AC-18 names it.
func TestAC18TheTextIsNotTheJSONAndHasARowForEachGate(t *testing.T) {
	t.Parallel()
	tracedStory(t)
	text := mustRun(t, "trace", "T-1").stdout
	if json.Valid([]byte(text)) || strings.Contains(text, `"gates":`) || strings.Contains(text, `"agent_types":`) {
		t.Fatalf("AC-18 sdlc trace without --json printed JSON:\n%s", text)
	}
	for line := range strings.SplitSeq(text, "\n") {
		if l := strings.TrimSpace(line); strings.HasPrefix(l, "{") && json.Valid([]byte(l)) {
			t.Errorf("AC-18 a line of the text is a JSON document: %s", l)
		}
	}
	if !strings.Contains(text, "no agent and no person accounts for") {
		t.Errorf("AC-18 the rest is not named as time no agent and no person accounts for:\n%s", text)
	}
	row := func(gate string, figures ...string) int {
		t.Helper()
		for i, line := range strings.Split(text, "\n") {
			if !strings.Contains(line, gate) {
				continue
			}
			for _, f := range figures {
				if strings.Contains(line, f) {
					return i
				}
			}
		}
		t.Errorf("AC-18 no line names %s with its wall time (any of %q):\n%s", gate, figures, text)
		return -1
	}
	// dor took 10 minutes, analysis 35, tests_frozen three hours and 15.
	dor, analysis := row("dor", "10m", "0:10", "600"), row("analysis", "35m", "0:35", "2100")
	frozen := row("tests_frozen", "3h15m", "3:15", "11700")
	if dor >= 0 && analysis >= 0 && frozen >= 0 && (dor >= analysis || analysis >= frozen) {
		t.Errorf("AC-18 the gates' rows are not in the loop's order:\n%s", text)
	}
}

// AC-18, G3: an iteration that ended with no question open and started again
// eight hours later is eight hours between iterations, not time nobody
// accounts for.
func TestAC18TimeBetweenIterationsCountsALoopEndWithNoQuestion(t *testing.T) {
	t.Parallel()
	d := traceOf(t, newGateRecord("T-1").
		pass(model.GateDoR, 10*time.Minute).
		event("loop_end", "", 20*time.Minute).
		event("loop_start", "", 8*time.Hour+20*time.Minute).
		pass(model.GateAnalysis, 8*time.Hour+30*time.Minute))
	seconds(t, "AC-18 time between iterations", d.Total.Between, 8*time.Hour)
	seconds(t, "AC-18 analysis's time between iterations", d.gate(t, model.GateAnalysis).Between, 8*time.Hour)
	seconds(t, "AC-18 waiting on a person", d.Total.Person, 0)
	seconds(t, "AC-18 analysis's rest", d.gate(t, model.GateAnalysis).Unaccounted, 20*time.Minute)
}

// AC-18, G4: time is charged to the gate the story was working towards at
// each instant. A run across a gate's pass is split at it, and a gate
// reopened takes the time back.
func TestAC18ARunAcrossAPassIsSplitAndAReopenedGateTakesTheTimeBack(t *testing.T) {
	t.Parallel()
	g := newGateRecord("T-1").
		pass(model.GateDoR, 10*time.Minute).
		pass(model.GateAnalysis, 20*time.Minute).
		pass(model.GateTestsFrozen, 30*time.Minute).
		agent("sdlc:implementer", "a1", 35*time.Minute, 45*time.Minute).
		pass(model.GatePlan, 40*time.Minute)
	g.r.Reopen(model.GateTestsFrozen, "the freeze was lifted", at(50*time.Minute))
	g.agent("sdlc:sdet", "a2", 55*time.Minute, 65*time.Minute).
		pass(model.GateTestsFrozen, 70*time.Minute).
		pass(model.GatePlan, 80*time.Minute).
		pass(model.GateDesignReview, 90*time.Minute)
	d := traceOf(t, g)
	seconds(t, "AC-18 plan's agents' time, the run up to the pass", d.gate(t, model.GatePlan).Agents, 5*time.Minute)
	seconds(t, "AC-18 design_review's agents' time, the run after the pass", d.gate(t, model.GateDesignReview).Agents, 5*time.Minute)
	seconds(t, "AC-18 tests_frozen's agents' time, after it was reopened", d.gate(t, model.GateTestsFrozen).Agents, 10*time.Minute)
	seconds(t, "AC-18 tests_frozen's wall time, before it passed and after it was reopened", d.gate(t, model.GateTestsFrozen).Wall, 30*time.Minute)
	seconds(t, "AC-18 agents' time in all", d.Total.Agents, 20*time.Minute)
}

// AC-18, G5: an agent working while a question is open counts once. The rest
// is the wall time less the union of agents, a person and the time between
// iterations, never their sum, and is never below zero.
func TestAC18TheRestIsTheWallLessTheUnionAndNeverBelowZero(t *testing.T) {
	t.Parallel()
	d := traceOf(t, newGateRecord("T-1").
		pass(model.GateDoR, 10*time.Minute).
		agent("sdlc:researcher", "a1", 20*time.Minute, 100*time.Minute).
		escalate("spec_unclear", 30*time.Minute, 90*time.Minute).
		event("loop_start", "", 90*time.Minute).
		pass(model.GateAnalysis, 2*time.Hour))
	seconds(t, "AC-18 agents' time", d.Total.Agents, 80*time.Minute)
	seconds(t, "AC-18 waiting on a person", d.Total.Person, time.Hour)
	seconds(t, "AC-18 the rest, two hours less 20m to 100m", d.Total.Unaccounted, 40*time.Minute)
	for _, s := range append([]traceSpan{d.Total}, d.Gates...) {
		if s.Unaccounted != nil && *s.Unaccounted < 0 {
			t.Errorf("AC-18 %q has %vs that nobody accounts for, below zero", s.Gate, *s.Unaccounted)
		}
	}
}

// allRow is one story's row of `sdlc trace --all --json`.
type allRow struct {
	Story       string   `json:"story"`
	End         *string  `json:"end"`
	Wall        *float64 `json:"wall"`
	Agents      *float64 `json:"agents"`
	FirstPass   bool     `json:"first_pass"`
	DesignToEnd *float64 `json:"design_to_end"`
	Tokens      *float64 `json:"tokens"`
	PeakContext *float64 `json:"peak_context"`
	TestTime    *float64 `json:"test_time"`
	Refusals    *float64 `json:"refusals"`
	DiffFiles   *float64 `json:"diff_files"`
	DiffLines   *float64 `json:"diff_lines"`
}

// walkedStory walks a story through every gate, a minute apart, with a block
// at blockAt's review where it names one, and lands it at end.
func walkedStory(id string, end time.Duration, blockAt model.Gate) *gateRecord {
	g := newGateRecord(id)
	for i, gate := range model.Gates {
		d := time.Duration(i+1) * time.Minute
		if gate == blockAt {
			g.review(gate, "reviewer", model.VerdictBlock, 1, d)
			g.review(gate, "reviewer", model.VerdictApprove, 2, d)
		}
		if gate != model.GateRetro {
			g.pass(gate, d)
		}
	}
	return g.pass(model.GateRetro, end-time.Minute).event("landed", "", end)
}

// AC-19, G6 and the human advocate's finding 7: each --all column holds the
// story's figure, not null; a review block alone loses first-pass success; and
// a story still under way has no end and is left out of the medians.
func TestAC19EveryColumnOfTraceAllHoldsItsFigureAndAStoryUnderWayHasNoEnd(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-2", "T-5", "T-6", "T-7")
	walkedStory("T-2", 10*time.Hour, "").
		agent("sdlc:sdet", "a1", time.Hour, 2*time.Hour).
		agent("sdlc:verifier", "a2", 90*time.Minute, 3*time.Hour).
		save(t, root)
	traceLines(t, root, "T-2",
		record(t, "agent_run", 2*time.Hour, map[string]any{"agent_type": "sdlc:sdet", "agent_id": "a1",
			"models": map[string]any{"m": tok(100, 200, 300, 400)}, "responses": 1, "peak_context": 600}),
		record(t, "turn", 4*time.Hour, map[string]any{"models": map[string]any{"m": tok(10, 20, 30, 40)}, "responses": 1, "peak_context": 900}),
		record(t, "shell", 5*time.Hour, map[string]any{"class": "test", "program": "go", "duration_ms": 2000, "outcome": "pass"}),
		record(t, "shell", 5*time.Hour+time.Minute, map[string]any{"class": "test", "program": "go", "duration_ms": 3000, "outcome": "fail", "exit_code": 1}),
		record(t, "shell", 5*time.Hour+2*time.Minute, map[string]any{"class": "git", "program": "git", "duration_ms": 500}),
		record(t, "refusal", 6*time.Hour, map[string]any{"rule": "r1", "tool": "Bash"}),
		record(t, "refusal", 6*time.Hour+time.Minute, map[string]any{"rule": "r1", "tool": "Bash"}),
		record(t, "refusal", 6*time.Hour+2*time.Minute, map[string]any{"rule": "r2", "tool": "Write"}),
		record(t, "diff", 7*time.Hour, map[string]any{"files": 4, "added": 30, "removed": 12, "test_files": 1, "test_added": 10, "test_removed": 0}),
	)
	walkedStory("T-5", 4*time.Hour, model.GateCodeReview).save(t, root)
	walkedStory("T-6", 3*time.Hour, model.GateVerifierReview).save(t, root)
	// Under way: passed the plan, and a gate recorded twenty hours in.
	newGateRecord("T-7").pass(model.GateDoR, time.Minute).pass(model.GateAnalysis, 2*time.Minute).
		pass(model.GateTestsFrozen, 3*time.Minute).pass(model.GatePlan, 20*time.Hour).save(t, root)

	var doc struct {
		Stories []allRow            `json:"stories"`
		Medians map[string]*float64 `json:"medians"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, "trace", "--all", "--json").stdout), &doc); err != nil {
		t.Fatal(err)
	}
	rows := map[string]allRow{}
	for _, s := range doc.Stories {
		rows[s.Story] = s
	}
	t2 := rows["T-2"]
	seconds(t, "AC-19 T-2's agents' time, the union of 1h-2h and 1h30m-3h", t2.Agents, 2*time.Hour)
	if t2.PeakContext == nil || *t2.PeakContext != 900 {
		t.Errorf("AC-19 T-2's peak context = %v, want 900, the largest of the run's and the turn's", t2.PeakContext)
	}
	if t2.Tokens == nil || *t2.Tokens != 1100 {
		t.Errorf("AC-19 T-2's tokens = %v, want 1100", t2.Tokens)
	}
	seconds(t, "AC-19 T-2's time in test runs", t2.TestTime, 5*time.Second)
	if t2.Refusals == nil || *t2.Refusals != 3 {
		t.Errorf("AC-19 T-2's refusals = %v, want 3", t2.Refusals)
	}
	if t2.DiffFiles == nil || *t2.DiffFiles != 4 || t2.DiffLines == nil || *t2.DiffLines != 42 {
		t.Errorf("AC-19 T-2's diff = %v files and %v lines, want 4 and 42, the lines added and removed", t2.DiffFiles, t2.DiffLines)
	}
	designPass := time.Duration(1+indexOf(model.Gates, model.GateDesignReview)) * time.Minute
	seconds(t, "AC-19 T-2's time from the design review's pass to the landing", t2.DesignToEnd, 10*time.Hour-designPass)
	if !t2.FirstPass || rows["T-5"].FirstPass || rows["T-6"].FirstPass {
		t.Errorf("AC-19 first-pass success: T-2 %v, T-5 %v (a code review block), T-6 %v (a verifier review block); want true, false, false",
			t2.FirstPass, rows["T-5"].FirstPass, rows["T-6"].FirstPass)
	}
	if t7, ok := rows["T-7"]; !ok || t7.End != nil {
		t.Errorf("AC-19 a story under way is shown as ended: %+v", t7)
	}
	// Of 10h, 4h and 3h; with T-7's twenty hours it would be 7h.
	seconds(t, "AC-19 the median wall time, of the stories that ended", doc.Medians["wall"], 4*time.Hour)
	text := mustRun(t, "trace", "--all").stdout
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, "T-7") && !strings.Contains(line, "in progress") {
			t.Errorf("AC-19 the row of a story under way does not say it is in progress: %s", line)
		}
	}
}

func indexOf(gates []model.Gate, g model.Gate) int {
	for i, x := range gates {
		if x == g {
			return i
		}
	}
	return -1
}

// AC-18 and AC-19: the command is one of its two forms. Neither form, or
// both, is refused with nothing on stdout; --all where no story has a record
// is an answer, not an error.
func TestAC18AC19TraceTakesAStoryOrAllAndAnEmptyProjectIsAnAnswer(t *testing.T) {
	t.Parallel()
	t.Run("AC-18 neither form or both", func(t *testing.T) {
		t.Parallel()
		tracedStory(t)
		for _, args := range [][]string{{"trace"}, {"trace", "T-1", "--all"}} {
			if r := run(t, args...); r.code == 0 || r.stdout != "" {
				t.Errorf("AC-18 sdlc %s exited %d with %q on stdout, want a refusal and nothing there", strings.Join(args, " "), r.code, r.stdout)
			}
		}
	})
	t.Run("AC-19 no story recorded", func(t *testing.T) {
		t.Parallel()
		gitProjectInitialised(t)
		r := run(t, "trace", "--all", "--json")
		if r.code != 0 {
			t.Fatalf("AC-19 sdlc trace --all with no story recorded exited %d:\n%s", r.code, r.stderr)
		}
		var doc struct {
			Stories []json.RawMessage `json:"stories"`
		}
		if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil || len(doc.Stories) != 0 {
			t.Errorf("AC-19 --all with no story recorded gave %q (%v)", r.stdout, err)
		}
		if text := mustRun(t, "trace", "--all").stdout; strings.TrimSpace(text) == "" {
			t.Error("AC-19 --all with no story recorded printed nothing at all")
		}
	})
}
