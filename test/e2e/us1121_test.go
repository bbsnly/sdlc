package e2e

// US-112.1: a bare `sdlc start` takes only a story marked ready, through the
// built binary in a real repository. internal/cli/us1121_test.go holds every
// case of the pick and of the refusal in-process.

import (
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// backlogStory is a story of a backlog made for a test.
type backlogStory struct {
	id, status string
	priority   int
}

// backlogOf commits a backlog of stories, each an id, a status and a
// priority, over the one `sdlc init` wrote.
func backlogOf(t *testing.T, root string, stories ...backlogStory) {
	t.Helper()
	list := make([]map[string]any, 0, len(stories))
	for _, s := range stories {
		list = append(list, map[string]any{
			"id": s.id, "title": "Story " + s.id, "status": s.status, "priority": s.priority,
			"acceptance_criteria": []map[string]string{{"id": "AC-1", "text": "an observable behaviour"}},
		})
	}
	writeJSON(t, filepath.Join(root, "user_stories.json"), map[string]any{"stories": list})
	commitEverything(t, root, "the backlog")
}

func TestAC5ASandboxBacklogWithATodoStoryOfALowerPriorityNumberStartsTheReadyOne(t *testing.T) {
	parallel(t)
	binary := build(t)
	root := project(t, binary)
	backlogOf(t, root, backlogStory{"T-1", "todo", 1}, backlogStory{"R-2", "ready", 5})

	var status struct {
		Next *struct {
			Story string `json:"story"`
		} `json:"next"`
	}
	if err := json.Unmarshal([]byte(runTool(t, binary, root, "status", "--json")), &status); err != nil {
		t.Fatal(err)
	}
	if status.Next == nil || status.Next.Story != "R-2" {
		t.Errorf("status names %+v as next up, want R-2", status.Next)
	}

	var started struct {
		Story    string `json:"story"`
		Resume   bool   `json:"resume"`
		NextGate string `json:"next_gate"`
	}
	if err := json.Unmarshal([]byte(runTool(t, binary, root, "start", "--json")), &started); err != nil {
		t.Fatal(err)
	}
	if started.Story != "R-2" || started.Resume || started.NextGate != "dor" {
		t.Errorf("a bare start took %+v, want R-2 begun afresh at Gate 1", started)
	}

	var after struct {
		Active  string         `json:"active"`
		Backlog map[string]int `json:"backlog"`
	}
	if err := json.Unmarshal([]byte(runTool(t, binary, root, "status", "--json")), &after); err != nil {
		t.Fatal(err)
	}
	if after.Active != "R-2" || after.Backlog["todo"] != 1 {
		t.Errorf("status after the start = %+v, want R-2 active and the todo story still todo", after)
	}
}

func TestAC2ABareStartWithOnlyTodoStoriesLeftRefusesThroughTheBinaryAndNamesThem(t *testing.T) {
	parallel(t)
	binary := build(t)
	root := project(t, binary)
	backlogOf(t, root, backlogStory{"T-3", "todo", 3}, backlogStory{"T-1", "todo", 1}, backlogStory{"T-2", "todo", 2}, backlogStory{"D-1", "done", 1})

	out, err := tool(t, binary, root, "start")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("a bare start with only todo stories left ended with %v, want exit status 1:\n%s", err, out)
	}
	if !strings.Contains(out, "SDLC-E0010") {
		t.Errorf("the refusal is not SDLC-E0010:\n%s", out)
	}
	if !regexp.MustCompile(`(?i)\b3\b[^.\n]{0,40}\btodo\b|\btodo\b[^.\n]{0,40}\b3\b`).MatchString(strings.NewReplacer("T-3", "ID", "T-1", "ID", "T-2", "ID").Replace(out)) {
		t.Errorf("the refusal does not say 3 todo stories were passed over:\n%s", out)
	}
	last := -1
	for _, id := range []string{"T-1", "T-2", "T-3"} {
		at := strings.Index(out, id)
		if at < 0 || at < last {
			t.Errorf("the refusal does not name %s after the one before it:\n%s", id, out)
		}
		last = at
	}
	if !regexp.MustCompile(`\bready\b`).MatchString(out) || !strings.Contains(out, "sdlc start") {
		t.Errorf("the refusal does not say to mark a story ready or to name one with sdlc start:\n%s", out)
	}

	if text := runTool(t, binary, root, "status"); strings.Contains(text, "next up") {
		t.Errorf("status offers a story as next up with only todo stories left:\n%s", text)
	}
}

// A guard, which passes before the change: naming a todo story still starts
// it, and Gate 1 decides whether it is ready.
func TestAC3GuardNamingATodoStoryStartsItThroughTheBinary(t *testing.T) {
	parallel(t)
	binary := build(t)
	root := project(t, binary)
	backlogOf(t, root, backlogStory{"R-1", "ready", 1}, backlogStory{"T-9", "todo", 9})

	var started struct {
		Story string `json:"story"`
	}
	if err := json.Unmarshal([]byte(runTool(t, binary, root, "start", "T-9", "--json")), &started); err != nil {
		t.Fatal(err)
	}
	if started.Story != "T-9" {
		t.Errorf("start T-9 took %q", started.Story)
	}
	runTool(t, binary, root, "gate", "dor", "pass", "--note", "the criteria are testable")
}
