package cli

// US-009: two refusals gave a reason that belongs to a different operation.
// These hold what a person reads, end to end through the commands:
//
//	AC-1  a git failure that wrote nothing names the subcommand that ran
//	AC-3  a plan that cannot be opened is the plan, not the test freeze
//	AC-5  `sdlc freeze` on a test file it cannot open keeps its own sentence
//
// The functions underneath are held where they live (internal/gitx and
// internal/store); what is held here is that the commands reach them.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// rrWhy is the text of the `why  ` line of a refusal, which may run on.
func rrWhy(t *testing.T, stderr string) string {
	t.Helper()
	for line := range strings.SplitSeq(stderr, "\n") {
		if why, ok := strings.CutPrefix(line, "  why  "); ok {
			return why
		}
	}
	t.Fatalf("the refusal has no why line:\n%s", stderr)
	return ""
}

// rrUnreadable takes every permission off the file, or skips the test where
// that does not stop its owner reading it.
func rrUnreadable(t *testing.T, path string) {
	t.Helper()
	skipIfPermissionsDoNotBind(t)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) }) // let t.TempDir() clean up
}

// ---------------------------------------------------------------- AC-1

// The refusal the story was found in: a gate that fails at the rework limit
// measures the tree with `git add`, and with git gone said the failure was in
// `git ls-files`.
func TestAC1AGateHandedOverWithGitMissingNamesTheGitAddThatDidNotFinish(t *testing.T) {
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "loop", map[string]any{"max_rework_rounds": 2, "max_review_rounds": 2, "max_stop_blocks": 3})
	mustRun(t, "start")
	mustRun(t, "gate", "dor", "fail", "--note", "AC-2 is not testable")
	t.Setenv("PATH", t.TempDir())

	r := run(t, "gate", "dor", "fail", "--note", "AC-2 is still not testable")
	if r.code == 0 {
		t.Fatal("a story was handed over with git not on PATH")
	}
	if !strings.Contains(r.stderr, "SDLC-E0021") || !strings.Contains(r.stderr, "git add did not finish") {
		t.Fatalf("the refusal does not say that git add did not finish:\n%s", r.stderr)
	}
	why := rrWhy(t, r.stderr)
	if !strings.Contains(why, "git add") {
		t.Errorf("why = %q, want it to name git add, the subcommand that ran", why)
	}
	if strings.Contains(why, "ls-files") {
		t.Errorf("why = %q, which sends a person to git ls-files when git add failed", why)
	}
}

// A second command, with a different subcommand: a fix that hard-codes the
// word for the first does not pass this one.
func TestAC1ApproveWithGitMissingGivesAReasonThatNamesTheSubcommandItsWhatNames(t *testing.T) {
	gitProjectInitialised(t)
	mustRun(t, "start")
	mustRun(t, "escalate", "spec_unclear", "--message", "which one?")
	t.Setenv("PATH", t.TempDir())

	r := run(t, "approve", "US-001")
	if r.code == 0 {
		t.Fatal("approve stamped a tree with git not on PATH")
	}
	var sub string
	for line := range strings.SplitSeq(r.stderr, "\n") {
		if what, ok := strings.CutPrefix(line, "sdlc: git "); ok && strings.HasSuffix(what, " did not finish") {
			sub = strings.TrimSuffix(what, " did not finish")
		}
	}
	if sub == "" {
		t.Fatalf("the refusal does not say which git subcommand did not finish:\n%s", r.stderr)
	}
	why := rrWhy(t, r.stderr)
	if !strings.Contains(why, "git "+sub) {
		t.Errorf("what names git %s and why = %q does not", sub, why)
	}
	if sub != "ls-files" && strings.Contains(why, "ls-files") {
		t.Errorf("git %s ran and why = %q names ls-files", sub, why)
	}
}

func TestAC1FreezeWithGitMissingNamesLsFilesBecauseThatIsWhatRan(t *testing.T) {
	gitProjectInitialised(t)
	mustRun(t, "start")
	t.Setenv("PATH", t.TempDir())

	r := run(t, "freeze")
	if r.code == 0 {
		t.Fatal("a freeze was taken with git not on PATH")
	}
	if !strings.Contains(r.stderr, "git ls-files did not finish") {
		t.Fatalf("freeze asks git ls-files for the working tree, and the refusal does not say so:\n%s", r.stderr)
	}
	if why := rrWhy(t, r.stderr); !strings.Contains(why, "git ls-files") {
		t.Errorf("why = %q, want it to name git ls-files, which is what ran", why)
	}
}

// ---------------------------------------------------------------- AC-3

func TestAC3ReviewAddDesignReviewSaysThePlanCouldNotBeReadAndNotTheFreeze(t *testing.T) {
	root := planned(t)
	rrUnreadable(t, filepath.Join(root, ".sdlc", "stories", "US-001", "PLAN.md"))

	r := runWith(t, "# architect\n", "review", "add", "design_review", "architect", "approve")
	if r.code == 0 {
		t.Fatal("a review was stamped against a plan that could not be read")
	}
	if !strings.Contains(r.stderr, ".sdlc/stories/US-001/PLAN.md") || !strings.Contains(r.stderr, "SDLC-E0005") {
		t.Fatalf("the refusal does not keep its code and the file it names:\n%s", r.stderr)
	}
	why := rrWhy(t, r.stderr)
	for _, banned := range []string{"freeze", "test file"} {
		if strings.Contains(strings.ToLower(why), banned) {
			t.Errorf("why = %q, which mentions %q for a plan", why, banned)
		}
	}
	if !strings.Contains(why, "plan could not be") {
		t.Errorf("why = %q, want it to say the plan could not be read", why)
	}
}

// The gate asks for the same subject again when it passes, and says the same.
func TestAC3TheDesignGateSaysThePlanCouldNotBeReadAndNotTheFreeze(t *testing.T) {
	root := planned(t)
	approveAll(t, model.GateDesignReview)
	rrUnreadable(t, filepath.Join(root, ".sdlc", "stories", "US-001", "PLAN.md"))

	r := run(t, "gate", "design_review", "pass")
	if r.code == 0 {
		t.Fatal("the gate passed while its plan could not be read")
	}
	why := rrWhy(t, r.stderr)
	for _, banned := range []string{"freeze", "test file"} {
		if strings.Contains(strings.ToLower(why), banned) {
			t.Errorf("why = %q, which mentions %q for a plan", why, banned)
		}
	}
	if !strings.Contains(why, "plan could not be") {
		t.Errorf("why = %q, want it to say the plan could not be read", why)
	}
}

// ---------------------------------------------------------------- AC-5 (guard: passes today)

func TestAC5FreezeOfATestItCannotOpenKeepsSayingWhatTheFreezeRecords(t *testing.T) {
	root := frozenStory(t)
	rrUnreadable(t, filepath.Join(root, "internal", "invoice_test.go"))

	r := run(t, "freeze")
	if r.code == 0 {
		t.Fatal("a freeze was taken of a test file that could not be opened")
	}
	if !strings.Contains(r.stderr, "internal/invoice_test.go could not be read") || !strings.Contains(r.stderr, "SDLC-E0005") {
		t.Fatalf("the refusal does not keep its code and the file it names:\n%s", r.stderr)
	}
	why := rrWhy(t, r.stderr)
	for _, want := range []string{"the freeze records what a test file contains", "this one could not be opened"} {
		if !strings.Contains(why, want) {
			t.Errorf("why = %q, want it to say %q", why, want)
		}
	}
}
