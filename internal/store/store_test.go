package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

var fixedTime = time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC)

func newStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("GIT_WORK_TREE", "")
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	p := &config.Project{Root: root, Config: config.Default()}
	return New(p).WithClock(func() time.Time { return fixedTime })
}

func writeBacklog(t *testing.T, s *Store, body string) {
	t.Helper()
	if err := os.WriteFile(s.cfg.BacklogPath(s.root), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func codeOf(t *testing.T, err error) sdlcerr.Code {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("error is not an *sdlcerr.Error: %v", err)
	}
	return e.Code
}

// A story id becomes a directory name, and ids come from a file an assistant
// may have written.
func TestCheckIDRejectsAnythingThatCouldEscapeTheStoriesDirectory(t *testing.T) {
	for _, id := range []string{
		"", ".", "..", "../escape", "a/b", `a\b`, "/absolute", "C:\\windows",
		"has space", "tab\there", "null\x00byte", "-leading-dash",
		strings.Repeat("x", 65),
	} {
		if err := CheckID(id); err == nil {
			t.Errorf("CheckID(%q) accepted an unsafe id", id)
		} else if got := codeOf(t, err); got != sdlcerr.UnsafeStoryID {
			t.Errorf("CheckID(%q) code = %s, want %s", id, got, sdlcerr.UnsafeStoryID)
		}
	}
}

func TestCheckIDAcceptsTheIdsPeopleActuallyUse(t *testing.T) {
	for _, id := range []string{"US-001", "AUTH-3", "PROJ-12.4", "a", "billing_2", "9LIVES-1"} {
		if err := CheckID(id); err != nil {
			t.Errorf("CheckID(%q) rejected a reasonable id: %v", id, err)
		}
	}
}

func TestStoryDirRefusesAnUnsafeID(t *testing.T) {
	s := newStore(t)
	if _, err := s.StoryDir("../../etc"); err == nil {
		t.Fatal("StoryDir built a path from an id that escapes")
	}
}

func TestBacklogMissing(t *testing.T) {
	s := newStore(t)
	_, err := s.Backlog()
	if err == nil {
		t.Fatal("Backlog succeeded with no file")
	}
	if got := codeOf(t, err); got != sdlcerr.BacklogMissing {
		t.Errorf("code = %s, want %s", got, sdlcerr.BacklogMissing)
	}
	if !strings.Contains(err.Error(), "no backlog") {
		t.Errorf("error = %v", err)
	}
}

func TestBacklogRejectsBrokenJSON(t *testing.T) {
	s := newStore(t)
	writeBacklog(t, s, `{"stories": [`)
	if _, err := s.Backlog(); codeOf(t, err) != sdlcerr.BacklogUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.BacklogUnreadable)
	}
}

func TestBacklogNamesTheStoryThatIsMissingAField(t *testing.T) {
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"Fine","status":"ready"},{"id":"B-2","status":"ready"}]}`)

	_, err := s.Backlog()
	if err == nil {
		t.Fatal("Backlog accepted a story with no title")
	}
	if !strings.Contains(err.Error(), "B-2") {
		t.Errorf("error does not name the story that broke it: %v", err)
	}
}

func TestBacklogRejectsAStoryWhoseIDWouldEscape(t *testing.T) {
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[{"id":"../../oops","title":"Sneaky","status":"ready"}]}`)

	_, err := s.Backlog()
	if err == nil {
		t.Fatal("Backlog accepted a traversing story id")
	}
	if got := codeOf(t, err); got != sdlcerr.UnsafeStoryID {
		t.Errorf("code = %s, want %s", got, sdlcerr.UnsafeStoryID)
	}
}

func TestStoryNotFoundListsWhatIsThere(t *testing.T) {
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"ready"},{"id":"B-2","title":"Two","status":"ready"}]}`)

	_, _, err := s.Story("C-3")
	if err == nil {
		t.Fatal("Story found one that is not there")
	}
	var e *sdlcerr.Error
	errors.As(err, &e)
	if !strings.Contains(e.Why, "A-1") || !strings.Contains(e.Why, "B-2") {
		t.Errorf("why = %q, want the available ids", e.Why)
	}
}

func TestSetStoryStatusSurvivesARoundTripAndKeepsUnknownFields(t *testing.T) {
	s := newStore(t)
	writeBacklog(t, s, `{"_schema":"x.json","stories":[
	  {"id":"A-1","title":"One","status":"ready","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN x, y."}]}]}`)

	if err := s.SetStoryStatus("A-1", model.StatusInProgress); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(s.cfg.BacklogPath(s.root))
	if err != nil {
		t.Fatal(err)
	}
	var back model.Backlog
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	got, _ := back.Find("A-1")
	if got.Status != model.StatusInProgress {
		t.Errorf("status = %q", got.Status)
	}
	if got.Updated != "2026-09-10T08:30:00Z" {
		t.Errorf("updated = %q, want the store's clock", got.Updated)
	}
	if back.Schema != "x.json" {
		t.Error("_schema was dropped from the user's file")
	}
	if len(got.AcceptanceCriteria) != 1 {
		t.Error("acceptance criteria were dropped from the user's file")
	}
	if !strings.HasSuffix(string(raw), "}\n") {
		t.Error("the file does not end in a newline; it lives in git and a diff will show it")
	}
}

func TestActiveIsEmptyBeforeAnIterationStarts(t *testing.T) {
	s := newStore(t)
	id, err := s.Active()
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Errorf("Active = %q, want empty", id)
	}
}

func TestSetActiveThenClearActive(t *testing.T) {
	s := newStore(t)
	if err := s.SetActive("A-1"); err != nil {
		t.Fatal(err)
	}
	if id, _ := s.Active(); id != "A-1" {
		t.Errorf("Active = %q", id)
	}
	if err := s.ClearActive(); err != nil {
		t.Fatal(err)
	}
	if id, _ := s.Active(); id != "" {
		t.Errorf("Active = %q after clearing", id)
	}
	// Clearing twice is not an error: the loop ends the same way whether or not
	// it had started.
	if err := s.ClearActive(); err != nil {
		t.Errorf("ClearActive on a clear store: %v", err)
	}
}

func TestActiveRefusesATamperedStateFile(t *testing.T) {
	s := newStore(t)
	path := filepath.Join(s.root, filepath.FromSlash(activeFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("../../../etc\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Active(); err == nil {
		t.Fatal("Active accepted a traversing id from the state file")
	}
}

func TestRecordStartsFreshAndThenPersists(t *testing.T) {
	s := newStore(t)

	r, err := s.Record("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Created != "2026-09-10T08:30:00Z" || len(r.Gates) != 0 {
		t.Errorf("fresh record = %+v", r)
	}

	r.SetGate(model.GateAnalysis, model.GatePass, "threats assessed", s.Now())
	r.Append("gate", "analysis passed", s.Now())
	if err := s.SaveRecord(r); err != nil {
		t.Fatal(err)
	}

	again, err := s.Record("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if !again.Pass(model.GateAnalysis) {
		t.Error("the gate did not survive the round trip")
	}
	if len(again.Events) != 1 {
		t.Errorf("events = %+v", again.Events)
	}
}

func TestRecordReportsACorruptFileAsABugRatherThanAnEdit(t *testing.T) {
	s := newStore(t)
	dir, _ := s.StoryDir("A-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, recordFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := s.Record("A-1")
	if got := codeOf(t, err); got != sdlcerr.StateUnreadable {
		t.Errorf("code = %s, want %s", got, sdlcerr.StateUnreadable)
	}
}

// An interrupted write must leave the previous file intact, never a truncated
// one: these files are the loop's memory.
func TestWriteLeavesNoTemporaryFilesBehind(t *testing.T) {
	s := newStore(t)
	if err := s.SetActive("A-1"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(s.root, filepath.FromSlash(stateDir)))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("a temporary file was left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("state dir holds %d entries, want just the active file", len(entries))
	}
}

func TestWriteReplacesTheOldContentCompletely(t *testing.T) {
	s := newStore(t)
	if err := s.SetActive("LONGER-STORY-ID"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetActive("A-1"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(activeFile)))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "A-1\n" {
		t.Errorf("active file = %q, want the new value alone", raw)
	}
}

func TestWriteFailureNamesThePathRelativeToTheRepository(t *testing.T) {
	s := newStore(t)
	// A file where a directory needs to be: the write cannot create its parent.
	blocked := filepath.Join(s.root, config.Dir)
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := s.SetActive("A-1")
	if err == nil {
		t.Fatal("SetActive succeeded with .sdlc blocked by a file")
	}
	if got := codeOf(t, err); got != sdlcerr.StateUnwritable {
		t.Errorf("code = %s, want %s", got, sdlcerr.StateUnwritable)
	}
	if !strings.Contains(err.Error(), ".sdlc/state/active") {
		t.Errorf("error does not name the path: %v", err)
	}
}

// A gate that ran twice has one record, not two: the second document replaces
// the first, in place, where the next gate is already looking for it.
func TestWritingADocumentTwiceLeavesTheSecondOne(t *testing.T) {
	s := newStore(t)
	analysis, _ := model.FindArtifact("analysis")

	if _, err := s.WriteArtifact("A-1", analysis, []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	path, err := s.WriteArtifact("A-1", analysis, []byte("second\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := ".sdlc/stories/A-1/ANALYSIS.md"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if path != ArtifactPath("A-1", analysis) {
		t.Errorf("WriteArtifact said %q but ArtifactPath says %q", path, ArtifactPath("A-1", analysis))
	}

	body, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "second\n" {
		t.Errorf("on disk: %q", body)
	}
}

// The story id reaches this from a file an assistant may have written, and it
// becomes a directory name.
func TestADocumentCannotEscapeTheStoriesDirectory(t *testing.T) {
	s := newStore(t)
	analysis, _ := model.FindArtifact("analysis")

	if _, err := s.WriteArtifact("../../etc", analysis, []byte("x\n")); err == nil {
		t.Fatal("a document was written outside the stories directory")
	} else if got := codeOf(t, err); got != sdlcerr.UnsafeStoryID {
		t.Errorf("code = %s, want %s", got, sdlcerr.UnsafeStoryID)
	}
}
