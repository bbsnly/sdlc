package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
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

// lockProbe is standard input that, before it hands over its document, runs
// another command that writes loop state. That command cannot finish while the
// one reading the input holds the project's lock.
type lockProbe struct {
	t      *testing.T
	doc    *strings.Reader
	probed bool
	result result
}

func (p *lockProbe) Read(b []byte) (int, error) {
	if !p.probed {
		p.probed = true
		p.result = run(p.t, "cost", "add", "--usd", "0.01")
	}
	return p.doc.Read(b)
}

// A document on standard input arrives when its writer is done with it. Read
// under the lock, every other command that writes loop state waited on that
// writer and then failed.
func TestADocumentIsReadBeforeTheLockIsTaken(t *testing.T) {
	gitProject(t)
	initialised(t)
	mustRun(t, "start")
	for _, args := range [][]string{
		{"artifact", "write", "analysis"},
		{"review", "add", "dor", "human-advocate", "note"},
	} {
		probe := &lockProbe{t: t, doc: strings.NewReader("# Notes\n\nfine\n")}
		var out, errb bytes.Buffer
		Execute(args, probe, &out, &errb)
		if !probe.probed {
			t.Fatalf("sdlc %s never read its input: %s", strings.Join(args, " "), errb.String())
		}
		if probe.result.code != 0 {
			t.Errorf("sdlc %s held the lock while it waited for its input:\n%s",
				strings.Join(args, " "), probe.result.stderr)
		}
	}
}

// gate --story and cost --story refuse an id the backlog does not have. These
// two made a directory and a record for it and said ok, and the gate they were
// feeding reported the document or the review missing afterwards.
func TestAMistypedStoryIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	mustRun(t, "start")
	for _, args := range [][]string{
		{"artifact", "write", "analysis", "--story", "NOPE"},
		{"review", "add", "dor", "human-advocate", "note", "--story", "NOPE"},
	} {
		r := runWith(t, "# Notes\n\nfine\n", args...)
		if r.code == 0 {
			t.Errorf("sdlc %s accepted a story that is not in the backlog", strings.Join(args, " "))
		}
		if !strings.Contains(r.stderr, "NOPE") {
			t.Errorf("sdlc %s does not name the story it refused:\n%s", strings.Join(args, " "), r.stderr)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "stories", "NOPE")); !os.IsNotExist(err) {
		t.Errorf("a directory was made for a story that does not exist: %v", err)
	}
}

// The session `sdlc start` runs in is the one the hook holds to the story, and
// picking the story up in another session moves it there.
func TestStartRecordsTheSessionItRunsIn(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	path := filepath.Join(root, ".sdlc", "state", "session")
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

	t.Setenv("CLAUDE_CODE_SESSION_ID", "session-a")
	mustRun(t, "start")
	if got := recorded(); got != "session-a" {
		t.Fatalf("after start, session = %q", got)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "session-b")
	mustRun(t, "start")
	if got := recorded(); got != "session-b" {
		t.Errorf("after resuming in another session, session = %q", got)
	}
	// From a terminal there is no session: the story holds every session.
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	mustRun(t, "start")
	if got := recorded(); got != "" {
		t.Errorf("a start outside Claude Code left session %q recorded", got)
	}
	t.Setenv("CLAUDE_CODE_SESSION_ID", "session-a")
	mustRun(t, "start")
	mustRun(t, "stop")
	if got := recorded(); got != "" {
		t.Errorf("stopping left session %q recorded", got)
	}
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
	gitProject(t)
	initialised(t)

	start := decode[startPayload](t, mustRun(t, "start", "--json"))
	if start.Story != "US-001" || start.Resume || start.NextGate != string(model.GateDoR) {
		t.Fatalf("start = %+v, want US-001 fresh at %s", start, model.GateDoR)
	}

	// Gate 2 comes after Gate 1 and cannot pass until its documents are stored,
	// so the flow through it is the flow a real story takes.
	mustRun(t, "gate", "dor", "pass", "--note", "criteria are testable")
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")

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
	// A session told only that it resumed would work Gate 1 over again.
	if again.NextGate != string(model.GateTestsFrozen) {
		t.Errorf("next_gate = %q on resuming, want %s: dor and analysis already passed",
			again.NextGate, model.GateTestsFrozen)
	}
}

func TestStartingTwiceOnTheSameStoryChangesNothing(t *testing.T) {
	gitProject(t)
	initialised(t)
	mustRun(t, "start")

	again := decode[startPayload](t, mustRun(t, "start", "--json"))
	if !again.Resume || again.Story != "US-001" {
		t.Errorf("start = %+v", again)
	}
}

// A story with no acceptance criteria gives Gate 3 nothing to write tests from,
// and every gate after it nothing to check the work against.
func TestDefinitionOfReadyRefusesAStoryWithNothingToTest(t *testing.T) {
	gitProject(t)
	initialised(t)
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"a","status":"ready","acceptance_criteria":[{"id":"AC-1","text":" "}]}]}`)
	mustRun(t, "start")

	r := run(t, "gate", "dor", "pass", "--note", "looks fine")
	if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0044") {
		t.Fatalf("dor passed for a story with nothing to test: exit %d\n%s", r.code, r.stderr)
	}
	// It has a criterion, so "none" would send the reader looking for nothing:
	// what is missing is the one field the loop reads.
	if !strings.Contains(r.stderr, `"text"`) {
		t.Errorf("the refusal does not name the field its criteria are missing:\n%s", r.stderr)
	}
	// A fail is always recordable: it is how the gate says the story is not ready.
	mustRun(t, "gate", "dor", "fail", "--note", "no acceptance criteria")
}

func TestStartingASecondStoryIsRefused(t *testing.T) {
	gitProject(t)
	initialised(t)
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

// Naming a story chose it over the priority order and skipped every other
// reason it would not have been picked: a dropped story, a blocked one, one
// marked done, or one waiting on a dependency all started.
func TestANamedStoryStartsOnlyWhenItCouldBePicked(t *testing.T) {
	for _, tc := range []struct{ name, story, want string }{
		{"waiting on a dependency", `{"id":"B-2","title":"Two","status":"ready","depends_on":["A-1"]}`, "waits on A-1"},
		{
			"waiting on a story that is not there", `{"id":"B-2","title":"Two","status":"ready","depends_on":["Z-9"]}`,
			"Z-9 (not in the backlog)",
		},
		{"dropped", `{"id":"B-2","title":"Two","status":"dropped"}`, "is dropped"},
		{"blocked", `{"id":"B-2","title":"Two","status":"blocked"}`, "is blocked"},
		{"marked done", `{"id":"B-2","title":"Two","status":"done"}`, "is marked done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gitProject(t)
			initialised(t)
			writeFile(t, ".", "user_stories.json",
				`{"stories":[{"id":"A-1","title":"One","status":"ready","priority":1},`+tc.story+`]}`)

			r := run(t, "start", "B-2")
			if r.code == 0 {
				t.Fatal("the story started")
			}
			for _, want := range []string{"SDLC-E0010", tc.want} {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
				}
			}
			if active := decode[statusPayload](t, mustRun(t, "status", "--json")).Active; active != "" {
				t.Errorf("the refused story is running anyway: %q", active)
			}
		})
	}

	gitProject(t)
	initialised(t)
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"One","status":"done","priority":1},
	  {"id":"B-2","title":"Two","status":"ready","depends_on":["A-1"]},
	  {"id":"C-3","title":"Three","status":"todo","priority":0}]}`)
	if got := decode[startPayload](t, mustRun(t, "start", "B-2", "--json")); got.Story != "B-2" {
		t.Errorf("a story whose dependency is done, named over a higher priority, did not start: %+v", got)
	}
}

func TestStoryListSaysWhenADependencyWillNeverBeDone(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"One","status":"dropped"},
	  {"id":"B-2","title":"Two","status":"ready","depends_on":["A-1"]}]}`)

	out := mustRun(t, "story", "list").stdout
	if !strings.Contains(out, "(waiting on A-1 (dropped))") {
		t.Errorf("story list does not say the dependency is dropped:\n%s", out)
	}
	rows := decode[storyListPayload](t, mustRun(t, "story", "list", "--json"))
	if got := rows.Stories[1].BlockedBy; len(got) != 1 || got[0] != "A-1" {
		t.Errorf("--json blocked_by is not the ids a script looks up: %v", got)
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

// An empty iteration file turns every rule off, and doctor's fix for it is to
// stop. So stop has to end the iteration over it rather than refuse.
func TestStopRemovesAnIterationFileThatNamesNoStory(t *testing.T) {
	root := project(t)
	mustRun(t, "init")
	active := filepath.Join(root, ".sdlc", "state", "active")
	if err := os.MkdirAll(filepath.Dir(active), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(active, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if r := run(t, "status"); r.code == 0 {
		t.Errorf("status read an empty iteration file as nothing running:\n%s", r.stdout)
	}
	r := mustRun(t, "stop")
	if !strings.Contains(r.stderr, "does not name a story") {
		t.Errorf("stop did not say why it removed the file: %q", r.stderr)
	}
	if _, err := os.Stat(active); !os.IsNotExist(err) {
		t.Errorf("the empty iteration file is still there after stop: %v", err)
	}
	mustRun(t, "status")
}

func TestStartExplainsWhenNothingIsRunnable(t *testing.T) {
	gitProject(t)
	initialised(t)
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"Done","status":"done"},
	  {"id":"B-2","title":"Blocked","status":"blocked"},
	  {"id":"C-3","title":"Waiting","status":"ready","depends_on":["B-2"]}]}`)

	r := run(t, "start")
	if r.code == 0 {
		t.Fatal("start picked a story when none was runnable")
	}
	for _, want := range []string{"SDLC-E0010", "3 stories", "1 done", "1 blocked", "waiting on a dependency"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr is missing %q:\n%s", want, r.stderr)
		}
	}
}

// A backlog that is finished is not a backlog that is stuck, and the sentence
// has to say which, because the two want opposite things from the reader.
func TestStartSaysWhenTheBacklogIsSimplyFinished(t *testing.T) {
	gitProject(t)
	initialised(t)
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"One","status":"done"},
	  {"id":"B-2","title":"Two","status":"done"}]}`)

	r := run(t, "start")
	if r.code == 0 {
		t.Fatal("start picked a story when every one was done")
	}
	if !strings.Contains(r.stderr, "every story in the backlog is finished") {
		t.Errorf("stderr reads as a blockage rather than a finished backlog:\n%s", r.stderr)
	}
}

// ------------------------------------------------------------------ gates

func TestGateRejectsANameThisVersionDoesNotKnow(t *testing.T) {
	gitProject(t)
	initialised(t)
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
	gitProject(t)
	initialised(t)
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
	gitProject(t)
	initialised(t)
	mustRun(t, "start")

	got := decode[gatePayload](t, mustRun(t, "gate", "code_review", "fail",
		"--note", "AC-2 is untested", "--json"))
	if got.Status != "fail" || got.Note != "AC-2 is untested" {
		t.Errorf("gate = %+v", got)
	}
}

func TestGateCanTargetAStoryThatIsNotTheActiveOne(t *testing.T) {
	gitProject(t)
	initialised(t)
	writeFile(t, ".", "user_stories.json", `{"stories":[
	  {"id":"A-1","title":"One","status":"ready","priority":1},
	  {"id":"B-2","title":"Two","status":"ready","priority":2,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN two is asked for, the system shall return 2"}]}]}`)
	mustRun(t, "start", "A-1")

	got := decode[gatePayload](t, mustRun(t, "gate", "dor", "pass", "--story", "B-2", "--json"))
	if got.Story != "B-2" {
		t.Errorf("gate = %+v", got)
	}
	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if status.Active != "A-1" || status.Gates["dor"] == "pass" {
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

func TestDoctorWantsTheContractHeadingNotALookalike(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", "CLAUDE.md", "# Mine\n\n### SDLC Contract\n\n- a subsection, not the section\n")

	got := decode[doctorPayload](t, run(t, "doctor", "--json"))
	for _, c := range got.Checks {
		if c.Name == "project contract" {
			if c.State != stateProblem {
				t.Errorf("project contract = %s (%s), want a problem", c.State, c.Detail)
			}
			return
		}
	}
	t.Fatal("doctor ran no project contract check")
}

// With the configuration broken, loop state is still checked, and the checks
// after it are skipped because of the configuration -- not because of the loop
// state that just passed.
func TestDoctorBlamesASkipOnWhatActuallyFailed(t *testing.T) {
	project(t)
	mustRun(t, "init")
	writeFile(t, ".", ".sdlc/config.json", `{not json`)

	skipped := 0
	for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
		if c.State != stateSkipped {
			continue
		}
		skipped++
		if !strings.Contains(c.Detail, "configuration") {
			t.Errorf("%q was skipped with %q, but the configuration is what failed", c.Name, c.Detail)
		}
	}
	if skipped == 0 {
		t.Fatal("a broken configuration skipped nothing")
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

// A gate record is committed with the work, so a merge can leave conflict
// markers in one. The refusal called that a bug in sdlc and sent the reader to
// the issue tracker, when git had the file and doctor would name it.
func TestAnUnreadableRecordSaysHowToRecover(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	mustRun(t, "start")
	writeFile(t, root, ".sdlc/stories/US-001/gate-record.json", "<<<<<<< HEAD\n{}\n=======\n")

	r := run(t, "status")
	if r.code == 0 {
		t.Fatal("status read a gate record with conflict markers in it")
	}
	for _, want := range []string{"SDLC-E0005", "gate-record.json", "sdlc doctor", "git"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}
	if strings.Contains(r.stderr, "is a bug") {
		t.Errorf("a record broken by a merge is blamed on sdlc:\n%s", r.stderr)
	}
}

// The binary check told everyone to run "./task build", which only means
// something in a clone of this repository, and ignored SDLC_BIN, which the hook
// launcher reads before PATH.
func TestDoctorLooksForTheBinaryWhereTheHookDoes(t *testing.T) {
	project(t)
	mustRun(t, "init")
	binaryCheckIn := func() check {
		t.Helper()
		for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
			if c.Name == "sdlc on PATH" {
				return c
			}
		}
		t.Fatal("doctor no longer checks for the binary")
		return check{}
	}

	t.Setenv("PATH", t.TempDir())
	t.Setenv("SDLC_BIN", "")
	c := binaryCheckIn()
	if c.State != stateProblem || !strings.Contains(c.Fix, "npx @bbsnly/sdlc install") {
		t.Errorf("a binary nowhere to be found gives no fix an installed user can follow: %+v", c)
	}

	name := "sdlc"
	if runtime.GOOS == "windows" {
		name = "sdlc.exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SDLC_BIN", bin)
	if c := binaryCheckIn(); c.State != stateOK || !strings.Contains(c.Detail, bin) {
		t.Errorf("a binary SDLC_BIN points at, which the hook would run, was not found: %+v", c)
	}

	// The hooks pass over an SDLC_BIN that names nothing runnable, so a build
	// being tried out would quietly not be the one enforcing.
	for _, broken := range []string{filepath.Join(t.TempDir(), "gone", name), t.TempDir()} {
		t.Setenv("SDLC_BIN", broken)
		if c := binaryCheckIn(); c.State != stateProblem || !strings.Contains(c.Detail, "SDLC_BIN") {
			t.Errorf("SDLC_BIN=%s names nothing runnable and doctor said nothing: %+v", broken, c)
		}
	}

	// exec.LookPath finds a bare name on PATH, and the launchers never look one
	// up: they test SDLC_BIN as a path from where the hook runs, so SDLC_BIN=sdlc
	// with sdlc on PATH is passed over like any other name that is not a file.
	t.Setenv("PATH", filepath.Dir(bin))
	t.Setenv("SDLC_BIN", name)
	if c := binaryCheckIn(); c.State != stateProblem || !strings.Contains(c.Detail, "SDLC_BIN") {
		t.Errorf("SDLC_BIN=%s is on PATH but is not a path the hooks run, and doctor said nothing: %+v", name, c)
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

// The hook refuses a commit while the gate record cannot be read, and sends you
// to `sdlc stop` to commit as yourself. A stop that failed on the same broken
// record left no way out from inside the tool, and a doctor that did not read
// the record could not say what was wrong.
func TestAnUnreadableRecordHasAWayOut(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	mustRun(t, "start")
	record := filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json")
	if err := os.WriteFile(record, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	if out := run(t, "doctor").stdout; !strings.Contains(out, "gate-record.json") {
		t.Errorf("doctor did not name the unreadable record:\n%s", out)
	}
	r := mustRun(t, "stop")
	if !strings.Contains(r.stderr, "gate-record.json") {
		t.Errorf("stop did not say the record could not be written to:\n%s", r.stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "active")); !os.IsNotExist(err) {
		t.Error("stop left the iteration running")
	}
}

// The hook refuses a commit when the record is missing and sends you to doctor,
// and doctor, reading through Record, saw a fresh record and said all was well.
func TestDoctorNamesAMissingRecord(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	mustRun(t, "start")
	if err := os.Remove(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json")); err != nil {
		t.Fatal(err)
	}
	r := run(t, "doctor")
	if !strings.Contains(r.stdout, "gate-record.json is missing") {
		t.Errorf("doctor did not name the missing record:\n%s", r.stdout)
	}
	if r.code == 0 {
		t.Error("doctor exited zero with the gate record missing")
	}
}

// The hook's warnings about unreadable loop state all say "run sdlc doctor".
// Doctor did not read either file, so it answered that all was well.
func TestDoctorNamesLoopStateTheHookCannotRead(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	state := filepath.Join(root, ".sdlc", "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "tests.lock"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := run(t, "doctor")
	if out := r.stdout + r.stderr; !strings.Contains(out, "loop state") || !strings.Contains(out, "tests.lock") {
		t.Errorf("doctor did not name the unreadable freeze:\n%s", out)
	}
}

// A freeze the record holds and tests.lock does not has the hook treating every
// test as frozen, and sending people here. Doctor said the freeze all read.
func TestDoctorNamesAFreezeTheLockNoLongerHolds(t *testing.T) {
	root := frozenStory(t)
	// Before the freeze, there is none for tests.lock to hold.
	if r := run(t, "doctor"); strings.Contains(r.stdout, "no longer holds") {
		t.Errorf("doctor named a freeze before one was taken:\n%s", r.stdout)
	}
	mustRun(t, "freeze")
	lock := filepath.Join(root, ".sdlc", "state", "tests.lock")
	for _, body := range []string{"{}", `{"story":"OTHER-1","files":{}}`} {
		if err := os.WriteFile(lock, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		r := run(t, "doctor")
		if !strings.Contains(r.stdout, "tests.lock no longer holds that freeze") {
			t.Errorf("doctor did not name the freeze tests.lock lost to %s:\n%s", body, r.stdout)
		}
		if r.code == 0 {
			t.Errorf("doctor exited zero with a freeze that tests.lock (%s) no longer holds", body)
		}
	}
}

// Doctor read only the active story's record. A stopped story's record that no
// longer reads was reported as fine, and the next start on it failed with an
// error calling itself a bug.
func TestDoctorNamesTheRecordOfAStoryNobodyIsWorkingOn(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	mustRun(t, "start")
	mustRun(t, "stop")
	record := filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json")
	if err := os.WriteFile(record, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := run(t, "doctor")
	if !strings.Contains(r.stdout, `the record for "US-001"`) || !strings.Contains(r.stdout, `"sdlc start US-001" fails`) {
		t.Errorf("doctor did not name the unreadable record of the stopped story:\n%s", r.stdout)
	}
	if r.code == 0 {
		t.Error("doctor exited zero with a gate record that does not read")
	}
}

// A broken configuration made doctor skip every check after it, the loop state
// included. The hook, with both files broken, warns about both and says doctor
// names them; doctor named one.
func TestDoctorNamesBrokenLoopStateBehindABrokenConfiguration(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	state := filepath.Join(root, ".sdlc", "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(state, "tests.lock"), filepath.Join(root, ".sdlc", "config.json")} {
		if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	r := run(t, "doctor")
	if !strings.Contains(r.stdout, "configuration") || !strings.Contains(r.stdout, "tests.lock") {
		t.Errorf("doctor did not name both the configuration and the freeze:\n%s", r.stdout)
	}
	if r.code == 0 {
		t.Error("doctor exited zero with the configuration and the freeze broken")
	}
}

// The configuration's own comment says to run doctor after changing anything,
// and doctor called a misspelled setting fine. Nor did it say which setting the
// loop could not use: only that one could not be.
func TestDoctorNamesTheSettingsThatAreWrong(t *testing.T) {
	project(t)
	mustRun(t, "init")
	configuration := func() check {
		t.Helper()
		for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
			if c.Name == "configuration" {
				return c
			}
		}
		t.Fatal("doctor ran no configuration check")
		return check{}
	}
	if c := configuration(); c.State != stateOK {
		t.Fatalf("the configuration sdlc init wrote is a problem: %s (%s)", c.Detail, c.Fix)
	}

	writeFile(t, ".", ".sdlc/config.json", `{"version":1,"loop":{"max_stop_block":0}}`)
	if c := configuration(); c.State != stateProblem || !strings.Contains(c.Detail, "loop.max_stop_block") {
		t.Errorf("a misspelled setting was not named: %s %q", c.State, c.Detail)
	}

	writeFile(t, ".", ".sdlc/config.json", `{"version":1,"loop":{"max_stop_blocks":-1}}`)
	c := configuration()
	if c.State != stateProblem || !strings.Contains(c.Detail, "loop.max_stop_blocks is -1") {
		t.Errorf("the setting the loop cannot use was not named: %s %q", c.State, c.Detail)
	}
	if strings.Contains(c.Fix, "sdlc init") {
		t.Errorf("the fix for a file that is there is to create it: %q", c.Fix)
	}
}

// A backlog that is there and does not read was answered with "run sdlc init
// to create it", which leaves a file that is already there alone.
func TestDoctorDoesNotSendABrokenBacklogToInit(t *testing.T) {
	project(t)
	mustRun(t, "init")
	backlog := func() check {
		t.Helper()
		for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
			if c.Name == "backlog" {
				return c
			}
		}
		t.Fatal("doctor ran no backlog check")
		return check{}
	}

	writeFile(t, ".", "user_stories.json", `{"stories":[{"id":"A-1","title":"One","status":"Ready"}]}`)
	c := backlog()
	if c.State != stateProblem || !strings.Contains(c.Detail, `"Ready"`) {
		t.Errorf("the story the backlog cannot use was not named: %s %q", c.State, c.Detail)
	}
	if strings.Contains(c.Fix, "sdlc init") {
		t.Errorf("the fix for a backlog that is there is to create it: %q", c.Fix)
	}

	if err := os.Remove("user_stories.json"); err != nil {
		t.Fatal(err)
	}
	if c := backlog(); !strings.Contains(c.Fix, "sdlc init") {
		t.Errorf("a missing backlog was not sent to sdlc init: %q", c.Fix)
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
		"go build ./...":                                 {"go"},
		"go build ./... && go vet ./...":                 {"go"},
		`test -z "$(gofmt -l .)"`:                        {"gofmt"},
		"npm run build --silent":                         {"npm"},
		"pytest -q --cov | grep -E '^TOTAL'":             {"pytest", "grep"},
		`case "$FILE" in *.go) gofmt -w "$FILE" ;; esac`: {"gofmt"},
		"":    nil,
		"   ": nil,
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
