package cli

// US-112.1: a bare `sdlc start` takes only a story marked ready. Every command
// here runs in-process, so that what the story changes in model and cli is
// measured by coverage; test/e2e/us1121_test.go drives the built binary.
//
// What the refusal says is held to what AC-2 names -- the code, the exit code,
// how many todo stories were passed over, which ones, and the two ways on --
// and to nothing of its wording, because a frozen sentence takes a person's
// unfreeze to reword.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// us1121 is a story of a backlog made for a test. A priority below zero is no
// priority at all.
type us1121 struct {
	id, status string
	priority   int
	deps       []string
	criteria   bool
}

const us1121NoPriority = -1

// us1121Title is what a story is called here. It is distinctive, so that a
// refusal that echoes a title is seen doing it: ids are what a refusal names.
func us1121Title(id string) string { return "Zebra crossing for " + id }

// us1121Write is a person writing the backlog by hand in root.
func us1121Write(t *testing.T, root string, stories ...us1121) {
	t.Helper()
	list := make([]map[string]any, 0, len(stories))
	for _, s := range stories {
		m := map[string]any{"id": s.id, "title": us1121Title(s.id), "status": s.status}
		if s.priority != us1121NoPriority {
			m["priority"] = s.priority
		}
		if len(s.deps) > 0 {
			m["depends_on"] = s.deps
		}
		if s.criteria {
			m["acceptance_criteria"] = []map[string]string{{"id": "AC-1", "type": "WHEN", "text": "WHEN x, it shall y."}}
		}
		list = append(list, m)
	}
	raw, err := json.MarshalIndent(map[string]any{"stories": list}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "user_stories.json", string(raw)+"\n")
}

// us1121Project is a repository with the loop set up, the stories written and
// the test's commands running in it.
func us1121Project(t *testing.T, stories ...us1121) string {
	t.Helper()
	root := gitProjectInitialised(t)
	us1121Write(t, root, stories...)
	return root
}

func us1121Ready(id string, priority int) us1121 {
	return us1121{id: id, status: "ready", priority: priority}
}
func us1121Todo(id string, priority int) us1121 {
	return us1121{id: id, status: "todo", priority: priority}
}

// us1121Refusal is what a refused bare start said, in both forms a reader has:
// the text on standard error, and the three fields of --json.
type us1121Refusal struct {
	form string
	code int
	text string
	id   string
}

func us1121Refused(t *testing.T, run func(args ...string) result) []us1121Refusal {
	t.Helper()
	text := run("start")
	machine := run("start", "--json")
	var payload errorPayload
	if err := json.Unmarshal([]byte(machine.stdout), &payload); err != nil {
		t.Fatalf("sdlc start --json did not answer with one JSON object (exit %d): %v\n%s\n%s",
			machine.code, err, machine.stdout, machine.stderr)
	}
	return []us1121Refusal{
		{"text", text.code, text.stderr, ""},
		{"json", machine.code, strings.Join([]string{payload.Error, payload.Why, payload.Fix}, "\n"), payload.Code},
	}
}

func us1121Run(t *testing.T) func(args ...string) result {
	t.Helper()
	return func(args ...string) result { return run(t, args...) }
}

// ----------------------------------------------------------------- ids

const us1121IDChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-"

// us1121Names is whether text names id as a word of its own: T-1 is not named
// by T-10, and a quotation mark or a comma ends it.
func us1121Names(text, id string) bool { return us1121Index(text, id) >= 0 }

func us1121Index(text, id string) int {
	for from := 0; ; {
		at := strings.Index(text[from:], id)
		if at < 0 {
			return -1
		}
		at += from
		end := at + len(id)
		before := at == 0 || !strings.ContainsRune(us1121IDChars, rune(text[at-1]))
		after := end == len(text) || !strings.ContainsRune(us1121IDChars, rune(text[end]))
		if before && after {
			return at
		}
		from = at + 1
	}
}

// us1121WithoutIDs is text with every id in ids replaced by a word with no
// digit in it, so that the digits left are counts.
func us1121WithoutIDs(text string, ids []string) string {
	for _, id := range slices.SortedFunc(slices.Values(ids), func(a, b string) int { return len(b) - len(a) }) {
		for {
			at := us1121Index(text, id)
			if at < 0 {
				break
			}
			text = text[:at] + "ID" + text[at+len(id):]
		}
	}
	return text
}

// us1121SaysCount is whether text says n of the todo stories were passed over:
// the number stands within a few words of "todo", whichever way round the
// sentence is put. A number that stands right before "todo" has to be n.
func us1121SaysCount(t *testing.T, text string, ids []string, n int) bool {
	t.Helper()
	bare := strings.ToLower(us1121WithoutIDs(text, ids))
	for _, m := range regexp.MustCompile(`\b(\d+)\s+(?:[a-z]+\s+){0,2}todo\b`).FindAllStringSubmatch(bare, -1) {
		if m[1] != strconv.Itoa(n) {
			t.Errorf("the message says %s todo stories were passed over, want %d:\n%s", m[1], n, text)
		}
	}
	near := regexp.MustCompile(fmt.Sprintf(`\b%[1]d\b[^.\n]{0,40}\btodo\b|\btodo\b[^.\n]{0,40}\b%[1]d\b`, n))
	return near.MatchString(bare)
}

// us1121TwoWays is whether text says what to do: mark a story ready, or name
// one with sdlc start.
func us1121TwoWays(text string) (ready, named bool) {
	return regexp.MustCompile(`\bready\b`).MatchString(text), strings.Contains(text, "sdlc start")
}

// ----------------------------------------------------------------- AC-1

func TestAC1ABareStartTakesAReadyStoryOverATodoStoryWithALowerPriorityNumber(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		stories []us1121
		want    string
	}{
		{"one of each", []us1121{us1121Todo("T-1", 1), us1121Ready("R-2", 5)}, "R-2"},
		{"other ids and numbers", []us1121{us1121Ready("W-30", 40), us1121Todo("A-1", 2), us1121Todo("A-2", us1121NoPriority)}, "W-30"},
		{"priority 0 is still the front of the ready ones", []us1121{
			us1121Todo("A-0", 0), us1121Ready("R-4", 3), us1121Ready("R-7", 0), us1121Ready("R-5", us1121NoPriority)}, "R-7"},
		{"ready ids that tie are read as numbers", []us1121{
			us1121Todo("A-0", 2), us1121Ready("R-10", 2), us1121Ready("R-9", 2)}, "R-9"},
		{"a ready story whose dependency is done", []us1121{
			{id: "D-1", status: "done", priority: 1}, us1121Todo("T-0", 0),
			{id: "R-2", status: "ready", priority: 9, deps: []string{"D-1"}}}, "R-2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			us1121Project(t, tc.stories...)
			r := run(t, "start", "--json")
			if r.code != 0 {
				t.Fatalf("a bare start refused although %s is ready (exit %d):\n%s%s", tc.want, r.code, r.stdout, r.stderr)
			}
			if got := decode[startPayload](t, r); got.Story != tc.want || got.Resume {
				t.Errorf("a bare start took %+v, want %s begun afresh", got, tc.want)
			}
			if strings.Contains(r.stderr, "SDLC-E0010") {
				t.Errorf("a bare start with a ready story to take still said SDLC-E0010:\n%s", r.stderr)
			}
		})
	}
}

// A ready story that waits on one that is not done is not startable, and the
// todo story it waits on is not a ready one: nothing starts.
func TestAC1AReadyStoryWaitingOnATodoStoryIsNotPickedAndNeitherIsTheTodoStory(t *testing.T) {
	t.Parallel()
	us1121Project(t,
		us1121Todo("T-1", 5),
		us1121{id: "R-2", status: "ready", priority: 1, deps: []string{"T-1"}})

	r := run(t, "start")
	if r.code == 0 {
		t.Fatalf("a bare start began a story:\n%s", r.stdout)
	}
	if !strings.Contains(r.stderr, "SDLC-E0010") {
		t.Errorf("the refusal is not SDLC-E0010:\n%s", r.stderr)
	}
	if active := decode[statusPayload](t, mustRun(t, "status", "--json")).Active; active != "" {
		t.Errorf("the refused start left %s running", active)
	}
}

// ----------------------------------------------------------------- AC-2

// us1121Distractors are in every backlog of the AC-2 table: none is ready, and
// none of them is a todo story that otherwise could start.
func us1121Distractors() []us1121 {
	return []us1121{
		{id: "D-1", status: "done", priority: 1},
		{id: "B-1", status: "blocked", priority: 2},
		{id: "X-1", status: "dropped", priority: 3},
		// Waits on a story that is not done, and has the front priority: counted,
		// it would head the list.
		{id: "TW", status: "todo", priority: 0, deps: []string{"B-1"}},
	}
}

func TestAC2ARefusalCountsTheTodoStoriesItPassedOverNamesUpToFiveAndSaysHowToGoOn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		todo  []us1121 // in the order the backlog lists them
		named []string // in the order the pick would offer them: priority, then id
		left  []string // passed over and counted, and not named
	}{
		{"one", []us1121{us1121Todo("S-4", 3)}, []string{"S-4"}, nil},
		{"three, listed out of order", []us1121{us1121Todo("S-3", 2), us1121Todo("S-10", 1), us1121Todo("S-2", 1)},
			[]string{"S-2", "S-10", "S-3"}, nil},
		{"exactly five, one of them waiting on a story that is done", []us1121{
			us1121Todo("P-5", 5), us1121Todo("P-3", 3), us1121Todo("P-2", 3), us1121Todo("P-4", 0),
			{id: "P-6", status: "todo", priority: 2, deps: []string{"D-1"}}},
			[]string{"P-4", "P-6", "P-2", "P-3", "P-5"}, nil},
		{"six, so one is counted and not named", []us1121{
			us1121Todo("Q-6", 6), us1121Todo("Q-1", 1), us1121Todo("Q-9", 1),
			us1121Todo("Q-3", us1121NoPriority), us1121Todo("Q-4", 0), us1121Todo("Q-12", 1)},
			[]string{"Q-4", "Q-1", "Q-9", "Q-12", "Q-6"}, []string{"Q-3"}},
		{"nine", []us1121{
			us1121Todo("N-1", 9), us1121Todo("N-2", 8), us1121Todo("N-3", 7), us1121Todo("N-4", 6), us1121Todo("N-5", 5),
			us1121Todo("N-6", 4), us1121Todo("N-7", 3), us1121Todo("N-8", 2), us1121Todo("N-9", 1)},
			[]string{"N-9", "N-8", "N-7", "N-6", "N-5"}, []string{"N-4", "N-3", "N-2", "N-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := us1121Project(t, append(us1121Distractors(), tc.todo...)...)
			before, err := os.ReadFile(filepath.Join(root, "user_stories.json"))
			if err != nil {
				t.Fatal(err)
			}
			every := []string{"D-1", "B-1", "X-1", "TW"}
			for _, s := range tc.todo {
				every = append(every, s.id)
			}
			count := len(tc.named) + len(tc.left)

			for _, refusal := range us1121Refused(t, us1121Run(t)) {
				if refusal.code != 1 {
					t.Errorf("%s: exit code %d, want 1", refusal.form, refusal.code)
				}
				if refusal.form == "json" && refusal.id != "SDLC-E0010" {
					t.Errorf("json: code %q, want SDLC-E0010", refusal.id)
				}
				if refusal.form == "text" && !strings.Contains(refusal.text, "SDLC-E0010") {
					t.Errorf("text: the refusal is not SDLC-E0010:\n%s", refusal.text)
				}
				if !us1121SaysCount(t, refusal.text, every, count) {
					t.Errorf("%s: the message does not say %d todo stories were passed over:\n%s", refusal.form, count, refusal.text)
				}
				last := -1
				for _, id := range tc.named {
					at := us1121Index(refusal.text, id)
					if at < 0 {
						t.Errorf("%s: the message does not name %s:\n%s", refusal.form, id, refusal.text)
						continue
					}
					if at < last {
						t.Errorf("%s: %s is named out of order, want priority and then id: %v\n%s", refusal.form, id, tc.named, refusal.text)
					}
					last = at
				}
				for _, id := range append([]string{"TW", "B-1", "D-1", "X-1"}, tc.left...) {
					if us1121Names(refusal.text, id) {
						t.Errorf("%s: the message names %s, which is not one of the first five todo stories that could start:\n%s",
							refusal.form, id, refusal.text)
					}
				}
				if strings.Contains(refusal.text, "Zebra") {
					t.Errorf("%s: the message echoes a story's title:\n%s", refusal.form, refusal.text)
				}
				ready, named := us1121TwoWays(refusal.text)
				if !ready || !named {
					t.Errorf("%s: the message does not say to mark a story ready or to name one with sdlc start:\n%s", refusal.form, refusal.text)
				}
			}

			after, err := os.ReadFile(filepath.Join(root, "user_stories.json"))
			if err != nil || string(after) != string(before) {
				t.Errorf("the refused starts changed the backlog (%v)", err)
			}
			if active := decode[statusPayload](t, mustRun(t, "status", "--json")).Active; active != "" {
				t.Errorf("the refused starts left %s running", active)
			}
		})
	}
}

// A ready story that cannot start leaves the todo ones to be passed over, and
// the todo story it waits on is one of them.
func TestAC2ARefusalCountsTheTodoStoryAReadyStoryWaitsOn(t *testing.T) {
	t.Parallel()
	us1121Project(t,
		us1121Todo("T-1", 5),
		us1121{id: "R-2", status: "ready", priority: 1, deps: []string{"T-1"}})
	ids := []string{"T-1", "R-2"}
	for _, refusal := range us1121Refused(t, us1121Run(t)) {
		if !us1121SaysCount(t, refusal.text, ids, 1) || !us1121Names(refusal.text, "T-1") {
			t.Errorf("%s: the message does not say one todo story, T-1, was passed over:\n%s", refusal.form, refusal.text)
		}
		if us1121Names(refusal.text, "R-2") {
			t.Errorf("%s: the message names R-2, which is ready and is not a todo story passed over:\n%s", refusal.form, refusal.text)
		}
	}
}

// Decision 1 of the person: the count and the ids leave out what another
// checkout holds, as the pick does.
func TestAC2TheCountAndTheIdsLeaveOutATodoStoryAnotherCheckoutHolds(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "H-1"}, wtStory{id: "T-2"}, wtStory{id: "T-3"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, main, wtSessionMain, "H-1")
	// The person puts the story the main checkout is on back to todo, and the
	// others with it.
	for _, id := range []string{"H-1", "T-2", "T-3"} {
		wtSetStatus(t, main, id, model.StatusTodo)
	}

	r := wtRun(t, wt, wtSessionWorktree, "", "start")
	if r.code != 1 || !strings.Contains(r.stderr, "SDLC-E0010") {
		t.Fatalf("start in the worktree exited %d, want a refusal under SDLC-E0010:\n%s%s", r.code, r.stdout, r.stderr)
	}
	ids := []string{"H-1", "T-2", "T-3"}
	if !us1121SaysCount(t, r.stderr, ids, 2) {
		t.Errorf("the message does not say 2 todo stories were passed over, T-2 and T-3 and not H-1:\n%s", r.stderr)
	}
	for _, id := range []string{"T-2", "T-3"} {
		if !us1121Names(r.stderr, id) {
			t.Errorf("the message does not name %s:\n%s", id, r.stderr)
		}
	}
}

// ----------------------------------------------------------------- guards of AC-2

// A guard, which passes before the change: where no todo story could start,
// the refusal is the one it was, and says nothing of passing any over.
func TestAC2GuardAStuckBacklogWithNoTodoStoryThatCouldStartIsRefusedAsBefore(t *testing.T) {
	t.Parallel()
	for name, stories := range map[string][]us1121{
		"ready waits on blocked": {
			{id: "B-1", status: "blocked", priority: 1}, {id: "D-2", status: "done", priority: 2},
			{id: "R-3", status: "ready", priority: 3, deps: []string{"B-1"}}},
		"the only todo story waits on a blocked one": {
			{id: "B-1", status: "blocked", priority: 1},
			{id: "T-4", status: "todo", priority: 2, deps: []string{"B-1"}}},
		"every story is finished": {
			{id: "D-1", status: "done", priority: 1}, {id: "D-2", status: "done", priority: 2}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			us1121Project(t, stories...)
			for _, refusal := range us1121Refused(t, us1121Run(t)) {
				if refusal.code != 1 {
					t.Errorf("%s: exit code %d, want 1", refusal.form, refusal.code)
				}
				if refusal.form == "json" && refusal.id != "SDLC-E0010" {
					t.Errorf("json: code %q, want SDLC-E0010", refusal.id)
				}
				if us1121Names(refusal.text, "T-4") {
					t.Errorf("%s: the message names a todo story that waits on a dependency:\n%s", refusal.form, refusal.text)
				}
				if strings.Contains(refusal.text, "sdlc start ") {
					t.Errorf("%s: the message sends the reader to name a story with nothing to name:\n%s", refusal.form, refusal.text)
				}
			}
		})
	}
}

// A guard: a todo story that only another checkout holds is not one the
// refusal passes over either.
func TestAC2GuardOnlyAHeldTodoStoryLeftIsTheRefusalItWasWithNoStoryToName(t *testing.T) {
	t.Parallel()
	main := wtRepository(t, wtStory{id: "H-1"})
	wt := wtWorktree(t, main, "wt")
	wtStarted(t, main, wtSessionMain, "H-1")
	wtSetStatus(t, main, "H-1", model.StatusTodo)

	r := wtRun(t, wt, wtSessionWorktree, "", "start")
	if r.code != 1 || !strings.Contains(r.stderr, "SDLC-E0010") {
		t.Fatalf("start in the worktree exited %d, want SDLC-E0010:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if strings.Contains(r.stderr, "sdlc start ") {
		t.Errorf("the message offers to name a story that another checkout holds:\n%s", r.stderr)
	}
}

// ----------------------------------------------------------------- AC-3

// Guards, which pass before the change: naming a todo story starts it, over a
// ready one with a better priority, and Gate 1 -- not start -- says whether it
// is ready.
func TestAC3GuardNamingATodoStoryStartsItAndGateOneDecidesWhetherItIsReady(t *testing.T) {
	t.Parallel()
	t.Run("AC-3 a todo story named over a ready one starts", func(t *testing.T) {
		t.Parallel()
		us1121Project(t, us1121Ready("R-1", 1), us1121Todo("T-9", 9))
		got := decode[startPayload](t, mustRun(t, "start", "T-9", "--json"))
		if got.Story != "T-9" || got.Resume || got.NextGate != "dor" {
			t.Errorf("start T-9 gave %+v, want T-9 begun afresh at Gate 1", got)
		}
	})
	t.Run("AC-3 a todo story with no criteria is refused by Gate 1", func(t *testing.T) {
		t.Parallel()
		us1121Project(t, us1121Todo("T-9", 9))
		mustRun(t, "start", "T-9")
		if r := run(t, "gate", "dor", "pass", "--note", "checked"); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0044") {
			t.Errorf("Gate 1 passed a story with no criteria, or refused it for another reason (exit %d):\n%s", r.code, r.stderr)
		}
	})
	t.Run("AC-3 a todo story with a criterion passes Gate 1", func(t *testing.T) {
		t.Parallel()
		us1121Project(t, us1121{id: "T-9", status: "todo", priority: 9, criteria: true})
		mustRun(t, "start", "T-9")
		if r := run(t, "gate", "dor", "pass", "--note", "checked"); r.code != 0 {
			t.Errorf("Gate 1 refused a todo story that has a criterion (exit %d):\n%s", r.code, r.stderr)
		}
	})
}

// ----------------------------------------------------------------- AC-4

// us1121Seen is what each command says is next, from one place.
type us1121Seen struct {
	status, list string // the story named next, or ""
	start        string // the story a bare start begins or resumes, or ""
	resume       bool
	refusal      string // the code a bare start refused with, or ""
}

// us1121Look asks status and story list first and a bare start last, because
// start moves the story it takes.
func us1121Look(t *testing.T, run func(args ...string) result) us1121Seen {
	t.Helper()
	var seen us1121Seen
	status := run("status", "--json")
	if status.code != 0 {
		t.Fatalf("sdlc status --json exited %d:\n%s", status.code, status.stderr)
	}
	var sp statusPayload
	if err := json.Unmarshal([]byte(status.stdout), &sp); err != nil {
		t.Fatalf("status --json: %v\n%s", err, status.stdout)
	}
	if sp.Next != nil {
		seen.status = sp.Next.Story
	}
	list := run("story", "list", "--json")
	if list.code != 0 {
		t.Fatalf("sdlc story list --json exited %d:\n%s", list.code, list.stderr)
	}
	var lp storyListPayload
	if err := json.Unmarshal([]byte(list.stdout), &lp); err != nil {
		t.Fatalf("story list --json: %v\n%s", err, list.stdout)
	}
	for _, row := range lp.Stories {
		if row.Next {
			if seen.list != "" {
				t.Errorf("story list marks both %s and %s as next", seen.list, row.ID)
			}
			seen.list = row.ID
		}
	}
	start := run("start", "--json")
	if start.code == 0 {
		sp := decode[startPayload](t, start)
		seen.start, seen.resume = sp.Story, sp.Resume
		return seen
	}
	var ep errorPayload
	if err := json.Unmarshal([]byte(start.stdout), &ep); err != nil {
		t.Fatalf("start --json refused without one JSON object: %v\n%s%s", err, start.stdout, start.stderr)
	}
	seen.refusal = ep.Code
	return seen
}

func TestAC4StatusAndStoryListNameWhatABareStartWouldPickOrNothingWhereItRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		stories []us1121
		next    string // "" where a bare start refuses
	}{
		{"an all-todo backlog", []us1121{us1121Todo("T-1", 1), us1121Todo("T-2", 2)}, ""},
		{"a mixed backlog", []us1121{us1121Todo("T-1", 1), us1121Ready("R-2", 5), us1121Todo("T-3", 0)}, "R-2"},
		{"a mixed backlog, ready first and then todo, with other numbers", []us1121{
			us1121Ready("R-8", 30), us1121Todo("R-1", 1), us1121Ready("R-7", 20)}, "R-7"},
		{"a ready story that waits on a todo one", []us1121{
			us1121Todo("T-1", 5), {id: "R-2", status: "ready", priority: 1, deps: []string{"T-1"}}}, ""},
		{"todo stories and ones that are done, blocked and dropped", []us1121{
			{id: "D-1", status: "done", priority: 1}, {id: "B-2", status: "blocked", priority: 2},
			us1121Todo("T-3", 3), {id: "X-4", status: "dropped", priority: 4}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			us1121Project(t, tc.stories...)
			seen := us1121Look(t, us1121Run(t))
			if seen.status != tc.next || seen.list != tc.next || seen.start != tc.next {
				t.Errorf("status names %q, story list marks %q and a bare start takes %q: all three want %q",
					seen.status, seen.list, seen.start, tc.next)
			}
			if tc.next == "" && seen.refusal != "SDLC-E0010" {
				t.Errorf("a bare start refused with %q, want SDLC-E0010", seen.refusal)
			}
		})
	}
}

// A guard, which passes before the change: a story under way is resumed first,
// whatever its status was before it started, and the three agree on it.
func TestAC4GuardAStoryUnderWayIsResumedFirstWhateverItsStatusWasAndStatusAndStoryListAgree(t *testing.T) {
	t.Parallel()
	us1121Project(t, us1121Ready("R-1", 1), us1121Todo("T-9", 9))
	mustRun(t, "start", "T-9")
	mustRun(t, "stop")

	seen := us1121Look(t, us1121Run(t))
	if seen.status != "T-9" || seen.list != "T-9" || seen.start != "T-9" || !seen.resume {
		t.Errorf("after T-9 was started by name and stopped: status %q, list %q, start %q (resume %v); want T-9 three times, resumed",
			seen.status, seen.list, seen.start, seen.resume)
	}
}

// A guard: a story that waits on a person is not offered as next up, and a
// bare start refuses it, so the two agree.
func TestAC4GuardAStoryAwaitingAPersonIsNeitherNextUpNorStartedByABareStart(t *testing.T) {
	t.Parallel()
	us1121Project(t, us1121Ready("R-1", 1), us1121Todo("T-2", 2))
	mustRun(t, "start", "R-1")
	mustRun(t, "escalate", "spec_unclear", "--message", "which greeting?")

	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if status.Next != nil {
		t.Errorf("status offers %+v as next up while R-1 waits on a person", status.Next)
	}
	if len(status.Waiting) != 1 || status.Waiting[0].Story != "R-1" {
		t.Errorf("status waiting = %+v, want R-1", status.Waiting)
	}
	if r := run(t, "start"); r.code == 0 || strings.Contains(r.stderr, "SDLC-E0010") {
		t.Errorf("a bare start with R-1 waiting on a person exited %d, want a refusal other than SDLC-E0010:\n%s", r.code, r.stderr)
	}
}

// What another checkout holds is not there to pick, and a todo story in front
// of the ready one is no more picked.
func TestAC4StatusAndStoryListLeaveOutWhatAnotherCheckoutHoldsAndAgreeWithABareStart(t *testing.T) {
	t.Parallel()
	t.Run("AC-4 the ready story is next where a todo one has the lower number", func(t *testing.T) {
		t.Parallel()
		main := wtRepository(t, wtStory{id: "H-1"}, wtStory{id: "T-2"}, wtStory{id: "R-3"})
		wt := wtWorktree(t, main, "wt")
		wtStarted(t, main, wtSessionMain, "H-1")
		wtSetStatus(t, main, "T-2", model.StatusTodo)

		seen := us1121Look(t, func(args ...string) result { return wtRun(t, wt, wtSessionWorktree, "", args...) })
		if seen.status != "R-3" || seen.list != "R-3" || seen.start != "R-3" {
			t.Errorf("status names %q, story list marks %q and a bare start takes %q: all three want R-3",
				seen.status, seen.list, seen.start)
		}
	})
	t.Run("AC-4 with only a todo story free nothing is next", func(t *testing.T) {
		t.Parallel()
		main := wtRepository(t, wtStory{id: "H-1"}, wtStory{id: "T-2"})
		wt := wtWorktree(t, main, "wt")
		wtStarted(t, main, wtSessionMain, "H-1")
		wtSetStatus(t, main, "T-2", model.StatusTodo)

		seen := us1121Look(t, func(args ...string) result { return wtRun(t, wt, wtSessionWorktree, "", args...) })
		if seen.status != "" || seen.list != "" || seen.start != "" || seen.refusal != "SDLC-E0010" {
			t.Errorf("status names %q, story list marks %q, a bare start took %q or refused with %q: want nothing and SDLC-E0010",
				seen.status, seen.list, seen.start, seen.refusal)
		}
	})
}

// Decision 2: with only todo stories left, status and story list name nothing
// as next and say what AC-2 says to do; in text, and not in a field of --json.
func TestAC4StatusAndStoryListWithOnlyTodoStoriesLeftNameNothingAndSayHowToGoOn(t *testing.T) {
	t.Parallel()
	us1121Project(t, us1121Todo("T-1", 1), us1121Todo("T-2", 2))

	// A paragraph that says both ways on: what a person does is one thought,
	// and the status counts or the table beside it are not it.
	says := func(out string) bool {
		for p := range strings.SplitSeq(strings.ReplaceAll(out, "\r\n", "\n"), "\n\n") {
			if ready, named := us1121TwoWays(p); ready && named {
				return true
			}
		}
		return false
	}

	status := mustRun(t, "status").stdout
	if strings.Contains(status, "next up") {
		t.Errorf("status offers a story as next up with only todo stories left:\n%s", status)
	}
	if !says(status) {
		t.Errorf("status does not say to mark a story ready or to name one with sdlc start:\n%s", status)
	}

	list := mustRun(t, "story", "list").stdout
	for line := range strings.Lines(list) {
		if strings.HasPrefix(line, "> ") {
			t.Errorf("story list marks a story as the one start would pick: %q\n%s", line, list)
		}
	}
	if !says(list) {
		t.Errorf("story list does not say to mark a story ready or to name one with sdlc start:\n%s", list)
	}

	// Nothing is added to the JSON: `next` is absent where start would refuse.
	raw := mustRun(t, "status", "--json").stdout
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["next"]; ok {
		t.Errorf("status --json has a next field with only todo stories left:\n%s", raw)
	}
	for _, row := range decode[storyListPayload](t, mustRun(t, "story", "list", "--json")).Stories {
		if row.Next {
			t.Errorf("story list --json marks %s as next", row.ID)
		}
	}
}

// With a ready story left both say, as they did, which one is next.
func TestAC4StatusAndStoryListNameTheReadyStoryInTextWhereATodoStoryHasTheLowerNumber(t *testing.T) {
	t.Parallel()
	us1121Project(t, us1121Todo("T-1", 1), us1121Ready("R-2", 5))

	status := mustRun(t, "status").stdout
	if !regexp.MustCompile(`next up\s+R-2\b`).MatchString(status) {
		t.Errorf("status does not name R-2 as next up:\n%s", status)
	}
	list := mustRun(t, "story", "list").stdout
	for line := range strings.Lines(list) {
		marked := strings.HasPrefix(line, "> ")
		switch {
		case strings.Contains(line, "R-2") && !marked:
			t.Errorf("story list does not mark R-2: %q", line)
		case strings.Contains(line, "T-1") && marked:
			t.Errorf("story list marks the todo T-1: %q", line)
		}
	}
}

// Decision 3: doctor stays ok and its words stay the same; an all-todo backlog
// has none runnable, as a backlog with every story blocked does.
func TestAC4DoctorSaysNoneRunnableAndStaysOKForABacklogOfTodoStories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		stories []us1121
		want    string
	}{
		{"all todo", []us1121{us1121Todo("T-1", 1), us1121Todo("T-2", 2)}, "(2 stories, none runnable)"},
		{"one todo", []us1121{us1121Todo("T-1", 1)}, "(1 story, none runnable)"},
		{"a ready story among them", []us1121{us1121Todo("T-1", 1), us1121Ready("R-2", 2)}, "(2 stories, one runnable)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			us1121Project(t, tc.stories...)
			var found bool
			for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
				if c.Name != checkBacklog {
					continue
				}
				found = true
				if c.State != stateOK || !strings.HasSuffix(c.Detail, tc.want) || c.Fix != "" {
					t.Errorf("the backlog check is %q %q (fix %q), want ok and a detail ending %q", c.State, c.Detail, c.Fix, tc.want)
				}
			}
			if !found {
				t.Error("doctor has no check of the backlog")
			}
		})
	}
}
