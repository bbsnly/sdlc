package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// gitProject is a real repository, because the freeze asks git which files are
// part of the working tree rather than walking it and guessing.
//
// Both git variables are set, and set consistently: the tool reads GIT_WORK_TREE
// to find the project, git refuses a work tree without a git directory, and an
// inherited pair from the developer's own shell would otherwise decide where
// these tests think they are.
func gitProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)

	cmd := exec.CommandContext(t.Context(), "git", "init", "--quiet", "--initial-branch=main")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(root)
	return root
}

// initialised is `sdlc init` with what it wrote committed, which is where a
// project is when its first story starts: `sdlc start` begins new work only from
// a trunk with nothing uncommitted on it.
func initialised(t *testing.T) {
	t.Helper()
	mustRun(t, "init")
	commitEverything(t, ".")
}

// frozenStory is a project with an iteration running and one acceptance test
// written, which is the state Gate 3 starts from.
func frozenStory(t *testing.T) string {
	t.Helper()
	root := gitProject(t)
	initialised(t)
	mustRun(t, "start")
	reach(t, root, model.GateTestsFrozen)
	writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// AC-1\n")
	mustRunWith(t, "# Test plan\n\nAC-1 -> TestRejectsZero\n", "artifact", "write", "test_plan")
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
	gitProject(t)
	initialised(t)
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

func TestLiftingAFreezeThatIsNotThereSaysSo(t *testing.T) {
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

// A freeze that will not read is lifted on the record, the same as one that
// does: freezing again is refused until it is.
func TestAnUnreadableFreezeIsLiftedOnTheRecord(t *testing.T) {
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
	root := frozenStory(t)
	mustRun(t, "freeze")

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
}

// A skill reads status to find out where it is. If the freeze is not in there,
// the only way to know whether Gate 3 happened is to guess.
func TestStatusReportsTheFreeze(t *testing.T) {
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
	root := frozenStory(t)
	mustRun(t, "freeze")
	addStory(t, root, "US-002")
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
