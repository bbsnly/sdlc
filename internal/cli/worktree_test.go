package cli

// US-029.1 in-process: every checkout git knows as a worktree of one repository
// works one backlog, one record of each story and one claim on it. The same
// criteria are driven through the built binary in test/e2e/worktree_test.go;
// these run the commands through Execute, so that what the story adds to cli,
// store and config is measured by coverage.
//
// The worktrees are made with `git worktree add`, as a person makes one, each
// on a branch of its own that its own .sdlc/config.json names as trunk: each
// checkout keeps reading its own configuration, and a new story starts only on
// trunk. Refusals are matched by the code they print, so that nothing here
// names an identifier the story has still to add.
//
// Helpers carry a wt prefix: they live in package cli beside whatever the
// story adds to it.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// The Claude Code sessions stories are held in. One session holds one story.
const (
	wtSessionMain     = "wt-session-main"
	wtSessionWorktree = "wt-session-worktree"
	wtSessionThird    = "wt-session-third"
)

func wtTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

func wtGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func wtCommit(t *testing.T, dir, message string) {
	t.Helper()
	wtGit(t, dir, "add", "-A")
	wtGit(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit", "--quiet", "--allow-empty", "-m", message)
}

// wtRun runs a command in dir, from session, or from a terminal when session
// is empty. The test's commands go on running there, in that session, until it
// says otherwise.
func wtRun(t *testing.T, dir, session, stdin string, args ...string) result {
	t.Helper()
	chdir(t, dir)
	setenv(t, store.SessionEnv, session)
	return runWith(t, stdin, args...)
}

func wtMust(t *testing.T, dir, session, stdin string, args ...string) result {
	t.Helper()
	r := wtRun(t, dir, session, stdin, args...)
	if r.code != 0 {
		t.Fatalf("sdlc %s in %s exited %d:\n%s%s", strings.Join(args, " "), dir, r.code, r.stdout, r.stderr)
	}
	return r
}

// wtStarted is the story `sdlc start` began or resumed in dir.
func wtStarted(t *testing.T, dir, session string, args ...string) startPayload {
	t.Helper()
	return decode[startPayload](t, wtMust(t, dir, session, "", append(append([]string{"start"}, args...), "--json")...))
}

// wtStory is a story of a backlog made for a test: the example story sdlc init
// writes, under another id.
type wtStory struct {
	id        string
	dependsOn []string
}

// wtRepository is a main checkout with the loop set up and the stories given,
// each a priority lower than the one before, all committed.
//
// Story worktrees are off, committed, so every worktree made from it has them
// off too: these are US-029.1's tests, of checkouts a person made, and `sdlc
// start` in them is the released tool's (US-029.2 AC-9).
func wtRepository(t *testing.T, stories ...wtStory) string {
	t.Helper()
	return wtRepositoryWith(t, func(root string) { worktreesOff(t, root) }, stories...)
}

// wtRepositoryWith is wtRepository with configure, when there is one, run on
// the new project before it is committed.
func wtRepositoryWith(t *testing.T, configure func(root string), stories ...wtStory) string {
	t.Helper()
	root := wtTempDir(t)
	wtRepositoryAt(t, root, configure, stories...)
	return root
}

// wtRepositoryAt is wtRepositoryWith in root, an empty directory, for a
// walked template's builder.
func wtRepositoryAt(t *testing.T, root string, configure func(root string), stories ...wtStory) {
	t.Helper()
	seedGitInit(t, root)
	if len(stories) > 0 {
		wtSetStories(t, root, stories...)
	}
	if configure != nil {
		configure(root)
	}
	wtCommit(t, root, "the project")
}

func wtSetStories(t *testing.T, root string, stories ...wtStory) {
	t.Helper()
	path := filepath.Join(root, "user_stories.json")
	var backlog map[string]any
	wtReadJSON(t, path, &backlog)
	list, _ := backlog["stories"].([]any)
	if len(list) == 0 {
		t.Fatal("the scaffolded backlog is empty")
	}
	template, _ := list[0].(map[string]any)
	out := make([]any, 0, len(stories))
	for i, s := range stories {
		next := maps.Clone(template)
		next["id"] = s.id
		next["title"] = "Story " + s.id
		next["priority"] = i + 1
		next["status"] = string(model.StatusReady)
		delete(next, "depends_on")
		if len(s.dependsOn) > 0 {
			next["depends_on"] = s.dependsOn
		}
		out = append(out, next)
	}
	backlog["stories"] = out
	wtWriteJSON(t, path, backlog)
}

// wtSetStatus is a person editing a story's status in the backlog by hand.
func wtSetStatus(t *testing.T, root, id string, status model.Status) {
	t.Helper()
	path := filepath.Join(root, "user_stories.json")
	var backlog map[string]any
	wtReadJSON(t, path, &backlog)
	for _, story := range wtStoriesIn(t, backlog) {
		if story["id"] == id {
			story["status"] = string(status)
		}
	}
	wtWriteJSON(t, path, backlog)
}

// wtStoriesIn is the stories of a backlog read as a generic object, which a
// test edits in place.
func wtStoriesIn(t *testing.T, backlog map[string]any) []map[string]any {
	t.Helper()
	list, ok := backlog["stories"].([]any)
	if !ok {
		t.Fatalf("the backlog has no list of stories: %v", backlog["stories"])
	}
	stories := make([]map[string]any, 0, len(list))
	for _, s := range list {
		story, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("the backlog has a story that is not an object: %v", s)
		}
		stories = append(stories, story)
	}
	return stories
}

// wtStatuses is each story's status in root's backlog.
func wtStatuses(t *testing.T, root string) map[string]string {
	t.Helper()
	var backlog model.Backlog
	wtReadJSON(t, filepath.Join(root, "user_stories.json"), &backlog)
	out := map[string]string{}
	for _, s := range backlog.Stories {
		out[s.ID] = string(s.Status)
	}
	return out
}

func wtReadJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func wtWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func wtRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	return string(raw)
}

func wtExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// wtWorktree adds a linked worktree of main on a branch of its own, and makes
// that branch the worktree's trunk in its own configuration.
func wtWorktree(t *testing.T, main, name string) string {
	t.Helper()
	dir := filepath.Join(wtTempDir(t), name)
	wtGit(t, main, "worktree", "add", "--quiet", "-b", name, dir)
	wtTrunkIs(t, dir, name)
	return dir
}

// wtTrunkIs makes branch the trunk in dir's own configuration, where it has
// one, so that the refusal a test waits for is not the one about the branch.
func wtTrunkIs(t *testing.T, dir, branch string) {
	t.Helper()
	path := filepath.Join(dir, ".sdlc", "config.json")
	if !wtExists(path) {
		return
	}
	var cfg map[string]any
	wtReadJSON(t, path, &cfg)
	section, _ := cfg["git"].(map[string]any)
	if section == nil {
		section = map[string]any{}
	}
	section["trunk_branch"] = branch
	cfg["git"] = section
	wtWriteJSON(t, path, cfg)
}

func wtRecordPath(root, id string) string {
	return filepath.Join(root, ".sdlc", "stories", id, model.RecordFile)
}

// wtFiles is every regular file under root, by slash-separated relative path,
// with its content. A root that does not exist has none.
func wtFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// wtLoopState is what the loop keeps in a checkout: everything under .sdlc/,
// and the backlog.
func wtLoopState(t *testing.T, root string) map[string]string {
	t.Helper()
	state := wtFiles(t, filepath.Join(root, ".sdlc"))
	if backlog := filepath.Join(root, "user_stories.json"); wtExists(backlog) {
		state["<backlog>"] = wtRead(t, backlog)
	}
	return state
}

// wtPerCheckout is what AC-5 keeps apart: a checkout's freeze, the session
// bound to it and the Stop guard's count.
func wtPerCheckout(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range []string{"tests.lock", "session", "stop-blocks.json"} {
		out[name] = wtRead(t, filepath.Join(root, ".sdlc", "state", name))
	}
	return out
}

// wtNames is whether text names the path, as it is or as sdlc quotes it in a
// message: slash-separated, then quoted (AC-3 of US-050). The quoted native
// path is not accepted, because on Windows it doubles every separator.
func wtNames(text, path string) bool {
	quoted := strconv.Quote(filepath.ToSlash(path))
	return strings.Contains(text, path) || strings.Contains(text, quoted[1:len(quoted)-1])
}

// wtTies is whether a JSON document ties the story to the checkout at path: an
// object with the story's id among its values that names that path and none of
// the others, or an entry keyed by the id that names the path.
func wtTies(doc any, id, path string, others ...string) bool {
	switch v := doc.(type) {
	case map[string]any:
		hasID := false
		for k, x := range v {
			if s, ok := x.(string); ok && s == id {
				hasID = true
			}
			if k == id && wtMentions(x, path) {
				return true
			}
			if wtTies(x, id, path, others...) {
				return true
			}
		}
		if !hasID || !wtMentions(v, path) {
			return false
		}
		for _, other := range others {
			if wtMentions(v, other) {
				return false
			}
		}
		return true
	case []any:
		for _, x := range v {
			if wtTies(x, id, path, others...) {
				return true
			}
		}
	}
	return false
}

func wtMentions(doc any, path string) bool {
	switch v := doc.(type) {
	case string:
		return v == path || filepath.Clean(v) == path || wtNames(v, path)
	case map[string]any:
		for _, x := range v {
			if wtMentions(x, path) {
				return true
			}
		}
	case []any:
		for _, x := range v {
			if wtMentions(x, path) {
				return true
			}
		}
	}
	return false
}

func wtDocument(t *testing.T, r result) any {
	t.Helper()
	var doc any
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r.stdout)
	}
	return doc
}

// ---------------------------------------------------------------- AC-1

func TestAC1AWorktreeReadsAndWritesTheMainCheckoutsBacklogAndRecords(t *testing.T) {
	t.Parallel()
	for _, layout := range []struct {
		name    string
		ignored bool
	}{
		{"AC-1 where the backlog is committed", false},
		{"AC-1 where the backlog and the records are ignored", true},
	} {
		t.Run(layout.name, func(t *testing.T) {
			main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
			if layout.ignored {
				writeFile(t, main, ".gitignore", "/user_stories.json\n/.sdlc/stories/\n/.sdlc/state/\n")
				wtGit(t, main, "rm", "--cached", "--quiet", "user_stories.json")
				wtCommit(t, main, "the backlog is the loop's")
			}
			wt := wtWorktree(t, main, "wt")
			wtSetStories(t, main, wtStory{id: "US-001"}, wtStory{id: "US-002"}, wtStory{id: "US-003"})
			if !layout.ignored {
				writeFile(t, wt, "user_stories.json",
					strings.Replace(wtRead(t, filepath.Join(wt, "user_stories.json")), `"Story US-001"`, `"A copy nobody reads"`, 1))
			}
			copyBefore := wtRead(t, filepath.Join(wt, "user_stories.json"))

			listed := decode[storyListPayload](t, wtMust(t, wt, wtSessionWorktree, "", "story", "list", "--json"))
			var ids []string
			for _, row := range listed.Stories {
				ids = append(ids, row.ID)
				if row.ID == "US-001" && row.Title != "Story US-001" {
					t.Errorf("story list in the worktree read the worktree's copy: US-001 is titled %q", row.Title)
				}
			}
			if !slices.Equal(ids, []string{"US-001", "US-002", "US-003"}) {
				t.Errorf("story list in the worktree lists %v, want the main checkout's three stories", ids)
			}

			if got := wtStarted(t, wt, wtSessionWorktree); got.Story != "US-001" {
				t.Fatalf("start in the worktree began %q, want US-001", got.Story)
			}
			if got := wtStatuses(t, main)["US-001"]; got != "in_progress" {
				t.Errorf("the main checkout's backlog has US-001 %q, want in_progress", got)
			}
			wtMust(t, wt, wtSessionWorktree, "", "gate", "dor", "pass", "--note", "criteria are testable")
			wtMust(t, wt, wtSessionWorktree, "# Analysis\n", "artifact", "write", "analysis")
			wtMust(t, wt, wtSessionWorktree, "", "cost", "add", "--usd", "1.50")
			for _, name := range []string{model.RecordFile, "ANALYSIS.md"} {
				if !wtExists(filepath.Join(main, ".sdlc", "stories", "US-001", name)) {
					t.Errorf("%s was not written in the main checkout", name)
				}
			}
			if wtExists(filepath.Join(wt, ".sdlc", "stories")) {
				t.Error("the worktree has a .sdlc/stories of its own")
			}
			if got := decode[statusPayload](t, wtMust(t, wt, wtSessionWorktree, "", "status", "--json")); got.Gates["dor"] != "pass" {
				t.Errorf("status in the worktree shows gates %v, want dor passed", got.Gates)
			}

			// A record git checked out in the worktree, saying more than the real
			// one, is never read and never touched (D5).
			var planted map[string]any
			wtReadJSON(t, wtRecordPath(main, "US-001"), &planted)
			if gates, ok := planted["gates"].(map[string]any); ok {
				gates["analysis"] = gates["dor"]
			}
			if err := os.MkdirAll(filepath.Dir(wtRecordPath(wt, "US-001")), 0o755); err != nil {
				t.Fatal(err)
			}
			wtWriteJSON(t, wtRecordPath(wt, "US-001"), planted)
			plantedBody := wtRead(t, wtRecordPath(wt, "US-001"))
			if got := decode[statusPayload](t, wtMust(t, wt, wtSessionWorktree, "", "status", "--json")); got.Gates["analysis"] == "pass" {
				t.Error("status in the worktree read the record git put there")
			}

			wtMust(t, wt, wtSessionWorktree, "", "escalate", "spec_unclear", "--message", "which greeting?")
			if got := wtStatuses(t, main)["US-001"]; got != "awaiting_human" {
				t.Errorf("escalating in the worktree left US-001 %q in the main checkout, want awaiting_human", got)
			}
			wtMust(t, wt, "", "", "approve", "US-001")
			if got := wtStatuses(t, main)["US-001"]; got != "in_progress" {
				t.Errorf("approving in the worktree left US-001 %q in the main checkout, want in_progress", got)
			}
			// D3: the story resumes in the checkout that holds it.
			if got := wtStarted(t, wt, wtSessionWorktree); got.Story != "US-001" || !got.Resume {
				t.Errorf("start in the worktree after the approval gave %+v, want US-001 resumed", got)
			}
			wtMust(t, wt, "", "", "stop")

			if got := wtRead(t, filepath.Join(wt, "user_stories.json")); got != copyBefore {
				t.Error("the worktree's own backlog was written to")
			}
			if got := wtFiles(t, filepath.Join(wt, ".sdlc", "stories")); len(got) != 1 || got["US-001/"+model.RecordFile] != plantedBody {
				t.Errorf("the worktree's .sdlc/stories holds %v, want only the record git put there, untouched",
					slices.Collect(maps.Keys(got)))
			}
		})
	}
}

func TestAC1InitInAWorktreeWritesTheStarterBacklogInTheMainCheckout(t *testing.T) {
	t.Parallel()
	main := wtTempDir(t)
	wtGit(t, main, "init", "--quiet", "--initial-branch=main")
	writeFile(t, main, "README.md", "a project without the loop\n")
	wtCommit(t, main, "a project")
	wt := wtWorktree(t, main, "wt")

	wtMust(t, wt, "", "", "init")
	if !wtExists(filepath.Join(main, "user_stories.json")) {
		t.Error("init in the worktree wrote no backlog in the main checkout")
	}
	if wtExists(filepath.Join(wt, "user_stories.json")) {
		t.Error("init in the worktree wrote a backlog in the worktree")
	}
}

// ---------------------------------------------------------------- AC-2

func TestAC2StartInAWorktreeSkipsAStoryTheMainCheckoutHolds(t *testing.T) {
	t.Parallel()
	t.Run("AC-2 a story under way is neither resumed nor picked", func(t *testing.T) {
		t.Parallel()
		main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
		wt := wtWorktree(t, main, "wt")
		wtStarted(t, main, wtSessionMain)
		if got := wtStarted(t, wt, wtSessionWorktree); got.Story != "US-002" || got.Resume {
			t.Errorf("start in the worktree gave %+v, want US-002 begun afresh", got)
		}
		if got := wtStatuses(t, main); got["US-001"] != "in_progress" || got["US-002"] != "in_progress" {
			t.Errorf("the backlog is %v, want both stories in_progress", got)
		}
	})

	t.Run("AC-2 a story waiting on a person where it is held is skipped", func(t *testing.T) {
		t.Parallel()
		main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
		wt := wtWorktree(t, main, "wt")
		wtStarted(t, main, wtSessionMain)
		wtMust(t, main, wtSessionMain, "", "escalate", "spec_unclear", "--message", "which greeting?")
		if got := wtStarted(t, wt, wtSessionWorktree); got.Story != "US-002" {
			t.Errorf("start in the worktree gave %q, want US-002", got.Story)
		}
	})

	t.Run("AC-2 a story stopped part-way stays held", func(t *testing.T) {
		t.Parallel()
		main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
		wt := wtWorktree(t, main, "wt")
		wtStarted(t, main, wtSessionMain)
		wtMust(t, main, "", "", "stop")
		if got := wtStarted(t, wt, wtSessionWorktree); got.Story != "US-002" {
			t.Errorf("start in the worktree gave %q, want US-002", got.Story)
		}
	})

	t.Run("AC-2 a story waiting on a held story waits as if the held story were missing", func(t *testing.T) {
		t.Parallel()
		main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002", dependsOn: []string{"US-001"}},
			wtStory{id: "US-003"})
		wt := wtWorktree(t, main, "wt")
		wtStarted(t, main, wtSessionMain)
		if got := wtStarted(t, wt, wtSessionWorktree); got.Story != "US-003" {
			t.Errorf("start in the worktree gave %q, want US-003", got.Story)
		}
	})

	t.Run("AC-2 with only the held story left there is nothing to start", func(t *testing.T) {
		t.Parallel()
		main := wtRepository(t, wtStory{id: "US-001"})
		wt := wtWorktree(t, main, "wt")
		wtStarted(t, main, wtSessionMain)
		before := wtLoopState(t, main)
		if r := wtRun(t, wt, wtSessionWorktree, "", "start"); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0010") {
			t.Errorf("start in the worktree exited %d, want SDLC-E0010:\n%s%s", r.code, r.stdout, r.stderr)
		}
		if !maps.Equal(before, wtLoopState(t, main)) {
			t.Error("the refused start changed the main checkout's loop state")
		}
	})
}

// ---------------------------------------------------------------- AC-4

// The moment itself is the end-to-end test's, three processes started at once.
// Here each checkout starts in turn, and each start is what the lock serialises:
// every one is given a story of its own, and the one backlog keeps every change.
func TestAC4EachCheckoutThatStartsIsGivenItsOwnStoryAndTheBacklogKeepsEveryChange(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"}, wtStory{id: "US-003"})
	second, third := wtWorktree(t, main, "wt-b"), wtWorktree(t, main, "wt-c")
	copies := map[string]string{
		second: wtRead(t, filepath.Join(second, "user_stories.json")),
		third:  wtRead(t, filepath.Join(third, "user_stories.json")),
	}

	given := map[string]string{}
	for _, c := range []struct{ dir, session string }{
		{main, wtSessionMain}, {second, wtSessionWorktree}, {third, wtSessionThird},
	} {
		id := wtStarted(t, c.dir, c.session).Story
		if other, taken := given[id]; taken {
			t.Errorf("%s was given to both %s and %s", id, other, c.dir)
		}
		given[id] = c.dir
	}
	want := map[string]string{"US-001": "in_progress", "US-002": "in_progress", "US-003": "in_progress"}
	if got := wtStatuses(t, main); !maps.Equal(got, want) {
		t.Errorf("the backlog holds %v, want %v", got, want)
	}
	for dir, body := range copies {
		if wtRead(t, filepath.Join(dir, "user_stories.json")) != body {
			t.Errorf("start wrote the worktree's own copy of the backlog in %s", dir)
		}
	}
}

// ---------------------------------------------------------------- AC-5

func TestAC5FreezeUnfreezeAndStopInOneCheckoutLeaveTheOthersFreezeSessionAndStopCount(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := wtWorktree(t, main, "wt")
	for _, c := range []struct{ dir, session, id string }{
		{main, wtSessionMain, "US-001"}, {wt, wtSessionWorktree, "US-002"},
	} {
		wtStarted(t, c.dir, c.session, c.id)
		reach(t, c.dir, model.GatePlan)
		// A stop count for the other checkout to leave alone.
		writeFile(t, c.dir, ".sdlc/state/stop-blocks.json", `{"story":"`+c.id+`","progress":"x","blocks":1}`+"\n")
	}
	if got := wtStatuses(t, main); got["US-001"] != "in_progress" || got["US-002"] != "in_progress" {
		t.Fatalf("precondition: the main checkout's backlog is %v, want both stories in_progress", got)
	}
	if !wtExists(wtRecordPath(main, "US-002")) {
		t.Fatal("precondition: the worktree's story has no record in the main checkout")
	}

	unchanged := func(t *testing.T, what, other string, act func()) {
		t.Helper()
		before := wtPerCheckout(t, other)
		act()
		after := wtPerCheckout(t, other)
		for name := range before {
			if before[name] != after[name] {
				t.Errorf("%s changed %s in %s", what, name, other)
			}
		}
	}
	for _, pair := range []struct {
		name, here, session, there string
	}{
		{"in the worktree", wt, wtSessionWorktree, main},
		{"in the main checkout", main, wtSessionMain, wt},
	} {
		t.Run("AC-5 unfreeze and freeze "+pair.name, func(t *testing.T) {
			unchanged(t, "unfreeze "+pair.name, pair.there, func() {
				wtMust(t, pair.here, "", "", "unfreeze", "--reason", "the test has to change")
			})
			unchanged(t, "freeze "+pair.name, pair.there, func() {
				wtMust(t, pair.here, pair.session, "", "freeze")
			})
		})
	}
	t.Run("AC-5 stop in the worktree", func(t *testing.T) {
		unchanged(t, "stop in the worktree", main, func() {
			wtMust(t, wt, "", "", "stop")
		})
	})
}

// ---------------------------------------------------------------- AC-6

func TestAC6StatusStoryListAndDoctorInEitherCheckoutNameTheCheckoutHoldingEachStory(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"}, wtStory{id: "US-003"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, main, wtSessionMain, "US-001")
	wtStarted(t, wt, wtSessionWorktree, "US-002")

	for _, c := range []struct{ name, dir, session string }{
		{"from the main checkout", main, wtSessionMain},
		{"from the worktree", wt, wtSessionWorktree},
	} {
		t.Run("AC-6 status "+c.name, func(t *testing.T) {
			r := wtMust(t, c.dir, c.session, "", "status", "--json")
			if got := decode[statusPayload](t, r).Backlog; got["in_progress"] != 2 || got["ready"] != 1 {
				t.Errorf("status counts %v, want the repository's 2 in_progress and 1 ready", got)
			}
			doc := wtDocument(t, r)
			if !wtTies(doc, "US-001", main, wt) || !wtTies(doc, "US-002", wt, main) {
				t.Errorf("status --json does not tie each held story to its checkout:\n%s", r.stdout)
			}
			if out := wtMust(t, c.dir, c.session, "", "status").stdout; !wtNames(out, main) || !wtNames(out, wt) {
				t.Errorf("status does not name the checkouts holding the stories:\n%s", out)
			}
		})

		t.Run("AC-6 story list "+c.name, func(t *testing.T) {
			r := wtMust(t, c.dir, c.session, "", "story", "list", "--json")
			got := map[string]string{}
			for _, row := range decode[storyListPayload](t, r).Stories {
				got[row.ID] = row.Status
			}
			if want := map[string]string{"US-001": "in_progress", "US-002": "in_progress", "US-003": "ready"}; !maps.Equal(got, want) {
				t.Errorf("story list shows %v, want %v", got, want)
			}
			doc := wtDocument(t, r)
			if !wtTies(doc, "US-001", main, wt) || !wtTies(doc, "US-002", wt, main) {
				t.Errorf("story list --json does not tie each held story to its checkout:\n%s", r.stdout)
			}
			if out := wtMust(t, c.dir, c.session, "", "story", "list").stdout; !wtNames(out, main) || !wtNames(out, wt) {
				t.Errorf("story list does not name the checkouts holding the stories:\n%s", out)
			}
		})

		t.Run("AC-6 doctor "+c.name, func(t *testing.T) {
			r := wtRun(t, c.dir, c.session, "", "doctor", "--json")
			for _, check := range decode[doctorPayload](t, r).Checks {
				if check.Name == "loop state" && (!wtNames(check.Detail, main) || !wtNames(check.Detail, wt)) {
					t.Errorf("doctor's loop state does not name the checkouts holding the stories: %q", check.Detail)
				}
			}
		})
	}

	t.Run("AC-6 doctor in the worktree reads every record in the main checkout", func(t *testing.T) {
		writeFile(t, main, ".sdlc/stories/US-003/"+model.RecordFile, "{ not a record")
		r := wtRun(t, wt, wtSessionWorktree, "", "doctor", "--json")
		reported := false
		for _, check := range decode[doctorPayload](t, r).Checks {
			if check.Name == "loop state" && check.State == stateProblem && strings.Contains(check.Detail, "US-003") {
				reported = true
			}
		}
		if !reported {
			t.Errorf("doctor in the worktree does not report the main checkout's broken US-003 record:\n%s", r.stdout)
		}
	})
}

func TestAC6LogInTheMainCheckoutListsAStoryCommittedInAWorktreeAndWhoHoldsIt(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "US-001"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, wt, wtSessionWorktree)
	reach(t, wt, model.GateRetro)

	for _, dir := range []string{main, wt} {
		r := wtMust(t, dir, "", "", "log", "--all", "--json")
		var entry map[string]any
		var logged struct {
			Entries []map[string]any `json:"entries"`
		}
		if err := json.Unmarshal([]byte(r.stdout), &logged); err != nil {
			t.Fatal(err)
		}
		for _, e := range logged.Entries {
			if e["story"] == "US-001" {
				entry = e
			}
		}
		if entry == nil {
			t.Errorf("sdlc log in %s does not list US-001, committed in the worktree", dir)
			continue
		}
		if !wtMentions(entry, wt) {
			t.Errorf("sdlc log --json in %s does not name %s as holding US-001: %v", dir, wt, entry)
		}
		if out := wtMust(t, dir, "", "", "log", "--all").stdout; !wtNames(out, wt) {
			t.Errorf("sdlc log in %s does not name the checkout holding US-001:\n%s", dir, out)
		}
	}
}
