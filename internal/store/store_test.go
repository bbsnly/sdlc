package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
		// Windows drops the trailing dot, so this is A-1's directory.
		"A-1.", "A-1.-.",
	} {
		if err := CheckID(id); err == nil {
			t.Errorf("CheckID(%q) accepted an unsafe id", id)
		} else if got := codeOf(t, err); got != sdlcerr.UnsafeStoryID {
			t.Errorf("CheckID(%q) code = %s, want %s", id, got, sdlcerr.UnsafeStoryID)
		}
	}
}

func TestCheckIDAcceptsTheIdsPeopleActuallyUse(t *testing.T) {
	for _, id := range []string{"US-001", "AUTH-3", "PROJ-12.4", "a", "billing_2", "9LIVES-1", strings.Repeat("x", 64)} {
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

// A status the loop does not know was read as no status: the story was never
// picked, never listed as waiting, and nothing said why.
func TestBacklogRefusesAStatusTheLoopDoesNotKnow(t *testing.T) {
	for _, story := range []string{
		`{"id":"B-2","title":"Two","status":"Ready"}`,
		`{"id":"B-2","title":"Two"}`,
	} {
		s := newStore(t)
		writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"Fine","status":"ready"},`+story+`]}`)
		_, err := s.Backlog()
		if codeOf(t, err) != sdlcerr.BacklogUnreadable {
			t.Errorf("%s: code = %v, want %s", story, err, sdlcerr.BacklogUnreadable)
			continue
		}
		var e *sdlcerr.Error
		errors.As(err, &e)
		for _, want := range []string{"B-2", "ready", "dropped"} {
			if !strings.Contains(e.What+" "+e.Why, want) {
				t.Errorf("%s: the refusal does not name %q: %s / %s", story, want, e.What, e.Why)
			}
		}
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

// A second story with an id was never read: every lookup found the first, so
// starting the backlog moved the other one, and doctor called the backlog fine.
func TestBacklogRefusesTwoStoriesWithOneID(t *testing.T) {
	for _, second := range []string{"A-1", "a-1"} {
		s := newStore(t)
		writeBacklog(t, s, `{"stories":[{"id":"B-0","title":"Other","status":"ready"},`+
			`{"id":"A-1","title":"First copy","status":"done"},`+
			`{"id":"`+second+`","title":"Second copy","status":"ready"}]}`)

		_, err := s.Backlog()
		if err == nil {
			t.Errorf("the backlog was read with A-1 and %s in it", second)
			continue
		}
		if got := codeOf(t, err); got != sdlcerr.BacklogUnreadable {
			t.Errorf("%s: code = %s, want %s", second, got, sdlcerr.BacklogUnreadable)
		}
		var e *sdlcerr.Error
		errors.As(err, &e)
		if !strings.Contains(e.What, second) || !strings.Contains(e.Why, "2 and 3") {
			t.Errorf("%s: the refusal does not say which stories share it: %s / %s", second, e.What, e.Why)
		}
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

// The backlog is the user's file. Moving a story changes its status and its
// updated time, and nothing else: not the fields the tool has no name for, not
// the order of the keys, not the way the file is laid out.
func TestMovingAStoryChangesOnlyItsStatusAndUpdatedTime(t *testing.T) {
	for _, tc := range []struct {
		name, before, after string
	}{
		{
			name: "fields the tool does not know, and the layout, survive",
			before: `{
    "owner": "billing team",
    "_schema": "x.json",
    "stories": [
        {"id": "A-0", "title": "Other", "status": "ready", "estimate": 3},
        {
            "id": "A-1",
            "status": "ready",
            "title": "Totals & <taxes>",
            "estimate": 5,
            "non_goals": ["persistence", "currency"],
            "acceptance_criteria": [{"id": "AC-1", "text": "WHEN x, y.", "owner": "qa"}],
            "updated": "2026-01-01T00:00:00Z",
            "custom": {"b": 1, "a": 2}
        }
    ]
}
`,
			after: `{
    "owner": "billing team",
    "_schema": "x.json",
    "stories": [
        {"id": "A-0", "title": "Other", "status": "ready", "estimate": 3},
        {
            "id": "A-1",
            "status": "in_progress",
            "title": "Totals & <taxes>",
            "estimate": 5,
            "non_goals": ["persistence", "currency"],
            "acceptance_criteria": [{"id": "AC-1", "text": "WHEN x, y.", "owner": "qa"}],
            "updated": "2026-09-10T08:30:00Z",
            "custom": {"b": 1, "a": 2}
        }
    ]
}
`,
		},
		{
			name: "a missing key is added after the last one, spaced like it",
			before: "{\"stories\": [\n\t{\n\t\t\"id\": \"A-1\",\n\t\t\"title\": \"One\",\n\t\t\"status\": \"ready\"," +
				"\n\t\t\"priority\": 10\n\t}\n]}\n",
			after: "{\"stories\": [\n\t{\n\t\t\"id\": \"A-1\",\n\t\t\"title\": \"One\",\n\t\t\"status\": \"in_progress\"," +
				"\n\t\t\"priority\": 10,\n\t\t\"updated\": \"2026-09-10T08:30:00Z\"\n\t}\n]}\n",
		},
		{
			name:   "a compact file stays compact, and gains the newline git wants",
			before: `{"stories":[{"id":"A-1","title":"One","Status":"ready"}]}`,
			after:  `{"stories":[{"id":"A-1","title":"One","Status":"in_progress","updated":"2026-09-10T08:30:00Z"}]}` + "\n",
		},
		{
			name:   "a file written with CRLF keeps its line endings",
			before: "{\r\n  \"stories\": [\r\n    {\"id\": \"A-1\", \"title\": \"One\", \"status\": \"ready\"}\r\n  ]\r\n}",
			after: "{\r\n  \"stories\": [\r\n    {\"id\": \"A-1\", \"title\": \"One\", \"status\": \"in_progress\"," +
				" \"updated\": \"2026-09-10T08:30:00Z\"}\r\n  ]\r\n}\r\n",
		},
		{
			name:   "a file saved with a byte order mark keeps it",
			before: "\ufeff{\r\n  \"stories\": [{\"id\": \"A-1\", \"title\": \"One\", \"status\": \"ready\"}]\r\n}\r\n",
			after: "\ufeff{\r\n  \"stories\": [{\"id\": \"A-1\", \"title\": \"One\", \"status\": \"in_progress\"," +
				" \"updated\": \"2026-09-10T08:30:00Z\"}]\r\n}\r\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(t)
			writeBacklog(t, s, tc.before)

			if err := s.SetStoryStatus("A-1", model.StatusInProgress); err != nil {
				t.Fatal(err)
			}

			raw, err := os.ReadFile(s.cfg.BacklogPath(s.root))
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tc.after {
				t.Errorf("backlog after the move:\n%q\nwant:\n%q", raw, tc.after)
			}
			if _, err := s.Backlog(); err != nil {
				t.Errorf("the edited backlog does not read back: %v", err)
			}
		})
	}
}

// Nor what kind of file it is, or who may read it. A backlog kept elsewhere
// and linked into the project was replaced by a copy, leaving the file it
// pointed to as it was; a private one became readable by everybody.
func TestMovingAStoryKeepsTheBacklogALinkWithItsPermissions(t *testing.T) {
	s := newStore(t)
	target := filepath.Join(t.TempDir(), "stories.json")
	if err := os.WriteFile(target, []byte(`{"stories":[{"id":"A-1","title":"One","status":"ready"}]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := s.cfg.BacklogPath(s.root)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if err := s.SetStoryStatus("A-1", model.StatusInProgress); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the backlog is no longer a link: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"in_progress"`) {
		t.Errorf("the file the backlog links to was not changed:\n%s", raw)
	}
	if runtime.GOOS == "windows" {
		return // Windows does not carry Unix permission bits
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the backlog's permissions changed: %v %v", info.Mode().Perm(), err)
	}
}

// What a review is stamped with leaves out what sdlc writes as a story moves,
// so that waiting for a person and resuming does not make every review stale.
// It leaves out nothing else.
func TestTheBacklogReadsTheSameForReviewAfterAStoryWaitsAndResumes(t *testing.T) {
	later := func() time.Time { return fixedTime.Add(48 * time.Hour) }
	for _, before := range []string{
		"{\n  \"stories\": [\n    {\n      \"id\": \"A-1\",\n      \"status\": \"ready\",\n" +
			"      \"title\": \"One\",\n      \"estimate\": 5\n    }\n  ]\n}\n",
		"{\"stories\": [\n\t{\n\t\t\"id\": \"A-1\",\n\t\t\"title\": \"One\",\n\t\t\"status\": \"todo\"\n\t}\n]}\n",
		`{"stories":[{"Status":"ready","id":"A-1","title":"One"},{"id":"B-2","title":"Two","status":"ready"}]}`,
	} {
		s := newStore(t)
		writeBacklog(t, s, before)
		path := s.cfg.BacklogPath(s.root)

		// Reviews happen after the story has started, so that is the backlog
		// they see.
		if err := s.SetStoryStatus("A-1", model.StatusInProgress); err != nil {
			t.Fatal(err)
		}
		reviewed, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		resumed := s.WithClock(later)
		for _, status := range []model.Status{model.StatusAwaitingHuman, model.StatusInProgress} {
			if err := resumed.SetStoryStatus("A-1", status); err != nil {
				t.Fatal(err)
			}
		}
		now, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(now) == string(reviewed) {
			t.Fatal("the round trip changed nothing on disk, so it proves nothing")
		}

		was, ok := withoutStoryFields(reviewed, storyBookkeeping...)
		is, ok2 := withoutStoryFields(now, storyBookkeeping...)
		if !ok || !ok2 {
			t.Fatalf("a valid backlog could not be read for review:\n%s", now)
		}
		if string(was) != string(is) {
			t.Errorf("waiting and resuming changed what a review sees:\nbefore:\n%s\nafter:\n%s", was, is)
		}

		retitled, _ := withoutStoryFields([]byte(strings.Replace(string(now), `"One"`, `"Changed"`, 1)),
			storyBookkeeping...)
		if string(retitled) == string(is) {
			t.Error("a change to the story itself was left out of what a review sees")
		}

		// A backlog saved with a byte order mark is read for review as well.
		marked, ok := withoutStoryFields(append([]byte("\ufeff"), now...), storyBookkeeping...)
		if !ok || string(marked) != string(is) {
			t.Errorf("a byte order mark changed what a review sees (read: %v):\n%s", ok, marked)
		}
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

	_, err := s.Active()
	if err == nil {
		t.Fatal("Active accepted a traversing id from the state file")
	}
	// There is no story by that name to rename; ending the iteration clears it.
	var e *sdlcerr.Error
	if !errors.As(err, &e) || !strings.Contains(e.Fix, "sdlc stop") || strings.Contains(e.Fix, "rename") {
		t.Errorf("the fix does not lead to clearing the file: %v", err)
	}
}

// Ending an iteration removes the file; nothing leaves it empty. The hook reads
// an empty one as naming no story and enforces nothing, so reading it here as
// "no iteration" would have doctor report all well while nothing was enforced.
func TestActiveRefusesAnEmptyStateFile(t *testing.T) {
	for _, body := range []string{"", "\n", "  \n"} {
		s := newStore(t)
		path := filepath.Join(s.root, filepath.FromSlash(activeFile))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Active(); err == nil {
			t.Errorf("Active read a state file holding %q as no iteration", body)
		}
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

// The hook refuses an id with ".." in it and enforces nothing for that
// iteration. `sdlc start A..1` succeeded, so the loop ran with every rule off.
func TestAStoryIDWithTwoDotsIsRefusedLikeTheHookRefusesIt(t *testing.T) {
	for _, id := range []string{"A..1", "..", "A-1/..", "..A"} {
		if CheckID(id) == nil {
			t.Errorf("CheckID accepted %q, which the hook refuses", id)
		}
	}
	for _, id := range []string{"A-1", "US-001", "billing.2", "a.b.c"} {
		if err := CheckID(id); err != nil {
			t.Errorf("CheckID refused %q: %v", id, err)
		}
	}
}
