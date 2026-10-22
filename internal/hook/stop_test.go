package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
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
		"hook_event_name": "Stop", "cwd": root, "stop_hook_active": alreadySentBack, "session_id": working,
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
	for _, want := range []string{"analysis", "sdlc escalate"} {
		if !strings.Contains(r.Reason, want) {
			t.Errorf("the reason does not say %q: %s", want, r.Reason)
		}
	}
	// Not `sdlc stop`: ending the iteration part-way turns every rule off, and
	// the hook refuses it from the session this reason is read by.
	if strings.Contains(r.Reason, "sdlc stop") {
		t.Errorf("the reason sends the session to end the iteration: %s", r.Reason)
	}
	if again := stop(t, root, true, noEnv); again.Decision != "" {
		t.Error("a stop that was already sent back was held again, which holds the session in a loop")
	}
}

// The stop guard holds the session working the story. Any other session in the
// repository ends its turn when it likes.
func TestOnlyTheSessionWorkingTheStoryIsSentBack(t *testing.T) {
	root := storyUnderWay(t)
	write(t, root, ".sdlc/state/session", "session-a\n")
	stopIn := func(session string) stopOutcome {
		t.Helper()
		event, err := json.Marshal(map[string]any{
			"hook_event_name": "Stop", "cwd": root, "session_id": session,
		})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if code := Run([]string{"Stop"}, bytes.NewReader(event), &out, io.Discard, noEnv); code != 0 {
			t.Fatalf("exit %d", code)
		}
		var r stopOutcome
		if err := json.Unmarshal(out.Bytes(), &r); err != nil {
			t.Fatalf("stdout is not JSON: %v (%q)", err, out.String())
		}
		return r
	}
	if r := stopIn("session-b"); r.Decision != "" {
		t.Errorf("another session was sent back while a story was under way: %s", r.Reason)
	}
	if r := stopIn("session-a"); r.Decision != "block" {
		t.Error("the session working the story stopped mid-story without a word")
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

// A count below zero is not one any stop wrote. Read as it was, the count grew
// from there and never reached the limit, and a story that kept stopping was
// never handed to anybody.
func TestACountBelowZeroStartsAgain(t *testing.T) {
	root := storyUnderWay(t)
	stop(t, root, false, noEnv)
	path := filepath.Join(root, ".sdlc", "state", "stop-blocks.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var count map[string]any
	if err := json.Unmarshal(raw, &count); err != nil {
		t.Fatal(err)
	}
	count["blocks"] = -1
	if raw, err = json.Marshal(count); err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/state/stop-blocks.json", string(raw))

	for i := 1; i <= 3; i++ {
		if r := stop(t, root, false, noEnv); r.Decision != "block" {
			t.Fatalf("stop %d after the count was spoiled went through before the limit", i)
		}
	}
	if r := stop(t, root, false, noEnv); !strings.Contains(r.SystemMessage, "sdlc approve A-1") {
		t.Errorf("a story whose count was below zero was not handed to a person: %+v", r)
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

// stopWhileLocked stops a story that has already been sent back to the limit,
// while another command holds the lock and does what during does, and returns
// the stop guard's answer once that command lets go.
func stopWhileLocked(t *testing.T, during func(root string)) (string, stopOutcome) {
	t.Helper()
	root := storyUnderWay(t)
	for range 3 {
		stop(t, root, false, noEnv)
	}

	g, err := store.Lock(root, "review add")
	if err != nil {
		t.Fatal(err)
	}
	event, err := json.Marshal(map[string]any{"hook_event_name": "Stop", "cwd": root, "session_id": working})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan []byte, 1)
	go func() {
		var out bytes.Buffer
		Run([]string{"Stop"}, bytes.NewReader(event), &out, io.Discard, noEnv)
		done <- out.Bytes()
	}()
	time.Sleep(300 * time.Millisecond)
	during(root)
	g.Release()

	var r stopOutcome
	if err := json.Unmarshal(<-done, &r); err != nil {
		t.Fatal(err)
	}
	return root, r
}

// The count is read once the lock is held. Read before it, a stop that arrived
// while a reviewer was recording its verdict handed the story to a person the
// moment after the story moved.
func TestAStopThatWaitedOnProgressBeingRecordedIsNotHandedOver(t *testing.T) {
	root, r := stopWhileLocked(t, func(root string) {
		recordGates(t, root, model.GateDoR, model.GateAnalysis)
	})
	if r.Decision != "block" {
		t.Errorf("a stop that waited on progress being recorded was not sent back: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Error("a story was handed to a person just after progress was recorded on it")
	}
}

// Nor is one that waited on the iteration being ended: by the time it can act,
// no story is being worked on.
func TestAStopThatWaitedOnTheIterationEndingHandsNothingOver(t *testing.T) {
	root, r := stopWhileLocked(t, func(root string) {
		if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
			t.Error(err)
		}
	})
	if r.Decision != "" || r.SystemMessage != "" {
		t.Errorf("a stop after the iteration ended was acted on: %+v", r)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "A-1", model.RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	var record model.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	if _, ok := record.PendingEscalation(); ok {
		t.Error("a story whose iteration had ended was handed to a person")
	}
}

// The stop guard's own limit comes from loop.max_stop_blocks, but a
// configuration that will not read is not a reason to stop enforcing it: the
// default limit still applies.
func TestAStopWithABrokenConfigurationStillUsesTheDefaultLimit(t *testing.T) {
	root := storyUnderWay(t)
	write(t, root, ".sdlc/config.json", "{not json")
	if r := stop(t, root, false, noEnv); r.Decision != "block" {
		t.Errorf("a broken configuration turned the stop guard off instead of falling back "+
			"to the default limit: %+v", r)
	}
}

// A story that has been started but has not been through Gate 1 yet has no
// gate record for the stop guard to measure progress from. That is a file
// that has simply not been written yet, not a reason to hold the session.
func TestAStopBeforeAnyGateRecordExistsFailsOpen(t *testing.T) {
	root := loopProject(t)
	if r := stop(t, root, false, noEnv); r.Decision != "" {
		t.Errorf("a stop before any gate record existed was held: %+v", r)
	}
}

// Saving the count is not the point of the stop guard. A count that cannot be
// written must not turn a session stopped mid-story into one held by an error
// nobody sent back.
func TestAStopThatCannotSaveItsCountFailsOpen(t *testing.T) {
	root := storyUnderWay(t)
	if err := os.MkdirAll(filepath.Join(root, ".sdlc", "state", "stop-blocks.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := stop(t, root, false, noEnv); r.Decision != "" {
		t.Errorf("a stop guard that could not save its count was held: %+v", r)
	}
}

// The stop guard takes the project's lock before reading anything. A lock it
// cannot take at all -- its cache directory blocked by something that is not
// a directory -- is not a reason to hold the session either.
func TestAStopWaitingOnALockItCannotTakeFailsOpen(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the cache directory is found differently on Windows")
	}
	root := storyUnderWay(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	cacheBase := filepath.Join(home, "cache")
	if runtime.GOOS == "darwin" {
		cacheBase = filepath.Join(home, "Library", "Caches")
	} else {
		t.Setenv("XDG_CACHE_HOME", cacheBase)
	}
	if err := os.MkdirAll(cacheBase, 0o755); err != nil {
		t.Fatal(err)
	}
	// A plain file named "sdlc" blocks the lock directory the guard needs to
	// create beneath it.
	if err := os.WriteFile(filepath.Join(cacheBase, "sdlc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if r := stop(t, root, false, noEnv); r.Decision != "" {
		t.Errorf("a stop guard that could not take its lock was held: %+v", r)
	}
}

// A story that has passed every gate but has not had its iteration ended --
// nobody has run `sdlc stop` yet -- is told to end it, not sent to work a
// gate that no longer exists.
func TestAStopAfterEveryGateHasPassedSaysToEndTheIteration(t *testing.T) {
	root := storyUnderWay(t)
	recordGates(t, root, model.Gates...)

	r := stop(t, root, false, noEnv)
	if r.Decision != "block" {
		t.Fatal("a stop after every gate passed, with the iteration still open, went through without a word")
	}
	if !strings.Contains(r.Reason, "sdlc stop") {
		t.Errorf("the reason does not say to end the iteration: %s", r.Reason)
	}
	if strings.Contains(r.Reason, "sdlc escalate") {
		t.Errorf("a story with every gate passed was sent to escalate instead of ending: %s", r.Reason)
	}
}

// Escalating is not guaranteed: the story might no longer be in the backlog by
// the time the loop tries to hand it to a person. That failure must not be
// mistaken for the session being held.
func TestAStoryThatCannotBeEscalatedFailsOpenWithAWarning(t *testing.T) {
	root := storyUnderWay(t)
	for range 3 {
		stop(t, root, false, noEnv)
	}
	write(t, root, "user_stories.json", `{"stories":[]}`+"\n")

	r := stop(t, root, false, noEnv)
	if r.Decision != "" {
		t.Errorf("a stop that could not be escalated was held instead of failing open: %+v", r)
	}
	if !strings.Contains(r.SystemMessage, "could not be handed") {
		t.Errorf("no warning that the hand-over failed: %q", r.SystemMessage)
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Error("the iteration ended even though the hand-over failed")
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

// ---------------------------------------------------------------- US-085
//
// A background subagent that is still running is not the session stopping.
// Claude Code lists what is in flight in `background_tasks` on the Stop event;
// only an entry whose `type` is exactly "subagent" holds the count, for at most
// thirty minutes from the first stop let through at the story's recorded
// progress. What the tests below bind, and the implementation has to provide:
//
//	decideStop(raw, getenv, warn, now func() time.Time) turnReply
//	    the clock as a fourth parameter, as gitBudget takes its own
//	subagentWait
//	    the bound, a time.Duration constant of this package
//	"waiting_since" in .sdlc/state/stop-blocks.json
//	    when the first stop was let through, as RFC 3339 text; no key until
//	    a stop has been let through
//	a debug line whose message holds cannotTell
//	    for a stop whose task list is absent or will not read
//
// Everything else is read from the reply and from the count file.

// cannotTell is what the one debug line says when the hook has no task list it
// can read.
const cannotTell = "cannot tell whether a subagent is running"

// stopClock is the moment the first stop of a test arrives. The guard is given
// its time, so the bound is tested by moving this and not by waiting.
var stopClock = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

// Text an entry of the task list carries that the hook has no business
// reading: the model wrote it. None of it may reach a log, a file or a reply.
const (
	leakID          = "TASK-ID-THAT-MUST-NOT-BE-KEPT"
	leakDescription = "DESCRIPTION-THAT-MUST-NOT-BE-KEPT"
	leakCommand     = "COMMAND-THAT-MUST-NOT-BE-KEPT"
	leakPrompt      = "PROMPT-THAT-MUST-NOT-BE-KEPT"
	leakAgentType   = "AGENT-TYPE-THAT-MUST-NOT-BE-KEPT"
)

var leaks = []string{leakID, leakDescription, leakCommand, leakPrompt, leakAgentType}

// subagent is one entry of the task list for a subagent, as the hooks reference
// describes it.
func subagent(id string) string {
	return `{"id":"` + id + `","type":"subagent","status":"running","description":"` + leakDescription +
		`","agent_type":"` + leakAgentType + `","prompt":"` + leakPrompt + `"}`
}

// shellTask is an entry for a shell command left running, which is not a
// subagent however much of it says so.
const shellTask = `{"id":"` + leakID + `","type":"shell","status":"running","description":"` + leakDescription +
	`","command":"` + leakCommand + `"}`

func tasks(entries ...string) string { return "[" + strings.Join(entries, ",") + "]" }

// noTaskList is a Stop event with no `background_tasks` at all, which is what
// every release before the field sends.
const noTaskList = ""

// stopEvent is the JSON of a Stop. list is the raw value of `background_tasks`,
// written as it is given so that a value of the wrong type can be sent.
func stopEvent(t *testing.T, root, session, list string, alreadySentBack bool) []byte {
	t.Helper()
	event := map[string]any{
		"hook_event_name": "Stop", "cwd": root, "stop_hook_active": alreadySentBack, "session_id": session,
	}
	if list != noTaskList {
		if !json.Valid([]byte(list)) {
			t.Fatalf("the test's own task list is not JSON: %s", list)
		}
		event["background_tasks"] = json.RawMessage(list)
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// guard puts one Stop to the stop guard at the time given.
func guard(t *testing.T, raw []byte, getenv func(string) string, now time.Time) stopOutcome {
	t.Helper()
	reply := decideStop(raw, getenv, func(string) {}, func() time.Time { return now })
	if !reply.Continue {
		t.Fatal("the stop guard ended the session outright")
	}
	return stopOutcome(reply)
}

// stopAt is a stop of the session working the story, with the task list given,
// after has passed since stopClock.
func stopAt(t *testing.T, root, list string, after time.Duration) stopOutcome {
	t.Helper()
	return guard(t, stopEvent(t, root, working, list, false), noEnv, stopClock.Add(after))
}

// waited holds a reply to the ordinary allow: nothing sent back, nothing said.
func waited(t *testing.T, r stopOutcome, what string) {
	t.Helper()
	if r.Decision != "" || r.Reason != "" || r.SystemMessage != "" {
		t.Fatalf("%s: the stop was not let through with the ordinary allow: %+v", what, r)
	}
}

// sentBack holds a reply to a stop counted as the nth in a row, with the limit
// the guard has always named.
func sentBack(t *testing.T, r stopOutcome, n int, what string) {
	t.Helper()
	if r.Decision != "block" {
		t.Fatalf("%s: the stop was not counted and sent back: %+v", what, r)
	}
	want := fmt.Sprintf("after 3 stops in a row with nothing recorded the loop hands it to a person itself (this was %d).", n)
	if !strings.HasSuffix(r.Reason, want) {
		t.Fatalf("%s: the reason does not end with %q:\n%s", what, want, r.Reason)
	}
	if r.SystemMessage != "" {
		t.Errorf("%s: a stop sent back also said %q", what, r.SystemMessage)
	}
}

// handedOver holds a reply, and the story, to the hand-over as it has always
// been: the fourth stop, the same words, a loop_stalled question on the record.
func handedOver(t *testing.T, root string, r stopOutcome, what string) {
	t.Helper()
	if r.Decision != "" || r.Reason != "" {
		t.Fatalf("%s: the stop past the limit was held: %+v", what, r)
	}
	want := "sdlc: A-1 stopped 4 times in a row with nothing recorded, so it has been handed to a person " +
		"and the iteration has ended. `sdlc status` shows the question; answer it with `/sdlc:approve A-1`, or `sdlc approve A-1`."
	if r.SystemMessage != want {
		t.Errorf("%s: the hand-over says\n%q\nwant\n%q", what, r.SystemMessage, want)
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "active")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s: the iteration is still running on a story handed to a person", what)
	}
	e, ok := pendingQuestion(t, root)
	question := "the session stopped 4 times in a row without recording anything on the story. Read " +
		".sdlc/stories/A-1/gate-record.json for where it got to, and answer with `/sdlc:approve A-1`, or `sdlc approve A-1` to let the loop carry on."
	if !ok || e.Type != "loop_stalled" || e.Message != question {
		t.Errorf("%s: the record holds %+v, want a loop_stalled question saying\n%q", what, e, question)
	}
	if backlog, _ := os.ReadFile(filepath.Join(root, "user_stories.json")); !strings.Contains(string(backlog), "awaiting_human") {
		t.Errorf("%s: the story is not waiting for a person:\n%s", what, backlog)
	}
}

func pendingQuestion(t *testing.T, root string) (model.Escalation, bool) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "A-1", model.RecordFile))
	if err != nil {
		t.Fatal(err)
	}
	var record model.Record
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	return record.PendingEscalation()
}

// stillUnderWay holds the story to being worked on, with nobody asked anything.
func stillUnderWay(t *testing.T, root, what string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
		t.Errorf("%s: the iteration has ended", what)
	}
	if e, ok := pendingQuestion(t, root); ok {
		t.Errorf("%s: the story was handed to a person: %+v", what, e)
	}
}

// keptCount is the count file read strictly: a key this does not name fails the
// test, because the format gains one value and no other.
type keptCount struct {
	Story        string  `json:"story"`
	Blocks       int     `json:"blocks"`
	Progress     string  `json:"progress"`
	WaitingSince *string `json:"waiting_since"`
}

const countFile = ".sdlc/state/stop-blocks.json"

func countBytes(t *testing.T, root string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(countFile)))
	if err != nil {
		t.Fatalf("the count file: %v", err)
	}
	return raw
}

func countOf(t *testing.T, root string) keptCount {
	t.Helper()
	raw := countBytes(t, root)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c keptCount
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("the count file is not in the format it is documented to have: %v\n%s", err, raw)
	}
	return c
}

// sinceOf is when the count file says the wait began.
func sinceOf(t *testing.T, root string) time.Time {
	t.Helper()
	c := countOf(t, root)
	if c.WaitingSince == nil {
		t.Fatalf("the count file does not say when the wait began:\n%s", countBytes(t, root))
	}
	at, err := time.Parse(time.RFC3339, *c.WaitingSince)
	if err != nil {
		t.Fatalf("the count file's waiting_since is not RFC 3339: %v", err)
	}
	return at
}

// noWaitKept holds the count file to the format it had before the wait existed.
func noWaitKept(t *testing.T, root, what string) {
	t.Helper()
	if c := countOf(t, root); c.WaitingSince != nil {
		t.Errorf("%s: the count file gained a waiting_since of %q with no stop let through", what, *c.WaitingSince)
	}
}

// progressOf is the story's record by content, as the guard measures it.
func progressOf(t *testing.T, root string) string {
	t.Helper()
	progress, err := store.New(&config.Project{Root: root, Config: config.Default()}).RecordProgress("A-1")
	if err != nil {
		t.Fatal(err)
	}
	return progress
}

// keepCount writes a count file as an earlier stop, or an earlier release, left
// it. since is the raw JSON of waiting_since, and "" leaves the key out.
func keepCount(t *testing.T, root, story string, blocks int, progress, since string) {
	t.Helper()
	body := fmt.Sprintf(`{"story":%q,"blocks":%d,"progress":%q`, story, blocks, progress)
	if since != "" {
		body += `,"waiting_since":` + since
	}
	body += "}\n"
	if !json.Valid([]byte(body)) {
		t.Fatalf("the test's own count file is not JSON: %s", body)
	}
	write(t, root, countFile, body)
}

// stamp is a time as the count file holds one.
func stamp(at time.Time) string { return `"` + at.UTC().Format(time.RFC3339) + `"` }

// debugLog collects what the guard logs from here on, at debug level, until the
// test ends or asks again.
func debugLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	before := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(before) })
	return &buf
}

func linesHolding(log, what string) []string {
	var lines []string
	for line := range strings.SplitSeq(log, "\n") {
		if strings.Contains(line, what) {
			lines = append(lines, line)
		}
	}
	return lines
}

// oneLineSaysItCannotTell holds a log to AC-3: one line, at debug level, for
// the stop and not for each entry.
func oneLineSaysItCannotTell(t *testing.T, log, what string) {
	t.Helper()
	lines := linesHolding(log, cannotTell)
	if len(lines) != 1 {
		t.Fatalf("%s: %d debug lines say the hook %s, want exactly one:\n%s", what, len(lines), cannotTell, log)
	}
	if !strings.Contains(lines[0], "level=DEBUG") {
		t.Errorf("%s: the line is not at debug level: %s", what, lines[0])
	}
}

// keepsNothingOfTheEntries looks for the entries' own text in the log and in
// every file under .sdlc.
func keepsNothingOfTheEntries(t *testing.T, root, log, what string) {
	t.Helper()
	for _, leak := range leaks {
		if strings.Contains(log, leak) {
			t.Errorf("%s: the debug log holds %s:\n%s", what, leak, log)
		}
	}
	err := filepath.WalkDir(filepath.Join(root, ".sdlc"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, leak := range leaks {
			if bytes.Contains(raw, []byte(leak)) {
				t.Errorf("%s: %s holds %s", what, path, leak)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func saysNothingOfTheEntries(t *testing.T, r stopOutcome, what string) {
	t.Helper()
	for _, leak := range leaks {
		if strings.Contains(r.Reason, leak) || strings.Contains(r.SystemMessage, leak) {
			t.Errorf("%s: the reply holds %s: %+v", what, leak, r)
		}
	}
}

// ---------------------------------------------------------------- AC-1

// US-006 was handed to a person after three stops while its researcher was
// still running in the background: the session had nothing to record yet, and
// every one of its stops was counted.
func TestAC1AStopWhileASubagentIsListedIsLetThroughAndNotCounted(t *testing.T) {
	root := storyUnderWay(t)

	// More stops than the limit, each with another subagent beside other work.
	for i := range 5 {
		list := tasks(shellTask, subagent(fmt.Sprintf("agent-%d", i)))
		r := stopAt(t, root, list, time.Duration(i)*time.Minute)
		waited(t, r, fmt.Sprintf("stop %d with a subagent listed", i+1))
	}
	if c := countOf(t, root); c.Story != "A-1" || c.Blocks != 0 {
		t.Errorf("stops let through while a subagent ran were counted: %+v", c)
	}
	stillUnderWay(t, root, "after five stops with a subagent listed")

	// The first stop that is counted is the first: the others added nothing.
	sentBack(t, stopAt(t, root, tasks(), 6*time.Minute), 1, "the first stop after the subagent finished")
}

// "Waits" is the ordinary allow and nothing new in the protocol: Claude Code
// wakes the session when the subagent reports.
func TestAC1TheReplyThatLetsTheSessionWaitIsTheOrdinaryAllow(t *testing.T) {
	root := storyUnderWay(t)
	var out, errOut bytes.Buffer
	event := stopEvent(t, root, working, tasks(subagent("agent-1")), false)
	if code := Run([]string{"Stop"}, bytes.NewReader(event), &out, &errOut, noEnv); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.String() != "{\"continue\":true}\n" || errOut.Len() != 0 {
		t.Errorf("stdout %q and stderr %q, want exactly {\"continue\":true}, a newline and nothing else", out.String(), errOut.String())
	}
	if c := countOf(t, root); c.Blocks != 0 {
		t.Errorf("the stop was counted: %+v", c)
	}
}

// The list also holds shell commands, monitors, workflows and teammates, and
// each entry's label is the one thing in it the hook goes by: the documented
// string, exactly.
func TestAC1OnlyTheExactLabelSubagentHoldsTheCount(t *testing.T) {
	var fifty []string
	for i := range 50 {
		fifty = append(fifty, fmt.Sprintf(`{"id":"sh-%d","type":"shell","status":"running"}`, i))
	}
	for name, list := range map[string]string{
		"an entry with the label and nothing else":           `[{"type":"subagent"}]`,
		"an entry as the reference describes it":             tasks(subagent("agent-1")),
		"a subagent after a shell command":                   tasks(shellTask, subagent("agent-1")),
		"a subagent after fifty other entries":               tasks(append(fifty, subagent("agent-1"))...),
		"a subagent beside an entry whose label is a number": `[{"type":5},{"type":"subagent"}]`,
		"a subagent beside an entry with no label":           `[{"id":"x"},{"type":"subagent"}]`,
		"a subagent with fields nobody documented":           `[{"type":"subagent","new_field":{"deep":[1,2,{"a":null}]}}]`,
	} {
		t.Run("AC-1 holds the count: "+name, func(t *testing.T) {
			root := storyUnderWay(t)
			waited(t, stopAt(t, root, list, 0), name)
			if c := countOf(t, root); c.Blocks != 0 {
				t.Errorf("the stop was counted: %+v", c)
			}
		})
	}

	for name, list := range map[string]string{
		"an empty list":                 `[]`,
		"a shell command":               tasks(shellTask),
		"a monitor":                     `[{"id":"m","type":"monitor","status":"running"}]`,
		"a workflow":                    `[{"id":"w","type":"workflow","status":"running"}]`,
		"a teammate":                    `[{"id":"t","type":"teammate","status":"running"}]`,
		"every other kind at once":      `[{"type":"shell"},{"type":"monitor"},{"type":"workflow"},{"type":"teammate"}]`,
		"a capital letter":              `[{"type":"Subagent"}]`,
		"capitals":                      `[{"type":"SUBAGENT"}]`,
		"a space after":                 `[{"type":"subagent "}]`,
		"a space before":                `[{"type":" subagent"}]`,
		"a hyphen":                      `[{"type":"sub-agent"}]`,
		"the plural":                    `[{"type":"subagents"}]`,
		"a longer label":                `[{"type":"subagent_task"}]`,
		"the label agent":               `[{"type":"agent"}]`,
		"an empty label":                `[{"type":""}]`,
		"a label that is a number":      `[{"type":5}]`,
		"a label that is true":          `[{"type":true}]`,
		"a label that is null":          `[{"type":null}]`,
		"a label that is a list":        `[{"type":["subagent"]}]`,
		"a label that is an object":     `[{"type":{"subagent":true}}]`,
		"no label at all":               `[{"id":"a","status":"running","agent_type":"subagent"}]`,
		"the label under another key":   `[{"kind":"subagent","task_type":"subagent","type ":"subagent"}]`,
		"an entry that is null":         `[null]`,
		"an entry that is empty":        `[{}]`,
		"subagent said everywhere else": `[{"id":"subagent","type":"shell","status":"subagent","description":"subagent","agent_type":"subagent","command":"subagent"}]`,
	} {
		t.Run("AC-1 does not hold the count: "+name, func(t *testing.T) {
			root := storyUnderWay(t)
			sentBack(t, stopAt(t, root, list, 0), 1, name)
			if c := countOf(t, root); c.Blocks != 1 {
				t.Errorf("the count is %+v, want one stop", c)
			}
			noWaitKept(t, root, name)
		})
	}
}

// The reference lists no values for `status` and gives no release for the
// field, and the rest of an entry is text the model asked for. So neither is
// read: a subagent holds the count whatever they say, and whatever type they
// are.
func TestAC1AnEntrysStatusAndFreeTextAreNeverRead(t *testing.T) {
	for name, entry := range map[string]string{
		"a status of completed":          `{"type":"subagent","status":"completed"}`,
		"a status of failed":             `{"type":"subagent","status":"failed"}`,
		"an empty status":                `{"type":"subagent","status":""}`,
		"no status":                      `{"type":"subagent","id":"a"}`,
		"a status that is a number":      `{"type":"subagent","status":5}`,
		"a status that is an object":     `{"type":"subagent","status":{"state":"done"}}`,
		"a status that is null":          `{"type":"subagent","status":null}`,
		"a description that is a list":   `{"type":"subagent","description":["a","b"]}`,
		"a description that is a number": `{"type":"subagent","description":7}`,
		"an agent type that is a number": `{"type":"subagent","agent_type":7}`,
		"a command that is an object":    `{"type":"subagent","command":{"argv":["sleep","1"]}}`,
	} {
		t.Run("AC-1 "+name, func(t *testing.T) {
			root := storyUnderWay(t)
			waited(t, stopAt(t, root, tasks(entry), 0), name)
			if c := countOf(t, root); c.Blocks != 0 {
				t.Errorf("the stop was counted: %+v", c)
			}
		})
	}
}

// Each entry carries up to a thousand characters of description and, for a
// shell task, its command line. The contract stores a session id and a
// checkout's path and nothing else, so none of it is logged, written under
// .sdlc, or said back.
func TestAC1NothingFromTheEntriesIsLoggedStoredOrSaid(t *testing.T) {
	root := storyUnderWay(t)
	log := debugLog(t)
	busy := tasks(shellTask, subagent(leakID))
	other := tasks(shellTask)
	unreadable := tasks(subagent(leakID), `"`+leakDescription+`"`)

	replies := []stopOutcome{
		stopAt(t, root, busy, 0),
		stopAt(t, root, other, time.Minute),
		stopAt(t, root, unreadable, 2*time.Minute),
		stopAt(t, root, busy, 31*time.Minute),
		stopAt(t, root, other, 32*time.Minute),
	}
	waited(t, replies[0], "a subagent listed")
	sentBack(t, replies[1], 1, "no subagent listed")
	sentBack(t, replies[2], 2, "a list that will not read")
	sentBack(t, replies[3], 3, "a subagent listed after the wait ran out")
	handedOver(t, root, replies[4], "the fourth stop counted")
	for i, r := range replies {
		saysNothingOfTheEntries(t, r, fmt.Sprintf("reply %d", i+1))
	}
	keepsNothingOfTheEntries(t, root, log.String(), "after five stops")
}

// The waiting stop gains the session nothing but the wait: whoever sends a Stop
// event that lists a subagent, the count is where it was.
func TestAC1AStopLetThroughNeverLowersResetsOrDeletesTheCount(t *testing.T) {
	root := storyUnderWay(t)
	sentBack(t, stopAt(t, root, tasks(), 0), 1, "stop 1")
	sentBack(t, stopAt(t, root, tasks(), time.Minute), 2, "stop 2")
	before := countOf(t, root)
	noWaitKept(t, root, "after two counted stops")

	waited(t, stopAt(t, root, tasks(subagent("agent-1")), 2*time.Minute), "a subagent listed with two stops counted")
	after := countOf(t, root)
	if after.Story != before.Story || after.Blocks != 2 || after.Progress != before.Progress {
		t.Errorf("a stop let through changed the count from %+v to %+v", before, after)
	}
	// The one thing it may write down is when the wait began.
	if at := sinceOf(t, root); !at.Equal(stopClock.Add(2 * time.Minute)) {
		t.Errorf("the wait is recorded as beginning at %s, want the time of the first stop let through, %s",
			at, stopClock.Add(2*time.Minute))
	}

	// A second one changes nothing at all.
	kept := countBytes(t, root)
	waited(t, stopAt(t, root, tasks(subagent("agent-2")), 3*time.Minute), "a second stop let through")
	if again := countBytes(t, root); !bytes.Equal(kept, again) {
		t.Errorf("a second stop let through rewrote the count file:\n%s\nwas\n%s", again, kept)
	}

	sentBack(t, stopAt(t, root, tasks(), 4*time.Minute), 3, "the next stop with no subagent")
}

// With no count yet, the first stop let through still has to write down when
// the wait began, or the wait would have no end.
func TestAC1TheFirstStopLetThroughWritesDownWhenTheWaitBegan(t *testing.T) {
	root := storyUnderWay(t)
	waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "the story's first stop")
	c := countOf(t, root)
	if c.Story != "A-1" || c.Blocks != 0 || c.Progress != progressOf(t, root) {
		t.Errorf("the count file is %+v, want A-1 with no stops at the record's present progress", c)
	}
	if at := sinceOf(t, root); !at.Equal(stopClock) {
		t.Errorf("the wait is recorded as beginning at %s, want %s", at, stopClock)
	}
}

// A running subagent is not the session stopping, so a story whose count has
// already reached the limit is not handed over on the strength of one more
// stop made while the subagent is still at work.
func TestAC1AStoryAtTheLimitIsNotHandedOverWhileASubagentIsListed(t *testing.T) {
	root := storyUnderWay(t)
	for i := 1; i <= 3; i++ {
		sentBack(t, stopAt(t, root, tasks(), time.Duration(i)*time.Second), i, fmt.Sprintf("stop %d", i))
	}

	for i := range 3 {
		waited(t, stopAt(t, root, tasks(subagent("agent-1")), time.Minute+time.Duration(i)*time.Second),
			"a stop at the limit with a subagent listed")
	}
	stillUnderWay(t, root, "at the limit with a subagent listed")
	if c := countOf(t, root); c.Blocks != 3 {
		t.Errorf("the count at the limit is %+v after stops let through, want 3", c)
	}

	handedOver(t, root, stopAt(t, root, tasks(), 2*time.Minute), "the next stop with no subagent")
}

// ---------------------------------------------------------------- AC-2

// Once the subagent has reported, a stop with nothing recorded is what it
// always was, and the limit and the hand-over's words are the ones a person
// already knows.
func TestAC2OnceTheSubagentHasFinishedStopsAreCountedAndTheStoryIsHandedOverAsBefore(t *testing.T) {
	root := storyUnderWay(t)
	log := debugLog(t)
	waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "the subagent running")
	waited(t, stopAt(t, root, tasks(subagent("agent-1")), time.Minute), "the subagent still running")

	// Finished: the list is there, and no subagent is in it.
	for i, list := range []string{tasks(), tasks(shellTask), `[{"id":"m","type":"monitor","status":"running"}]`} {
		sentBack(t, stopAt(t, root, list, time.Duration(i+2)*time.Minute), i+1, "with the list "+list)
		if c := countOf(t, root); c.Blocks != i+1 {
			t.Errorf("the count is %+v after %d stops", c, i+1)
		}
	}
	stillUnderWay(t, root, "after three stops")
	handedOver(t, root, stopAt(t, root, tasks(), 5*time.Minute), "the fourth stop")

	// The hook could tell every time, so it never says it could not.
	if lines := linesHolding(log.String(), cannotTell); len(lines) != 0 {
		t.Errorf("a list that reads was logged as one that does not:\n%s", strings.Join(lines, "\n"))
	}

	if r := stopAt(t, root, tasks(), 6*time.Minute); r.Decision != "" {
		t.Error("a stop was held with no iteration running")
	}
}

// ---------------------------------------------------------------- AC-3

// unreadableLists is every way the hook is left unable to tell: no list, a
// list that is not one, and a list with an entry that is not an entry.
var unreadableLists = map[string]string{
	"no list":                               noTaskList,
	"null":                                  `null`,
	"a string":                              `"subagent"`,
	"a number":                              `5`,
	"true":                                  `true`,
	"one entry that is not in a list":       `{"type":"subagent"}`,
	"an empty object":                       `{}`,
	"a list of a string":                    `["subagent"]`,
	"a list of a number":                    `[5]`,
	"a list of true":                        `[true]`,
	"a list of a list":                      `[[{"type":"subagent"}]]`,
	"three entries that are not entries":    `["a","b","c"]`,
	"a subagent beside an unreadable entry": `[{"type":"subagent"},"x"]`,
	"an unreadable entry before a subagent": `[7,{"type":"subagent"}]`,
}

// A field of the wrong type in the payload makes the whole payload unreadable,
// and an unreadable payload is let through uncounted. Read that way, a list
// that will not read would be a way never to be counted. So it is read on its
// own, and what it cannot say counts the stop as it always was, with one line
// in the debug log for whoever asks why.
func TestAC3AStopTheHookCannotTellAboutIsCountedAndSaysSoOnceInTheDebugLog(t *testing.T) {
	for name, list := range unreadableLists {
		t.Run("AC-3 "+name, func(t *testing.T) {
			root := storyUnderWay(t)
			log := debugLog(t)

			r := stopAt(t, root, list, 0)
			sentBack(t, r, 1, name)
			if c := countOf(t, root); c.Story != "A-1" || c.Blocks != 1 {
				t.Errorf("the count is %+v, want one stop", c)
			}
			noWaitKept(t, root, name)
			oneLineSaysItCannotTell(t, log.String(), name)
			saysNothingOfTheEntries(t, r, name)
		})
	}
}

// One line for each stop, so the log of a story that was handed over shows a
// line for every stop that led there, and the hand-over is the one it always
// was. The wait having begun changes none of it.
func TestAC3StopsTheHookCannotTellAboutReachTheLimitAndHandTheStoryOverAsBefore(t *testing.T) {
	root := storyUnderWay(t)
	waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "a subagent listed")

	for i, list := range []string{noTaskList, `null`, `"a string"`} {
		log := debugLog(t)
		sentBack(t, stopAt(t, root, list, time.Duration(i+1)*time.Minute), i+1, "with the list "+list)
		oneLineSaysItCannotTell(t, log.String(), "with the list "+list)
	}
	log := debugLog(t)
	handedOver(t, root, stopAt(t, root, `[{"type":"subagent"},"x"]`, 4*time.Minute), "the fourth stop the hook cannot tell about")
	oneLineSaysItCannotTell(t, log.String(), "the stop that handed the story over")
}

// The line names no entry: a list that will not read is the one most likely to
// hold what nobody expected.
func TestAC3TheLineThatSaysTheHookCannotTellCarriesNothingFromTheList(t *testing.T) {
	root := storyUnderWay(t)
	log := debugLog(t)
	list := tasks(shellTask, subagent(leakID), `"`+leakPrompt+`"`)
	sentBack(t, stopAt(t, root, list, 0), 1, "a list with an entry that is not one")
	oneLineSaysItCannotTell(t, log.String(), "a list with an entry that is not one")
	keepsNothingOfTheEntries(t, root, log.String(), "a list with an entry that is not one")
}

// The list is read after every check the guard already made, so a session that
// is not working the story, a stop already sent back and a guard turned off are
// answered as they were: nothing counted, nothing written, nothing logged
// about a list nobody had a reason to read.
func TestAC3TheTaskListIsReadOnlyForAStopTheGuardWouldCount(t *testing.T) {
	for _, c := range []struct {
		name    string
		session string
		active  bool
		getenv  func(string) string
		prepare func(*testing.T, string)
	}{
		{name: "another session", session: "session-b", getenv: noEnv},
		{name: "a stop already sent back", session: working, active: true, getenv: noEnv},
		{name: "enforcement off", session: working, getenv: env(map[string]string{"SDLC_ENFORCE": "0"})},
		{name: "the guard turned off", session: working, getenv: noEnv, prepare: func(t *testing.T, root string) {
			t.Helper()
			write(t, root, ".sdlc/config.json", `{"version":1,"loop":{"max_stop_blocks":0}}`)
		}},
		{name: "no iteration running", session: working, getenv: noEnv, prepare: func(t *testing.T, root string) {
			t.Helper()
			if err := os.Remove(filepath.Join(root, ".sdlc", "state", "active")); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		for _, list := range []string{`"a string"`, tasks(subagent("agent-1")), tasks()} {
			t.Run("AC-3 "+c.name+" with the list "+list, func(t *testing.T) {
				root := storyUnderWay(t)
				if c.prepare != nil {
					c.prepare(t, root)
				}
				log := debugLog(t)
				r := guard(t, stopEvent(t, root, c.session, list, c.active), c.getenv, stopClock)
				waited(t, r, c.name)
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(countFile))); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("a stop the guard does not count wrote the count file: %v", err)
				}
				if lines := linesHolding(log.String(), cannotTell); len(lines) != 0 {
					t.Errorf("the list was read for a stop the guard does not count:\n%s", strings.Join(lines, "\n"))
				}
			})
		}
	}

	// What tells them apart from a stop the guard does count: the same event
	// from the session working the story, with nothing turned off.
	t.Run("AC-3 and the session working the story with a list that will not read", func(t *testing.T) {
		root := storyUnderWay(t)
		log := debugLog(t)
		r := guard(t, stopEvent(t, root, working, `"a string"`, false), noEnv, stopClock)
		sentBack(t, r, 1, "the session working the story")
		oneLineSaysItCannotTell(t, log.String(), "the session working the story")
	})
}

// ---------------------------------------------------------------- AC-4

// Whatever the list holds, what the hook prints is one JSON object and a
// newline, with the fields the protocol has and no other.
func TestAC4EveryReplyToAStopWithATaskListIsOneJSONObjectOfTheProtocol(t *testing.T) {
	type printed struct {
		Continue      bool   `json:"continue"`
		Decision      string `json:"decision"`
		Reason        string `json:"reason"`
		SystemMessage string `json:"systemMessage"`
	}
	reply := func(t *testing.T, root, list string) (string, printed) {
		t.Helper()
		var out, errOut bytes.Buffer
		if code := Run([]string{"Stop"}, bytes.NewReader(stopEvent(t, root, working, list, false)), &out, &errOut, noEnv); code != 0 {
			t.Fatalf("exit %d", code)
		}
		if errOut.Len() != 0 {
			t.Errorf("the hook wrote to stderr: %q", errOut.String())
		}
		stdout := out.String()
		if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
			t.Fatalf("the hook printed something other than one line: %q", stdout)
		}
		dec := json.NewDecoder(strings.NewReader(stdout))
		dec.DisallowUnknownFields()
		var p printed
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("stdout is not the JSON the protocol has: %v\n%q", err, stdout)
		}
		if !p.Continue {
			t.Fatalf("the stop guard ended the session outright: %q", stdout)
		}
		return stdout, p
	}

	t.Run("AC-4 a subagent listed", func(t *testing.T) {
		root := storyUnderWay(t)
		if stdout, _ := reply(t, root, tasks(shellTask, subagent("agent-1"))); stdout != "{\"continue\":true}\n" {
			t.Errorf("stdout is %q, want exactly {\"continue\":true} and a newline", stdout)
		}
	})
	t.Run("AC-4 no subagent listed", func(t *testing.T) {
		root := storyUnderWay(t)
		reply(t, root, tasks(subagent("agent-1")))
		if stdout, p := reply(t, root, tasks(shellTask)); p.Decision != "block" || !strings.Contains(p.Reason, "(this was 1)") {
			t.Errorf("the first stop after the subagent finished was not sent back as the first: %q", stdout)
		}
	})
	for name, list := range unreadableLists {
		t.Run("AC-4 a list the hook cannot read: "+name, func(t *testing.T) {
			root := storyUnderWay(t)
			log := debugLog(t)
			if stdout, p := reply(t, root, list); p.Decision != "block" || !strings.Contains(p.Reason, "(this was 1)") {
				t.Errorf("the stop was not counted and sent back: %q", stdout)
			}
			oneLineSaysItCannotTell(t, log.String(), name)
		})
	}
	// Run reads the machine's clock, so the wait that has run out is one the
	// count file is given: begun thirty-one minutes before now.
	t.Run("AC-4 the hand-over, with a subagent listed after the wait ran out", func(t *testing.T) {
		root := storyUnderWay(t)
		keepCount(t, root, "A-1", 3, progressOf(t, root), stamp(time.Now().Add(-31*time.Minute)))
		stdout, p := reply(t, root, tasks(subagent("agent-1")))
		if p.Decision != "" || !strings.Contains(p.SystemMessage, "handed to a person") {
			t.Errorf("the story was not handed over: %q", stdout)
		}
	})
}

// ---------------------------------------------------------------- AC-5 (derived, D2)

// A subagent that never ends, or a new one started each time the last ends,
// would hold the count for good. So the wait has an end: thirty minutes from
// the first stop let through at the story's recorded progress. At thirty
// minutes it has run out.
func TestAC5TheWaitEndsThirtyMinutesAfterTheFirstStopLetThrough(t *testing.T) {
	for _, c := range []struct {
		name    string
		after   time.Duration
		counted bool
	}{
		{"the same moment", 0, false},
		{"a second later", time.Second, false},
		{"fifteen minutes later", 15 * time.Minute, false},
		{"a second short of thirty minutes", 30*time.Minute - time.Second, false},
		{"thirty minutes exactly", 30 * time.Minute, true},
		{"a second past thirty minutes", 30*time.Minute + time.Second, true},
		{"an hour later", time.Hour, true},
		{"a day later", 24 * time.Hour, true},
	} {
		t.Run("AC-5 "+c.name, func(t *testing.T) {
			root := storyUnderWay(t)
			waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "the first stop")
			if at := sinceOf(t, root); !at.Equal(stopClock) {
				t.Fatalf("the wait is recorded as beginning at %s, want %s", at, stopClock)
			}

			r := stopAt(t, root, tasks(subagent("agent-1")), c.after)
			if !c.counted {
				waited(t, r, c.name)
				if got := countOf(t, root); got.Blocks != 0 {
					t.Errorf("the stop was counted: %+v", got)
				}
				return
			}
			sentBack(t, r, 1, c.name)
			if got := countOf(t, root); got.Blocks != 1 {
				t.Errorf("the count is %+v, want one stop", got)
			}
		})
	}
}

// A wait that began in a count file already on disk is measured the same way:
// this is how a wait is carried from one stop, and one process, to the next.
func TestAC5AWaitTheCountFileAlreadyHoldsIsMeasuredFromItsTime(t *testing.T) {
	for _, c := range []struct {
		name    string
		began   time.Duration
		counted bool
	}{
		{"begun a second ago", -time.Second, false},
		{"begun a second short of thirty minutes ago", -30*time.Minute + time.Second, false},
		{"begun thirty minutes ago", -30 * time.Minute, true},
		{"begun thirty-one minutes ago", -31 * time.Minute, true},
	} {
		t.Run("AC-5 "+c.name, func(t *testing.T) {
			root := storyUnderWay(t)
			keepCount(t, root, "A-1", 2, progressOf(t, root), stamp(stopClock.Add(c.began)))
			r := stopAt(t, root, tasks(subagent("agent-1")), 0)
			if c.counted {
				sentBack(t, r, 3, c.name)
				return
			}
			waited(t, r, c.name)
			if got := countOf(t, root); got.Blocks != 2 {
				t.Errorf("the count is %+v, want the two stops it held", got)
			}
			if at := sinceOf(t, root); !at.Equal(stopClock.Add(c.began)) {
				t.Errorf("a stop let through moved the start of the wait to %s", at)
			}
		})
	}
}

// Past the bound a listed subagent holds nothing: the stops are counted and the
// story handed over exactly as if no list had been sent, and the hook does not
// say it could not tell, because it could.
func TestAC5OnceTheWaitHasRunOutStopsAreCountedAndTheStoryIsHandedOverAsBefore(t *testing.T) {
	root := storyUnderWay(t)
	waited(t, stopAt(t, root, tasks(subagent("agent-0")), 0), "the first stop")

	log := debugLog(t)
	for i := 1; i <= 3; i++ {
		list := tasks(subagent(fmt.Sprintf("agent-%d", i)))
		sentBack(t, stopAt(t, root, list, 30*time.Minute+time.Duration(i)*time.Minute), i,
			fmt.Sprintf("stop %d after the wait ran out, with a subagent listed", i))
	}
	stillUnderWay(t, root, "after three stops counted")
	handedOver(t, root, stopAt(t, root, tasks(subagent("agent-4")), 40*time.Minute), "the fourth stop after the wait ran out")
	if lines := linesHolding(log.String(), cannotTell); len(lines) != 0 {
		t.Errorf("a list that reads was logged as one that does not:\n%s", strings.Join(lines, "\n"))
	}
}

// Why a story was, or was not, handed over has to be answerable from the debug
// log: a stop let through for a subagent, and one counted in spite of one, each
// leave a line that says a subagent was the reason.
func TestAC5TheDebugLogSaysWhenASubagentHeldTheCountAndWhenTheWaitHadRunOut(t *testing.T) {
	root := storyUnderWay(t)

	log := debugLog(t)
	waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "the first stop")
	if lines := linesHolding(log.String(), "subagent"); len(lines) == 0 {
		t.Errorf("nothing in the debug log says a subagent held the count:\n%s", log.String())
	}

	log = debugLog(t)
	sentBack(t, stopAt(t, root, tasks(subagent("agent-1")), 31*time.Minute), 1, "after the wait ran out")
	if lines := linesHolding(log.String(), "subagent"); len(lines) == 0 {
		t.Errorf("nothing in the debug log says the stop was counted with a subagent listed:\n%s", log.String())
	}
	keepsNothingOfTheEntries(t, root, log.String(), "the line for a stop counted with a subagent listed")
}

// A session that starts a new subagent each time the last ends, or simply stops
// again, must not start the thirty minutes again: that is the unbounded wait by
// another route. Nor does a stop that was counted in between.
func TestAC5NeitherANewStopNorANewSubagentNorACountedStopRestartsTheWait(t *testing.T) {
	root := storyUnderWay(t)
	waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "the first stop")

	sentBack(t, stopAt(t, root, tasks(), 5*time.Minute), 1, "a stop with no subagent, five minutes in")

	waited(t, stopAt(t, root, tasks(subagent("agent-2")), 10*time.Minute), "a new subagent, ten minutes in")
	if at := sinceOf(t, root); !at.Equal(stopClock) {
		t.Errorf("a new subagent moved the start of the wait to %s", at)
	}
	waited(t, stopAt(t, root, tasks(subagent("agent-3"), subagent("agent-4")), 20*time.Minute), "two new subagents, twenty minutes in")
	waited(t, stopAt(t, root, tasks(subagent("agent-5")), 30*time.Minute-time.Second), "a second short of thirty minutes")
	if c := countOf(t, root); c.Blocks != 1 {
		t.Errorf("the count is %+v, want the one stop counted", c)
	}
	if at := sinceOf(t, root); !at.Equal(stopClock) {
		t.Errorf("the start of the wait moved to %s", at)
	}

	// Thirty minutes from the first stop let through, not from any later one.
	sentBack(t, stopAt(t, root, tasks(subagent("agent-6")), 30*time.Minute), 2, "thirty minutes from the first stop")
	sentBack(t, stopAt(t, root, tasks(subagent("agent-7")), 31*time.Minute), 3, "a subagent never seen before, after the wait ran out")
	handedOver(t, root, stopAt(t, root, tasks(subagent("agent-8")), 32*time.Minute), "the next stop")
}

// Recording anything on the story is the one thing that starts the wait again,
// as it is the one thing that starts the count again: a session that is getting
// somewhere may wait for its next subagent too.
func TestAC5OnlyRecordedProgressStartsTheWaitAgain(t *testing.T) {
	root := storyUnderWay(t)
	waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "the first stop")
	sentBack(t, stopAt(t, root, tasks(subagent("agent-1")), 31*time.Minute), 1, "after the wait ran out")
	old := countOf(t, root).Progress

	recordGates(t, root, model.GateDoR, model.GateAnalysis)
	waited(t, stopAt(t, root, tasks(subagent("agent-2")), 32*time.Minute), "the first stop after progress was recorded")
	c := countOf(t, root)
	if c.Blocks != 0 || c.Progress == old || c.Progress != progressOf(t, root) {
		t.Errorf("the count is %+v, want none at the record's new progress", c)
	}
	if at := sinceOf(t, root); !at.Equal(stopClock.Add(32 * time.Minute)) {
		t.Errorf("the wait is recorded as beginning at %s, want the first stop after the progress, %s",
			at, stopClock.Add(32*time.Minute))
	}

	waited(t, stopAt(t, root, tasks(subagent("agent-2")), 62*time.Minute-time.Second), "a second short of thirty minutes after the progress")
	sentBack(t, stopAt(t, root, tasks(subagent("agent-2")), 62*time.Minute), 1, "thirty minutes after the progress")
}

// The stored time belongs to the count it was written with. Where the count is
// replaced -- another story's, or this story's before something was recorded on
// it -- the time goes with it, whatever it says.
func TestAC5TheStoredTimeFallsAwayWhenTheStoryOrItsProgressDiffers(t *testing.T) {
	for _, c := range []struct {
		name            string
		story, progress string
		since           string
	}{
		{"another story's, long run out", "B-9", "", stamp(stopClock.Add(-31 * time.Minute))},
		{"another story's, still running", "B-9", "", stamp(stopClock.Add(-29 * time.Minute))},
		{"this story's before progress was recorded, long run out", "A-1", "an-earlier-record", stamp(stopClock.Add(-31 * time.Minute))},
		{"this story's before progress was recorded, still running", "A-1", "an-earlier-record", stamp(stopClock.Add(-29 * time.Minute))},
		{"this story's before progress was recorded, in the future", "A-1", "an-earlier-record", stamp(stopClock.Add(time.Hour))},
		{"this story's before progress was recorded, unreadable", "A-1", "an-earlier-record", `"yesterday"`},
	} {
		t.Run("AC-5 "+c.name, func(t *testing.T) {
			root := storyUnderWay(t)
			progress := c.progress
			if progress == "" {
				progress = progressOf(t, root)
			}
			keepCount(t, root, c.story, 2, progress, c.since)

			waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "the first stop at this progress")
			got := countOf(t, root)
			if got.Story != "A-1" || got.Blocks != 0 || got.Progress != progressOf(t, root) {
				t.Errorf("the count is %+v, want a new one for A-1 at the record's progress", got)
			}
			if at := sinceOf(t, root); !at.Equal(stopClock) {
				t.Errorf("the wait is recorded as beginning at %s, want this stop's time, %s", at, stopClock)
			}
			// Measured from this stop, so a time carried over would end it early.
			waited(t, stopAt(t, root, tasks(subagent("agent-1")), 29*time.Minute), "twenty-nine minutes later")
			sentBack(t, stopAt(t, root, tasks(subagent("agent-1")), 30*time.Minute), 1, "thirty minutes later")
		})
	}
}

// A count below zero once never reached the limit. The stored time gets the
// same care: one that will not read, is ahead of the clock, or is far behind it
// is not the start of a wait, and the stop is counted. A time ahead of the
// clock is the one that would otherwise stretch the wait past thirty minutes.
// And none of them starts a new wait: only recorded progress does that.
func TestAC5AStoredTimeThatCannotBeTheStartOfAWaitCountsTheStopUntilTheStoryIsHandedOver(t *testing.T) {
	for name, since := range map[string]string{
		"words":                           `"yesterday"`,
		"a time of day":                   `"12:00"`,
		"a date":                          `"2026-10-08"`,
		"a date and a time with a space":  `"2026-10-08 11:50:00"`,
		"seconds since 1970 as text":      `"1791460200"`,
		"a space":                         `" "`,
		"a second ahead of the clock":     stamp(stopClock.Add(time.Second)),
		"an hour ahead of the clock":      stamp(stopClock.Add(time.Hour)),
		"the year 9999":                   `"9999-12-31T23:59:59Z"`,
		"thirty minutes behind the clock": stamp(stopClock.Add(-30 * time.Minute)),
		"a year behind the clock":         stamp(stopClock.Add(-365 * 24 * time.Hour)),
		"1970":                            `"1970-01-01T00:00:00Z"`,
		"the year 1":                      `"0001-01-01T00:00:00Z"`,
	} {
		t.Run("AC-5 "+name, func(t *testing.T) {
			root := storyUnderWay(t)
			keepCount(t, root, "A-1", 0, progressOf(t, root), since)
			// Every stop at the same moment, so that a time ahead of the clock
			// stays ahead of it.
			for i := 1; i <= 3; i++ {
				list := tasks(subagent(fmt.Sprintf("agent-%d", i)))
				sentBack(t, stopAt(t, root, list, 0), i,
					fmt.Sprintf("stop %d with a subagent listed and a stored time of %s", i, since))
			}
			handedOver(t, root, stopAt(t, root, tasks(subagent("agent-4")), 0), "the fourth stop")
		})
	}

	// What tells them apart from a wait that is running: the same count file
	// with a time a second behind the clock lets the same stop through.
	t.Run("AC-5 and a time that can be one holds the count", func(t *testing.T) {
		root := storyUnderWay(t)
		keepCount(t, root, "A-1", 0, progressOf(t, root), stamp(stopClock.Add(-time.Second)))
		waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "a wait begun a second ago")
	})
}

// A count file written before the wait existed has no time in it. That is a
// wait that has not begun, not one that ran out: the count it holds stays, and
// the first stop let through starts the thirty minutes.
func TestAC5ACountFileFromBeforeTheWaitExistedHasNoWaitBegun(t *testing.T) {
	root := storyUnderWay(t)
	keepCount(t, root, "A-1", 2, progressOf(t, root), "")

	waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "the first stop with a subagent listed")
	if c := countOf(t, root); c.Blocks != 2 {
		t.Errorf("the count is %+v, want the two stops the file held", c)
	}
	if at := sinceOf(t, root); !at.Equal(stopClock) {
		t.Errorf("the wait is recorded as beginning at %s, want %s", at, stopClock)
	}
	sentBack(t, stopAt(t, root, tasks(), time.Minute), 3, "the next stop with no subagent")
}

// And a project that never sees a subagent keeps the file it has always had: a
// stop that is counted with no wait begun writes no time.
func TestAC5ACountedStopWithNoWaitBegunWritesTheCountFileAsBefore(t *testing.T) {
	root := storyUnderWay(t)
	for i, list := range []string{noTaskList, tasks(), tasks(shellTask)} {
		sentBack(t, stopAt(t, root, list, time.Duration(i)*time.Minute), i+1, "with the list "+list)
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(countBytes(t, root), &keys); err != nil {
			t.Fatal(err)
		}
		var names []string
		for k := range keys {
			names = append(names, k)
		}
		slices.Sort(names)
		if !slices.Equal(names, []string{"blocks", "progress", "story"}) {
			t.Errorf("the count file holds the keys %v, want blocks, progress and story as before", names)
		}
	}
}

// Writing down when the wait began is not the point of the guard either. A
// count file that cannot be written does not turn a session that was to be let
// through into one sent back, and it breaks nothing.
func TestAC5AWaitThatCannotBeWrittenDownStillLetsTheStopThrough(t *testing.T) {
	root := storyUnderWay(t)
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(countFile)), 0o755); err != nil {
		t.Fatal(err)
	}
	waited(t, stopAt(t, root, tasks(subagent("agent-1")), 0), "a wait that could not be written down")
	stillUnderWay(t, root, "a wait that could not be written down")
}

// The bound is a fixed thirty minutes, and docs/enforcement.md is where a
// person finds it. The page states the number the code uses.
func TestAC5TheEnforcementPageStatesTheWaitTheCodeUses(t *testing.T) {
	if subagentWait != 30*time.Minute {
		t.Errorf("the wait is %s, and the story fixes it at thirty minutes", subagentWait)
	}
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "enforcement.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(raw), "\n## Stopping mid-story\n")
	if !found {
		t.Fatal("docs/enforcement.md has no \"Stopping mid-story\" section")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	// The page is wrapped, so a phrase can be broken across two lines.
	section = strings.Join(strings.Fields(section), " ")

	for _, want := range []string{
		fmt.Sprintf("%d minutes", int(subagentWait/time.Minute)),
		"subagent",
		"`background_tasks`",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("the \"Stopping mid-story\" section of docs/enforcement.md never says %q", want)
		}
	}
}
