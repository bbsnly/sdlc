package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

type stopOutcome struct {
	Continue      bool   `json:"continue"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	SystemMessage string `json:"systemMessage"`
}

func stop(t *testing.T, root string, alreadySentBack bool, getenv func(string) string) stopOutcome {
	t.Helper()
	event, err := json.Marshal(map[string]any{
		"hook_event_name": "Stop", "cwd": root, "stop_hook_active": alreadySentBack,
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Run([]string{"Stop"}, bytes.NewReader(event), &out, io.Discard, getenv); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r stopOutcome
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, out.String())
	}
	if !r.Continue {
		t.Fatal("the stop guard ended the session outright")
	}
	return r
}

// storyUnderWay is a repository with an iteration running on A-1, which has
// passed Gate 1.
func storyUnderWay(t *testing.T) string {
	t.Helper()
	root := loopProject(t)
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	initRepo := exec.CommandContext(t.Context(), "git", "init", "--quiet")
	initRepo.Dir = root
	if out, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	recordGates(t, root, model.GateDoR)
	return root
}

func recordGates(t *testing.T, root string, gates ...model.Gate) {
	t.Helper()
	at := time.Now()
	record := model.NewRecord("A-1", at)
	for _, g := range gates {
		record.SetGate(g, model.GatePass, "", at)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))
}

// loop.max_stop_blocks was read by nothing, and nothing stopped a session
// ending its turn with a story half done and nobody told.
func TestAStopMidStoryIsSentBackWithWhatToDoInstead(t *testing.T) {
	root := storyUnderWay(t)

	r := stop(t, root, false, noEnv)
	if r.Decision != "block" {
		t.Fatal("a session stopped mid-story without a word")
	}
	for _, want := range []string{"analysis", "sdlc escalate", "sdlc stop"} {
		if !strings.Contains(r.Reason, want) {
			t.Errorf("the reason does not say %q: %s", want, r.Reason)
		}
	}
	if again := stop(t, root, true, noEnv); again.Decision != "" {
		t.Error("a stop that was already sent back was held again, which holds the session in a loop")
	}
}

// The session's project directory can be above the repository the story is in.
// The stop is still one made mid-story.
func TestAStopFromASessionOpenedAboveTheRepositoryIsStillSentBack(t *testing.T) {
	root := storyUnderWay(t)
	above := env(map[string]string{"CLAUDE_PROJECT_DIR": filepath.Dir(root)})
	if r := stop(t, root, false, above); r.Decision != "block" {
		t.Error("a session opened above the repository stopped mid-story without a word")
	}
}

func TestAStoryThatKeepsStoppingIsHandedToAPerson(t *testing.T) {
	root := storyUnderWay(t)
	for i := 1; i <= 3; i++ {
		if r := stop(t, root, false, noEnv); r.Decision != "block" {
			t.Fatalf("stop %d went through before the limit", i)
		}
	}

	r := stop(t, root, false, noEnv)
	if r.Decision != "" {
		t.Fatalf("the stop past the limit was held: %s", r.Reason)
	}
	if !strings.Contains(r.SystemMessage, "sdlc approve A-1") {
		t.Errorf("nobody is told who answers: %q", r.SystemMessage)
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "active")); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the iteration is still running on a story handed to a person")
	}
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "A-1", model.RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	var record model.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if e, ok := record.PendingEscalation(); !ok || e.Type != "loop_stalled" {
		t.Errorf("no loop_stalled escalation on the record: %+v", record.Escalations)
	}
	if backlog, _ := os.ReadFile(filepath.Join(root, "user_stories.json")); !strings.Contains(string(backlog), "awaiting_human") {
		t.Errorf("the story is not waiting for a person:\n%s", backlog)
	}

	if r := stop(t, root, false, noEnv); r.Decision != "" {
		t.Error("a stop was held with no iteration running")
	}
}

// The count is of stops with nothing done in between, so a session making
// progress is never handed over for it.
func TestRecordingAnythingStartsTheCountAgain(t *testing.T) {
	root := storyUnderWay(t)
	stop(t, root, false, noEnv)
	stop(t, root, false, noEnv)

	recordGates(t, root, model.GateDoR, model.GateAnalysis)
	for i := 1; i <= 3; i++ {
		if r := stop(t, root, false, noEnv); r.Decision != "block" {
			t.Fatalf("stop %d after progress was not sent back: %+v", i, r)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Error("a story that was making progress was handed to a person")
	}
}

func TestTheStopGuardCanBeTurnedOff(t *testing.T) {
	root := storyUnderWay(t)
	if r := stop(t, root, false, env(map[string]string{"SDLC_ENFORCE": "0"})); r.Decision != "" {
		t.Error("SDLC_ENFORCE=0 did not turn the stop guard off")
	}
	write(t, root, ".sdlc/config.json", `{"version":1,"loop":{"max_stop_blocks":0}}`)
	if r := stop(t, root, false, noEnv); r.Decision != "" || r.SystemMessage != "" {
		t.Errorf("max_stop_blocks 0 did not turn the stop guard off: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Error("with the guard off, a stop still handed the story to a person")
	}
}
