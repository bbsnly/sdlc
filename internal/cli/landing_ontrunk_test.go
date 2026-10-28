package cli

// US-088 in process: a finished story in its worktree lands on trunk on its
// own, whatever other stories have landed since it began.
//
// `sdlc stop` puts the story's commits onto the trunk that moved and lands
// them in the one run (AC-8), runs the project's commands on that tree before
// trunk moves (AC-11), and leaves the reviews and a person's approval standing
// where the story's own change is the one that was reviewed (AC-9), stale
// where it is not (AC-10). A conflict leaves the worktree and its branch as
// they were and goes to a person (AC-12), and two stops at once both land
// (AC-13). The freeze takes the test files trunk gained, changed or removed
// for trunk's (AC-1, AC-2), says so and records the trunk commit (AC-5), and
// holds everything else as it always did (AC-3, AC-4).
//
// The AC numbers in this file are US-088's; landing_test.go's are US-029.3's.
// Everything here reads what a caller can see, as landing_test.go does: what
// a command printed and how it exited, git's own answers, the record and the
// backlog on disk. No test names a function the story has still to add, but
// AC-13, which US-107 extended: it pauses one stop and gives the other a clock
// through the seams landing_lockwait_test.go describes.
//
// A repository here has a committer of its own in its configuration, as a
// project a person commits in has: the binary's own replay of the story's
// commits needs one, and the test environment gives git none.
//
// Helpers carry an ot prefix, for onto trunk.

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

const otSecondSession = "ot-session-second"

// The test files of these repositories: one trunk holds before the story
// starts, which the story's freeze therefore holds; the story's own, which
// the test author writes at Gate 3; and the ones trunk gains afterwards.
const (
	otTaxTest     = "internal/tax_test.go"
	otInvoiceTest = "internal/invoice_test.go"
	otRefundTest  = "internal/refund_test.go"
	otLaterTest   = "internal/later_test.go"

	otRefundV1 = "package internal\n\n// refunds, as another story wrote them\n"
	otRefundV2 = "package internal\n\n// refunds, as another story changed them\n"
	otTaxV2    = "package internal\n\n// changed on trunk\n"
	otTaxV3    = "package internal\n\n// changed on trunk again\n"
	otOwn      = "package internal\n\n// written after the freeze\n"
)

// otIdentity gives the repository a committer, as a person's has.
func otIdentity(t *testing.T, main string) {
	t.Helper()
	swGit(t, main, "config", "user.email", "t@example.com")
	swGit(t, main, "config", "user.name", "Test")
}

// otWalked is ldWalkedFrom in a repository with a committer.
func otWalked(t *testing.T, w ldWalk) (main, wt, cut string) {
	t.Helper()
	w.identified = true
	return ldWalkedFrom(t, w)
}

// otFinished is a finished story with its bookkeeper's commit, in a
// repository whose trunk held internal/tax_test.go before the story started,
// and the commit its branch was cut from.
func otFinished(t *testing.T) (main, wt, cut string) {
	t.Helper()
	main, wt, cut = otWalked(t, ldWalk{finished: true, bookkept: true, taxTest: true})
	return main, wt, cut
}

// otRemoveOnTrunk is another story's commit that takes a file off trunk.
func otRemoveOnTrunk(t *testing.T, main, rel string) string {
	t.Helper()
	swGit(t, main, "rm", "--quiet", "--", rel)
	swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "another story removes "+rel)
	return ldTrunk(t, main)
}

// otCommitAll commits everything the story's worktree holds.
func otCommitAll(t *testing.T, wt, message string) {
	t.Helper()
	swGit(t, wt, "add", "-A")
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
}

// otSubjects is the subject of every commit in from..to, newest first.
func otSubjects(t *testing.T, dir, from, to string) string {
	t.Helper()
	return swGit(t, dir, "log", "--format=%s", from+".."+to)
}

// otAt is the file at rel as rev commits it in dir's repository, and whether
// rev has one.
func otAt(t *testing.T, dir, rev, rel string) (string, bool) {
	t.Helper()
	if _, ok := swGitTry(t, dir, "cat-file", "-e", rev+":"+rel); !ok {
		return "", false
	}
	return swGitRaw(t, dir, "show", rev+":"+rel), true
}

// otLanding is a stop that landed the story: trunk's new tip, and what the
// stop said on both streams.
type otLanding struct{ tip, said string }

// otLands is the session stopping US-001 in its worktree, which has to land it
// on top of onto, the trunk commit it found: trunk descends from onto, holds
// commits of the story's beyond it, has no merge commit, and the story is
// done. No refusal is printed on the way.
func otLands(t *testing.T, main, wt, onto string) otLanding {
	t.Helper()
	r := wtRun(t, wt, ldSession, "", "stop", "--json")
	said := r.stdout + r.stderr
	if r.code != 0 {
		t.Fatalf("sdlc stop in %s exited %d where it should have landed the story on %s:\n%s", wt, r.code, onto[:12], said)
	}
	if strings.Contains(said, "SDLC-E") {
		t.Errorf("the stop that landed the story printed a refusal on the way:\n%s", said)
	}
	if got := decode[stopPayload](t, r); !got.OK || got.Story != "US-001" || !got.Done {
		t.Errorf("stop --json = %+v, want US-001 done", got)
	}
	tip := ldTrunk(t, main)
	if tip == onto {
		t.Fatalf("trunk is still at %s after the stop: nothing landed\n%s", onto[:12], said)
	}
	if _, ok := swGitTry(t, main, "merge-base", "--is-ancestor", onto, tip); !ok {
		t.Fatalf("trunk at %s does not descend from %s, where it was before the stop", tip[:12], onto[:12])
	}
	if merges := swGit(t, main, "rev-list", "--merges", onto+".."+tip); merges != "" {
		t.Errorf("trunk has merge commits after the landing: %s", merges)
	}
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
		t.Errorf("US-001 is %s after it landed, want done", got)
	}
	return otLanding{tip: tip, said: said}
}

// otNothingReopened holds a landed story's record to AC-1's last words: the
// freeze was never lifted, and every gate is still the pass it was.
func otNothingReopened(t *testing.T, main string) {
	t.Helper()
	r := swRecord(t, main, "US-001")
	for _, e := range r.Events {
		// The loop lifts a finished story's freeze itself as it closes it, and
		// writes that as an event of the same type; `sdlc log` tells the two
		// apart by this message, and so does this.
		if e.Type == "unfreeze" && e.Message != "the story is finished, so its freeze is lifted" {
			t.Errorf("the freeze was lifted on the way to the landing: %s", e.Message)
		}
	}
	for _, g := range model.Gates {
		if got := r.Gates[g].Status; got != model.GatePass {
			t.Errorf("gate %s is %q on the landed story's record, want the pass it had", g, got)
		}
	}
}

// otSaysAccepted holds what a command printed to AC-5: it says how many test
// files it took for trunk's, and names each.
func otSaysAccepted(t *testing.T, said string, files ...string) {
	t.Helper()
	count := regexp.MustCompile(`\b` + strconv.Itoa(len(files)) + ` test files?\b`)
	if !count.MatchString(said) {
		t.Errorf("the command does not say it took %d test file(s) for trunk's:\n%s", len(files), said)
	}
	for _, f := range files {
		if !wtNames(said, f) {
			t.Errorf("the command does not name %s, which it took for trunk's:\n%s", f, said)
		}
	}
}

// otTaken is what a record says of the test files taken for trunk's, in the
// field of its own the plan gives it on disk: trunk_tests, holding the trunk
// commit they were compared with, the files there, and the frozen files gone.
type otTaken struct {
	Commit string   `json:"commit"`
	Files  []string `json:"files"`
	Gone   []string `json:"gone"`
}

// otTrunkTests is US-001's record's trunk_tests, and whether it has one.
func otTrunkTests(t *testing.T, main string) (otTaken, bool) {
	t.Helper()
	var record struct {
		TrunkTests *otTaken `json:"trunk_tests"`
	}
	wtReadJSON(t, wtRecordPath(main, "US-001"), &record)
	if record.TrunkTests == nil {
		return otTaken{}, false
	}
	return *record.TrunkTests, true
}

// otRecordTook holds the record to AC-5: its trunk_tests names commit, in
// full, as the trunk commit the test files were compared with, and names
// exactly files as there and gone as removed. Another field holding that
// commit -- where the story landed, or what the stop put it onto -- says
// nothing about the test files, and does not pass for it.
func otRecordTook(t *testing.T, main, commit string, files, gone []string) {
	t.Helper()
	got, ok := otTrunkTests(t, main)
	if !ok {
		t.Errorf("the record has no trunk_tests: it does not say which trunk commit the test files were compared with")
		return
	}
	if got.Commit != commit {
		t.Errorf("the record's trunk_tests names commit %q, want %s, the trunk commit the story sat on", got.Commit, commit)
	}
	for _, names := range [][]string{got.Files, got.Gone, files, gone} {
		slices.Sort(names)
	}
	if !slices.Equal(got.Files, files) {
		t.Errorf("the record's trunk_tests names files %q, want %q", got.Files, files)
	}
	if !slices.Equal(got.Gone, gone) {
		t.Errorf("the record's trunk_tests names %q as gone, want %q", got.Gone, gone)
	}
}

// otSnapshot is everything a refused landing has to leave as it was: every
// branch, the worktrees git lists, and in the story's worktree its HEAD, the
// branch it is on, what git status says, the stashes, whether anything is
// part-way, and the files named.
type otSnapshot struct {
	Refs, Worktrees, Head, On, Status, Stashes string
	PartWay                                    []string
	Files                                      map[string]string
}

func otSnap(t *testing.T, main, wt string, files ...string) otSnapshot {
	t.Helper()
	s := otSnapshot{
		Refs:      swRefs(t, main),
		Worktrees: strings.Join(swWorktrees(t, main), "\n"),
		Head:      swRev(t, wt, "HEAD"),
		Status:    swGitRaw(t, wt, "status", "--porcelain=v1", "--untracked-files=all"),
		Stashes:   swStashes(t, wt),
		Files:     map[string]string{},
	}
	s.On = swBranchOn(t, wt)
	for _, name := range []string{"rebase-merge", "rebase-apply", "REBASE_HEAD", "CHERRY_PICK_HEAD", "MERGE_HEAD"} {
		p, ok := gfOwnPath(wt, name)
		if !ok {
			p = swGit(t, wt, "rev-parse", "--git-path", name)
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(wt, p)
		}
		if wtExists(p) {
			s.PartWay = append(s.PartWay, name)
		}
	}
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(wt, filepath.FromSlash(f)))
		if err != nil {
			s.Files[f] = "(" + err.Error() + ")"
			continue
		}
		s.Files[f] = string(raw)
	}
	return s
}

func (s otSnapshot) unchanged(t *testing.T, after otSnapshot, what string) {
	t.Helper()
	if !reflect.DeepEqual(s, after) {
		t.Errorf("%s did not leave the worktree and the branches as they were:\nbefore %+v\nafter  %+v", what, s, after)
	}
}

// otNotHandedOver holds a refusal to AC-11: the story stays with its session.
// Nothing sends it to a person, in the fix or on the record.
func otNotHandedOver(t *testing.T, main, fix string) {
	t.Helper()
	if strings.Contains(fix, "sdlc escalate") || strings.Contains(fix, "hands the story to a person") {
		t.Errorf("the fix hands the story to a person, and a command that failed is the session's to rework:\n%s", fix)
	}
	if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusAwaitingHuman) {
		t.Error("the story waits for a person after a command failed on the tree about to land")
	}
	if pending, ok := swRecord(t, main, "US-001").PendingEscalation(); ok {
		t.Errorf("a question to a person is open on the record: %+v", pending)
	}
}

// ---------------------------------------------------------------- AC-8

// AC-8: trunk moved since the story began, and the story's commits apply onto
// it. The one stop puts them there and lands them: no SDLC-E0054, no rebase by
// hand, and no approve, unfreeze or release from a person. Trunk ends as the
// commit it had moved to with the story's own commits on top, each still its
// own commit, and the record names that range.
func TestAC8TheOneStopPutsAFinishedStoryOntoTheTrunkThatMovedAndLandsIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		trunk func(t *testing.T, main string)
	}{
		{"AC-8 another story's file", func(t *testing.T, main string) {
			t.Helper()
			ldMoveTrunk(t, main)
		}},
		{"AC-8 several commits", func(t *testing.T, main string) {
			t.Helper()
			ldMoveTrunk(t, main)
			swCommitFile(t, main, "NOTES.md", "a second story's work\n", "a second story lands")
			swCommitFile(t, main, "CHANGES.md", "another story's work, changed\n", "a third story lands")
		}},
		{"AC-8 a change to a file trunk already had", func(t *testing.T, main string) {
			t.Helper()
			swCommitFile(t, main, "README.md", "# Billing\n\nAnother story's paragraph.\n", "another story's README")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt, cut := otWalked(t, ldWalk{finished: true, bookkept: true})
			old := swRev(t, wt, "HEAD")
			subjects := otSubjects(t, wt, cut, old)
			tc.trunk(t, main)
			moved := ldTrunk(t, main)

			landed := otLands(t, main, wt, moved)
			if strings.Contains(landed.said, "git rebase") {
				t.Errorf("the stop that landed the story still speaks of a rebase to run by hand:\n%s", landed.said)
			}
			if got := otSubjects(t, main, moved, landed.tip); got != subjects {
				t.Errorf("trunk gained the commits\n%s\nwant the story's own, each as it was:\n%s", got, subjects)
			}
			if got, _ := otAt(t, main, landed.tip, "refund.go"); !strings.Contains(got, "type Refund") {
				t.Errorf("trunk has refund.go %q, want the story's", got)
			}
			if got := swGitRaw(t, main, "status", "--porcelain", "--", "billing.go", "refund.go", "price list.txt", "CODEMAP.md"); got != "" {
				t.Errorf("the main checkout's working tree is not trunk's where the story landed:\n%s", got)
			}
			r := swRecord(t, main, "US-001")
			if r.CommitBase != moved || r.Commit != landed.tip {
				t.Errorf("the record names %s..%s as what landed, want %s..%s", r.CommitBase, r.Commit, moved, landed.tip)
			}
			if r.Commit == old {
				t.Errorf("the record names %s, the commit the story was at before it was put onto trunk", old)
			}
			if swListed(t, main, wt) || swHasBranch(t, main, "story/US-001") {
				t.Error("the landed story's worktree or branch is still there")
			}
		})
	}
}

// AC-8 and the person's answer on the approval: a story in a tier that pauses,
// approved at Gate 8, lands on that approval when the stop itself puts it onto
// the trunk that moved, its own change being the one that was approved. Nobody
// approves again.
func TestAC8AStoryInATierThatPausesLandsOnItsGate8ApprovalAfterTrunkMoved(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{tier: "high"})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, "")
	ldBookkeeping(t, wt)
	approvals := len(swRecord(t, main, "US-001").Approvals)
	moved := ldMoveTrunk(t, main)

	otLands(t, main, wt, moved)
	r := swRecord(t, main, "US-001")
	if got := len(r.Approvals); got != approvals {
		t.Errorf("the record has %d approvals after the landing, want the %d it had: none asked for again", got, approvals)
	}
	if pending, ok := r.PendingEscalation(); ok {
		t.Errorf("the landing left a question to a person open: %+v", pending)
	}
}

// AC-8, AC-5 and THREATS 4: every stop measures against trunk where it is
// then. A story refused once for a command that failed, with trunk moved again
// since, is put onto trunk's new tip, has the test file trunk gained meanwhile
// taken for trunk's, and lands there.
func TestAC8AStopAfterTrunkMovedAgainFollowsTrunkToWhereItIsThen(t *testing.T) {
	t.Parallel()
	main := lxRepository(t, map[string]string{"test": "test ! -f CHANGES.md || test -f FIXED.md"}, nil)
	otIdentity(t, main)
	wt := ldFinished(t, main)
	ldBookkeeping(t, wt)
	first := ldMoveTrunk(t, main)
	swRefused(t, ldCheckFailed, wt, ldSession, "stop")
	if got := ldTrunk(t, main); got != first {
		t.Fatalf("the refused landing moved trunk to %s", got)
	}

	swCommitFile(t, main, "FIXED.md", "trunk put right\n", "trunk is put right")
	second := swCommitFile(t, main, otLaterTest, otRefundV1, "and gains a test")

	landed := otLands(t, main, wt, second)
	otSaysAccepted(t, landed.said, otLaterTest)
	otRecordTook(t, main, second, []string{otLaterTest}, nil)
	if got, _ := otAt(t, main, landed.tip, otLaterTest); got != otRefundV1 {
		t.Errorf("trunk's %s is %q after the landing, want it as trunk had it", otLaterTest, got)
	}
}

// ---------------------------------------------------------------- AC-9

const otLedger = "package billing\n\n// one\n// two\n// three\n// four\n// five\n// six\n// seven\n// eight\n// nine\n// ten\n"

// AC-9: the reviews stand where the story's own change is the same after it
// is put onto the new trunk. Trunk changing a line beside the story's own, in
// the same file and without a conflict, is what trunk gained, not a change to
// the story: no SDLC-E0029, and no review is recorded again.
func TestAC9TheReviewsStandWhenTrunkChangedLinesBesideTheStorysOwn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, from, to string }{
		{"AC-9 two lines above the story's change", "// five\n", "// five, as trunk has it\n"},
		{"AC-9 three lines below the story's change", "// ten\n", "// ten, as trunk has it\n"},
		{"AC-9 far from the story's change", "// one\n", "// one, as trunk has it\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main := lxRepository(t, nil, nil)
			otIdentity(t, main)
			swCommitFile(t, main, "ledger.go", otLedger, "the ledger")
			wt := ldStarted(t, main)
			ldWork(t, wt)
			writeFile(t, wt, "ledger.go", strings.Replace(otLedger, "// seven\n", "// seven, as the story has it\n", 1))
			swPass(t, wt, ldSession, model.GateImplementation, "")
			ldBookkeeping(t, wt)
			reviews := len(swRecord(t, main, "US-001").Reviews)
			moved := swCommitFile(t, main, "ledger.go", strings.Replace(otLedger, tc.from, tc.to, 1), "another story's line")

			landed := otLands(t, main, wt, moved)
			got, _ := otAt(t, main, landed.tip, "ledger.go")
			for _, want := range []string{"// seven, as the story has it\n", tc.to} {
				if !strings.Contains(got, want) {
					t.Errorf("trunk's ledger.go does not hold %q after the landing:\n%s", want, got)
				}
			}
			if got := len(swRecord(t, main, "US-001").Reviews); got != reviews {
				t.Errorf("the record has %d reviews after the landing, want the %d it had: none asked for again", got, reviews)
			}
		})
	}
}

// ---------------------------------------------------------------- AC-10

// AC-10: a change that is not the one reviewed is stale wherever trunk
// stands. Committed after the reviews, with trunk moved since, it is refused
// with SDLC-E0029, which names the reviews to run again, and nothing lands;
// recorded again on the change as it is, they let it land, as today.
func TestAC10AChangeCommittedAfterTheReviewsIsStaleThoughTrunkMovedToo(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
	lxChangeAfterTheReviews(t, wt)
	moved := ldMoveTrunk(t, main)

	out := swRefused(t, ldStale, wt, ldSession, "stop")
	said, fix := ldRefusal(t, out, ldStale)
	suSays(t, said+fix, "verifier", "code-reviewer")
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("trunk moved to %s with a change nobody reviewed", got)
	}
	ldStillHeld(t, main, wt)

	ldReviewAgain(t, wt)
	landed := otLands(t, main, wt, moved)
	if got, _ := otAt(t, main, landed.tip, "refund.go"); !strings.Contains(got, "Amount int") {
		t.Errorf("trunk has refund.go %q, want the change that was reviewed again", got)
	}
}

// AC-10 and the person's answer on the approval: where the change differs
// from the one a person approved at Gate 8, the approval is stale, as today.
// The reviews are asked for first, and once they are recorded again the stop
// is refused with SDLC-E0037 until a person approves the work as it now is.
func TestAC10TheGate8ApprovalIsStaleWhenTheChangeDiffersFromTheOneApproved(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{tier: "high"})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, "")
	ldBookkeeping(t, wt)
	lxChangeAfterTheReviews(t, wt)
	moved := ldMoveTrunk(t, main)

	// US-092: a commit above the range the commit gate recorded, in a tier that
	// waits for a person, is refused with SDLC-E0037 as well, and the stop may
	// say so before it asks for the reviews again. Either way nothing lands.
	first := wtRun(t, wt, ldSession, "", "stop")
	said := first.stdout + first.stderr
	refusedForEither := strings.Contains(said, ldStale) || strings.Contains(said, ldNotApproved)
	if first.code == 0 || !refusedForEither {
		t.Fatalf("sdlc stop exited %d where it should have been refused with %s or %s:\n%s", first.code, ldStale, ldNotApproved, said)
	}
	ldReviewAgain(t, wt)
	out := swRefused(t, ldNotApproved, wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	suSays(t, fix, `"sdlc approve US-001"`, worktreeIn("US-001"))
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("trunk moved to %s on the approval of another change", got)
	}
	ldStillHeld(t, main, wt)
}

// ---------------------------------------------------------------- AC-11

// AC-11: the project's commands run on the tree the story's commits make on
// the new trunk, and before anything reaches trunk: each sees HEAD at the
// commit that then lands, with trunk's file in it, while trunk is still where
// it was.
func TestAC11TheCommandsRunOnTheNewTreeWhileTrunkIsStillWhereItWas(t *testing.T) {
	t.Parallel()
	marks := wtTempDir(t)
	mark := func(key string) string {
		at := suSh(filepath.Join(marks, key))
		return "git rev-parse HEAD refs/heads/main > " + at + " && test -f CHANGES.md && test -f refund.go"
	}
	commands := map[string]string{}
	for _, key := range ldQuality {
		commands[key] = mark(key)
	}
	commands["coverage"] += " && echo 95"
	commands["mutation"] += " && echo 90"
	main := lxRepository(t, commands, nil)
	otIdentity(t, main)
	wt := ldFinished(t, main)
	ldBookkeeping(t, wt)
	moved := ldMoveTrunk(t, main)

	landed := otLands(t, main, wt, moved)
	for _, key := range ldQuality {
		ran := strings.Fields(wtRead(t, filepath.Join(marks, key)))
		if len(ran) != 2 {
			t.Errorf("commands.%s did not run before the story landed", key)
			continue
		}
		if ran[0] != landed.tip {
			t.Errorf("commands.%s ran on %s, want the tree that landed, %s", key, ran[0], landed.tip)
		}
		if ran[1] != moved {
			t.Errorf("commands.%s ran with trunk at %s, want it still at %s: nothing reaches trunk before the commands pass", key, ran[1], moved)
		}
	}
}

// AC-11: a command that fails on that tree, and only there, lands nothing.
// Trunk is unchanged, the refusal names the command, and the story stays with
// its session to rework: nothing hands it to a person.
func TestAC11ACommandThatFailsOnTheNewTreeLandsNothingAndLeavesTheStoryWithItsSession(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ key, command string }{
		{"test", "test ! -f CHANGES.md"},
		{"lint", "test ! -f CHANGES.md"},
		{"coverage", "if test -f CHANGES.md; then echo 50; else echo 95; fi"},
	} {
		t.Run("AC-11 commands."+tc.key, func(t *testing.T) {
			t.Parallel()
			main, wt := ldWalked(t, ldWalk{finished: true, lx: true, set: map[string]string{tc.key: tc.command}})
			otIdentity(t, main)
			ldBookkeeping(t, wt)
			moved := ldMoveTrunk(t, main)

			out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
			said, fix := ldRefusal(t, out, ldCheckFailed)
			suSays(t, said, "commands."+tc.key, "Nothing landed")
			if strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(fix), "fix")) == "" {
				t.Errorf("the refusal does not say what the session does next:\n%s", out)
			}
			otNotHandedOver(t, main, fix)
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s though commands.%s failed on the tree", got, tc.key)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// ---------------------------------------------------------------- AC-12

// AC-12: the story's commits do not apply onto the new trunk. The stop leaves
// the worktree and its branch exactly as they were before it tried -- every
// branch, HEAD, the files, nothing part-way and nothing stashed -- lands
// nothing, names the files in conflict, and hands the story to a person: the
// fix sends it to them, in the one wording it has, the stop does not escalate
// it itself, and nothing tells the session to resolve the conflict. A second
// stop finds the same and leaves the same.
func TestAC12AConflictLeavesTheWorktreeAndBranchAsTheyWereAndHandsTheStoryToAPerson(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, file string
		trunk      func(t *testing.T, main string)
	}{
		{"AC-12 both changed the same lines", "billing.go", func(t *testing.T, main string) {
			t.Helper()
			swCommitFile(t, main, "billing.go", ldBilling+"\n// Ledger is what another story adds.\ntype Ledger struct{}\n", "another story's ledger")
		}},
		{"AC-12 both bookkeepers changed CODEMAP.md", "CODEMAP.md", func(t *testing.T, main string) {
			t.Helper()
			swCommitFile(t, main, "CODEMAP.md", "# CODEMAP\n\nledger.go keeps the ledger.\n", "docs: map another story")
		}},
		{"AC-12 trunk removed a file the story changed", "price list.txt", func(t *testing.T, main string) {
			t.Helper()
			otRemoveOnTrunk(t, main, "price list.txt")
		}},
		{"AC-12 both added the same file", "refund.go", func(t *testing.T, main string) {
			t.Helper()
			swCommitFile(t, main, "refund.go", "package billing\n\n// Refund, as another story has it.\ntype Refund struct{ Cents int }\n", "another story's refund")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
			tip := swRev(t, wt, "HEAD")
			tc.trunk(t, main)
			moved := ldTrunk(t, main)
			files := []string{"billing.go", "refund.go", "price list.txt", "CODEMAP.md"}
			before := otSnap(t, main, wt, files...)

			out := swRefused(t, ldAny, wt, ldSession, "stop")
			if !wtNames(out, tc.file) {
				t.Errorf("the refusal does not name %s, the file in conflict:\n%s", tc.file, out)
			}
			before.unchanged(t, otSnap(t, main, wt, files...), "the stop that met a conflict")
			if got := swRev(t, main, "refs/heads/story/US-001"); got != tip {
				t.Errorf("story/US-001 is at %s after the conflict, want it left at %s", got, tip)
			}
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s though the story's commits did not apply", got)
			}
			if strings.Contains(out, "rebase --continue") || strings.Contains(out, "rebase --skip") {
				t.Errorf("the refusal tells its reader to carry a rebase on, and resolving a conflict is a person's:\n%s", out)
			}
			// US-090 AC-8: the one wording the stop gives. It does not escalate
			// the story itself; its fix tells the assistant to.
			if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusAwaitingHuman) {
				t.Errorf("the stop handed the story to a person itself: the backlog has it %q, and its fix is what sends it to one", got)
			}
			if !strings.Contains(hcFlat(out), hcOneWording) {
				t.Errorf("the refusal does not say %q:\n%s", hcOneWording, out)
			}
			if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusDone) {
				t.Error("a story that did not land is done")
			}
			if got := swClaimed(t, main, "US-001"); !config.SamePath(got, wt) {
				t.Errorf("the claim on US-001 names %q, want it left with %s", got, wt)
			}

			// The second call: whoever runs it, nothing more is lost or moved.
			swRefused(t, ldAny, wt, ldSession, "stop")
			before.unchanged(t, otSnap(t, main, wt, files...), "a second stop after the conflict")
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("the second stop moved trunk to %s", got)
			}
		})
	}
}

// AC-12, at its edge: a worktree holding work nobody committed is not a tree
// the stop may move. With trunk moved it is refused, and the work is where it
// was: not committed, not stashed, not written over.
func TestAC12AWorktreeHoldingUncommittedWorkIsLeftAsItWasWhenTrunkMoved(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
	moved := ldMoveTrunk(t, main)
	writeFile(t, wt, "README.md", "# Billing, edited in the worktree\n")
	writeFile(t, wt, "scratch.txt", "a thought\n")
	before := otSnap(t, main, wt, "README.md", "scratch.txt")
	if !strings.Contains(before.Status, "README.md") {
		t.Fatalf("precondition: git status does not show the edit:\n%s", before.Status)
	}

	swRefused(t, ldAny, wt, ldSession, "stop")
	before.unchanged(t, otSnap(t, main, wt, "README.md", "scratch.txt"), "the stop refused for uncommitted work")
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("trunk moved to %s from a worktree with uncommitted work", got)
	}
}

// AC-12, at its edge: a repository whose git has no committer to make the
// story's commits again with. The stop either lands the story all the same, or
// refuses and leaves the worktree and its branch as they were; it never leaves
// a replay part-way.
func TestAC12WithNoCommitterTheStopLandsOrLeavesEverythingAsItWas(t *testing.T) {
	t.Parallel()
	main, wt, _ := ldWalkedFrom(t, ldWalk{finished: true})
	swGit(t, main, "config", "user.useConfigOnly", "true")
	ldBookkeeping(t, wt)
	moved := ldMoveTrunk(t, main)
	before := otSnap(t, main, wt, "billing.go", "refund.go")

	r := wtRun(t, wt, ldSession, "", "stop")
	if r.code == 0 {
		if got := ldTrunk(t, main); got == moved {
			t.Errorf("stop exited 0 and trunk is still at %s:\n%s", moved, r.stdout+r.stderr)
		}
		return
	}
	if !strings.Contains(r.stderr, ldAny) {
		t.Errorf("stop exited %d with no code:\n%s", r.code, r.stderr)
	}
	before.unchanged(t, otSnap(t, main, wt, "billing.go", "refund.go"), "the stop refused for want of a committer")
	if got := ldTrunk(t, main); got != moved {
		t.Errorf("the refused stop moved trunk to %s", got)
	}
}

// ---------------------------------------------------------------- AC-13

// otSecondStory is US-002, begun in its own worktree by another session before
// anything landed, and worked through every gate: a test file and a source
// file of its own, neither a path US-001 touches.
func otSecondStory(t *testing.T, main string) string {
	t.Helper()
	wt := swCheckStarted(t, main, "US-002", otSecondSession, swStarted(t, main, otSecondSession))
	swReach(t, wt, otSecondSession, model.GateTestsFrozen)
	writeFile(t, wt, "internal/shipping_test.go", "package internal\n\n// AC-1 of US-002\n")
	mustRunWith(t, "# Test plan\n", "artifact", "write", "test_plan")
	mustRun(t, "freeze")
	for _, r := range model.ReviewersFor(model.GateTestsFrozen) {
		if !r.WhenDoRAdvocate {
			mustRunWith(t, "# "+r.Role+"\n", "review", "add", string(model.GateTestsFrozen), r.Role, "approve")
		}
	}
	mustRun(t, "gate", string(model.GateTestsFrozen), "pass", "--note", "1 criterion, 1 failing test")
	swPass(t, wt, otSecondSession, model.GatePlan, model.GateImplementation)
	writeFile(t, wt, "shipping.go", "package billing\n\n// Shipping is what US-002 adds.\ntype Shipping struct{}\n")
	swPass(t, wt, otSecondSession, model.GateImplementation, "")
	return wt
}

// otStop is `sdlc stop --json` in dir from session, run beside another: it
// reads nothing the test set for its own commands.
func otStop(dir, session string) result {
	var out, errb bytes.Buffer
	getenv := func(name string) string {
		if name == store.SessionEnv {
			return session
		}
		return os.Getenv(name)
	}
	code := ExecuteIn(dir, getenv, []string{"stop", "--json"}, strings.NewReader(""), &out, &errb)
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

// AC-13: two finished stories, each begun before the other landed, stopped at
// the same moment from their two worktrees. Both land, one after the other:
// trunk holds the commits of both in one line, and the second's range begins
// where the first's ends. Trunk had moved before either stop, so each has its
// commits to put onto it and the commands to run, and the two overlap.
//
// US-107: under the full race suite one stop could hold the loop's state past
// the whole ten seconds the other waits for it, and the other was refused with
// SDLC-E0006, which the runbook answers by stopping again. So does this test,
// saying so and counting (US-107 AC-2), and it shows the case it answers
// without loading the machine and without a clock on the wall: the first stop
// is paused as it begins to land, holding the loop's state, while the
// second's wait runs out on a clock the test gives it (US-107 AC-1).
//
// Nothing here waits on a clock: both stops are let go by one channel, and the
// criterion holds however the two then fall, so the outcome is the same for
// every interleaving.
func TestAC13TwoStoriesStoppedAtTheSameMomentBothLandOneAfterTheOther(t *testing.T) {
	t.Parallel()
	t.Run("US-107 AC-2 both stops let go by one channel land, each refused for the loop's state run again and counted", func(t *testing.T) {
		t.Parallel()
		main, first, second, moved := otTwoFinished(t)
		stops := []struct{ story, dir, session string }{{"US-001", first, ldSession}, {"US-002", second, otSecondSession}}
		results := make([]result, len(stops))
		again := make([]int, len(stops))
		gun := make(chan struct{})
		var wg sync.WaitGroup
		for i, s := range stops {
			wg.Go(func() {
				<-gun
				results[i], again[i] = lwStopAgain(t, s.story, func(int) result { return otStop(s.dir, s.session) })
			})
		}
		close(gun)
		wg.Wait()

		t.Logf("US-107 AC-2: the two stops were run again %d times in all for the loop's state held past the wait (%s %d, %s %d)",
			again[0]+again[1], stops[0].story, again[0], stops[1].story, again[1])
		for i, r := range results {
			if r.code != 0 {
				t.Errorf("the stop in %s exited %d after it was run again %d times, want both stories landed:\n%s%s",
					stops[i].dir, r.code, again[i], r.stdout, r.stderr)
			}
		}
		otBothLanded(t, main, moved, false)
	})

	t.Run("US-107 AC-1 AC-2 the first stop holds the loop's state past the whole wait of the second", func(t *testing.T) {
		t.Parallel()
		main, first, second, moved := otTwoFinished(t)
		held := lwPausedStop(t, first, nil)
		held.waitReached(t)

		clock := newLWClock()
		var waitedOut, firstStop result
		last, again := lwStopAgain(t, "US-002", func(try int) result {
			if try == 0 {
				waitedOut = lwStop(second, otSecondSession, clock, nil)
				return waitedOut
			}
			// Run again once the stop in front has finished, as a session
			// does: it was the stop in front that held the state.
			if try == 1 {
				firstStop = held.result(t)
			}
			return otStop(second, otSecondSession)
		})

		if !lwWaitedOut(waitedOut) {
			t.Fatalf("the second stop, waiting on its own clock while the first held the loop's state, exited %d, "+
				"want it refused with %s %q:\n%s%s", waitedOut.code, sdlcerr.StateUnwritable, lwStillRunning, waitedOut.stdout, waitedOut.stderr)
		}
		if doc, _ := lwRefusalOf(waitedOut); !strings.Contains(doc.Why, "`sdlc stop`") || !strings.Contains(doc.Why, "longer than 10s") {
			t.Errorf("the second stop's refusal says %q, want the first stop named as holding the state for longer than 10s", doc.Why)
		}
		if clock.sleeps() == 0 {
			t.Error("the second stop never slept on the clock it was given")
		}
		if again != 1 {
			t.Errorf("the second stop was run again %d times, want once: refused while the first held the state, and landed after", again)
		}
		if firstStop.code != 0 {
			t.Errorf("the first stop exited %d once let go, want US-001 landed:\n%s%s", firstStop.code, firstStop.stdout, firstStop.stderr)
		}
		if last.code != 0 {
			t.Errorf("the second stop, run again, exited %d, want US-002 landed:\n%s%s", last.code, last.stdout, last.stderr)
		}
		otBothLanded(t, main, moved, true)
	})
}

// otTwoFinished is the repository of AC-13: US-001 and US-002 each finished in
// a worktree of its own, begun before either landed, on a trunk that moved
// since; and where trunk was before the stops.
func otTwoFinished(t *testing.T) (main, first, second, moved string) {
	t.Helper()
	main = lxRepository(t, nil, nil, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	otIdentity(t, main)
	first = ldFinished(t, main)
	second = otSecondStory(t, main)
	return main, first, second, ldMoveTrunk(t, main)
}

// otBothLanded is what AC-13 requires once both stops have run: trunk
// descends from moved in one line and holds both stories' files, both are
// done, and their records name one landed on moved and the other on top of it,
// up to trunk's tip. Where inOrder, US-001 is the one that landed on moved.
func otBothLanded(t *testing.T, main, moved string, inOrder bool) {
	t.Helper()
	tip := ldTrunk(t, main)
	if _, ok := swGitTry(t, main, "merge-base", "--is-ancestor", moved, tip); !ok {
		t.Fatalf("trunk at %s does not descend from %s, where it was before the stops", tip[:12], moved[:12])
	}
	if merges := swGit(t, main, "rev-list", "--merges", moved+".."+tip); merges != "" {
		t.Errorf("trunk has merge commits: %s", merges)
	}
	for _, f := range []string{"refund.go", "shipping.go", otInvoiceTest, "internal/shipping_test.go", "CHANGES.md"} {
		if _, ok := otAt(t, main, tip, f); !ok {
			t.Errorf("trunk does not hold %s after both stops", f)
		}
	}
	statuses := wtStatuses(t, main)
	for _, id := range []string{"US-001", "US-002"} {
		if statuses[id] != string(model.StatusDone) {
			t.Errorf("%s is %s after both stops, want done", id, statuses[id])
		}
	}
	a, b := swRecord(t, main, "US-001"), swRecord(t, main, "US-002")
	if !inOrder && b.CommitBase == moved {
		a, b = b, a
	}
	if a.CommitBase != moved || a.Commit == "" || b.CommitBase != a.Commit || b.Commit != tip {
		t.Errorf("the records name %s..%s and %s..%s, want US-001 landed on %s and US-002 on top of it, "+
			"or the other way about where the test chose no order, up to %s",
			a.CommitBase, a.Commit, b.CommitBase, b.Commit, moved, tip)
	}
}

// ---------------------------------------------------------------- AC-1

// AC-1: trunk gained a test file and a source file since the story's freeze
// was taken, as another story landing does. They are trunk's, the same in the
// worktree as in the trunk commit the story sits on, so the stop lands the
// story with no SDLC-E0043: the freeze is not lifted and no gate is reopened.
// AC-5: the stop says how many it took for trunk's and names them, and the
// record holds the trunk commit they were compared with.
func TestAC1AndAC5TestFilesTrunkGainedAreTakenForTrunksAndTheStoryLands(t *testing.T) {
	t.Parallel()
	for _, byHand := range []bool{false, true} {
		name := map[bool]string{false: "AC-1 AC-5 the stop alone", true: "AC-1 AC-5 after a rebase by hand"}[byHand]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otFinished(t)
			lock := wtRead(t, filepath.Join(wt, ".sdlc", "state", "tests.lock"))
			if lock == "" {
				t.Fatal("precondition: the story's worktree holds no freeze")
			}
			swCommitFile(t, main, "ledger.go", "package billing\n\n// Ledger is another story's.\ntype Ledger struct{}\n", "another story's code")
			swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			moved := swCommitFile(t, main, "internal/ledger_test.go", otRefundV2, "and a second test")
			if byHand {
				ldRebase(t, wt)
				if got := wtRead(t, filepath.Join(wt, ".sdlc", "state", "tests.lock")); got != lock {
					t.Fatal("precondition: the rebase changed the freeze")
				}
			}

			landed := otLands(t, main, wt, moved)
			otNothingReopened(t, main)
			otSaysAccepted(t, landed.said, otRefundTest, "internal/ledger_test.go")
			otRecordTook(t, main, moved, []string{otRefundTest, "internal/ledger_test.go"}, nil)
			for f, want := range map[string]string{otRefundTest: otRefundV1, "internal/ledger_test.go": otRefundV2} {
				if got, _ := otAt(t, main, landed.tip, f); got != want {
					t.Errorf("trunk's %s is %q after the landing, want it as trunk had it", f, got)
				}
			}
			if got, ok := otAt(t, main, landed.tip, otInvoiceTest); !ok || got == "" {
				t.Error("the story's own frozen test did not reach trunk")
			}
		})
	}
}

// AC-1 and AC-5 at a gate the freeze guards: a story rebased onto the trunk
// that gained a test file before its commit gate passes that gate, which says
// what it took for trunk's and writes the trunk commit on the record.
func TestAC1AndAC5TheCommitGateTakesTrunksTestFilesAfterARebaseAndSaysSo(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{taxTest: true})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	commitEverything(t, wt)
	moved := swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
	ldRebase(t, wt)
	ldReviewAgain(t, wt)

	r := wtRun(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed after a rebase")
	said := r.stdout + r.stderr
	if r.code != 0 {
		t.Fatalf("the commit gate exited %d on a tree whose only new test file is trunk's:\n%s", r.code, said)
	}
	otSaysAccepted(t, said, otRefundTest)
	otRecordTook(t, main, moved, []string{otRefundTest}, nil)
	if got := swRecord(t, main, "US-001").Gates[model.GateCommit].Status; got != model.GatePass {
		t.Errorf("the commit gate is %q on the record, want pass", got)
	}
}

// ---------------------------------------------------------------- AC-2

// AC-2: a test file the freeze holds was changed, or removed, on trunk since
// the freeze. Put onto trunk, the worktree's copy is trunk's, or is gone where
// trunk removed it, and the freeze is not broken for it: status says the
// freeze is intact, and the story lands with trunk's copy on trunk.
func TestAC2AFrozenTestTrunkChangedOrRemovedDoesNotBreakTheFreeze(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		removed bool
		byHand  bool
	}{
		{name: "AC-2 changed on trunk, the stop alone"},
		{name: "AC-2 changed on trunk, after a rebase by hand", byHand: true},
		{name: "AC-2 removed on trunk, the stop alone", removed: true},
		{name: "AC-2 removed on trunk, after a rebase by hand", removed: true, byHand: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otFinished(t)
			if _, held := lockOnDisk(t, wt).Files[otTaxTest]; !held {
				t.Fatalf("precondition: the story's freeze does not hold %s", otTaxTest)
			}
			var moved string
			if tc.removed {
				moved = otRemoveOnTrunk(t, main, otTaxTest)
			} else {
				moved = swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
			}
			if tc.byHand {
				ldRebase(t, wt)
				status := decode[statusPayload](t, wtMust(t, wt, ldSession, "", "status", "--json"))
				if status.Freeze == nil || !status.Freeze.Intact || len(status.Freeze.Changed) != 0 {
					t.Errorf("status reports the freeze as broken for a file that is trunk's: %+v", status.Freeze)
				}
			}

			landed := otLands(t, main, wt, moved)
			otNothingReopened(t, main)
			got, there := otAt(t, main, landed.tip, otTaxTest)
			switch {
			case tc.removed && there:
				t.Errorf("trunk holds %s again after the landing, and trunk had removed it", otTaxTest)
			case !tc.removed && got != otTaxV2:
				t.Errorf("trunk's %s is %q after the landing, want trunk's own change", otTaxTest, got)
			}
			// Red team V5: what was taken is said, counted and named, where
			// trunk changed a frozen test and where it removed one.
			otSaysAccepted(t, landed.said, otTaxTest)
			if tc.removed {
				otRecordTook(t, main, moved, nil, []string{otTaxTest})
			} else {
				otRecordTook(t, main, moved, []string{otTaxTest}, nil)
			}
		})
	}
}

// ---------------------------------------------------------------- AC-3

// AC-3: a test file in the worktree that the freeze does not hold, and that is
// not trunk's, is refused with SDLC-E0043 naming it at every gate the freeze
// guards, as today -- beside a test file that is trunk's, which is not named.
func TestAC3ATestFileThatIsNotTrunksIsRefusedAtEveryGateTheFreezeGuards(t *testing.T) {
	t.Parallel()
	for _, gate := range []model.Gate{model.GateTestsFrozen, model.GatePlan, model.GateImplementation, model.GateVerification, model.GateCommit} {
		t.Run("AC-3 "+string(gate), func(t *testing.T) {
			t.Parallel()
			main := ldRepository(t, func(root string) { writeFile(t, root, otTaxTest, "package internal\n") })
			otIdentity(t, main)
			wt := swCheckStarted(t, main, "US-001", ldSession, swStarted(t, main, ldSession))
			swReach(t, wt, ldSession, gate)
			// Everything else the gate asks for is there, so that the test
			// file is all that stands in its way.
			satisfy(t, wt, gate)
			writeFile(t, wt, otLaterTest, otOwn)
			if gate == model.GateCommit {
				otCommitAll(t, wt, "a test written after the freeze")
			}

			r := wtRun(t, wt, ldSession, "", "gate", string(gate), "pass", "--note", "with a test nobody froze")
			said := r.stdout + r.stderr
			if r.code == 0 || !strings.Contains(said, ldUnfrozenTest) || !wtNames(said, otLaterTest) {
				t.Fatalf("gate %s pass exited %d, want SDLC-E0043 naming %s:\n%s", gate, r.code, otLaterTest, said)
			}
			if wtNames(said, otInvoiceTest) || wtNames(said, otTaxTest) {
				t.Errorf("the refusal names a test the freeze holds:\n%s", said)
			}
			if got := swRecord(t, main, "US-001").Gates[gate].Status; got == model.GatePass {
				t.Errorf("gate %s is recorded as passed", gate)
			}
		})
	}
}

// AC-3 at the stop: with trunk moved and a test file of trunk's to take, a
// test file the story committed after its freeze is still refused with
// SDLC-E0043, by name, and nothing lands.
func TestAC3TheStopRefusesATestFileThatIsNotTrunksThoughItTakesTrunksOwn(t *testing.T) {
	t.Parallel()
	for _, byHand := range []bool{false, true} {
		name := map[bool]string{false: "AC-3 the stop alone", true: "AC-3 after a rebase by hand"}[byHand]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otFinished(t)
			moved := swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			if byHand {
				ldRebase(t, wt)
			}
			swCommitFile(t, wt, otLaterTest, otOwn, "a test written after the freeze")

			out := swRefused(t, ldUnfrozenTest, wt, ldSession, "stop")
			said, _ := ldRefusal(t, out, ldUnfrozenTest)
			if !wtNames(said, otLaterTest) {
				t.Errorf("the refusal does not name %s:\n%s", otLaterTest, said)
			}
			if wtNames(said, otRefundTest) {
				t.Errorf("the refusal names %s, which is trunk's:\n%s", otRefundTest, said)
			}
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s with a test file nobody froze", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// otSymlink replaces the file at rel in the worktree with a link to a file
// beside it that holds body, or skips where the platform makes none.
func otSymlink(t *testing.T, wt, rel, body string) {
	t.Helper()
	path := filepath.Join(wt, filepath.FromSlash(rel))
	target := strings.TrimSuffix(filepath.Base(rel), ".go") + ".txt"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), target), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("this platform makes no symbolic link here: %v", err)
	}
}

// AC-3 and THREATS 1, 3, 4, 5: what passes for trunk's copy is the file as the
// trunk commit the story sits on holds it, by git's own object and mode, and
// nothing else. Each of these is a test file a session could make that only
// looks like trunk's; each is refused with SDLC-E0043 by name, and nothing
// lands.
func TestAC3ATestFileThatOnlyLooksLikeTrunksIsStillRefused(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, file string
		// trunk moves trunk before the story is rebased onto it by hand.
		trunk func(t *testing.T, main string)
		// story is what the session does in the worktree after the rebase.
		story func(t *testing.T, main, wt string)
		// uncommitted leaves the story's doing out of any commit.
		uncommitted bool
	}{
		{name: "AC-3 trunk's copy at an older commit than the one the story sits on", file: otRefundTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
				swCommitFile(t, main, otRefundTest, otRefundV2, "another story changes it")
			},
			story: func(t *testing.T, _, wt string) {
				t.Helper()
				writeFile(t, wt, otRefundTest, otRefundV1)
			}},
		{name: "AC-3 a file trunk had and has since removed", file: otRefundTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
				otRemoveOnTrunk(t, main, otRefundTest)
			},
			story: func(t *testing.T, _, wt string) {
				t.Helper()
				writeFile(t, wt, otRefundTest, otRefundV1)
			}},
		{name: "AC-3 trunk's bytes at a path trunk does not have", file: otLaterTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			},
			story: func(t *testing.T, _, wt string) {
				t.Helper()
				writeFile(t, wt, otLaterTest, otRefundV1)
			}},
		{name: "AC-3 a link to a file with trunk's bytes", file: otRefundTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			},
			story: func(t *testing.T, _, wt string) {
				t.Helper()
				otSymlink(t, wt, otRefundTest, otRefundV1)
			}},
		{name: "AC-3 trunk's bytes made executable", file: otRefundTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			},
			story: func(t *testing.T, _, wt string) {
				t.Helper()
				swGit(t, wt, "update-index", "--chmod=+x", "--", otRefundTest)
				if runtime.GOOS != "windows" {
					if err := os.Chmod(filepath.Join(wt, filepath.FromSlash(otRefundTest)), 0o755); err != nil {
						t.Fatal(err)
					}
				}
			}},
		{name: "AC-3 trunk's file edited and not committed", file: otRefundTest, uncommitted: true,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			},
			story: func(t *testing.T, _, wt string) {
				t.Helper()
				writeFile(t, wt, otRefundTest, otRefundV2)
			}},
		{name: "AC-3 a tag, a branch and a remote's branch named like trunk at the story's own commit", file: otLaterTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				ldMoveTrunk(t, main)
			},
			story: func(t *testing.T, main, wt string) {
				t.Helper()
				tip := swCommitFile(t, wt, otLaterTest, otOwn, "a test written after the freeze")
				swGit(t, main, "tag", "main", tip)
				swGit(t, main, "update-ref", "refs/heads/heads/main", tip)
				swGit(t, main, "update-ref", "refs/remotes/origin/main", tip)
				swGit(t, main, "update-ref", "refs/remotes/origin/HEAD", tip)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otFinished(t)
			tc.trunk(t, main)
			moved := ldTrunk(t, main)
			ldRebase(t, wt)
			tc.story(t, main, wt)
			if !tc.uncommitted {
				if dirty := swGitRaw(t, wt, "status", "--porcelain"); dirty != "" {
					otCommitAll(t, wt, "what the session made")
				}
			}

			out := swRefused(t, ldUnfrozenTest, wt, ldSession, "stop")
			if !wtNames(out, tc.file) {
				t.Errorf("the refusal does not name %s:\n%s", tc.file, out)
			}
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s with a test file that is not trunk's", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// AC-3 and THREATS 1: which branch is trunk is the main checkout's to say. A
// worktree whose own configuration names the story's branch as trunk does not
// make the story's own committed test file trunk's at the commit gate.
func TestAC3TheWorktreesOwnConfigurationDoesNotSayWhichBranchIsTrunk(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{taxTest: true})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	writeFile(t, wt, otLaterTest, otOwn)
	setGitOnDisk(t, wt, "trunk_branch", "story/US-001")
	commitEverything(t, wt)

	r := wtRun(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "with a trunk of the story's own choosing")
	said := r.stdout + r.stderr
	if r.code == 0 || !strings.Contains(said, ldUnfrozenTest) || !wtNames(said, otLaterTest) {
		t.Fatalf("gate commit pass exited %d, want SDLC-E0043 naming %s:\n%s", r.code, otLaterTest, said)
	}
	if got := swRecord(t, main, "US-001").Gates[model.GateCommit].Status; got == model.GatePass {
		t.Error("the commit gate is recorded as passed")
	}
}

// AC-3 and THREATS 2: a story not in a worktree of its own has trunk and its
// own commits on one branch, so every test file it commits is in "trunk's"
// commit by the time the commit gate runs. None is taken for trunk's there: a
// test file committed after the freeze is refused with SDLC-E0043 as today, in
// one checkout and in a worktree a person made.
func TestAC3WithoutAStoryWorktreeACommittedTestFileIsNeverTakenForTrunks(t *testing.T) {
	t.Parallel()
	for _, own := range []bool{false, true} {
		name := map[bool]string{false: "AC-3 one checkout", true: "AC-3 a worktree a person made"}[own]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := wtRepository(t, wtStory{id: "US-001"})
			if own {
				root = wtWorktree(t, root, "wt")
			}
			wtStarted(t, root, ldSession)
			swReach(t, root, ldSession, model.GateCommit)
			commitEverything(t, root)
			writeFile(t, root, otLaterTest, otOwn)
			otCommitAll(t, root, "a test written after the freeze")

			r := wtRun(t, root, ldSession, "", "gate", "commit", "pass", "--note", "with a test nobody froze")
			said := r.stdout + r.stderr
			if r.code == 0 || !strings.Contains(said, ldUnfrozenTest) || !wtNames(said, otLaterTest) {
				t.Fatalf("gate commit pass exited %d, want SDLC-E0043 naming %s:\n%s", r.code, otLaterTest, said)
			}
		})
	}
}

// AC-3, AC-6 and THREATS 7: a test file that came from trunk is held like any
// other. A story that takes one away -- one the freeze never held, because
// trunk gained it afterwards -- does not land its removal: the stop refuses,
// names the file, and trunk keeps it.
func TestAC3AStoryThatTakesAwayATestFileTrunkGainedDoesNotLand(t *testing.T) {
	t.Parallel()
	for _, byHand := range []bool{true, false} {
		name := map[bool]string{true: "AC-3 removed after a rebase by hand", false: "AC-3 the stop alone, on a trunk that moved again"}[byHand]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otFinished(t)
			swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
			ldRebase(t, wt)
			swGit(t, wt, "rm", "--quiet", "--", otRefundTest)
			otCommitAll(t, wt, "take trunk's test away")
			ldReviewAgain(t, wt)
			if !byHand {
				ldMoveTrunk(t, main)
			}
			moved := ldTrunk(t, main)

			out := swRefused(t, ldAny, wt, ldSession, "stop")
			if !wtNames(out, otRefundTest) {
				t.Errorf("the refusal does not name %s, the test file taken away:\n%s", otRefundTest, out)
			}
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s, losing a test file another story landed", got)
			}
			if got, ok := otAt(t, main, "refs/heads/main", otRefundTest); !ok || got != otRefundV1 {
				t.Errorf("trunk's %s is %q, want it kept as trunk had it", otRefundTest, got)
			}
		})
	}
}

// ---------------------------------------------------------------- AC-4

// AC-4 and THREATS 4, 5, 7: a test file the freeze holds that is neither as it
// was frozen nor trunk's copy at the commit the story sits on is a broken
// freeze, SDLC-E0025 naming it, as today -- whatever trunk did to the file
// meanwhile, and whatever the copy is made to look like.
func TestAC4AFrozenTestThatIsNeitherAsFrozenNorTrunksBreaksTheFreeze(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, file string
		trunk      func(t *testing.T, main string)
		story      func(t *testing.T, wt string)
	}{
		{name: "AC-4 changed again after trunk changed it", file: otTaxTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
			},
			story: func(t *testing.T, wt string) {
				t.Helper()
				writeFile(t, wt, otTaxTest, otTaxV3)
			}},
		{name: "AC-4 trunk's copy at an older commit than the one the story sits on", file: otTaxTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
				swCommitFile(t, main, otTaxTest, otTaxV3, "and another")
			},
			story: func(t *testing.T, wt string) {
				t.Helper()
				writeFile(t, wt, otTaxTest, otTaxV2)
			}},
		{name: "AC-4 removed by the story where trunk only changed it", file: otTaxTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
			},
			story: func(t *testing.T, wt string) {
				t.Helper()
				swGit(t, wt, "rm", "--quiet", "--", otTaxTest)
			}},
		{name: "AC-4 the story's own frozen test removed, which trunk never had", file: otInvoiceTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				ldMoveTrunk(t, main)
			},
			story: func(t *testing.T, wt string) {
				t.Helper()
				swGit(t, wt, "rm", "--quiet", "--", otInvoiceTest)
			}},
		{name: "AC-4 the story's own frozen test changed, which trunk never had", file: otInvoiceTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				ldMoveTrunk(t, main)
			},
			story: func(t *testing.T, wt string) {
				t.Helper()
				writeFile(t, wt, otInvoiceTest, otOwn)
			}},
		{name: "AC-4 put back changed where trunk removed it", file: otTaxTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				otRemoveOnTrunk(t, main, otTaxTest)
			},
			story: func(t *testing.T, wt string) {
				t.Helper()
				writeFile(t, wt, otTaxTest, otTaxV3)
			}},
		{name: "AC-4 a link to a file with trunk's bytes", file: otTaxTest,
			trunk: func(t *testing.T, main string) {
				t.Helper()
				swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
			},
			story: func(t *testing.T, wt string) {
				t.Helper()
				otSymlink(t, wt, otTaxTest, otTaxV2)
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt, _ := otFinished(t)
			tc.trunk(t, main)
			moved := ldTrunk(t, main)
			ldRebase(t, wt)
			tc.story(t, wt)
			otCommitAll(t, wt, "what the session made of a frozen test")

			status := decode[statusPayload](t, wtMust(t, wt, ldSession, "", "status", "--json"))
			if status.Freeze == nil || status.Freeze.Intact {
				t.Errorf("status reports the freeze as intact: %+v", status.Freeze)
			}
			out := swRefused(t, ldFrozenMoved, wt, ldSession, "stop")
			if !wtNames(out, tc.file) {
				t.Errorf("the refusal does not name %s:\n%s", tc.file, out)
			}
			if got := ldTrunk(t, main); got != moved {
				t.Errorf("trunk moved to %s past a broken freeze", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// AC-4 before any rebase: a frozen test edited to what trunk has since made of
// it, in a worktree that still sits on the commit it was cut from, is not
// trunk's copy at that commit. The freeze is broken for it at the next gate
// and in status, as today.
func TestAC4AFrozenTestEditedToTrunksLaterCopyBreaksTheFreezeWhereTheStorySits(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{taxTest: true})
	swCommitFile(t, main, otTaxTest, otTaxV2, "another story's change")
	writeFile(t, wt, otTaxTest, otTaxV2)
	ldWork(t, wt)

	status := decode[statusPayload](t, wtMust(t, wt, ldSession, "", "status", "--json"))
	if status.Freeze == nil || status.Freeze.Intact {
		t.Errorf("status reports the freeze as intact: %+v", status.Freeze)
	}
	r := wtRun(t, wt, ldSession, "", "gate", "implementation", "pass", "--note", "with a frozen test edited")
	said := r.stdout + r.stderr
	if r.code == 0 || !strings.Contains(said, ldFrozenMoved) || !wtNames(said, otTaxTest) {
		t.Fatalf("gate implementation pass exited %d, want SDLC-E0025 naming %s:\n%s", r.code, otTaxTest, said)
	}
}
