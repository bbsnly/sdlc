package cli

// US-112.6: budget.gate_hours. The records here are written by hand at fixed
// past times, so every figure can be worked out on paper; a real `sdlc gate` or
// `sdlc review add` then runs on them, at the clock's own time, which is after
// every agent run and changes no gate's agents' time.
//
// Nothing here pins the words of the line or the prefix of the event, which are
// the planner's. A line is told by what AC-1 says it names: the gate key as a
// word, the number set, and the agents' time, in one of the forms ghTimeForms
// lists. An event is told by its type and the gate key as a word in its message,
// and `sdlc cost add` puts a per-story alert of the same type beside it.

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const ghStory = "US-001"

// ghWord is whether text names key as a word: not design_review for review, and
// not gate_hours for gate. Every gate key is lower case and may hold an
// underscore, so those are what cannot stand beside it.
func ghWord(text, key string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(key) + `($|[^A-Za-z0-9_])`).MatchString(text)
}

var ghToken = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?[a-z0-9]*`)

// ghTimeForms is every way the line may state an agents' time: as `sdlc trace`
// prints it in its table (28m, 1h05m, 30m1s), as Go prints a duration (28m0s),
// or as hours to one, two or three places, with an h or not (0.47, 0.47h).
func ghTimeForms(d time.Duration) []string {
	forms := []string{humanTime(d), d.String()}
	for places := 1; places <= 3; places++ {
		hours := strconv.FormatFloat(d.Hours(), 'f', places, 64)
		trimmed := strings.TrimRight(strings.TrimRight(hours, "0"), ".")
		forms = append(forms, hours, hours+"h", trimmed, trimmed+"h")
	}
	return forms
}

// ghStatesNumber is whether line states the number set, in any spelling of it.
func ghStatesNumber(line string, number float64) bool {
	for _, token := range ghToken.FindAllString(line, -1) {
		if v, err := strconv.ParseFloat(strings.TrimSuffix(token, "h"), 64); err == nil && v == number {
			return true
		}
	}
	return false
}

func ghStatesTime(line string, d time.Duration) bool {
	forms := ghTimeForms(d)
	for _, token := range ghToken.FindAllString(line, -1) {
		if slices.Contains(forms, token) {
			return true
		}
	}
	return false
}

// ghLines is the lines of stderr that name the gate and the number set for it.
// A refusal's text may name a gate; it does not also state its number.
func ghLines(stderr, gate string, number float64) []string {
	var out []string
	for line := range strings.SplitSeq(stderr, "\n") {
		if ghWord(line, gate) && ghStatesNumber(line, number) {
			out = append(out, line)
		}
	}
	return out
}

// ghSaysOnce holds stderr to one line for the gate, stating its time and number.
func ghSaysOnce(t *testing.T, what, stderr, gate string, number float64, agents time.Duration) {
	t.Helper()
	lines := ghLines(stderr, gate, number)
	if len(lines) != 1 {
		t.Fatalf("%s: %d lines name %s and %v, want one:\n%s", what, len(lines), gate, number, stderr)
	}
	if !ghStatesTime(lines[0], agents) {
		t.Errorf("%s: the line does not state the agents' time %v in any of %v:\n%s", what, agents, ghTimeForms(agents), lines[0])
	}
}

// ghAdded is the lines on's stderr has that off's has not, and those off's has
// that on's lost, each as many times as it differs.
func ghAdded(off, on string) (added, lost []string) {
	count := map[string]int{}
	for line := range strings.SplitSeq(off, "\n") {
		count[line]++
	}
	for line := range strings.SplitSeq(on, "\n") {
		if count[line] > 0 {
			count[line]--
			continue
		}
		added = append(added, line)
	}
	for _, line := range slices.Sorted(maps.Keys(count)) {
		for range count[line] {
			lost = append(lost, line)
		}
	}
	return added, lost
}

// ghSetHours sets budget.gate_hours in the file, or takes it away for nil. The
// rest of the budget stays, $60 and its alerts.
func ghSetHours(t *testing.T, root string, hours any) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(config.File))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	budget, ok := cfg["budget"].(map[string]any)
	if !ok {
		t.Fatalf("the scaffolded configuration has no budget: %v", cfg)
	}
	if hours == nil {
		delete(budget, "gate_hours")
	} else {
		budget["gate_hours"] = hours
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// ghRewrite gives the story, started by a real `sdlc start`, the history build
// makes: its creation, events and gates become those of a story begun at t0, and
// everything else the start put on the record stays.
func ghRewrite(t *testing.T, root string, build func(*gateRecord)) {
	t.Helper()
	path := ghRecordPath(root)
	started, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	g := newGateRecord(ghStory)
	build(g)
	g.save(t, root)
	fixture, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var was, now map[string]any
	if err := json.Unmarshal(started, &was); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(fixture, &now); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"created", "updated", "events", "gates"} {
		if v, ok := now[key]; ok {
			was[key] = v
		}
	}
	out, err := json.Marshal(was)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// ghWorked is the story every number here is measured against. Its agents' time:
//
//	analysis      28m   two runs, 18m and 20m, overlapping: 38m if added up
//	tests_frozen  30m   20m, then 10m of a run that goes on past the pass, and a
//	                    run that never ended, which would add 1h45m
//	plan          15m   the 5m left of that run, and 10m
//
// and design_review is in force at its end, with no agent's time.
func ghWorked(g *gateRecord) {
	g.pass(model.GateDoR, 10*time.Minute).
		agent("sdlc:researcher", "a1", 12*time.Minute, 30*time.Minute).
		agent("Explore", "a2", 20*time.Minute, 40*time.Minute).
		pass(model.GateAnalysis, 45*time.Minute).
		agent("sdlc:sdet", "a3", 50*time.Minute, 70*time.Minute).
		agent("sdlc:verifier", "open", 75*time.Minute, -1).
		agent("sdlc:sdet", "across", 2*time.Hour+50*time.Minute, 3*time.Hour+5*time.Minute).
		pass(model.GateTestsFrozen, 3*time.Hour).
		agent("sdlc:planner", "a6", 3*time.Hour+5*time.Minute, 3*time.Hour+15*time.Minute).
		pass(model.GatePlan, 3*time.Hour+20*time.Minute).
		event("loop_end", "", 3*time.Hour+30*time.Minute)
}

const (
	ghAnalysis    = 28 * time.Minute
	ghTestsFrozen = 30 * time.Minute
	ghPlan        = 15 * time.Minute
)

// ghOverAnalysis has analysis over its number, tests_frozen exactly on it, plan
// under it, implementation with no time at all, and verification turned off.
func ghOverAnalysis() map[string]any {
	return map[string]any{"analysis": 0.4, "tests_frozen": 0.5, "plan": 0.3, "implementation": 0.01, "verification": 0}
}

// ghStarted is the project with the story started and its history in place.
func ghStarted(t *testing.T, build func(*gateRecord)) string {
	t.Helper()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	ghRewrite(t, root, build)
	return root
}

func ghRecordOf(t *testing.T, root, story string) model.Record {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", story, model.RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	var r model.Record
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

// ghBudget is the record's budget events: those that name a gate, by gate, and
// how many name none, which are the per-story alerts.
func ghBudget(r model.Record) (byGate map[string]int, alerts int) {
	byGate = map[string]int{}
	for _, e := range r.Events {
		if e.Type != "budget" {
			continue
		}
		named := false
		for _, g := range model.Gates {
			if ghWord(e.Message, string(g)) {
				byGate[string(g)]++
				named = true
			}
		}
		if !named {
			alerts++
		}
	}
	return byGate, alerts
}

func ghCounts(t *testing.T, what string, root string, want map[string]int) {
	t.Helper()
	got, _ := ghBudget(ghRecordOf(t, root, ghStory))
	if !maps.Equal(got, want) {
		t.Errorf("%s: budget events by gate = %v, want %v", what, got, want)
	}
}

// ghTree is every file of the loop's state and the backlog, by path. A story's
// worktree is not state, and there is none in these projects.
func ghTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	read := func(path string) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files[path] = raw
	}
	read(filepath.Join(root, "user_stories.json"))
	err := filepath.WalkDir(filepath.Join(root, ".sdlc"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			read(path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func ghRestore(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for path := range ghTree(t, root) {
		if _, kept := files[path]; !kept {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
	}
	for path, raw := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// ghTwice runs the one command from the one state, with the setting absent and
// with hours, so that nothing here pins what the command says today. The state
// is left as the second run left it.
func ghTwice(t *testing.T, root string, hours any, stdin string, args ...string) (off, on result) {
	t.Helper()
	ghSetHours(t, root, nil)
	before := ghTree(t, root)
	off = runWith(t, stdin, args...)
	ghRestore(t, root, before)
	ghSetHours(t, root, hours)
	on = runWith(t, stdin, args...)
	return off, on
}

// ghControl puts the state back as it was, sets hours, and runs the command: the
// control a test of silence needs, since silence is what a binary without the
// feature gives. The same case with a number that is passed has to print the line.
func ghControl(t *testing.T, root string, before map[string][]byte, hours any, stdin string, args ...string) result {
	t.Helper()
	ghRestore(t, root, before)
	ghSetHours(t, root, hours)
	return runWith(t, stdin, args...)
}

// ghCommand is one command the setting must leave alone, from the worked story.
type ghCommand struct {
	name    string
	stdin   string
	args    []string
	refused bool
}

var ghCommands = []ghCommand{
	{name: "a gate that passes", args: []string{"gate", "dor", "pass"}},
	{name: "a gate that fails", args: []string{"gate", "plan", "fail", "--note", "again"}},
	{name: "a gate refused for its documents", args: []string{"gate", "analysis", "pass"}, refused: true},
	{name: "a review", stdin: "# architect\n", args: []string{"review", "add", "design_review", "architect", "approve"}},
	{name: "a block", stdin: "# architect\n", args: []string{"review", "add", "design_review", "architect", "block", "--important", "1", "--note", "no step"}},
	{name: "a review the project does not expect", stdin: "# advocate\n", args: []string{"review", "add", "dor", "human-advocate", "approve"}, refused: true},
	{name: "status", args: []string{"status"}},
}

// AC-1: the same fixture twice, so that whatever the commands print today is what
// they have to go on printing, refusal or not, --json or not.
func TestAC1OverItsNumberEachCommandAddsOneStderrLineAndLeavesStdoutAndExitAlone(t *testing.T) {
	t.Parallel()
	for _, c := range ghCommands {
		for _, form := range []string{"prose", "json"} {
			t.Run(c.name+" "+form, func(t *testing.T) {
				t.Parallel()
				root := ghStarted(t, ghWorked)
				args := c.args
				if form == "json" {
					args = append(slices.Clone(args), "--json")
				}
				off, on := ghTwice(t, root, ghOverAnalysis(), c.stdin, args...)
				if (off.code != 0) != c.refused {
					t.Fatalf("the fixture did not do what this row is for: exit %d\n%s%s", off.code, off.stdout, off.stderr)
				}
				if on.code != off.code {
					t.Errorf("AC-1 exit %d with the setting, %d without", on.code, off.code)
				}
				if on.stdout != off.stdout {
					t.Errorf("AC-1 stdout changed.\nwithout:\n%s\nwith:\n%s", off.stdout, on.stdout)
				}
				added, lost := ghAdded(off.stderr, on.stderr)
				if len(lost) != 0 {
					t.Errorf("AC-1 stderr lost %q", lost)
				}
				if len(added) != 1 {
					t.Fatalf("AC-1 stderr gained %d lines, want one: %q", len(added), added)
				}
				ghSaysOnce(t, "AC-1", added[0], "analysis", 0.4, ghAnalysis)
			})
		}
	}
}

// AC-1 "decide": what a command records is what it records without the setting,
// but for the one event.
func TestAC1TheCommandRecordsWhatItWouldWithoutTheSettingButForTheEvent(t *testing.T) {
	t.Parallel()
	for _, c := range ghCommands {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, ghWorked)
			before := ghTree(t, root)
			ghSetHours(t, root, nil)
			runWith(t, c.stdin, c.args...)
			off := ghRecordOf(t, root, ghStory)
			ran := ghControl(t, root, before, ghOverAnalysis(), c.stdin, c.args...)
			on := ghRecordOf(t, root, ghStory)
			ghSaysOnce(t, "AC-1 the setting is at work", ran.stderr, "analysis", 0.4, ghAnalysis)
			// The one difference there is meant to be, on a command that saves.
			want := map[string]int{"analysis": 1}
			if c.refused || c.args[0] == "status" {
				want = map[string]int{}
			}
			ghCounts(t, "AC-2 but for the event", root, want)

			gates := func(r model.Record) map[string]string {
				out := map[string]string{}
				for g, res := range r.Gates {
					out[string(g)] = string(res.Status)
				}
				return out
			}
			if !maps.Equal(gates(off), gates(on)) {
				t.Errorf("AC-1 the gates differ: %v without, %v with", gates(off), gates(on))
			}
			history := func(r model.Record) []string {
				var out []string
				for _, e := range r.Events {
					if e.Type != "budget" {
						out = append(out, e.Type+" "+e.Message)
					}
				}
				return out
			}
			if !slices.Equal(history(off), history(on)) {
				t.Errorf("AC-1 the history differs:\nwithout %q\nwith    %q", history(off), history(on))
			}
			if len(off.Reviews) != len(on.Reviews) {
				t.Errorf("AC-1 %d reviews without the setting and %d with", len(off.Reviews), len(on.Reviews))
			}
		})
	}
}

// AC-1: every gate with a number is checked, not the one on the command line.
func TestAC1EveryGateOverItsNumberHasALineWhicheverGateTheCommandNames(t *testing.T) {
	t.Parallel()
	hours := map[string]any{"analysis": 0.25, "tests_frozen": 0.2, "plan": 0.1, "retro": 0.1}
	for _, c := range ghCommands {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, ghWorked)
			off, on := ghTwice(t, root, hours, c.stdin, c.args...)
			added, _ := ghAdded(off.stderr, on.stderr)
			if len(added) != 3 {
				t.Errorf("AC-1 stderr gained %d lines, want one for each of three gates: %q", len(added), added)
			}
			ghSaysOnce(t, "AC-1 analysis", on.stderr, "analysis", 0.25, ghAnalysis)
			ghSaysOnce(t, "AC-1 tests_frozen", on.stderr, "tests_frozen", 0.2, ghTestsFrozen)
			ghSaysOnce(t, "AC-1 plan", on.stderr, "plan", 0.1, ghPlan)
		})
	}
}

// A story other than the active one is named on the command line, and its record
// is the one in hand: the active story's time says nothing of it.
func TestAC1AGateNamedWithStoryIsMeasuredOnThatStorysRecord(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-2")
	mustRun(t, "start", ghStory)
	newGateRecord("T-2").agent("sdlc:researcher", "t2", 2*time.Minute, 14*time.Minute).event("loop_end", "", time.Hour).save(t, root)
	ghRewrite(t, root, ghWorked)
	ghSetHours(t, root, map[string]any{"dor": 0.1, "analysis": 0.4})

	r := run(t, "gate", "dor", "fail", "--story", "T-2", "--note", "again")
	if r.code != 0 {
		t.Fatalf("the fixture did not do what this is for: exit %d\n%s", r.code, r.stderr)
	}
	ghSaysOnce(t, "AC-1 the story named", r.stderr, "dor", 0.1, 12*time.Minute)
	if lines := ghLines(r.stderr, "analysis", 0.4); len(lines) != 0 {
		t.Errorf("AC-1 the line speaks of the active story's time: %q", lines)
	}
	if got, _ := ghBudget(ghRecordOf(t, root, "T-2")); got["dor"] != 1 {
		t.Errorf("AC-2 the story named has budget events %v, want one for dor", got)
	}
	ghCounts(t, "AC-2 the active story", root, map[string]int{})
}

// A row is a history and the agents' time paper gives its gate. Each is one thing
// a sum of agent events gets wrong and `sdlc trace` gets right, and the row holds
// `sdlc trace` itself to the same figure, so that the line is shown to be trace's
// number and not one that happens to agree with it on a simple case.
func TestAC1TheLineStatesTheAgentsTimeAsSdlcTraceCountsIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		build  func(*gateRecord)
		gate   string
		number float64
		agents time.Duration
	}{
		{
			name: "parallel agents are counted once",
			build: func(g *gateRecord) {
				g.pass(model.GateDoR, time.Minute).
					agent("a", "p1", 2*time.Minute, 22*time.Minute).
					agent("b", "p2", 2*time.Minute, 22*time.Minute).
					agent("c", "p3", 12*time.Minute, 32*time.Minute).
					event("loop_end", "", time.Hour)
			},
			gate: "analysis", number: 0.25, agents: 30 * time.Minute,
		},
		{
			name: "a run across a pass is charged to each gate its part",
			build: func(g *gateRecord) {
				g.pass(model.GateDoR, time.Minute).
					pass(model.GateAnalysis, 2*time.Minute).
					agent("a", "x1", 3*time.Minute, 43*time.Minute).
					pass(model.GateTestsFrozen, 13*time.Minute).
					event("loop_end", "", time.Hour)
			},
			gate: "plan", number: 0.25, agents: 30 * time.Minute,
		},
		{
			name: "the time before that pass goes to the gate before it",
			build: func(g *gateRecord) {
				g.pass(model.GateDoR, time.Minute).
					pass(model.GateAnalysis, 2*time.Minute).
					agent("a", "x1", 3*time.Minute, 43*time.Minute).
					pass(model.GateTestsFrozen, 23*time.Minute).
					event("loop_end", "", time.Hour)
			},
			gate: "tests_frozen", number: 0.25, agents: 20 * time.Minute,
		},
		{
			name: "a gate failed and worked on again sums its stretches",
			build: func(g *gateRecord) {
				g.pass(model.GateDoR, time.Minute).
					agent("a", "r1", 2*time.Minute, 22*time.Minute).
					pass(model.GateAnalysis, 23*time.Minute).
					agent("b", "r2", 24*time.Minute, 28*time.Minute).
					fail(model.GateAnalysis, 30*time.Minute).
					agent("c", "r3", 31*time.Minute, 46*time.Minute).
					event("loop_end", "", time.Hour)
			},
			gate: "analysis", number: 0.5, agents: 35 * time.Minute,
		},
		{
			name: "a gate reopened by a gate_reopened event takes the time back",
			build: func(g *gateRecord) {
				g.pass(model.GateDoR, time.Minute).
					agent("a", "o1", 2*time.Minute, 22*time.Minute).
					pass(model.GateAnalysis, 23*time.Minute).
					event(model.EventGatesReopened, "analysis reopened", 24*time.Minute).
					agent("b", "o2", 25*time.Minute, 40*time.Minute).
					event("loop_end", "", time.Hour)
			},
			gate: "analysis", number: 0.5, agents: 35 * time.Minute,
		},
		{
			name: "a run that never ended is left out",
			build: func(g *gateRecord) {
				g.pass(model.GateDoR, time.Minute).
					agent("a", "e1", 2*time.Minute, 22*time.Minute).
					agent("b", "e2", 23*time.Minute, -1).
					event("loop_end", "", 2*time.Hour)
			},
			gate: "analysis", number: 0.25, agents: 20 * time.Minute,
		},
		{
			name: "an agent's time waiting on a person still counts as agents' time",
			build: func(g *gateRecord) {
				g.pass(model.GateDoR, time.Minute).
					agent("a", "w1", 2*time.Minute, 42*time.Minute).
					escalate("spec_unclear", 12*time.Minute, 2*time.Hour).
					event("loop_start", "", 3*time.Hour)
			},
			gate: "analysis", number: 0.5, agents: 40 * time.Minute,
		},
		{
			name:  "a run after the commit gate passed is past the story's end",
			build: ghCommitted,
			gate:  "commit", number: 0.25, agents: 20 * time.Minute,
		},
		{
			name:  "and charged to no gate after it",
			build: ghCommitted,
			gate:  "retro", number: 0.001, agents: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, tc.build)
			d := decode[traceDoc](t, mustRun(t, "trace", ghStory, "--json"))
			if tc.agents == 0 {
				if s := ghRowAgents(d, tc.gate); s != 0 {
					t.Fatalf("the fixture is wrong: trace gives %s %vs", tc.gate, s)
				}
				ghSetHours(t, root, map[string]any{tc.gate: tc.number})
				if lines := ghLines(mustRun(t, "status").stderr, tc.gate, tc.number); len(lines) != 0 {
					t.Errorf("AC-1 trace gives this gate no time and the line says otherwise: %q", lines)
				}
				// The gate before it had the time, and its number is passed.
				ghSetHours(t, root, map[string]any{"commit": 0.25})
				ghSaysOnce(t, "AC-1 the gate that had the time", mustRun(t, "status").stderr, "commit", 0.25, 20*time.Minute)
				return
			}
			seconds(t, "the fixture, as sdlc trace counts it", d.gate(t, model.Gate(tc.gate)).Agents, tc.agents)
			ghSetHours(t, root, map[string]any{tc.gate: tc.number})
			ghSaysOnce(t, "AC-1", mustRun(t, "status").stderr, tc.gate, tc.number, tc.agents)
		})
	}
}

// ghCommitted is a story that spent 20m on its commit gate, and has a run after
// that gate passed, where the story ended.
func ghCommitted(g *gateRecord) {
	for _, gate := range model.Gates[:slices.Index(model.Gates, model.GateCommit)] {
		g.pass(gate, time.Minute)
	}
	g.agent("a", "c1", 5*time.Minute, 25*time.Minute).
		pass(model.GateCommit, 41*time.Minute).
		agent("b", "c2", 42*time.Minute, 2*time.Hour).
		event("loop_end", "", 3*time.Hour)
}

func ghRowAgents(d traceDoc, gate string) float64 {
	for _, row := range d.Gates {
		if row.Gate == gate && row.Agents != nil {
			return *row.Agents
		}
	}
	return 0
}

// A record may hold events out of the order of their times: the hook and a
// command each stamp their own, and the replay sorts. A time that does not read
// is dropped by the replay, so it does not begin a run either.
func TestAC1EventsOutOfOrderOrWithATimeThatDoesNotReadAreReadAsSdlcTraceReadsThem(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	path := filepath.Join(root, ".sdlc", "stories", ghStory, model.RecordFile)
	ghRewrite(t, root, func(g *gateRecord) {
		g.pass(model.GateDoR, time.Minute).
			agent("a", "s1", 2*time.Minute, 32*time.Minute).
			event("loop_end", "", time.Hour)
	})
	var doc map[string]any
	if err := json.Unmarshal(ghBytes(t, path), &doc); err != nil {
		t.Fatal(err)
	}
	events, _ := doc["events"].([]any)
	slices.Reverse(events)
	events = append(events, map[string]any{"at": "not a time", "type": "agent_start", "agent_id": "bad", "agent_type": "a", "message": ""},
		map[string]any{"at": model.Timestamp(at(59 * time.Minute)), "type": "agent_end", "agent_id": "bad", "agent_type": "a", "message": ""})
	doc["events"] = events
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	d := decode[traceDoc](t, mustRun(t, "trace", ghStory, "--json"))
	seconds(t, "the fixture, as sdlc trace counts it", d.gate(t, model.GateAnalysis).Agents, 30*time.Minute)
	ghSetHours(t, root, map[string]any{"analysis": 0.25})
	ghSaysOnce(t, "AC-1", mustRun(t, "status").stderr, "analysis", 0.25, 30*time.Minute)
}

func ghBytes(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// The figure changes with the record: a line with a figure hard-coded, or taken
// from the number set, does not get past two records.
func TestAC1TheLinesFigureFollowsTheRecordAndItsNumberFollowsTheConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		end    time.Duration
		agents time.Duration
		number float64
	}{
		{22 * time.Minute, 20 * time.Minute, 0.1},
		{47 * time.Minute, 45 * time.Minute, 0.2},
		{2*time.Hour + 2*time.Minute, 2 * time.Hour, 1.5},
	} {
		t.Run(tc.agents.String(), func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, func(g *gateRecord) {
				g.pass(model.GateDoR, time.Minute).
					agent("a", "f1", 2*time.Minute, tc.end).
					event("loop_end", "", 3*time.Hour)
			})
			ghSetHours(t, root, map[string]any{"analysis": tc.number})
			ghSaysOnce(t, "AC-1", mustRun(t, "status").stderr, "analysis", tc.number, tc.agents)
		})
	}
}

// AC-1 says passed, and a person settled that as strictly greater. The record's
// times are whole seconds, so a second is the boundary.
func TestAC1PassedIsStrictlyGreaterToTheSecond(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		run  time.Duration
		over bool
	}{
		{"a second under", 1799 * time.Second, false},
		{"exactly on it", 1800 * time.Second, false},
		{"a second over", 1801 * time.Second, true},
	} {
		for _, c := range ghCommands {
			t.Run(tc.name+" "+c.name, func(t *testing.T) {
				t.Parallel()
				root := ghStarted(t, func(g *gateRecord) {
					g.pass(model.GateDoR, time.Minute).
						agent("a", "b1", 2*time.Minute, 2*time.Minute+tc.run).
						event("loop_end", "", 2*time.Hour)
				})
				before := ghTree(t, root)
				off, on := ghTwice(t, root, map[string]any{"analysis": 0.5}, c.stdin, c.args...)
				added, _ := ghAdded(off.stderr, on.stderr)
				if on.stdout != off.stdout || on.code != off.code {
					t.Errorf("AC-1 the command changed: exit %d and %d\n%s\n%s", off.code, on.code, off.stdout, on.stdout)
				}
				// The same run against a number well under it.
				under := ghControl(t, root, before, map[string]any{"analysis": 0.25}, c.stdin, c.args...)
				if lines := ghLines(under.stderr, "analysis", 0.25); len(lines) != 1 {
					t.Errorf("AC-1 the same run past a lower number said %q, want one line:\n%s", lines, under.stderr)
				}
				if tc.over && len(added) != 1 {
					t.Errorf("AC-1 a gate past its number said %q, want one line", added)
				}
				if !tc.over && len(added) != 0 {
					t.Errorf("AC-1 a gate not past its number said %q", added)
				}
			})
		}
	}
}

// Nothing to say, nothing to record. 0 is how a number is turned off, a very
// large number means never and must not overflow on its way to a duration, and a
// story with no agent events has nothing to compare.
func TestAC1AC2NoLineAndNoEventWhereNoNumberIsPassed(t *testing.T) {
	t.Parallel()
	ranLong := func(g *gateRecord) {
		g.pass(model.GateDoR, time.Minute).agent("a", "s1", 2*time.Minute, 5*time.Hour).event("loop_end", "", 6*time.Hour)
	}
	noAgents := func(g *gateRecord) {
		g.pass(model.GateDoR, time.Minute).event("loop_end", "", 6*time.Hour)
	}
	onlyOpen := func(g *gateRecord) {
		g.pass(model.GateDoR, time.Minute).agent("a", "s1", 2*time.Minute, -1).event("loop_end", "", 6*time.Hour)
	}
	otherGate := func(g *gateRecord) {
		g.agent("a", "s1", time.Minute, 5*time.Hour).event("loop_end", "", 6*time.Hour)
	}
	// Each row's control is the same story with the one thing that kept it silent
	// put right: a number that is passed, the run ended, or the gate that had the time.
	analysisOver := map[string]any{"analysis": 0.5}
	for _, tc := range []struct {
		name   string
		build  func(*gateRecord)
		hours  map[string]any
		fix    func(t *testing.T, root string)
		over   map[string]any
		gate   string
		number float64
	}{
		{name: "0 is off", build: ranLong, hours: map[string]any{"analysis": 0}, over: analysisOver, gate: "analysis", number: 0.5},
		{name: "0 on every gate is off", build: ranLong, hours: ghEveryGate(0), over: analysisOver, gate: "analysis", number: 0.5},
		{name: "1e308 is never", build: ranLong, hours: map[string]any{"analysis": 1e308}, over: analysisOver, gate: "analysis", number: 0.5},
		{name: "1e10 is past where a duration overflows", build: ranLong, hours: map[string]any{"analysis": 1e10}, over: analysisOver, gate: "analysis", number: 0.5},
		{
			name: "a story with no agent events", build: noAgents, hours: ghEveryGate(0.0001),
			fix: func(t *testing.T, root string) {
				t.Helper()
				ghAppendAgent(t, root, "s2", 2*time.Minute, 62*time.Minute)
			},
			over: analysisOver, gate: "analysis", number: 0.5,
		},
		{
			name: "a run still open is not counted", build: onlyOpen, hours: ghEveryGate(0.0001),
			fix: func(t *testing.T, root string) {
				t.Helper()
				ghAppendEvent(t, root, map[string]any{"at": model.Timestamp(at(62 * time.Minute)),
					"type": model.EventAgentEnd, "agent_type": "a", "agent_id": "s1", "message": ""})
			},
			over: analysisOver, gate: "analysis", number: 0.5,
		},
		{name: "time on another gate", build: otherGate, hours: map[string]any{"analysis": 0.0001, "retro": 0.0001},
			over: map[string]any{"dor": 0.5}, gate: "dor", number: 0.5},
	} {
		for _, c := range ghCommands {
			t.Run(tc.name+" "+c.name, func(t *testing.T) {
				t.Parallel()
				root := ghStarted(t, tc.build)
				before := ghTree(t, root)
				off, on := ghTwice(t, root, tc.hours, c.stdin, c.args...)
				if on.stderr != off.stderr || on.stdout != off.stdout || on.code != off.code {
					t.Errorf("AC-1 the command changed.\nwithout: exit %d\n%s%s\nwith: exit %d\n%s%s",
						off.code, off.stdout, off.stderr, on.code, on.stdout, on.stderr)
				}
				ghCounts(t, "AC-2", root, map[string]int{})

				ghRestore(t, root, before)
				if tc.fix != nil {
					tc.fix(t, root)
				}
				ghSetHours(t, root, tc.over)
				r := runWith(t, c.stdin, c.args...)
				if lines := ghLines(r.stderr, tc.gate, tc.number); len(lines) != 1 {
					t.Errorf("AC-1 the control said %q, want one line:\n%s", lines, r.stderr)
				}
			})
		}
	}
}

func ghEveryGate(number float64) map[string]any {
	out := map[string]any{}
	for _, g := range model.Gates {
		out[string(g)] = number
	}
	return out
}

// A number too small to round to a nanosecond is still a number: 1e-12 hours is
// passed as soon as there is any time at all.
func TestAC1AVerySmallNumberIsPassedByASecond(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, func(g *gateRecord) {
		g.pass(model.GateDoR, time.Minute).agent("a", "s1", 2*time.Minute, 3*time.Minute).event("loop_end", "", time.Hour)
	})
	off, on := ghTwice(t, root, map[string]any{"analysis": 1e-12}, "", "status")
	if added, _ := ghAdded(off.stderr, on.stderr); len(added) != 1 || !ghWord(added[0], "analysis") {
		t.Errorf("AC-1 a minute against 1e-12 hours said %q, want one line naming analysis", added)
	}
}

// A person settled it: a command that cannot read the story's record prints
// nothing. The record is over its number in every row, so a line here came from
// something other than the record in hand.
func TestAC1ACommandRefusedBeforeItHasTheRecordPrintsNoLine(t *testing.T) {
	t.Parallel()
	rightGate := []string{"gate", "dor", "pass"}
	rightReview := []string{"review", "add", "design_review", "architect", "approve"}
	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "an unknown gate", args: []string{"gate", "nosuch", "pass"}},
		{name: "an unknown outcome", args: []string{"gate", "dor", "maybe"}},
		{name: "an unknown story", args: []string{"gate", "dor", "pass", "--story", "NOPE-1"}},
		{name: "a review of an unknown gate", stdin: "# x\n", args: []string{"review", "add", "nosuch", "architect", "approve"}},
		{name: "an unknown reviewer", stdin: "# x\n", args: []string{"review", "add", "design_review", "nosuch", "approve"}},
		{name: "an unknown verdict", stdin: "# x\n", args: []string{"review", "add", "design_review", "architect", "maybe"}},
		// US-115 AC-3: refused with the unknown verdict, before the record is read.
		{name: "a block with no important finding", stdin: "# x\n", args: []string{"review", "add", "design_review", "architect", "block"}},
		{name: "a negative count", stdin: "# x\n", args: []string{"review", "add", "design_review", "architect", "approve", "--nits", "-1"}},
		{name: "a review of an unknown story", stdin: "# x\n", args: []string{"review", "add", "design_review", "architect", "approve", "--story", "NOPE-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, ghWorked)
			before := ghTree(t, root)
			off, on := ghTwice(t, root, ghOverAnalysis(), tc.stdin, tc.args...)
			if off.code == 0 {
				t.Fatalf("the fixture did not do what this row is for: %s", off.stdout)
			}
			if on.stderr != off.stderr || on.stdout != off.stdout || on.code != off.code {
				t.Errorf("AC-1 the refusal changed.\nwithout: exit %d\n%s%s\nwith: exit %d\n%s%s",
					off.code, off.stdout, off.stderr, on.code, on.stdout, on.stderr)
			}
			ghSetHours(t, root, nil)
			after := ghTree(t, root)
			if !bytes.Equal(before[ghRecordPath(root)], after[ghRecordPath(root)]) {
				t.Error("AC-2 the refusal changed the record")
			}
			// The command spelt right gets as far as the record, and says so.
			right := rightGate
			if tc.args[0] == "review" {
				right = rightReview
			}
			r := ghControl(t, root, before, ghOverAnalysis(), tc.stdin, right...)
			ghSaysOnce(t, "AC-1 the same command spelt right", r.stderr, "analysis", 0.4, ghAnalysis)
		})
	}
}

func ghRecordPath(root string) string {
	return filepath.Join(root, ".sdlc", "stories", ghStory, model.RecordFile)
}

func TestAC1ARecordThatDoesNotReadPrintsNoLine(t *testing.T) {
	t.Parallel()
	for _, c := range ghCommands {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, ghWorked)
			before := ghTree(t, root)
			writeFile(t, root, ".sdlc/stories/"+ghStory+"/"+model.RecordFile, "{not a record")
			off, on := ghTwice(t, root, ghOverAnalysis(), c.stdin, c.args...)
			if off.code == 0 {
				t.Fatalf("a record that does not read was read: %s", off.stdout)
			}
			if on.stderr != off.stderr || on.stdout != off.stdout || on.code != off.code {
				t.Errorf("AC-1 the refusal changed.\nwithout: exit %d\n%s%s\nwith: exit %d\n%s%s",
					off.code, off.stdout, off.stderr, on.code, on.stdout, on.stderr)
			}
			// The record that reads, and the same command.
			r := ghControl(t, root, before, ghOverAnalysis(), c.stdin, c.args...)
			ghSaysOnce(t, "AC-1 a record that reads", r.stderr, "analysis", 0.4, ghAnalysis)
		})
	}
}

// With no story being worked on there is no record to read: status has its
// answer without one, and a gate is refused before it has one.
func TestAC1WithNoStoryBeingWorkedOnNothingIsSaid(t *testing.T) {
	t.Parallel()
	for _, c := range ghCommands {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, ghWorked)
			mustRun(t, "stop")
			ghRewrite(t, root, ghWorked)
			off, on := ghTwice(t, root, ghOverAnalysis(), c.stdin, c.args...)
			if on.stderr != off.stderr || on.stdout != off.stdout || on.code != off.code {
				t.Errorf("AC-1 the command changed.\nwithout: exit %d\n%s%s\nwith: exit %d\n%s%s",
					off.code, off.stdout, off.stderr, on.code, on.stdout, on.stderr)
			}
			// The story being worked on again, and the same command.
			mustRun(t, "start", ghStory)
			ghRewrite(t, root, ghWorked)
			r := runWith(t, c.stdin, c.args...)
			ghSaysOnce(t, "AC-1 with the story being worked on", r.stderr, "analysis", 0.4, ghAnalysis)
		})
	}
}

// Held by another checkout, the record is not this command's to read.
func TestAC1ACommandForAStoryAnotherCheckoutHoldsPrintsNoLine(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: ghStory}, wtStory{id: "US-002"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, main, wtSessionMain)
	ghRewrite(t, main, ghWorked)
	ghSetHours(t, wt, ghOverAnalysis())
	ghSetHours(t, main, ghOverAnalysis())

	commands := []struct {
		stdin string
		args  []string
	}{
		{args: []string{"gate", "dor", "pass", "--story", ghStory}},
		{stdin: "# x\n", args: []string{"review", "add", "design_review", "architect", "approve", "--story", ghStory}},
	}
	for _, tc := range commands {
		out := wtRefused(t, wtHeldElsewhere, wt, wtSessionWorktree, tc.stdin, tc.args...)
		if lines := ghLines(out, "analysis", 0.4); len(lines) != 0 {
			t.Errorf("AC-1 %v printed a line for a story another checkout holds: %q", tc.args, lines)
		}
	}
	ghCounts(t, "AC-2", main, map[string]int{})

	// From the checkout that holds it, the same commands read the record.
	for _, tc := range commands {
		r := wtRun(t, main, wtSessionMain, tc.stdin, tc.args...)
		ghSaysOnce(t, "AC-1 from the checkout that holds it", r.stderr, "analysis", 0.4, ghAnalysis)
	}
}

// The session check refuses before the record is read.
func TestAC1ACommandFromAnotherSessionPrintsNoLine(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setenv(t, store.SessionEnv, "session-holding")
	mustRun(t, "start")
	ghRewrite(t, root, ghWorked)
	ghSetHours(t, root, ghOverAnalysis())
	setenv(t, store.SessionEnv, "session-another")

	for _, c := range ghCommands {
		if c.args[0] == "status" {
			continue
		}
		r := runWith(t, c.stdin, c.args...)
		if r.code == 0 {
			t.Fatalf("sdlc %v went through from another session: %s", c.args, r.stdout)
		}
		if lines := ghLines(r.stderr, "analysis", 0.4); len(lines) != 0 {
			t.Errorf("AC-1 %v printed a line before it had the record: %q", c.args, lines)
		}
	}
	ghCounts(t, "AC-2", root, map[string]int{})

	// From the session that holds the story, the same commands read the record.
	setenv(t, store.SessionEnv, "session-holding")
	for _, c := range ghCommands {
		if c.args[0] == "status" {
			continue
		}
		r := runWith(t, c.stdin, c.args...)
		ghSaysOnce(t, "AC-1 from the session holding it", r.stderr, "analysis", 0.4, ghAnalysis)
	}
}

// ------------------------------------------------------------------ AC-2

// AC-2: one event, and the line every time, with a per-story alert of the same
// type on the record before and after: an alert is not the event, and the count
// does not stop at one by counting the type.
func TestAC2OneEventPerGateAcrossRepeatedCommandsAndTheLineKeepsPrinting(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, ghWorked)
	ghSetHours(t, root, ghOverAnalysis())

	if r := mustRun(t, "cost", "add", "--usd", "35"); !strings.Contains(r.stderr, "50%") {
		t.Fatalf("the fixture did not raise a per-story alert: %q", r.stderr)
	}
	if _, alerts := ghBudget(ghRecordOf(t, root, ghStory)); alerts != 1 {
		t.Fatalf("the fixture's per-story alert did not reach the record as a budget event: %d", alerts)
	}
	ghCounts(t, "AC-2 a per-story alert is not the event", root, map[string]int{})

	steps := []struct {
		stdin string
		args  []string
	}{
		{args: []string{"gate", "plan", "fail", "--note", "one"}},
		{args: []string{"status"}},
		{args: []string{"gate", "plan", "fail", "--note", "two"}},
		{stdin: "# architect\n", args: []string{"review", "add", "design_review", "architect", "approve"}},
		{stdin: "# red team\n", args: []string{"review", "add", "design_review", "red-team", "approve"}},
		{args: []string{"status", "--json"}},
	}
	for _, step := range steps {
		r := runWith(t, step.stdin, step.args...)
		if r.code != 0 {
			t.Fatalf("sdlc %v exited %d:\n%s", step.args, r.code, r.stderr)
		}
		ghSaysOnce(t, "AC-2 sdlc "+strings.Join(step.args, " "), r.stderr, "analysis", 0.4, ghAnalysis)
		ghCounts(t, "AC-2 sdlc "+strings.Join(step.args, " "), root, map[string]int{"analysis": 1})
	}

	if r := mustRun(t, "cost", "add", "--usd", "15"); !strings.Contains(r.stderr, "80%") {
		t.Fatalf("the fixture did not raise a second alert: %q", r.stderr)
	}
	r := mustRun(t, "gate", "plan", "fail", "--note", "three")
	ghSaysOnce(t, "AC-2 after a second alert", r.stderr, "analysis", 0.4, ghAnalysis)
	byGate, alerts := ghBudget(ghRecordOf(t, root, ghStory))
	if byGate["analysis"] != 1 || alerts != 2 {
		t.Errorf("AC-2 %v by gate and %d alerts, want analysis once and two alerts", byGate, alerts)
	}
}

// AC-2 is "the first time a gate passes its number", gate by gate: a gate's event
// does not stop another's, and one recorded does not repeat.
func TestAC2ASecondGateGetsItsOwnEventWhenItPassesItsNumber(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, ghWorked)
	hours := map[string]any{"analysis": 0.4, "design_review": 0.5}
	ghSetHours(t, root, hours)
	mustRun(t, "gate", "plan", "fail", "--note", "one")
	ghCounts(t, "AC-2 one gate past its number", root, map[string]int{"analysis": 1})

	// A run from before that command came in late: it falls in design_review's time,
	// and takes that gate past its number too.
	ghAppendAgent(t, root, "later", 3*time.Hour+40*time.Minute, 4*time.Hour+20*time.Minute)
	r := mustRun(t, "gate", "plan", "fail", "--note", "two")
	ghSaysOnce(t, "AC-2 the first gate", r.stderr, "analysis", 0.4, ghAnalysis)
	ghSaysOnce(t, "AC-2 the second gate", r.stderr, "design_review", 0.5, 40*time.Minute)
	ghCounts(t, "AC-2 two gates past their numbers", root, map[string]int{"analysis": 1, "design_review": 1})
	mustRun(t, "gate", "plan", "fail", "--note", "three")
	ghCounts(t, "AC-2 a third command", root, map[string]int{"analysis": 1, "design_review": 1})
}

// ghAppendEvent adds events to the record as it stands; the replay sorts by time.
func ghAppendEvent(t *testing.T, root string, added ...map[string]any) {
	t.Helper()
	path := ghRecordPath(root)
	var doc map[string]any
	if err := json.Unmarshal(ghBytes(t, path), &doc); err != nil {
		t.Fatal(err)
	}
	events, _ := doc["events"].([]any)
	for _, e := range added {
		events = append(events, e)
	}
	doc["events"] = events
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// ghAppendAgent adds a finished agent run to the record as it stands.
func ghAppendAgent(t *testing.T, root, id string, from, to time.Duration) {
	t.Helper()
	ghAppendEvent(t, root,
		map[string]any{"at": model.Timestamp(at(from)), "type": model.EventAgentStart, "agent_type": "a", "agent_id": id, "message": ""},
		map[string]any{"at": model.Timestamp(at(to)), "type": model.EventAgentEnd, "agent_type": "a", "agent_id": id, "message": ""})
}

// THREATS T7: the event is once per gate per story for the whole record, so a gate
// that passes its number, is reopened and passes it by more records nothing more.
func TestAC2AGateReopenedAndFurtherPastItsNumberRecordsNothingMore(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, ghWorked)
	ghSetHours(t, root, map[string]any{"analysis": 0.4})
	mustRun(t, "gate", "plan", "fail", "--note", "one")
	ghCounts(t, "AC-2 the first time", root, map[string]int{"analysis": 1})

	// Reopened, and worked on: both from before that command, so that the clock
	// plays no part, and analysis now has 20m more.
	ghAppendEvent(t, root, map[string]any{"at": model.Timestamp(at(3*time.Hour + 35*time.Minute)),
		"type": model.EventGatesReopened, "message": "analysis reopened"})
	ghAppendAgent(t, root, "again", 3*time.Hour+40*time.Minute, 4*time.Hour)
	r := mustRun(t, "gate", "plan", "fail", "--note", "two")
	ghSaysOnce(t, "AC-2 the line goes on, with the new figure", r.stderr, "analysis", 0.4, 48*time.Minute)
	ghCounts(t, "AC-2 reopened", root, map[string]int{"analysis": 1})
}

// A person settled it: status never writes. Its promise, "It changes nothing", is
// held to all of the loop's state and not only the record.
func TestAC1AC2StatusPrintsTheLineAndWritesNothing(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, ghWorked)
	ghSetHours(t, root, ghOverAnalysis())
	before := ghTree(t, root)
	for _, args := range [][]string{{"status"}, {"status", "--json"}, {"status"}} {
		r := mustRun(t, args...)
		ghSaysOnce(t, "AC-1 sdlc "+strings.Join(args, " "), r.stderr, "analysis", 0.4, ghAnalysis)
	}
	after := ghTree(t, root)
	if !maps.EqualFunc(before, after, bytes.Equal) {
		for path := range after {
			if !bytes.Equal(before[path], after[path]) {
				t.Errorf("AC-2 status changed %s", path)
			}
		}
		for path := range before {
			if _, ok := after[path]; !ok {
				t.Errorf("AC-2 status removed %s", path)
			}
		}
	}
	ghCounts(t, "AC-2 status", root, map[string]int{})

	// And the next command that does save is the one that records it, which is
	// why nothing is lost: at most one command late.
	mustRun(t, "gate", "plan", "fail", "--note", "one")
	ghCounts(t, "AC-2 the next save", root, map[string]int{"analysis": 1})
}

// A person settled it: a refused command changes nothing, and prints the line, for
// a gate refused repeatedly is the circle this story exists to show.
func TestAC2ARefusedCommandPrintsTheLineAndAddsNoEvent(t *testing.T) {
	t.Parallel()
	for _, c := range ghCommands {
		if !c.refused {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, ghWorked)
			ghSetHours(t, root, ghOverAnalysis())
			before := ghBytes(t, ghRecordPath(root))
			for range 3 {
				r := runWith(t, c.stdin, c.args...)
				if r.code == 0 {
					t.Fatalf("the fixture did not do what this row is for: %s", r.stdout)
				}
				ghSaysOnce(t, "AC-1 a refusal", r.stderr, "analysis", 0.4, ghAnalysis)
				if after := ghBytes(t, ghRecordPath(root)); !bytes.Equal(before, after) {
					t.Fatalf("AC-2 the refused command changed the record:\n%s\n%s", before, after)
				}
			}
			mustRun(t, "gate", "plan", "fail", "--note", "one")
			ghCounts(t, "AC-2 the first command to save", root, map[string]int{"analysis": 1})
		})
	}
}

// ANALYSIS Q6: for a story in its own worktree with a list of its commits, a gate
// has saved the record before it refuses. That save is not the event's.
func TestAC2AGateRefusedInTheStorysWorktreeRecordsNoEventWithTheForksItSaves(t *testing.T) {
	t.Parallel()
	main, wt := ktCommitted(t)
	ghAppendAgentsOver(t, main)
	ghSetHours(t, main, map[string]any{"dor": 0.25})
	ghSetHours(t, wt, map[string]any{"dor": 0.25})
	if forks := ktForks(t, swRecord(t, main, ghStory)); len(forks) == 0 {
		t.Fatal("the fixture's record has no list of the story's commits, so the gate does not save before it checks")
	}
	r := wtRun(t, wt, ldSession, "", "gate", "retro", "pass")
	if r.code == 0 {
		t.Fatalf("the retro passed with the commit gate still to run: %s", r.stdout)
	}
	ghSaysOnce(t, "AC-1 a gate refused in its worktree", r.stderr, "dor", 0.25, 30*time.Minute)
	if got, _ := ghBudget(ghRecordOf(t, main, ghStory)); len(got) != 0 {
		t.Errorf("AC-2 a refused gate recorded budget events %v", got)
	}
}

// ANALYSIS section 6: a failing gate or a stopping block ends in a hand-over, which
// saves the record; the event has to be on it by then.
func TestAC2TheEventSurvivesAGateFailureThatHandsTheStoryOver(t *testing.T) {
	t.Parallel()
	root := hoGateAtTheLimit(t)
	ghAppendAgentsOver(t, root)
	ghSetHours(t, root, map[string]any{"dor": 0.25})

	r := run(t, "gate", "dor", "fail", "--note", "still not testable", "--json")
	if r.code != 0 {
		t.Fatalf("the hand-over did not go through: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if got := decode[gatePayload](t, r); got.HandedOver == "" {
		t.Fatalf("the fixture did not hand the story over: %s", r.stdout)
	}
	ghSaysOnce(t, "AC-1 at the hand-over", r.stderr, "dor", 0.25, 30*time.Minute)
	ghCounts(t, "AC-2 at the hand-over", root, map[string]int{"dor": 1})
	if got := hoStatus(t, root); got != string(model.StatusAwaitingHuman) {
		t.Errorf("the backlog calls the story %q", got)
	}
}

// ghAppendAgentsOver gives a story begun by a real start thirty minutes of an
// agent's time before anything that has happened to it: the gate in force then is
// dor, which nothing has passed.
func ghAppendAgentsOver(t *testing.T, root string) {
	t.Helper()
	path := ghRecordPath(root)
	var doc map[string]any
	if err := json.Unmarshal(ghBytes(t, path), &doc); err != nil {
		t.Fatal(err)
	}
	doc["created"] = model.Timestamp(at(0))
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	ghAppendAgent(t, root, "before", time.Minute, 31*time.Minute)
}

func TestAC2TheEventSurvivesABlockThatHandsTheStoryOver(t *testing.T) {
	t.Parallel()
	root := hoBlockedOnce(t)
	ghAppendAgentsOver(t, root)
	ghSetHours(t, root, map[string]any{"dor": 0.25})

	r := runWith(t, "# architect\n", append(slices.Clone(hoSecondBlock), "--json")...)
	if r.code != 0 {
		t.Fatalf("the hand-over did not go through: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	if got := decode[reviewPayload](t, r); got.HandedOver == "" {
		t.Fatalf("the fixture did not hand the story over: %s", r.stdout)
	}
	ghSaysOnce(t, "AC-1 at the hand-over", r.stderr, "dor", 0.25, 30*time.Minute)
	ghCounts(t, "AC-2 at the hand-over", root, map[string]int{"dor": 1})
}

// US-008's rule stands: a hand-over that cannot be completed leaves the record byte
// for byte, so no event; the line has been printed by then, and the command run
// again counts the failure and records the event once.
func TestAC2AHandOverThatCannotBeCompletedLeavesNoEventAndTheCommandAgainRecordsOne(t *testing.T) {
	hoEachCause(t, func(t *testing.T, cause hoCause) {
		t.Helper()
		root := hoGateAtTheLimit(t)
		ghAppendAgentsOver(t, root)
		ghSetHours(t, root, map[string]any{"dor": 0.25})
		before := hoRecordBytes(t, root)
		mend := cause.stop(t, root)

		r := run(t, "gate", "dor", "fail", "--note", "still not testable")
		if r.code == 0 {
			t.Fatalf("the hand-over went through with %s: %s", cause.name, r.stdout)
		}
		if !strings.Contains(r.stderr, "gate") {
			t.Errorf("the refusal has gone: %s", r.stderr)
		}
		ghSaysOnce(t, "AC-1 a refused hand-over", r.stderr, "dor", 0.25, 30*time.Minute)
		if after := hoRecordBytes(t, root); !bytes.Equal(before, after) {
			t.Errorf("AC-2 the refused hand-over changed the record:\n%s\n%s", before, after)
		}
		mend()

		mustRun(t, "gate", "dor", "fail", "--note", "still not testable")
		ghCounts(t, "AC-2 the command again", root, map[string]int{"dor": 1})
		after := ghRecordOf(t, root, ghStory)
		if n := after.SinceDecision(model.EventGate, hoDorFailed); n != 2 {
			t.Errorf("the failure was counted %d times, want twice", n)
		}
	})
}

// A save that fails is a command that recorded nothing: no event in the record, and
// the next command to save has it all to do.
func TestAC2ASaveThatFailsRecordsNoEventAndTheNextOneDoes(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	for _, tc := range []struct {
		name  string
		stdin string
		args  []string
	}{
		{name: "a gate", args: []string{"gate", "plan", "fail", "--note", "one"}},
		{name: "a gate that passes", args: []string{"gate", "dor", "pass"}},
		{name: "a review", stdin: "# architect\n", args: []string{"review", "add", "design_review", "architect", "approve"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, ghWorked)
			ghSetHours(t, root, map[string]any{"analysis": 0.4})
			before := ghBytes(t, ghRecordPath(root))
			mend := hoDeny(t, filepath.Dir(ghRecordPath(root)))

			r := runWith(t, tc.stdin, tc.args...)
			if r.code == 0 {
				t.Fatalf("the save went through in a directory that may not be written: %s", r.stdout)
			}
			ghSaysOnce(t, "AC-1 a failed save", r.stderr, "analysis", 0.4, ghAnalysis)
			if after := ghBytes(t, ghRecordPath(root)); !bytes.Equal(before, after) {
				t.Errorf("AC-2 the failed save changed the record:\n%s", after)
			}
			mend()

			mustRunWith(t, tc.stdin, tc.args...)
			ghCounts(t, "AC-2 the next save", root, map[string]int{"analysis": 1})
		})
	}
}

// ------------------------------------------------------------------ AC-2 trace

// ghRows is the gate rows of `sdlc trace --json`, as they are: the marker's form is
// the planner's, a key beside the figures.
func ghRows(t *testing.T, r result) map[string]map[string]any {
	t.Helper()
	var doc struct {
		Gates []map[string]any `json:"gates"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Fatalf("sdlc trace --json is not one document: %v\n%s", err, r.stdout)
	}
	rows := map[string]map[string]any{}
	for _, row := range doc.Gates {
		gate, _ := row["gate"].(string)
		rows[gate] = row
	}
	return rows
}

// ghMarked is the gates whose row in `sdlc trace --json` holds something beyond the
// figures every row has, and not empty, false, zero or null.
func ghMarked(rows map[string]map[string]any) []string {
	figures := []string{"gate", "wall", "agents", "person", "between", "unaccounted"}
	var out []string
	for gate, row := range rows {
		for key, v := range row {
			if slices.Contains(figures, key) {
				continue
			}
			switch x := v.(type) {
			case nil:
				continue
			case bool:
				if !x {
					continue
				}
			case string:
				if x == "" {
					continue
				}
			case float64:
				if x == 0 {
					continue
				}
			}
			out = append(out, gate)
			break
		}
	}
	slices.Sort(out)
	return out
}

// ghTextRows is the rows of the time table in `sdlc trace`, by gate.
func ghTextRows(t *testing.T, stdout string) map[string]string {
	t.Helper()
	rows := map[string]string{}
	inTable := false
	for line := range strings.SplitSeq(stdout, "\n") {
		switch {
		case strings.HasPrefix(line, "time, by the gate"):
			inTable = true
			continue
		case !inTable:
			continue
		case line == "":
			return rows
		}
		fields := strings.Fields(line)
		if len(fields) > 0 && slices.Contains(model.Gates, model.Gate(fields[0])) {
			rows[fields[0]] = line
		}
	}
	return rows
}

// ghNeutralise turns the record's budget events into a type
// the replay passes over, so that the story's span and every figure stay the same
// and only the mark can differ.
func ghNeutralise(t *testing.T, root string) {
	t.Helper()
	path := ghRecordPath(root)
	var doc map[string]any
	if err := json.Unmarshal(ghBytes(t, path), &doc); err != nil {
		t.Fatal(err)
	}
	events, _ := doc["events"].([]any)
	for _, raw := range events {
		e, _ := raw.(map[string]any)
		if e["type"] == "budget" {
			e["type"] = "budget_neutralised"
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// AC-2: trace marks that gate and no other. The two runs differ only in the event,
// so whatever else differs on a row is the mark.
func TestAC2SdlcTraceMarksTheGateThatPassedItsNumberAndNoOther(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, ghWorked)
	ghSetHours(t, root, ghOverAnalysis())
	mustRun(t, "gate", "plan", "fail", "--note", "one")
	ghCounts(t, "the fixture", root, map[string]int{"analysis": 1})

	before := ghBytes(t, ghRecordPath(root))
	marked := mustRun(t, "trace", ghStory)
	markedJSON := mustRun(t, "trace", ghStory, "--json")
	if after := ghBytes(t, ghRecordPath(root)); !bytes.Equal(before, after) {
		t.Error("sdlc trace only reads, and it changed the record")
	}
	if got := ghMarked(ghRows(t, markedJSON)); !slices.Equal(got, []string{"analysis"}) {
		t.Errorf("AC-2 sdlc trace --json marks %v, want analysis alone:\n%s", got, markedJSON.stdout)
	}

	ghNeutralise(t, root)
	plain := ghTextRows(t, mustRun(t, "trace", ghStory).stdout)
	rows := ghTextRows(t, marked.stdout)
	if len(rows) == 0 || len(rows) != len(plain) {
		t.Fatalf("the table has %d rows with the event and %d without", len(rows), len(plain))
	}
	for gate, row := range rows {
		same := row == plain[gate]
		if gate == "analysis" && same {
			t.Errorf("AC-2 sdlc trace does not mark analysis:\n%s", row)
		}
		if gate != "analysis" && !same {
			t.Errorf("AC-2 sdlc trace marks %s:\n%s\n%s", gate, row, plain[gate])
		}
	}
	// The mark is beside the figures, not in place of them.
	if !strings.Contains(rows["analysis"], humanTime(ghAnalysis)) {
		t.Errorf("AC-2 the marked row lost its agents' time:\n%s", rows["analysis"])
	}
	d := decode[traceDoc](t, markedJSON)
	seconds(t, "AC-2 the marked row's agents' time", d.gate(t, model.GateAnalysis).Agents, ghAnalysis)
}

// Decided from AC-2's words, "the gate record shall gain a `budget` event ... and
// `sdlc trace` shall mark that gate": the mark is the record's event, and sdlc trace
// only reads. So a gate past its number with no event yet, which is where a status
// leaves it, has no mark, and the next save brings both.
func TestAC2AGatePastItsNumberWithNoEventYetHasNoMarkInTrace(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, ghWorked)
	ghSetHours(t, root, ghOverAnalysis())
	mustRun(t, "status")
	ghCounts(t, "the fixture", root, map[string]int{})
	before := ghBytes(t, ghRecordPath(root))

	if got := ghMarked(ghRows(t, mustRun(t, "trace", ghStory, "--json"))); len(got) != 0 {
		t.Errorf("AC-2 sdlc trace --json marks %v with no event on the record", got)
	}
	unmarked := ghTextRows(t, mustRun(t, "trace", ghStory).stdout)
	if after := ghBytes(t, ghRecordPath(root)); !bytes.Equal(before, after) {
		t.Error("sdlc trace only reads, and it changed the record")
	}

	mustRun(t, "gate", "plan", "fail", "--note", "one")
	if got := ghMarked(ghRows(t, mustRun(t, "trace", ghStory, "--json"))); !slices.Equal(got, []string{"analysis"}) {
		t.Errorf("AC-2 sdlc trace --json marks %v once the event is there, want analysis", got)
	}
	if ghTextRows(t, mustRun(t, "trace", ghStory).stdout)["analysis"] == unmarked["analysis"] {
		t.Error("AC-2 sdlc trace does not mark analysis once the event is there")
	}
}

// The mark says what happened to the story, so taking the number out of the file
// afterwards does not unsay it.
func TestAC2TheMarkStaysWhenTheNumberIsTakenOutOfTheConfiguration(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, ghWorked)
	ghSetHours(t, root, ghOverAnalysis())
	mustRun(t, "gate", "plan", "fail", "--note", "one")
	ghSetHours(t, root, nil)
	if got := ghMarked(ghRows(t, mustRun(t, "trace", ghStory, "--json"))); !slices.Equal(got, []string{"analysis"}) {
		t.Errorf("AC-2 sdlc trace --json marks %v, want analysis", got)
	}
}

// Both are of type budget, and a per-story alert is about no gate.
func TestAC2APerStoryAlertMarksNoGateInTrace(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, ghWorked)
	ghSetHours(t, root, ghOverAnalysis())
	mustRun(t, "cost", "add", "--usd", "60")
	if _, alerts := ghBudget(ghRecordOf(t, root, ghStory)); alerts == 0 {
		t.Fatal("the fixture raised no per-story alert")
	}
	if got := ghMarked(ghRows(t, mustRun(t, "trace", ghStory, "--json"))); len(got) != 0 {
		t.Errorf("AC-2 sdlc trace --json marks %v for a per-story alert", got)
	}
	mustRun(t, "gate", "plan", "fail", "--note", "one")
	if got := ghMarked(ghRows(t, mustRun(t, "trace", ghStory, "--json"))); !slices.Equal(got, []string{"analysis"}) {
		t.Errorf("AC-2 with an alert beside the event, sdlc trace --json marks %v, want analysis", got)
	}
}

// A story's budget events are that story's: one marked is not another's mark.
func TestAC2TheEventAndTheMarkAreEachStorysOwn(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-2")
	mustRun(t, "start", ghStory)
	ghRewrite(t, root, ghWorked)
	newGateRecord("T-2").pass(model.GateDoR, time.Minute).event("loop_end", "", time.Hour).save(t, root)
	ghSetHours(t, root, map[string]any{"analysis": 0.4})

	mustRun(t, "gate", "plan", "fail", "--note", "one")
	ghCounts(t, "AC-2 the story worked on", root, map[string]int{"analysis": 1})
	if got := ghMarked(ghRows(t, mustRun(t, "trace", "T-2", "--json"))); len(got) != 0 {
		t.Errorf("AC-2 another story is marked at %v", got)
	}
}

// ------------------------------------------------------------------ AC-3

// AC-3: silent, with the key absent or empty, on every command.
func TestAC3AbsentOrNamingNoGateNothingIsPrintedOrRecorded(t *testing.T) {
	t.Parallel()
	for name, hours := range map[string]any{"absent": nil, "an empty map": map[string]any{}} {
		for _, c := range ghCommands {
			for _, form := range []string{"prose", "json"} {
				t.Run(name+" "+c.name+" "+form, func(t *testing.T) {
					t.Parallel()
					root := ghStarted(t, ghWorked)
					before := ghTree(t, root)
					args := c.args
					if form == "json" {
						args = append(slices.Clone(args), "--json")
					}
					off, on := ghTwice(t, root, hours, c.stdin, args...)
					if on.stderr != off.stderr || on.stdout != off.stdout || on.code != off.code {
						t.Errorf("AC-3 the command changed.\nabsent: exit %d\n%s%s\nthis: exit %d\n%s%s",
							off.code, off.stdout, off.stderr, on.code, on.stdout, on.stderr)
					}
					ghCounts(t, "AC-3", root, map[string]int{})

					// The map naming a gate, on the same story and the same command.
					r := ghControl(t, root, before, ghOverAnalysis(), c.stdin, args...)
					ghSaysOnce(t, "AC-3 naming a gate", r.stderr, "analysis", 0.4, ghAnalysis)
				})
			}
		}
	}
}

// A value that fails the loader is refused by every command that loads the file, in
// words that name the key and what is allowed.
var ghBadHours = []struct {
	name, key string
	value     any
}{
	{"a negative number", "plan", map[string]any{"plan": -1}},
	{"a string", "plan", map[string]any{"plan": "1"}},
	{"null", "plan", map[string]any{"plan": nil}},
	{"true", "plan", map[string]any{"plan": true}},
	{"a key in another case", "Plan", map[string]any{"Plan": 1}},
	{"a key that is not a gate", "nope", map[string]any{"nope": 1}},
	{"a list", "budget.gate_hours", []any{1}},
}

func ghNamesTheKeyAndTheGates(t *testing.T, what, said, key string) {
	t.Helper()
	for _, want := range []string{"gate_hours", key} {
		if !strings.Contains(said, want) {
			t.Errorf("%s: the refusal does not name %q:\n%s", what, want, said)
		}
	}
	for _, g := range model.Gates {
		if !strings.Contains(said, string(g)) {
			t.Errorf("%s: the refusal does not name the gate key %s:\n%s", what, g, said)
		}
	}
}

func TestAC3EveryCommandThatLoadsTheConfigurationRefusesABadValueWithE0045(t *testing.T) {
	t.Parallel()
	for _, bad := range ghBadHours {
		t.Run(bad.name, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, ghWorked)
			ghSetHours(t, root, bad.value)
			before := ghBytes(t, ghRecordPath(root))
			for _, args := range [][]string{
				{"status"},
				{"gate", "plan", "fail", "--note", "one"},
				{"review", "add", "design_review", "architect", "approve"},
				{"cost"},
				{"trace", ghStory},
				{"review", "list"},
			} {
				r := runWith(t, "# x\n", args...)
				what := "sdlc " + strings.Join(args, " ")
				if r.code == 0 {
					t.Errorf("AC-3 %s accepted %s", what, bad.name)
					continue
				}
				if !strings.Contains(r.stderr, "SDLC-E0045") {
					t.Errorf("AC-3 %s was not refused with SDLC-E0045:\n%s", what, r.stderr)
				}
				ghNamesTheKeyAndTheGates(t, "AC-3 "+what, r.stderr, bad.key)
			}
			if after := ghBytes(t, ghRecordPath(root)); !bytes.Equal(before, after) {
				t.Error("AC-3 a refused configuration changed the record")
			}
		})
	}
}

func TestAC3RefusalInJSONCarriesE0045(t *testing.T) {
	t.Parallel()
	root := ghStarted(t, ghWorked)
	ghSetHours(t, root, map[string]any{"Plan": 1})
	r := run(t, "status", "--json")
	if r.code == 0 {
		t.Fatalf("AC-3 accepted: %s", r.stdout)
	}
	var got struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r.stdout)
	}
	if got.Code != "SDLC-E0045" {
		t.Errorf("AC-3 code = %q, want SDLC-E0045:\n%s", got.Code, r.stdout)
	}
}

// doctor loads the file itself and reports a configuration it cannot use as a check
// that failed, in the error's own words. AC-3 names doctor and the code.
func TestAC3DoctorRefusesABadValueNamingE0045TheKeyAndTheGateKeys(t *testing.T) {
	t.Parallel()
	for _, bad := range ghBadHours {
		for _, form := range []string{"prose", "json"} {
			t.Run(bad.name+" "+form, func(t *testing.T) {
				t.Parallel()
				root := ghStarted(t, ghWorked)
				ghSetHours(t, root, bad.value)
				args := []string{"doctor"}
				if form == "json" {
					args = append(args, "--json")
				}
				r := run(t, args...)
				if r.code == 0 {
					t.Errorf("AC-3 doctor passed a configuration holding %s:\n%s", bad.name, r.stdout)
				}
				said := r.stdout + r.stderr
				if !strings.Contains(said, "SDLC-E0045") {
					t.Errorf("AC-3 doctor did not say SDLC-E0045:\n%s", said)
				}
				ghNamesTheKeyAndTheGates(t, "AC-3 doctor", said, bad.key)
			})
		}
	}
}

// A key doctor does not know is one it reports as changing nothing, and a number
// set here changes something: doctor reads the setting as the setting it is.
func TestAC3DoctorDoesNotCallGoodNumbersASettingItDoesNotKnow(t *testing.T) {
	t.Parallel()
	for _, form := range []string{"prose", "json"} {
		t.Run(form, func(t *testing.T) {
			t.Parallel()
			root := ghStarted(t, ghWorked)
			ghSetHours(t, root, ghEveryGate(0.5))
			args := []string{"doctor"}
			if form == "json" {
				args = append(args, "--json")
			}
			r := run(t, args...)
			if said := r.stdout + r.stderr; strings.Contains(said, "gate_hours") {
				t.Errorf("AC-3 doctor has something to say of good numbers:\n%s", said)
			}
		})
	}
}

// ANALYSIS section 4, item 8: the landing reads trunk's file with Parse, and a bad
// setting committed there is refused like any other.
func TestAC3ABadGateHoursCommittedOnTrunkIsRefusedAtTheLanding(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
	orig := wtRead(t, filepath.Join(main, config.File))
	ghSetHours(t, main, map[string]any{"Plan": 1})
	bad := wtRead(t, filepath.Join(main, config.File))
	swCommitFile(t, main, config.File, bad, "a mistyped key on trunk")
	// The disk copy reads, so that what fails is trunk's commit, read through git.
	writeFile(t, main, config.File, orig)

	wtRefused(t, "SDLC-E0045", wt, ldSession, "", "stop")
	ldStillHeld(t, main, wt)
}

// ------------------------------------------------------------------ AC-4

func TestAC4InitWritesNoGateHours(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	raw := ghBytes(t, filepath.Join(root, filepath.FromSlash(config.File)))
	if bytes.Contains(raw, []byte("gate_hours")) {
		t.Errorf("AC-4 sdlc init wrote gate_hours:\n%s", raw)
	}
	// A project sets its own: on what init wrote, a story past any number is silent
	// until a person adds one.
	mustRun(t, "start")
	ghRewrite(t, root, ghWorked)
	if lines := ghLines(mustRun(t, "status").stderr, "analysis", 0.4); len(lines) != 0 {
		t.Errorf("AC-4 what init wrote has a number in force: %q", lines)
	}
	ghSetHours(t, root, ghOverAnalysis())
	ghSaysOnce(t, "AC-4 a number a person added", mustRun(t, "status").stderr, "analysis", 0.4, ghAnalysis)
}

// ghBudgetSection is the budget section of docs/configuration.md, with each run of
// white space inside a block made one space, blocks still apart.
func ghBudgetSection(t *testing.T) []string {
	t.Helper()
	text := strings.ReplaceAll(page(t, "configuration.md"), "\r\n", "\n")
	_, rest, found := strings.Cut(text, "## `budget`\n")
	if !found {
		t.Fatal("docs/configuration.md has no budget section")
	}
	section, _, _ := strings.Cut(rest, "\n## ")
	var blocks []string
	for block := range strings.SplitSeq(section, "\n\n") {
		if b := strings.Join(strings.Fields(block), " "); b != "" {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

func TestAC4TheBudgetSectionDescribesGateHoursAndThatItNeverBlocksAndWhy(t *testing.T) {
	t.Parallel()
	blocks := ghBudgetSection(t)
	whole := strings.Join(blocks, "\n")
	if !strings.Contains(whole, "gate_hours") {
		t.Fatalf("AC-4 the budget section does not describe gate_hours:\n%s", whole)
	}
	// "Nothing blocks" is what the section says of the per-story budget today, so
	// that sentence cannot be the one this holds to; and the reason has to be beside
	// the claim.
	said := false
	for _, block := range blocks {
		if !strings.Contains(strings.ToLower(block), "never blocks") {
			continue
		}
		said = true
		if !strings.Contains(block, "halfway") {
			t.Errorf("AC-4 the docs say gate_hours never blocks, and not there why (a story stopped halfway costs more):\n%s", block)
		}
	}
	if !said {
		t.Errorf("AC-4 the budget section does not say gate_hours never blocks:\n%s", whole)
	}
}

// The figures AC-4 asks for are in the story's notes (ANALYSIS section 1), and a
// person settled that the docs say they include idle sessions, which agents' time
// does not.
func TestAC4TheBudgetSectionGivesEveryMedianAndUpperQuartile(t *testing.T) {
	t.Parallel()
	blocks := ghBudgetSection(t)
	whole := strings.Join(blocks, "\n")
	for _, tc := range []struct{ gate, median, upper string }{
		{"analysis", "0.27", "0.39"},
		{"tests_frozen", "0.80", "1.54"},
		{"plan", "0.53", "0.94"},
		{"design_review", "0.49", "0.96"},
		{"implementation", "0.65", "1.32"},
		{"verification", "0.52", "1.31"},
		{"code_review", "0.31", "0.99"},
	} {
		// Together, in that order, and close: a table row, or a list item, or a
		// sentence, with nothing but its own figures between the key and them.
		found := regexp.MustCompile("(^|[^a-z_])" + tc.gate + "([^a-z_][^0-9]{0,40})" +
			regexp.QuoteMeta(tc.median) + "[^0-9]{1,20}" + regexp.QuoteMeta(tc.upper) + "($|[^0-9])").MatchString(whole)
		if !found {
			t.Errorf("AC-4 the budget section does not give %s %s and %s together, median first", tc.gate, tc.median, tc.upper)
		}
	}
	for _, want := range []string{"median", "upper quartile", "idle"} {
		if !strings.Contains(strings.ToLower(whole), want) {
			t.Errorf("AC-4 the budget section does not say %q:\n%s", want, whole)
		}
	}
}
