package cli

// US-094: the record keeps the trunk test files a gate accepted until a later
// gate judges them again.
//
// A gate that checks the freeze -- tests_frozen, plan, implementation,
// verification, commit -- says which test files it took for trunk's and writes
// them on the record as trunk_tests. Recording the gate then ended by
// forgetting the field whenever the commit gate did not stand, so before the
// commit gate the record said nothing, and every such gate added the same
// trunk_tests event again.
//
// The one route to a gate before the commit gate that accepts anything is a
// rework: the story has commits of its own, is put onto a later trunk commit,
// and a gate is recorded while the code review still stands. Every test here
// goes that way, through the real commands, and reads the record as the store
// wrote it: the trunk_tests field and the trunk_tests events.
//
// Besides the story's three criteria the tests hold what a person decided on
// 2026-10-13, named D1 to D3 where a test is about one:
//
//   - D1: a guarded gate recorded as passed while the commit gate stands
//     leaves the field naming what that gate accepted, with one event;
//   - D2: a judgement that accepts nothing empties the field with no event,
//     and the same files after the field was emptied add one;
//   - D3: a person's `sdlc unfreeze` empties the field whether or not the
//     commit gate stands.
//
// Helpers are landing_test.go's (ld), landing_ontrunk_test.go's (ot),
// storyworktree_test.go's (sw) and trunkonstory_test.go's (ts); the ones added
// here carry a kt prefix.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const ktLedgerTest = "internal/ledger_test.go"

// ktMove is a commit another story lands on trunk, and what a gate of this
// story then takes for trunk's once it sits on that commit.
type ktMove struct {
	name  string
	do    func(t *testing.T, main string)
	files []string
	gone  []string
}

// The moves the rows use. No two give the same files, so that a fix that knew
// one name, or one count, holds for one row only.
var (
	ktGainsRefund = ktMove{name: "trunk gained a test file",
		do: func(t *testing.T, main string) {
			t.Helper()
			swCommitFile(t, main, otRefundTest, otRefundV1, "another story's test")
		},
		files: []string{otRefundTest}}
	ktGainsTwo = ktMove{name: "trunk gained two test files",
		do: func(t *testing.T, main string) {
			t.Helper()
			writeFile(t, main, ktLedgerTest, otRefundV2)
			swGit(t, main, "add", "--", ktLedgerTest)
			swCommitFile(t, main, otLaterTest, otRefundV1, "another story's two tests")
		},
		files: []string{ktLedgerTest, otLaterTest}}
	ktChangesTax = ktMove{name: "trunk changed a frozen test file",
		do: func(t *testing.T, main string) {
			t.Helper()
			swCommitFile(t, main, otTaxTest, otTaxV2, "another story changes the tax test")
		},
		files: []string{otTaxTest}}
	ktRemovesTax = ktMove{name: "trunk removed a frozen test file and gained one",
		do: func(t *testing.T, main string) {
			t.Helper()
			writeFile(t, main, otLaterTest, otRefundV2)
			swGit(t, main, "add", "--", otLaterTest)
			otRemoveOnTrunk(t, main, otTaxTest)
		},
		files: []string{otLaterTest}, gone: []string{otTaxTest}}
)

// ktBuilt is the repository every template here starts from: trunk holds
// internal/tax_test.go before the story, and US-001 is worked in its worktree
// through the code review, with its change committed and the commit gate still
// to run.
func ktBuilt(t *testing.T, root string) string {
	t.Helper()
	ldRepositoryAt(t, root, func(root string) { writeFile(t, root, otTaxTest, "package internal\n") })
	otIdentity(t, root)
	wt := ldStarted(t, root)
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	commitEverything(t, wt)
	return wt
}

// ktIn has the test's commands run in the story's worktree, from its session.
func ktIn(t *testing.T, main string) (wt string) {
	t.Helper()
	wt = swWorktreeOf(main, "US-001")
	chdir(t, wt)
	setenv(t, store.SessionEnv, ldSession)
	return wt
}

// ktCommitted is a copy of that story: every gate before the commit gate
// passed on the cut, nothing accepted as trunk's, and trunk not yet moved.
func ktCommitted(t *testing.T) (main, wt string) {
	t.Helper()
	main = walkedCopy(t, "US-094 committed, the commit gate still to run", func(t *testing.T, root string) {
		t.Helper()
		ktBuilt(t, root)
	})
	return main, ktIn(t, main)
}

// ktFinished is a copy of that story finished after trunk moved by m: rebased,
// reviewed again, its commit gate the first to accept m's files, and the retro
// recorded. It returns the trunk commit the commit gate compared with.
func ktFinished(t *testing.T, m ktMove) (main, wt, moved string) {
	t.Helper()
	main, notes := walkedCopyNoted(t, "US-094 finished after "+m.name, func(t *testing.T, root string) map[string]string {
		t.Helper()
		wt := ktBuilt(t, root)
		m.do(t, root)
		ldRebase(t, wt)
		ldReviewAgain(t, wt)
		wtMust(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed after a rebase")
		satisfy(t, wt, model.GateRetro)
		wtMust(t, wt, ldSession, "", "gate", "retro", "pass", "--note", "no deviations")
		return map[string]string{"moved": ldTrunk(t, root)}
	})
	return main, ktIn(t, main), notes["moved"]
}

// ktOnto is another story landing m on trunk and this story rebased onto it,
// and the trunk commit it then sits on.
func ktOnto(t *testing.T, main, wt string, m ktMove) string {
	t.Helper()
	m.do(t, main)
	ldRebase(t, wt)
	return ldTrunk(t, main)
}

// ktRecorded is the session recording a gate as failed or pending, which has
// to work.
func ktRecorded(t *testing.T, wt string, gate model.Gate, status model.GateStatus) {
	t.Helper()
	wtMust(t, wt, ldSession, "", "gate", string(gate), string(status), "--note", "recorded "+string(status)+" in the rework")
}

// ktPass is the session passing a gate again, its reviews given on the tree as
// it stands, which has to work. It returns what the gate printed.
func ktPass(t *testing.T, wt string, gate model.Gate) string {
	t.Helper()
	for _, r := range model.ReviewersFor(gate) {
		if r.WhenDoRAdvocate {
			continue
		}
		wtMust(t, wt, ldSession, "# "+r.Role+", in the rework\n", "review", "add", string(gate), r.Role, "approve")
	}
	r := wtMust(t, wt, ldSession, "", "gate", string(gate), "pass", "--note", "passed in the rework")
	return r.stdout + r.stderr
}

// ktReworkedFrom is the story put onto trunk moved by m and reworked from
// gate: that gate recorded as failed, which the code review still standing
// allows on a HEAD that moved. Nothing is accepted yet, and the record says
// so. It returns the trunk commit the story sits on.
func ktReworkedFrom(t *testing.T, main, wt string, m ktMove, gate model.Gate) string {
	t.Helper()
	moved := ktOnto(t, main, wt, m)
	ktRecorded(t, wt, gate, model.GateFail)
	ktHolds(t, main, "before any gate of the rework passed", nil, 0)
	return moved
}

// ktReworked is a copy of the story put onto trunk moved by m and reworked from
// the implementation: recorded as failed there, then every gate from it up to,
// and not including, upTo passed again, the first of them accepting m's files.
// It returns the trunk commit the story sits on, and the cut. Under the race
// detector a walk made in every row cost more than everything the row was
// about, so the rows that start past one share it.
func ktReworked(t *testing.T, m ktMove, upTo model.Gate) (main, wt, moved, cut string) {
	t.Helper()
	main, notes := walkedCopyNoted(t, "US-094 reworked after "+m.name+" up to "+string(upTo), func(t *testing.T, root string) map[string]string {
		t.Helper()
		wt := ktBuilt(t, root)
		cut := ldTrunk(t, root) // trunk has not moved since the story was cut
		moved := ktOnto(t, root, wt, m)
		ktRecorded(t, wt, model.GateImplementation, model.GateFail)
		ktPassFrom(t, wt, model.GateImplementation, upTo)
		return map[string]string{"moved": moved, "cut": cut}
	})
	return main, ktIn(t, main), notes["moved"], notes["cut"]
}

// ktWant is what the record's trunk_tests has to name.
type ktWant struct {
	commit string
	files  []string
	gone   []string
}

func ktTook(commit string, m ktMove) *ktWant {
	return &ktWant{commit: commit, files: m.files, gone: m.gone}
}

// ktEvents is the message of every trunk_tests event on the record, oldest
// first.
func ktEvents(t *testing.T, main string) []string {
	t.Helper()
	var out []string
	for _, e := range swRecord(t, main, "US-001").Events {
		if e.Type == "trunk_tests" {
			out = append(out, e.Message)
		}
	}
	return out
}

// ktHolds holds the record, as the store reads it, to naming want in its
// trunk_tests -- the trunk commit in full, exactly those files and exactly
// those gone -- or to having none where want is nil, and to holding that many
// trunk_tests events.
func ktHolds(t *testing.T, main, when string, want *ktWant, events int) {
	t.Helper()
	got := swRecord(t, main, "US-001").TrunkTests
	switch {
	case want == nil && got != nil:
		t.Errorf("%s: the record's trunk_tests is %+v, want none", when, *got)
	case want != nil && got == nil:
		t.Errorf("%s: the record has no trunk_tests, want commit %s, files %q, gone %q", when, want.commit, want.files, want.gone)
	case want != nil:
		files, gone := slices.Sorted(slices.Values(got.Files)), slices.Sorted(slices.Values(got.Gone))
		if got.Commit != want.commit || !slices.Equal(files, slices.Sorted(slices.Values(want.files))) ||
			!slices.Equal(gone, slices.Sorted(slices.Values(want.gone))) {
			t.Errorf("%s: the record's trunk_tests is %+v, want commit %s, files %q, gone %q", when, *got, want.commit, want.files, want.gone)
		}
	}
	if said := ktEvents(t, main); len(said) != events {
		t.Errorf("%s: the record has %d trunk_tests event(s), want %d:\n%s", when, len(said), events, strings.Join(said, "\n"))
	}
}

// ktLastEventNames holds the newest trunk_tests event to saying what want
// names: the trunk commit, and every file there and gone.
func ktLastEventNames(t *testing.T, main, when string, want *ktWant) {
	t.Helper()
	said := ktEvents(t, main)
	if len(said) == 0 {
		t.Errorf("%s: the record has no trunk_tests event", when)
		return
	}
	last := said[len(said)-1]
	if !strings.Contains(last, want.commit[:12]) {
		t.Errorf("%s: the newest trunk_tests event does not name trunk's commit %s:\n%s", when, want.commit[:12], last)
	}
	for _, f := range append(slices.Clone(want.files), want.gone...) {
		if !wtNames(last, f) {
			t.Errorf("%s: the newest trunk_tests event does not name %s:\n%s", when, f, last)
		}
	}
}

// ktSaysAccepted holds what a gate printed to naming every file it took for
// trunk's, there or gone.
func ktSaysAccepted(t *testing.T, said string, want *ktWant) {
	t.Helper()
	if !strings.Contains(said, "accepted as main's") {
		t.Errorf("the gate does not say it accepted test files as trunk's:\n%s", said)
	}
	for _, f := range append(slices.Clone(want.files), want.gone...) {
		if !wtNames(said, f) {
			t.Errorf("the gate does not name %s, which it took for trunk's:\n%s", f, said)
		}
	}
}

// ktSaysNothing holds a gate that accepted nothing to saying nothing of test
// files.
func ktSaysNothing(t *testing.T, said string) {
	t.Helper()
	if strings.Contains(said, "accepted as") {
		t.Errorf("the gate speaks of test files accepted as trunk's, and it accepted none:\n%s", said)
	}
}

// ktCommitStands is whether the commit gate stands passed on the record.
func ktCommitStands(t *testing.T, main string) bool {
	t.Helper()
	return swRecord(t, main, "US-001").Pass(model.GateCommit)
}

// ktForks is the record's list of the story's own commits.
func ktForks(t *testing.T, r *model.Record) []model.Fork {
	t.Helper()
	var forks []model.Fork
	if len(r.Forks) > 0 {
		if err := json.Unmarshal(r.Forks, &forks); err != nil {
			t.Fatalf("the record's forks do not read: %v\n%s", err, r.Forks)
		}
	}
	return forks
}

// ktMarked writes a replay mark on the record by hand, as a stop that put the
// story onto trunk itself leaves one, so that a test can see it forgotten.
func ktMarked(t *testing.T, main, wt, onto string) {
	t.Helper()
	var record map[string]any
	wtReadJSON(t, wtRecordPath(main, "US-001"), &record)
	head := swRev(t, wt, "HEAD")
	record["replay"] = map[string]any{"from": head, "to": head, "onto": onto}
	wtWriteJSON(t, wtRecordPath(main, "US-001"), record)
	if swRecord(t, main, "US-001").Replay == nil {
		t.Fatal("precondition: the replay mark written by hand does not read back")
	}
}

// ktForgets holds the record to AC-3's "as today": with the commit gate not
// standing it holds nothing that gate recorded -- the range's end, the
// reviewed change's fingerprint and its base, a landing mark, the replay mark
// -- and still holds whose the commits are and where the branch was cut.
func ktForgets(t *testing.T, main, when string, before *model.Record) {
	t.Helper()
	if before.Commit == "" || before.Change == "" || before.ChangeBase == "" || before.Replay == nil {
		t.Fatalf("precondition, %s: the record had commit %q, change %q, change_base %q, replay %+v; want all four to forget",
			when, before.Commit, before.Change, before.ChangeBase, before.Replay)
	}
	after := swRecord(t, main, "US-001")
	if after.Pass(model.GateCommit) {
		t.Fatalf("precondition, %s: the commit gate still stands", when)
	}
	if after.Commit != "" || after.RangeUnknown || after.HeadPastCommit != "" {
		t.Errorf("%s: the record still holds the range's end: commit %q, range_unknown %v, head_past_commit %q",
			when, after.Commit, after.RangeUnknown, after.HeadPastCommit)
	}
	if after.Change != "" || after.ChangeBase != "" {
		t.Errorf("%s: the record still holds the reviewed change, %q from %q", when, after.Change, after.ChangeBase)
	}
	if after.Landing != nil {
		t.Errorf("%s: the record still holds a landing mark, %+v", when, *after.Landing)
	}
	if after.Replay != nil {
		t.Errorf("%s: the record still holds the replay mark, %+v", when, *after.Replay)
	}
	ktKeepsForks(t, main, when, before)
}

// ktKeepsForks holds the record to keeping every entry of its list of the
// story's own commits, in order, and where the branch was cut: a gate may add
// to the list, and nothing takes from it.
func ktKeepsForks(t *testing.T, main, when string, before *model.Record) {
	t.Helper()
	after := swRecord(t, main, "US-001")
	was, is := ktForks(t, before), ktForks(t, after)
	if len(was) == 0 || before.Cut == "" {
		t.Fatalf("precondition, %s: the record had forks %v and cut %q; want both to keep", when, was, before.Cut)
	}
	if len(is) < len(was) || !slices.Equal(is[:len(was)], was) {
		t.Errorf("%s: the record's forks are %v, want every entry of %v kept", when, is, was)
	}
	if after.Cut != before.Cut {
		t.Errorf("%s: the record's cut is %q, want %q kept", when, after.Cut, before.Cut)
	}
}

// ---------------------------------------------------------------- AC-1

// AC-1, the table's row 1: the first gate of a rework to check the freeze
// accepts trunk's test files, says so, and leaves the record naming the trunk
// commit, the files and the ones gone, with one event, though the commit gate
// has not passed. Each guarded gate before the commit gate is the first in one
// row, each against other files.
func TestAC1APassedGateThatAcceptsTrunksTestFilesLeavesThemOnTheRecordBeforeTheCommitGate(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		gate model.Gate
		move ktMove
	}{
		{model.GateTestsFrozen, ktGainsTwo},
		{model.GatePlan, ktRemovesTax},
		{model.GateImplementation, ktGainsRefund},
		{model.GateVerification, ktChangesTax},
	} {
		t.Run("AC-1 "+string(row.gate)+" where "+row.move.name, func(t *testing.T) {
			main, wt := ktCommitted(t)
			moved := ktReworkedFrom(t, main, wt, row.move, row.gate)
			want := ktTook(moved, row.move)

			said := ktPass(t, wt, row.gate)
			ktSaysAccepted(t, said, want)
			if ktCommitStands(t, main) {
				t.Fatal("precondition: the commit gate stands, and this is a gate before it")
			}
			ktHolds(t, main, "after "+string(row.gate)+" passed and accepted them", want, 1)
			ktLastEventNames(t, main, "after "+string(row.gate)+" passed", want)
		})
	}
}

// AC-1, rows 6 and 7: once a gate accepted them, the record goes on naming
// them after every later gate that does not judge them again -- the gate that
// accepted them recorded as failed or pending, one after it, one before it,
// tests_frozen itself, the commit gate recorded as pending where it did not
// stand, and a gate that does not check the freeze recorded as passed -- and
// gains no event at any of them.
func TestAC1TheRecordStillNamesThemAfterEveryLaterGateThatDoesNotJudgeThemAgain(t *testing.T) {
	t.Parallel()
	type step struct {
		gate   model.Gate
		status model.GateStatus
	}
	for _, row := range []struct {
		name  string
		from  model.Gate
		move  ktMove
		steps []step
	}{
		{name: "gates recorded as failed or pending", from: model.GateImplementation, move: ktGainsRefund, steps: []step{
			{model.GateVerification, model.GateFail},
			{model.GateImplementation, model.GatePending},
			{model.GateImplementation, model.GateFail},
			{model.GateDesignReview, model.GatePending},
			{model.GatePlan, model.GateFail},
			{model.GateCommit, model.GatePending},
			{model.GateTestsFrozen, model.GateFail},
			{model.GateAnalysis, model.GatePending},
		}},
		{name: "gates that do not check the freeze recorded as passed", from: model.GatePlan, move: ktRemovesTax, steps: []step{
			{model.GateDesignReview, model.GatePass},
			{model.GateDesignReview, model.GatePass},
			{model.GateCodeReview, model.GateFail},
			{model.GateCommit, model.GateFail},
			{model.GateDesignReview, model.GatePass},
		}},
	} {
		t.Run("AC-1 "+row.name, func(t *testing.T) {
			main, wt := ktCommitted(t)
			moved := ktReworkedFrom(t, main, wt, row.move, row.from)
			want := ktTook(moved, row.move)
			ktPass(t, wt, row.from)
			ktHolds(t, main, "after "+string(row.from)+" passed and accepted them", want, 1)

			for _, s := range row.steps {
				when := "after " + string(s.gate) + " was then recorded " + string(s.status)
				if s.status == model.GatePass {
					ktSaysNothing(t, ktPass(t, wt, s.gate))
				} else {
					ktRecorded(t, wt, s.gate, s.status)
				}
				if got := swRecord(t, main, "US-001").Gates[s.gate].Status; got != s.status {
					t.Fatalf("precondition, %s: the gate is %q on the record", when, got)
				}
				if ktCommitStands(t, main) {
					t.Fatalf("precondition, %s: the commit gate stands", when)
				}
				ktHolds(t, main, when, want, 1)
			}
		})
	}
}

// AC-1, row 3: a later guarded gate that accepts other files, or the same
// files against another trunk commit, replaces what the record names and adds
// one event that names the new commit. The commit gate does not stand before
// or after.
func TestAC1ALaterGateThatAcceptsOtherFilesOrAnotherTrunkCommitReplacesWhatTheRecordNames(t *testing.T) {
	t.Parallel()
	onlySource := ktMove{name: "trunk gained a source file only", do: func(t *testing.T, main string) {
		t.Helper()
		ldMoveTrunk(t, main)
	}}
	for _, row := range []struct {
		name   string
		first  ktMove
		second ktMove
		gate   model.Gate
		want   ktMove // what is accepted once the story sits on both moves
	}{
		{name: "another file as well, on a later trunk commit", first: ktGainsRefund, second: ktGainsTwo, gate: model.GateVerification,
			want: ktMove{files: []string{otRefundTest, ktLedgerTest, otLaterTest}}},
		{name: "the same files against a later trunk commit", first: ktRemovesTax, second: onlySource, gate: model.GateImplementation,
			want: ktRemovesTax},
		{name: "a file gone as well, on a later trunk commit", first: ktGainsRefund, second: ktRemovesTax, gate: model.GatePlan,
			want: ktMove{files: []string{otRefundTest, otLaterTest}, gone: []string{otTaxTest}}},
	} {
		t.Run("AC-1 "+row.name, func(t *testing.T) {
			main, wt, first, _ := ktReworked(t, row.first, model.GateCommit)
			was := ktTook(first, row.first)
			ktHolds(t, main, "after the first rework's gates, the code review among them", was, 1)

			second := ktOnto(t, main, wt, row.second)
			if second == first {
				t.Fatal("precondition: trunk did not move a second time")
			}
			ktRecorded(t, wt, row.gate, model.GateFail)
			ktHolds(t, main, "after "+string(row.gate)+" was recorded as failed on the later trunk commit", was, 1)

			want := ktTook(second, row.want)
			ktSaysAccepted(t, ktPass(t, wt, row.gate), want)
			if ktCommitStands(t, main) {
				t.Fatal("precondition: the commit gate stands")
			}
			ktHolds(t, main, "after "+string(row.gate)+" passed on the later trunk commit", want, 2)
			ktLastEventNames(t, main, "after "+string(row.gate)+" passed on the later trunk commit", want)
		})
	}
}

// ktPassFrom is ktPass for every gate from `from` up to, and not including, upTo.
func ktPassFrom(t *testing.T, wt string, from, upTo model.Gate) {
	t.Helper()
	on := false
	for _, gate := range model.Gates {
		if gate == upTo {
			return
		}
		if on = on || gate == from; on {
			ktPass(t, wt, gate)
		}
	}
}

// AC-1 with D2, row 4: a later guarded gate that judges again and accepts
// nothing -- trunk took the file away again -- empties what the record names
// and writes no event, though the commit gate did not stand before or after.
// A field kept by never writing an empty judgement would still name a file
// that is no longer there.
func TestAC1ALaterGuardedGateThatAcceptsNothingEmptiesWhatTheRecordNamesWithNoEvent(t *testing.T) {
	t.Parallel()
	for _, gate := range []model.Gate{model.GateImplementation, model.GateTestsFrozen} {
		t.Run("AC-1 D2 "+string(gate), func(t *testing.T) {
			main, wt, first, _ := ktReworked(t, ktGainsRefund, model.GateCommit)
			ktHolds(t, main, "after the first rework's gates", ktTook(first, ktGainsRefund), 1)

			otRemoveOnTrunk(t, main, otRefundTest)
			ldRebase(t, wt)
			ktRecorded(t, wt, gate, model.GateFail)
			ktHolds(t, main, "after "+string(gate)+" was recorded as failed, trunk's file gone again", ktTook(first, ktGainsRefund), 1)

			ktSaysNothing(t, ktPass(t, wt, gate))
			if ktCommitStands(t, main) {
				t.Fatal("precondition: the commit gate stands")
			}
			ktHolds(t, main, "after "+string(gate)+" passed and accepted nothing", nil, 1)
		})
	}
}

// AC-1, row 5: a gate that is asked to pass and refused records nothing. One
// refused before anything was accepted fills no trunk_tests, though trunk's
// file is there to accept; one refused after a gate accepted trunk's file
// leaves the record naming it. Both are SDLC-E0043 for a test file of the
// story's own that the freeze does not hold.
func TestAC1ARefusedGateNeitherFillsNorEmptiesTheRecordsTrunkTests(t *testing.T) {
	t.Parallel()
	t.Run("AC-1 refused before anything was accepted", func(t *testing.T) {
		main, wt := ktCommitted(t)
		ktReworkedFrom(t, main, wt, ktGainsTwo, model.GateImplementation)
		writeFile(t, wt, "internal/own_test.go", otOwn)

		out := swRefused(t, ldUnfrozenTest, wt, ldSession, "gate", "implementation", "pass", "--note", "with a test of its own")
		if !wtNames(out, "internal/own_test.go") {
			t.Errorf("the refusal does not name the story's own test file:\n%s", out)
		}
		ktHolds(t, main, "after the implementation was refused", nil, 0)
	})
	t.Run("AC-1 refused after a gate accepted trunk's file", func(t *testing.T) {
		main, wt, moved, _ := ktReworked(t, ktRemovesTax, model.GateVerification)
		want := ktTook(moved, ktRemovesTax)
		ktHolds(t, main, "after the implementation passed and accepted it", want, 1)
		writeFile(t, wt, "internal/own_test.go", otOwn)

		out := swRefused(t, ldUnfrozenTest, wt, ldSession, "gate", "verification", "pass", "--note", "with a test of its own")
		if !wtNames(out, "internal/own_test.go") {
			t.Errorf("the refusal does not name the story's own test file:\n%s", out)
		}
		if got := swRecord(t, main, "US-001").Gates[model.GateVerification].Status; got == model.GatePass {
			t.Fatal("precondition: the refused verification is passed on the record")
		}
		ktHolds(t, main, "after the verification was refused", want, 1)
	})
}

// ---------------------------------------------------------------- AC-2

// AC-2, rows 2, 8 and 10, and the reproduction's own walk: after the first
// gate of a rework accepted trunk's files, every later gate through the commit
// gate and the retro leaves the record naming them, each guarded gate says it
// accepted them, and the record holds the one event it had. Today the same
// walk leaves five. Then D2: the commit gate recorded as failed empties the
// field with no event, and passed again it names the same files with one more.
func TestAC2ALaterGateThatAcceptsTheSameFilesAgainstTheSameTrunkCommitAddsNoFurtherEvent(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		from model.Gate
		move ktMove
	}{
		{model.GateTestsFrozen, ktGainsTwo},
		{model.GateImplementation, ktRemovesTax},
	} {
		t.Run("AC-2 reworked from "+string(row.from)+" where "+row.move.name, func(t *testing.T) {
			main, wt := ktCommitted(t)
			moved := ktReworkedFrom(t, main, wt, row.move, row.from)
			want := ktTook(moved, row.move)

			on := false
			for _, gate := range append(slices.Clone(model.Gates), model.GateCommit) {
				if on = on || gate == row.from; !on {
					continue
				}
				if gate == model.GateRetro {
					wtMust(t, wt, ldSession, "# Retro\n", "artifact", "write", "retro")
				}
				said := ktPass(t, wt, gate)
				switch gate {
				case model.GateTestsFrozen, model.GatePlan, model.GateImplementation, model.GateVerification, model.GateCommit:
					ktSaysAccepted(t, said, want)
				default:
					ktSaysNothing(t, said)
				}
				ktHolds(t, main, "after "+string(gate)+" passed", want, 1)
			}
			if !ktCommitStands(t, main) {
				t.Fatal("precondition: the commit gate does not stand after the walk")
			}

			ktRecorded(t, wt, model.GateCommit, model.GateFail)
			ktHolds(t, main, "after the commit gate was recorded as failed", nil, 1)
			ktSaysAccepted(t, ktPass(t, wt, model.GateCommit), want)
			ktHolds(t, main, "after the commit gate passed again, the field emptied in between", want, 2)
			ktLastEventNames(t, main, "after the commit gate passed again", want)
		})
	}
}

// ktWrites writes trunk_tests on the record by hand, as another binary or an
// edit could have left it, and holds it to reading back.
func ktWrites(t *testing.T, main string, taken ktWant) {
	t.Helper()
	var record map[string]any
	wtReadJSON(t, wtRecordPath(main, "US-001"), &record)
	field := map[string]any{"commit": taken.commit}
	if len(taken.files) > 0 {
		field["files"] = taken.files
	}
	if len(taken.gone) > 0 {
		field["gone"] = taken.gone
	}
	record["trunk_tests"] = field
	wtWriteJSON(t, wtRecordPath(main, "US-001"), record)
	if got := swRecord(t, main, "US-001").TrunkTests; got == nil || got.Commit != taken.commit ||
		!slices.Equal(got.Files, taken.files) || !slices.Equal(got.Gone, taken.gone) {
		t.Fatalf("precondition: the trunk_tests written by hand reads back as %+v, want %+v", got, taken)
	}
}

// AC-2, its boundary: "the same" is the same trunk commit, the same files and
// the same files gone, all three. A record that names anything else when a
// guarded gate judges -- another commit, a file more, a file fewer, none gone
// where one is, one gone where none is -- is put right by that gate with one
// event, and a record that names exactly what the gate accepts gains none.
// What the record names is written by hand here, because no command leaves the
// same commit with other files: the freeze changes only by a person's
// unfreeze, which empties the field.
func TestAC2OnlyTheSameTrunkCommitFilesAndGoneAreTheSameAcceptance(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name   string
		named  func(cut string, want ktWant) ktWant // what the record names before the gate
		events int
	}{
		{name: "exactly what the gate accepts", events: 1,
			named: func(_ string, want ktWant) ktWant { return want }},
		{name: "another trunk commit", events: 2,
			named: func(cut string, want ktWant) ktWant { want.commit = cut; return want }},
		{name: "a file more", events: 2,
			named: func(_ string, want ktWant) ktWant {
				want.files = append(slices.Clone(want.files), otRefundTest)
				return want
			}},
		{name: "no file where there is one", events: 2,
			named: func(_ string, want ktWant) ktWant { want.files = nil; return want }},
		{name: "none gone where one is", events: 2,
			named: func(_ string, want ktWant) ktWant { want.gone = nil; return want }},
		{name: "another file gone", events: 2,
			named: func(_ string, want ktWant) ktWant { want.gone = []string{otInvoiceTest}; return want }},
	} {
		t.Run("AC-2 the record names "+row.name, func(t *testing.T) {
			main, wt, moved, cut := ktReworked(t, ktRemovesTax, model.GateVerification)
			want := ktTook(moved, ktRemovesTax)
			if got := len(ktEvents(t, main)); got != 1 {
				t.Fatalf("precondition: the record has %d trunk_tests events after the implementation passed, want 1", got)
			}
			ktWrites(t, main, row.named(cut, *want))

			ktSaysAccepted(t, ktPass(t, wt, model.GateVerification), want)
			ktHolds(t, main, "after the verification passed over a record that named "+row.name, want, row.events)
			ktLastEventNames(t, main, "after the verification passed", want)
		})
	}
}

// ---------------------------------------------------------------- AC-3

// AC-3, rows 12 and 13: each way the commit gate is opened again without a
// gate that judges -- itself recorded as failed or pending, a gate before it
// recorded as failed or pending, a gate before it that does not check the
// freeze recorded as passed -- leaves the record with no trunk_tests, no new
// event, and nothing else the commit gate recorded, the replay mark included.
// Whose the commits are stays.
func TestAC3TheCommitGateOpenedAgainEmptiesTheRecordsTrunkTestsWithTheRestItRecorded(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		gate   model.Gate
		status model.GateStatus
		move   ktMove
	}{
		{model.GateCommit, model.GateFail, ktGainsRefund},
		{model.GateCommit, model.GatePending, ktRemovesTax},
		{model.GateCodeReview, model.GateFail, ktRemovesTax},
		{model.GateVerification, model.GatePending, ktGainsRefund},
		{model.GateTestsFrozen, model.GateFail, ktGainsRefund},
		{model.GateVerifierReview, model.GatePass, ktRemovesTax},
		{model.GateDesignReview, model.GatePass, ktGainsRefund},
	} {
		t.Run("AC-3 "+string(row.gate)+" recorded "+string(row.status)+" where "+row.move.name, func(t *testing.T) {
			main, wt, moved := ktFinished(t, row.move)
			ktMarked(t, main, wt, moved)
			ktHolds(t, main, "on the finished story", ktTook(moved, row.move), 1)
			before := swRecord(t, main, "US-001")

			when := "after " + string(row.gate) + " was recorded " + string(row.status)
			if row.status == model.GatePass {
				ktSaysNothing(t, ktPass(t, wt, row.gate))
			} else {
				ktRecorded(t, wt, row.gate, row.status)
			}
			ktHolds(t, main, when, nil, 1)
			ktForgets(t, main, when, before)
		})
	}
}

// AC-3 with D1, rows 14 to 16: a guarded gate recorded as passed while the
// commit gate stands reopens the commit gate and judges in the one command.
// What the commit gate recorded goes, as AC-3 says, and the record then names
// what this gate accepted, with exactly one new event: the same files the
// commit gate had (row 14), other files on a later trunk commit (row 15), or
// nothing and no event where it accepted none (row 16). Today rows 14 and 15
// leave no trunk_tests, and row 14 no event either: the gate said what it
// accepted and the record said nothing.
func TestAC3AGuardedGatePassedWhileTheCommitGateStandsLeavesTheRecordNamingWhatItAccepted(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		gate model.Gate
		move ktMove
	}{
		{model.GateVerification, ktGainsRefund},
		{model.GatePlan, ktRemovesTax},
		{model.GateTestsFrozen, ktGainsRefund},
		{model.GateImplementation, ktRemovesTax},
	} {
		t.Run("AC-3 D1 "+string(row.gate)+" accepts what the commit gate had, where "+row.move.name, func(t *testing.T) {
			main, wt, moved := ktFinished(t, row.move)
			ktMarked(t, main, wt, moved)
			want := ktTook(moved, row.move)
			ktHolds(t, main, "on the finished story", want, 1)
			before := swRecord(t, main, "US-001")

			ktSaysAccepted(t, ktPass(t, wt, row.gate), want)
			when := "after " + string(row.gate) + " passed over the standing commit gate"
			ktForgets(t, main, when, before)
			ktHolds(t, main, when, want, 2)
			ktLastEventNames(t, main, when, want)
		})
	}

	t.Run("AC-3 D1 a guarded gate accepts other files on a later trunk commit", func(t *testing.T) {
		main, wt, moved := ktFinished(t, ktGainsRefund)
		ktMarked(t, main, wt, moved)
		ktHolds(t, main, "on the finished story", ktTook(moved, ktGainsRefund), 1)
		before := swRecord(t, main, "US-001")
		later := ktOnto(t, main, wt, ktRemovesTax)
		want := &ktWant{commit: later, files: []string{otRefundTest, otLaterTest}, gone: []string{otTaxTest}}

		ktSaysAccepted(t, ktPass(t, wt, model.GateImplementation), want)
		when := "after the implementation passed over the standing commit gate, on a later trunk commit"
		ktForgets(t, main, when, before)
		ktHolds(t, main, when, want, 2)
		ktLastEventNames(t, main, when, want)
	})

	t.Run("AC-3 D1 a guarded gate accepts nothing", func(t *testing.T) {
		main, wt, moved := ktFinished(t, ktGainsRefund)
		ktMarked(t, main, wt, moved)
		ktHolds(t, main, "on the finished story", ktTook(moved, ktGainsRefund), 1)
		before := swRecord(t, main, "US-001")
		otRemoveOnTrunk(t, main, otRefundTest)
		ldRebase(t, wt)

		ktSaysNothing(t, ktPass(t, wt, model.GateVerification))
		when := "after the verification passed over the standing commit gate and accepted nothing"
		ktForgets(t, main, when, before)
		ktHolds(t, main, when, nil, 1)
	})
}

// AC-3 with D3, rows 17 to 20: a person's `sdlc unfreeze` leaves the record
// with no trunk_tests and no new trunk_tests event, whether the commit gate
// stood (row 17), did not stand with tests_frozen passed (row 18), or did not
// stand with tests_frozen recorded as failed or pending, where the unfreeze
// reopens no gate at all (row 19). The tests are about to change, and an
// acceptance older than the freeze in force says nothing of them. Whose the
// commits are stays, and freezing again writes nothing of test files.
func TestAC3APersonsUnfreezeEmptiesTheRecordsTrunkTestsWhetherOrNotTheCommitGateStands(t *testing.T) {
	t.Parallel()
	lifted := func(t *testing.T, main, wt, when string, stood bool) {
		t.Helper()
		const events = 1
		before := swRecord(t, main, "US-001")
		wtMust(t, wt, "", "", "unfreeze", "--reason", "AC-2's test has to change")
		after := swRecord(t, main, "US-001")
		if n := len(after.Events); n == 0 || !slices.ContainsFunc(after.Events[len(before.Events):], func(e model.Event) bool { return e.Type == "unfreeze" }) {
			t.Fatalf("precondition, %s: the record has no unfreeze event from the person's command", when)
		}
		ktHolds(t, main, "after a person's unfreeze "+when, nil, events)
		if stood {
			ktForgets(t, main, "after a person's unfreeze "+when, before)
		} else {
			ktKeepsForks(t, main, "after a person's unfreeze "+when, before)
		}

		// Freezing again starts the list of the story's own commits afresh,
		// which is the freeze's to do, so the list is not read after it.
		wtMust(t, wt, ldSession, "", "freeze")
		ktHolds(t, main, "after the tests were frozen again, "+when, nil, events)
	}

	t.Run("AC-3 D3 the commit gate stood", func(t *testing.T) {
		main, wt, moved := ktFinished(t, ktRemovesTax)
		ktMarked(t, main, wt, moved)
		ktHolds(t, main, "on the finished story", ktTook(moved, ktRemovesTax), 1)
		lifted(t, main, wt, "while the commit gate stood", true)
	})

	for _, row := range []struct {
		name string
		move ktMove
		upTo model.Gate         // the first gate of the rework not passed again
		then []model.GateStatus // what tests_frozen is recorded as before the unfreeze
	}{
		{"tests_frozen passed", ktGainsRefund, model.GateCommit, nil},
		{"tests_frozen recorded as failed", ktRemovesTax, model.GateVerification, []model.GateStatus{model.GateFail}},
		{"tests_frozen put back to pending", ktGainsRefund, model.GateCommit, []model.GateStatus{model.GatePending}},
	} {
		t.Run("AC-3 D3 the commit gate did not stand, "+row.name, func(t *testing.T) {
			main, wt, moved, _ := ktReworked(t, row.move, row.upTo)
			want := ktTook(moved, row.move)
			for _, status := range row.then {
				ktRecorded(t, wt, model.GateTestsFrozen, status)
			}
			if ktCommitStands(t, main) {
				t.Fatal("precondition: the commit gate stands")
			}
			ktHolds(t, main, "before the unfreeze, a gate of the rework having accepted them", want, 1)
			lifted(t, main, wt, "while the commit gate did not stand, "+row.name, false)
		})
	}
}

// ---------------------------------------------------------------- the stop

// AC-1 at the stop, rows 21, 22 and 24, which do not change: a stop that lands
// the story where its commit gate judged it leaves the record naming the same
// files with no new event; a stop that first puts the story onto a later trunk
// commit itself names that commit and its files with one new event; and the
// stop lifting the finished story's freeze, which is not a person's unfreeze,
// leaves the landed record naming them.
func TestAC1TheStopLeavesTheLandedRecordNamingWhatWasAcceptedWithAndWithoutItsOwnReplay(t *testing.T) {
	t.Parallel()
	liftedByTheStop := func(t *testing.T, main string) {
		t.Helper()
		events := swRecord(t, main, "US-001").Events
		if n := len(events); n == 0 || events[n-1].Type != "unfreeze" {
			t.Fatalf("precondition: the landed record does not end with the stop lifting the freeze: %+v", events)
		}
	}
	t.Run("AC-1 trunk where the commit gate judged it", func(t *testing.T) {
		main, wt, moved := ktFinished(t, ktRemovesTax)
		want := ktTook(moved, ktRemovesTax)
		ktHolds(t, main, "on the finished story", want, 1)

		otLands(t, main, wt, moved)
		liftedByTheStop(t, main)
		ktHolds(t, main, "on the landed record", want, 1)
	})
	t.Run("AC-1 trunk moved again, and the stop put the story onto it", func(t *testing.T) {
		main, wt, moved := ktFinished(t, ktGainsRefund)
		ktHolds(t, main, "on the finished story", ktTook(moved, ktGainsRefund), 1)
		ktGainsTwo.do(t, main)
		later := ldTrunk(t, main)
		want := &ktWant{commit: later, files: []string{otRefundTest, ktLedgerTest, otLaterTest}}

		otLands(t, main, wt, later)
		liftedByTheStop(t, main)
		ktHolds(t, main, "on the landed record", want, 2)
		ktLastEventNames(t, main, "on the landed record", want)
	})
}
