package cli

// US-088, after the first design review: the cases the reviewers showed no
// test held, each through the commands and by what a caller can see, as
// landing_ontrunk_test.go's are. The AC numbers are US-088's; the names in
// brackets are the findings (architect B1 to B3, security 1 to 5, the red
// team's V1 to V8).
//
// A design that takes a test file for trunk's because it equals what trunk
// has, with no evidence that trunk changed it; that asks git status whether a
// file is as committed; that measures a change by its lines alone; or that
// lets any approval stand for the one the commit gate asked for, passes
// landing_ontrunk_test.go and fails here.
//
// Helpers carry the ot prefix of that file.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const otTaxBase = "package internal\n"

// What a story's own Gate 3 does to trunk's test file, internal/tax_test.go,
// before the tests are frozen: otOwnGate3 takes one of these.
const (
	// otGate3Writes writes the file again, otOwn.
	otGate3Writes = "writes"
	// otGate3Removes removes it through git, so that the index no longer names
	// the file: the freeze reads every test file git knows of.
	otGate3Removes = "removes"
	// otGate3Renames moves it to otRenamedTest through git.
	otGate3Renames = "renames"
	otRenamedTest  = "internal/duty_test.go"
)

// otOwnGate3 is US-001 in its worktree, in a repository whose trunk holds
// internal/tax_test.go, with what gate3 names having had its way with the
// worktree's tests before they were frozen, as the test author does. The story
// is taken to the implementation with its work written, and through the retro
// where finished is set.
//
// It is a walked template per gate3 and finished, made by the real commands
// once per package run.
func otOwnGate3(t *testing.T, gate3 string, finished bool) (main, wt string) {
	t.Helper()
	main, _ = walkedCopyNoted(t, fmt.Sprintf("trunk's test file, after a Gate 3 that %s it, finished %v", gate3, finished),
		func(t *testing.T, root string) map[string]string {
			t.Helper()
			ldRepositoryAt(t, root, func(root string) { writeFile(t, root, otTaxTest, otTaxBase) })
			otIdentity(t, root)
			wt := swCheckStarted(t, root, "US-001", ldSession, swStarted(t, root, ldSession))
			swReach(t, wt, ldSession, model.GateTestsFrozen)
			switch gate3 {
			case otGate3Writes:
				writeFile(t, wt, otTaxTest, otOwn)
			case otGate3Removes:
				swGit(t, wt, "rm", "--quiet", "--", otTaxTest)
			case otGate3Renames:
				swGit(t, wt, "mv", "--", otTaxTest, otRenamedTest)
			default:
				t.Fatalf("a Gate 3 that %q the test file is not one this knows", gate3)
			}
			swPass(t, wt, ldSession, model.GateTestsFrozen, model.GateImplementation)
			ldWork(t, wt)
			if finished {
				swPass(t, wt, ldSession, model.GateImplementation, "")
			}
			return nil
		})
	wt = swWorktreeOf(main, "US-001")
	chdir(t, wt)
	setenv(t, store.SessionEnv, ldSession)
	return main, wt
}

// otBrokenFreeze holds status to saying the freeze is not intact.
func otBrokenFreeze(t *testing.T, wt string) {
	t.Helper()
	status := decode[statusPayload](t, wtMust(t, wt, ldSession, "", "status", "--json"))
	if status.Freeze == nil || status.Freeze.Intact {
		t.Errorf("status reports the freeze as intact: %+v", status.Freeze)
	}
}

// ---------------------------------------------------------------- AC-4, AC-1 to AC-3

// AC-4 (architect B1, security 1): a test the story's own Gate 3 changed and
// froze, put back afterwards to the copy the story was cut from, is a broken
// freeze. That copy is also what trunk has, since trunk never touched the
// file, and that does not make it trunk's own: it is SDLC-E0025 by name at a
// gate and at the stop, never rebased, rebased by hand, and where the stop
// itself would put the story onto a trunk that moved.
func TestAC4AFrozenTestTheStoryChangedAndPutBackIsABrokenFreezeNeverTrunksOwn(t *testing.T) {
	t.Parallel()
	t.Run("AC-4 at a gate, never rebased", func(t *testing.T) {
		t.Parallel()
		main, wt := otOwnGate3(t, otGate3Writes, false)
		if _, held := lockOnDisk(t, wt).Files[otTaxTest]; !held {
			t.Fatalf("precondition: the story's freeze does not hold %s", otTaxTest)
		}
		writeFile(t, wt, otTaxTest, otTaxBase)

		otBrokenFreeze(t, wt)
		r := wtRun(t, wt, ldSession, "", "gate", "implementation", "pass", "--note", "with a frozen test put back")
		said := r.stdout + r.stderr
		if r.code == 0 || !strings.Contains(said, ldFrozenMoved) || !wtNames(said, otTaxTest) {
			t.Fatalf("gate implementation pass exited %d, want SDLC-E0025 naming %s:\n%s", r.code, otTaxTest, said)
		}
		if got := swRecord(t, main, "US-001").Gates[model.GateImplementation].Status; got == model.GatePass {
			t.Error("the implementation gate is recorded as passed")
		}
	})
	for _, tc := range []struct {
		name          string
		moved, byHand bool
	}{
		{name: "AC-4 at the stop, never rebased"},
		{name: "AC-4 at the stop, after a rebase by hand", moved: true, byHand: true},
		{name: "AC-4 at the stop alone, on a trunk that moved", moved: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt := otOwnGate3(t, otGate3Writes, true)
			ldBookkeeping(t, wt)
			if tc.moved {
				ldMoveTrunk(t, main)
			}
			if tc.byHand {
				ldRebase(t, wt)
			}
			swCommitFile(t, wt, otTaxTest, otTaxBase, "put the frozen test back as trunk has it")
			ldReviewAgain(t, wt)
			trunk := ldTrunk(t, main)
			if got, _ := otAt(t, main, trunk, otTaxTest); got != otTaxBase {
				t.Fatalf("precondition: trunk's %s is %q, and the case is the worktree's copy being trunk's", otTaxTest, got)
			}

			otBrokenFreeze(t, wt)
			out := swRefused(t, ldFrozenMoved, wt, ldSession, "stop")
			if !wtNames(out, otTaxTest) {
				t.Errorf("the refusal does not name %s:\n%s", otTaxTest, out)
			}
			if got := ldTrunk(t, main); got != trunk {
				t.Errorf("trunk moved to %s past a broken freeze", got)
			}
			if _, took := otTrunkTests(t, main); took {
				t.Error("the record says a test file was taken for trunk's, and trunk never changed one")
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// AC-1 and AC-3 (architect B2): a test file the story's own Gate 3 removed, or
// renamed, is the story's to take away: trunk did not gain or change it after
// the story was cut. The story passes its commit gate, on its cut and rebased
// before it, and lands by the stop alone and after a rebase by hand; trunk
// ends without the file.
func TestAC1AStoryWhoseOwnGate3RemovedOrRenamedATestFileStillPassesAndLands(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"removed", "renamed"} {
		gate3 := map[string]string{"removed": otGate3Removes, "renamed": otGate3Renames}[kind]
		landedWithout := func(t *testing.T, main, tip string) {
			t.Helper()
			if _, there := otAt(t, main, tip, otTaxTest); there {
				t.Errorf("trunk holds %s after the landing, and the story's Gate 3 took it away", otTaxTest)
			}
			if _, there := otAt(t, main, tip, otRenamedTest); there != (kind == "renamed") {
				t.Errorf("trunk holds %s: %v, want %v", otRenamedTest, there, kind == "renamed")
			}
			otNothingReopened(t, main)
		}
		for _, byHand := range []bool{false, true} {
			how := map[bool]string{false: "the stop alone", true: "after a rebase by hand"}[byHand]
			t.Run("AC-1 "+kind+", "+how, func(t *testing.T) {
				t.Parallel()
				main, wt := otOwnGate3(t, gate3, true)
				ldBookkeeping(t, wt)
				moved := ldMoveTrunk(t, main)
				if byHand {
					ldRebase(t, wt)
				}
				landedWithout(t, main, otLands(t, main, wt, moved).tip)
			})
		}
		t.Run("AC-1 "+kind+", rebased before its commit gate", func(t *testing.T) {
			t.Parallel()
			main, wt := otOwnGate3(t, gate3, false)
			swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
			commitEverything(t, wt)
			moved := ldMoveTrunk(t, main)
			ldRebase(t, wt)
			ldReviewAgain(t, wt)

			r := wtRun(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed after a rebase")
			if r.code != 0 {
				t.Fatalf("the commit gate exited %d for a story whose Gate 3 took a test file away:\n%s", r.code, r.stdout+r.stderr)
			}
			swPass(t, wt, ldSession, model.GateRetro, "")
			landedWithout(t, main, otLands(t, main, wt, moved).tip)
		})
	}
}

// AC-3 and AC-4 (security 2, red team V7): a test file that came from trunk is
// trunk's own only while the file on disk is the one trunk commits. Edited
// under an index flag that makes git status say nothing has changed, it is
// refused: SDLC-E0043 for a file trunk gained, SDLC-E0025 for a frozen one
// trunk changed.
func TestAC3AndAC4ATrunksTestFileEditedBehindAnIndexFlagIsStillRefused(t *testing.T) {
	t.Parallel()
	for _, flag := range []string{"--assume-unchanged", "--skip-worktree"} {
		for _, tc := range []struct{ name, file, trunk, edited, code string }{
			{"AC-3 a test file trunk gained", otRefundTest, otRefundV1, otRefundV2, ldUnfrozenTest},
			{"AC-4 a frozen test trunk changed", otTaxTest, otTaxV2, otTaxV3, ldFrozenMoved},
		} {
			t.Run(tc.name+", edited under "+flag, func(t *testing.T) {
				t.Parallel()
				main, wt, _ := otFinished(t)
				moved := swCommitFile(t, main, tc.file, tc.trunk, "another story's test")
				ldRebase(t, wt)
				swGit(t, wt, "update-index", flag, "--", tc.file)
				writeFile(t, wt, tc.file, tc.edited)
				if status := swGitRaw(t, wt, "status", "--porcelain", "--untracked-files=all"); status != "" {
					t.Fatalf("precondition: git status shows the edit, so the flag hides nothing:\n%s", status)
				}

				out := swRefused(t, tc.code, wt, ldSession, "stop")
				if !wtNames(out, tc.file) {
					t.Errorf("the refusal does not name %s:\n%s", tc.file, out)
				}
				if got := ldTrunk(t, main); got != moved {
					t.Errorf("trunk moved to %s with a test file edited behind %s", got, flag)
				}
				ldStillHeld(t, main, wt)
			})
		}
	}
}

// AC-3 (security 2): trunk's own is a plain file of trunk's mode with trunk's
// bytes, as the disk has it. A test file trunk gained that the session has
// made executable where git is told not to look at modes, or replaced with a
// link to the same bytes behind an index flag, shows no change in git status
// and is refused all the same with SDLC-E0043.
func TestAC3ATrunksTestFileThatIsNoLongerAPlainFileOfTrunksModeIsStillRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps no executable bit on disk, and makes a link only with a privilege")
	}
	t.Parallel()
	for _, tc := range []struct {
		name string
		do   func(t *testing.T, wt, path string)
	}{
		{"AC-3 made executable with core.fileMode off", func(t *testing.T, wt, path string) {
			t.Helper()
			swGit(t, wt, "config", "core.fileMode", "false")
			if err := os.Chmod(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		{"AC-3 a link to the same bytes under --assume-unchanged", func(t *testing.T, wt, path string) {
			t.Helper()
			target := filepath.Join(wtTempDir(t), "refund_test.go")
			if err := os.WriteFile(target, []byte(otRefundV1), 0o644); err != nil {
				t.Fatal(err)
			}
			swGit(t, wt, "update-index", "--assume-unchanged", "--", otRefundTest)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("no symbolic link could be made here: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otFinished(t)
			moved := swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			ldRebase(t, wt)
			tc.do(t, wt, filepath.Join(wt, filepath.FromSlash(otRefundTest)))
			if status := swGitRaw(t, wt, "status", "--porcelain", "--untracked-files=all"); status != "" {
				t.Fatalf("precondition: git status shows the change, so nothing is hidden:\n%s", status)
			}

			out := swRefused(t, ldUnfrozenTest, wt, ldSession, "stop")
			if !wtNames(out, otRefundTest) {
				t.Errorf("the refusal does not name %s:\n%s", otRefundTest, out)
			}
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s with a test file that is not trunk's as trunk has it", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// AC-5: however many test files were taken for trunk's, the stop says how
// many, and the record names every one. Twelve are more than the line lists:
// it names the first ten and says how many more there are.
func TestAC5MoreTestFilesThanTheLineListsAreCountedAndAllAreOnTheRecord(t *testing.T) {
	t.Parallel()
	main, wt, _ := otFinished(t)
	var files []string
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		files = append(files, "internal/gained_"+n+"_test.go")
		writeFile(t, main, files[len(files)-1], otRefundV1)
	}
	swGit(t, main, append([]string{"add", "--"}, files...)...)
	swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "another story's twelve tests")
	moved := ldTrunk(t, main)

	landed := otLands(t, main, wt, moved)
	for _, want := range []string{"12 test files", "and 2 more", files[0], files[9]} {
		if !strings.Contains(landed.said, want) {
			t.Errorf("the stop does not say %q of the twelve test files it took for trunk's:\n%s", want, landed.said)
		}
	}
	otRecordTook(t, main, moved, files, nil)
}

// AC-3 (red team V6): what a test file is compared with is the trunk commit
// the story sits on, not trunk's tip. A story still on the commit it was cut
// from, which writes the test file trunk has gained since, byte for byte, has
// a test file its freeze does not hold: SDLC-E0043, at a gate before the
// commit and at the commit gate.
func TestAC3AnUnrebasedStoryThatAddsTheTestFileTrunkGainedSinceIsRefused(t *testing.T) {
	t.Parallel()
	for _, gate := range []model.Gate{model.GateImplementation, model.GateCommit} {
		t.Run("AC-3 "+string(gate), func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otWalked(t, ldWalk{taxTest: true})
			ldWork(t, wt)
			swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			if gate == model.GateCommit {
				swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
			}
			writeFile(t, wt, otRefundTest, otRefundV1)
			if gate == model.GateCommit {
				commitEverything(t, wt)
			}

			r := wtRun(t, wt, ldSession, "", "gate", string(gate), "pass", "--note", "with trunk's newer test file")
			said := r.stdout + r.stderr
			if r.code == 0 || !strings.Contains(said, ldUnfrozenTest) || !wtNames(said, otRefundTest) {
				t.Fatalf("gate %s pass exited %d, want SDLC-E0043 naming %s:\n%s", gate, r.code, otRefundTest, said)
			}
			if got := swRecord(t, main, "US-001").Gates[gate].Status; got == model.GatePass {
				t.Errorf("gate %s is recorded as passed", gate)
			}
			if _, took := otTrunkTests(t, main); took {
				t.Error("the record says a test file was taken for trunk's in a story that sits on its cut")
			}
		})
	}
}

// AC-5 (red team V4): the trunk commit on the record is the one the test
// files were compared with, which is the commit the story sits on. A story
// rebased onto trunk where it gained a test file, with trunk gone further
// since and gaining another, has the first taken for trunk's at its commit
// gate, and the record names the commit it was rebased onto, not trunk's tip.
func TestAC5TheRecordNamesTheTrunkCommitTheStorySitsOnNotWhereTrunkWentSince(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{taxTest: true})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	commitEverything(t, wt)
	on := swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
	ldRebase(t, wt)
	swCommitFile(t, main, otLaterTest, otRefundV2, "and trunk gains another, later")
	ldReviewAgain(t, wt)

	r := wtRun(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed after a rebase")
	said := r.stdout + r.stderr
	if r.code != 0 {
		t.Fatalf("the commit gate exited %d on a tree whose only new test file is trunk's:\n%s", r.code, said)
	}
	otSaysAccepted(t, said, otRefundTest)
	if wtNames(said, otLaterTest) {
		t.Errorf("the gate names %s, which the story's worktree does not have:\n%s", otLaterTest, said)
	}
	otRecordTook(t, main, on, []string{otRefundTest}, nil)
}

// AC-5 (red team V4): where no test file was taken for trunk's, the record
// says nothing of any, whatever else on it names a trunk commit. A story
// rebased by hand onto a trunk that gained no test file lands with no
// trunk_tests on its record, and the stop does not speak of test files taken.
func TestAC5ARecordSaysNothingOfTrunksTestFilesWhereNoneWasTaken(t *testing.T) {
	t.Parallel()
	main, wt, _ := otFinished(t)
	moved := ldMoveTrunk(t, main)
	ldRebase(t, wt)

	landed := otLands(t, main, wt, moved)
	if got, took := otTrunkTests(t, main); took {
		t.Errorf("the record's trunk_tests is %+v after a landing that took no test file for trunk's", got)
	}
	if strings.Contains(landed.said, "test file") {
		t.Errorf("the stop speaks of test files, and none was taken for trunk's:\n%s", landed.said)
	}
}

// ---------------------------------------------------------------- AC-10

const (
	otBookLine = "// the story's line\n"
	otBook     = "package billing\n\n// one\n// two\n// same\n// three\n// four\n// five\n// six\n// seven\n" +
		"// eight\n// nine\n// same\n// ten\n// eleven\n// twelve\n"
)

// otBookWith is the book with the story's line after the line named, where
// one is, and without the nth of its two equal lines, where n is 1 or 2.
func otBookWith(after string, without int) string {
	var out strings.Builder
	seen := 0
	for line := range strings.Lines(otBook) {
		if line == "// same\n" {
			if seen++; seen == without {
				continue
			}
		}
		out.WriteString(line)
		if after != "" && line == after {
			out.WriteString(otBookLine)
		}
	}
	return out.String()
}

// AC-10 (red team V1 and V2, security 3): the reviewed change is its lines
// where they are. A commit made after the reviews that holds the very same
// added and removed lines somewhere else -- further up the file, in another
// file, or with the other of two equal lines removed -- is not the reviewed
// change: SDLC-E0029, with trunk where it was and with trunk moved, nothing
// lands, and a second stop is refused the same way.
func TestAC10TheSameLinesSomewhereElseAreNotTheReviewedChange(t *testing.T) {
	t.Parallel()
	reviewed := otBookWith("// seven\n", 2)
	for _, tc := range []struct {
		name  string
		files map[string]string
	}{
		{"AC-10 the added line further up the file", map[string]string{"book.go": otBookWith("// two\n", 2)}},
		{"AC-10 the added line in another file", map[string]string{
			"book.go":   otBookWith("", 2),
			"refund.go": "package billing\n\n// Refund is what US-001 adds.\ntype Refund struct{}\n" + otBookLine,
		}},
		{"AC-10 the other of two equal lines removed", map[string]string{"book.go": otBookWith("// seven\n", 1)}},
	} {
		for _, moved := range []bool{false, true} {
			where := map[bool]string{false: "trunk where it was", true: "trunk moved"}[moved]
			t.Run(tc.name+", "+where, func(t *testing.T) {
				t.Parallel()
				main := lxRepository(t, nil, nil)
				otIdentity(t, main)
				swCommitFile(t, main, "book.go", otBook, "the book")
				wt := ldStarted(t, main)
				ldWork(t, wt)
				writeFile(t, wt, "book.go", reviewed)
				swPass(t, wt, ldSession, model.GateImplementation, "")
				ldBookkeeping(t, wt)
				for path, body := range tc.files {
					writeFile(t, wt, path, body)
				}
				otCommitAll(t, wt, "after the reviews")
				if moved {
					ldMoveTrunk(t, main)
				}
				trunk := ldTrunk(t, main)

				for _, stop := range []string{"the stop", "a second stop"} {
					swRefused(t, ldStale, wt, ldSession, "stop")
					if got := ldTrunk(t, main); got != trunk {
						t.Fatalf("%s moved trunk to %s with lines nobody reviewed where they are", stop, got)
					}
				}
				if got, _ := otAt(t, main, "refs/heads/main", "book.go"); got != otBook {
					t.Errorf("trunk's book.go is %q, want it as trunk had it", got)
				}
				ldStillHeld(t, main, wt)
			})
		}
	}
}

// AC-10 (architect B3): the approval of a story in a tier that pauses stands
// across the stop's own putting of the story onto trunk, and across nothing
// else. Rebased by hand, the story is refused with SDLC-E0037 until a person
// approves the work as it now is, as before this story.
func TestAC10AStoryInATierThatPausesRebasedByHandIsNotLandedOnTheApprovalItHad(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{tier: "high"})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, "")
	ldBookkeeping(t, wt)
	moved := ldMoveTrunk(t, main)
	ldRebase(t, wt)

	out := swRefused(t, ldNotApproved, wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	suSays(t, fix, `"sdlc approve US-001"`, worktreeIn("US-001"))
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("trunk moved to %s on an approval of the work before somebody rebased it", got)
	}
	ldStillHeld(t, main, wt)
}

// AC-10 (security 5): the approval that lets a story in a tier that pauses
// land is the one a person gave of the reviewed work before its commit. An
// approval that answered an earlier question -- here, which of two readings
// of the story was meant -- is not that approval. A story that was not in a
// tier that pauses when it was committed, and is in one when it is stopped
// with trunk moved, is refused with SDLC-E0037, by the stop alone and after a
// rebase by hand.
func TestAC10AnApprovalThatAnsweredAnEarlierQuestionDoesNotStandForTheOneBeforeTheCommit(t *testing.T) {
	t.Parallel()
	for _, byHand := range []bool{true, false} {
		name := map[bool]string{true: "AC-10 after a rebase by hand", false: "AC-10 the stop alone"}[byHand]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otWalked(t, ldWalk{})
			wtMust(t, wt, ldSession, "", "escalate", "spec_unclear", "--message", "which one?")
			wtMust(t, wt, "", "", "approve", "US-001")
			if got := swStarted(t, wt, ldSession); got.Story != "US-001" || !got.Resume {
				t.Fatalf("start after the answer gave %+v, want US-001 resumed", got)
			}
			ldWork(t, wt)
			swPass(t, wt, ldSession, model.GateImplementation, "")
			ldBookkeeping(t, wt)
			if got := len(swRecord(t, main, "US-001").Approvals); got != 1 {
				t.Fatalf("precondition: the record has %d approvals, want the one that answered the question", got)
			}
			ldTier(t, main, "high")
			moved := ldMoveTrunk(t, main)
			if byHand {
				ldRebase(t, wt)
			}

			swRefused(t, ldNotApproved, wt, ldSession, "stop")
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s on an approval that answered another question", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// ---------------------------------------------------------------- AC-9, AC-11, AC-12

// AC-9 and AC-10 (security 4): the repository's configuration is the
// session's to write, and a merge driver in it does not decide what the stop
// lands. Trunk and the story changed different lines of one file, so putting
// the story onto trunk merges it; the driver configured for that would write
// the story's line somewhere else and report success. It never runs. Named by
// merge.default, the file is merged as text and the story lands as reviewed;
// named by an attribute, the stop either does the same or refuses and leaves
// everything as it was.
func TestAC9AMergeDriverTheRepositoryConfiguresDoesNotDecideWhatTheStopLands(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the driver here is a shell script marked executable, which Windows does not run as such")
	}
	t.Parallel()
	const storys, trunks = "// seven, as the story has it\n", "// one, as trunk has it\n"
	want := strings.Replace(strings.Replace(otLedger, "// seven\n", storys, 1), "// one\n", trunks, 1)
	for _, tc := range []struct {
		name      string
		mayRefuse bool
		name2     func(t *testing.T, main string)
	}{
		{name: "AC-9 named by merge.default", name2: func(t *testing.T, main string) {
			t.Helper()
			swGit(t, main, "config", "merge.default", "steer")
		}},
		{name: "AC-9 named by an attribute", mayRefuse: true, name2: func(t *testing.T, main string) {
			t.Helper()
			writeFile(t, main, ".git/info/attributes", "ledger.go merge=steer\n")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main := lxRepository(t, nil, nil)
			otIdentity(t, main)
			swCommitFile(t, main, "ledger.go", otLedger, "the ledger")
			wt := ldStarted(t, main)
			ldWork(t, wt)
			writeFile(t, wt, "ledger.go", strings.Replace(otLedger, "// seven\n", storys, 1))
			swPass(t, wt, ldSession, model.GateImplementation, "")
			ldBookkeeping(t, wt)
			moved := swCommitFile(t, main, "ledger.go", strings.Replace(otLedger, "// one\n", trunks, 1), "another story's line")

			bin := wtTempDir(t)
			mark, driver := filepath.Join(bin, "ran"), filepath.Join(bin, "driver")
			steered := strings.Replace(otLedger, "// two\n", "// two\n"+storys, 1)
			script := "#!/bin/sh\necho ran > " + suSh(mark) + "\nprintf '%s' " + suSh(steered) + " > \"$2\"\nexit 0\n"
			if err := os.WriteFile(driver, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			swGit(t, main, "config", "merge.steer.driver", suSh(driver)+" %O %A %B")
			tc.name2(t, main)
			before := otSnap(t, main, wt, "ledger.go", "billing.go", "refund.go")

			r := wtRun(t, wt, ldSession, "", "stop", "--json")
			if wtExists(mark) {
				t.Errorf("the repository's merge driver ran in the stop:\n%s", r.stdout+r.stderr)
			}
			if r.code != 0 {
				if !tc.mayRefuse {
					t.Fatalf("stop exited %d, want the story landed with the file merged as text:\n%s", r.code, r.stdout+r.stderr)
				}
				if !strings.Contains(r.stdout+r.stderr, ldAny) {
					t.Errorf("stop exited %d with no code:\n%s", r.code, r.stdout+r.stderr)
				}
				before.unchanged(t, otSnap(t, main, wt, "ledger.go", "billing.go", "refund.go"), "the stop that would not merge with the driver")
				if got := ldTrunk(t, main); got != moved {
					t.Errorf("the refused stop moved trunk to %s", got)
				}
				return
			}
			tip := ldTrunk(t, main)
			if tip == moved {
				t.Fatalf("stop exited 0 and trunk is still at %s:\n%s", moved, r.stdout+r.stderr)
			}
			if got, _ := otAt(t, main, tip, "ledger.go"); got != want {
				t.Errorf("trunk's ledger.go is\n%s\nwant the story's line and trunk's, each where its side put it:\n%s", got, want)
			}
		})
	}
}

// AC-11 (red team V3): nothing a refused stop leaves behind lets the next one
// skip the commands. A story put onto the trunk that moved, refused because a
// command failed there, is refused again by the next stop and the one after
// with trunk untouched: each runs the command again, each answers SDLC-E0055
// naming it, and trunk stays where it was.
func TestAC11EveryStopAfterACommandFailedRunsTheCommandsAgainAndRefusesAgain(t *testing.T) {
	t.Parallel()
	ran := filepath.Join(wtTempDir(t), "ran")
	runs := func() int { return strings.Count(wtRead(t, ran), "ran\n") }
	main, wt := ldWalked(t, ldWalk{finished: true, lx: true, set: map[string]string{
		"test": "echo ran >> " + suSh(ran) + " && test ! -f CHANGES.md",
	}})
	otIdentity(t, main)
	ldBookkeeping(t, wt)
	moved := ldMoveTrunk(t, main)

	for i, stop := range []string{"the first stop", "the second stop", "the third stop"} {
		before := runs()
		out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
		said, _ := ldRefusal(t, out, ldCheckFailed)
		suSays(t, said, "commands.test", "Nothing landed")
		if got := runs(); got <= before {
			t.Errorf("%s did not run commands.test: it has run %d times, as before", stop, got)
		}
		if got := ldTrunk(t, main); got != moved {
			t.Fatalf("%s moved trunk to %s though commands.test fails on the tree", stop, got)
		}
		if i > 0 && strings.Contains(out, "SDLC-E0054") {
			t.Errorf("%s speaks of a trunk that moved, and trunk has not moved since the last:\n%s", stop, out)
		}
	}
	ldStillHeld(t, main, wt)
}

// AC-12 (red team V8): what a stop that met a conflict leaves as it was
// includes what the session had beside its commits: a stash, and an ignored
// file at a path trunk does not track. Neither is dropped, applied, or
// cleaned away, on the first stop or the second.
func TestAC12AConflictLeavesAStashAndAnIgnoredFileWhereTheyWere(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true})
	writeFile(t, wt, "README.md", "# Billing, set aside\n")
	swGit(t, wt, "stash", "push", "--quiet", "-m", "set aside")
	tip := ldBookkeeping(t, wt)
	writeFile(t, main, ".git/info/exclude", "/build/\n")
	writeFile(t, wt, "build/out.bin", "built\n")
	if status := swGitRaw(t, wt, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Fatalf("precondition: git status shows the ignored file or the stash:\n%s", status)
	}
	moved := swCommitFile(t, main, "billing.go", ldBilling+"\n// Ledger is what another story adds.\ntype Ledger struct{}\n", "another story's ledger")
	files := []string{"billing.go", "refund.go", "README.md", "build/out.bin"}
	before := otSnap(t, main, wt, files...)
	if !strings.Contains(before.Stashes, "set aside") {
		t.Fatalf("precondition: the worktree has no stash:\n%s", before.Stashes)
	}

	for _, stop := range []string{"the stop that met a conflict", "a second stop after the conflict"} {
		out := swRefused(t, ldAny, wt, ldSession, "stop")
		if !wtNames(out, "billing.go") {
			t.Errorf("the refusal does not name billing.go, the file in conflict:\n%s", out)
		}
		before.unchanged(t, otSnap(t, main, wt, files...), stop)
		if got := swRev(t, main, "refs/heads/story/US-001"); got != tip {
			t.Errorf("story/US-001 is at %s after %s, want it left at %s", got, stop, tip)
		}
		if got := ldTrunk(t, main); got != moved {
			t.Errorf("trunk moved to %s though the story's commits did not apply", got)
		}
	}
}
