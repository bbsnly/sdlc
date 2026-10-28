package cli

// US-029.3, in process: the landing as PLAN.md lays it out, state by state,
// and the cases its reviewers asked a test for.
//
//   - The commands run whenever the change is not the verified one, rebased
//     or not (red team H1), and where trunk already holds a changed tip.
//   - A refused fast-forward leaves the record exactly as it was: no landing
//     mark, change_base as the commit gate wrote it (red team O3).
//   - Nothing the story's branch did to the backlog reaches trunk (security
//     round 2, B2): a change to another story's status is refused on both
//     landing paths.
//   - What passing means is trunk's committed configuration, not the main
//     checkout's disk (T5), and the number judged is the last one on stdout.
//   - A person's write to the record while the commands run is neither lost
//     nor landed over (red team D1).
//   - Each refusal in PLAN.md's table of states, with its code; the fix a
//     person runs where trunk no longer holds the reviewed base; and what
//     stop, status, the gates and approve print around a landing.
//
// Everything here drives the commands through Execute and reads what a
// caller can see, as landing_test.go does, and shares its helpers.
//
// Helpers carry an lx prefix.

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// lxRepository is ldRepository with every quality command configured and
// passing, and the thresholds set; set, where given, changes commands, and
// thresholds, where given, replaces ldThresholds'.
func lxRepository(t *testing.T, set map[string]string, thresholds map[string]any, stories ...wtStory) string {
	t.Helper()
	root := wtTempDir(t)
	lxRepositoryAt(t, root, set, thresholds, stories...)
	return root
}

// lxRepositoryAt is lxRepository in root, an empty directory, for a walked
// template's builder.
func lxRepositoryAt(t *testing.T, root string, set map[string]string, thresholds map[string]any, stories ...wtStory) {
	t.Helper()
	commands := ldPassing()
	maps.Copy(commands, set)
	ldRepositoryAt(t, root, func(root string) {
		setConfigOnDisk(t, root, "commands", commands)
		if thresholds == nil {
			ldThresholds(t, root)
		} else {
			setConfigOnDisk(t, root, "thresholds", thresholds)
		}
	}, stories...)
}

// lxRecord is the story's record as it is on disk, every field of it.
func lxRecord(t *testing.T, main string) map[string]any {
	t.Helper()
	var r map[string]any
	wtReadJSON(t, wtRecordPath(main, "US-001"), &r)
	return r
}

// lxChangeAfterTheReviews commits a change to the story's code after the
// retro, and returns the new tip.
func lxChangeAfterTheReviews(t *testing.T, wt string) string {
	t.Helper()
	return swCommitFile(t, wt, "refund.go", "package billing\n\ntype Refund struct{ Amount int }\n", "after the reviews")
}

// lxMarker is a command that writes the commit it runs on to a file, and the
// file.
func lxMarker(t *testing.T) (command, file string) {
	t.Helper()
	file = filepath.Join(wtTempDir(t), "ran")
	return "git rev-parse HEAD > " + suSh(file), file
}

func lxNotDone(t *testing.T, main string) {
	t.Helper()
	if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusDone) {
		t.Error("a story that did not land is done")
	}
}

// lxWithout is swRefs' listing without the named ref's line.
func lxWithout(refs, ref string) string {
	var kept []string
	for line := range strings.Lines(refs) {
		if !strings.HasPrefix(line, ref+" ") {
			kept = append(kept, strings.TrimRight(line, "\n"))
		}
	}
	return strings.Join(kept, "\n")
}

// lxAt is a fixed moment, for what a test writes to a record itself.
var lxAt = time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------- H1

// AC-4 and red team H1: a change that is not the verified one has the
// commands run on it though nothing was rebased, and lands only when they
// pass on the tree that lands.
func TestAC4AChangedChangeHasTheCommandsRunWithoutARebase(t *testing.T) {
	t.Parallel()
	t.Run("AC-4 they pass", func(t *testing.T) {
		t.Parallel()
		mark, ran := lxMarker(t)
		main := lxRepository(t, map[string]string{"test": mark}, nil)
		wt := ldFinished(t, main)
		ldBookkeeping(t, wt)
		tip := lxChangeAfterTheReviews(t, wt)
		ldReviewAgain(t, wt)

		ldLanded(t, main, wt, tip)
		if got := strings.TrimSpace(wtRead(t, ran)); got != tip {
			t.Errorf("commands.test last ran on %q, want the changed tree that landed, %s", got, tip)
		}
	})
	t.Run("AC-4 one fails", func(t *testing.T) {
		t.Parallel()
		main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true, lx: true, set: map[string]string{"test": "exit 1"}})
		lxChangeAfterTheReviews(t, wt)
		ldReviewAgain(t, wt)
		before := ldTrunk(t, main)

		out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
		said, _ := ldRefusal(t, out, ldCheckFailed)
		suSays(t, said, "commands.test")
		if got := ldTrunk(t, main); got != before {
			t.Errorf("trunk moved to %s though commands.test failed on the changed tree", got)
		}
		ldStillHeld(t, main, wt)
	})
}

// AC-4, red team: where trunk already holds the story's tip and no landing
// mark stands for commands that passed on it, a changed change still has the
// commands run, and a failing one keeps the story from being recorded as
// landed.
func TestAC4WhereTrunkAlreadyHoldsAChangedTipTheCommandsStillRun(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true, lx: true, set: map[string]string{"test": "exit 1"}})
	ldOnSpike(t, main)
	ldBookkeeping(t, wt)
	tip := lxChangeAfterTheReviews(t, wt)
	ldReviewAgain(t, wt)
	swGit(t, main, "update-ref", "refs/heads/main", tip)

	out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
	said, _ := ldRefusal(t, out, ldCheckFailed)
	suSays(t, said, "commands.test")
	lxNotDone(t, main)
	if got := swRecord(t, main, "US-001").Commit; got == tip {
		t.Error("the record names the changed tip as landed though its commands failed")
	}
}

// ---------------------------------------------------------------- O3

// Red team O3: a fast-forward that is refused leaves the record as the
// commit gate and the retro left it -- no landing mark, change_base as the
// commit gate wrote it -- and the backlog and the claim with it.
func TestO3ARefusedFastForwardLeavesTheRecordAsItWas(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, rel, body string }{
		{"O3 a change in the way", "billing.go", ldBilling + "// a person's edit\n"},
		{"O3 an untracked file in the way", "refund.go", "package billing // a person's own\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
			before := lxRecord(t, main)
			if base, _ := before["change_base"].(string); base == "" {
				t.Error("the commit gate recorded no change_base")
			}
			backlog := wtRead(t, filepath.Join(main, "user_stories.json"))
			writeFile(t, main, tc.rel, tc.body)

			swRefused(t, ldUncommitted, wt, ldSession, "stop")
			after := lxRecord(t, main)
			if _, ok := after["landing"]; ok {
				t.Errorf("the landing mark was left behind: %v", after["landing"])
			}
			if after["change_base"] != before["change_base"] {
				t.Errorf("change_base is %v after the refusal, want the commit gate's %v", after["change_base"], before["change_base"])
			}
			if !reflect.DeepEqual(after, before) {
				t.Error("the refused landing changed the story's record")
			}
			if got := wtRead(t, filepath.Join(main, "user_stories.json")); got != backlog {
				t.Error("the refused landing changed the backlog")
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// ---------------------------------------------------------------- B2

// Security round 2, B2: the backlog is the loop's. A commit on the story's
// branch that changes only another story's status -- which the review subject
// does not see -- is refused at the landing with SDLC-E0055, naming the file,
// whether the landing would move trunk or finds trunk already holding the
// tip. No checkout has trunk checked out, so nothing else stands in the way.
func TestB2ABacklogChangeOnTheStorysBranchIsRefusedAtTheLanding(t *testing.T) {
	t.Parallel()
	for _, already := range []bool{false, true} {
		name := map[bool]string{false: "B2 trunk at the cut", true: "B2 trunk already holding the tip"}[already]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			main, wt := ldWalked(t, ldWalk{stories: []wtStory{{id: "US-001"}, {id: "US-002"}}})
			ldOnSpike(t, main)
			ldWork(t, wt)
			wtSetStatus(t, wt, "US-002", model.StatusDone)
			swPass(t, wt, ldSession, model.GateImplementation, "")
			tip := ldBookkeeping(t, wt)
			if already {
				swGit(t, main, "update-ref", "refs/heads/main", tip)
			}
			before := ldTrunk(t, main)

			out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
			said, _ := ldRefusal(t, out, ldCheckFailed)
			suSays(t, said, strconv.Quote("user_stories.json"))
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s", got)
			}
			lxNotDone(t, main)
			if got := wtStatuses(t, main)["US-002"]; got == string(model.StatusDone) {
				t.Error("another story's status reached the backlog")
			}
		})
	}
}

// ---------------------------------------------------------------- T5, D11

// THREATS T5: what the commands are is trunk's committed configuration. An
// uncommitted change to the main checkout's configuration that would make a
// failing command pass does not let the story land.
func TestT5TheMainCheckoutsUncommittedConfigurationDoesNotDecideWhatPasses(t *testing.T) {
	t.Parallel()
	main := lxRepository(t, map[string]string{"test": "test ! -f CHANGES.md"}, nil)
	wt := ldFinished(t, main)
	ldBookkeeping(t, wt)
	ldMoveTrunk(t, main)
	ldRebase(t, wt)
	setConfigOnDisk(t, main, "commands", ldPassing())
	before := ldTrunk(t, main)

	out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
	said, _ := ldRefusal(t, out, ldCheckFailed)
	suSays(t, said, "commands.test")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s on the main checkout's uncommitted commands", got)
	}
}

// AC-4 and red team D11: coverage and mutation pass on exit 0 and a last
// number on stdout at least the threshold -- not the first number, not one on
// stderr -- and a zero threshold needs no number.
func TestAC4TheNumberJudgedIsTheLastOnePrintedOnStdout(t *testing.T) {
	t.Parallel()
	zero := map[string]any{"diff_size_cap": 0, "coverage_min": 0, "mutation_min": 70}
	for _, tc := range []struct {
		name, command string
		thresholds    map[string]any
		lands         bool
	}{
		{name: "AC-4 a larger number on stderr", command: "echo 50; echo 99 >&2"},
		{name: "AC-4 a passing number first", command: "echo 99; echo 50"},
		{name: "AC-4 a passing number and a failing exit", command: "echo 95; exit 3"},
		{name: "AC-4 a passing number last", command: "echo 50; echo 95", lands: true},
		{name: "AC-4 no number against a zero threshold", command: "exit 0", thresholds: zero, lands: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main := lxRepository(t, map[string]string{"coverage": tc.command}, tc.thresholds)
			wt := ldFinished(t, main)
			ldBookkeeping(t, wt)
			ldMoveTrunk(t, main)
			rebased := ldRebase(t, wt)
			if tc.lands {
				ldLanded(t, main, wt, rebased)
				return
			}
			before := ldTrunk(t, main)
			out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
			said, _ := ldRefusal(t, out, ldCheckFailed)
			suSays(t, said, "commands.coverage")
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s though commands.coverage did not pass", got)
			}
		})
	}
}

// ---------------------------------------------------------------- D1

// Red team D1: while the commands run, a person may reopen the story. The
// stop then does not land it, and what the person wrote is still on the
// record afterwards. The person's write is made by the command itself, at the
// moment it runs: the record as `sdlc gate verification fail` leaves it.
func TestD1AStoryAPersonReopensWhileTheCommandsRunIsNotLandedAndTheirWriteStays(t *testing.T) {
	t.Parallel()
	reopened := filepath.Join(wtTempDir(t), "reopened.json")
	main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
	commands := ldPassing()
	commands["test"] = "cp " + suSh(reopened) + " " + suSh(wtRecordPath(main, "US-001"))
	setConfigOnDisk(t, main, "commands", commands)
	ldThresholds(t, main)
	suCommit(t, main, "commands that reopen the story as they run", ".sdlc/config.json")
	ldRebase(t, wt)

	r := swRecord(t, main, "US-001")
	r.SetGate(model.GateVerification, model.GateFail, "reopened by a person", lxAt)
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reopened, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	before := ldTrunk(t, main)

	swRefused(t, ldAny, wt, ldSession, "stop")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s though the story was reopened while the commands ran", got)
	}
	if got := swRecord(t, main, "US-001").Gates[model.GateVerification].Status; got != model.GateFail {
		t.Errorf("verification is %q on the record after the stop, want the person's fail kept", got)
	}
	lxNotDone(t, main)
}

// ---------------------------------------------------------------- D10

// Red team D10 and AC-10: a landing cut short after trunk moved and before
// the record was written leaves its mark; with one more commit on the story's
// branch since, the next stop lands the rest and records the whole range,
// from the mark's start.
func TestAC10ALandingCutShortAndACommitSinceRecordTheWholeRange(t *testing.T) {
	t.Parallel()
	main, wt, cut := ldWalkedFrom(t, ldWalk{finished: true})
	ldOnSpike(t, main)
	first := ldBookkeeping(t, wt)
	r := lxRecord(t, main)
	r["landing"] = map[string]any{"from": cut, "to": first}
	wtWriteJSON(t, wtRecordPath(main, "US-001"), r)
	swGit(t, main, "update-ref", "refs/heads/main", first, cut)
	tip := swCommitFile(t, wt, "CODEMAP.md", "# CODEMAP\n\nbilling.go keeps invoices, and refunds are in refund.go.\n",
		"docs: map US-001 again")

	ldLanded(t, main, wt, tip)
	got := swRecord(t, main, "US-001")
	if got.CommitBase != cut || got.Commit != tip {
		t.Errorf("the record's range is %s..%s, want the whole landing, %s..%s", got.CommitBase, got.Commit, cut, tip)
	}
	if _, ok := lxRecord(t, main)["landing"]; ok {
		t.Error("the landing mark is still on the record after the story landed")
	}
}

// ---------------------------------------------------------------- states

// PLAN.md's table of states: each way the worktree, trunk, the range, the
// commands or the checkouts can stand that is not a landing is refused with
// its code, moves no branch, leaves the story not done, and ends saying
// nothing landed.
func TestLandingRefusesEachStateWithItsCode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code string
		commands   map[string]string
		spike      bool // no checkout has trunk checked out
		already    bool // trunk is moved to the story's tip after prepare
		ownMove    bool // the story's branch is moved by a command, not the landing
		prepare    func(t *testing.T, main, wt string)
		names      string
	}{
		{name: "E0055 the worktree part-way through a rebase", code: ldCheckFailed,
			prepare: func(t *testing.T, main, wt string) {
				t.Helper()
				ldMoveTrunk(t, main)
				swGitTry(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "--exec", "false", "main")
			}},
		{name: "E0055 the worktree on no branch", code: ldCheckFailed,
			prepare: func(t *testing.T, _, wt string) {
				t.Helper()
				swGit(t, wt, "switch", "--quiet", "--detach")
			}},
		{name: "E0055 the worktree on another branch", code: ldCheckFailed,
			prepare: func(t *testing.T, _, wt string) {
				t.Helper()
				swGit(t, wt, "switch", "--quiet", "-c", "other")
			}},
		{name: "E0055 the story's branch gone", code: ldCheckFailed,
			prepare: func(t *testing.T, _, wt string) {
				t.Helper()
				swGit(t, wt, "switch", "--quiet", "--detach")
				swGit(t, wt, "branch", "--quiet", "-D", "story/US-001")
			}},
		{name: "E0045 trunk's branch gone", code: "SDLC-E0045", spike: true,
			prepare: func(t *testing.T, main, _ string) {
				t.Helper()
				swGit(t, main, "branch", "--quiet", "-D", "main")
			}},
		{name: "E0054 a merge commit on the story's branch", code: ldBehindTrunk,
			prepare: func(t *testing.T, main, wt string) {
				t.Helper()
				lxMerge(t, main, wt)
			}},
		{name: "E0055 a merge commit trunk already holds", code: ldCheckFailed, spike: true, already: true,
			prepare: func(t *testing.T, main, wt string) {
				t.Helper()
				lxMerge(t, main, wt)
			}},
		{name: "E0055 the loop's own files in the range", code: ldCheckFailed, names: ".sdlc/notes.json",
			prepare: func(t *testing.T, _, wt string) {
				t.Helper()
				swCommitFile(t, wt, ".sdlc/notes.json", "{}\n", "the loop's files")
			}},
		{name: "E0055 the loop's own files in another case", code: ldCheckFailed, spike: true,
			prepare: func(t *testing.T, _, wt string) {
				t.Helper()
				swCommitFile(t, wt, ".SDLC/notes.json", "{}\n", "the loop's files, in capitals")
			}},
		{name: "E0032 the worktree not clean before the commands", code: "SDLC-E0032",
			prepare: func(t *testing.T, main, wt string) {
				t.Helper()
				ldMoveTrunk(t, main)
				ldRebase(t, wt)
				writeFile(t, wt, "README.md", "# Billing, edited in the worktree\n")
			}},
		{name: "E0055 a command that moves a branch", code: ldCheckFailed, ownMove: true,
			commands: map[string]string{"test": "git -c user.email=t@example.com -c user.name=Test commit --quiet --allow-empty -m sneaky"},
			prepare: func(t *testing.T, main, wt string) {
				t.Helper()
				ldMoveTrunk(t, main)
				ldRebase(t, wt)
			}},
		{name: "E0055 a command that rewrites a tracked file", code: ldCheckFailed, names: "README.md",
			commands: map[string]string{"build": "echo rebuilt >> README.md"},
			prepare: func(t *testing.T, main, wt string) {
				t.Helper()
				ldMoveTrunk(t, main)
				ldRebase(t, wt)
			}},
		{name: "E0055 coverage that prints no number", code: ldCheckFailed,
			commands: map[string]string{"coverage": "exit 0"},
			prepare: func(t *testing.T, main, wt string) {
				t.Helper()
				ldMoveTrunk(t, main)
				ldRebase(t, wt)
			}},
		{name: "E0056 a merge part-way in the main checkout", code: ldUncommitted,
			prepare: func(t *testing.T, main, _ string) {
				t.Helper()
				writeFile(t, main, ".git/MERGE_HEAD", ldTrunk(t, main)+"\n")
			}},
		{name: "E0056 the main checkout's index locked", code: ldUncommitted, names: "index.lock",
			prepare: func(t *testing.T, main, _ string) {
				t.Helper()
				writeFile(t, main, ".git/index.lock", "")
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			main, wt := ldWalked(t, ldWalk{finished: true, lx: true, set: tc.commands})
			if tc.spike {
				ldOnSpike(t, main)
			}
			ldBookkeeping(t, wt)
			tc.prepare(t, main, wt)
			if tc.already {
				swGit(t, main, "update-ref", "refs/heads/main", swRev(t, wt, "HEAD"))
			}
			refs := swRefs(t, main)

			out := swRefused(t, tc.code, wt, ldSession, "stop")
			if tc.names != "" && !wtNames(out, tc.names) {
				t.Errorf("the refusal does not name %s:\n%s", tc.names, out)
			}
			suSays(t, out, "Nothing landed")
			got := swRefs(t, main)
			if tc.ownMove {
				refs, got = lxWithout(refs, "refs/heads/story/US-001"), lxWithout(got, "refs/heads/story/US-001")
			}
			if got != refs {
				t.Errorf("the refused landing moved a branch:\nbefore\n%s\nafter\n%s", refs, got)
			}
			lxNotDone(t, main)
		})
	}
}

// lxMerge puts a merge commit on the story's branch: a side line off trunk,
// with trunk's own tree, merged in.
func lxMerge(t *testing.T, main, wt string) {
	t.Helper()
	trunk := ldTrunk(t, main)
	side := swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit-tree", trunk+"^{tree}", "-p", trunk, "-m", "a side line")
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"merge", "--quiet", "--no-ff", "--no-edit", side)
}

// A second stop while one is landing the story is refused with SDLC-E0006,
// and lands nothing.
func TestAStopWhileAnotherIsLandingTheStoryIsRefused(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
	cfg, err := config.Load(main)
	if err != nil {
		t.Fatal(err)
	}
	// Asked for by its method, not called, so that this package compiles
	// before the store has it: LockLanding(id) (*Guard, error), PLAN.md step 7.
	var s any = store.New(&config.Project{Root: main, Config: cfg})
	locks, ok := s.(interface {
		LockLanding(id string) (*store.Guard, error)
	})
	if !ok {
		t.Fatal("the store has no LockLanding(id) (*Guard, error) to hold a story's landing with")
	}
	held, err := locks.LockLanding("US-001")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	before := ldTrunk(t, main)

	swRefused(t, "SDLC-E0006", wt, ldSession, "stop")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s while another stop held the landing", got)
	}
	ldStillHeld(t, main, wt)
}

// E0054 where trunk no longer holds the reviewed base -- rewritten, or gone
// back behind it -- is the fix a person runs: built from the base's own hash,
// and handing the story to a person. The plain rebase the session may run is
// not offered.
func TestAC3WhereTrunkNoLongerHoldsTheReviewedBaseTheFixIsAPersons(t *testing.T) {
	t.Parallel()
	for _, rewritten := range []bool{true, false} {
		name := map[bool]string{true: "AC-3 trunk rewritten", false: "AC-3 trunk gone back behind the base"}[rewritten]
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			main, wt, cut := ldWalkedFrom(t, ldWalk{finished: true, trunkMoved: true})
			ldOnSpike(t, main)
			ldBookkeeping(t, wt)
			back := swRev(t, main, cut+"~1")
			if rewritten {
				back = swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test",
					"commit-tree", cut+"^{tree}", "-p", back, "-m", "trunk rewritten")
			}
			swGit(t, main, "update-ref", "refs/heads/main", back, cut)

			out := swRefused(t, ldBehindTrunk, wt, ldSession, "stop")
			_, fix := ldRefusal(t, out, ldBehindTrunk)
			suSays(t, fix, cut[:12], "sdlc escalate")
			if ldRebaseCommand.MatchString(out) {
				t.Errorf("the plain rebase is offered where trunk no longer holds the base:\n%s", out)
			}
			if got := ldTrunk(t, main); got != back {
				t.Errorf("trunk moved to %s", got)
			}
		})
	}
}

// ---------------------------------------------------------------- L0 (b)

// After an escalation, the story is a person's: stop in its worktree with no
// iteration running is refused while the question is open, refused again
// after a rejection, and lands once a person approves.
func TestAFinishedStoryHandedToAPersonLandsOnlyOnceTheyApprove(t *testing.T) {
	t.Parallel()
	t.Run("pending, then approved", func(t *testing.T) {
		t.Parallel()
		main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
		tip := swRev(t, wt, "HEAD")
		wtMust(t, wt, ldSession, "", "escalate", "landing", "--message", "land it now?")

		swRefused(t, "SDLC-E0036", wt, "", "stop")
		if got := ldTrunk(t, main); got == tip {
			t.Fatal("a story with a question open landed")
		}
		approved := wtMust(t, wt, "", "", "approve", "US-001")
		suSays(t, approved.stdout, "`sdlc stop` in .sdlc/worktrees/US-001 lands it")

		wtMust(t, wt, "", "", "stop")
		if got := ldTrunk(t, main); got != tip {
			t.Errorf("trunk is at %s after the approved stop, want %s", got, tip)
		}
		if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
			t.Errorf("US-001 is %s, want done", got)
		}
	})
	t.Run("rejected", func(t *testing.T) {
		t.Parallel()
		main, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
		before := ldTrunk(t, main)
		wtMust(t, wt, ldSession, "", "escalate", "landing", "--message", "land it now?")
		wtMust(t, wt, "", "", "approve", "US-001", "--reject", "not this week")

		swRefused(t, "SDLC-E0033", wt, "", "stop")
		if got := ldTrunk(t, main); got != before {
			t.Errorf("a rejected story landed, trunk at %s", got)
		}
	})
}

// ---------------------------------------------------------------- words

// What the landing says it did: the move and the checkout that moved with it,
// or that only the branch moved; a story already on trunk; what was removed;
// and what was kept.
func TestWhatALandingSaysItDid(t *testing.T) {
	t.Parallel()
	said := func(t *testing.T, wt string) string {
		t.Helper()
		r := wtMust(t, wt, ldSession, "", "stop")
		return r.stdout + r.stderr
	}
	t.Run("trunk checked out in the main checkout", func(t *testing.T) {
		t.Parallel()
		_, wt, from := ldWalkedFrom(t, ldWalk{finished: true, bookkept: true})
		tip := swRev(t, wt, "HEAD")
		suSays(t, said(t, wt), "main moved from "+from[:12]+" to "+tip[:12]+" by fast-forward", "moved with it",
			"Removed .sdlc/worktrees/US-001", "not committed")
	})
	t.Run("no checkout with trunk checked out", func(t *testing.T) {
		t.Parallel()
		main, wt := ldWalked(t, ldWalk{finished: true})
		ldOnSpike(t, main)
		ldBookkeeping(t, wt)
		suSays(t, said(t, wt), "only the branch moved")
	})
	t.Run("already on trunk", func(t *testing.T) {
		t.Parallel()
		main, wt := ldWalked(t, ldWalk{finished: true})
		ldOnSpike(t, main)
		tip := ldBookkeeping(t, wt)
		swGit(t, main, "update-ref", "refs/heads/main", tip)
		suSays(t, said(t, wt), "US-001 was already on main at "+tip[:12])
	})
	t.Run("kept", func(t *testing.T) {
		t.Parallel()
		_, wt := ldWalked(t, ldWalk{finished: true, bookkept: true})
		writeFile(t, wt, "scratch.txt", "a thought\n")
		out := said(t, wt)
		suSays(t, out, "Kept .sdlc/worktrees/US-001 and story/US-001")
		if !wtNames(out, "scratch.txt") {
			t.Errorf("the kept line does not name scratch.txt:\n%s", out)
		}
	})
}

// A finished story waits to land in its worktree: the commit gate says where
// it is committed, the retro does not say it is done, and status there says
// that stop lands it.
func TestAFinishedStoryInItsWorktreeIsSaidToWaitToLand(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	satisfy(t, wt, model.GateCommit)
	committed := wtMust(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed")
	suSays(t, committed.stdout+committed.stderr, "committed on story/US-001 in")
	satisfy(t, wt, model.GateRetro)
	retro := decode[ldGatePayload](t, wtMust(t, wt, ldSession, "", "gate", "retro", "pass", "--note", "done", "--json"))
	if retro.Done {
		t.Error("the retro says a story that has not landed is done")
	}
	suSays(t, wtMust(t, wt, ldSession, "", "status").stdout, "sdlc stop")
	if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusDone) {
		t.Error("the backlog says done before the story landed")
	}
}

// ---------------------------------------------------------------- the change base

// Red team D4 as PLAN.md round 3 answers it: the change is measured from the
// commit the branch was cut from wherever HEAD still holds it, so none of the
// story's own commits is ever outside it, and its fingerprint is on the record
// from the commit gate. Only where a person's rebase took the cut out of HEAD
// is it measured from trunk's merge base with HEAD. A single checkout records
// none of it (AC-11).
//
// US-059 (AC-3, the person's first answer of 2026-10-12) changes the second
// case: a story rebased onto a trunk that only moved on still holds the cut,
// and trunk's commits lie between the cut and the story's own. Measured from
// the cut, the change took those in, and the commit gate warned that the story
// changes the loop's files where only trunk had. It is measured from the trunk
// commit the story sits on, which leaves none of the story's commits out.
func TestTheChangeIsRecordedAtTheCommitGateFromTheRightBase(t *testing.T) {
	t.Parallel()
	t.Run("D4 as cut", func(t *testing.T) {
		t.Parallel()
		main, _, cut := ldWalkedFrom(t, ldWalk{finished: true})
		r := lxRecord(t, main)
		if r["cut"] != cut || r["change_base"] != cut {
			t.Errorf("cut %v and change_base %v, want both %s", r["cut"], r["change_base"], cut)
		}
		if change, _ := r["change"].(string); change == "" {
			t.Error("the commit gate recorded no change")
		}
		if _, ok := r["landing"]; ok {
			t.Error("a landing mark was written before any landing")
		}
	})
	t.Run("D4 rebased onto a trunk that holds the cut", func(t *testing.T) {
		t.Parallel()
		main, wt, cut := ldWalkedFrom(t, ldWalk{})
		ldWork(t, wt)
		swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
		commitEverything(t, wt)
		moved := ldMoveTrunk(t, main)
		ldRebase(t, wt)
		ldReviewAgain(t, wt)
		wtMust(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed after a rebase")
		r := lxRecord(t, main)
		if r["cut"] != cut {
			t.Errorf("cut is %v, want the commit the branch was cut from, %s", r["cut"], cut)
		}
		if got := r["change_base"]; got != moved {
			t.Errorf("change_base is %v, want the trunk commit the story sits on, %s, and not the cut HEAD still holds, %s",
				got, moved, cut)
		}
	})
	t.Run("D4 rebased where HEAD no longer holds the cut", func(t *testing.T) {
		t.Parallel()
		main, wt, cut := ldWalkedFrom(t, ldWalk{trunkMoved: true})
		ldOnSpike(t, main)
		ldWork(t, wt)
		swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
		commitEverything(t, wt)
		rewritten := swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test",
			"commit-tree", cut+"^{tree}", "-p", cut+"~1", "-m", "trunk rewritten")
		swGit(t, main, "update-ref", "refs/heads/main", rewritten, cut)
		swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "--quiet", "--onto", "main", cut)
		ldReviewAgain(t, wt)
		wtMust(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "committed after a person's rebase")
		if got := lxRecord(t, main)["change_base"]; got != rewritten {
			t.Errorf("change_base is %v, want trunk's merge base with HEAD, %s", got, rewritten)
		}
	})
	t.Run("AC-11 a single checkout", func(t *testing.T) {
		t.Parallel()
		root := wtRepository(t, wtStory{id: "US-001"})
		wtStarted(t, root, ldSession)
		swReach(t, root, ldSession, "")
		r := lxRecord(t, root)
		for _, key := range []string{"cut", "change", "change_base", "landing"} {
			if _, ok := r[key]; ok {
				t.Errorf("a story in a single checkout has %s on its record", key)
			}
		}
	})
}
