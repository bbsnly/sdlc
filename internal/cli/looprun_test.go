package cli

import (
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// reach does the real work of every gate before the one named, so that a test
// about one gate starts from a story that genuinely got there.
//
// It is also the loop written out in one place: if a gate grows a requirement
// and this is not updated, every test that walks past it fails, which is the
// reminder that the requirement is real.
func reach(t *testing.T, root string, upTo model.Gate) {
	t.Helper()
	for _, gate := range model.Gates {
		if gate == upTo {
			return
		}
		satisfy(t, root, gate)
		mustRun(t, "gate", string(gate), "pass", "--note", "reached by the test helper")
	}
}

// satisfy does what one gate needs before it can be recorded.
func satisfy(t *testing.T, root string, gate model.Gate) {
	t.Helper()
	switch gate {
	case model.GateAnalysis:
		mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
		mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	case model.GateTestsFrozen:
		writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// AC-1\n")
		mustRunWith(t, "# Test plan\n", "artifact", "write", "test_plan")
		mustRun(t, "freeze")
	case model.GatePlan:
		mustRunWith(t, "# Plan\n", "artifact", "write", "plan")
	case model.GateVerification:
		mustRunWith(t, "# Verification\n", "artifact", "write", "verification")
	case model.GateRetro:
		mustRunWith(t, "# Retro\n", "artifact", "write", "retro")
	case model.GateCommit:
		commitEverything(t, root)
	}
	for _, r := range model.ReviewersFor(gate) {
		mustRunWith(t, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
	}
}

func commitEverything(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"add", "-A"},
		{"-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "story"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// A story going all the way through is the claim on the front page. Every gate
// here refuses something, and this is the shape of a run where nothing had to
// be refused.
func TestAStoryGoesAllTheWayThrough(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")

	reach(t, root, "")

	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	for _, gate := range model.Gates {
		if status.Gates[string(gate)] != "pass" {
			t.Errorf("%s = %q at the end of the loop", gate, status.Gates[string(gate)])
		}
	}
	if status.Freeze == nil || !status.Freeze.Intact {
		t.Errorf("freeze = %+v", status.Freeze)
	}
}

// The order is not a suggestion. A gate recorded out of order is how a story
// reaches the commit gate having skipped the one that would have stopped it.
func TestAGateCannotBeRecordedOutOfOrder(t *testing.T) {
	gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")

	r := run(t, "gate", "code_review", "pass", "--note", "looks fine")
	if r.code == 0 {
		t.Fatal("a late gate passed on a story that had done none of the earlier ones")
	}
	for _, want := range []string{"SDLC-E0031", "dor", "analysis"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}

	// A gate that failed is not a gate that passed.
	mustRun(t, "gate", "dor", "fail", "--note", "AC-2 is not testable")
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	if r := run(t, "gate", "analysis", "pass"); r.code == 0 {
		t.Error("a gate passed on the back of one that failed")
	}
}

// Recording a failure has to stay possible at every gate, including the ones
// with requirements. A loop that cannot say "this did not work" has nowhere to
// put the truth.
func TestAnyGateCanBeRecordedAsFailedWithNothingDone(t *testing.T) {
	gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")

	for _, gate := range model.Gates {
		mustRun(t, "gate", string(gate), "fail", "--note", "not this time")
	}
}

// finishedStory is a project whose only story has passed every gate: the state
// the loop is in when a session ends.
func finishedStory(t *testing.T) string {
	t.Helper()
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")
	reach(t, root, "")
	return root
}

// A story whose gates have all passed has to leave the backlog. Nothing else in
// the loop moves a story to done, and a real run found out what that costs: the
// story stayed in_progress after its retro, and `sdlc start` took it as the one
// already under way, so the next session reopened finished work.
func TestPassingTheLastGateTakesTheStoryOutOfTheBacklog(t *testing.T) {
	finishedStory(t)
	mustRun(t, "stop")

	story := onlyStory(t, decode[storyListPayload](t, mustRun(t, "story", "list", "--json")))
	if story.Status != string(model.StatusDone) {
		t.Errorf("%s = %q after every gate passed", story.ID, story.Status)
	}
	if story.Next {
		t.Errorf("`sdlc start` would pick %s up again", story.ID)
	}

	// The iteration is over, so this is the reading a new session gets.
	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if status.Next != nil {
		t.Errorf("status still offers %s as the next story", status.Next.Story)
	}
	if status.Backlog[string(model.StatusInProgress)] != 0 {
		t.Errorf("backlog = %v, with the finished story still counted as in progress", status.Backlog)
	}
}

// The gate that finishes a story says so, because the session that recorded it
// is the one that has to know the loop is over.
func TestTheLastGateSaysTheStoryIsDone(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")
	reach(t, root, model.GateRetro)
	satisfy(t, root, model.GateRetro)

	// The gate that finishes the story, not a repeat of it: the session that
	// records the last gate is the one that has to be told the loop is over.
	if out := mustRun(t, "gate", "retro", "pass").stdout; !strings.Contains(out, "is done") {
		t.Errorf("the gate that finished the story does not say so:\n%s", out)
	}
	// Recording it again is idempotent, and still reports the truth.
	again := decode[gatePayload](t, mustRun(t, "gate", "retro", "pass", "--json"))
	if !again.Done {
		t.Error("re-recording the last gate stopped reporting the story as finished")
	}
}

// Done is a reading of the record, not a one-way door. A gate recorded as
// failed afterwards -- a code review reopened, a verification redone -- puts the
// story back to work, or the rework would have nowhere to happen.
func TestAFailureAfterTheLastGateSendsTheStoryBackToWork(t *testing.T) {
	finishedStory(t)

	mustRun(t, "gate", "code_review", "fail", "--note", "AC-2 turned out to be untested")

	story := onlyStory(t, decode[storyListPayload](t, mustRun(t, "story", "list", "--json")))
	if story.Status != string(model.StatusInProgress) {
		t.Errorf("%s = %q after a gate failed on finished work", story.ID, story.Status)
	}
	// Back to in_progress is only half the claim: the rework has to have
	// somewhere to happen, and that means `sdlc start` picking the story up.
	if !story.Next {
		t.Errorf("`sdlc start` would not pick %s up to do the rework", story.ID)
	}
}

// onlyStory is the one story a scaffolded project has, and fails rather than
// passing vacuously when the backlog is not what the test thinks it is.
func onlyStory(t *testing.T, payload storyListPayload) storyRow {
	t.Helper()
	if len(payload.Stories) != 1 {
		t.Fatalf("the backlog has %d stories, not the scaffolded one", len(payload.Stories))
	}
	return payload.Stories[0]
}

// Ending an iteration and finishing a story are different things, and the
// sentence a session ends on is the only place a person sees which happened.
func TestStopSaysWhetherTheStoryIsFinished(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")
	reach(t, root, model.GatePlan)

	midway := decode[stopPayload](t, mustRun(t, "stop", "--json"))
	if midway.Done {
		t.Error("stopping part-way through reported the story as done")
	}

	// Carry on where the stop left off, rather than from the top: the tests
	// are already frozen, and the loop is meant to be resumable.
	mustRun(t, "start")
	for _, gate := range model.Gates[slices.Index(model.Gates, model.GatePlan):] {
		satisfy(t, root, gate)
		mustRun(t, "gate", string(gate), "pass", "--note", "carried on after the stop")
	}

	out := mustRun(t, "stop").stdout
	if !strings.Contains(out, "the story is done") {
		t.Errorf("stopping after every gate passed does not say the story is finished:\n%s", out)
	}
	if strings.Contains(out, "picks it up again") {
		t.Errorf("stopping a finished story offers to resume it:\n%s", out)
	}
}

// The two kinds of nothing are not the same. A backlog that is finished is the
// loop having worked; one that is blocked is a problem to go and look at.
func TestStatusSaysWhichKindOfNothingIsLeft(t *testing.T) {
	finishedStory(t)
	mustRun(t, "stop")

	out := mustRun(t, "status").stdout
	if !strings.Contains(out, "the one story in the backlog is finished") {
		t.Errorf("status does not say the backlog is finished:\n%s", out)
	}
	if !strings.Contains(out, "Add a story") {
		t.Errorf("status does not say what to do about a finished backlog:\n%s", out)
	}
	if strings.Contains(out, "holding each story back") {
		t.Errorf("status reports finished work as blocked:\n%s", out)
	}
}

// A gate is recorded against a story in the backlog. A mistyped --story used to
// start a record for a story that does not exist, which nothing would ever read
// again -- and now that the gate also settles the story's status, a refusal has
// to come before anything is written.
func TestAGateCannotBeRecordedAgainstAStoryThatIsNotInTheBacklog(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")

	r := run(t, "gate", "dor", "pass", "--story", "NOPE-9")
	if r.code == 0 {
		t.Fatal("a gate was recorded against a story the backlog has never heard of")
	}
	if !strings.Contains(r.stderr, "SDLC-E0009") {
		t.Errorf("the refusal does not name the story as missing:\n%s", r.stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "stories", "NOPE-9")); !os.IsNotExist(err) {
		t.Error("the refused gate left a record directory behind")
	}
}

// stop is the last thing a session runs, and the sentence it prints has to be
// true of the backlog afterwards. It settles the story itself rather than
// working the answer out a second time, so a backlog left behind by anything
// else -- a crash between two writes, an older build, a hand edit -- is
// repaired here instead of being contradicted.
func TestStopRepairsABacklogThatDisagreesWithTheRecord(t *testing.T) {
	root := finishedStory(t)
	setStatusOnDisk(t, root, model.StatusDone, model.StatusInProgress)

	if out := mustRun(t, "stop").stdout; !strings.Contains(out, "the story is done") {
		t.Errorf("stop does not say the story is done:\n%s", out)
	}
	story := onlyStory(t, decode[storyListPayload](t, mustRun(t, "story", "list", "--json")))
	if story.Status != string(model.StatusDone) {
		t.Errorf("stop said the story was done and left the backlog saying %q", story.Status)
	}
}

// A finished story is reopened by recording a gate as failed, not by starting
// it. Starting it would put work that is already done back in progress with
// nothing on the record saying why, which is the defect this all exists to fix.
func TestStartRefusesAStoryThatIsAlreadyFinished(t *testing.T) {
	finishedStory(t)

	// While the iteration is still open, there is nothing left to resume.
	open := run(t, "start")
	if open.code == 0 {
		t.Fatal("start resumed a story with no gate left to work")
	}
	if !strings.Contains(open.stderr, "SDLC-E0033") || !strings.Contains(open.stderr, "sdlc stop") {
		t.Errorf("the refusal does not point at ending the iteration:\n%s", open.stderr)
	}

	mustRun(t, "stop")
	closed := run(t, "start", "US-001")
	if closed.code == 0 {
		t.Fatal("start reopened a finished story by name")
	}
	for _, want := range []string{"SDLC-E0033", "is finished", "fail"} {
		if !strings.Contains(closed.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, closed.stderr)
		}
	}

	// And the refusal changed nothing.
	story := onlyStory(t, decode[storyListPayload](t, mustRun(t, "story", "list", "--json")))
	if story.Status != string(model.StatusDone) {
		t.Errorf("the refused start left the story as %q", story.Status)
	}
}

// setStatusOnDisk rewrites the backlog behind the tool's back, which is how a
// test reaches a state only a crash or an older build could produce.
func setStatusOnDisk(t *testing.T, root string, from, to model.Status) {
	t.Helper()
	path := filepath.Join(root, "user_stories.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	before := string(raw)
	after := strings.Replace(before, `"status": "`+string(from)+`"`, `"status": "`+string(to)+`"`, 1)
	if after == before {
		t.Fatalf("no story in the backlog had the status %q", from)
	}
	if err := os.WriteFile(path, []byte(after), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Between the last gate and `sdlc stop` a story is finished but not yet put
// down. Calling that "in progress" invites a session to carry on working
// something that has nothing left to work.
func TestStatusSaysAStoryIsFinishedBeforeItIsPutDown(t *testing.T) {
	finishedStory(t)

	out := mustRun(t, "status").stdout
	if !strings.Contains(out, "finished") {
		t.Errorf("status does not say the active story is finished:\n%s", out)
	}
	if strings.Contains(out, "in progress") {
		t.Errorf("status calls a story with no gate left in progress:\n%s", out)
	}
	if !strings.Contains(out, "`sdlc stop` to end the iteration") {
		t.Errorf("status does not say how to put a finished story down:\n%s", out)
	}
}

// addStory appends a second story to the scaffolded backlog, so that a test
// can be about what happens after the first one is finished.
func addStory(t *testing.T, root, id string) {
	t.Helper()
	path := filepath.Join(root, "user_stories.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var backlog struct {
		Schema  string           `json:"_schema"`
		Stories []map[string]any `json:"stories"`
	}
	if err := json.Unmarshal(raw, &backlog); err != nil {
		t.Fatal(err)
	}
	if len(backlog.Stories) == 0 {
		t.Fatal("the scaffolded backlog is empty")
	}
	next := maps.Clone(backlog.Stories[0])
	next["id"] = id
	next["title"] = "The story after the first one"
	next["status"] = string(model.StatusReady)
	next["priority"] = 2
	backlog.Stories = append(backlog.Stories, next)

	out, err := json.MarshalIndent(backlog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The loop is meant to run for as long as there are stories. It ran for
// exactly one: the test freeze is a single file for the whole project, and
// nothing ever lifted it, so the second story's Gate 3 refused with "already
// frozen" and named `sdlc unfreeze` -- the override for changing tests
// mid-story, recorded as a deviation -- as the way out. Every project hit
// this on its second story.
func TestASecondStoryCanFreezeItsOwnTests(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	addStory(t, root, "US-002")
	mustRun(t, "start")

	reach(t, root, "")
	mustRun(t, "stop")

	started := mustRun(t, "start")
	if !strings.Contains(started.stdout, "US-002") {
		t.Fatalf("the second story was not picked up: %s", started.stdout)
	}
	for _, gate := range []model.Gate{model.GateDoR, model.GateAnalysis} {
		satisfy(t, root, gate)
		mustRun(t, "gate", string(gate), "pass", "--note", "reached by the test")
	}

	writeFile(t, root, "internal/second_test.go", "package internal\n\n// AC-1\n")
	mustRunWith(t, "# Test plan\n", "artifact", "write", "test_plan")
	if r := run(t, "freeze"); r.code != 0 {
		t.Fatalf("the second story could not freeze its tests (exit %d):\n%s%s", r.code, r.stdout, r.stderr)
	}
	if got := lockOnDisk(t, root).Story; got != "US-002" {
		t.Errorf("the freeze names %q, want US-002", got)
	}
}

// Finishing a story lifts its freeze. Once the story is done its tests are on
// trunk and there is nothing left to protect, and leaving the freeze behind is
// what stopped the next story.
func TestFinishingAStoryLiftsItsFreeze(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")
	reach(t, root, "")
	mustRun(t, "stop")

	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "tests.lock")); !os.IsNotExist(err) {
		t.Errorf("the freeze outlived the story it was taken for: %v", err)
	}
}

// Stopping an unfinished story must not lift its freeze. `sdlc stop` is how a
// session ends mid-story, and the story is picked up again next time; a freeze
// that came off here would make `stop`, edit, `start` the way round every gate
// after Gate 3.
func TestStoppingAnUnfinishedStoryKeepsItsFreeze(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")
	reach(t, root, model.GatePlan)

	if lockOnDisk(t, root).Story == "" {
		t.Fatal("the tests were not frozen")
	}
	mustRun(t, "stop")
	if got := lockOnDisk(t, root).Story; got == "" {
		t.Error("stopping an unfinished story lifted its freeze")
	}
}
