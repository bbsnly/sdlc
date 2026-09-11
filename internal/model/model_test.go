package model

import (
	"encoding/json"
	"strings"
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

// ---------------------------------------------------------------- artifacts

// The registry is what the CLI, the policy and the skills all read. A row that
// names a gate that does not exist, or two rows claiming the same file, would
// be found by a user rather than by a test.
func TestTheArtifactRegistryIsConsistent(t *testing.T) {
	gates := map[Gate]bool{}
	for _, g := range Gates {
		gates[g] = true
	}
	names, files := map[string]bool{}, map[string]bool{}

	for _, a := range Artifacts {
		switch {
		case a.Name == "" || a.File == "" || a.Role == "":
			t.Errorf("%+v has an empty field", a)
		case !gates[a.Gate]:
			t.Errorf("%s belongs to %q, which is not a gate", a.Name, a.Gate)
		case names[a.Name]:
			t.Errorf("%s is registered twice", a.Name)
		case files[strings.ToLower(a.File)]:
			t.Errorf("two documents both claim %s", a.File)
		}
		names[a.Name], files[strings.ToLower(a.File)] = true, true

		if got, ok := FindArtifact(a.Name); !ok || got != a {
			t.Errorf("FindArtifact(%q) = %+v, %v", a.Name, got, ok)
		}
		if got, ok := ArtifactByFile(a.File); !ok || got != a {
			t.Errorf("ArtifactByFile(%q) = %+v, %v", a.File, got, ok)
		}
		if !strings.Contains(ArtifactNames(), a.Name) {
			t.Errorf("ArtifactNames() does not mention %s: %q", a.Name, ArtifactNames())
		}
	}
}

// The name is what a person types, so spacing and case are theirs to get wrong.
func TestAnArtifactNameIsForgivingButNotVague(t *testing.T) {
	for _, in := range []string{"analysis", "ANALYSIS", "  Analysis  "} {
		if _, ok := FindArtifact(in); !ok {
			t.Errorf("FindArtifact(%q) found nothing", in)
		}
	}
	for _, in := range []string{"", "analysi", "analysis.md", "postmortem"} {
		if got, ok := FindArtifact(in); ok {
			t.Errorf("FindArtifact(%q) = %+v, want nothing", in, got)
		}
	}
}

// macOS and Windows treat analysis.md and ANALYSIS.md as the same file, so a
// rule that matched only one spelling would stop enforcing on two of the three
// platforms this ships to.
func TestAFileNameMatchesWhateverCaseItArrivesIn(t *testing.T) {
	for _, in := range []string{"ANALYSIS.md", "analysis.md", "Analysis.MD"} {
		if _, ok := ArtifactByFile(in); !ok {
			t.Errorf("ArtifactByFile(%q) found nothing", in)
		}
	}
	if _, ok := ArtifactByFile("notes.md"); ok {
		t.Error("notes.md was taken for a gate's document")
	}
}

// Where a resumed loop picks up. The skill reads this from `sdlc status`
// rather than working the gate order out for itself, so it has to be the same
// answer the gate ordering rule gives.
func TestNextGateIsTheFirstOneNotPassed(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	r := NewRecord("A-1", at)

	if got, ok := r.NextGate(); !ok || got != Gates[0] {
		t.Errorf("a fresh record resumes at %q, %v; want %q", got, ok, Gates[0])
	}

	for i, g := range Gates {
		r.SetGate(g, GatePass, "", at)
		got, ok := r.NextGate()
		if i == len(Gates)-1 {
			if ok {
				t.Errorf("every gate passed, but NextGate returned %q", got)
			}
			continue
		}
		if !ok || got != Gates[i+1] {
			t.Errorf("after %q passed, next = %q, %v; want %q", g, got, ok, Gates[i+1])
		}
	}
}

// A failed gate is where the loop resumes, not something it has moved past.
func TestAFailedGateIsWhereTheLoopResumes(t *testing.T) {
	at := time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)
	r := NewRecord("A-1", at)
	r.SetGate(Gates[0], GatePass, "", at)
	r.SetGate(Gates[1], GateFail, "open questions", at)

	if got, ok := r.NextGate(); !ok || got != Gates[1] {
		t.Errorf("next = %q, %v; want the failed gate %q", got, ok, Gates[1])
	}
}
