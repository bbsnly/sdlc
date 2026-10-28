package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

func TestStopCountIsEmptyBeforeAnythingIsSaved(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if got := s.StopCount(); got != (StopCount{}) {
		t.Errorf("StopCount = %+v, want the zero value", got)
	}
}

func TestSaveStopCountThenStopCountRoundTrips(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	want := StopCount{Story: "A-1", Blocks: 2, Progress: "abc123"}
	if err := s.SaveStopCount(want); err != nil {
		t.Fatal(err)
	}
	if got := s.StopCount(); got != want {
		t.Errorf("StopCount = %+v, want %+v", got, want)
	}
}

// A count that will not read counts nothing: the next one written replaces
// it, rather than a bad file wedging every future stop.
func TestStopCountReadsAsZeroWhenTheFileIsMissingOrBroken(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path := filepath.Join(s.root, filepath.FromSlash(stopFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := s.StopCount(); got != (StopCount{}) {
		t.Errorf("StopCount on malformed JSON = %+v, want the zero value", got)
	}
}

// A count below zero was never written by a stop: the count only grows from
// there, so one far enough below zero never reached the limit and the story
// was never handed over.
func TestStopCountReadsAsZeroWhenBlocksIsNegative(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.SaveStopCount(StopCount{Story: "A-1", Blocks: -1}); err != nil {
		t.Fatal(err)
	}
	if got := s.StopCount(); got != (StopCount{}) {
		t.Errorf("StopCount with a negative count = %+v, want the zero value", got)
	}
}

// US-085. The count gains one value: when the first stop was let through
// because a subagent of the session was still running, which is where the
// thirty minutes the hook waits are measured from. The store keeps it as the
// text it was given, RFC 3339 when the hook wrote it, and reads nothing into
// it: whether it is the start of a wait is the hook's to say, and a value that
// is no time at all has to reach the hook as it is, because "no wait begun" and
// "a time nobody can read" are answered differently there.
//
// What these bind: the field StopCount.WaitingSince, a string, under the JSON
// key "waiting_since", left out of the file while it is empty.

const aWaitBegan = "2026-10-08T12:00:00Z"

func stopFileBody(t *testing.T, s *Store) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(stopFile)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func writeStopFile(t *testing.T, s *Store, body string) {
	t.Helper()
	path := filepath.Join(s.root, filepath.FromSlash(stopFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func stopFileKeys(t *testing.T, s *Store) []string {
	t.Helper()
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stopFileBody(t, s)), &keys); err != nil {
		t.Fatalf("%s is not a JSON object: %v", stopFile, err)
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

func TestAC5TheStopCountKeepsWhenTheWaitBeganUnderWaitingSince(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	want := StopCount{Story: "A-1", Blocks: 2, Progress: "abc123", WaitingSince: aWaitBegan}
	if err := s.SaveStopCount(want); err != nil {
		t.Fatal(err)
	}
	if got := s.StopCount(); got != want {
		t.Errorf("StopCount = %+v, want %+v", got, want)
	}

	// On disk it is one more key, holding the text as it was given.
	if keys := stopFileKeys(t, s); !slices.Equal(keys, []string{"blocks", "progress", "story", "waiting_since"}) {
		t.Errorf("%s holds the keys %v, want blocks, progress, story and waiting_since", stopFile, keys)
	}
	var onDisk struct {
		WaitingSince string `json:"waiting_since"`
	}
	if err := json.Unmarshal([]byte(stopFileBody(t, s)), &onDisk); err != nil || onDisk.WaitingSince != aWaitBegan {
		t.Errorf("waiting_since on disk is %q (%v), want %q", onDisk.WaitingSince, err, aWaitBegan)
	}
}

// A project that never sees a subagent keeps the file every release so far has
// written: three keys, and no fourth that says nothing.
func TestAC5AStopCountWithNoWaitBegunIsWrittenAsItAlwaysWas(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.SaveStopCount(StopCount{Story: "A-1", Blocks: 2, Progress: "abc123"}); err != nil {
		t.Fatal(err)
	}
	if keys := stopFileKeys(t, s); !slices.Equal(keys, []string{"blocks", "progress", "story"}) {
		t.Errorf("%s holds the keys %v, want blocks, progress and story and no other", stopFile, keys)
	}
}

// A count file an earlier release wrote has no time in it, and reads as a count
// whose wait has not begun: the count it holds is kept.
func TestAC5AStopCountFileWithoutTheTimeReadsAsNoWaitBegun(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	writeStopFile(t, s, `{"story":"A-1","blocks":2,"progress":"abc123"}`+"\n")
	if got, want := s.StopCount(), (StopCount{Story: "A-1", Blocks: 2, Progress: "abc123"}); got != want {
		t.Errorf("StopCount = %+v, want %+v", got, want)
	}
	if got := s.StopCount().WaitingSince; got != "" {
		t.Errorf("WaitingSince = %q from a file with no time in it, want none", got)
	}
}

// A time the store cannot make sense of is not the store's to drop or to mend.
// Read as "no wait begun", a stored value nobody can read would start a new
// thirty minutes; read as it is, the hook counts the stop. The count beside it
// is kept either way.
func TestAC5AStoredTimeIsHandedBackAsItWasWrittenWhateverItSays(t *testing.T) {
	t.Parallel()
	for _, since := range []string{"yesterday", " ", "2026-10-08", "9999-12-31T23:59:59Z", "0001-01-01T00:00:00Z", "1791460200"} {
		s := newStore(t)
		raw, err := json.Marshal(map[string]any{"story": "A-1", "blocks": 2, "progress": "abc123", "waiting_since": since})
		if err != nil {
			t.Fatal(err)
		}
		writeStopFile(t, s, string(raw))
		if got, want := s.StopCount(), (StopCount{Story: "A-1", Blocks: 2, Progress: "abc123", WaitingSince: since}); got != want {
			t.Errorf("StopCount with waiting_since %q = %+v, want %+v", since, got, want)
		}
	}
}

// A count below zero is no stop's, and the time written beside it goes with it:
// the whole count starts again.
func TestAC5ANegativeStopCountTakesItsTimeWithIt(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.SaveStopCount(StopCount{Story: "A-1", Blocks: -1, WaitingSince: aWaitBegan}); err != nil {
		t.Fatal(err)
	}
	if got := s.StopCount(); got != (StopCount{}) {
		t.Errorf("StopCount with a negative count = %+v, want the zero value", got)
	}
}

func TestAC5ClearingTheStopCountClearsWhenTheWaitBegan(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.SaveStopCount(StopCount{Story: "A-1", Blocks: 3, WaitingSince: aWaitBegan}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearStopCount(); err != nil {
		t.Fatal(err)
	}
	if got := s.StopCount(); got != (StopCount{}) {
		t.Errorf("StopCount after ClearStopCount = %+v, want the zero value", got)
	}
}

func TestClearStopCountRemovesTheFile(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.SaveStopCount(StopCount{Story: "A-1", Blocks: 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearStopCount(); err != nil {
		t.Fatal(err)
	}
	if got := s.StopCount(); got != (StopCount{}) {
		t.Errorf("StopCount after ClearStopCount = %+v", got)
	}
	// Clearing twice is not an error.
	if err := s.ClearStopCount(); err != nil {
		t.Errorf("ClearStopCount on a clear store: %v", err)
	}
}

func TestClearStopCountFailsWhenTheFileCannotBeRemoved(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
	if err := s.SaveStopCount(StopCount{Story: "A-1", Blocks: 1}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.root, filepath.FromSlash(stateDir))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := s.ClearStopCount(); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

// RecordProgress is the record's hash by content: a different answer means
// something was written to it since the last count was taken.
func TestRecordProgressChangesWhenTheRecordChanges(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	r, err := s.Record("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRecord(r); err != nil {
		t.Fatal(err)
	}
	before, err := s.RecordProgress("A-1")
	if err != nil {
		t.Fatal(err)
	}

	r.Append("gate", "something happened", s.Now())
	if err := s.SaveRecord(r); err != nil {
		t.Fatal(err)
	}
	after, err := s.RecordProgress("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Error("RecordProgress did not change when the record changed")
	}

	if _, err := s.RecordProgress("../../etc"); codeOf(t, err) != sdlcerr.UnsafeStoryID {
		t.Errorf("code = %v, want %s", err, sdlcerr.UnsafeStoryID)
	}
}

// Escalate hands the story to a person: it records the question against the
// work as it stands, moves the story to awaiting_human, and ends the
// iteration if it was the story running.
func TestEscalateRecordsTheQuestionEndsTheIterationAndMarksTheStoryWaiting(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress"}]}`)
	commitAll(t, s.root, "base")
	if err := s.SetActive("A-1"); err != nil {
		t.Fatal(err)
	}

	tree, err := s.Escalate(t.Context(), "A-1", "risk", "needs a person's call")
	if err != nil {
		t.Fatal(err)
	}
	if tree == "" {
		t.Error("Escalate returned no tree hash")
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

	r, err := s.Record("A-1")
	if err != nil {
		t.Fatal(err)
	}
	pending, ok := r.PendingEscalation()
	if !ok || pending.Message != "needs a person's call" || pending.TreeHash != tree {
		t.Errorf("PendingEscalation = %+v, %v, want the message and tree hash recorded", pending, ok)
	}
}

// Escalating a story other than the one under way leaves the iteration alone.
func TestEscalatingAnotherStoryLeavesTheIterationRunning(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[
		{"id":"A-1","title":"One","status":"in_progress"},
		{"id":"B-2","title":"Two","status":"in_progress"}]}`)
	commitAll(t, s.root, "base")
	if err := s.SetActive("A-1"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Escalate(t.Context(), "B-2", "risk", "x"); err != nil {
		t.Fatal(err)
	}
	if id, _ := s.Active(); id != "A-1" {
		t.Errorf("Active = %q, want A-1 still running", id)
	}
}

func TestEscalateFailsWhenTheStoryIsNotInTheBacklog(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[]}`)
	commitAll(t, s.root, "base")
	if _, err := s.Escalate(t.Context(), "A-1", "risk", "x"); codeOf(t, err) != sdlcerr.StoryNotFound {
		t.Errorf("code = %v, want %s", err, sdlcerr.StoryNotFound)
	}
}

func TestEscalateFailsWhenTheTreeCannotBeMeasured(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	// No git repository: ReviewSubject falls through to gitx.TreeHash, which
	// needs one.
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress"}]}`)
	if _, err := s.Escalate(t.Context(), "A-1", "risk", "x"); codeOf(t, err) != sdlcerr.RepositoryUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.RepositoryUnreadable)
	}
}

func TestEscalateFailsWhenActiveStateCannotBeRead(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress"}]}`)
	commitAll(t, s.root, "base")
	path := filepath.Join(s.root, filepath.FromSlash(activeFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("../../etc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Escalate(t.Context(), "A-1", "risk", "x"); codeOf(t, err) != sdlcerr.UnsafeStoryID {
		t.Errorf("code = %v, want %s", err, sdlcerr.UnsafeStoryID)
	}
}

func TestEscalateFailsWhenTheRecordCannotBeRead(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress"}]}`)
	commitAll(t, s.root, "base")
	dir, _ := s.StoryDir("A-1")
	if err := os.MkdirAll(filepath.Join(dir, recordFile), 0o755); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	if _, err := s.Escalate(t.Context(), "A-1", "risk", "x"); codeOf(t, err) != sdlcerr.StateUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnreadable)
	}
}

func TestEscalateFailsWhenTheRecordCannotBeSaved(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress"}]}`)
	commitAll(t, s.root, "base")
	dir, err := s.StoryDir("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if _, err := s.Escalate(t.Context(), "A-1", "risk", "x"); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

// SetStoryStatus is the step after the record is saved, and can still fail on
// its own: the backlog is a different file in a different directory.
func TestEscalateFailsWhenTheStoryStatusCannotBeSet(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress"}]}`)
	commitAll(t, s.root, "base")
	// The story's own directory is created and writable ahead of time, so only
	// the backlog's directory -- the repository root -- is what is denied.
	if err := s.SaveRecord(model.NewRecord("A-1", s.Now())); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.root, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.root, 0o755) })

	if _, err := s.Escalate(t.Context(), "A-1", "risk", "x"); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

// The iteration is ended last, and can still fail: it removes files of its
// own, under .sdlc/state, a directory the story's record does not touch.
func TestEscalateFailsWhenTheIterationCannotBeEnded(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress"}]}`)
	commitAll(t, s.root, "base")
	if err := s.SetActive("A-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.bindSession("session-a"); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(s.root, filepath.FromSlash(stateDir))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if _, err := s.Escalate(t.Context(), "A-1", "risk", "x"); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}
