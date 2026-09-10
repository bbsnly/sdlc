package model

import (
	"encoding/json"
	"testing"
	"time"
)

func p(n int) *int { return &n }

func story(id string, status Status, priority *int, deps ...string) Story {
	return Story{ID: id, Title: id, Status: status, Priority: priority, DependsOn: deps}
}

func TestNextPrefersAStoryAlreadyUnderWay(t *testing.T) {
	b := &Backlog{Stories: []Story{
		story("A-1", StatusReady, p(1)),
		story("B-2", StatusInProgress, p(9)),
	}}

	got, ok := b.Next()
	if !ok {
		t.Fatal("Next found nothing")
	}
	if got.Story.ID != "B-2" {
		t.Errorf("Next = %s, want the in-progress B-2", got.Story.ID)
	}
	if !got.Resume {
		t.Error("Resume = false, want true for a story already under way")
	}
}

func TestNextResumesAStoryWaitingOnAHuman(t *testing.T) {
	b := &Backlog{Stories: []Story{
		story("A-1", StatusReady, p(1)),
		story("B-2", StatusAwaitingHuman, p(9)),
	}}

	got, _ := b.Next()
	if got.Story.ID != "B-2" || !got.Resume {
		t.Errorf("Next = %+v, want B-2 as a resume", got.Story)
	}
}

// A priority of 0 means fix-forward, which is the front of the queue. An absent
// priority means nobody has said, which is the back of it.
func TestNextTreatsZeroAsUrgentAndAbsentAsLast(t *testing.T) {
	b := &Backlog{Stories: []Story{
		story("A-1", StatusReady, nil),
		story("B-2", StatusReady, p(1)),
		story("C-3", StatusReady, p(0)),
	}}

	got, _ := b.Next()
	if got.Story.ID != "C-3" {
		t.Errorf("Next = %s, want the priority-0 C-3", got.Story.ID)
	}

	b.Stories[2].Status = StatusDone
	got, _ = b.Next()
	if got.Story.ID != "B-2" {
		t.Errorf("Next = %s, want B-2 ahead of the unprioritised A-1", got.Story.ID)
	}
}

func TestNextBreaksTiesByIDSoTheChoiceIsStable(t *testing.T) {
	b := &Backlog{Stories: []Story{
		story("Z-9", StatusReady, p(2)),
		story("A-1", StatusReady, p(2)),
	}}

	for range 5 {
		got, _ := b.Next()
		if got.Story.ID != "A-1" {
			t.Fatalf("Next = %s, want A-1 every time", got.Story.ID)
		}
	}
}

func TestNextSkipsAStoryWithAnUnfinishedDependency(t *testing.T) {
	b := &Backlog{Stories: []Story{
		story("A-1", StatusReady, p(1), "B-2"),
		story("B-2", StatusTodo, p(5)),
	}}

	got, _ := b.Next()
	if got.Story.ID != "B-2" {
		t.Errorf("Next = %s, want B-2: A-1 depends on it", got.Story.ID)
	}

	b.Stories[1].Status = StatusDone
	got, _ = b.Next()
	if got.Story.ID != "A-1" {
		t.Errorf("Next = %s, want A-1 once its dependency is done", got.Story.ID)
	}
}

func TestNextFindsNothingWhenEverythingIsHeldBack(t *testing.T) {
	b := &Backlog{Stories: []Story{
		story("A-1", StatusDone, p(1)),
		story("B-2", StatusBlocked, p(2)),
		story("C-3", StatusReady, p(3), "B-2"),
		story("D-4", StatusDropped, p(4)),
	}}

	if _, ok := b.Next(); ok {
		t.Error("Next found a runnable story among done, blocked, dropped and dependent ones")
	}
}

func TestBlockedByNamesTheUnfinishedDependencies(t *testing.T) {
	b := &Backlog{Stories: []Story{
		story("A-1", StatusReady, p(1), "B-2", "C-3"),
		story("B-2", StatusDone, p(2)),
		story("C-3", StatusTodo, p(3)),
	}}

	got := b.BlockedBy(&b.Stories[0])
	if len(got) != 1 || got[0] != "C-3" {
		t.Errorf("BlockedBy = %v, want only the unfinished C-3", got)
	}
}

func TestFindAndIDs(t *testing.T) {
	b := &Backlog{Stories: []Story{story("A-1", StatusReady, p(1)), story("B-2", StatusReady, p(2))}}

	if s, ok := b.Find("B-2"); !ok || s.ID != "B-2" {
		t.Errorf("Find(B-2) = %v, %v", s, ok)
	}
	if _, ok := b.Find("nope"); ok {
		t.Error("Find returned a story that is not there")
	}
	if got := b.IDs(); len(got) != 2 || got[0] != "A-1" {
		t.Errorf("IDs = %v", got)
	}
}

// Find returns a pointer into the backlog so that a caller can change a status
// and save. A copy would drop the change silently.
func TestFindReturnsAPointerIntoTheBacklog(t *testing.T) {
	b := &Backlog{Stories: []Story{story("A-1", StatusReady, p(1))}}
	s, _ := b.Find("A-1")
	s.Status = StatusDone
	if b.Stories[0].Status != StatusDone {
		t.Error("changing the returned story did not change the backlog")
	}
}

func TestStatusAndGateValidation(t *testing.T) {
	if !Status("ready").Valid() || Status("nearly").Valid() {
		t.Error("Status.Valid is wrong")
	}
	if !Gate("analysis").Valid() || Gate("vibes").Valid() {
		t.Error("Gate.Valid is wrong")
	}
	if !GateStatus("pass").Valid() || GateStatus("probably").Valid() {
		t.Error("GateStatus.Valid is wrong")
	}
	if len(Gates) != 11 {
		t.Errorf("Gates has %d entries; the nine gates record eleven results", len(Gates))
	}
}

func TestRecordRecordsGatesAndHistory(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	r := NewRecord("A-1", at)

	if r.Pass(GateAnalysis) {
		t.Error("a fresh record reports a gate as passed")
	}
	r.SetGate(GateAnalysis, GatePass, "threats assessed", at)
	if !r.Pass(GateAnalysis) {
		t.Error("SetGate(pass) did not take")
	}
	r.SetGate(GateAnalysis, GateFail, "missed a trust boundary", at)
	if r.Pass(GateAnalysis) {
		t.Error("a gate cannot stay passed after it fails")
	}
	if got := r.Gates[GateAnalysis].Note; got != "missed a trust boundary" {
		t.Errorf("note = %q", got)
	}

	r.Append("loop_start", "iteration started", at)
	if len(r.Events) != 1 || r.Events[0].At != "2026-09-10T08:30:00Z" {
		t.Errorf("events = %+v", r.Events)
	}
}

// A record written by the shell kit must still load, and one written here must
// still be readable by it.
func TestRecordRoundTripsTheOnDiskShape(t *testing.T) {
	const onDisk = `{"story":"A-1","created":"2026-09-10T08:30:00Z",
	  "gates":{"analysis":{"status":"pass","at":"2026-09-10T08:31:00Z","note":"ok"}},
	  "events":[{"at":"2026-09-10T08:30:00Z","type":"loop_start","message":"iteration started"}],
	  "escalations":[],"approvals":[],
	  "metrics":{"compactions":0,"review_rounds":0}}`

	var r Record
	if err := json.Unmarshal([]byte(onDisk), &r); err != nil {
		t.Fatal(err)
	}
	if !r.Pass(GateAnalysis) {
		t.Error("a pass written by the shell kit did not read back as a pass")
	}
	if r.Metrics["compactions"] != float64(0) {
		t.Errorf("metrics = %v", r.Metrics)
	}

	out, err := json.Marshal(&r)
	if err != nil {
		t.Fatal(err)
	}
	var again Record
	if err := json.Unmarshal(out, &again); err != nil {
		t.Fatal(err)
	}
	if again.Gates[GateAnalysis].At != "2026-09-10T08:31:00Z" {
		t.Errorf("round trip lost the gate time: %s", out)
	}
}

func TestTimestampIsUTCToTheSecond(t *testing.T) {
	zone := time.FixedZone("UTC+7", 7*60*60)
	got := Timestamp(time.Date(2026, 9, 10, 15, 4, 5, 999, zone))
	if got != "2026-09-10T08:04:05Z" {
		t.Errorf("Timestamp = %q, want the UTC instant to the second", got)
	}
}

// The backlog on disk uses acceptance criteria as objects, not strings.
func TestBacklogParsesTheTemplateShape(t *testing.T) {
	const onDisk = `{"_schema":".sdlc/templates/story.schema.json","stories":[
	  {"id":"US-001","title":"Example","priority":1,"status":"ready","risk_tier":"low",
	   "depends_on":[],
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN x, the system shall y."}],
	   "non_goals":["persistence"]}]}`

	var b Backlog
	if err := json.Unmarshal([]byte(onDisk), &b); err != nil {
		t.Fatal(err)
	}
	s, ok := b.Find("US-001")
	if !ok {
		t.Fatal("US-001 did not parse")
	}
	if len(s.AcceptanceCriteria) != 1 || s.AcceptanceCriteria[0].ID != "AC-1" {
		t.Errorf("acceptance criteria = %+v", s.AcceptanceCriteria)
	}
	if s.Priority == nil || *s.Priority != 1 {
		t.Errorf("priority = %v", s.Priority)
	}
	if b.Schema == "" {
		t.Error("_schema was dropped; saving would strip it from the user's file")
	}
}
