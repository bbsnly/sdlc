package cli

// US-091 in process: trunk's branch ref put on a commit of the story's own does
// not make the test files that commit added or changed trunk's.
//
// US-088 takes a test file for trunk's when it differs between the commit the
// story was cut from and the trunk commit the story sits on, and the story's
// commits after that one leave it alone. That commit is where the story's
// branch meets trunk's tip, so a person who moves trunk to a story commit that
// is not the tip makes every test file at or under it read as trunk's: the
// commit gate printed "accepted as main's" for a test added after the freeze,
// and `sdlc stop` landed it. AC-1 holds the answer to what it was before trunk
// moved; AC-2 holds that a test file only trunk changed is still taken for
// trunk's, wherever the story got onto trunk's later commits; AC-3 is the
// sandbox test in test/e2e.
//
// The fix adds a field to the story's record, and this file does not know its
// name. Everything here reads what a caller can see: the exit code, the SDLC-E
// code, what a command printed, git's own answers and the record's trunk_tests.
// Where a test needs "a record from before the field" it takes a record the
// binary wrote and drops every top-level key the record did not have at the
// commit this story began from (tsRecordKeysBefore), whatever the fix named its
// own.
//
// Every refusal here is held to the answer the same state gave with trunk at
// the commit the story was cut from: that is what AC-1 asks for, and it is the
// answer a hard-coded code and file name cannot give.
//
// Helpers carry a ts prefix, for trunk on a story's commit.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// tsFile is a source file the story's last commit holds and the others do not.
const tsFile = "price list.txt"

// tsCodeLine is the line a refusal ends with: its code and the page that
// explains it.
var tsCodeLine = regexp.MustCompile(`(?m)^\s+(SDLC-E\d{4})\s+https://`)

// tsPredates is what a refusal for a record from before the field has to say:
// that the record is older than what the binary now writes. The person's answer
// fixes the meaning and not the sentence, so several spellings pass; today's
// refusals match none of them.
var tsPredates = regexp.MustCompile(`(?i)predate|(older|earlier|previous|former)\s+(sdlc|version|release|binary)|(written|made|saved)\s+(by|before|with)\s+(an?\s+)?(older|earlier|previous|former)`)

// tsRecordKeysBefore are the top-level keys of a story's record at the commit
// this story began from (model.Record's json tags). A key that is not among
// them was added by this story's fix.
var tsRecordKeysBefore = []string{
	"story", "created", "base", "commit_base", "commit", "range_unknown", "head_past_commit", "cut",
	"change", "change_base", "landing", "replay", "trunk_tests", "gates", "events", "escalations",
	"approvals", "reviews", "security_sensitive", "flags", "metrics", "spend", "staffing",
}

// tsStory is a story in its worktree that has passed every gate before the
// commit with nothing committed: the commit gate has still to run.
type tsStory struct{ main, wt, cut string }

// tsWorked takes a story, whose trunk held internal/tax_test.go before it began
// and whose freeze therefore holds it, through the gates up to the commit.
// afterVerification is what is done to the tree once the verification has
// passed: a test file written, or a frozen one changed, by a route the hook did
// not see.
//
// Then the story's own commits are made, steps, each watched by the hook first
// where hooked, and returned. Every row of these tests tries trunk on each of
// its commits, from the same starting point, so this is a walked template
// named key, which has to say what afterVerification and steps do.
func tsWorked(t *testing.T, key string, afterVerification func(t *testing.T, wt string), steps []tsStep, hooked bool) (tsStory, []string) {
	t.Helper()
	main, notes := walkedCopyNoted(t, "trunk on the story's own commit: "+key, func(t *testing.T, root string) map[string]string {
		t.Helper()
		cut := ldWalkAt(t, ldWalk{taxTest: true}, root)
		wt := swWorktreeOf(root, "US-001")
		otIdentity(t, root)
		ldWork(t, wt)
		swPass(t, wt, ldSession, model.GateImplementation, model.GateVerifierReview)
		if afterVerification != nil {
			afterVerification(t, wt)
		}
		swPass(t, wt, ldSession, model.GateVerifierReview, model.GateCommit)
		var commits []string
		for _, step := range steps {
			if hooked {
				watched(t, wt, swRev(t, wt, "HEAD"))
			}
			commits = append(commits, tsCommit(t, wt, step.message, step.paths...))
		}
		return map[string]string{"cut": cut, "commits": strings.Join(commits, " ")}
	})
	wt := swWorktreeOf(main, "US-001")
	chdir(t, wt)
	setenv(t, store.SessionEnv, ldSession)
	return tsStory{main: main, wt: wt, cut: notes["cut"]}, strings.Fields(notes["commits"])
}

// tsCommit commits the paths named, or everything the worktree holds where none
// is, and returns the commit.
func tsCommit(t *testing.T, wt, message string, paths ...string) string {
	t.Helper()
	add := []string{"add", "-A"}
	if len(paths) > 0 {
		add = append(add, "--")
		add = append(add, paths...)
	}
	swGit(t, wt, add...)
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
	return swRev(t, wt, "HEAD")
}

// tsMoveTrunk is a person moving trunk's branch to a commit that descends from
// where it is: a fast-forward in the main checkout, with no session.
func tsMoveTrunk(t *testing.T, main, commit string) {
	t.Helper()
	swGit(t, main, "merge", "--ff-only", "--quiet", commit)
	if got := ldTrunk(t, main); got != commit {
		t.Fatalf("precondition: trunk is at %s after the move, want %s", got, commit)
	}
}

// tsAnswer is what a command said: its exit code, the code its refusal ended
// with ("" for none), and everything it printed.
type tsAnswer struct {
	code int
	sdlc string
	out  string
}

func tsAnswerOf(r result) tsAnswer {
	out := r.stdout + r.stderr
	a := tsAnswer{code: r.code, out: out}
	if all := tsCodeLine.FindAllStringSubmatch(out, -1); len(all) > 0 {
		a.sdlc = all[len(all)-1][1]
	}
	return a
}

// tsRun is a command of the session in the story's worktree.
func tsRun(t *testing.T, wt string, args ...string) tsAnswer {
	t.Helper()
	return tsAnswerOf(wtRun(t, wt, ldSession, "", args...))
}

// tsCommitGate is the session running the commit gate.
func tsCommitGate(t *testing.T, wt string) tsAnswer {
	t.Helper()
	return tsRun(t, wt, "gate", "commit", "pass", "--note", "committed")
}

// tsRefused holds a command to being refused with code, naming the files. It
// stops the test where it is not, because what a test goes on to ask of the
// state a refusal leaves means nothing otherwise.
func tsRefused(t *testing.T, what string, a tsAnswer, code string, named ...string) {
	t.Helper()
	if a.code == 0 || a.sdlc != code {
		t.Fatalf("%s exited %d with %q, want it refused with %s:\n%s", what, a.code, a.sdlc, code, a.out)
	}
	for _, f := range named {
		if !wtNames(a.out, f) {
			t.Fatalf("%s does not name %s:\n%s", what, f, a.out)
		}
	}
}

// tsSameAnswer holds AC-1 to its words: the command, with trunk on a commit of
// the story's own, gives the exit code, the refusal and the file names that it
// gave before trunk moved, and takes nothing for trunk's.
func tsSameAnswer(t *testing.T, what string, before, after tsAnswer, files ...string) {
	t.Helper()
	if after.code != before.code || after.sdlc != before.sdlc {
		t.Errorf("%s exits %d with %q where trunk is on the story's own commit, and exited %d with %q before trunk moved:\n%s",
			what, after.code, after.sdlc, before.code, before.sdlc, after.out)
	}
	for _, f := range files {
		if wtNames(before.out, f) != wtNames(after.out, f) {
			t.Errorf("%s names %s: %v before trunk moved and %v after:\n%s", what, f, wtNames(before.out, f), wtNames(after.out, f), after.out)
		}
	}
	if strings.Contains(after.out, "accepted as") {
		t.Errorf("%s says it accepted a test file as trunk's, and the file is the story's:\n%s", what, after.out)
	}
}

// tsNothingTaken holds the record to AC-1: no test file was taken for trunk's.
func tsNothingTaken(t *testing.T, main string) {
	t.Helper()
	if got, took := otTrunkTests(t, main); took {
		t.Errorf("the record's trunk_tests is %+v, and the story's own test file was taken for trunk's", got)
	}
}

// ---------------------------------------------------------------- AC-1

// tsStep is one commit of the story's own: what is added to it, or everything
// left where none is named.
type tsStep struct {
	message string
	paths   []string
}

// AC-1, at the commit gate: trunk's branch is pointed at a commit of the story's
// own, the first, a middle one, the one before the tip, or the tip. The test
// file that commit added after the freeze, the frozen test from the cut that it
// changed or took away, and the story's own frozen test that it changed are not
// trunk's: the gate gives the answer it gave before trunk moved, SDLC-E0043 for
// a file the freeze does not hold and SDLC-E0025 for a frozen one, names the
// same files, takes nothing for trunk's and writes no trunk_tests.
//
// Each row is tried with trunk on every commit of the story, so that a fix that
// answers for the first commit only, or for the one with the test only, is
// found out. The rows with the hook watching the commits show that evidence the
// hook wrote down adds nothing a person's move can take away.
func TestAC1TrunkOnACommitOfTheStorysOwnLeavesItsTestFilesTheStorysAtTheCommitGate(t *testing.T) {
	t.Parallel()
	later := func(t *testing.T, wt string) { t.Helper(); writeFile(t, wt, otLaterTest, otOwn) }
	for _, row := range []struct {
		name   string
		after  func(t *testing.T, wt string)
		steps  []tsStep
		hooked bool
		code   string
		named  []string
		// brokenFreeze is a frozen file the story changed or took away, which
		// `sdlc status` has to say changed since.
		brokenFreeze bool
	}{
		{name: "a test added after the freeze in the first commit", after: later, code: ldUnfrozenTest, named: []string{otLaterTest},
			steps: []tsStep{{"tests", []string{otInvoiceTest, otLaterTest}}, {"code", nil}}},
		{name: "a test added after the freeze in the first commit, the commits watched by the hook", after: later, hooked: true,
			code: ldUnfrozenTest, named: []string{otLaterTest},
			steps: []tsStep{{"tests", []string{otInvoiceTest, otLaterTest}}, {"code", nil}}},
		{name: "a test added after the freeze in a middle commit", after: later, code: ldUnfrozenTest, named: []string{otLaterTest},
			steps: []tsStep{{"frozen test", []string{otInvoiceTest}}, {"late test", []string{otLaterTest}}, {"code", nil}}},
		{name: "a test added after the freeze in the commit before the tip", after: later, code: ldUnfrozenTest, named: []string{otLaterTest},
			steps: []tsStep{
				{"frozen test", []string{otInvoiceTest}},
				{"code", []string{"billing.go", "refund.go"}},
				{"late test", []string{otLaterTest}},
				{"last file", []string{tsFile}},
			}},
		{name: "a frozen test from the cut changed in the first commit", brokenFreeze: true, code: ldFrozenMoved, named: []string{otTaxTest},
			after: func(t *testing.T, wt string) { t.Helper(); writeFile(t, wt, otTaxTest, otTaxV2) },
			steps: []tsStep{{"tests", []string{otInvoiceTest, otTaxTest}}, {"code", nil}}},
		{name: "a frozen test from the cut taken away in the first commit", brokenFreeze: true, code: ldFrozenMoved, named: []string{otTaxTest},
			after: func(t *testing.T, wt string) {
				t.Helper()
				if err := os.Remove(filepath.Join(wt, filepath.FromSlash(otTaxTest))); err != nil {
					t.Fatal(err)
				}
			},
			steps: []tsStep{{"tests", []string{otInvoiceTest, otTaxTest}}, {"code", nil}}},
		{name: "the story's own frozen test changed in the first commit", brokenFreeze: true, code: ldFrozenMoved, named: []string{otInvoiceTest},
			after: func(t *testing.T, wt string) { t.Helper(); writeFile(t, wt, otInvoiceTest, otOwn) },
			steps: []tsStep{{"tests", []string{otInvoiceTest}}, {"code", nil}}},
	} {
		for pos := range row.steps {
			t.Run("AC-1 "+row.name+", trunk on commit "+string(rune('1'+pos)), func(t *testing.T) {
				s, commits := tsWorked(t, "AC-1 "+row.name, row.after, row.steps, row.hooked)
				files := []string{otLaterTest, otTaxTest, otInvoiceTest}
				before := tsCommitGate(t, s.wt)
				tsRefused(t, "the commit gate with trunk at the cut", before, row.code, row.named...)

				tsMoveTrunk(t, s.main, commits[pos])
				after := tsCommitGate(t, s.wt)
				tsSameAnswer(t, "the commit gate", before, after, files...)
				for _, f := range row.named {
					if !wtNames(after.out, f) {
						t.Errorf("the commit gate does not name %s:\n%s", f, after.out)
					}
				}
				tsNothingTaken(t, s.main)
				if got := swRecord(t, s.main, "US-001").Gates[model.GateCommit].Status; got == model.GatePass {
					t.Error("the commit gate is recorded as passed over a test file the freeze never held")
				}
				if row.brokenFreeze {
					status := decode[statusPayload](t, wtMust(t, s.wt, ldSession, "", "status", "--json"))
					if status.Freeze == nil || status.Freeze.Intact {
						t.Errorf("status reports the freeze as intact where the story changed %v: %+v", row.named, status.Freeze)
					}
				}
			})
		}
	}
}

// AC-1 and the gates before the commit: with the story's commits already on the
// branch, which only a person can have put there, plan, implementation and
// verification are refused with SDLC-E0046 before the freeze is looked at, and
// trunk being on one of those commits changes nothing. The refusal is the one
// the state gave before trunk moved, and not an SDLC-E0043 or SDLC-E0025.
func TestAC1TheGatesBeforeTheCommitAnswerAsBeforeWhenTrunkIsOnACommitOfTheStorysOwn(t *testing.T) {
	t.Parallel()
	const moved = "SDLC-E0046"
	for _, gate := range []model.Gate{model.GatePlan, model.GateImplementation, model.GateVerification} {
		t.Run("AC-1 "+string(gate), func(t *testing.T) {
			main := ldRepository(t, func(root string) { writeFile(t, root, otTaxTest, "package internal\n") })
			otIdentity(t, main)
			wt := swCheckStarted(t, main, "US-001", ldSession, swStarted(t, main, ldSession))
			swReach(t, wt, ldSession, gate)
			satisfy(t, wt, gate)
			writeFile(t, wt, otLaterTest, otOwn)
			first := tsCommit(t, wt, "tests", otLaterTest)
			tsCommit(t, wt, "code")

			args := []string{"gate", string(gate), "pass", "--note", "with a commit on the branch"}
			before := tsRun(t, wt, args...)
			tsRefused(t, "gate "+string(gate)+" with trunk at the cut", before, moved)
			tsMoveTrunk(t, main, first)
			after := tsRun(t, wt, args...)
			tsSameAnswer(t, "gate "+string(gate), before, after, otLaterTest)
			tsNothingTaken(t, main)
		})
	}
}

// tsFinishedWithAHandCommit is a finished story, whose commit gate passed on its
// work, and a commit a person made by hand on the branch after that, carrying a
// test file nobody froze, and the bookkeeper's commit on top of it. The commits
// are the work, the hand commit and the bookkeeper's.
func tsFinishedWithAHandCommit(t *testing.T) (main, wt string, commits []string) {
	t.Helper()
	main, wt, _ = otWalked(t, ldWalk{finished: true, taxTest: true})
	commits = append(commits, swRev(t, wt, "HEAD"))
	commits = append(commits, swCommitFile(t, wt, otLaterTest, otOwn, "a test by hand, after the freeze"))
	commits = append(commits, ldBookkeeping(t, wt))
	return main, wt, commits
}

// AC-1 at the stop, and decision 3: a commit a person made by hand on the
// story's branch is one of the story's own. Trunk is pointed at the story's
// work, at the person's commit, or at the tip; every time the stop is refused
// with SDLC-E0043 naming the test file, ahead of the question of the reviews
// (SDLC-E0029) as it was before trunk moved, and recording the reviews again
// does not let it land. Trunk, the branch and the story are as they were.
func TestAC1TheStopRefusesAHandMadeCommitsTestFileWhereverTrunkIsOnTheStorysCommits(t *testing.T) {
	t.Parallel()
	for pos, name := range []string{"the work", "the hand-made commit", "the tip"} {
		t.Run("AC-1 trunk on "+name, func(t *testing.T) {
			main, wt, commits := tsFinishedWithAHandCommit(t)
			before := tsRun(t, wt, "stop")
			tsRefused(t, "the stop with trunk at the cut", before, ldUnfrozenTest, otLaterTest)

			tsMoveTrunk(t, main, commits[pos])
			snapshot := otSnap(t, main, wt, "CODEMAP.md", otLaterTest)
			after := tsRun(t, wt, "stop")
			tsSameAnswer(t, "the stop", before, after, otLaterTest, otInvoiceTest)
			if !wtNames(after.out, otLaterTest) {
				t.Errorf("the stop does not name %s:\n%s", otLaterTest, after.out)
			}
			if !strings.Contains(after.out, "Nothing landed") {
				t.Errorf("the stop does not say nothing landed:\n%s", after.out)
			}

			ldReviewAgain(t, wt)
			again := tsRun(t, wt, "stop")
			tsSameAnswer(t, "the stop, with the reviews recorded again", before, again, otLaterTest, otInvoiceTest)
			if got := ldTrunk(t, main); got != commits[pos] {
				t.Errorf("trunk is at %s after the refused stops, want it left at %s", got, commits[pos])
			}
			if t.Failed() {
				return // a story that landed has no worktree left to look at
			}
			snapshot.unchanged(t, otSnap(t, main, wt, "CODEMAP.md", otLaterTest), "the refused stops")
			ldStillHeld(t, main, wt)
			tsNothingTaken(t, main)
		})
	}
}

// AC-1 and THREATS 2: trunk on a story's commit, then a commit of trunk's own on
// top of it, and the story put onto that by the stop's own replay or by a rebase
// by hand. The story's commit is hidden under a commit that really is trunk's,
// and a fix that asks only whether the commit the story sits on is trunk's lets
// the test file through. The stop is refused with SDLC-E0043 naming it, from the
// check that comes before any replay, so the branch is not rewritten.
func TestAC1ACommitOfTrunksOwnOnTopOfAStoryCommitDoesNotHideItsTestFile(t *testing.T) {
	t.Parallel()
	for _, byHand := range []bool{false, true} {
		name := map[bool]string{false: "AC-1 the stop alone", true: "AC-1 after a rebase by hand"}[byHand]
		t.Run(name, func(t *testing.T) {
			main, wt, commits := tsFinishedWithAHandCommit(t)
			tsMoveTrunk(t, main, commits[1])
			f := swCommitFile(t, main, "CHANGES.md", "another story's work\n", "another story lands on top")
			if byHand {
				ldRebase(t, wt)
			}
			ldReviewAgain(t, wt)
			snapshot := otSnap(t, main, wt, "CODEMAP.md", otLaterTest)

			a := tsRun(t, wt, "stop")
			tsRefused(t, "the stop", a, ldUnfrozenTest, otLaterTest)
			if strings.Contains(a.out, "Replayed") || strings.Contains(a.out, "accepted as") {
				t.Errorf("the stop replayed the story or took a test file for trunk's:\n%s", a.out)
			}
			if got := ldTrunk(t, main); got != f {
				t.Errorf("trunk is at %s after the refused stop, want it left at %s", got, f)
			}
			if !byHand {
				snapshot.unchanged(t, otSnap(t, main, wt, "CODEMAP.md", otLaterTest), "the refused stop")
			}
			ldStillHeld(t, main, wt)
			tsNothingTaken(t, main)
		})
	}
}

// AC-1 after AC-2 (ANALYSIS case 13): the story's commits are put onto a trunk
// that gained a test of its own, by a rebase by hand, and trunk is then pointed
// at the story's first commit as it now is. The test file the story added after
// its freeze is refused, named, and trunk's test file is not held against the
// story -- before the move and after it.
func TestAC1ATrunkMovedOntoARebasedStoryCommitStillRefusesTheStorysTestAndNotTrunks(t *testing.T) {
	t.Parallel()
	for pos, name := range []string{"the first rebased commit", "the tip"} {
		t.Run("AC-1 trunk on "+name, func(t *testing.T) {
			s, _ := tsWorked(t, "a test added after the freeze in the first commit, then rebased",
				func(t *testing.T, wt string) { t.Helper(); writeFile(t, wt, otLaterTest, otOwn) },
				[]tsStep{{"tests", []string{otInvoiceTest, otLaterTest}}, {"code", nil}}, false)
			swCommitFile(t, s.main, "ledger.go", "package billing\n\n// Ledger is another story's.\ntype Ledger struct{}\n", "another story's code")
			foreign := swCommitFile(t, s.main, otRefundTest, otRefundV1, "another story's test")
			ldRebase(t, s.wt)
			ldReviewAgain(t, s.wt)
			rebased := strings.Fields(swGit(t, s.wt, "rev-list", "--reverse", foreign+"..HEAD"))
			if len(rebased) != 2 {
				t.Fatalf("precondition: %d story commits on trunk's, want 2", len(rebased))
			}

			before := tsCommitGate(t, s.wt)
			tsRefused(t, "the commit gate with trunk at trunk's own commit", before, ldUnfrozenTest, otLaterTest)
			if wtNames(before.out, otRefundTest) {
				t.Fatalf("precondition: the refusal names %s, trunk's:\n%s", otRefundTest, before.out)
			}
			tsMoveTrunk(t, s.main, rebased[pos])
			after := tsCommitGate(t, s.wt)
			tsSameAnswer(t, "the commit gate", before, after, otLaterTest, otRefundTest)
			if wtNames(after.out, otRefundTest) {
				t.Errorf("the refusal names %s, which is trunk's and not held against the story:\n%s", otRefundTest, after.out)
			}
			tsNothingTaken(t, s.main)
		})
	}
}

// AC-1 and THREATS (the evidence lost by the loop itself): the commit gate
// passed, and a gate was reopened after it, which clears what the commit gate
// recorded. The story is reworked, a person adds a test file by hand on the
// branch after the code review, and trunk is pointed at that commit. The commit
// gate refuses it with SDLC-E0043 -- and not for want of the evidence: a record
// that lost it would refuse with the words for a record from before the field,
// and this one has not lost it.
func TestAC1AGateReopenedAfterTheCommitGateDoesNotForgetWhichCommitsAreTheStorys(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, taxTest: true})
	wtMust(t, wt, ldSession, "", "gate", "implementation", "fail", "--note", "reworked")
	writeFile(t, wt, "FIXED.md", "what the rework changed\n")
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	late := swCommitFile(t, wt, otLaterTest, otOwn, "a test by hand, after the freeze")
	swCommitFile(t, wt, "FIXED.md", "what the rework changed, and more\n", "rework")
	ldReviewAgain(t, wt)

	before := tsCommitGate(t, wt)
	tsRefused(t, "the commit gate with trunk at the cut", before, ldUnfrozenTest, otLaterTest)
	tsMoveTrunk(t, main, late)
	after := tsCommitGate(t, wt)
	tsSameAnswer(t, "the commit gate", before, after, otLaterTest)
	if tsPredates.MatchString(after.out) {
		t.Errorf("the refusal speaks of a record from before the field, and this record was written by this binary:\n%s", after.out)
	}
	tsNothingTaken(t, main)
}

// AC-1 against its non-goal: a person moving trunk is not stopped, and a story
// whose commits changed no test file is not refused for it. Trunk is pointed at
// a commit of the story's own that adds only source; once the reviews are
// recorded again for the change after them the stop lands the story, takes
// nothing for trunk's, and lands what is above trunk.
func TestAC1TrunkOnACommitOfTheStorysOwnThatAddsNoTestFileIsNotARefusal(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, taxTest: true})
	honest := swCommitFile(t, wt, "notes.go", "package billing\n\n// notes\n", "a source file by hand")
	ldBookkeeping(t, wt)
	tsMoveTrunk(t, main, honest)
	ldReviewAgain(t, wt)

	landed := otLands(t, main, wt, honest)
	if strings.Contains(landed.said, "accepted as") {
		t.Errorf("the stop took a test file for trunk's where there was none to take:\n%s", landed.said)
	}
	tsNothingTaken(t, main)
}

// AC-1 and THREATS (a landing cut short): the landing moved trunk to the story's
// tip itself and was cut short before it recorded the landing, which leaves its
// mark. The next stop finishes it. Trunk is on the story's tip by the landing's
// own act, the shape AC-1's refusal must not turn into one.
//
// It passes before the fix, and has to go on passing: it holds the fix to
// refusing test files and not to refusing every story whose commits trunk holds.
func TestAC1ALandingCutShortAfterTheMoveOfTrunkIsFinishedByTheNextStop(t *testing.T) {
	t.Parallel()
	main, wt, cut := ldWalkedFrom(t, ldWalk{finished: true, taxTest: true, spike: true})
	tip := ldBookkeeping(t, wt)
	r := lxRecord(t, main)
	r["landing"] = map[string]any{"from": cut, "to": tip}
	wtWriteJSON(t, wtRecordPath(main, "US-001"), r)
	swGit(t, main, "update-ref", "refs/heads/main", tip, cut)

	ldLanded(t, main, wt, tip)
	if got := swRecord(t, main, "US-001"); got.CommitBase != cut || got.Commit != tip {
		t.Errorf("the record names %s..%s as what landed, want the mark's %s..%s", got.CommitBase, got.Commit, cut, tip)
	}
}

// ---------------------------------------------------------------- AC-2

// AC-2: trunk gained commits that were never the story's, and the story is put
// onto them. The test files those commits added, changed or removed are trunk's,
// are said and counted and named, and the record keeps the trunk commit they
// were compared with -- with the line and the record that US-088 gave. This is
// asked of the stop's own replay and of a rebase by hand, for each kind of file;
// every row passes before the fix and has to go on passing.
func TestAC2TestFilesTrunkGainedAreStillTrunksWhereverTheStoryGotOntoThem(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name  string
		trunk func(t *testing.T, main string) string
		files []string
		gone  []string
	}{
		{name: "a new test file", files: []string{otRefundTest},
			trunk: func(t *testing.T, main string) string {
				t.Helper()
				swCommitFile(t, main, "ledger.go", "package billing\n\n// Ledger is another story's.\ntype Ledger struct{}\n", "another story's code")
				return swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			}},
		{name: "a frozen test trunk changed", files: []string{otTaxTest},
			trunk: func(t *testing.T, main string) string {
				t.Helper()
				return swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
			}},
		{name: "a frozen test trunk removed", gone: []string{otTaxTest},
			trunk: func(t *testing.T, main string) string { t.Helper(); return otRemoveOnTrunk(t, main, otTaxTest) }},
		{name: "a new test and a frozen one, in two commits", files: []string{otRefundTest, otTaxTest},
			trunk: func(t *testing.T, main string) string {
				t.Helper()
				swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
				return swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
			}},
	} {
		for _, how := range []string{"the stop's own replay", "a rebase by hand", "a rebase by hand and the commit gate run again"} {
			t.Run("AC-2 "+row.name+", "+how, func(t *testing.T) {
				main, wt, _ := otFinished(t)
				moved := row.trunk(t, main)
				switch how {
				case "a rebase by hand":
					ldRebase(t, wt)
				case "a rebase by hand and the commit gate run again":
					ldRebase(t, wt)
					ldReviewAgain(t, wt)
					gate := tsCommitGate(t, wt)
					if gate.code != 0 {
						t.Fatalf("the commit gate exited %d on a tree whose only new test files are trunk's:\n%s", gate.code, gate.out)
					}
					otSaysAccepted(t, gate.out, append(slices.Clone(row.files), row.gone...)...)
					otRecordTook(t, main, moved, row.files, row.gone)
					cgRetro(t, wt)
				}

				landed := otLands(t, main, wt, moved)
				otNothingReopened(t, main)
				otSaysAccepted(t, landed.said, append(slices.Clone(row.files), row.gone...)...)
				otRecordTook(t, main, moved, row.files, row.gone)
				var event string
				for _, e := range swRecord(t, main, "US-001").Events {
					if e.Type == "trunk_tests" {
						event = e.Message
					}
				}
				if event == "" || !strings.Contains(event, moved[:12]) {
					t.Errorf("the record has no trunk_tests event naming %s: %q", moved[:12], event)
				}
			})
		}
	}
}

// AC-2 and decision 3: a commit a person made by hand on the story's branch is
// the story's own, and says nothing against the test file trunk gained. A story
// with such a commit, and with trunk's test file under it after the stop's
// replay, is landed with the file accepted as trunk's.
func TestAC2ATestFileTrunkGainedIsStillTrunksBesideACommitMadeByHandOnTheBranch(t *testing.T) {
	t.Parallel()
	main, wt, _ := otFinished(t)
	swCommitFile(t, wt, "notes.go", "package billing\n\n// notes\n", "a source file by hand")
	moved := swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
	ldReviewAgain(t, wt)

	landed := otLands(t, main, wt, moved)
	otSaysAccepted(t, landed.said, otRefundTest)
	otRecordTook(t, main, moved, []string{otRefundTest}, nil)
}

// ---------------------------------------------------------------- decision 6

// tsStripped drops from the story's record every top-level key it did not have
// before the fix, by the rule given, and says how many it touched. A record
// from before the field is the same record without them; the other rules are
// the field emptied, malformed or naming a commit git does not have.
func tsStripped(t *testing.T, main string, how func(value any) (any, bool)) int {
	t.Helper()
	record := lxRecord(t, main)
	touched := 0
	for key, value := range record {
		if slices.Contains(tsRecordKeysBefore, key) {
			continue
		}
		touched++
		if replacement, keep := how(value); keep {
			record[key] = replacement
		} else {
			delete(record, key)
		}
	}
	wtWriteJSON(t, wtRecordPath(main, "US-001"), record)
	return touched
}

// tsDropped is the field gone: a record from before it, or one an older sdlc
// saved.
func tsDropped(any) (any, bool) { return nil, false }

// tsPredatesHeld holds a refusal for a record from before the field: the code
// of the refusal it replaces, what it says of the record, and a next step.
func tsPredatesHeld(t *testing.T, a tsAnswer, code string, named string) {
	t.Helper()
	tsRefused(t, "the command on a record from before the field", a, code, named)
	if !tsPredates.MatchString(a.out) {
		t.Errorf("the refusal does not say that the record predates the field the loop now writes:\n%s", a.out)
	}
	_, fix := ldRefusal(t, a.out, code)
	if !strings.Contains(fix, "unfreeze") {
		t.Errorf("the refusal's fix does not say what a person does next:\n%s", fix)
	}
}

// Decision 6: a record written before the fix has no evidence of whose commits
// are whose, and the new binary takes nothing for trunk's from it -- no
// fallback to the rule of today. A story in flight when the fix is released,
// finished and sitting on a trunk that gained a test file, is refused at the
// stop with the code it had before US-088 (SDLC-E0043 for a file the freeze does
// not hold, SDLC-E0025 for a frozen one trunk changed), says that the record
// predates the field, and nothing lands.
func TestAC2ARecordFromBeforeTheFieldTakesNothingForTrunksAndSaysWhy(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name, file, code string
		trunk            func(t *testing.T, main string) string
	}{
		{"a new test file", otRefundTest, ldUnfrozenTest,
			func(t *testing.T, main string) string {
				t.Helper()
				return swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			}},
		{"a frozen test trunk changed", otTaxTest, ldFrozenMoved,
			func(t *testing.T, main string) string {
				t.Helper()
				return swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
			}},
		{"a frozen test trunk removed", otTaxTest, ldFrozenMoved,
			func(t *testing.T, main string) string { t.Helper(); return otRemoveOnTrunk(t, main, otTaxTest) }},
	} {
		for _, how := range []string{"the stop", "a rebase by hand and the commit gate"} {
			t.Run("AC-2 "+row.name+", "+how, func(t *testing.T) {
				main, wt, _ := otFinished(t)
				moved := row.trunk(t, main)
				tsStripped(t, main, tsDropped)
				var a tsAnswer
				var snapshot otSnapshot
				if how == "the stop" {
					snapshot = otSnap(t, main, wt, "CODEMAP.md")
					a = tsRun(t, wt, "stop")
				} else {
					ldRebase(t, wt)
					ldReviewAgain(t, wt)
					a = tsCommitGate(t, wt)
				}
				tsPredatesHeld(t, a, row.code, row.file)
				if strings.Contains(a.out, "accepted as") {
					t.Errorf("the command took a test file for trunk's on a record that cannot say whose it is:\n%s", a.out)
				}
				tsNothingTaken(t, main)
				if how == "the stop" {
					if got := ldTrunk(t, main); got != moved {
						t.Errorf("trunk moved to %s past a record from before the field", got)
					}
					snapshot.unchanged(t, otSnap(t, main, wt, "CODEMAP.md"), "the refused stop")
					ldStillHeld(t, main, wt)
				}
			})
		}
	}
}

// Decision 6, and the cost it is meant to have: a record from before the field
// refuses trunk's test files and nothing else. Trunk moved on by a file that is
// not a test, or not at all, and the stop lands the story as it did.
func TestAC2ARecordFromBeforeTheFieldStillLandsAStoryWhoseTrunkGainedNoTestFile(t *testing.T) {
	t.Parallel()
	for _, moves := range []bool{false, true} {
		name := map[bool]string{false: "AC-2 trunk did not move", true: "AC-2 trunk gained a source file"}[moves]
		t.Run(name, func(t *testing.T) {
			main, wt, _ := otFinished(t)
			moved := ldTrunk(t, main)
			if moves {
				moved = ldMoveTrunk(t, main)
			}
			tsStripped(t, main, tsDropped)

			landed := otLands(t, main, wt, moved)
			if strings.Contains(landed.said, "accepted as") || strings.Contains(landed.said, "SDLC-E") {
				t.Errorf("the stop speaks of test files or a refusal where there was no test file of trunk's:\n%s", landed.said)
			}
		})
	}
}

// THREATS (tampering): the evidence emptied or malformed is no better than none.
// Each is refused, nothing lands, and nothing is taken for trunk's: with the key
// emptied by SDLC-E0043; with a value of the wrong kind the record cannot be read,
// which is a refusal of its own. Nothing here says which top-level key the fix
// added; a row that finds none fails, because then nothing was written to tamper
// with and the story's commits are told apart by nothing.
//
// Evidence that names commits git does not have, or another story's, is not a
// row: after a rebase by hand or the stop's replay the commits the record names
// are not the story's any more, which is the ordinary state AC-2 has to accept.
func TestAC1EvidenceEmptiedOrMalformedIsRefusedLikeEvidenceThatIsGone(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name string
		how  func(any) (any, bool)
		// anyRefusal accepts any SDLC-E code: an unreadable record is refused
		// before the freeze is asked.
		anyRefusal bool
	}{
		{"set to null", func(any) (any, bool) { return nil, true }, false},
		{"set to an empty string", func(any) (any, bool) { return "", true }, false},
		{"set to an empty list", func(any) (any, bool) { return []any{}, true }, false},
		{"set to an empty object", func(any) (any, bool) { return map[string]any{}, true }, false},
		{"set to a value of the wrong kind", func(any) (any, bool) { return 7, true }, true},
	} {
		t.Run("AC-1 the field "+row.name, func(t *testing.T) {
			main, wt, _ := otFinished(t)
			moved := swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			if n := tsStripped(t, main, row.how); n == 0 {
				t.Fatal("the record has no key beyond those it had before the fix: nothing tells the story's commits from trunk's")
			}
			a := tsRun(t, wt, "stop")
			code := ldUnfrozenTest
			if row.anyRefusal {
				code = a.sdlc
			}
			if a.code == 0 || !strings.HasPrefix(a.sdlc, "SDLC-E") || a.sdlc != code {
				t.Fatalf("the stop exited %d with %q on a record whose evidence is tampered with, want a refusal:\n%s", a.code, a.sdlc, a.out)
			}
			if strings.Contains(a.out, "accepted as") {
				t.Errorf("a test file was taken for trunk's on tampered evidence:\n%s", a.out)
			}
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s on tampered evidence", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// ---------------------------------------------------------------- docs

// tsSection is the text of the heading's section on a page: from the heading to
// the next heading of the same or a higher level.
func tsSection(t *testing.T, name, heading string) string {
	t.Helper()
	body := page(t, name)
	start := strings.Index(body, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("docs/%s has no %q heading", name, heading)
	}
	rest := body[start+1+len(heading):]
	level := len(heading) - len(strings.TrimLeft(heading, "#"))
	end := regexp.MustCompile(`(?m)^#{1,` + string(rune('0'+level)) + `} `).FindStringIndex(rest)
	if end == nil {
		return rest
	}
	return rest[:end[0]]
}

// tsBullets is the top-level list items of a page's text, each with its runs of
// white space made one space, so that a sentence the page wraps is one sentence.
func tsBullets(text string) []string {
	var out []string
	for bullet := range strings.SplitSeq(text, "\n- ") {
		out = append(out, strings.Join(strings.Fields(bullet), " "))
	}
	return out
}

// The story owes docs/enforcement.md a "Not held" entry for the one corner no
// evidence reaches: a late test committed by hand directly on the trunk commit
// the story sits on, with trunk fast-forwarded to it before any sdlc command ran
// (decision 5). The words are the docs author's; this holds that the entry is
// there -- a bullet of that section that names a test file, a commit by hand,
// the fast-forward, trunk's and no sdlc command in between -- and that it does
// not tell a person to move trunk.
func TestAC1TheNotHeldListNamesALateTestCommittedByHandAndFastForwardedToTrunk(t *testing.T) {
	t.Parallel()
	section := tsSection(t, "enforcement.md", "### Not held")
	needs := []*regexp.Regexp{
		regexp.MustCompile(`test file`), regexp.MustCompile(`by hand`), regexp.MustCompile(`fast-forward`),
		regexp.MustCompile(`trunk's|as trunk|from trunk`),
		regexp.MustCompile(`no (sdlc )?command|before any (sdlc )?command|without (an? |any )?(sdlc )?command`),
	}
	var entry string
bullets:
	for _, bullet := range tsBullets(section) {
		for _, need := range needs {
			if !need.MatchString(strings.ToLower(bullet)) {
				continue bullets
			}
		}
		entry = bullet
	}
	if entry == "" {
		t.Fatal(`docs/enforcement.md's "Not held" list has no entry for a test file committed by hand and fast-forwarded to trunk before any sdlc command saw it`)
	}
	if !strings.HasPrefix(entry, "**") {
		t.Errorf("the entry does not begin with a bold name, as its siblings do:\n%s", entry)
	}
	for _, recipe := range []string{"git merge --ff-only", "git branch -f", "update-ref", "git reset"} {
		if strings.Contains(entry, recipe) {
			t.Errorf("the entry reads as a recipe for moving trunk (%q):\n%s", recipe, entry)
		}
	}
}

// docs/the-loop.md says a test file the story's own commits added, changed or
// took away is refused as it always was. It was not, where trunk's branch is on
// one of them, and after this story it is: the sentence has to say so, and that
// a commit made by hand on the branch, and one a rebase made again, are among
// the story's own (decision 3).
func TestAC1TheLoopPageSaysTheStorysOwnCommitsStayTheStorysWhereTrunkIsOnOne(t *testing.T) {
	t.Parallel()
	var para string
	for _, bullet := range tsBullets(page(t, "the-loop.md")) {
		if strings.Contains(bullet, "own commits added, changed or took away") {
			para = bullet
		}
	}
	if para == "" {
		t.Fatal(`docs/the-loop.md no longer says what happens to a test file the story's own commits added, changed or took away`)
	}
	lower := strings.ToLower(para)
	if !regexp.MustCompile(`(took away|taken away)[^.]*\btrunk\b[^.]*\bone of\b|(took away|taken away)[^.]*\bone of\b[^.]*\btrunk\b`).MatchString(lower) {
		t.Errorf("the page does not say that the refusal holds where trunk's branch is on one of the story's own commits:\n%s", para)
	}
	if !strings.Contains(lower, "by hand") || !regexp.MustCompile(`made again|again`).MatchString(lower) {
		t.Errorf("the page does not say that a commit made by hand, and a commit a rebase made again, are the story's own:\n%s", para)
	}
}

// Decision 6 in the troubleshooting page: SDLC-E0043 and SDLC-E0025 each say
// what the refusal for a record from before the field is, and what a person
// does about it.
func TestAC2TheTroubleshootingPageExplainsTheRefusalForARecordFromBeforeTheField(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"SDLC-E0043", "SDLC-E0025"} {
		section := tsSection(t, "troubleshooting.md", "### "+code)
		if !tsPredates.MatchString(section) {
			t.Errorf("docs/troubleshooting.md's %s section does not say what the refusal for a record from before the field means", code)
		}
		if !strings.Contains(section, "unfreeze") {
			t.Errorf("docs/troubleshooting.md's %s section does not say that a person unfreezes and freezes again", code)
		}
	}
}
