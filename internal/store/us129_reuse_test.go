package store

// US-129 AC-4, for a write under .sdlc/ and the backlog: what one process
// reads after it saved a gate record, or set a story's status, is what it
// saved; a record a caller changed in memory and did not save is read again as
// it is on disk; and a file saved through one spelling of the project's root
// is read as saved through another. Each passes at the base, which keeps no
// answer, and fails against a reuse layer that hands a caller the value it
// keeps, or keys what it keeps by the spelling it was asked with (US-129's
// design reviews: security F-3, red team F-3 and F-5).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
)

// us129At is the time every record of these rows carries.
var us129At = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)

// us129Project is a project in root with the default configuration and US-001
// in its backlog, ready, and a new record of US-001 saved.
func us129Project(t *testing.T, root string) *Store {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".sdlc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sdlc", "config.json"), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	s := New(&config.Project{Root: root, Config: cfg})
	backlog := `{"stories":[{"id":"US-001","title":"Invoices","status":"ready",` +
		`"acceptance_criteria":[{"id":"AC-1","text":"An invoice has a number."}]}]}` + "\n"
	if err := os.WriteFile(cfg.BacklogPath(root), []byte(backlog), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRecord(model.NewRecord("US-001", us129At)); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAC4ARecordReadAfterTheProcessSavedItIsTheRecordAsSaved(t *testing.T) {
	s := us129Project(t, t.TempDir())
	first, err := s.Record("US-001")
	if err != nil {
		t.Fatal(err)
	}
	if first.Gates[model.GateDoR].Status == model.GatePass {
		t.Fatal("a new record has dor passed")
	}
	first.SetGate(model.GateDoR, model.GatePass, "us129", us129At)
	if err := s.SaveRecord(first); err != nil {
		t.Fatal(err)
	}
	again, err := s.Record("US-001")
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Gates[model.GateDoR]; got.Status != model.GatePass || got.Note != "us129" {
		t.Errorf("AC-4 the record read after the process saved dor as passed has dor %+v", got)
	}
}

// The cli changes a record in memory and then can refuse before it saves
// (SetGate, then noteCommits): a later read that handed back the changed value
// would let the next save write the refused pass (security F-3).
func TestAC4ARecordChangedInMemoryAndNotSavedIsNotWhatTheNextReadReturns(t *testing.T) {
	s := us129Project(t, t.TempDir())
	changed, err := s.Record("US-001")
	if err != nil {
		t.Fatal(err)
	}
	changed.SetGate(model.GateDoR, model.GatePass, "refused before it was saved", us129At)
	again, err := s.Record("US-001")
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Gates[model.GateDoR]; got.Status == model.GatePass {
		t.Errorf("AC-4 the record read after a caller passed dor in memory, and saved nothing, has dor %+v", got)
	}
}

// As above, through a second store of the same invocation, and to the whole
// record: what is read is what the file holds (red team F-5(a)).
func TestAC4ARecordChangedInMemoryAndNotSavedIsReadAgainAsOnDisk(t *testing.T) {
	root := t.TempDir()
	s := us129Project(t, root)
	changed, err := s.Record("US-001")
	if err != nil {
		t.Fatal(err)
	}
	changed.SetGate(model.GateAnalysis, model.GatePass, "never saved", us129At)
	changed.Events = append(changed.Events, model.Event{At: us129At.Format(time.RFC3339), Type: model.EventAgentStart,
		Message: "never saved", AgentType: "sdlc:researcher", AgentID: "us129"})
	other, err := s.At(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, st := range map[string]*Store{"the same store": s, "a store of the same invocation": other} {
		again, err := st.Record("US-001")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json"))
		if err != nil {
			t.Fatal(err)
		}
		var onDisk model.Record
		if err := json.Unmarshal(raw, &onDisk); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(again)
		if err != nil {
			t.Fatal(err)
		}
		want, err := json.Marshal(&onDisk)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("AC-4 %s read the record changed in memory and not saved as\n%s\nwant it as on disk:\n%s", name, got, want)
		}
	}
}

// SetStoryStatus reads the backlog, edits it and writes it: the backlog read
// after it, in the same process, has the new status (red team F-3).
func TestAC4TheBacklogReadAfterTheProcessSetAStoryStatusHasTheNewStatus(t *testing.T) {
	s := us129Project(t, t.TempDir())
	before, err := s.Backlog()
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Stories) != 1 || before.Stories[0].Status != model.StatusReady {
		t.Fatalf("the backlog starts as %+v", before.Stories)
	}
	if err := s.SetStoryStatus("US-001", model.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	after, err := s.Backlog()
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Stories[0].Status; got != model.StatusInProgress {
		t.Errorf("AC-4 the backlog read after the process set US-001 in progress has it %s", got)
	}
	story, _, err := s.Story("US-001")
	if err != nil {
		t.Fatal(err)
	}
	if story.Status != model.StatusInProgress {
		t.Errorf("AC-4 the story read after the process set it in progress is %s", story.Status)
	}
}

// git resolves symbolic links, and a store opened where git said the checkout
// is spells the root otherwise than one opened where the person stood: on
// macOS the temporary directory is /var/..., which is /private/var/... A
// write through one spelling has to reach a read through the other (red team
// F-5(b), security F-4).
func TestAC4ARecordSavedThroughOneSpellingOfTheRootIsReadAsSavedThroughAnother(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved == root {
		t.Skipf("the temporary directory %s is reached through no symbolic link here, so the project has one spelling; macOS's /var is the case", root)
	}
	for _, way := range [][2]string{{root, resolved}, {resolved, root}} {
		s := us129Project(t, way[0])
		if _, err := s.Record("US-001"); err != nil {
			t.Fatal(err)
		}
		other, err := s.At(way[1])
		if err != nil {
			t.Fatal(err)
		}
		saved, err := other.Record("US-001")
		if err != nil {
			t.Fatal(err)
		}
		saved.SetGate(model.GateDoR, model.GatePass, "saved through "+way[1], us129At)
		if err := other.SaveRecord(saved); err != nil {
			t.Fatal(err)
		}
		again, err := s.Record("US-001")
		if err != nil {
			t.Fatal(err)
		}
		if got := again.Gates[model.GateDoR]; got.Status != model.GatePass {
			t.Errorf("AC-4 the record read through %s after the process saved dor passed through %s has dor %+v", way[0], way[1], got)
		}
	}
}
