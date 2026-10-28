package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
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
	// GIT_WORK_TREE and friends are stripped for the whole binary by TestMain
	// (see testenv_test.go), so a real one exported into a developer's shell
	// cannot point a test at the wrong repository.
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

// runGit runs git in dir, for the handful of tests that need a real
// repository to hash or diff rather than the backlog alone.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// initGit turns root into a fresh, empty git repository.
func initGit(t *testing.T, root string) {
	t.Helper()
	runGit(t, root, "init", "--quiet", "--initial-branch=main")
}

// commitAll stages everything in root and commits it, for a base commit later
// changes can be measured against.
func commitAll(t *testing.T, root, message string) {
	t.Helper()
	runGit(t, root, "add", "-A")
	runGit(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
}

// skipIfPermissionsDoNotBind is for a test that denies a write by taking away
// a directory's write permission: not on Windows, which does not carry Unix
// permission bits, and not as root, which ignores them.
func skipIfPermissionsDoNotBind(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this platform does not restrict a directory owner's own writes by permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

// A story id becomes a directory name, and ids come from a file an assistant
// may have written.
func TestCheckIDRejectsAnythingThatCouldEscapeTheStoriesDirectory(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	for _, id := range []string{"US-001", "AUTH-3", "PROJ-12.4", "a", "billing_2", "9LIVES-1", strings.Repeat("x", 64)} {
		if err := CheckID(id); err != nil {
			t.Errorf("CheckID(%q) rejected a reasonable id: %v", id, err)
		}
	}
}

func TestStoryDirRefusesAnUnsafeID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, err := s.StoryDir("../../etc"); err == nil {
		t.Fatal("StoryDir built a path from an id that escapes")
	}
}

func TestBacklogMissing(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	s := newStore(t)
	writeBacklog(t, s, `{"stories": [`)
	if _, err := s.Backlog(); codeOf(t, err) != sdlcerr.BacklogUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.BacklogUnreadable)
	}
}

// A status the loop does not know was read as no status: the story was never
// picked, never listed as waiting, and nothing said why.
func TestBacklogRefusesAStatusTheLoopDoesNotKnow(t *testing.T) {
	t.Parallel()
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
		if !errors.As(err, &e) {
			t.Fatalf("err = %v, want an sdlcerr.Error", err)
		}
		for _, want := range []string{"B-2", "ready", "dropped"} {
			if !strings.Contains(e.What+" "+e.Why, want) {
				t.Errorf("%s: the refusal does not name %q: %s / %s", story, want, e.What, e.Why)
			}
		}
	}
}

func TestBacklogNamesTheStoryThatIsMissingAField(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
		if !errors.As(err, &e) {
			t.Fatalf("err = %v, want an sdlcerr.Error", err)
		}
		if !strings.Contains(e.What, second) || !strings.Contains(e.Why, "2 and 3") {
			t.Errorf("%s: the refusal does not say which stories share it: %s / %s", second, e.What, e.Why)
		}
	}
}

func TestBacklogRejectsAStoryWhoseIDWouldEscape(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"ready"},{"id":"B-2","title":"Two","status":"ready"}]}`)

	_, _, err := s.Story("C-3")
	if err == nil {
		t.Fatal("Story found one that is not there")
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an sdlcerr.Error", err)
	}
	if !strings.Contains(e.Why, "A-1") || !strings.Contains(e.Why, "B-2") {
		t.Errorf("why = %q, want the available ids", e.Why)
	}
}

func TestSetStoryStatusSurvivesARoundTripAndKeepsUnknownFields(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	s := newStore(t)
	id, err := s.Active()
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Errorf("Active = %q, want empty", id)
	}
}

func TestSetActivePropagatesAnUnsafeID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.SetActive("../../etc"); codeOf(t, err) != sdlcerr.UnsafeStoryID {
		t.Errorf("code = %v, want %s", err, sdlcerr.UnsafeStoryID)
	}
}

func TestSetActiveThenClearActive(t *testing.T) {
	t.Parallel()
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

// The session working the story is recorded while it is under way, and goes
// with the iteration.
func TestTheSessionWorkingTheStoryEndsWithTheIteration(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path := filepath.Join(s.root, filepath.FromSlash(sessionFile))
	recorded := func() string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return ""
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(raw))
	}
	if err := s.SetActive("A-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.bindSession("3f2c9e10-aa01-4b7e-9f00-5d1c2b3a4e5f"); err != nil {
		t.Fatal(err)
	}
	if got := recorded(); got != "3f2c9e10-aa01-4b7e-9f00-5d1c2b3a4e5f" {
		t.Fatalf("session = %q", got)
	}
	// Outside Claude Code there is no session, and a start there records none.
	// Nor does a value that could be a path.
	for _, id := range []string{"", "../../elsewhere", "two words"} {
		if err := s.bindSession("session-a"); err != nil {
			t.Fatal(err)
		}
		if err := s.bindSession(id); err != nil {
			t.Fatal(err)
		}
		if got := recorded(); got != "" {
			t.Errorf("binding %q left %q recorded", id, got)
		}
	}
	if err := s.bindSession("session-a"); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearActive(); err != nil {
		t.Fatal(err)
	}
	if got := recorded(); got != "" {
		t.Errorf("ending the iteration left session %q recorded", got)
	}
}

func TestActiveRefusesATamperedStateFile(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// A clone brings links with everything else. With .sdlc/state committed as a
// link to a directory outside the repository, the stop hook wrote its count
// there, and every other write of the loop's state would have followed it.
func TestStateIsNotWrittenThroughALinkOutOfTheRepository(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(s.root, ".sdlc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.root, ".sdlc", "state")); err != nil {
		t.Skipf("this system cannot make a symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "active"), []byte("A-1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, err := range map[string]error{
		"SaveStopCount":  s.SaveStopCount(StopCount{Story: "A-1", Blocks: 1}),
		"SetActive":      s.SetActive("A-2"),
		"ClearActive":    s.ClearActive(),
		"ClearStopCount": s.ClearStopCount(),
		"ClearLock":      s.ClearLock(),
	} {
		if err == nil {
			t.Errorf("%s went through a link out of the repository", name)
		} else if got := codeOf(t, err); got != sdlcerr.StateUnwritable {
			t.Errorf("%s: code = %s, want %s", name, got, sdlcerr.StateUnwritable)
		}
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "active" {
		t.Errorf("the directory outside holds %v, want only the active file it started with", entries)
	}
	if raw, err := os.ReadFile(filepath.Join(outside, "active")); err != nil || string(raw) != "A-1\n" {
		t.Errorf("the active file outside was changed: %q, %v", raw, err)
	}
}

// ---------------------------------------------------------------- Root, Config, CommitPause, StorySubject

func TestConfigReturnsWhatTheStoreWasOpenedWith(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cfg := config.Default()
	cfg.Git.TrunkBranch = "trunk-x"
	s := New(&config.Project{Root: root, Config: cfg})
	if got := s.Config().Git.TrunkBranch; got != "trunk-x" {
		t.Errorf("Config().Git.TrunkBranch = %q, want trunk-x", got)
	}
	if s.Root() != root {
		t.Errorf("Root() = %q, want %q", s.Root(), root)
	}
}

func TestCommitPauseReportsTheStorysRiskTierAndWhetherItWaits(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[
		{"id":"A-1","title":"One","status":"ready"},
		{"id":"A-2","title":"Two","status":"ready","risk_tier":"high"}]}`)

	tier, pauses, err := s.CommitPause("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if tier != config.DefaultRiskTier || pauses {
		t.Errorf("A-1: tier=%q pauses=%v, want %q/false", tier, pauses, config.DefaultRiskTier)
	}

	tier, pauses, err = s.CommitPause("A-2")
	if err != nil {
		t.Fatal(err)
	}
	if tier != "high" || !pauses {
		t.Errorf("A-2: tier=%q pauses=%v, want high/true", tier, pauses)
	}

	if _, _, err := s.CommitPause("Z-9"); codeOf(t, err) != sdlcerr.StoryNotFound {
		t.Errorf("code = %v, want %s", err, sdlcerr.StoryNotFound)
	}
}

// StorySubject is what a review is stamped with: the story's own content,
// without the status and updated time the loop writes as it moves.
func TestStorySubjectIsStableAcrossAMoveButChangesWithTheStory(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"ready","priority":1}]}`)
	before, err := s.StorySubject("A-1")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetStoryStatus("A-1", model.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	after, err := s.StorySubject("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("StorySubject changed when only status and updated moved: %q -> %q", before, after)
	}

	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One (retitled)","status":"in_progress","priority":1}]}`)
	changed, err := s.StorySubject("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if changed == after {
		t.Error("StorySubject did not change when the story's own field changed")
	}

	if _, err := s.StorySubject("Z-9"); codeOf(t, err) != sdlcerr.StoryNotFound {
		t.Errorf("code = %v, want %s", err, sdlcerr.StoryNotFound)
	}
}

func TestStoryPropagatesABacklogReadError(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, _, err := s.Story("A-1"); codeOf(t, err) != sdlcerr.BacklogMissing {
		t.Errorf("code = %v, want %s", err, sdlcerr.BacklogMissing)
	}
}

// ---------------------------------------------------------------- more backlog reading

func TestBacklogFailsWhenTheFileCannotBeOpened(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path := s.cfg.BacklogPath(s.root)
	if err := os.MkdirAll(path, 0o755); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	if _, err := s.Backlog(); codeOf(t, err) != sdlcerr.BacklogUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.BacklogUnreadable)
	}
}

func TestBacklogRefusesAStoryWithNoID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"ready"},{"title":"No id","status":"ready"}]}`)
	_, err := s.Backlog()
	if codeOf(t, err) != sdlcerr.BacklogUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.BacklogUnreadable)
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an sdlcerr.Error", err)
	}
	if !strings.Contains(e.Why, "story number 2") {
		t.Errorf("why = %q, want it to name which story has no id", e.Why)
	}
}

func TestStoryNotFoundSaysTheBacklogIsEmpty(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[]}`)
	_, _, err := s.Story("A-1")
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an sdlcerr.Error", err)
	}
	if !strings.Contains(e.Why, "no stories") {
		t.Errorf("why = %q, want it to say the backlog has no stories", e.Why)
	}
}

// describeBacklog shows only the first 8 ids and a count of the rest, so the
// message stays readable against a backlog with hundreds of stories.
func TestStoryNotFoundSummarisesALongBacklog(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	var stories []string
	for i := 1; i <= 10; i++ {
		stories = append(stories, fmt.Sprintf(`{"id":"A-%d","title":"T","status":"ready"}`, i))
	}
	writeBacklog(t, s, `{"stories":[`+strings.Join(stories, ",")+`]}`)

	_, _, err := s.Story("Z-9")
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an sdlcerr.Error", err)
	}
	if !strings.Contains(e.Why, "A-1") || !strings.Contains(e.Why, "2 others") {
		t.Errorf("why = %q, want the first 8 ids and a count of the rest", e.Why)
	}
	if strings.Contains(e.Why, "A-9") {
		t.Errorf("why lists more than 8 ids: %q", e.Why)
	}
}

// One more than shown is "1 other", not "1 others".
func TestStoryNotFoundUsesTheSingularForExactlyOneMoreStory(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	var stories []string
	for i := 1; i <= 9; i++ {
		stories = append(stories, fmt.Sprintf(`{"id":"A-%d","title":"T","status":"ready"}`, i))
	}
	writeBacklog(t, s, `{"stories":[`+strings.Join(stories, ",")+`]}`)

	_, _, err := s.Story("Z-9")
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an sdlcerr.Error", err)
	}
	if !strings.Contains(e.Why, "1 other") || strings.Contains(e.Why, "1 others") {
		t.Errorf("why = %q, want the singular \"1 other\"", e.Why)
	}
}

// ---------------------------------------------------------------- more SetStoryStatus

func TestSetStoryStatusPropagatesABacklogReadError(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.SetStoryStatus("A-1", model.StatusInProgress); codeOf(t, err) != sdlcerr.BacklogMissing {
		t.Errorf("code = %v, want %s", err, sdlcerr.BacklogMissing)
	}
}

func TestSetStoryStatusOnAnUnknownStoryFails(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"ready"}]}`)
	if err := s.SetStoryStatus("Z-9", model.StatusInProgress); codeOf(t, err) != sdlcerr.StoryNotFound {
		t.Errorf("code = %v, want %s", err, sdlcerr.StoryNotFound)
	}
}

// Moving a story to the status it already has writes nothing: the whole tree
// is what a review is stamped with, and a write that changed only a timestamp
// would send a reviewer back to work that had not changed.
func TestSetStoryStatusToItsCurrentStatusWritesNothing(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	body := `{"stories":[{"id":"A-1","title":"One","status":"ready"}]}`
	writeBacklog(t, s, body)

	if err := s.SetStoryStatus("A-1", model.StatusReady); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(s.cfg.BacklogPath(s.root))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != body {
		t.Errorf("moving to the same status changed the file:\n%s", raw)
	}
}

func TestMovingAStoryFailsWhenTheBacklogDirectoryIsNotWritable(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"ready"}]}`)
	dir := filepath.Dir(s.cfg.BacklogPath(s.root))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	err := s.SetStoryStatus("A-1", model.StatusInProgress)
	if codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

// ---------------------------------------------------------------- Acknowledge

func TestAcknowledgedIsEmptyBeforeAnythingIsRecorded(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	sha, ok, err := s.Acknowledged()
	if err != nil {
		t.Fatal(err)
	}
	if ok || sha != "" {
		t.Errorf("Acknowledged = %q, %v, want empty/false", sha, ok)
	}
}

func TestAcknowledgeRecordsTheCommitAndAcknowledgedReadsItBack(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if err := s.Acknowledge("deadbeef"); err != nil {
		t.Fatal(err)
	}
	sha, ok, err := s.Acknowledged()
	if err != nil {
		t.Fatal(err)
	}
	if !ok || sha != "deadbeef" {
		t.Errorf("Acknowledged = %q, %v, want deadbeef/true", sha, ok)
	}
}

func TestAcknowledgedFailsWhenTheFileCannotBeRead(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path := filepath.Join(s.root, filepath.FromSlash(AcknowledgedFile))
	if err := os.MkdirAll(path, 0o755); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	if _, _, err := s.Acknowledged(); codeOf(t, err) != sdlcerr.StateUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnreadable)
	}
}

// ---------------------------------------------------------------- Active

func TestActiveFailsWhenTheFileCannotBeOpened(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path := filepath.Join(s.root, filepath.FromSlash(activeFile))
	if err := os.MkdirAll(path, 0o755); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	if _, err := s.Active(); codeOf(t, err) != sdlcerr.StateUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnreadable)
	}
}

func TestClearActiveFailsWhenTheStateCannotBeRemoved(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
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

	if err := s.ClearActive(); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

// ---------------------------------------------------------------- sessions

func TestBindSessionReadsTheEnvironmentAndWorkingSessionReadsItBack(t *testing.T) {
	s := newStore(t)
	t.Setenv(SessionEnv, "3f2c9e10-aa01-4b7e-9f00-5d1c2b3a4e5f")
	id, err := s.BindSession(false)
	if err != nil {
		t.Fatal(err)
	}
	if id != "3f2c9e10-aa01-4b7e-9f00-5d1c2b3a4e5f" {
		t.Errorf("BindSession = %q", id)
	}
	got, ok := s.WorkingSession()
	if !ok || got != id {
		t.Errorf("WorkingSession = %q, %v, want %q/true", got, ok, id)
	}
}

// Outside Claude Code there is no session in the environment. Picking up an
// iteration already under way leaves whatever is recorded alone; starting a
// fresh one removes it.
func TestBindSessionWithNoEnvironmentLeavesOrClearsTheRecord(t *testing.T) {
	s := newStore(t)
	t.Setenv(SessionEnv, "")
	if err := s.bindSession("earlier"); err != nil {
		t.Fatal(err)
	}

	id, err := s.BindSession(true)
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Errorf("BindSession(true) = %q, want empty", id)
	}
	if got, ok := s.WorkingSession(); !ok || got != "earlier" {
		t.Errorf("resuming cleared the session working the story: %q, %v", got, ok)
	}

	id, err = s.BindSession(false)
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Errorf("BindSession(false) = %q, want empty", id)
	}
	if _, ok := s.WorkingSession(); ok {
		t.Error("starting fresh with no session left one recorded")
	}
}

func TestWorkingSessionIsFalseForAnUnsafeValue(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path := filepath.Join(s.root, filepath.FromSlash(sessionFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("../../elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.WorkingSession(); ok {
		t.Error("WorkingSession accepted a value that could be a path")
	}
}

func TestBindSessionFailsWhenTheSessionFileCannotBeRemoved(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
	if err := s.bindSession("session-a"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.root, filepath.FromSlash(stateDir))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := s.bindSession(""); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

// A clone can bring a link in place of the session file too, the same way it
// can for the state directory as a whole. This is the path bindSession takes
// with no session to record: clearing whatever is there, rather than writing
// one.
func TestBindSessionDoesNotClearThroughALinkOutOfTheRepository(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(s.root, ".sdlc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.root, ".sdlc", "state")); err != nil {
		t.Skipf("this system cannot make a symlink: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "session"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := s.bindSession(""); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
	if raw, err := os.ReadFile(filepath.Join(outside, "session")); err != nil || string(raw) != "x\n" {
		t.Errorf("bindSession removed through the link: %q, %v", raw, err)
	}
}

// ---------------------------------------------------------------- the freeze

func TestLockIsNilBeforeTheTestsAreFrozen(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	l, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if l != nil {
		t.Errorf("Lock = %+v, want nil", l)
	}
}

func TestSaveLockThenLockRoundTripsAndClearLockRemovesIt(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	want := model.NewLock("A-1", map[string]string{"a_test.go": "deadbeef"}, s.Now())
	if err := s.SaveLock(want); err != nil {
		t.Fatal(err)
	}

	got, err := s.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Story != "A-1" || got.Files["a_test.go"] != "deadbeef" {
		t.Errorf("Lock = %+v", got)
	}

	if err := s.ClearLock(); err != nil {
		t.Fatal(err)
	}
	if l, err := s.Lock(); err != nil || l != nil {
		t.Errorf("Lock after ClearLock = %+v, %v", l, err)
	}
	// Clearing twice is not an error.
	if err := s.ClearLock(); err != nil {
		t.Errorf("ClearLock on a clear store: %v", err)
	}
}

func TestLockRejectsUnreadableOrCorruptFiles(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path := filepath.Join(s.root, filepath.FromSlash(lockFile))
	if err := os.MkdirAll(path, 0o755); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	if _, err := s.Lock(); codeOf(t, err) != sdlcerr.StateUnreadable {
		t.Errorf("directory: code = %v, want %s", err, sdlcerr.StateUnreadable)
	}

	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lock(); codeOf(t, err) != sdlcerr.StateUnreadable {
		t.Errorf("malformed JSON: code = %v, want %s", err, sdlcerr.StateUnreadable)
	}
}

func TestClearLockFailsWhenTheFileCannotBeRemoved(t *testing.T) {
	t.Parallel()
	skipIfPermissionsDoNotBind(t)
	s := newStore(t)
	if err := s.SaveLock(model.NewLock("A-1", nil, s.Now())); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.root, filepath.FromSlash(stateDir))
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if err := s.ClearLock(); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

// ---------------------------------------------------------------- HashFile

func TestHashFileHashesContentAndFailsWhenItCannotBeRead(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	rel := "some/file_test.go"
	path := filepath.Join(s.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package some\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256([]byte("package some\n"))
	want := hex.EncodeToString(sum[:])
	got, err := s.HashFile(rel)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("HashFile = %q, want %q", got, want)
	}

	if _, err := s.HashFile("missing/file.go"); codeOf(t, err) != sdlcerr.StateUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnreadable)
	}
}

// ---------------------------------------------------------------- records

func TestRecordPropagatesAnUnsafeID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if _, err := s.Record("../../etc"); codeOf(t, err) != sdlcerr.UnsafeStoryID {
		t.Errorf("code = %v, want %s", err, sdlcerr.UnsafeStoryID)
	}
}

func TestRecordFailsWhenTheFileCannotBeOpened(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	dir, _ := s.StoryDir("A-1")
	recordPath := filepath.Join(dir, recordFile)
	if err := os.MkdirAll(recordPath, 0o755); err != nil { // a directory, not a file
		t.Fatal(err)
	}
	if _, err := s.Record("A-1"); codeOf(t, err) != sdlcerr.StateUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnreadable)
	}
}

func TestRecordFillsInGatesWhenTheFileHasNone(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	dir, _ := s.StoryDir("A-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, recordFile), []byte(`{"story":"A-1"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := s.Record("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Gates == nil {
		t.Fatal("Record left Gates nil for a record file that had none")
	}
	if len(r.Gates) != 0 {
		t.Errorf("Gates = %v, want empty", r.Gates)
	}
}

func TestSaveRecordPropagatesAnUnsafeID(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	r := model.NewRecord("../../etc", s.Now())
	if err := s.SaveRecord(r); codeOf(t, err) != sdlcerr.UnsafeStoryID {
		t.Errorf("code = %v, want %s", err, sdlcerr.UnsafeStoryID)
	}
}

// ---------------------------------------------------------------- StoriesOnDisk

func TestStoriesOnDiskListsOnlyDirectoriesWithSafeIDs(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	ids, err := s.StoriesOnDisk()
	if err != nil {
		t.Fatal(err)
	}
	if ids != nil {
		t.Errorf("StoriesOnDisk with nothing on disk = %v, want nil", ids)
	}

	for _, id := range []string{"A-1", "B-2"} {
		dir, err := s.StoryDir(id)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	storiesRoot := filepath.Join(s.root, filepath.FromSlash(storiesDir))
	if err := os.WriteFile(filepath.Join(storiesRoot, "not-a-dir"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(storiesRoot, "a b"), 0o755); err != nil { // a space: not a safe id
		t.Fatal(err)
	}

	ids, err = s.StoriesOnDisk()
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(ids)
	if want := []string{"A-1", "B-2"}; !slices.Equal(ids, want) {
		t.Errorf("StoriesOnDisk = %v, want %v", ids, want)
	}
}

// AC-2: `.sdlc/stories` replaced by a file is loop state that cannot be read,
// SDLC-E0005, not a backlog with no story records -- on every platform. On
// Windows, os.ReadDir's open of a file fails with ERROR_PATH_NOT_FOUND, which
// Go maps to fs.ErrNotExist (syscall_windows.go), so StoriesOnDisk must not
// take that as "no stories" (store.go:618-619 does today).
func TestStoriesOnDiskFailsWhenTheDirectoryCannotBeReadAC2(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	storiesRoot := filepath.Join(s.root, filepath.FromSlash(storiesDir))
	if err := os.MkdirAll(filepath.Dir(storiesRoot), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(storiesRoot, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := s.StoriesOnDisk()
	got := codeOf(t, err)
	if got != sdlcerr.StateUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnreadable)
	}
	if got.String() != "SDLC-E0005" {
		t.Errorf("code = %s, want SDLC-E0005", got)
	}
}

// ---------------------------------------------------------------- artifacts and reviews

func TestArtifactStoredReportsWhetherTheFileIsOnDisk(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	analysis, _ := model.FindArtifact("analysis")
	if s.ArtifactStored("A-1", analysis) {
		t.Error("ArtifactStored is true before anything is written")
	}
	if _, err := s.WriteArtifact("A-1", analysis, []byte("x\n")); err != nil {
		t.Fatal(err)
	}
	if !s.ArtifactStored("A-1", analysis) {
		t.Error("ArtifactStored is false after WriteArtifact")
	}
	// An unsafe id answers false rather than failing: every path that could
	// have written the file has already refused it.
	if s.ArtifactStored("../../etc", analysis) {
		t.Error("ArtifactStored accepted a traversing id")
	}
}

func TestWriteArtifactFailsWhenTheFileCannotBeWritten(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	analysis, _ := model.FindArtifact("analysis")
	dir, _ := s.StoryDir("A-1")
	// A directory where the artifact's file needs to be.
	if err := os.MkdirAll(filepath.Join(dir, analysis.File), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteArtifact("A-1", analysis, []byte("x\n")); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

func TestWriteReviewKeepsEachRoundAndReviewPathAgrees(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path1, err := s.WriteReview("A-1", model.GateDesignReview, "architect", 1, []byte("first\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := ReviewPath("A-1", model.GateDesignReview, "architect", 1); path1 != want {
		t.Errorf("path = %q, want %q", path1, want)
	}
	path2, err := s.WriteReview("A-1", model.GateDesignReview, "architect", 2, []byte("second\n"))
	if err != nil {
		t.Fatal(err)
	}
	if path1 == path2 {
		t.Fatal("a second round overwrote the first round's path")
	}
	for path, want := range map[string]string{path1: "first\n", path2: "second\n"} {
		body, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != want {
			t.Errorf("%s: on disk %q, want %q", path, body, want)
		}
	}

	if _, err := s.WriteReview("../../etc", model.GateDesignReview, "architect", 1, []byte("x")); err == nil {
		t.Fatal("a review was written outside the stories directory")
	} else if got := codeOf(t, err); got != sdlcerr.UnsafeStoryID {
		t.Errorf("code = %s, want %s", got, sdlcerr.UnsafeStoryID)
	}
}

func TestWriteReviewFailsWhenTheFileCannotBeWritten(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	dir, _ := s.StoryDir("A-1")
	reviewPath := filepath.Join(dir, reviewsDir, reviewName(model.GateDesignReview, "architect", 1))
	if err := os.MkdirAll(reviewPath, 0o755); err != nil { // a directory where the file needs to be
		t.Fatal(err)
	}
	if _, err := s.WriteReview("A-1", model.GateDesignReview, "architect", 1, []byte("x")); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

// ---------------------------------------------------------------- small helpers

func TestRelativeFallsBackToTheFullPathWhenItWouldLeaveTheRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x")
	if got := relative(root, outside); got != outside {
		t.Errorf("relative = %q, want %q", got, outside)
	}
}
