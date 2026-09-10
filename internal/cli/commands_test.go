package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// project makes an empty Git repository, changes into it, and returns its path.
// Every command resolves the project from the working directory, so this is how
// they are all driven.
func project(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_WORK_TREE", "")
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

type result struct {
	stdout string
	stderr string
	code   int
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	return runWith(t, "", args...)
}

// runWith is run with a document on standard input, which is how a gate's
// documents arrive.
func runWith(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := Execute(args, strings.NewReader(stdin), &out, &errb)
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

func mustRun(t *testing.T, args ...string) result {
	t.Helper()
	r := run(t, args...)
	if r.code != 0 {
		t.Fatalf("sdlc %s failed with %d:\n%s", strings.Join(args, " "), r.code, r.stderr)
	}
	return r
}

func mustRunWith(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	r := runWith(t, stdin, args...)
	if r.code != 0 {
		t.Fatalf("sdlc %s failed with %d:\n%s", strings.Join(args, " "), r.code, r.stderr)
	}
	return r
}

func decode[T any](t *testing.T, r result) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(r.stdout), &v); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, r.stdout)
	}
	return v
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// ------------------------------------------------------------------ errors

// The three fields and the code are the contract with the reader, in both
// forms of output.
func TestAFailureCarriesWhatWhyFixAndACode(t *testing.T) {
	project(t)

	r := run(t, "status")
	if r.code == 0 {
		t.Fatal("status succeeded in a project that was never set up")
	}
	for _, want := range []string{"not been set up", "why  ", "fix  ", "SDLC-E0002", "troubleshooting.md"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr is missing %q:\n%s", want, r.stderr)
		}
	}
	if r.stdout != "" {
		t.Errorf("an error wrote to stdout, which a skill parses as data: %q", r.stdout)
	}
}

func TestAFailureInJSONCarriesTheSameFields(t *testing.T) {
	project(t)

	r := run(t, "status", "--json")
	if r.code == 0 {
		t.Fatal("status succeeded in a project that was never set up")
	}
	got := decode[errorPayload](t, r)
	if got.OK {
		t.Error("ok = true on a failure")
	}
	if got.Code != "SDLC-E0002" || got.Fix == "" || got.Why == "" {
		t.Errorf("payload = %+v", got)
	}
}

func TestOutsideARepositoryTheErrorSaysSo(t *testing.T) {
	t.Setenv("GIT_WORK_TREE", "")
	t.Chdir(t.TempDir())

	r := run(t, "status")
	if !strings.Contains(r.stderr, "SDLC-E0001") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

// ------------------------------------------------------------------ init

func TestInitThenStatusReadsAsAFirstRun(t *testing.T) {
	project(t)

	r := mustRun(t, "init")
	if !strings.Contains(r.stdout, "created  .sdlc/config.json") {
		t.Errorf("init did not say what it created:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "sdlc doctor") {
		t.Errorf("init did not say what to do next:\n%s", r.stdout)
	}

	r = mustRun(t, "status")
	for _, want := range []string{"No iteration running", "next up", "US-001", "sdlc start"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("status is missing %q:\n%s", want, r.stdout)
		}
	}
}

func TestInitJSONSaysWhatItDid(t *testing.T) {
	project(t)
	writeFile(t, ".", "go.mod", "module example.com/x\n")

	got := decode[initPayload](t, mustRun(t, "init", "--json"))
	if !got.OK || got.Stack != "Go" {
		t.Errorf("payload = %+v", got)
	}
	if len(got.Created) < 3 {
		t.Errorf("created = %v", got.Created)
	}
}

// ------------------------------------------------------------------ the loop

func TestAStoryGoesThroughStartGateAndStop(t *testing.T) {
	project(t)
	mustRun(t, "init")

	start := decode[startPayload](t, mustRun(t, "start", "--json"))
	if start.Story != "US-001" || start.Resume {
		t.Fatalf("start = %+v", start)
	}

	gate := decode[gatePayload](t, mustRun(t, "gate", "analysis", "pass",
		"--note", "no trust boundary crossed", "--json"))
	if gate.Story != "US-001" || gate.Gate != "analysis" || gate.Status != "pass" {
		t.Fatalf("gate = %+v", gate)
	}

	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if status.Active != "US-001" {
		t.Errorf("active = %q", status.Active)
	}
	if status.Gates["analysis"] != "pass" {
		t.Errorf("gates = %v", status.Gates)
	}
	if status.Backlog["in_progress"] != 1 {
		t.Errorf("backlog = %v", status.Backlog)
	}

	stop := decode[stopPayload](t, mustRun(t, "stop", "--json"))
	if !stop.WasA || stop.Story != "US-001" {
		t.Errorf("stop = %+v", stop)
	}

	// Stopping does not undo the work: the gate is still recorded, and the
	// story is still in progress, so starting again picks it up.
	status = decode[statusPayload](t, mustRun(t, "status", "--json"))
	if status.Active != "" {
		t.Errorf("active = %q after stopping", status.Active)
	}
	again := decode[startPayload](t, mustRun(t, "start", "--json"))
	if !again.Resume {
		t.Error("starting again on an unfinished story did not report a resume")
	}
}

func TestStartingTwiceOnTheSameStoryChangesNothing(t *testing.T) {
	project(t)
	mustRun(t, "init")
	mustRun(t, "start")

	again := decode[startPayload](t, mustRun(t, "start", "--json"))
	if !again.Resume || again.Story != "US-001" {
		t.Errorf("start = %+v", again)
	}
}

func TestStartingASecondStoryIsRefused(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"One","status":"ready","priority":1},
	  {"id":"B-2","title":"Two","status":"ready","priority":2}]}`)
	mustRun(t, "start", "A-1")

	r := run(t, "start", "B-2")
	if r.code == 0 {
		t.Fatal("a second story started while the first was still running")
	}
	if !strings.Contains(r.stderr, "SDLC-E0012") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestStoppingWhenNothingIsRunningIsNotAnError(t *testing.T) {
	project(t)
	mustRun(t, "init")

	r := mustRun(t, "stop")
	if !strings.Contains(r.stdout, "No iteration was running") {
		t.Errorf("stdout = %q", r.stdout)
	}
}

func TestStartExplainsWhenNothingIsRunnable(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"Done","status":"done"},
	  {"id":"B-2","title":"Blocked","status":"blocked"},
	  {"id":"C-3","title":"Waiting","status":"ready","depends_on":["B-2"]}]}`)

	r := run(t, "start")
	if r.code == 0 {
		t.Fatal("start picked a story when none was runnable")
	}
	for _, want := range []string{"SDLC-E0010", "3 stories", "1 are done", "1 are blocked", "waiting on a dependency"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr is missing %q:\n%s", want, r.stderr)
		}
	}
}

// ------------------------------------------------------------------ gates

func TestGateRejectsANameThisVersionDoesNotKnow(t *testing.T) {
	project(t)
	mustRun(t, "init")
	mustRun(t, "start")

	r := run(t, "gate", "vibes", "pass")
	if r.code == 0 {
		t.Fatal("an unknown gate was recorded")
	}
	if !strings.Contains(r.stderr, "SDLC-E0013") || !strings.Contains(r.stderr, "tests_frozen") {
		t.Errorf("stderr should name the code and list the gates:\n%s", r.stderr)
	}
}

func TestGateRejectsAnOutcomeThatIsNotAnOutcome(t *testing.T) {
	project(t)
	mustRun(t, "init")
	mustRun(t, "start")

	r := run(t, "gate", "analysis", "probably")
	if !strings.Contains(r.stderr, "SDLC-E0014") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestGateWithoutAnIterationSaysWhatToDo(t *testing.T) {
	project(t)
	mustRun(t, "init")

	r := run(t, "gate", "analysis", "pass")
	if !strings.Contains(r.stderr, "SDLC-E0011") || !strings.Contains(r.stderr, "sdlc start") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

// A gate that fails is still recorded successfully: the command reports what
// happened, it does not decide whether that is acceptable.
func TestRecordingAFailedGateSucceeds(t *testing.T) {
	project(t)
	mustRun(t, "init")
	mustRun(t, "start")

	got := decode[gatePayload](t, mustRun(t, "gate", "code_review", "fail",
		"--note", "AC-2 is untested", "--json"))
	if got.Status != "fail" || got.Note != "AC-2 is untested" {
		t.Errorf("gate = %+v", got)
	}
}

func TestGateCanTargetAStoryThatIsNotTheActiveOne(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"One","status":"ready","priority":1},
	  {"id":"B-2","title":"Two","status":"ready","priority":2}]}`)
	mustRun(t, "start", "A-1")

	got := decode[gatePayload](t, mustRun(t, "gate", "retro", "pass", "--story", "B-2", "--json"))
	if got.Story != "B-2" {
		t.Errorf("gate = %+v", got)
	}
	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if status.Active != "A-1" || status.Gates["retro"] == "pass" {
		t.Errorf("recording against B-2 changed A-1: %+v", status)
	}
}

// ------------------------------------------------------------------ backlog

func TestStoryListMarksWhatStartWouldPickAndWhyOthersWait(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"First","status":"ready","priority":1},
	  {"id":"B-2","title":"Second","status":"ready","priority":2,"depends_on":["A-1"]}]}`)

	r := mustRun(t, "story", "list")
	if !strings.Contains(r.stdout, "> A-1") {
		t.Errorf("the story start would pick is not marked:\n%s", r.stdout)
	}
	if !strings.Contains(r.stdout, "waiting on A-1") {
		t.Errorf("the blocked story does not say what it waits for:\n%s", r.stdout)
	}

	rows := decode[storyListPayload](t, mustRun(t, "story", "list", "--json"))
	if len(rows.Stories) != 2 || !rows.Stories[0].Next {
		t.Errorf("rows = %+v", rows.Stories)
	}
	if len(rows.Stories[1].BlockedBy) != 1 {
		t.Errorf("B-2 blocked_by = %v", rows.Stories[1].BlockedBy)
	}
}

func TestStoryListOnAnEmptyBacklogSaysWhatToDo(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", "user_stories.json", `{"stories":[]}`)

	r := mustRun(t, "story", "list")
	if !strings.Contains(r.stdout, "empty") || !strings.Contains(r.stdout, "user_stories.json") {
		t.Errorf("stdout = %q", r.stdout)
	}
}

func TestBacklogCountsReadInAFixedOrder(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"a","status":"done"},
	  {"id":"B-2","title":"b","status":"ready"},
	  {"id":"C-3","title":"c","status":"ready"}]}`)

	first := mustRun(t, "status").stdout
	for range 5 {
		if got := mustRun(t, "status").stdout; got != first {
			t.Fatalf("status is not stable:\n%s\n---\n%s", first, got)
		}
	}
	if !strings.Contains(first, "2 ready · 1 done") {
		t.Errorf("counts read in the wrong order: %q", first)
	}
}

// ------------------------------------------------------------------ doctor

// The rule doctor exists to keep: a check that only reports a problem leaves
// the reader to guess, which is how a tool teaches people to ignore it.
func TestEveryProblemDoctorReportsCarriesAFix(t *testing.T) {
	project(t)
	mustRun(t, "init")
	// Break several things at once, so one run covers several checks.
	if err := os.Remove(filepath.Join(".", "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, ".", "user_stories.json", `{"stories":[]}`)
	writeFile(t, ".", ".sdlc/config.json",
		`{"version":1,"commands":{"test":"definitely-not-a-real-program --run"}}`)

	got := decode[doctorPayload](t, run(t, "doctor", "--json"))
	if got.OK || got.Problems == 0 {
		t.Fatalf("doctor found nothing wrong: %+v", got)
	}
	for _, c := range got.Checks {
		if c.State == stateProblem && strings.TrimSpace(c.Fix) == "" {
			t.Errorf("%q is a problem with no fix", c.Name)
		}
		if c.Detail == "" {
			t.Errorf("%q says nothing about what it found", c.Name)
		}
	}
}

func TestDoctorNamesAConfiguredProgramThatIsNotInstalled(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", ".sdlc/config.json",
		`{"version":1,"commands":{"lint":"definitely-not-a-real-program run ./..."}}`)

	got := decode[doctorPayload](t, run(t, "doctor", "--json"))
	found := false
	for _, c := range got.Checks {
		if strings.Contains(c.Detail, "definitely-not-a-real-program") {
			found = true
			if !strings.Contains(c.Detail, "lint") {
				t.Errorf("the problem does not say which command would fail: %q", c.Detail)
			}
		}
	}
	if !found {
		t.Errorf("doctor did not notice a command whose program is not installed: %+v", got.Checks)
	}
}

// A failure early on must not produce a cascade of unrelated failures.
func TestDoctorSkipsWhatItCannotCheckYet(t *testing.T) {
	t.Setenv("GIT_WORK_TREE", "")
	t.Chdir(t.TempDir())

	got := decode[doctorPayload](t, run(t, "doctor", "--json"))
	problems, skipped := 0, 0
	for _, c := range got.Checks {
		switch c.State {
		case stateProblem:
			problems++
		case stateSkipped:
			skipped++
		}
	}
	if problems != 1 {
		t.Errorf("outside a repository doctor reported %d problems, want just the one", problems)
	}
	if skipped == 0 {
		t.Error("nothing was reported as skipped, so the later checks appear to have passed")
	}
}

func TestDoctorExitsNonZeroWhenSomethingIsWrong(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", "user_stories.json", `{"stories":[]}`)

	r := run(t, "doctor")
	if r.code == 0 {
		t.Error("doctor exited 0 with a problem to report")
	}
	// Its report is the message; an empty "sdlc: " line would be noise.
	if strings.Contains(r.stderr, "sdlc: \n") {
		t.Errorf("doctor printed an empty error line:\n%q", r.stderr)
	}
	if !strings.Contains(r.stdout, "fix:") {
		t.Errorf("stdout carried no fix:\n%s", r.stdout)
	}
}

func TestProgramsInFindsWhatWouldActuallyRun(t *testing.T) {
	for command, want := range map[string][]string{
		"go build ./...":                     {"go"},
		"go build ./... && go vet ./...":     {"go"},
		`test -z "$(gofmt -l .)"`:            {"gofmt"},
		"npm run build --silent":             {"npm"},
		"pytest -q --cov | grep -E '^TOTAL'": {"pytest", "grep"},
		"":                                   nil,
		"   ":                                nil,
	} {
		got := programsIn(command)
		if len(got) != len(want) {
			t.Errorf("programsIn(%q) = %v, want %v", command, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("programsIn(%q) = %v, want %v", command, got, want)
				break
			}
		}
	}
}
