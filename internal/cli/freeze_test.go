package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

	cmd := exec.CommandContext(t.Context(), "git", "init", "--quiet")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	t.Chdir(root)
	return root
}

// frozenStory is a project with an iteration running and one acceptance test
// written, which is the state Gate 3 starts from.
func frozenStory(t *testing.T) string {
	t.Helper()
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")
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
	mustRun(t, "init")
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
