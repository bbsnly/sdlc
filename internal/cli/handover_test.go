package cli

// US-008: a failure that reaches loop.max_rework_rounds, or a block that
// reaches loop.max_review_rounds, hands the story to a person. The command
// used to write the failure down first and hand over second, so a hand-over
// that could not be completed left the failure counted and nobody asked; run
// again, it counted the same failure a second time. A person settled the
// reading on 2026-10-11: the command is all-or-nothing.
//
//	AC-1  gate fail at the limit, hand-over refused: non-zero, record untouched
//	AC-2  the same command again, cause removed: counted once, gate_failing,
//	      awaiting_human
//	AC-3  review add block at the limit: the same, and no review file left
//	      that the record does not list
//	AC-4  below the limit nothing changes, with git or without it
//	AC-5  the refusal says the failure was not recorded and that running the
//	      command again is safe
//
// What stops a hand-over is more than a missing git: the backlog has to be
// written and the iteration ended, each in a directory of its own. Every test
// of a refused hand-over runs once for each cause in hoCauses.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// hoCause is one way of making a hand-over impossible from outside the
// command, and of putting it right again.
type hoCause struct {
	name string
	// permissions is a cause made by taking a permission away, which does not
	// bind on every platform or for every user.
	permissions bool
	// stop makes the hand-over impossible and returns what makes it possible
	// again. It is given the test of its own subtest, so that a variable it
	// sets for the process goes back when that subtest ends.
	stop func(t *testing.T, root string) (mend func())
}

// hoDeny takes a directory's write permission away and returns what gives it
// back; it is given back at the end of the test whatever happens, so that
// t.TempDir() can remove the directory.
func hoDeny(t *testing.T, dir string) func() {
	t.Helper()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	mend := func() {
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	return mend
}

var hoCauses = []hoCause{
	{
		// The reported case: the hand-over measures the tree, and git is gone.
		name: "git is not on PATH",
		stop: func(t *testing.T, _ string) func() {
			path := os.Getenv("PATH")
			t.Setenv("PATH", t.TempDir())
			return func() { t.Setenv("PATH", path) }
		},
	},
	{
		// git runs, and cannot measure the tree: `git add` stops at a file it
		// may not open.
		name:        "a file in the tree cannot be read",
		permissions: true,
		stop: func(t *testing.T, root string) func() {
			writeFile(t, root, "notes.txt", "what the story is about\n")
			path := filepath.Join(root, "notes.txt")
			if err := os.Chmod(path, 0o000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
			return func() {
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			}
		},
	},
	{
		// The tree is measured and the question could go on the record; the
		// story cannot be marked as waiting, because the backlog is replaced by
		// a file made beside it and nothing can be made there.
		name:        "the backlog cannot be written",
		permissions: true,
		stop:        hoDeny,
	},
	{
		// A story handed over stays this checkout's, by a claim kept in the git
		// directory. An iteration begun before claims were kept has none, and
		// the hand-over writes it before it ends the iteration.
		name:        "the claim on the story cannot be kept",
		permissions: true,
		stop: func(t *testing.T, root string) func() {
			claims := filepath.Join(root, ".git", "sdlc", "claims")
			if err := os.Remove(filepath.Join(claims, "US-001.json")); err != nil {
				t.Fatal(err)
			}
			return hoDeny(t, claims)
		},
	},
	{
		// Everything but the last step: the iteration ends by removing files
		// under .sdlc/state.
		name:        "the iteration cannot be ended",
		permissions: true,
		stop: func(t *testing.T, root string) func() {
			return hoDeny(t, filepath.Join(root, ".sdlc", "state"))
		},
	},
}

// hoEachCause runs body once for each cause that can be produced here.
func hoEachCause(t *testing.T, body func(t *testing.T, cause hoCause)) {
	t.Helper()
	for _, cause := range hoCauses {
		t.Run(cause.name, func(t *testing.T) {
			if cause.permissions {
				skipIfPermissionsDoNotBind(t)
			}
			body(t, cause)
		})
	}
}

func hoRecordBytes(t *testing.T, root string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// hoStatus is the status the backlog gives US-001, read from the file and not
// through a command: the commands are what is under test.
func hoStatus(t *testing.T, root string) string {
	t.Helper()
	var backlog struct {
		Stories []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"stories"`
	}
	if err := json.Unmarshal([]byte(backlogBytes(t, root)), &backlog); err != nil {
		t.Fatal(err)
	}
	for _, s := range backlog.Stories {
		if s.ID == "US-001" {
			return s.Status
		}
	}
	t.Fatal("US-001 is not in the backlog")
	return ""
}

// hoActive is the story the iteration is on, and "" when there is none.
func hoActive(t *testing.T) string {
	t.Helper()
	status := decode[activePayload](t, mustRun(t, "status", "--json"))
	if status.Active == nil {
		return ""
	}
	return *status.Active
}

// hoReviewFiles is every file in the story's reviews directory, by name.
func hoReviewFiles(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, ".sdlc", "stories", "US-001", "reviews"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// hoSaysNothingWasRecorded holds a refusal to AC-5, in whichever stream it was
// printed: the failure was not recorded, and the command is safe to run again.
func hoSaysNothingWasRecorded(t *testing.T, refusal string) {
	t.Helper()
	said := strings.ToLower(strings.Join(strings.Fields(refusal), " "))
	if !strings.Contains(said, "not recorded") {
		t.Errorf("the refusal does not say that the failure was not recorded:\n%s", refusal)
	}
	if !strings.Contains(said, "again") || !strings.Contains(said, "safe") {
		t.Errorf("the refusal does not say that running the command again is safe:\n%s", refusal)
	}
}

// hoStillBeingWorked holds a story whose hand-over was refused to where it was
// before the command: the record byte for byte, the backlog still calling it
// in_progress, and the iteration still on it. A story the backlog calls
// awaiting_human with no question on its record, or one with a question and
// an iteration still running, was half handed over.
func hoStillBeingWorked(t *testing.T, root string, before []byte) {
	t.Helper()
	if after := hoRecordBytes(t, root); !bytes.Equal(after, before) {
		t.Errorf("the refused command changed the gate record.\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if status := hoStatus(t, root); status != string(model.StatusInProgress) {
		t.Errorf("the refused command left the backlog calling the story %q, want in_progress", status)
	}
}

// hoGateAtTheLimit is a story one dor failure below loop.max_rework_rounds, 2.
func hoGateAtTheLimit(t *testing.T) string {
	t.Helper()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "loop", map[string]any{"max_rework_rounds": 2, "max_review_rounds": 2, "max_stop_blocks": 3})
	mustRun(t, "start")
	mustRun(t, "gate", "dor", "fail", "--note", "AC-2 is not testable")
	return root
}

var hoDorFailed = model.GateEvent(model.GateDoR, model.GateFail)

// ---------------------------------------------------------------- AC-1, AC-5

func TestAC1AGateFailureWhoseHandOverCannotBeCompletedLeavesTheRecordByteForByte(t *testing.T) {
	hoEachCause(t, func(t *testing.T, cause hoCause) {
		root := hoGateAtTheLimit(t)
		before := hoRecordBytes(t, root)
		mend := cause.stop(t, root)

		r := run(t, "gate", "dor", "fail", "--note", "AC-2 is still not testable")
		if r.code == 0 {
			t.Fatalf("the gate was recorded and the story was not handed over:\n%s", r.stdout)
		}
		if strings.Contains(r.stdout, "handed to a person") {
			t.Errorf("the refused command says the story was handed over:\n%s", r.stdout)
		}
		hoStillBeingWorked(t, root, before)

		// The reproduction ran it twice: each refusal used to add a failure.
		if again := run(t, "gate", "dor", "fail", "--note", "AC-2 is still not testable"); again.code == 0 {
			t.Fatalf("the second attempt was recorded and the story was not handed over:\n%s", again.stdout)
		}
		hoStillBeingWorked(t, root, before)

		mend()
		if active := hoActive(t); active != "US-001" {
			t.Errorf("the refused command ended the iteration: active = %q, want US-001", active)
		}
		record := readRecord(t, root, "US-001")
		if n := record.SinceDecision(model.EventGate, hoDorFailed); n != 1 {
			t.Errorf("two refused attempts left %d dor failures on the record, want the 1 that was there", n)
		}
	})
}

func TestAC5TheRefusedGateFailureSaysItWasNotRecordedAndIsSafeToRunAgain(t *testing.T) {
	hoEachCause(t, func(t *testing.T, cause hoCause) {
		root := hoGateAtTheLimit(t)
		cause.stop(t, root)

		r := run(t, "gate", "dor", "fail", "--note", "AC-2 is still not testable")
		if r.code == 0 {
			t.Fatalf("the gate was recorded and the story was not handed over:\n%s", r.stdout)
		}
		hoSaysNothingWasRecorded(t, r.stderr)

		// --json is how a script reads a refusal, and it is told the same.
		j := run(t, "gate", "dor", "fail", "--note", "AC-2 is still not testable", "--json")
		if j.code == 0 {
			t.Fatalf("the gate was recorded and the story was not handed over:\n%s", j.stdout)
		}
		hoSaysNothingWasRecorded(t, j.stdout)
	})
}

// A refusal that has nothing to do with a hand-over says nothing about one: a
// sentence added to every refusal would be wrong wherever it was not true.
func TestAC5ARefusalThatIsNotAHandOverDoesNotSayAFailureWentUnrecorded(t *testing.T) {
	t.Parallel()
	gitProjectInitialised(t)
	mustRun(t, "start")

	r := run(t, "gate", "plan", "pass")
	if r.code == 0 {
		t.Fatal("the plan gate passed before the gates in front of it")
	}
	if strings.Contains(strings.ToLower(r.stderr), "not recorded") {
		t.Errorf("a gate refused for its order talks of a failure that was not recorded:\n%s", r.stderr)
	}
}

// ---------------------------------------------------------------- AC-2

func TestAC2TheGateFailureRunAgainAfterTheCauseIsRemovedIsCountedOnceAndHandsTheStoryOver(t *testing.T) {
	hoEachCause(t, func(t *testing.T, cause hoCause) {
		root := hoGateAtTheLimit(t)
		mend := cause.stop(t, root)
		for range 2 {
			if r := run(t, "gate", "dor", "fail", "--note", "AC-2 is still not testable"); r.code == 0 {
				t.Fatalf("the gate was recorded and the story was not handed over:\n%s", r.stdout)
			}
		}
		mend()

		got := decode[gatePayload](t, mustRun(t, "gate", "dor", "fail", "--note", "AC-2 is still not testable", "--json"))
		if got.HandedOver != "gate_failing" {
			t.Fatalf("the same command, with the cause removed, did not hand the story over: %+v", got)
		}

		record := readRecord(t, root, "US-001")
		if n := record.SinceDecision(model.EventGate, hoDorFailed); n != 2 {
			t.Errorf("the record counts %d dor failures, want 2: one before, and the one handed over, once", n)
		}
		if n := len(record.Escalations); n != 1 {
			t.Fatalf("the record holds %d escalations, want 1: %+v", n, record.Escalations)
		}
		pending, ok := record.PendingEscalation()
		if !ok || pending.Type != "gate_failing" {
			t.Errorf("the pending escalation is %+v (%v), want gate_failing", pending, ok)
		}
		if !strings.Contains(pending.Message, "has failed 2 times") || !strings.Contains(pending.Message, "still not testable") {
			t.Errorf("the question a person reads miscounts the failures or loses the note: %q", pending.Message)
		}
		if n := record.Count("escalate", "gate_failing: "+pending.Message); n != 1 {
			t.Errorf("the record's history holds %d escalate events for the hand-over, want 1", n)
		}
		if status := hoStatus(t, root); status != string(model.StatusAwaitingHuman) {
			t.Errorf("the backlog calls the story %q, want awaiting_human", status)
		}
		if active := hoActive(t); active != "" {
			t.Errorf("the story was handed over and the iteration is still on %q", active)
		}
		if r := run(t, "start"); !strings.Contains(r.stderr, "SDLC-E0036") || !strings.Contains(r.stderr, "gate_failing") {
			t.Errorf("the story carried on past the hand-over:\n%s%s", r.stdout, r.stderr)
		}
	})
}

// ---------------------------------------------------------------- AC-3

// hoBlockedOnce is a story at the design review, one architect's block below
// loop.max_review_rounds, 2.
func hoBlockedOnce(t *testing.T) string {
	t.Helper()
	root := planned(t)
	mustRunWith(t, "# architect\n\nAC-3 has no step.\n",
		"review", "add", "design_review", "architect", "block", "--note", "AC-3 has no step")
	return root
}

var (
	hoArchitectBlocked = model.ReviewEvent(model.GateDesignReview, "architect", model.VerdictBlock)
	hoSecondBlock      = []string{"review", "add", "design_review", "architect", "block", "--note", "AC-3 still has no step"}
)

func TestAC3ABlockWhoseHandOverCannotBeCompletedLeavesTheRecordAndNoReviewFile(t *testing.T) {
	hoEachCause(t, func(t *testing.T, cause hoCause) {
		root := hoBlockedOnce(t)
		before, files := hoRecordBytes(t, root), hoReviewFiles(t, root)
		mend := cause.stop(t, root)

		r := runWith(t, "# architect\n\nrefused: AC-3 still has no step.\n", hoSecondBlock...)
		if r.code == 0 {
			t.Fatalf("the block was recorded and the story was not handed over:\n%s", r.stdout)
		}
		if strings.Contains(r.stdout, "handed to a person") {
			t.Errorf("the refused command says the story was handed over:\n%s", r.stdout)
		}
		hoStillBeingWorked(t, root, before)
		if after := hoReviewFiles(t, root); !slices.Equal(after, files) {
			t.Errorf("the refused review left a file the record does not list: reviews/ held %v and holds %v", files, after)
		}
		// AC-5, for a review.
		hoSaysNothingWasRecorded(t, r.stderr)

		if again := runWith(t, "# architect\n\nrefused again.\n", hoSecondBlock...); again.code == 0 {
			t.Fatalf("the second attempt was recorded and the story was not handed over:\n%s", again.stdout)
		}
		hoStillBeingWorked(t, root, before)
		if after := hoReviewFiles(t, root); !slices.Equal(after, files) {
			t.Errorf("the second refused review left a file the record does not list: reviews/ held %v and holds %v", files, after)
		}

		mend()
		if active := hoActive(t); active != "US-001" {
			t.Errorf("the refused command ended the iteration: active = %q, want US-001", active)
		}
	})
}

func TestAC3TheBlockRunAgainAfterTheCauseIsRemovedIsCountedOnceAndHandsTheStoryOver(t *testing.T) {
	hoEachCause(t, func(t *testing.T, cause hoCause) {
		root := hoBlockedOnce(t)
		mend := cause.stop(t, root)
		for range 2 {
			if r := runWith(t, "# architect\n\nrefused: AC-3 still has no step.\n", hoSecondBlock...); r.code == 0 {
				t.Fatalf("the block was recorded and the story was not handed over:\n%s", r.stdout)
			}
		}
		mend()

		const kept = "# architect\n\nAC-3 still has no step.\n"
		got := decode[reviewPayload](t, mustRunWith(t, kept, append(slices.Clone(hoSecondBlock), "--json")...))
		if got.HandedOver != "review_not_converging" {
			t.Fatalf("the same command, with the cause removed, did not hand the story over: %+v", got)
		}
		if got.Round != 2 {
			t.Errorf("the block that was handed over is round %d, want 2: a refused round is not a round", got.Round)
		}

		record := readRecord(t, root, "US-001")
		if n := record.SinceDecision(model.EventReview, hoArchitectBlocked); n != 2 {
			t.Errorf("the record counts %d blocks by the architect, want 2: one before, and the one handed over, once", n)
		}
		if n := len(record.Escalations); n != 1 {
			t.Fatalf("the record holds %d escalations, want 1: %+v", n, record.Escalations)
		}
		pending, ok := record.PendingEscalation()
		if !ok || pending.Type != "review_not_converging" {
			t.Errorf("the pending escalation is %+v (%v), want review_not_converging", pending, ok)
		}
		if !strings.Contains(pending.Message, "2 times") || !strings.Contains(pending.Message, "still has no step") {
			t.Errorf("the question a person reads miscounts the blocks or loses the note: %q", pending.Message)
		}
		if status := hoStatus(t, root); status != string(model.StatusAwaitingHuman) {
			t.Errorf("the backlog calls the story %q, want awaiting_human", status)
		}
		if active := hoActive(t); active != "" {
			t.Errorf("the story was handed over and the iteration is still on %q", active)
		}

		// Every file in reviews/ is one the record lists, and the other way
		// round; the one kept for round 2 is the review that was recorded,
		// not one of the two that were refused.
		var listed []string
		for _, rev := range record.Reviews {
			listed = append(listed, filepath.Base(filepath.FromSlash(rev.File)))
		}
		slices.Sort(listed)
		if files := hoReviewFiles(t, root); !slices.Equal(files, listed) {
			t.Errorf("reviews/ holds %v and the record lists %v", files, listed)
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(got.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != kept {
			t.Errorf("round 2's file holds a review that was refused, not the one recorded:\n%s", body)
		}
	})
}

// ---------------------------------------------------------------- AC-4 (guards: these pass today)

var hoTimestamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z`)

// hoTimeless is a record or a command's output with every time taken out,
// which is all that differs between two runs of the same commands.
func hoTimeless(text string) string { return hoTimestamp.ReplaceAllString(text, "<time>") }

// A failure below the limit asks git nothing, so it is recorded without git
// exactly as it is with it: what is printed, and every byte of the record but
// the times. A fix that measured the tree on every failure, to be ready for a
// hand-over, would refuse this one.
func TestAC4AGateFailureBelowTheLimitIsRecordedTheSameWithGitAndWithoutIt(t *testing.T) {
	failTwice := func(t *testing.T, withGit bool) (string, string) {
		t.Helper()
		root := gitProjectInitialised(t)
		setConfigOnDisk(t, root, "loop", map[string]any{"max_rework_rounds": 3, "max_review_rounds": 2, "max_stop_blocks": 3})
		mustRun(t, "start")
		if !withGit {
			t.Setenv("PATH", t.TempDir())
		}
		var printed strings.Builder
		for _, extra := range [][]string{nil, {"--json"}} {
			args := append([]string{"gate", "dor", "fail", "--note", "AC-2 is not testable"}, extra...)
			r := run(t, args...)
			if r.code != 0 {
				t.Fatalf("a failure below the limit was refused (git on PATH: %v):\n%s", withGit, r.stderr)
			}
			printed.WriteString(r.stdout)
		}
		return printed.String(), hoTimeless(string(hoRecordBytes(t, root)))
	}

	var withOut, withRecord, withoutOut, withoutRecord string
	t.Run("with git", func(t *testing.T) { withOut, withRecord = failTwice(t, true) })
	t.Run("without git", func(t *testing.T) { withoutOut, withoutRecord = failTwice(t, false) })

	if withOut != withoutOut {
		t.Errorf("a failure below the limit prints one thing with git and another without.\nwith:\n%s\nwithout:\n%s", withOut, withoutOut)
	}
	if withRecord != withoutRecord {
		t.Errorf("a failure below the limit is recorded one way with git and another without.\nwith:\n%s\nwithout:\n%s",
			withRecord, withoutRecord)
	}
	if strings.Contains(withoutOut, "handed") || !strings.Contains(withoutRecord, hoDorFailed) {
		t.Errorf("two failures under a limit of three were not simply recorded:\n%s\n%s", withoutOut, withoutRecord)
	}
}

// The same for each thing that stops a hand-over: below the limit none of
// them is asked for, so none of them is in the way.
func TestAC4AGateFailureBelowTheLimitIsRecordedWhateverWouldStopAHandOver(t *testing.T) {
	hoEachCause(t, func(t *testing.T, cause hoCause) {
		root := gitProjectInitialised(t)
		setConfigOnDisk(t, root, "loop", map[string]any{"max_rework_rounds": 3, "max_review_rounds": 2, "max_stop_blocks": 3})
		mustRun(t, "start")
		mustRun(t, "gate", "dor", "fail", "--note", "AC-2 is not testable")
		mend := cause.stop(t, root)

		got := decode[gatePayload](t, mustRun(t, "gate", "dor", "fail", "--note", "AC-2 is still not testable", "--json"))
		if !got.OK || got.HandedOver != "" || got.Status != "fail" {
			t.Errorf("the second failure of three allowed was not simply recorded: %+v", got)
		}
		mend()
		record := readRecord(t, root, "US-001")
		if n := record.SinceDecision(model.EventGate, hoDorFailed); n != 2 {
			t.Errorf("the record counts %d dor failures, want 2", n)
		}
		if result := record.Gates[model.GateDoR]; result.Status != model.GateFail || result.Note != "AC-2 is still not testable" {
			t.Errorf("the record's dor gate is %+v, want the failure and its note", result)
		}
		if len(record.Escalations) != 0 || hoStatus(t, root) != string(model.StatusInProgress) || hoActive(t) != "US-001" {
			t.Errorf("a failure below the limit moved the story: %d escalations, status %q, active %q",
				len(record.Escalations), hoStatus(t, root), hoActive(t))
		}
	})
}

// A limit of zero is no limit, so no failure is ever at it.
func TestAC4WithALimitOfZeroEveryFailureIsRecordedWithoutGit(t *testing.T) {
	root := planned(t)
	setConfigOnDisk(t, root, "loop", map[string]any{"max_rework_rounds": 0, "max_review_rounds": 0, "max_stop_blocks": 3})
	t.Setenv("PATH", t.TempDir())

	for i := range 4 {
		got := decode[gatePayload](t, mustRun(t, "gate", "design_review", "fail", "--note", "the plan has no step for AC-3", "--json"))
		if !got.OK || got.HandedOver != "" {
			t.Fatalf("failure %d under a limit of zero was not simply recorded: %+v", i+1, got)
		}
		block := decode[reviewPayload](t, mustRunWith(t, "# architect\n",
			"review", "add", "design_review", "architect", "block", "--note", "AC-3 has no step", "--json"))
		if !block.OK || block.HandedOver != "" || block.Round != i+1 {
			t.Fatalf("block %d under a limit of zero was not simply recorded: %+v", i+1, block)
		}
	}
	record := readRecord(t, root, "US-001")
	failures := record.SinceDecision(model.EventGate, model.GateEvent(model.GateDesignReview, model.GateFail))
	blocks := record.SinceDecision(model.EventReview, hoArchitectBlocked)
	if failures != 4 || blocks != 4 || len(record.Escalations) != 0 {
		t.Errorf("the record counts %d failures and %d blocks with %d escalations, want 4, 4 and none",
			failures, blocks, len(record.Escalations))
	}
	if files := hoReviewFiles(t, root); len(files) != 4 {
		t.Errorf("reviews/ holds %v, want the four rounds", files)
	}
}

// A block below the limit, and a block that cannot stop the gate however many
// there are, are recorded with nothing that a hand-over needs.
func TestAC4ABlockBelowTheLimitIsRecordedWhateverWouldStopAHandOver(t *testing.T) {
	hoEachCause(t, func(t *testing.T, cause hoCause) {
		root := planned(t)
		mend := cause.stop(t, root)

		first := decode[reviewPayload](t, mustRunWith(t, "# architect\n",
			"review", "add", "design_review", "architect", "block", "--note", "AC-3 has no step", "--json"))
		if !first.OK || first.HandedOver != "" || first.Round != 1 {
			t.Errorf("the first block of two allowed was not simply recorded: %+v", first)
		}
		// An advisory reviewer's block is a finding, not a round the rework
		// lost: the third is no nearer a hand-over than the first.
		for i := range 3 {
			advisory := decode[reviewPayload](t, mustRunWith(t, "# red team\n",
				"review", "add", "design_review", "red-team", "block", "--json"))
			if !advisory.OK || advisory.HandedOver != "" || advisory.Round != i+1 {
				t.Errorf("advisory block %d was not simply recorded: %+v", i+1, advisory)
			}
		}
		mend()

		record := readRecord(t, root, "US-001")
		if n := record.SinceDecision(model.EventReview, hoArchitectBlocked); n != 1 {
			t.Errorf("the record counts %d blocks by the architect, want 1", n)
		}
		if files := hoReviewFiles(t, root); len(files) != 4 {
			t.Errorf("reviews/ holds %v, want the architect's round and the red team's three", files)
		}
		if len(record.Escalations) != 0 || hoStatus(t, root) != string(model.StatusInProgress) || hoActive(t) != "US-001" {
			t.Errorf("a block below the limit moved the story: %d escalations, status %q, active %q",
				len(record.Escalations), hoStatus(t, root), hoActive(t))
		}
	})
}
