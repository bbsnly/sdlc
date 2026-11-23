package model

// US-134 AC-3 and AC-13: the results a gate run and a freeze run record are part
// of the record's format. They are read back as they were written, a record an
// older sdlc wrote without them reads and saves as it did, and a result does not
// outlive the pass that recorded it (THREATS B1). Each is held through the
// record's JSON, the only form a reader outside this package has.

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

// us134Saved is raw read into a Record and written again, read back as plain
// JSON: what the next reader of the file finds.
func us134Saved(t *testing.T, raw string, change func(*Record)) map[string]any {
	t.Helper()
	var r Record
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("the record does not load: %v", err)
	}
	if change != nil {
		change(&r)
	}
	out, err := json.Marshal(&r)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(out, &saved); err != nil {
		t.Fatal(err)
	}
	return saved
}

func us134Gate(saved map[string]any) map[string]any {
	gates, _ := saved["gates"].(map[string]any)
	entry, _ := gates[string(GateImplementation)].(map[string]any)
	return entry
}

const us134WithResults = `{
  "story": "US-1", "created": "2026-11-21T10:00:00Z",
  "gates": {
    "plan": {"status": "pass", "at": "2026-11-21T10:00:00Z"},
    "implementation": {
      "status": "pass", "at": "2026-11-21T11:00:00Z", "tree_hash": "4b825dc642cb6eb9a060e54bf8d69288fbee4904",
      "commands": [
        {"name": "build", "exit": 0, "duration_ms": 1500},
        {"name": "test", "exit": 0, "duration_ms": 61000}
      ]
    }
  },
  "freeze_run": {"name": "commands.test_files", "exit": 1, "duration_ms": 2500, "tree_hash": "9d1dcfdaf1a6857c5f83dc27019c7600e1ffaff8"},
  "events": [], "escalations": []
}`

const us134WithoutResults = `{
  "story": "US-1", "created": "2026-11-21T10:00:00Z",
  "gates": {"implementation": {"status": "pass", "at": "2026-11-21T11:00:00Z", "note": "frozen tests green"}},
  "events": [], "escalations": []
}`

// AC-3: each command's name, exit status and duration, and the tree hash, are
// kept through a load and a save; AC-13: so is the freeze's run.
func TestAC3AndAC13TheRecordedRunsAreKeptThroughALoadAndASave(t *testing.T) {
	t.Parallel()
	saved := us134Saved(t, us134WithResults, nil)
	var want map[string]any
	if err := json.Unmarshal([]byte(us134WithResults), &want); err != nil {
		t.Fatal(err)
	}
	got, _ := us134Gate(saved)["commands"].([]any)
	w, _ := us134Gate(want)["commands"].([]any)
	if len(got) != len(w) {
		t.Fatalf("AC-3 the implementation gate's results came back as %v, want %v", got, w)
	}
	for i := range w {
		us134Holds(t, "AC-3 a command's result", got[i], w[i])
	}
	if got := us134Gate(saved)["tree_hash"]; got != "4b825dc642cb6eb9a060e54bf8d69288fbee4904" {
		t.Errorf("AC-3 the tree hash came back as %v", got)
	}
	us134Holds(t, "AC-13 the freeze's run", saved["freeze_run"], want["freeze_run"])
}

// us134Holds holds got to every field of want: a field the plan adds beside
// them, a digest or a threshold, is the plan's.
func us134Holds(t *testing.T, what string, got, want any) {
	t.Helper()
	g, _ := got.(map[string]any)
	w, _ := want.(map[string]any)
	for key, value := range w {
		if !reflect.DeepEqual(g[key], value) {
			t.Errorf("%s came back with %s %v, want %v: %v", what, key, g[key], value, g)
		}
	}
}

// AC-3 is met here already and this holds it: a record written before the
// results existed loads and is saved with nothing added, so an older sdlc reads
// what this one saves.
func TestAC3ARecordWrittenBeforeTheResultsLoadsAndSavesAsBefore(t *testing.T) {
	t.Parallel()
	saved := us134Saved(t, us134WithoutResults, nil)
	entry := us134Gate(saved)
	if entry["status"] != "pass" || entry["note"] != "frozen tests green" {
		t.Errorf("AC-3 the older record's gate came back as %v", entry)
	}
	for _, key := range []string{"commands", "tree_hash"} {
		if _, added := entry[key]; added {
			t.Errorf("AC-3 saving an older record added %q to its gate: %v", key, entry)
		}
	}
	if _, added := saved["freeze_run"]; added {
		t.Errorf("AC-3 saving an older record added freeze_run: %v", saved["freeze_run"])
	}
}

// AC-3, THREATS B1: SetGate keeps what it does not set, so a result left on the
// gate would stand for a fail recorded after it, or for a gate reopened by an
// earlier one. Neither keeps it.
func TestAC3AResultIsNotKeptByTheGateRecordedAgainOrReopened(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, time.November, 21, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		change func(*Record)
	}{
		{"AC-3 recorded fail", func(r *Record) { r.SetGate(GateImplementation, GateFail, "rework", at) }},
		{"AC-3 recorded pending", func(r *Record) { r.SetGate(GateImplementation, GatePending, "", at) }},
		{"AC-3 reopened by the plan", func(r *Record) { r.SetGate(GatePlan, GatePass, "a new plan", at) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := us134Gate(us134Saved(t, us134WithResults, tc.change))
			for _, key := range []string{"commands", "tree_hash"} {
				if v, kept := entry[key]; kept {
					t.Errorf("%s: the gate kept %s %v", tc.name, key, v)
				}
			}
		})
	}
}
