package store

// US-008: Escalate asked git for the tree before it wrote anything, and so
// wrote nothing when git refused. The steps after its own write could still
// fail -- the backlog, the iteration -- and each left the story half handed
// over: a question on the record that the backlog did not show, or a story
// the backlog called awaiting_human with its iteration still running. The
// commands that hand a story over at a limit (internal/cli has them) can only
// be all-or-nothing if this is.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// hoStopped is one step of Escalate after its own write that can be made to
// fail from outside, by the directory whose write permission is taken away.
var hoStopped = []struct {
	name string
	dir  func(s *Store) string
}{
	{"the backlog cannot be written", func(s *Store) string { return s.root }},
	{"the iteration cannot be ended", func(s *Store) string { return filepath.Join(s.root, filepath.FromSlash(stateDir)) }},
}

// hoUnderWay is a story being worked on, with one gate failure on its record.
func hoUnderWay(t *testing.T) (*Store, string) {
	t.Helper()
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress"}]}`)
	commitAll(t, s.root, "base")
	record := model.NewRecord("A-1", s.Now())
	record.SetGate(model.GateDoR, model.GateFail, "AC-2 is not testable", s.Now())
	record.Append(model.EventGate, model.GateEvent(model.GateDoR, model.GateFail), s.Now())
	if err := s.SaveRecord(record); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActive("A-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.bindSession("session-a"); err != nil {
		t.Fatal(err)
	}
	dir, err := s.StoryDir("A-1")
	if err != nil {
		t.Fatal(err)
	}
	return s, filepath.Join(dir, recordFile)
}

func TestAC1AnEscalationThatCannotBeCompletedLeavesTheRecordTheBacklogAndTheIterationAsTheyWere(t *testing.T) {
	t.Parallel()
	for _, stopped := range hoStopped {
		t.Run(stopped.name, func(t *testing.T) {
			skipIfPermissionsDoNotBind(t)
			s, recordPath := hoUnderWay(t)
			before, err := os.ReadFile(recordPath)
			if err != nil {
				t.Fatal(err)
			}
			dir := stopped.dir(s)
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

			// Twice, as a command run again is: a second refusal adds nothing
			// to what the first left.
			for attempt := 1; attempt <= 2; attempt++ {
				if _, err := s.Escalate(t.Context(), "A-1", "gate_failing", "dor has failed 2 times"); codeOf(t, err) != sdlcerr.StateUnwritable {
					t.Fatalf("attempt %d: code = %v, want %s", attempt, err, sdlcerr.StateUnwritable)
				}
				after, err := os.ReadFile(recordPath)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(after, before) {
					t.Errorf("attempt %d: the refused escalation changed the record.\nbefore:\n%s\nafter:\n%s", attempt, before, after)
				}
				story, _, err := s.Story("A-1")
				if err != nil {
					t.Fatal(err)
				}
				if story.Status != model.StatusInProgress {
					t.Errorf("attempt %d: status = %q, want %s: nobody was asked anything", attempt, story.Status, model.StatusInProgress)
				}
				if id, err := s.Active(); err != nil || id != "A-1" {
					t.Errorf("attempt %d: Active = %q, %v, want the iteration still on A-1", attempt, id, err)
				}
				if session, ok := s.WorkingSession(); !ok || session != "session-a" {
					t.Errorf("attempt %d: WorkingSession = %q, %v, want the session still working the story", attempt, session, ok)
				}
			}
		})
	}
}

func TestAC2AnEscalationRefusedAndThenMadeAgainAsksItsQuestionOnce(t *testing.T) {
	t.Parallel()
	for _, stopped := range hoStopped {
		t.Run(stopped.name, func(t *testing.T) {
			skipIfPermissionsDoNotBind(t)
			s, _ := hoUnderWay(t)
			dir := stopped.dir(s)
			if err := os.Chmod(dir, 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
			for range 2 {
				if _, err := s.Escalate(t.Context(), "A-1", "gate_failing", "dor has failed 2 times"); err == nil {
					t.Fatal("the story was handed over with a step of the hand-over denied")
				}
			}
			if err := os.Chmod(dir, 0o755); err != nil {
				t.Fatal(err)
			}

			tree, err := s.Escalate(t.Context(), "A-1", "gate_failing", "dor has failed 2 times")
			if err != nil {
				t.Fatal(err)
			}
			record, err := s.Record("A-1")
			if err != nil {
				t.Fatal(err)
			}
			if n := len(record.Escalations); n != 1 {
				t.Fatalf("the record holds %d escalations, want the 1 that was made: %+v", n, record.Escalations)
			}
			if pending, ok := record.PendingEscalation(); !ok || pending.Type != "gate_failing" || pending.TreeHash != tree {
				t.Errorf("PendingEscalation = %+v, %v, want gate_failing against the tree returned", pending, ok)
			}
			if n := record.Count("escalate", "gate_failing: dor has failed 2 times"); n != 1 {
				t.Errorf("the record's history holds %d escalate events, want 1", n)
			}
			if n := record.SinceDecision(model.EventGate, model.GateEvent(model.GateDoR, model.GateFail)); n != 1 {
				t.Errorf("the record counts %d dor failures, want the 1 it held before", n)
			}
			story, _, err := s.Story("A-1")
			if err != nil {
				t.Fatal(err)
			}
			if story.Status != model.StatusAwaitingHuman {
				t.Errorf("status = %q, want %s", story.Status, model.StatusAwaitingHuman)
			}
			if id, _ := s.Active(); id != "" {
				t.Errorf("Active = %q, want the iteration ended", id)
			}
		})
	}
}
