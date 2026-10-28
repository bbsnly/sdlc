package cli

// US-090: the conditions US-088's design reviews added to the landing after
// its tests were frozen, each held here by a test. Nothing in this file is
// about a change to what the landing does: every case is one the code already
// answers, and the test is what fails when the condition is taken out.
//
// The stop's own replay is trusted for one tip on one trunk commit and no
// other (AC-1); both of its line-for-line comparisons are asked (AC-2); a
// review recorded after the commit gate leaves no tree to read an approval
// against (AC-3); an attribute nobody committed does not join a conflict
// (AC-4); a frozen test is trunk's only where the freeze holds it as the cut
// has it, and gone only where trunk removed it (AC-5); a worktree taken away
// before the story landed is said to be that (AC-7); a conflict's fix has one
// wording (AC-8); a gate that does not check the freeze leaves trunk_tests as
// it was (AC-9); and a directory against a file, and a rename against a
// delete, are conflicts that name their paths (AC-10).
//
// The AC numbers in this file are US-090's. AC-6 is in test/e2e, because it
// asks the hook, and AC-11 and the git half of AC-4 are in internal/gitx.
// Everything here reads what a caller can see, as landing_ontrunk_test.go
// does, and uses its helpers. The ones added here carry an hc prefix, for held
// conditions.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	hcConflict = "SDLC-E0075"

	// hcOneWording is how the fix of a stop that met a conflict sends the story
	// to a person: the assistant escalates, and the stop itself does not.
	hcOneWording = `An assistant does not resolve it: it hands the story to a person with "sdlc escalate" and stops`
)

// hcDoor is a command that fails on a tree holding CHANGES.md, the file
// another story lands in these tests, until the door is opened: a file outside
// the repository, so that opening it changes no tree.
func hcDoor(t *testing.T) (command string, open func()) {
	t.Helper()
	door := filepath.Join(wtTempDir(t), "open")
	return "test ! -f CHANGES.md || test -f " + suSh(door), func() {
		t.Helper()
		if err := os.WriteFile(door, []byte("open\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// hcFlat is text with every run of white space made one space, so that a
// sentence is found whatever line it was broken on.
func hcFlat(text string) string { return strings.Join(strings.Fields(text), " ") }

// hcReplayed holds the record to carrying the mark of a stop's own replay that
// put the story onto the trunk commit onto and left it at the worktree's HEAD,
// and returns that tip. Each AC-1 case begins from it: without the mark the
// case is another one.
func hcReplayed(t *testing.T, main, wt, onto string) string {
	t.Helper()
	tip := swRev(t, wt, "HEAD")
	mark := swRecord(t, main, "US-001").Replay
	if mark == nil {
		t.Fatalf("precondition: the record carries no mark of a replay after the stop that put the story onto %s", onto[:12])
	}
	if mark.To != tip || mark.Onto != onto {
		t.Fatalf("precondition: the replay mark is %+v, want the story left at %s on %s", *mark, tip, onto)
	}
	return tip
}

// hcNotLanded holds a refused stop to having moved nothing: trunk where it
// was, and the story still its session's.
func hcNotLanded(t *testing.T, main, wt, trunk, why string) {
	t.Helper()
	if got := ldTrunk(t, main); got != trunk {
		t.Errorf("trunk moved from %s to %s %s", trunk[:12], got, why)
	}
	ldStillHeld(t, main, wt)
}

// ---------------------------------------------------------------- AC-1

// AC-1: the mark of a stop's own replay names the commit that replay made. A
// commit the session makes on top of it afterwards is not that commit, and
// the reviews do not stand for it because of the mark: SDLC-E0029, and nothing
// lands. The story is put onto the trunk that moved, a command fails on that
// tree, the session commits a change, and the command passes on what it then
// has: only the reviews stand between that change and trunk.
func TestAC1ACommitMadeOnTopOfTheStopsOwnReplayIsNotTakenForTheReplayedTip(t *testing.T) {
	t.Parallel()
	command, open := hcDoor(t)
	main := lxRepository(t, map[string]string{"test": command}, nil)
	otIdentity(t, main)
	wt := ldFinished(t, main)
	ldBookkeeping(t, wt)
	moved := ldMoveTrunk(t, main)
	swRefused(t, ldCheckFailed, wt, ldSession, "stop")
	replayed := hcReplayed(t, main, wt, moved)

	open()
	tip := lxChangeAfterTheReviews(t, wt)
	if tip == replayed {
		t.Fatal("precondition: the session's commit did not move the story's tip")
	}
	for _, stop := range []string{"the stop", "a second stop"} {
		out := swRefused(t, ldStale, wt, ldSession, "stop")
		said, fix := ldRefusal(t, out, ldStale)
		suSays(t, said+fix, "story/US-001", "review")
		hcNotLanded(t, main, wt, moved, "with a commit nobody reviewed, made after "+stop+"'s own replay")
	}
	if got, _ := otAt(t, main, "refs/heads/main", "refund.go"); got != "" {
		t.Errorf("trunk holds refund.go %q, and nothing of the story's has landed", got)
	}

	// Reviewed as it now is, it lands: the refusal was for the reviews alone.
	ldReviewAgain(t, wt)
	landed := otLands(t, main, wt, moved)
	if got, _ := otAt(t, main, landed.tip, "refund.go"); !strings.Contains(got, "Amount int") {
		t.Errorf("trunk has refund.go %q, want the change that was reviewed again", got)
	}
}

// AC-1 in a tier that waits for a person: the Gate 8 approval does not stand
// because of the mark either. The commit made on top of the stop's own replay
// is refused for its reviews first, SDLC-E0029, and once they are recorded
// again, with SDLC-E0037 until a person approves the work as it now is.
func TestAC1InATierThatWaitsACommitMadeOnTopOfTheStopsOwnReplayNeedsItsReviewsAndApprovalAgain(t *testing.T) {
	t.Parallel()
	main, wt, moved, open := hcApprovedAndReplayed(t)
	approvals := len(swRecord(t, main, "US-001").Approvals)

	open()
	lxChangeAfterTheReviews(t, wt)
	swRefused(t, ldStale, wt, ldSession, "stop")
	hcNotLanded(t, main, wt, moved, "on the reviews of the work before the session changed it")

	ldReviewAgain(t, wt)
	out := swRefused(t, ldNotApproved, wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	suSays(t, fix, `"sdlc approve US-001"`, worktreeIn("US-001"))
	hcNotLanded(t, main, wt, moved, "on the approval of the work before the session changed it")
	if got := len(swRecord(t, main, "US-001").Approvals); got != approvals {
		t.Errorf("the record has %d approvals, want the %d it had: nobody approved the change", got, approvals)
	}
}

// hcApprovedAndReplayed is US-001 in a tier that waits for a person, approved
// at Gate 8 and finished, which a stop then put onto the trunk that moved and
// refused for a command that fails there: the record carries the mark of that
// replay, and the approval stood across it. It returns the trunk commit the
// story was put onto, and what lets the command pass.
func hcApprovedAndReplayed(t *testing.T) (main, wt, moved string, open func()) {
	t.Helper()
	command, open := hcDoor(t)
	commands := ldPassing()
	commands["test"] = command
	main = ldRepository(t, func(root string) {
		ldTier(t, root, "high")
		setConfigOnDisk(t, root, "commands", commands)
		ldThresholds(t, root)
	})
	otIdentity(t, main)
	wt = ldStarted(t, main)
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, "")
	ldBookkeeping(t, wt)
	moved = ldMoveTrunk(t, main)
	swRefused(t, ldCheckFailed, wt, ldSession, "stop")
	hcReplayed(t, main, wt, moved)
	return main, wt, moved, open
}

// AC-1, the approval alone: the Gate 8 approval stands across the stop's own
// replay for the tip that replay made, and for no other because the record
// carries a mark. Here the tip moves after the replay with the story's change
// the very one that was reviewed, so no review is asked for again and none is
// recorded: the reviewed tree the approval was given for is still the one the
// record names. Only the tip says the work is not what the stop made. The
// commit is made again with another message, or a person rebases the story by
// hand onto a trunk that moved once more; either way the stop is refused with
// SDLC-E0037, as rebased, and nothing lands on the approval the story had.
func TestAC1InATierThatWaitsTheApprovalDoesNotStandForATipTheReplayMarkDoesNotName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// move changes the story's tip and returns where trunk is then.
		move func(t *testing.T, main, wt, moved string) string
	}{
		{"AC-1 the tip made again with another message", func(t *testing.T, _, wt, moved string) string {
			t.Helper()
			swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "--amend", "-m", "docs: map US-001, said another way")
			return moved
		}},
		{"AC-1 rebased by hand onto a trunk that moved again", func(t *testing.T, main, wt, _ string) string {
			t.Helper()
			again := swCommitFile(t, main, "NOTES.md", "a second story's work\n", "a second story lands")
			ldRebase(t, wt)
			return again
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt, moved, open := hcApprovedAndReplayed(t)
			replayed := swRev(t, wt, "HEAD")
			lines := swGitRaw(t, wt, "diff", moved, replayed)
			reviews, approvals := len(swRecord(t, main, "US-001").Reviews), len(swRecord(t, main, "US-001").Approvals)

			open()
			trunk := tc.move(t, main, wt, moved)
			tip := swRev(t, wt, "HEAD")
			if tip == replayed {
				t.Fatal("precondition: the story's tip is still the one the stop's replay made")
			}
			if got := swGitRaw(t, wt, "diff", trunk, tip); got != lines {
				t.Fatalf("precondition: the story's change on trunk is no longer the one that was reviewed:\n%s\nwas\n%s", got, lines)
			}
			if mark := swRecord(t, main, "US-001").Replay; mark == nil || mark.To != replayed {
				t.Fatalf("precondition: the record's replay mark is %+v, want it still naming %s", mark, replayed)
			}

			for _, stop := range []string{"the stop", "a second stop"} {
				out := swRefused(t, ldNotApproved, wt, ldSession, "stop")
				said, fix := ldRefusal(t, out, ldNotApproved)
				suSays(t, hcFlat(said), `"US-001" has not been approved by a person since it was rebased`)
				suSays(t, fix, `"sdlc approve US-001"`, worktreeIn("US-001"))
				if strings.Contains(out, ldStale) {
					t.Errorf("%s asks for the reviews again, and the change is the one that was reviewed:\n%s", stop, out)
				}
				hcNotLanded(t, main, wt, trunk, "at "+stop+", on the approval of a tip the story is no longer at")
			}
			r := swRecord(t, main, "US-001")
			if len(r.Reviews) != reviews || len(r.Approvals) != approvals {
				t.Errorf("the record has %d reviews and %d approvals, want the %d and %d it had: the case is that nothing was recorded since",
					len(r.Reviews), len(r.Approvals), reviews, approvals)
			}
		})
	}
}

// AC-1: the mark names the trunk commit the story was put onto, and stands
// only while the change is measured from that commit. A person takes trunk
// back by one commit after the stop's own replay; the story's tip is the one
// the mark names, and what would land now carries a commit that is trunk's no
// longer. The mark does not make that the reviewed change: SDLC-E0029, and
// trunk stays where the person put it.
func TestAC1TheReplayMarkDoesNotStandOnceTrunkIsNoLongerTheCommitItNames(t *testing.T) {
	t.Parallel()
	command, open := hcDoor(t)
	main := lxRepository(t, map[string]string{"test": command}, nil)
	otIdentity(t, main)
	wt := ldFinished(t, main)
	ldBookkeeping(t, wt)
	between := ldMoveTrunk(t, main)
	moved := swCommitFile(t, main, "NOTES.md", "a second story's work\n", "a second story lands")
	swRefused(t, ldCheckFailed, wt, ldSession, "stop")
	replayed := hcReplayed(t, main, wt, moved)

	open()
	swGit(t, main, "reset", "--quiet", "--hard", between)
	if got := ldTrunk(t, main); got != between {
		t.Fatalf("precondition: trunk is at %s, want it taken back to %s", got, between)
	}
	for _, stop := range []string{"the stop", "a second stop"} {
		swRefused(t, ldStale, wt, ldSession, "stop")
		hcNotLanded(t, main, wt, between, "at "+stop+", with a commit that is not the story's among what would land")
		if got := swRev(t, wt, "HEAD"); got != replayed {
			t.Fatalf("the story's tip moved from %s to %s, and the case is the tip the mark names", replayed, got)
		}
	}
	if _, there := otAt(t, main, "refs/heads/main", "NOTES.md"); there {
		t.Error("trunk holds NOTES.md again, which the person took off it")
	}
}

// AC-1 (US-088's G5): a trunk that already holds the story's tip is not a
// later trunk commit the story sits on. A person brought trunk forward to the
// story's branch before the story's commit gate; the test file the story
// committed after its freeze is then in trunk's history, and is still the
// story's own: SDLC-E0043 by name at the commit gate, and the record says
// nothing was taken for trunk's.
func TestAC1ATrunkBroughtForwardToTheStorysTipDoesNotMakeTheStorysOwnTestFileTrunks(t *testing.T) {
	t.Parallel()
	main, wt, cut := otWalked(t, ldWalk{taxTest: true})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	writeFile(t, wt, otLaterTest, otOwn)
	commitEverything(t, wt)
	// Reviewed as committed, so that the test file is all that stands in the
	// gate's way.
	ldReviewAgain(t, wt)
	tip := swRev(t, wt, "HEAD")
	swGit(t, main, "merge", "--quiet", "--ff-only", tip)
	if got := ldTrunk(t, main); got != tip || tip == cut {
		t.Fatalf("precondition: trunk is at %s, want it brought forward from %s to the story's tip %s", got, cut, tip)
	}
	if _, there := otAt(t, main, "refs/heads/main", otLaterTest); !there {
		t.Fatalf("precondition: trunk does not hold %s, the story's own test file", otLaterTest)
	}

	r := wtRun(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "with a test nobody froze, which trunk now holds")
	said := r.stdout + r.stderr
	if r.code == 0 || !strings.Contains(said, ldUnfrozenTest) || !wtNames(said, otLaterTest) {
		t.Fatalf("gate commit pass exited %d, want SDLC-E0043 naming %s:\n%s", r.code, otLaterTest, said)
	}
	if strings.Contains(said, "accepted as") {
		t.Errorf("the gate says it took a test file for trunk's:\n%s", said)
	}
	if got := swRecord(t, main, "US-001").Gates[model.GateCommit].Status; got == model.GatePass {
		t.Error("the commit gate is recorded as passed")
	}
	if got, took := otTrunkTests(t, main); took {
		t.Errorf("the record's trunk_tests is %+v, and the only test file trunk gained is the story's own", got)
	}
}

// AC-1 (G5, at the commit gate's own measure): the same rule says where the
// story's change is measured from. With trunk brought forward to the story's
// tip before its commit gate, the tip is not a later trunk commit the story
// sits on: the change is still measured from the commit the story was cut
// from, the range the gate prints and records is the story's own commits, and
// the stop then finds trunk holding them and records that range as what
// landed. Measured from the tip, the story would have no change at all.
func TestAC1ATrunkBroughtForwardToTheStorysTipLeavesTheChangeMeasuredFromTheCut(t *testing.T) {
	t.Parallel()
	main, wt, cut := otWalked(t, ldWalk{taxTest: true})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	commitEverything(t, wt)
	tip := swRev(t, wt, "HEAD")
	swGit(t, main, "merge", "--quiet", "--ff-only", tip)
	if got := ldTrunk(t, main); got != tip || tip == cut {
		t.Fatalf("precondition: trunk is at %s, want it brought forward from %s to the story's tip %s", got, cut, tip)
	}
	reviewed, err := gitx.Fingerprint(t.Context(), wt, cut, tip, leftOut...)
	if err != nil {
		t.Fatal(err)
	}

	r := wtMust(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed, and trunk already holds it")
	said := r.stdout + r.stderr
	if want := "committed on story/US-001 in " + cut[:12] + ".." + tip[:12]; !strings.Contains(said, want) {
		t.Errorf("the commit gate does not say %q:\n%s", want, said)
	}
	if strings.Contains(said, "counts as rebased") {
		t.Errorf("the commit gate speaks of a rebase, and the story sits on the commit it was cut from:\n%s", said)
	}
	record := swRecord(t, main, "US-001")
	if record.ChangeBase != cut {
		t.Errorf("the record measures the story's change from %q, want %s, the commit it was cut from", record.ChangeBase, cut)
	}
	if record.Change != reviewed {
		t.Errorf("the record's change is %q, want the fingerprint of the story's own commits, %s", record.Change, reviewed)
	}
	if record.CommitBase != cut || record.Commit != tip {
		t.Errorf("the record names %s..%s as the story's commits, want %s..%s", record.CommitBase, record.Commit, cut, tip)
	}
}

// AC-1 (G5, where a reviewer is brought back): the same rule says which paths
// the story has committed. A story that commits a file under a path that
// brings the security reviewer back is asked for that review, and goes on
// being asked once a person has brought trunk forward to the story's tip: the
// tip is not a later trunk commit the story sits on, with the file trunk's
// and none of the story's.
func TestAC1ATrunkBroughtForwardToTheStorysTipDoesNotHideWhatTheStoryCommitted(t *testing.T) {
	t.Parallel()
	const own = "internal/hook/own.go"
	main := ldRepository(t, func(root string) { setConfigOnDisk(t, root, "staffing", stfSection()) })
	otIdentity(t, main)
	cut := ldTrunk(t, main)
	wt := swCheckStarted(t, main, "US-001", ldSession, swStarted(t, main, ldSession))
	chdir(t, wt)
	setenv(t, store.SessionEnv, ldSession)
	mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	stfSet(t, stfFull())
	stfAnalysed(t, false)
	mustRun(t, "staffing", "amend", "security", "--left-out", "--reason", "nothing here crosses a trust boundary")
	stfThrough(t, wt, model.GateImplementation, "security")
	ldWork(t, wt)
	stfThrough(t, wt, "", "security")
	if by := stfShow(t, "--gate", "code_review").agent(t, "security").BroughtBackBy; by != "" {
		t.Fatalf("precondition: the security reviewer is brought back by %q before the story committed anything there", by)
	}
	tip := swCommitFile(t, wt, own, "package hook\n", "the story's own hook")
	if by := stfShow(t, "--gate", "code_review").agent(t, "security").BroughtBackBy; by != own {
		t.Fatalf("precondition: the security reviewer is brought back by %q, want the story's own %s", by, own)
	}

	swGit(t, main, "merge", "--quiet", "--ff-only", tip)
	if got := ldTrunk(t, main); got != tip || tip == cut {
		t.Fatalf("precondition: trunk is at %s, want it brought forward from %s to the story's tip %s", got, cut, tip)
	}
	if by := stfShow(t, "--gate", "code_review").agent(t, "security").BroughtBackBy; by != own {
		t.Errorf("with trunk at the story's tip the security reviewer is brought back by %q, want the story's own %s still", by, own)
	}
}

// ---------------------------------------------------------------- AC-2

// The shape is a file in which a block of two lines can be read as added in
// two places, because the line before it is the same as its last: git reads it
// one way where the line above is indented less than the block, and the other
// way where it is indented the same. One side adds the block and the other
// indents the line above, two lines from where the block goes in, so the two
// apply with no conflict.
const (
	hcShapeFile = "shape.txt"
	hcShapeBase = "top\n\nzed\n    }\n    tail\n"
	hcBlockIn   = "top\n\nzed\n    }\n    new\n    }\n    tail\n"
	hcIndented  = "top\n\n    zed\n    }\n    tail\n"
)

// AC-2: across the stop's own replay the story's change is compared line for
// line from both sides: the story's own change before and after, and trunk's
// change before and after the story's was put on it. Each comparison is shown
// failing on its own, the other holding: the reviews are stale either way,
// SDLC-E0029, nothing lands, and the record carries no mark of a replay.
func TestAC2EitherLineForLineComparisonFailingOnItsOwnMakesTheReviewsStale(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		story, trunk string
		// storys and trunks are whether each comparison holds: the story's
		// change read on the old trunk and on the new, and trunk's change read
		// without the story and with it.
		storys, trunks bool
	}{
		{name: "AC-2 the story's own change reads differently on the new trunk, and trunk's reads the same",
			story: hcBlockIn, trunk: hcIndented, storys: false, trunks: true},
		{name: "AC-2 trunk's change reads differently under the story, and the story's own reads the same",
			story: hcIndented, trunk: hcBlockIn, storys: true, trunks: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main := lxRepository(t, nil, nil)
			otIdentity(t, main)
			fork := swCommitFile(t, main, hcShapeFile, hcShapeBase, "the shape")
			wt := ldStarted(t, main)
			ldWork(t, wt)
			writeFile(t, wt, hcShapeFile, tc.story)
			swPass(t, wt, ldSession, model.GateImplementation, "")
			tip := ldBookkeeping(t, wt)
			moved := swCommitFile(t, main, hcShapeFile, tc.trunk, "another story's shape")

			out := swRefused(t, ldStale, wt, ldSession, "stop")
			said, _ := ldRefusal(t, out, ldStale)
			suSays(t, hcFlat(said), "line for line")
			after := swRev(t, wt, "HEAD")
			if after == tip {
				t.Fatalf("precondition: the story is still at %s, so its commits were not put onto trunk:\n%s", tip, out)
			}
			if got, _ := otAt(t, wt, after, hcShapeFile); got != strings.Replace(hcBlockIn, "zed\n", "    zed\n", 1) {
				t.Fatalf("precondition: the replay made %s\n%s\nwant both sides' changes with no conflict", hcShapeFile, got)
			}
			same := func(a, b [2]string) bool {
				t.Helper()
				one, err := gitx.LinesFingerprint(t.Context(), wt, a[0], a[1], leftOut...)
				if err != nil {
					t.Fatal(err)
				}
				other, err := gitx.LinesFingerprint(t.Context(), wt, b[0], b[1], leftOut...)
				if err != nil {
					t.Fatal(err)
				}
				return one == other
			}
			storys := same([2]string{fork, tip}, [2]string{moved, after})
			trunks := same([2]string{fork, moved}, [2]string{tip, after})
			if storys != tc.storys || trunks != tc.trunks {
				t.Fatalf("precondition: the story's change compares equal: %v, trunk's: %v; the case is %v and %v, one failing alone",
					storys, trunks, tc.storys, tc.trunks)
			}

			hcNotLanded(t, main, wt, moved, "with a change that is not, line for line, the reviewed one")
			if mark, ok := lxRecord(t, main)["replay"]; ok {
				t.Errorf("the record carries a replay mark, %v, for a result that was refused", mark)
			}
			// A second stop judges the tip the first left by the change the
			// commit gate recorded, and that is not it either.
			swRefused(t, ldStale, wt, ldSession, "stop")
			hcNotLanded(t, main, wt, moved, "at a second stop")
		})
	}
}

// ---------------------------------------------------------------- AC-3

// AC-3: a review is stamped with the tree as it is when it is recorded, so one
// recorded after the commit gate last passed could choose which approval the
// landing reads. The landing then takes no tree as the reviewed one: a story
// in a tier that waits, approved at Gate 8, with its reviews recorded again
// after its commit gate -- on the very tree that was approved -- and put onto
// a trunk that moved by the stop itself, is refused with SDLC-E0037. Without
// those later reviews the same story lands on its approval, which
// TestAC8AStoryInATierThatPausesLandsOnItsGate8ApprovalAfterTrunkMoved holds.
func TestAC3AReviewRecordedAfterTheCommitGatePassedLeavesNoTreeToReadTheApprovalAgainst(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{tier: "high"})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, "")
	ldReviewAgain(t, wt)
	tip := ldBookkeeping(t, wt)
	approvals := len(swRecord(t, main, "US-001").Approvals)
	moved := ldMoveTrunk(t, main)

	out := swRefused(t, ldNotApproved, wt, ldSession, "stop")
	said, fix := ldRefusal(t, out, ldNotApproved)
	suSays(t, said, `"US-001" has not been approved by a person`)
	suSays(t, fix, `"sdlc approve US-001"`, worktreeIn("US-001"))
	hcNotLanded(t, main, wt, moved, "on an approval read against a tree a later review named")
	// The refusal is of the stop's own replay, not of a change or a rebase by
	// hand: the story sits on trunk, and the record carries the mark.
	if got := hcReplayed(t, main, wt, moved); got == tip {
		t.Errorf("the story is still at %s: the stop did not put it onto trunk", tip)
	}
	r := swRecord(t, main, "US-001")
	if got := len(r.Approvals); got != approvals {
		t.Errorf("the record has %d approvals, want the %d it had", got, approvals)
	}
	if pending, ok := r.PendingEscalation(); ok {
		t.Errorf("the stop opened a question to a person itself: %+v", pending)
	}
	swRefused(t, ldNotApproved, wt, ldSession, "stop")
	hcNotLanded(t, main, wt, moved, "at a second stop")
}

// ---------------------------------------------------------------- AC-4

// hcUnionTree is a tree, written to the repository's objects and to no branch,
// whose .gitattributes gives every path git's union merge.
func hcUnionTree(t *testing.T, main string) string {
	t.Helper()
	file := filepath.Join(wtTempDir(t), "gitattributes")
	if err := os.WriteFile(file, []byte("* merge=union\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	status := swGitRaw(t, main, "status", "--porcelain")
	blob := swGit(t, main, "hash-object", "-w", "--", file)
	// Through the index, which is put back as it was: a tree is all git takes
	// for attr.tree, and making one needs nothing else that a branch has.
	swGit(t, main, "update-index", "--add", "--cacheinfo", "100644,"+blob+",.gitattributes")
	tree := swGit(t, main, "write-tree")
	swGit(t, main, "update-index", "--force-remove", "--", ".gitattributes")
	if got := swGitRaw(t, main, "status", "--porcelain"); got != status {
		t.Fatalf("precondition: making the tree left the main checkout changed:\n%s\nwas\n%s", got, status)
	}
	return tree
}

// AC-4: an attribute nobody committed does not change how the stop's own
// replay merges a file. The story and trunk changed the same lines of
// billing.go, which is a conflict; an attributes file named by
// core.attributesFile, and a tree named by attr.tree, each give the file
// git's union merge, which would join the two with no conflict and no reviewer
// having seen the result. The stop still meets the conflict: SDLC-E0075 naming
// the file, nothing landed, and the worktree as it was.
func TestAC4AnAttributeNobodyCommittedDoesNotJoinAConflictInTheStopsOwnReplay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		set  func(t *testing.T, main string)
	}{
		{"AC-4 an attributes file named by core.attributesFile", func(t *testing.T, main string) {
			t.Helper()
			file := filepath.Join(wtTempDir(t), "attributes")
			if err := os.WriteFile(file, []byte("* merge=union\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			swGit(t, main, "config", "core.attributesFile", filepath.ToSlash(file))
		}},
		{"AC-4 a tree named by attr.tree", func(t *testing.T, main string) {
			t.Helper()
			swGit(t, main, "config", "attr.tree", hcUnionTree(t, main))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
			tip := swRev(t, wt, "HEAD")
			moved := swCommitFile(t, main, "billing.go", ldBilling+"\n// Ledger is what another story adds.\ntype Ledger struct{}\n", "another story's ledger")
			tc.set(t, main)
			if got := swGit(t, wt, "check-attr", "merge", "--", "billing.go"); !strings.HasSuffix(got, ": union") {
				if strings.Contains(tc.name, "attr.tree") {
					t.Skipf("this git does not read attr.tree, which came with 2.44: billing.go's merge attribute is %q", got)
				}
				t.Fatalf("precondition: billing.go's merge attribute is %q in the worktree, want union", got)
			}
			files := []string{"billing.go", "refund.go", "price list.txt", "CODEMAP.md"}
			before := otSnap(t, main, wt, files...)

			out := swRefused(t, hcConflict, wt, ldSession, "stop")
			if !wtNames(out, "billing.go") {
				t.Errorf("the refusal does not name billing.go, the file in conflict:\n%s", out)
			}
			before.unchanged(t, otSnap(t, main, wt, files...), "the stop that met a conflict an attribute would have joined")
			if got := swRev(t, main, "refs/heads/story/US-001"); got != tip {
				t.Errorf("story/US-001 is at %s after the conflict, want it left at %s", got, tip)
			}
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s with both sides' lines joined by an attribute nobody committed", got)
			}
			if got, _ := otAt(t, main, "refs/heads/main", "billing.go"); strings.Contains(got, "type Invoice") {
				t.Errorf("trunk's billing.go holds the story's lines:\n%s", got)
			}
		})
	}
}

// ---------------------------------------------------------------- AC-5

func hcSum(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// hcRefusedAsBroken holds status to a broken freeze, and `sdlc gate commit
// pass` to SDLC-E0025 naming the frozen test, with the gate not recorded and
// nothing taken for trunk's.
func hcRefusedAsBroken(t *testing.T, main, wt, file, note string) string {
	t.Helper()
	otBrokenFreeze(t, wt)
	r := wtRun(t, wt, ldSession, "", "gate", "commit", "pass", "--note", note)
	said := r.stdout + r.stderr
	if r.code == 0 || !strings.Contains(said, ldFrozenMoved) || !wtNames(said, file) {
		t.Fatalf("gate commit pass exited %d, want SDLC-E0025 naming %s:\n%s", r.code, file, said)
	}
	if strings.Contains(said, "accepted as") {
		t.Errorf("the gate says it took a test file for trunk's:\n%s", said)
	}
	if got := swRecord(t, main, "US-001").Gates[model.GateCommit].Status; got == model.GatePass {
		t.Error("the commit gate is recorded as passed")
	}
	if got, took := otTrunkTests(t, main); took {
		t.Errorf("the record's trunk_tests is %+v, and the frozen test is not trunk's to take", got)
	}
	return said
}

// AC-5: a frozen test is trunk's only where the freeze holds it as the commit
// the story was cut from has it. Here the story's own test author changed
// internal/tax_test.go before the freeze, so the freeze holds that change;
// the change was undone before the story's commit and so never committed,
// trunk changed the file, and the story was rebased: the file on disk is
// trunk's, byte for byte, and the story's commits never touched it. It is
// still not what was frozen, and what was frozen is not what trunk started
// from: a broken freeze in status, and SDLC-E0025 by name at the commit gate.
func TestAC5AFrozenTestTheFreezeDoesNotHoldAsTheCutHasItIsNotTakenForTrunks(t *testing.T) {
	t.Parallel()
	main := ldRepository(t, func(root string) { writeFile(t, root, otTaxTest, otTaxBase) })
	otIdentity(t, main)
	wt := swCheckStarted(t, main, "US-001", ldSession, swStarted(t, main, ldSession))
	swReach(t, wt, ldSession, model.GateTestsFrozen)
	writeFile(t, wt, otTaxTest, otOwn)
	swPass(t, wt, ldSession, model.GateTestsFrozen, model.GateImplementation)
	if got := lockOnDisk(t, wt).Files[otTaxTest]; got != hcSum(otOwn) {
		t.Fatalf("precondition: the freeze holds %s as %q, want the story's own change of it, %s", otTaxTest, got, hcSum(otOwn))
	}
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	writeFile(t, wt, otTaxTest, otTaxBase)
	commitEverything(t, wt)
	moved := swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
	ldRebase(t, wt)
	ldReviewAgain(t, wt)
	if got := wtRead(t, filepath.Join(wt, filepath.FromSlash(otTaxTest))); got != otTaxV2 {
		t.Fatalf("precondition: the worktree's %s is %q, want trunk's copy", otTaxTest, got)
	}
	if changed := swGit(t, wt, "diff", "--name-only", moved, "HEAD", "--", otTaxTest); changed != "" {
		t.Fatalf("precondition: the story's commits change %s, and the case is that only trunk did", otTaxTest)
	}
	if status := swGitRaw(t, wt, "status", "--porcelain", "--", otTaxTest); status != "" {
		t.Fatalf("precondition: %s is not as committed:\n%s", otTaxTest, status)
	}

	hcRefusedAsBroken(t, main, wt, otTaxTest, "with a frozen test that is trunk's copy and not what was frozen")
}

// AC-5: a frozen test that is gone from the worktree is trunk's doing only
// where trunk removed it. Here trunk only changed internal/tax_test.go; the
// story was rebased onto that, and the file was then deleted and the deletion
// never committed, so the story's commits do not touch it. It is not taken as
// removed on trunk: a broken freeze in status, and SDLC-E0025 at the commit
// gate, naming the file as gone.
func TestAC5AFrozenTestGoneFromTheWorktreeIsNotTakenAsRemovedWhereTrunkOnlyChangedIt(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{taxTest: true})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	commitEverything(t, wt)
	moved := swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
	ldRebase(t, wt)
	ldReviewAgain(t, wt)
	if err := os.Remove(filepath.Join(wt, filepath.FromSlash(otTaxTest))); err != nil {
		t.Fatal(err)
	}
	if changed := swGit(t, wt, "diff", "--name-only", moved, "HEAD", "--", otTaxTest); changed != "" {
		t.Fatalf("precondition: the story's commits change %s, and the case is that only trunk did", otTaxTest)
	}
	if _, there := otAt(t, main, moved, otTaxTest); !there {
		t.Fatalf("precondition: trunk has no %s, and the case is that trunk only changed it", otTaxTest)
	}

	said := hcRefusedAsBroken(t, main, wt, otTaxTest, "with a frozen test deleted and not committed")
	if !strings.Contains(said, otTaxTest+" (gone)") {
		t.Errorf("the refusal does not say %s is gone:\n%s", otTaxTest, said)
	}
}

// ---------------------------------------------------------------- AC-7

// AC-7: a story's worktree can be taken away while its stop waits its turn to
// land, by a person as well as by another stop that landed it, and only the
// backlog says which. Taken away with the story not done, the stop is refused
// with SDLC-E0055: it says the worktree is gone and the story has not landed,
// and its fix sends its reader to `sdlc status` and `sdlc doctor` in the main
// checkout and tells an assistant to stop and remove nothing. It never says
// the story landed.
func TestAC7AStopWhoseWorktreeIsGoneBeforeTheStoryLandedSaysSoWithItsCodeAndFix(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("Windows does not take away a directory a running command has open, so the worktree cannot go while its stop waits")
	}
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
	tip := swRev(t, wt, "HEAD")
	moved := ldMoveTrunk(t, main)
	inFront := otLandingInFront(t, main)

	stop := otStopBeside(wt)
	stop.waits(t)
	swGit(t, main, "worktree", "remove", "--force", wt)
	if wtExists(wt) {
		t.Fatalf("precondition: %s is still there after git took the worktree away", wt)
	}
	inFront.Release()

	r := stop.ended(t)
	out := r.stdout + r.stderr
	if r.code == 0 || !strings.Contains(out, ldCheckFailed) {
		t.Fatalf("the stop exited %d, want SDLC-E0055 for a worktree that is gone with the story not landed:\n%s", r.code, out)
	}
	if strings.Contains(out, "landed while this stop waited") {
		t.Errorf("the stop says the story landed, and it did not:\n%s", out)
	}
	said, fix := ldRefusal(t, out, ldCheckFailed)
	suSays(t, hcFlat(said), worktreeIn("US-001")+" is gone", `"US-001" has not landed`, "Nothing landed")
	suSays(t, hcFlat(fix), `"sdlc status"`, `"sdlc doctor"`, "in the main checkout", assistantStops)
	if strings.Contains(fix, "sdlc escalate") {
		t.Errorf("the fix sends the story to a person as a question, and there is nothing to decide but where it stands:\n%s", fix)
	}
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("trunk moved to %s with the story's worktree gone", got)
	}
	if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusDone) {
		t.Error("a story that did not land is done")
	}
	if got := swRev(t, main, "refs/heads/story/US-001"); got != tip {
		t.Errorf("story/US-001 is at %s, want the story's commits left at %s", got, tip)
	}
}

// ---------------------------------------------------------------- AC-8

// AC-8: a stop that meets a conflict has one way of sending the story to a
// person, and the test asks for that one. The stop does not escalate: the
// story is not awaiting_human on the backlog and no question is open on its
// record. Its fix says, in these words, that an assistant does not resolve the
// conflict and hands the story over with "sdlc escalate".
func TestAC8AStopThatMeetsAConflictTellsTheAssistantToEscalateAndDoesNotEscalateItself(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
	swCommitFile(t, main, "billing.go", ldBilling+"\n// Ledger is what another story adds.\ntype Ledger struct{}\n", "another story's ledger")
	was := wtStatuses(t, main)["US-001"]
	events := len(swRecord(t, main, "US-001").Events)

	out := swRefused(t, hcConflict, wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, hcConflict)
	suSays(t, hcFlat(fix), hcOneWording)
	if got := wtStatuses(t, main)["US-001"]; got != was || got == string(model.StatusAwaitingHuman) {
		t.Errorf("the backlog has US-001 as %q after the stop, want it left %q: the stop tells the assistant to escalate, and does not do it", got, was)
	}
	r := swRecord(t, main, "US-001")
	if pending, ok := r.PendingEscalation(); ok {
		t.Errorf("the stop opened a question to a person itself: %+v", pending)
	}
	for _, kind := range swEventsSince(r, events) {
		if strings.Contains(kind, "escalat") {
			t.Errorf("the stop wrote a %s event on the record", kind)
		}
	}
	ldStillHeld(t, main, wt)
}

// ---------------------------------------------------------------- AC-9

// hcTrunkTestsEvents is how many times the record says test files were taken
// for trunk's.
func hcTrunkTestsEvents(t *testing.T, main string) int {
	t.Helper()
	n := 0
	for _, e := range swRecord(t, main, "US-001").Events {
		if e.Type == "trunk_tests" {
			n++
		}
	}
	return n
}

// AC-9: only a gate that checks the freeze says what was taken for trunk's.
// The commit gate of a story rebased onto a trunk that gained a test file
// writes trunk_tests on the record; the retro, which does not check the
// freeze, is then recorded, and the record's trunk_tests is what it was: the
// same commit, the same file, and no further event. The gate says nothing of
// test files either.
func TestAC9AGateThatDoesNotCheckTheFreezeLeavesTheRecordsTrunkTestsAsItWas(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{taxTest: true})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	commitEverything(t, wt)
	moved := swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
	ldRebase(t, wt)
	ldReviewAgain(t, wt)
	wtMust(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed after a rebase")
	before, took := otTrunkTests(t, main)
	if !took {
		t.Fatal("precondition: the commit gate wrote no trunk_tests on the record")
	}
	otRecordTook(t, main, moved, []string{otRefundTest}, nil)
	events := hcTrunkTestsEvents(t, main)

	satisfy(t, wt, model.GateRetro)
	r := wtMust(t, wt, ldSession, "", "gate", "retro", "pass", "--note", "no deviations")
	if said := r.stdout + r.stderr; strings.Contains(said, "test file") {
		t.Errorf("the retro speaks of test files, and it does not check the freeze:\n%s", said)
	}
	if got := swRecord(t, main, "US-001").Gates[model.GateRetro].Status; got != model.GatePass {
		t.Fatalf("precondition: the retro is %q on the record, want pass", got)
	}
	after, took := otTrunkTests(t, main)
	if !took {
		t.Fatalf("the record has no trunk_tests after the retro was recorded; it had %+v", before)
	}
	if !reflect.DeepEqual(after, before) {
		t.Errorf("the record's trunk_tests is %+v after the retro was recorded, want it as it was: %+v", after, before)
	}
	otRecordTook(t, main, moved, []string{otRefundTest}, nil)
	if got := hcTrunkTestsEvents(t, main); got != events {
		t.Errorf("the record has %d trunk_tests events after the retro, want the %d it had", got, events)
	}
}

// ---------------------------------------------------------------- AC-10

// AC-10: two conflicts git reports without a line in conflict. Trunk has a
// directory where the story has a file, and trunk deleted a file the story
// renamed. Each is SDLC-E0075, naming the story's path, with trunk unchanged
// and the worktree and its branch exactly as they were, on the first stop and
// the second.
func TestAC10ADirectoryAgainstAFileAndARenameAgainstADeleteAreConflictsNamedAndLeftAsTheyWere(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// names matches the story's path as the refusal lists it, quoted.
		// Git moves a file aside from a directory of its name, and reports
		// it under the name it was moved to: the story's path, then "~" and
		// a label for the side it came from.
		names *regexp.Regexp
		story func(t *testing.T, wt string)
		trunk func(t *testing.T, main string)
	}{
		{name: "AC-10 trunk has a directory where the story has a file", names: regexp.MustCompile(`^"notes(~[^"/]*)?"$`),
			story: func(t *testing.T, wt string) {
				t.Helper()
				writeFile(t, wt, "notes", "the story's notes\n")
			},
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, "notes/today.md", "another story's notes\n", "another story's notes")
			}},
		{name: "AC-10 trunk deleted a file the story renamed", names: regexp.MustCompile(`^"GUIDE\.md"$`),
			story: func(t *testing.T, wt string) {
				t.Helper()
				swGit(t, wt, "mv", "--", "README.md", "GUIDE.md")
			},
			trunk: func(t *testing.T, main string) {
				t.Helper()
				otRemoveOnTrunk(t, main, "README.md")
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main := lxRepository(t, nil, nil)
			otIdentity(t, main)
			wt := ldStarted(t, main)
			ldWork(t, wt)
			tc.story(t, wt)
			swPass(t, wt, ldSession, model.GateImplementation, "")
			tip := ldBookkeeping(t, wt)
			tc.trunk(t, main)
			moved := ldTrunk(t, main)
			files := []string{"billing.go", "refund.go", "notes", "README.md", "GUIDE.md", "CODEMAP.md"}
			before := otSnap(t, main, wt, files...)

			for _, stop := range []string{"the stop that met the conflict", "a second stop after it"} {
				r := wtRun(t, wt, ldSession, "", "stop")
				out := r.stdout + r.stderr
				if r.code == 0 {
					t.Fatalf("%s landed the story, exiting 0, where its commits do not apply:\n%s", stop, out)
				}
				if !strings.Contains(out, hcConflict) {
					t.Errorf("%s was refused with something other than SDLC-E0075:\n%s", stop, out)
				}
				// The paths in conflict are the ones the refusal lists, and
				// nothing else it says counts: a refusal that listed trunk's
				// notes/today.md would hold the story's "notes" as a
				// substring and name the wrong side.
				_, listed, found := strings.Cut(hcFlat(out), "stopped on a conflict in ")
				listed, _, _ = strings.Cut(listed, "; the rebase was abandoned")
				if !found || !tc.names.MatchString(listed) {
					t.Errorf("%s lists %q as in conflict, want the story's path there, matching %s:\n%s", stop, listed, tc.names, out)
				}
				if strings.Contains(listed, "today.md") {
					t.Errorf("%s lists trunk's path, %q, as in conflict, and the path a person resolves is the story's:\n%s", stop, listed, out)
				}
				before.unchanged(t, otSnap(t, main, wt, files...), stop)
				if got := swRev(t, main, "refs/heads/story/US-001"); got != tip {
					t.Errorf("story/US-001 is at %s after %s, want it left at %s", got, stop, tip)
				}
				if got := ldTrunk(t, main); got != moved {
					t.Errorf("%s moved trunk to %s though the story's commits did not apply", stop, got)
				}
			}
			ldStillHeld(t, main, wt)
		})
	}
}
