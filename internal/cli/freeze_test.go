package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// gitProject is a real repository, because the freeze asks git which files are
// part of the working tree rather than walking it and guessing. The test's
// commands run in it; nothing of git's own is set in the environment, so sdlc
// and git both find it by its .git, as they do for a person.
func gitProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	cmd := exec.CommandContext(t.Context(), "git", "init", "--quiet", "--initial-branch=main")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	chdir(t, root)
	return root
}

// initialised is `sdlc init` in root with what it wrote committed, which is
// where a project is when its first story starts: `sdlc start` begins new work
// only from a trunk with nothing uncommitted on it. It runs with nothing the
// test set, because it builds initialisedTemplate.
//
// Story worktrees are turned off: these tests are about one checkout, and hold
// what `sdlc start` does there to the released tool (US-029.2 AC-9). What start
// does with them on is storyworktree_test.go's.
func initialised(t *testing.T, root string) {
	t.Helper()
	runIn(t, root, "init")
	worktreesOff(t, root)
	commitEverything(t, root)
}

// worktreesOff sets git.worktrees to false in root's configuration, keeping
// the rest of the git section as it is.
func worktreesOff(t *testing.T, root string) {
	t.Helper()
	setGitOnDisk(t, root, "worktrees", false)
}

// setGitOnDisk sets one key of the configuration's git section.
func setGitOnDisk(t *testing.T, root, key string, value any) {
	t.Helper()
	path := filepath.Join(root, ".sdlc", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	section, _ := cfg["git"].(map[string]any)
	if section == nil {
		section = map[string]any{}
	}
	section[key] = value
	cfg["git"] = section
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// frozenStory is a project with an iteration running and one acceptance test
// written, which is the state Gate 3 starts from.
func frozenStory(t *testing.T) string {
	t.Helper()
	root := walkedCopy(t, "frozen story", func(t *testing.T, root string) {
		t.Helper()
		gitProjectInitialisedAt(t, root)
		mustRun(t, "start")
		reach(t, root, model.GateTestsFrozen)
		writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// AC-1\n")
		mustRunWith(t, "# Test plan\n\nAC-1 -> TestRejectsZero\n", "artifact", "write", "test_plan")
	})
	chdir(t, root)
	return root
}

func lockOnDisk(t *testing.T, root string) struct {
	Story string            `json:"story"`
	Files map[string]string `json:"files"`
} {
	t.Helper()
	var l struct {
		Story string            `json:"story"`
		Files map[string]string `json:"files"`
	}
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "state", "tests.lock"))
	if err != nil {
		t.Fatalf("no freeze on disk: %v", err)
	}
	if err := json.Unmarshal(raw, &l); err != nil {
		t.Fatalf("the freeze is not valid JSON: %v\n%s", err, raw)
	}
	return l
}

func TestFreezingRecordsWhatEveryTestContains(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)

	got := decode[freezePayload](t, mustRun(t, "freeze", "--json"))
	if got.Story != "US-001" || got.Count != 1 {
		t.Fatalf("freeze = %+v", got)
	}
	if got.Files[0] != "internal/invoice_test.go" {
		t.Errorf("files = %v", got.Files)
	}

	lock := lockOnDisk(t, root)
	if lock.Story != "US-001" {
		t.Errorf("the freeze belongs to %q", lock.Story)
	}
	if sum := lock.Files["internal/invoice_test.go"]; len(sum) != 64 {
		t.Errorf("the test was not recorded by content: %q", sum)
	}

	record, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), "freeze") {
		t.Errorf("the record does not mention the freeze:\n%s", record)
	}
}

// Build output and vendored code are not acceptance tests. Freezing them would
// be slow, noisy, and wrong the moment anything is rebuilt.
func TestTheFreezeIgnoresWhatGitIgnores(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	writeFile(t, root, ".gitignore", "vendor/\n")
	writeFile(t, root, "vendor/lib/thing_test.go", "package lib\n")

	got := decode[freezePayload](t, mustRun(t, "freeze", "--json"))
	for _, f := range got.Files {
		if strings.HasPrefix(f, "vendor/") {
			t.Errorf("the freeze reached into ignored files: %v", got.Files)
		}
	}
}

func TestFreezingTwiceIsRefused(t *testing.T) {
	t.Parallel()
	frozenStory(t)
	mustRun(t, "freeze")

	r := run(t, "freeze")
	if r.code == 0 {
		t.Fatal("a second freeze was taken over the top of the first")
	}
	for _, want := range []string{"SDLC-E0022", "sdlc unfreeze"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}
}

func TestFreezingWithNoTestsSaysWhereToLook(t *testing.T) {
	t.Parallel()
	gitProjectInitialised(t)
	mustRun(t, "start")

	r := run(t, "freeze")
	if r.code == 0 {
		t.Fatal("a freeze covering nothing was taken")
	}
	for _, want := range []string{"SDLC-E0024", "paths.tests"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}
}

// Lifting the freeze is sometimes right and is also exactly the move an agent
// would make to reach green. The reason is what tells the two apart, so it is
// not optional.
func TestLiftingTheFreezeNeedsAReasonAndRecordsIt(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")

	r := run(t, "unfreeze")
	if r.code == 0 {
		t.Fatal("the freeze was lifted with no reason given")
	}
	if !strings.Contains(r.stderr, "SDLC-E0026") {
		t.Errorf("stderr = %s", r.stderr)
	}

	const why = "AC-2's test asserted the old error message"
	mustRun(t, "unfreeze", "--reason", why)

	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "tests.lock")); err == nil {
		t.Error("the freeze is still on disk")
	}
	record, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), why) {
		t.Errorf("the reason is not on the record:\n%s", record)
	}
}

// A lifted freeze takes Gate 3 back with it, and every gate passed on top of it.
// Left passed, the loop carried on from where it was: nobody was sent back to
// change the test, and the first gate to notice the missing freeze said to
// freeze the wrong test again.
func TestLiftingTheFreezeSendsTheLoopBackToTheTests(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "frozen")
	satisfy(t, root, model.GatePlan)
	mustRun(t, "gate", "plan", "pass")

	out := decode[unfreezePayload](t, mustRun(t, "unfreeze", "--json",
		"--reason", "AC-2's test asserted the old error message"))
	if got := strings.Join(out.Reopened, ", "); got != "tests_frozen, plan" {
		t.Errorf("reopened %q, want tests_frozen and plan", got)
	}
	if next := decode[statusPayload](t, mustRun(t, "status", "--json")).NextGate; next != string(model.GateTestsFrozen) {
		t.Errorf("after the freeze was lifted the next gate is %q, want %s", next, model.GateTestsFrozen)
	}
}

func TestLiftingAFreezeThatIsNotThereSaysSo(t *testing.T) {
	t.Parallel()
	frozenStory(t)
	r := run(t, "unfreeze", "--reason", "no reason at all")
	if r.code == 0 {
		t.Fatal("a freeze that does not exist was lifted")
	}
	if !strings.Contains(r.stderr, "SDLC-E0023") {
		t.Errorf("stderr = %s", r.stderr)
	}
}

// Deleting the freeze and taking it again over edited tests was the whole of the
// way past it: every later check compares the tests against the freeze, and the
// freeze would say whatever they say now.
func TestAFreezeThatWentMissingIsNotTakenAgain(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "frozen")
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "tests.lock")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// quietly different\n")

	r := run(t, "freeze")
	if r.code == 0 {
		t.Fatal("a freeze that went missing was taken again over the edited tests")
	}
	for _, want := range []string{"SDLC-E0025", "sdlc unfreeze", "freeze_broken"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}

	// A person who removed it on purpose says so, and then it can be taken.
	mustRun(t, "unfreeze", "--reason", "removed the lock by hand to re-freeze")
	mustRun(t, "freeze")
}

// A command killed part-way through a write leaves its temporary file beside
// the one it was replacing. Nothing removed it, and the commit gate refused the
// tree as uncommitted until somebody found it and deleted it by hand.
func TestWhatAKilledCommandLeftHalfWrittenIsClearedByTheNext(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"PAY-1","title":"Refunds","status":"ready","risk_tier":"low","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN a paid invoice is refunded, the money goes back"}]}]}`)
	kept := []string{".sdlc/state/notes.tmp1", ".env.tmp5"}
	for _, rel := range kept {
		writeFile(t, root, rel, "not ours")
	}
	commitEverything(t, root)
	left := []string{
		".sdlc/stories/PAY-1/.gate-record.json.tmp3230204710",
		".user_stories.json.tmp118",
	}
	for _, rel := range left {
		writeFile(t, root, rel, "half a record")
	}

	mustRun(t, "start")
	for _, rel := range left {
		if _, err := os.Stat(filepath.Join(root, rel)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s, left by a killed command, is still there: %v", rel, err)
		}
	}
	for _, rel := range kept {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf("%s, which no write of ours makes, was removed: %v", rel, err)
		}
	}
}

// The commit is the last gate a freeze deleted by hand can be caught at: every
// gate after tests_frozen compared the tests against a freeze that was no
// longer there to compare against.
func TestAStoryIsNotCommittedOnceItsFreezeIsGone(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "human_gates", map[string]any{"pre_commit_pause_tiers": []string{}})
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"PAY-1","title":"Refunds","status":"ready","risk_tier":"high","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN a paid invoice is refunded, the money goes back"}]}]}`)
	mustRun(t, "start")
	reach(t, root, model.GateCommit)
	if err := os.Remove(filepath.Join(root, ".sdlc", "state", "tests.lock")); err != nil {
		t.Fatal(err)
	}
	commitEverything(t, root)

	r := run(t, "gate", "commit", "pass", "--note", "on trunk")
	if r.code == 0 {
		t.Fatal("a story whose freeze was deleted was committed")
	}
	if !strings.Contains(r.stderr, "not frozen for this story") {
		t.Errorf("the refusal did not say the freeze is gone:\n%s", r.stderr)
	}
}

// A freeze that will not read is lifted on the record, the same as one that
// does: freezing again is refused until it is.
func TestAnUnreadableFreezeIsLiftedOnTheRecord(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	writeFile(t, root, ".sdlc/state/tests.lock", "not json")

	const why = "tests.lock was truncated by a crash"
	mustRun(t, "unfreeze", "--reason", why)
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "tests.lock")); err == nil {
		t.Error("the unreadable freeze is still on disk")
	}
	record, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), why) {
		t.Errorf("the reason is not on the record:\n%s", record)
	}
	mustRun(t, "freeze")
}

// ------------------------------------------------------------------ the gate

func TestTheTestGateCannotPassWithoutAFreeze(t *testing.T) {
	t.Parallel()
	frozenStory(t)

	r := run(t, "gate", "tests_frozen", "pass", "--note", "tests written")
	if r.code == 0 {
		t.Fatal("the test gate passed with nothing frozen")
	}
	for _, want := range []string{"SDLC-E0023", "sdlc freeze"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}

	mustRun(t, "freeze")
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "3 criteria, 3 failing tests")
}

// This is the whole argument. If a frozen test can change and the gate still
// passes, the freeze is decoration.
func TestTheTestGateNoticesAFrozenTestThatChanged(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// quietly different\n")

	r := run(t, "gate", "tests_frozen", "pass")
	if r.code == 0 {
		t.Fatal("the gate passed on tests that are not the ones that were frozen")
	}
	for _, want := range []string{"SDLC-E0025", "internal/invoice_test.go"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}
}

func TestTheTestGateNoticesAFrozenTestThatVanished(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	if err := os.Remove(filepath.Join(root, "internal", "invoice_test.go")); err != nil {
		t.Fatal(err)
	}

	r := run(t, "gate", "tests_frozen", "pass")
	if r.code == 0 {
		t.Fatal("the gate passed on a test that is gone")
	}
	if !strings.Contains(r.stderr, "gone") {
		t.Errorf("the refusal does not say the file is missing:\n%s", r.stderr)
	}
}

// A test the freeze does not hold was checked by nothing: it ran at
// verification as though it had been written before the code, and every gate
// after the freeze let it through.
func TestATestTheFreezeDoesNotHoldStopsTheGates(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	writeFile(t, root, "internal/later_test.go", "package internal\n")

	r := run(t, "gate", "tests_frozen", "pass")
	if r.code == 0 {
		t.Fatal("the test gate passed with a test the freeze does not hold")
	}
	for _, want := range []string{"SDLC-E0043", "internal/later_test.go", "sdlc unfreeze"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}

	// Past Gate 3 too, which is where one would be written to pass.
	if err := os.Remove(filepath.Join(root, "internal", "later_test.go")); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "1 test, failing")
	writeFile(t, root, "internal/later_test.go", "package internal\n")
	mustRunWith(t, "# Plan\n", "artifact", "write", "plan")
	if r := run(t, "gate", "plan", "pass"); !strings.Contains(r.stderr, "SDLC-E0043") {
		t.Errorf("the plan gate took a test the freeze does not hold: code %d\n%s", r.code, r.stderr)
	}

	// Without the setting, freezing again is still a second freeze over the top.
	if r := run(t, "freeze"); !strings.Contains(r.stderr, "SDLC-E0022") {
		t.Errorf("a freeze took a new test with new test files not allowed: code %d\n%s", r.code, r.stderr)
	}
	if _, held := lockOnDisk(t, root).Files["internal/later_test.go"]; held {
		t.Error("the freeze holds a test added after it, with new test files not allowed")
	}
}

// freeze.allow_new_test_files let the test author write a test after the
// freeze, and nothing ever froze it. Freezing again adds it, so it is held
// like the rest from then on.
func TestAProjectThatAllowsNewTestsFreezesThemToo(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	setConfigOnDisk(t, root, "freeze", map[string]any{"allow_new_test_files": true})
	mustRun(t, "freeze")
	writeFile(t, root, "internal/later_test.go", "package internal\n")

	r := run(t, "gate", "tests_frozen", "pass")
	if !strings.Contains(r.stderr, "SDLC-E0043") || !strings.Contains(r.stderr, `"sdlc freeze" again`) {
		t.Errorf("the refusal does not send a project that allows new tests to freeze them: code %d\n%s",
			r.code, r.stderr)
	}

	got := decode[freezePayload](t, mustRun(t, "freeze", "--json"))
	if len(got.Added) != 1 || got.Added[0] != "internal/later_test.go" || got.Count != 2 {
		t.Errorf("freeze = %+v", got)
	}
	lock := lockOnDisk(t, root)
	if len(lock.Files["internal/later_test.go"]) != 64 || len(lock.Files["internal/invoice_test.go"]) != 64 {
		t.Errorf("the freeze does not hold both tests by content: %v", lock.Files)
	}
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "2 tests, both failing")

	// Nothing new to add is a second freeze over the top.
	if r := run(t, "freeze"); !strings.Contains(r.stderr, "SDLC-E0022") {
		t.Errorf("a freeze with nothing to add was not refused: code %d\n%s", r.code, r.stderr)
	}

	// A frozen test that changed does not go through on the back of a new one.
	writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// quietly different\n")
	writeFile(t, root, "internal/third_test.go", "package internal\n")
	if r := run(t, "freeze"); !strings.Contains(r.stderr, "SDLC-E0025") {
		t.Errorf("a freeze was extended over a changed test: code %d\n%s", r.code, r.stderr)
	}
	if after := lockOnDisk(t, root); after.Files["internal/invoice_test.go"] != lock.Files["internal/invoice_test.go"] ||
		after.Files["internal/third_test.go"] != "" {
		t.Errorf("the freeze changed when it was refused: %v", after.Files)
	}
}

// A freeze taken for another story is stale, not applicable. Accepting it would
// let one story's freeze wave the next one through.
func TestAFreezeFromAnotherStoryDoesNotCount(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "frozen")
	satisfy(t, root, model.GatePlan)

	lock := filepath.Join(root, ".sdlc", "state", "tests.lock")
	raw, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte(strings.Replace(string(raw), `"US-001"`, `"OTHER-9"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	r := run(t, "gate", "tests_frozen", "pass")
	if r.code == 0 {
		t.Fatal("another story's freeze passed this story's gate")
	}
	if !strings.Contains(r.stderr, "OTHER-9") {
		t.Errorf("the refusal does not say whose freeze it is:\n%s", r.stderr)
	}

	// Nor at any gate after it, which checks the freeze again every time the
	// loop moves.
	if r := run(t, "gate", "plan", "pass"); r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0023") {
		t.Errorf("another story's freeze passed this story's plan gate: code %d\n%s", r.code, r.stderr)
	}
}

// A finished story's freeze is lifted when its iteration ends, and only its
// own. A freeze naming another story is not this story's to release.
func TestStoppingAFinishedStoryLeavesAnotherStorysFreezeAlone(t *testing.T) {
	t.Parallel()
	root := finishedStory(t)
	lock := filepath.Join(root, ".sdlc", "state", "tests.lock")
	raw, err := os.ReadFile(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte(strings.Replace(string(raw), `"US-001"`, `"OTHER-9"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}

	mustRun(t, "stop")
	if got := lockOnDisk(t, root).Story; got != "OTHER-9" {
		t.Errorf("the freeze on disk now belongs to %q", got)
	}
}

// A skill reads status to find out where it is. If the freeze is not in there,
// the only way to know whether Gate 3 happened is to guess.
func TestStatusReportsTheFreeze(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)

	before := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if before.Freeze != nil {
		t.Errorf("freeze = %+v before anything was frozen", before.Freeze)
	}

	mustRun(t, "freeze")
	after := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if after.Freeze == nil {
		t.Fatal("status does not report the freeze")
	}
	if after.Freeze.Files != 1 || !after.Freeze.Intact || after.Freeze.Story != "US-001" {
		t.Errorf("freeze = %+v", after.Freeze)
	}

	writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// changed\n")
	broken := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if broken.Freeze.Intact {
		t.Error("status calls the freeze intact after a frozen test changed")
	}
	if len(broken.Freeze.Changed) != 1 || broken.Freeze.Changed[0] != "internal/invoice_test.go" {
		t.Errorf("changed = %v", broken.Freeze.Changed)
	}

	// And the prose says it too, for the person reading a terminal.
	if out := mustRun(t, "status").stdout; !strings.Contains(out, "changed since") {
		t.Errorf("the prose does not mention it:\n%s", out)
	}
}

// A freeze belongs to the story it was taken for. Started on a second story,
// `sdlc freeze` said "already frozen" and sent people to unfreeze -- which
// lifted the first story's freeze, logged it on the second story's record, and
// left the first story's tests editable when it was picked up again.
func TestAnotherStorysFreezeIsNotThisStorysToLift(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	addStory(t, root)
	mustRun(t, "stop")
	// Committed, or the second story would not start on top of the first
	// one's work.
	commitEverything(t, root)
	mustRun(t, "start", "US-002")

	r := run(t, "unfreeze", "--reason", "these are in my way")
	if r.code == 0 {
		t.Fatal("unfreeze lifted a freeze that belongs to another story")
	}
	if !strings.Contains(r.stderr, "US-001") {
		t.Errorf("the refusal does not say whose freeze it is:\n%s", r.stderr)
	}
	if got := lockOnDisk(t, root).Story; got != "US-001" {
		t.Fatalf("the freeze on disk now belongs to %q", got)
	}

	r = run(t, "freeze")
	if r.code == 0 {
		t.Fatal("a second freeze was taken over the first story's")
	}
	if strings.Contains(r.stderr, "unfreeze") {
		t.Errorf("freeze still sends people to unfreeze another story's freeze:\n%s", r.stderr)
	}

	// Dropping the story it belongs to is the way past it.
	// US-001 is first in the backlog, so its status is the first one replaced.
	setStatusOnDisk(t, root, model.StatusInProgress, model.StatusDropped)
	mustRun(t, "freeze")
	if got := lockOnDisk(t, root).Story; got != "US-002" {
		t.Errorf("after dropping US-001 the freeze belongs to %q, want US-002", got)
	}
}

// A freeze naming a story the backlog has never heard of any more -- not
// dropped, gone entirely -- is not a leftover either: nobody can say it is
// finished, so it still stands in the way.
func TestAFreezeForAStoryRemovedFromTheBacklogIsNotALeftover(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "frozen")
	addStory(t, root)
	mustRun(t, "stop")
	commitEverything(t, root)
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"US-002","title":"Two","status":"ready","priority":2}]}`)
	mustRun(t, "start")

	r := run(t, "freeze")
	if r.code == 0 {
		t.Fatal("a freeze for a story no longer in the backlog was replaced without a word")
	}
	if !strings.Contains(r.stderr, "SDLC-E0022") {
		t.Errorf("stderr = %s", r.stderr)
	}
	if got := lockOnDisk(t, root).Story; got != "US-001" {
		t.Errorf("the freeze on disk now belongs to %q", got)
	}
}

// paths.tests names nothing to freeze: freezing refuses rather than locking an
// empty set and reporting success.
func TestFreezingRefusesWhenTheProjectHasNotSaidWhatATestLooksLike(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "paths", map[string]any{"tests": map[string]any{"dirs": []string{}, "file_globs": []string{}}})
	mustRun(t, "start")

	r := run(t, "freeze")
	if r.code == 0 {
		t.Fatal("a freeze was taken for a project that names no test files")
	}
	if !strings.Contains(r.stderr, "has not said what a test file looks like") {
		t.Errorf("stderr = %s", r.stderr)
	}
}

// Git that cannot run is said with its own reason: freeze asks it which files
// are in the working tree, and a project without git on PATH cannot answer.
func TestFreezeRefusesWithGitsOwnReasonWhenGitCannotRun(t *testing.T) {
	gitProjectInitialised(t)
	mustRun(t, "start")
	t.Setenv("PATH", t.TempDir())

	r := run(t, "freeze")
	if r.code == 0 {
		t.Fatal("a freeze was taken with git not on PATH")
	}
	if !strings.Contains(r.stderr, "SDLC-E0021") || !strings.Contains(r.stderr, "did not finish") {
		t.Errorf("the refusal does not carry git's own reason:\n%s", r.stderr)
	}
}

// unfreeze with no iteration running has no story to lift a freeze from.
func TestUnfreezeWithNoIterationSaysWhatToDo(t *testing.T) {
	t.Parallel()
	gitProjectInitialised(t)

	r := run(t, "unfreeze", "--reason", "no reason at all")
	if r.code == 0 || !strings.Contains(r.stderr, "no story being worked on") {
		t.Errorf("stderr = %s", r.stderr)
	}
}

// A record that will not read is a record unfreeze cannot lift the freeze on,
// whether or not there is a freeze on disk to go with it.
func TestUnfreezeRefusesWhenTheRecordCannotBeRead(t *testing.T) {
	t.Parallel()
	t.Run("with no freeze taken", func(t *testing.T) {
		root := gitProjectInitialised(t)
		mustRun(t, "start")
		writeFile(t, root, ".sdlc/stories/US-001/gate-record.json", "{not json")

		r := run(t, "unfreeze", "--reason", "tidying up")
		if r.code == 0 {
			t.Fatal("unfreeze lifted a freeze it could not read the record for")
		}
		if !strings.Contains(r.stderr, "gate-record.json") || !strings.Contains(r.stderr, "SDLC-E0005") {
			t.Errorf("stderr does not name the record:\n%s", r.stderr)
		}
	})
	t.Run("with a freeze taken", func(t *testing.T) {
		root := frozenStory(t)
		mustRun(t, "freeze")
		writeFile(t, root, ".sdlc/stories/US-001/gate-record.json", "{not json")

		r := run(t, "unfreeze", "--reason", "tidying up")
		if r.code == 0 {
			t.Fatal("unfreeze lifted a freeze it could not read the record for")
		}
		if !strings.Contains(r.stderr, "gate-record.json") || !strings.Contains(r.stderr, "SDLC-E0005") {
			t.Errorf("stderr does not name the record:\n%s", r.stderr)
		}
	})
}

// paths.tests cleared after a freeze was taken leaves the freeze checking only
// what it always did -- the frozen files themselves -- and stops looking for
// test files it no longer has a setting to judge.
func TestClearingPathsTestsAfterAFreezeStopsCheckingForUnheldOnes(t *testing.T) {
	t.Parallel()
	root := frozenStory(t)
	mustRun(t, "freeze")
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "frozen")
	mustRunWith(t, "# Plan\n", "artifact", "write", "plan")
	setConfigOnDisk(t, root, "paths", map[string]any{"tests": map[string]any{"dirs": []string{}, "file_globs": []string{}}})
	writeFile(t, root, "internal/later_test.go", "package internal\n")

	if r := run(t, "gate", "plan", "pass"); r.code != 0 {
		t.Errorf("a new test file was noticed after paths.tests was cleared: %s", r.stderr)
	}
}
