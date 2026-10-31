package cli

// US-112, the human advocate's Gate 7 notes 1 to 3: in --all --json a story
// still under way has no first-pass verdict yet; an unreadable trace in
// --all says how to find out why; and the two wrong forms of `sdlc trace`
// each say which mistake was made.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// AC-19, AC-20, note 1: first_pass is null while end is null; a script that
// read `true` took it for a verdict.
func TestAC19AC20AStoryUnderWayHasNoFirstPassInTraceAllJSON(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-2", "T-5", "T-7")
	walkedStory("T-2", 5*time.Hour, "").save(t, root)
	walkedStory("T-5", 5*time.Hour, model.GateCodeReview).save(t, root)
	newGateRecord("T-7").pass(model.GateDoR, time.Minute).pass(model.GateAnalysis, 2*time.Minute).save(t, root)
	var doc struct {
		Stories []map[string]json.RawMessage `json:"stories"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, "trace", "--all", "--json").stdout), &doc); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range doc.Stories {
		var id string
		_ = json.Unmarshal(s["story"], &id)
		got[id] = string(s["first_pass"])
	}
	for id, want := range map[string]string{"T-2": "true", "T-5": "false", "T-7": "null"} {
		if got[id] != want {
			t.Errorf("AC-19 %s's first_pass = %q, want %s", id, got[id], want)
		}
	}
}

// AC-19, note 2: the line for a trace --all cannot read names the command
// that says why, and the row is still shown.
func TestAC19AnUnreadableTraceInTraceAllSaysWhichCommandTellsWhy(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-8")
	walkedStory("T-8", 2*time.Hour, "").save(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".sdlc", "stories", "T-8", "trace.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := run(t, "trace", "--all")
	if r.code != 0 || !strings.Contains(r.stdout, "T-8") {
		t.Fatalf("AC-19 --all with one unreadable trace exited %d without T-8's row:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, "T-8") || !strings.Contains(r.stderr, `sdlc trace T-8`) {
		t.Errorf("AC-19 the note on T-8's unreadable trace does not say that `sdlc trace T-8` tells why:\n%s", r.stderr)
	}
}

// AC-18, AC-19, note 3: neither form and both forms are refused with
// SDLC-E0034 and nothing on stdout, and each says which of the two it was.
func TestAC18AC19NeitherFormAndBothFormsOfTraceSayWhichMistakeWasMade(t *testing.T) {
	t.Parallel()
	tracedStory(t)
	neither, both := run(t, "trace"), run(t, "trace", "T-1", "--all")
	for _, r := range []result{neither, both} {
		if r.code == 0 || r.stdout != "" || !strings.Contains(r.stderr, "SDLC-E0034") {
			t.Fatalf("AC-18 a wrong form exited %d with stdout %q:\n%s", r.code, r.stdout, r.stderr)
		}
	}
	if neither.stderr == both.stderr {
		t.Errorf("AC-18 `sdlc trace` and `sdlc trace T-1 --all` say the same thing:\n%s", neither.stderr)
	}
	if !strings.Contains(both.stderr, "both") || strings.Contains(neither.stderr, "both") {
		t.Errorf("AC-18 only the refusal of an id with --all should say both were given:\nneither: %s\nboth: %s", neither.stderr, both.stderr)
	}
}
