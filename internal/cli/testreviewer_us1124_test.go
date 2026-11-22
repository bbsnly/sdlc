package cli

// US-112.4: the test reviewer reads the new tests before `sdlc freeze`, and the
// freeze waits for its verdict where the story waits for it at all.
//
// Every project here is initialised's with the test reviewer put back, and the
// project manager left off, which is decision D1's case: with nobody staffing
// the story, every enabled agent takes part and is waited for. The staffed cases
// put the project manager back as staffing_test.go does. Helpers carry a tr
// prefix.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	trReviewer = "test-reviewer"
	trTest     = "package internal\n\n// AC-1\n"
	trFile     = "internal/invoice_test.go"
	// trFrozen is what `sdlc freeze` printed before the story, for the one test
	// file the helpers write (decision D2: it prints nothing new).
	trFrozen  = "US-001  1 test file frozen\n  internal/invoice_test.go\n"
	trMissing = "SDLC-E0029"
	trBlocks  = "SDLC-E0030"
)

// trStory is US-001 at Gate 3 with its test written and its test plan stored,
// in a project whose team gives the test reviewer the entry given.
func trStory(t *testing.T, entry map[string]any) string {
	t.Helper()
	root := gitProjectInitialised(t)
	setTeamAgentOnDisk(t, root, trReviewer, entry)
	commitEverything(t, root)
	mustRun(t, "start")
	reach(t, root, model.GateTestsFrozen)
	writeFile(t, root, trFile, trTest)
	mustRunWith(t, "# Test plan\n\nAC-1 -> TestRejectsZero\n", "artifact", "write", "test_plan")
	return root
}

// trReview records the test reviewer's verdict, a block with one important
// finding.
func trReview(t *testing.T, verdict string, extra ...string) result {
	t.Helper()
	args := []string{"review", "add", "tests_frozen", trReviewer, verdict}
	if verdict == "block" {
		args = append(args, "--important", "1", "--note", "AC-2 has no test")
	}
	return mustRunWith(t, "# Test review\n", append(args, extra...)...)
}

// trSubject is the subject a review of the tests as they are now is stamped with.
func trSubject(t *testing.T) string {
	t.Helper()
	got := decode[reviewPayload](t, trReview(t, "note", "--json"))
	if got.Subject == "" {
		t.Fatalf("a review of tests_frozen was stamped with no subject: %+v", got)
	}
	return got.Subject
}

// trRefused holds a freeze to a refusal with the code given that names the test
// reviewer, says what to do next, prints nothing on stdout and freezes nothing.
func trRefused(t *testing.T, root string, r result, code string, says ...string) {
	t.Helper()
	stfRefused(t, r, code, append([]string{trReviewer}, says...)...)
	if _, err := os.Stat(filepath.Join(root, ".sdlc", "state", "tests.lock")); err == nil {
		t.Errorf("a refused freeze left a freeze on disk")
	}
}

// ---------------------------------------------------------------------- AC-3

func TestAC3AC9TheFreezeWaitsForTheTestReviewersApproveOfTheTestsAsTheyAre(t *testing.T) {
	t.Parallel()
	root := trStory(t, map[string]any{})

	trRefused(t, root, run(t, "freeze"), trMissing, "has not reviewed")
	trReview(t, "block")
	trRefused(t, root, run(t, "freeze"), trBlocks, "AC-2 has no test")
	trReview(t, "note")
	trRefused(t, root, run(t, "freeze"), trMissing, "has not approved")
	trReview(t, "approve")

	if got := mustRun(t, "freeze").stdout; got != trFrozen {
		t.Errorf("AC-3 the freeze after the approve printed %q, want what it always printed, %q", got, trFrozen)
	}
	// The wait is the freeze's; the gate after it still passes on the same tests.
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "1 criterion, 1 failing test")
}

func TestAC3AC4AnApproveOfTestsThatHaveChangedSinceIsStaleAndTheFreezeWaits(t *testing.T) {
	t.Parallel()
	root := trStory(t, map[string]any{})
	trReview(t, "approve")
	writeFile(t, root, trFile, trTest+"\n// changed after the review\n")

	trRefused(t, root, run(t, "freeze"), trMissing, "reviewed something that has changed since")
	if out := mustRun(t, "review", "list", "--gate", "tests_frozen").stdout; !strings.Contains(out, "stale") {
		t.Errorf("AC-4 review list does not call the approve of the old tests stale:\n%s", out)
	}
	trReview(t, "approve")
	if got := mustRun(t, "freeze").stdout; got != trFrozen {
		t.Errorf("the freeze printed %q", got)
	}
}

// Where can_block is false the reviewer is advisory: the freeze waits until it
// has reviewed at all, and its block does not stop it.
func TestAC3AC9WithCanBlockFalseTheFreezeWaitsOnlyForAReviewAndPassesOverABlock(t *testing.T) {
	t.Parallel()
	root := trStory(t, map[string]any{"can_block": false})

	trRefused(t, root, run(t, "freeze"), trMissing, "has not reviewed")
	trReview(t, "block")
	if got := mustRun(t, "freeze").stdout; got != trFrozen {
		t.Errorf("AC-3 the freeze over an advisory block printed %q", got)
	}
}

// The check runs with the lock let go, and the freeze is then taken again from
// the top. A block recorded meanwhile is read on the second pass (THREATS.md,
// tampering: approved, then changed).
func TestAC3ABlockRecordedWhileTheFreezeCheckRanRefusesTheFreeze(t *testing.T) {
	t.Parallel()
	root := trStory(t, map[string]any{})
	subject := decode[reviewPayload](t, trReview(t, "approve", "--json")).Subject

	recordPath := filepath.Join(root, ".sdlc", "stories", "US-001", model.RecordFile)
	var record model.Record
	wtReadJSON(t, recordPath, &record)
	one := 1
	record.AddReview(model.Review{Gate: model.GateTestsFrozen, Role: trReviewer, Verdict: model.VerdictBlock,
		Subject: subject, Note: "AC-2 has no test", Important: &one}, time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC))
	blocked := filepath.Join(wtTempDir(t), "record")
	wtWriteJSON(t, blocked, record)
	fcSet(t, root, `cp `+suSh(blocked)+` .sdlc/stories/US-001/`+model.RecordFile)

	trRefused(t, root, run(t, "freeze"), trBlocks)
}

// ---------------------------------------------------------------------- AC-4

// The subject is the test files by content and by name: unchanged, it is the
// same; any test file changed, added, renamed or swapped with another, it is
// not; a file that is no test, or one git ignores, changes nothing.
func TestAC4TheSubjectOfATestsFrozenReviewIsEveryTestFileTheFreezeFindsByContent(t *testing.T) {
	t.Parallel()
	root := trStory(t, map[string]any{})
	writeFile(t, root, "internal/other_test.go", "package internal\n\n// AC-2\n")
	first := trSubject(t)

	same := func(why string) {
		t.Helper()
		if got := trSubject(t); got != first {
			t.Errorf("AC-4 %s changed the subject: %s, was %s", why, got, first)
		}
	}
	differs := func(why string) {
		t.Helper()
		if got := trSubject(t); got == first {
			t.Errorf("AC-4 %s left the subject as it was", why)
		}
	}

	same("asking again with nothing changed")
	writeFile(t, root, "internal/invoice.go", "package internal\n")
	same("a file that is not a test")
	writeFile(t, root, ".gitignore", "vendor/\n")
	writeFile(t, root, "vendor/lib/thing_test.go", "package lib\n")
	first = trSubject(t) // the .gitignore is no test file either, and the ignored one is not found
	same("a test file git ignores")

	writeFile(t, root, "internal/other_test.go", "package internal\n\n// AC-2, changed\n")
	differs("a test file changed")
	writeFile(t, root, "internal/other_test.go", "package internal\n\n// AC-2\n")
	same("the test file put back as it was")

	writeFile(t, root, "internal/third_test.go", "package internal\n")
	differs("a test file added")
	if err := os.Remove(filepath.Join(root, "internal", "third_test.go")); err != nil {
		t.Fatal(err)
	}
	same("the added file taken away")

	if err := os.Rename(filepath.Join(root, "internal", "other_test.go"), filepath.Join(root, "internal", "renamed_test.go")); err != nil {
		t.Fatal(err)
	}
	differs("a test file renamed")
	if err := os.Rename(filepath.Join(root, "internal", "renamed_test.go"), filepath.Join(root, "internal", "other_test.go")); err != nil {
		t.Fatal(err)
	}
	same("the rename undone")

	// The two contents swapped: the same set of hashes under other names.
	writeFile(t, root, trFile, "package internal\n\n// AC-2\n")
	writeFile(t, root, "internal/other_test.go", trTest)
	differs("two test files' contents swapped")
}

// The unit row for the subject: gateSubject answers for tests_frozen, and the
// review commands and the freeze then ask the same function.
func TestAC4AC9GateSubjectAnswersForTestsFrozenWithTheTestFiles(t *testing.T) {
	t.Parallel()
	root := trStory(t, map[string]any{})
	subject := func() string {
		t.Helper()
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		got, err := gateSubject(t.Context(), readingOf(store.New(&config.Project{Root: root, Config: cfg})), "US-001", model.GateTestsFrozen)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	first := subject()
	if first == "" {
		t.Fatal("AC-4 gateSubject has no subject for tests_frozen, so every review of it is stale")
	}
	if again := subject(); again != first {
		t.Errorf("AC-4 the subject of unchanged tests changed: %s, was %s", again, first)
	}
	if recorded := decode[reviewPayload](t, trReview(t, "approve", "--json")).Subject; recorded != first {
		t.Errorf("AC-4 review add stamped %s, and gateSubject says %s", recorded, first)
	}
	writeFile(t, root, trFile, trTest+"// more\n")
	if changed := subject(); changed == first {
		t.Error("AC-4 the subject did not change with the test file")
	}
}

// ---------------------------------------------------------------------- AC-2

// The project manager picks: a staffing that leaves the test reviewer out has a
// freeze that waits for nothing and prints what it always did; one that keeps it
// in waits for it.
func TestAC2AStaffingThatLeavesTheTestReviewerOutFreezesWithNoReviewAndOneThatKeepsItWaits(t *testing.T) {
	t.Parallel()
	t.Run("AC_2 left out", func(t *testing.T) {
		t.Parallel()
		root := stfStaffed(t, trReviewer)
		writeFile(t, root, trFile, trTest)
		mustRunWith(t, "# Test plan\n", "artifact", "write", "test_plan")
		if got := mustRun(t, "freeze").stdout; got != trFrozen {
			t.Errorf("AC-2 the freeze with the test reviewer left out printed %q, want %q", got, trFrozen)
		}
		if out := mustRun(t, "review", "list", "--gate", "tests_frozen").stdout; !strings.Contains(out, "left out by the staffing") {
			t.Errorf("AC-2 review list does not say the staffing left the test reviewer out:\n%s", out)
		}
	})
	t.Run("AC_2 kept in", func(t *testing.T) {
		t.Parallel()
		root := stfStaffed(t)
		writeFile(t, root, trFile, trTest)
		mustRunWith(t, "# Test plan\n", "artifact", "write", "test_plan")
		trRefused(t, root, run(t, "freeze"), trMissing, "has not reviewed")
		trReview(t, "approve")
		mustRun(t, "freeze")
	})
}

// ---------------------------------------------------------------------- AC-5

// The rounds are the reviewer's own limit where it has one. Under US-131 the
// arbiter rule covers the test reviewer too, a person's decision on that story:
// the reviewer's limit-th block sends the tests to test-reviewer-arbiter and
// hands nothing over, the reviewer is refused from then on, and only the
// arbiter's own limit-th block, at the same limit, hands the story to a person.
// The hand-over names the arbiter, the gate and its last note.
func TestAC5ATestReviewerAtItsLimitSendsTheTestsToItsArbiterWhoseOwnLimitHandsTheStoryToAPerson(t *testing.T) {
	t.Parallel()
	const arbiter = trReviewer + "-arbiter"
	// limits is how many blocks the reviewer gave before the tests went to its
	// arbiter, how many the arbiter gave before the story went to a person, and
	// what status and the log then say.
	limits := func(entry map[string]any) (reviewer, ruling int, said string) {
		t.Helper()
		root := trStory(t, entry)
		for n := 1; n <= 6 && reviewer == 0; n++ {
			got := decode[arReview](t, trReview(t, "block", "--json"))
			if got.HandedOver != "" {
				t.Fatalf("AC-5 the test reviewer's block %d handed the story over as %q, want the tests sent to %s", n, got.HandedOver, arbiter)
			}
			if got.Arbiter != "" {
				if got.Arbiter != arbiter {
					t.Errorf("AC-5 the test reviewer's limit-th block names arbiter %q, want %s", got.Arbiter, arbiter)
				}
				reviewer = n
			}
		}
		if reviewer == 0 {
			t.Fatalf("AC-5 six blocks and the tests never went to %s", arbiter)
		}
		if refusal := arRefusedAt(t, root, model.GateTestsFrozen, trReviewer, "block"); !strings.Contains(refusal, "SDLC-E0027") ||
			!strings.Contains(refusal, arbiter) {
			t.Errorf("AC-5 the test reviewer's block past its limit was not refused with SDLC-E0027 naming %s:\n%s", arbiter, refusal)
		}
		for n := 1; n <= 6; n++ {
			got := decode[arReview](t, mustRunWith(t, "# Test review, the arbiter\n", "review", "add", "tests_frozen", arbiter, "block",
				"--important", "1", "--note", "AC-2 still has no test", "--json"))
			if got.HandedOver != "" {
				if got.HandedOver != "review_not_converging" {
					t.Errorf("AC-5 handed over as %q", got.HandedOver)
				}
				return reviewer, n, mustRun(t, "status").stdout + mustRun(t, "log").stdout
			}
		}
		t.Fatal("AC-5 six blocks by the arbiter and the story was never handed to a person")
		return 0, 0, ""
	}
	byDefault, rulingByDefault, _ := limits(map[string]any{})
	own, ruling, said := limits(map[string]any{"max_rounds": 1})
	if own >= byDefault {
		t.Errorf("AC-5 with max_rounds 1 the tests went to the arbiter after %d blocks, and after %d with loop.max_review_rounds",
			own, byDefault)
	}
	if rulingByDefault != byDefault || ruling != own {
		t.Errorf("AC-5 the arbiter handed the story over after %d and %d blocks, want its role's limits, %d and %d",
			rulingByDefault, ruling, byDefault, own)
	}
	for _, want := range []string{arbiter, "tests_frozen", "AC-2 still has no test"} {
		if !strings.Contains(said, want) {
			t.Errorf("AC-5 the hand-over does not name %q:\n%s", want, said)
		}
	}
}

// ---------------------------------------------------------------------- AC-7

func TestAC7AfterAPersonLiftsTheFreezeAChangedTestWaitsForANewApprove(t *testing.T) {
	t.Parallel()
	root := trStory(t, map[string]any{})
	trReview(t, "approve")
	mustRun(t, "freeze")
	mustRun(t, "gate", "tests_frozen", "pass", "--note", "frozen")

	const why = "AC-1's test asserted the old message"
	if got, want := mustRun(t, "unfreeze", "--reason", why).stdout,
		"US-001  freeze lifted on 1 test file\n  "+why+"\n  reopened: tests_frozen\n"; got != want {
		t.Errorf("AC-7 unfreeze printed %q, want what it printed before the story, %q", got, want)
	}
	writeFile(t, root, trFile, trTest+"\n// corrected\n")
	trRefused(t, root, run(t, "freeze"), trMissing, "reviewed something that has changed since")
	trReview(t, "approve")
	mustRun(t, "freeze")
}

func TestAC7AfterAnUnfreezeTheSameTestsKeepTheirApprove(t *testing.T) {
	t.Parallel()
	trStory(t, map[string]any{})
	trReview(t, "approve")
	mustRun(t, "freeze")
	mustRun(t, "unfreeze", "--reason", "looked again, nothing to change")
	if got := mustRun(t, "freeze").stdout; got != trFrozen {
		t.Errorf("AC-7 the freeze of the same tests after an unfreeze printed %q", got)
	}
}

// ---------------------------------------------------------------------- AC-8

func TestAC8ReviewListAndStaffingShowShowTheTestReviewerWhoIsExpectedAndItsPower(t *testing.T) {
	t.Parallel()
	t.Run("AC_8 blocking", func(t *testing.T) {
		t.Parallel()
		trStory(t, map[string]any{})
		want := "  tests_frozen    test-reviewer   blocking  not reviewed\n"
		if out := mustRun(t, "review", "list").stdout; !strings.Contains(out, want) {
			t.Errorf("AC-8 review list has no line %q:\n%s", want, out)
		}
		trReview(t, "approve")
		want = "  tests_frozen    test-reviewer   blocking  approve (round 1)\n"
		if out := mustRun(t, "review", "list", "--gate", "tests_frozen").stdout; !strings.Contains(out, want) {
			t.Errorf("AC-8 review list has no line %q:\n%s", want, out)
		}
		if _, ok := stfShow(t, "--gate", "tests_frozen").atGate(trReviewer); !ok {
			t.Error("AC-8 staffing show does not say the test reviewer is needed at tests_frozen")
		}
		if out := mustRun(t, "staffing", "show", "--gate", "tests_frozen").stdout; !strings.Contains(out, "test-reviewer   needed at this gate") {
			t.Errorf("AC-8 staffing show in prose does not name the test reviewer:\n%s", out)
		}
	})
	t.Run("AC_8 advisory", func(t *testing.T) {
		t.Parallel()
		trStory(t, map[string]any{"can_block": false})
		want := "  tests_frozen    test-reviewer   advisory  not reviewed\n"
		if out := mustRun(t, "review", "list").stdout; !strings.Contains(out, want) {
			t.Errorf("AC-8 review list has no line %q:\n%s", want, out)
		}
	})
}

// Decision D2: disabled, the freeze waits for nothing and prints what it always
// did; review list, status and staffing show are where it is named as disabled.
func TestAC8DisabledTheFreezePrintsWhatItDidAndTheOtherCommandsNameTheTestReviewerDisabled(t *testing.T) {
	t.Parallel()
	// instances is said too, so that the entry differs from the one initialised
	// wrote and there is a change to commit.
	trStory(t, map[string]any{"enabled": false, "instances": 1})

	if got := mustRun(t, "freeze").stdout; got != trFrozen {
		t.Errorf("D2 the freeze with the test reviewer disabled printed %q, want %q", got, trFrozen)
	}
	second := run(t, "freeze")
	if second.code == 0 || !strings.Contains(second.stderr, "SDLC-E0022") || strings.Contains(second.stderr, trReviewer) {
		t.Errorf("D2 a second freeze is refused as it always was, and names no reviewer: exit %d\n%s", second.code, second.stderr)
	}
	mustRun(t, "unfreeze", "--reason", "D2 again, as JSON")
	got := mustRun(t, "freeze", "--json").stdout
	if want := `{"ok":true,"story":"US-001","files":["internal/invoice_test.go"],"count":1}` + "\n"; got != want {
		t.Errorf("D2 freeze --json printed %q, want %q", got, want)
	}

	disabled := "disabled in team.agents.test-reviewer.enabled"
	if out := mustRun(t, "review", "list", "--gate", "tests_frozen").stdout; !strings.Contains(out, "test-reviewer") || !strings.Contains(out, disabled) {
		t.Errorf("AC-8 review list does not name the test reviewer as disabled:\n%s", out)
	}
	for _, args := range [][]string{{"status"}, {"staffing", "show"}} {
		if out := mustRun(t, args...).stdout; !strings.Contains(out, "test-reviewer is "+disabled) {
			t.Errorf("AC-8 sdlc %s does not name the test reviewer as disabled:\n%s", strings.Join(args, " "), out)
		}
	}
	if got := decode[statusPayload](t, mustRun(t, "status", "--json")).Disabled; !strings.Contains(strings.Join(got, ","), trReviewer) {
		t.Errorf("AC-8 status --json lists %q as disabled", got)
	}
}
