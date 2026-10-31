package e2e

// US-112.6 AC-4: the built binary, through the plugin's launcher, in a real git
// repository with the story in its own worktree, shows the warning line and the
// single budget event. Agents' time comes from the record, written here at fixed
// past times: the hook stamps its events with the clock, and hours of an agent's
// time cannot be had from it without waiting them out. The words of the line and
// of the event are the planner's, so a line is told by the gate key, the number
// and the time, and an event by its type and the gate key.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

var ghToken = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?[a-z0-9]*`)

// ghNames is whether text names the gate key as a word.
func ghNames(text, key string) bool {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(key) + `($|[^A-Za-z0-9_])`).MatchString(text)
}

// ghSetsHours writes budget.gate_hours into a checkout's configuration; nil takes
// it out.
func ghSetsHours(t *testing.T, dir string, hours any) {
	t.Helper()
	path := filepath.Join(dir, ".sdlc", "config.json")
	var cfg map[string]any
	readJSON(t, path, &cfg)
	budget, ok := cfg["budget"].(map[string]any)
	if !ok {
		t.Fatalf("the scaffolded configuration has no budget: %v", cfg)
	}
	if hours == nil {
		delete(budget, "gate_hours")
	} else {
		budget["gate_hours"] = hours
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// ghAgentsWorked gives the story, nothing passed yet, 35 minutes of agents' time
// on dor: two overlapping runs, 20m and 30m, and one that never ended.
func ghAgentsWorked(t *testing.T, record string) {
	t.Helper()
	var doc map[string]any
	readJSON(t, record, &doc)
	agent := func(at, kind, id string) map[string]any {
		return map[string]any{"at": "2026-10-01T" + at + "Z", "type": kind, "agent_type": "sdlc:researcher", "agent_id": id, "message": ""}
	}
	doc["created"] = "2026-10-01T10:00:00Z"
	doc["events"] = []map[string]any{
		{"at": "2026-10-01T10:00:00Z", "type": "loop_start", "message": ""},
		agent("10:05:00", "agent_start", "e1"),
		agent("10:10:00", "agent_start", "e2"),
		agent("10:25:00", "agent_end", "e1"),
		agent("10:30:00", "agent_start", "open"),
		agent("10:40:00", "agent_end", "e2"),
		{"at": "2026-10-01T12:00:00Z", "type": "loop_end", "message": ""},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// ghBudgetEvents is how many budget events of the record name the gate.
func ghBudgetEvents(t *testing.T, record, gate string) int {
	t.Helper()
	var r model.Record
	readJSON(t, record, &r)
	n := 0
	for _, e := range r.Events {
		if e.Type == "budget" && ghNames(e.Message, gate) {
			n++
		}
	}
	return n
}

// ghWarns holds stderr to the one line, naming the gate, the agents' time as sdlc
// trace prints it or in hours, and the number.
func ghWarns(t *testing.T, what, stderr string) {
	t.Helper()
	// A refusal's text may name dor; it does not also state its number.
	statesNumber := func(line string) bool {
		for _, token := range ghToken.FindAllString(line, -1) {
			if v, err := strconv.ParseFloat(strings.TrimSuffix(token, "h"), 64); err == nil && v == 0.5 {
				return true
			}
		}
		return false
	}
	var lines []string
	for line := range strings.SplitSeq(stderr, "\n") {
		if ghNames(line, "dor") && statesNumber(line) {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("AC-4 %s: %d lines name dor and 0.5, want one:\n%s", what, len(lines), stderr)
	}
	tokens := ghToken.FindAllString(lines[0], -1)
	spent := false
	for _, form := range []string{"35m", "35m0s", "0.6", "0.6h", "0.58", "0.58h", "0.583", "0.583h"} {
		spent = spent || slices.Contains(tokens, form)
	}
	if !spent {
		t.Errorf("AC-4 %s: the line does not state 35m of agents' time:\n%s", what, lines[0])
	}
}

func TestAC4TheBuiltBinaryShowsTheWarningLineAndTheSingleBudgetEventInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	written, err := os.ReadFile(filepath.Join(main, ".sdlc", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), "gate_hours") {
		t.Errorf("AC-4 sdlc init wrote gate_hours:\n%s", written)
	}
	sb.works(t, main, "", "", "start")
	record := filepath.Join(main, ".sdlc", "stories", "US-001", model.RecordFile)
	ghAgentsWorked(t, record)

	// Without the setting first: what the command has to go on printing.
	without := sb.does(t, tree, "", "", "status")
	withoutJSON := sb.does(t, tree, "", "", "status", "--json")
	if strings.Contains(without.stderr, "dor") {
		t.Fatalf("the fixture's status names dor on standard error before the setting is set:\n%s", without.stderr)
	}

	hours := map[string]any{"dor": 0.5, "analysis": 0.01, "plan": 0}
	ghSetsHours(t, main, hours)
	ghSetsHours(t, tree, hours)
	before, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}

	// Status says so and writes nothing: the event waits for a command that saves.
	status := sb.does(t, tree, "", "", "status")
	ghWarns(t, "sdlc status", status.stderr)
	if status.stdout != without.stdout || status.code != without.code {
		t.Errorf("AC-1 status changed: exit %d, %d\nwithout:\n%s\nwith:\n%s", without.code, status.code, without.stdout, status.stdout)
	}
	statusJSON := sb.does(t, tree, "", "", "status", "--json")
	ghWarns(t, "sdlc status --json", statusJSON.stderr)
	if statusJSON.stdout != withoutJSON.stdout {
		t.Errorf("AC-1 status --json changed:\nwithout:\n%s\nwith:\n%s", withoutJSON.stdout, statusJSON.stdout)
	}
	if after, err := os.ReadFile(record); err != nil || string(after) != string(before) {
		t.Errorf("AC-2 sdlc status changed the record (%v)", err)
	}
	if n := ghBudgetEvents(t, record, "dor"); n != 0 {
		t.Errorf("AC-2 sdlc status recorded %d events", n)
	}

	// A refused gate says so and records nothing.
	refused := sb.does(t, tree, "", "", "gate", "analysis", "pass")
	if refused.code == 0 {
		t.Fatalf("the analysis passed with nothing written: %s", refused.stdout)
	}
	ghWarns(t, "a refused sdlc gate", refused.stderr)
	if n := ghBudgetEvents(t, record, "dor"); n != 0 {
		t.Errorf("AC-2 a refused gate recorded %d events", n)
	}

	// The first command to save records it, once; the next ones say so and do not.
	for i, run := range []struct {
		agent, stdin string
		args         []string
	}{
		{"", "", []string{"gate", "dor", "pass", "--note", "first"}},
		{"", "", []string{"gate", "dor", "pass", "--note", "second"}},
		{"architect", "# architect\n", []string{"review", "add", "design_review", "architect", "approve"}},
	} {
		r := sb.does(t, tree, run.agent, run.stdin, run.args...)
		if r.code != 0 {
			t.Fatalf("sdlc %v exited %d:\n%s%s", run.args, r.code, r.stdout, r.stderr)
		}
		ghWarns(t, "sdlc "+strings.Join(run.args, " "), r.stderr)
		if n := ghBudgetEvents(t, record, "dor"); n != 1 {
			t.Errorf("AC-2 after command %d the record has %d budget events naming dor, want one", i+1, n)
		}
	}
	for _, g := range []string{"analysis", "plan"} {
		if n := ghBudgetEvents(t, record, g); n != 0 {
			t.Errorf("AC-2 %s has %d budget events and has not passed its number", g, n)
		}
	}

	// And sdlc trace marks that gate, and no other.
	var doc struct {
		Gates []map[string]any `json:"gates"`
	}
	out := sb.works(t, tree, "", "", "trace", "US-001", "--json")
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("sdlc trace --json printed something that is not one document: %v\n%s", err, out)
	}
	figures := []string{"gate", "wall", "agents", "person", "between", "unaccounted"}
	var marked []string
	for _, row := range doc.Gates {
		for key, v := range row {
			if !slices.Contains(figures, key) && v != nil && v != false && v != "" && v != float64(0) {
				gate, _ := row["gate"].(string)
				marked = append(marked, gate)
			}
		}
	}
	if !slices.Equal(marked, []string{"dor"}) {
		t.Errorf("AC-2 sdlc trace --json marks %v, want dor alone:\n%s", marked, out)
	}
}

// AC-3 through the binary: a bad value stops doctor and an ordinary command alike.
func TestAC3TheBuiltBinaryRefusesABadGateHoursWithE0045InTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	ghSetsHours(t, main, map[string]any{"Plan": 1})
	ghSetsHours(t, tree, map[string]any{"Plan": 1})

	for _, args := range [][]string{{"doctor"}, {"status"}, {"gate", "dor", "pass"}} {
		r := sb.person(t, tree, args...)
		said := r.stdout + r.stderr
		if r.code == 0 {
			t.Errorf("AC-3 sdlc %v accepted a key that is not a gate key:\n%s", args, said)
		}
		if !strings.Contains(said, "SDLC-E0045") || !strings.Contains(said, "Plan") {
			t.Errorf("AC-3 sdlc %v does not say SDLC-E0045 and the key:\n%s", args, said)
		}
		for _, g := range model.Gates {
			if !strings.Contains(said, string(g)) {
				t.Errorf("AC-3 sdlc %v does not name the gate key %s:\n%s", args, g, said)
			}
		}
	}
}
