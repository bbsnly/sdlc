package cli

// US-059 in process: the commit gate run again on a story that was put onto a
// trunk that moved.
//
// A story in its worktree gets onto the moved trunk in two ways: somebody
// rebases it by hand, or `sdlc stop` replays its commits there and then
// refuses because a configured command fails (SDLC-E0055), which sends the
// session back through the gates. Either way `sdlc gate commit pass` runs
// again. It measured the story from the commit its branch was cut from, so the
// range it printed and recorded took in trunk's commits (AC-3), and it warned
// that the story changes the loop's own files where only trunk had (AC-10).
// A story whose own commits change one is still warned, by name (AC-11).
//
// The AC numbers in this file are US-059's. Everything here reads what a
// caller can see: what the gate printed, the record on disk, git's own
// answers about commits the test made, and what a stop afterwards does. No
// test names a function the story has still to add.
//
// Helpers carry a cg prefix, for commit gate.

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	// cgConfig is the loop's own file another story's commit changes on trunk.
	cgConfig = ".sdlc/config.json"
	// cgTeam is a file of the project's own under the loop's directory, which
	// trunk holds before the story starts where a test says so.
	cgTeam = ".sdlc/team.md"
	// cgBacklog is the backlog, where the scaffold puts it.
	cgBacklog = "user_stories.json"

	cgTeamBody = "# Team\n\n// one\n// two\n// three\n// four\n// five\n// six\n// seven\n// eight\n// nine\n// ten\n"
	// cgNeedsFixed fails on a tree that has another story's CHANGES.md until
	// the story adds FIXED.md: it passes while the story is worked, fails on
	// the tree the stop makes on the moved trunk, and passes once reworked.
	cgNeedsFixed = "test ! -f CHANGES.md || test -f FIXED.md"
)

// cgCommit commits what is staged in dir.
func cgCommit(t *testing.T, dir, message string) string {
	t.Helper()
	swGit(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", message)
	return swRev(t, dir, "HEAD")
}

// cgTrunkMoves is another story landing on trunk after this one was cut: a
// file of its own and, in the same commit, a change to the loop's own
// configuration, which no commit of this story's touches.
func cgTrunkMoves(t *testing.T, main string) string {
	t.Helper()
	setConfigOnDisk(t, main, "loop", map[string]any{"max_review_rounds": 2, "max_rework_rounds": 3, "max_stop_blocks": 4})
	writeFile(t, main, "CHANGES.md", "another story's work\n")
	swGit(t, main, "add", "--", cgConfig, "CHANGES.md")
	return cgCommit(t, main, "another story: its work and the loop's configuration")
}

// cgGateAgain is the session running `sdlc gate commit pass` again in the
// story's worktree, which has to work.
func cgGateAgain(t *testing.T, wt string) result {
	t.Helper()
	return wtMust(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "the commit gate, run again")
}

// cgCommits is every commit in from..to, as git lists them.
func cgCommits(t *testing.T, dir, from, to string) []string {
	t.Helper()
	commits := strings.Fields(swGit(t, dir, "rev-list", from+".."+to))
	slices.Sort(commits)
	return commits
}

// cgHoldsRange holds what the gate printed and recorded to AC-3: the range is
// on..head, on being the trunk commit the story sits on and head the story's
// tip, both commits the test made; and what git lists over the recorded pair
// is the story's own commits, with none of trunk's among them.
func cgHoldsRange(t *testing.T, main, wt string, gate result, cut, on, head string) {
	t.Helper()
	want := "committed on story/US-001 in " + on[:12] + ".." + head[:12]
	printed := "(no such line)"
	for line := range strings.Lines(gate.stdout) {
		if strings.Contains(line, "committed on ") {
			printed = strings.TrimSpace(line)
		}
	}
	if printed != want {
		t.Errorf("the commit gate printed %q, want %q: from the trunk commit the story sits on to the story's tip", printed, want)
	}
	if strings.Contains(gate.stdout, "no range recorded") {
		t.Errorf("the commit gate recorded no range:\n%s", gate.stdout)
	}

	r := swRecord(t, main, "US-001")
	if r.CommitBase != on || r.Commit != head {
		t.Errorf("the record names %s..%s, want %s..%s", r.CommitBase, r.Commit, on, head)
	}
	if r.RangeUnknown || r.HeadPastCommit != "" {
		t.Errorf("the record says range_unknown %v and head_past_commit %q, want a range that ends at HEAD", r.RangeUnknown, r.HeadPastCommit)
	}
	if r.ChangeBase != on {
		t.Errorf("the record measures the story's change from %s, want %s, the trunk commit the story sits on", r.ChangeBase, on)
	}
	if r.CommitBase == "" || r.Commit == "" {
		return
	}
	own, trunks := cgCommits(t, wt, on, head), cgCommits(t, wt, cut, on)
	if len(own) == 0 || len(trunks) == 0 {
		t.Fatalf("precondition: the story has %d commits of its own and trunk gained %d", len(own), len(trunks))
	}
	held := cgCommits(t, wt, r.CommitBase, r.Commit)
	if !slices.Equal(held, own) {
		t.Errorf("the recorded range holds the commits\n%s\nwant the story's own:\n%s", strings.Join(held, "\n"), strings.Join(own, "\n"))
	}
	for _, c := range trunks {
		if slices.Contains(held, c) {
			t.Errorf("the recorded range holds %s, which is trunk's commit and not the story's", c[:12])
		}
	}
}

// cgWarns reports whether what the gate printed warns that the story changes
// path, in the words the gate has always used: the story's branch changes the
// path, which the landing refuses.
func cgWarns(gate result, path string) bool {
	for line := range strings.Lines(gate.stdout + gate.stderr) {
		if strings.Contains(line, "story/US-001 changes ") && strings.Contains(line, "which the landing refuses") &&
			wtNames(line, path) {
			return true
		}
	}
	return false
}

// cgNoWarning holds what the gate printed to AC-10: nothing says the story
// changes one of the loop's own files or the backlog, and the path only trunk
// changed is not named at all.
func cgNoWarning(t *testing.T, gate result, trunks string) {
	t.Helper()
	said := gate.stdout + gate.stderr
	if strings.Contains(said, "which the landing refuses") || strings.Contains(said, "story/US-001 changes ") {
		t.Errorf("the commit gate warns that the story changes the loop's own files, and only trunk's commit did:\n%s", said)
	}
	if wtNames(said, trunks) {
		t.Errorf("the commit gate names %s, which only trunk's commit changed:\n%s", trunks, said)
	}
}

// cgScene is a story put onto a trunk that moved, with the commit gate run
// again there: the commit the branch was cut from, the trunk commit the story
// now sits on, the story's tip, and what the gate printed.
type cgScene struct {
	main, wt, cut, on, head string
	gate                    result
}

// cgHow says how a scene's story was worked. hooked is with the hook running:
// each commit of the session's is made on a head the hook wrote down first,
// as store.ObserveCommit does when the hook allows a command that commits.
// The commits a rebase or the stop makes again are on heads nobody wrote down.
// Without it nothing was watched, as in a project that does not run the hook.
// set is commands of the project's configuration, every other one passing;
// with none the project configures no commands at all.
type cgHow struct {
	hooked bool
	set    map[string]string
}

// sees writes down the story's HEAD, where the hook is running.
func (h cgHow) sees(t *testing.T, wt string) {
	t.Helper()
	if h.hooked {
		watched(t, wt, swRev(t, wt, "HEAD"))
	}
}

// cgByHand is a finished story, with its bookkeeper's commit, rebased by hand
// onto a trunk another story moved, reviewed again there, and the commit gate
// run again. own, where given, commits something more of the story's before
// trunk moves.
//
// Where own is not given the scene is a walked template per how, made by the
// real commands once per package run: every test of it then starts from the
// same story, rebased and gated the same way, and reads the commits and what
// the gate printed from the template's notes.
func cgByHand(t *testing.T, how cgHow, own func(wt string)) cgScene {
	t.Helper()
	w := ldWalk{finished: true, lx: how.set != nil, set: how.set, identified: true, bookkept: !how.hooked}
	if own != nil {
		main, wt, cut := ldWalkedFrom(t, w)
		return cgByHandOn(t, how, own, main, wt, cut)
	}
	return cgSceneOf(t, "commit gate: rebased by hand, "+fmt.Sprintf("%+v", how), func(t *testing.T, root string) cgScene {
		t.Helper()
		cut := ldWalkAt(t, w, root)
		return cgByHandOn(t, how, nil, root, swWorktreeOf(root, "US-001"), cut)
	})
}

// cgByHandOn is cgByHand from the story ldWalk{finished, identified} leaves in
// main and its worktree wt, cut from the commit cut: the bookkeeper's commit
// is made here where the hook watches the story first.
func cgByHandOn(t *testing.T, how cgHow, own func(wt string), main, wt, cut string) cgScene {
	t.Helper()
	if how.hooked {
		// The story's work was committed on the commit its branch was cut
		// from, and its bookkeeper's commit on that one.
		watched(t, wt, cut)
		how.sees(t, wt)
		ldBookkeeping(t, wt)
	}
	if own != nil {
		own(wt)
	}
	on := cgTrunkMoves(t, main)
	head := ldRebase(t, wt)
	ldReviewAgain(t, wt)
	return cgScene{main: main, wt: wt, cut: cut, on: on, head: head, gate: cgGateAgain(t, wt)}
}

// cgSceneOf is a scene made by build in the empty directory a walked template
// is made in, and copied into a new directory of t's: the commits it names and
// what the gate printed are noted with the template and read back from the
// copy. The test's commands run in the story's worktree from ldSession.
func cgSceneOf(t *testing.T, key string, build func(t *testing.T, root string) cgScene) cgScene {
	t.Helper()
	main, notes := walkedCopyNoted(t, key, func(t *testing.T, root string) map[string]string {
		t.Helper()
		s := build(t, root)
		return map[string]string{"cut": s.cut, "on": s.on, "head": s.head,
			"stdout": s.gate.stdout, "stderr": s.gate.stderr, "code": strconv.Itoa(s.gate.code)}
	})
	code, err := strconv.Atoi(notes["code"])
	if err != nil {
		t.Fatal(err)
	}
	wt := swWorktreeOf(main, "US-001")
	chdir(t, wt)
	setenv(t, store.SessionEnv, ldSession)
	return cgScene{main: main, wt: wt, cut: notes["cut"], on: notes["on"], head: notes["head"],
		gate: result{stdout: notes["stdout"], stderr: notes["stderr"], code: code}}
}

// cgByTheStop is a finished story that `sdlc stop` put onto a trunk another
// story moved and then refused, because commands.test fails on that tree
// (SDLC-E0055). The session does what the refusal's fix says: it records the
// implementation as failed, puts the story right in a commit of its own, takes
// it through the gates again, and runs the commit gate. It is a walked template
// per how, as cgByHand's is.
func cgByTheStop(t *testing.T, how cgHow) cgScene {
	t.Helper()
	set := map[string]string{"test": cgNeedsFixed}
	maps.Copy(set, how.set)
	return cgSceneOf(t, "commit gate: put onto trunk by the stop's replay, "+fmt.Sprintf("%+v", how), func(t *testing.T, root string) cgScene {
		t.Helper()
		cut := ldWalkAt(t, ldWalk{finished: true, lx: true, set: set, identified: true}, root)
		main, wt := root, swWorktreeOf(root, "US-001")
		if how.hooked {
			watched(t, wt, cut)
		}
		how.sees(t, wt)
		ldBookkeeping(t, wt)
		on := cgTrunkMoves(t, main)

		swRefused(t, ldCheckFailed, wt, ldSession, "stop")
		if got := ldTrunk(t, main); got != on {
			t.Fatalf("precondition: the refused stop moved trunk to %s", got)
		}
		if _, ok := swGitTry(t, wt, "merge-base", "--is-ancestor", on, "HEAD"); !ok {
			t.Fatalf("precondition: the refused stop did not leave the story on trunk at %s", on[:12])
		}

		wtMust(t, wt, ldSession, "", "gate", "implementation", "fail", "--note", "commands.test fails on trunk as it is now")
		writeFile(t, wt, "FIXED.md", "what trunk's change asked of the story\n")
		swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
		how.sees(t, wt)
		satisfy(t, wt, model.GateCommit)
		head := swRev(t, wt, "HEAD")
		return cgScene{main: main, wt: wt, cut: cut, on: on, head: head, gate: cgGateAgain(t, wt)}
	})
}

// cgRetro passes the retro again: the commit gate recorded again reopens it,
// and a stop lands only a story with no gate left.
func cgRetro(t *testing.T, wt string) {
	t.Helper()
	swPass(t, wt, ldSession, model.GateRetro, "")
}

// ---------------------------------------------------------------- AC-3

// AC-3, a rebase by hand: the story's two commits sit on trunk's new commit,
// and the gate run again prints and records the range from that commit to the
// story's tip. Trunk's commit, which lies between the cut and the story's
// commits, is not in it.
func TestAC3TheCommitGateRunAgainAfterARebaseByHandNamesTheStorysOwnCommitsAndNoneOfTrunks(t *testing.T) {
	t.Parallel()
	s := cgByHand(t, cgHow{}, nil)
	cgHoldsRange(t, s.main, s.wt, s.gate, s.cut, s.on, s.head)
}

// AC-3, the stop's own replay: the stop put the story onto the moved trunk
// and refused on a command, and the story was reworked with a commit more.
// The gate run again names the story's three commits and not trunk's.
func TestAC3TheCommitGateRunAgainAfterTheStopsOwnReplayFailedNamesTheStorysOwnCommitsAndNoneOfTrunks(t *testing.T) {
	t.Parallel()
	s := cgByTheStop(t, cgHow{})
	if got := len(cgCommits(t, s.wt, s.on, s.head)); got != 3 {
		t.Fatalf("precondition: the story has %d commits on trunk's, want its work, its bookkeeper's and the rework", got)
	}
	cgHoldsRange(t, s.main, s.wt, s.gate, s.cut, s.on, s.head)
}

// AC-3 with the hook running, which is how a session's story is worked: the
// hook wrote down the heads the session committed on before the story was
// put onto trunk, and nobody wrote down the heads its commits were made on
// again. The gate run again still names the story's own commits, all of
// them, and not the one commit that sits on a head the hook did see, which
// is trunk's.
func TestAC3TheCommitGateRunAgainNamesTheStorysOwnCommitsWhereTheHookWatchedThemBeforeTheyWereMadeAgain(t *testing.T) {
	t.Parallel()
	t.Run("AC-3 after a rebase by hand", func(t *testing.T) {
		t.Parallel()
		s := cgByHand(t, cgHow{hooked: true}, nil)
		cgHoldsRange(t, s.main, s.wt, s.gate, s.cut, s.on, s.head)
		cgNoWarning(t, s.gate, cgConfig)
	})
	t.Run("AC-3 after the stop's own replay failed on a command", func(t *testing.T) {
		t.Parallel()
		s := cgByTheStop(t, cgHow{hooked: true})
		cgHoldsRange(t, s.main, s.wt, s.gate, s.cut, s.on, s.head)
		cgNoWarning(t, s.gate, cgConfig)
	})
}

// AC-3, trunk moved by more than one commit: none of them is in the range.
func TestAC3TheRangeLeavesOutEveryCommitTrunkGainedNotOnlyTheLast(t *testing.T) {
	t.Parallel()
	main, wt, cut := otWalked(t, ldWalk{finished: true, bookkept: true})
	cgTrunkMoves(t, main)
	swCommitFile(t, main, "NOTES.md", "a second story's work\n", "a second story lands")
	on := swCommitFile(t, main, "LATER.md", "a third story's work\n", "a third story lands")
	head := ldRebase(t, wt)
	ldReviewAgain(t, wt)

	cgHoldsRange(t, main, wt, cgGateAgain(t, wt), cut, on, head)
}

// AC-3 and the person's first answer: what `sdlc log` then reads from the
// record is the same range, so the story's entry does not list trunk's commit
// as the story's.
func TestAC3TheLogReadsTheStorysOwnRangeAfterTheGateRunAgain(t *testing.T) {
	t.Parallel()
	s := cgByHand(t, cgHow{}, nil)
	if got := ldLogged(t, s.wt); got.CommitBase != s.on || got.Commit != s.head {
		t.Errorf("sdlc log names %s..%s for US-001, want %s..%s", got.CommitBase, got.Commit, s.on, s.head)
	}
}

// AC-3 and the person's second answer: where the trunk commit the story sits
// on cannot be worked out -- the main checkout's configuration names a trunk
// this repository has no branch for -- the gate still passes, and starts the
// range where it did before, at the commit the branch was cut from. It can
// then warn of too much, and does: trunk's change to the loop's configuration
// is named.
//
// This passes before the story is built and has to go on passing.
func TestAC3WhereTheTrunkCommitTheStorySitsOnCannotBeWorkedOutTheGateStartsAtTheCutAsBefore(t *testing.T) {
	t.Parallel()
	main, wt, cut := otWalked(t, ldWalk{finished: true, bookkept: true})
	cgTrunkMoves(t, main)
	head := ldRebase(t, wt)
	ldReviewAgain(t, wt)
	setGitOnDisk(t, main, "trunk_branch", "a-trunk-nobody-has")

	gate := cgGateAgain(t, wt)
	want := "committed on story/US-001 in " + cut[:12] + ".." + head[:12]
	if !strings.Contains(gate.stdout, want) {
		t.Errorf("the commit gate did not print %q:\n%s", want, gate.stdout)
	}
	r := swRecord(t, main, "US-001")
	if r.CommitBase != cut || r.Commit != head || r.ChangeBase != cut {
		t.Errorf("the record names %s..%s and measures the change from %s, want all from the cut, %s, to %s",
			r.CommitBase, r.Commit, r.ChangeBase, cut, head)
	}
	if !cgWarns(gate, cgConfig) {
		t.Errorf("the commit gate does not warn of %s, which the range it fell back to holds a change to:\n%s",
			cgConfig, gate.stdout+gate.stderr)
	}
}

// ---------------------------------------------------------------- AC-10

// AC-10: trunk's commit changed the loop's configuration, or the backlog, and
// no commit of the story's did. The gate run again says nothing of the story
// changing them, on either way onto the moved trunk.
func TestAC10TheCommitGateDoesNotWarnOfTheLoopsFilesOnlyTrunksCommitsChanged(t *testing.T) {
	t.Parallel()
	t.Run("AC-10 after a rebase by hand", func(t *testing.T) {
		t.Parallel()
		s := cgByHand(t, cgHow{}, nil)
		cgNoWarning(t, s.gate, cgConfig)
	})
	t.Run("AC-10 after the stop's own replay failed on a command", func(t *testing.T) {
		t.Parallel()
		s := cgByTheStop(t, cgHow{})
		cgNoWarning(t, s.gate, cgConfig)
	})
	t.Run("AC-10 trunk changed the backlog", func(t *testing.T) {
		t.Parallel()
		main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
		writeFile(t, main, "CHANGES.md", "another story's work\n")
		writeFile(t, main, cgBacklog, wtRead(t, filepath.Join(main, cgBacklog))+"\n")
		swGit(t, main, "add", "--", cgBacklog, "CHANGES.md")
		cgCommit(t, main, "another story: its work, and the backlog as a person left it")
		ldRebase(t, wt)
		ldReviewAgain(t, wt)

		cgNoWarning(t, cgGateAgain(t, wt), cgBacklog)
	})
}

// AC-10 and AC-11 together: trunk's commit changed the loop's configuration
// and the story's own commit changed another file under the loop's directory.
// The warning names the story's file, and not the one that is trunk's.
func TestAC10AndAC11TheWarningNamesTheFileTheStoryChangedAndNotTheOneTrunkDid(t *testing.T) {
	t.Parallel()
	s := cgByHand(t, cgHow{}, func(wt string) {
		swCommitFile(t, wt, cgTeam, cgTeamBody, "the story writes under the loop's directory")
	})
	said := s.gate.stdout + s.gate.stderr
	if !cgWarns(s.gate, cgTeam) {
		t.Errorf("the commit gate does not warn that the story changes %s:\n%s", cgTeam, said)
	}
	if wtNames(said, cgConfig) {
		t.Errorf("the commit gate names %s, which only trunk's commit changed:\n%s", cgConfig, said)
	}
}

// ---------------------------------------------------------------- AC-11

// AC-11: a story whose own commits change one of the loop's files, or the
// backlog, is warned at the commit gate, and the file is named. That is so
// today, wherever the range starts, and every case here passes before the
// story is built: they are written first so that a start drawn too late --
// past a commit of the story's own -- fails them.
func TestAC11TheCommitGateStillWarnsOfALoopFileTheStorysOwnCommitsChangeAndNamesIt(t *testing.T) {
	t.Parallel()

	// started is a story at its implementation, in a repository whose trunk
	// holds .sdlc/team.md before the story is cut.
	started := func(t *testing.T) (main, wt string) {
		t.Helper()
		main = ldRepository(t, func(root string) { writeFile(t, root, cgTeam, cgTeamBody) })
		otIdentity(t, main)
		return main, ldStarted(t, main)
	}
	changed := strings.Replace(cgTeamBody, "// seven\n", "// seven, as the story has it\n", 1)
	warned := func(t *testing.T, gate result, path string) {
		t.Helper()
		if !cgWarns(gate, path) {
			t.Errorf("the commit gate does not warn that the story changes %s:\n%s", path, gate.stdout+gate.stderr)
		}
	}

	t.Run("AC-11 on the trunk it was cut from", func(t *testing.T) {
		t.Parallel()
		_, wt := started(t)
		ldWork(t, wt)
		writeFile(t, wt, cgTeam, changed)
		swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
		satisfy(t, wt, model.GateCommit)

		warned(t, wtMust(t, wt, ldSession, "", "gate", "commit", "pass", "--note", "the first pass"), cgTeam)
	})

	t.Run("AC-11 the very file trunk's commit also changed", func(t *testing.T) {
		t.Parallel()
		main, wt := started(t)
		ldWork(t, wt)
		writeFile(t, wt, cgTeam, changed)
		swPass(t, wt, ldSession, model.GateImplementation, "")
		ldBookkeeping(t, wt)
		on := swCommitFile(t, main, cgTeam, strings.Replace(cgTeamBody, "// one\n", "// one, as trunk has it\n", 1),
			"another story's line in the same file")
		ldRebase(t, wt)
		if got := cgCommits(t, wt, on, "HEAD"); len(got) != 2 {
			t.Fatalf("precondition: the story has %d commits on trunk's, want its work and its bookkeeper's", len(got))
		}
		ldReviewAgain(t, wt)

		warned(t, cgGateAgain(t, wt), cgTeam)
	})

	t.Run("AC-11 in the first of the story's several commits", func(t *testing.T) {
		t.Parallel()
		main, wt := started(t)
		// The story's first commit, the one the commit gate takes its work
		// in, holds the loop file; two later commits and the bookkeeper's
		// follow it, and none of those touches the file. A commit before the
		// commit gate is refused (SDLC-E0046), so the first is the earliest
		// a story's change can be.
		ldWork(t, wt)
		writeFile(t, wt, cgTeam, changed)
		swPass(t, wt, ldSession, model.GateImplementation, "")
		first := swRev(t, wt, "HEAD")
		swCommitFile(t, wt, "ledger.go", "package billing\n", "a later commit of the story's")
		swCommitFile(t, wt, "ledger.go", "package billing\n\n// Ledger\n", "and another")
		ldBookkeeping(t, wt)
		if got := swGit(t, wt, "diff", "--name-only", first, "HEAD", "--", cgTeam); got != "" {
			t.Fatalf("precondition: a commit after the story's first changes %s", cgTeam)
		}
		on := cgTrunkMoves(t, main)
		ldRebase(t, wt)
		if got := cgCommits(t, wt, on, "HEAD"); len(got) != 4 {
			t.Fatalf("precondition: the story has %d commits on trunk's, want four", len(got))
		}
		ldReviewAgain(t, wt)

		warned(t, cgGateAgain(t, wt), cgTeam)
	})

	t.Run("AC-11 after the stop's own replay, in the rework", func(t *testing.T) {
		t.Parallel()
		main, wt := ldWalked(t, ldWalk{finished: true, lx: true, set: map[string]string{"test": cgNeedsFixed}})
		otIdentity(t, main)
		ldBookkeeping(t, wt)
		cgTrunkMoves(t, main)
		swRefused(t, ldCheckFailed, wt, ldSession, "stop")
		wtMust(t, wt, ldSession, "", "gate", "implementation", "fail", "--note", "commands.test fails on trunk as it is now")
		writeFile(t, wt, "FIXED.md", "what trunk's change asked of the story\n")
		writeFile(t, wt, cgTeam, cgTeamBody)
		swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
		satisfy(t, wt, model.GateCommit)

		warned(t, cgGateAgain(t, wt), cgTeam)
	})

	t.Run("AC-11 the backlog", func(t *testing.T) {
		t.Parallel()
		s := cgByHand(t, cgHow{}, func(wt string) {
			swCommitFile(t, wt, cgBacklog, wtRead(t, filepath.Join(wt, cgBacklog))+"\n", "the story changes the backlog")
		})
		warned(t, s.gate, cgBacklog)
	})

	t.Run("AC-11 a path under the loop's directory spelled in another case", func(t *testing.T) {
		t.Parallel()
		// The path is given to git's index as it is spelled, since a disk
		// that folds case would hand back the directory's own spelling.
		const spelled = ".SDLC/Team.md"
		s := cgByHand(t, cgHow{}, func(wt string) {
			writeFile(t, wt, spelled, cgTeamBody)
			blob := swGit(t, wt, "hash-object", "-w", "--", spelled)
			swGit(t, wt, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+spelled)
			cgCommit(t, wt, "the story writes under the loop's directory, spelled its own way")
			if got := swGit(t, wt, "ls-files", "--", spelled); got != spelled {
				t.Skipf("git here keeps %q for %s, so a path differing only in case cannot be committed", got, spelled)
			}
		})
		warned(t, s.gate, spelled)
	})

	t.Run("AC-11 where the trunk commit the story sits on cannot be worked out", func(t *testing.T) {
		t.Parallel()
		main, wt := started(t)
		ldWork(t, wt)
		writeFile(t, wt, cgTeam, changed)
		swPass(t, wt, ldSession, model.GateImplementation, "")
		ldBookkeeping(t, wt)
		ldMoveTrunk(t, main)
		ldRebase(t, wt)
		ldReviewAgain(t, wt)
		setGitOnDisk(t, main, "trunk_branch", "a-trunk-nobody-has")

		warned(t, cgGateAgain(t, wt), cgTeam)
	})
}

// ---------------------------------------------------------------- the stop afterwards

// AC-3 and the person's first answer: what the gate now stores is what the
// landing reads, and a stop after the gate run again lands or refuses as it
// did. A story rebased by hand whose change is the one reviewed lands on the
// trunk commit it sits on, with no review asked for again, and the record
// then names what landed.
//
// The six tests from here hold what the landing does today, and pass before
// the story is built.
func TestAC3AStopAfterTheGateRunAgainLandsAStoryRebasedByHandWhoseChangeIsTheOneReviewed(t *testing.T) {
	t.Parallel()
	s := cgByHand(t, cgHow{}, nil)
	cgRetro(t, s.wt)
	reviews := len(swRecord(t, s.main, "US-001").Reviews)

	landed := otLands(t, s.main, s.wt, s.on)
	if landed.tip != s.head {
		t.Errorf("trunk is at %s after the landing, want the story's tip %s by fast-forward", landed.tip, s.head)
	}
	r := swRecord(t, s.main, "US-001")
	if got := len(r.Reviews); got != reviews {
		t.Errorf("the record has %d reviews after the landing, want the %d it had: none asked for again", got, reviews)
	}
	if r.CommitBase != s.on || r.Commit != landed.tip {
		t.Errorf("the record names %s..%s as what landed, want %s..%s", r.CommitBase, r.Commit, s.on, landed.tip)
	}
	if got, _ := otAt(t, s.main, landed.tip, cgConfig); !strings.Contains(got, `"max_stop_blocks": 4`) {
		t.Errorf("trunk's %s after the landing is not the one another story's commit left:\n%s", cgConfig, got)
	}
}

// AC-3 and the person's first answer: the landing runs the configured
// commands on the tree of a story that was put onto a trunk that moved, and
// goes on doing so after the commit gate is run again there. The gate runs no
// command itself, so without the stop nothing has run them on that tree: a
// command that fails there still refuses the landing (SDLC-E0055), and where
// they pass, they ran on the commit that landed.
func TestAC3AStopAfterTheGateRunAgainStillRunsTheConfiguredCommandsOnTheTreeThatLands(t *testing.T) {
	t.Parallel()
	ranOn := func(t *testing.T, file string) string {
		t.Helper()
		if !wtExists(file) {
			return "(the command never ran)"
		}
		return strings.TrimSpace(wtRead(t, file))
	}
	t.Run("AC-3 rebased by hand, and a command that fails on the tree", func(t *testing.T) {
		t.Parallel()
		s := cgByHand(t, cgHow{set: map[string]string{"test": "test ! -f CHANGES.md"}}, nil)
		cgRetro(t, s.wt)

		out := swRefused(t, ldCheckFailed, s.wt, ldSession, "stop")
		said, _ := ldRefusal(t, out, ldCheckFailed)
		suSays(t, said, "commands.test", "Nothing landed")
		if got := ldTrunk(t, s.main); got != s.on {
			t.Errorf("trunk moved to %s though commands.test fails on the tree", got)
		}
		ldStillHeld(t, s.main, s.wt)
	})
	t.Run("AC-3 rebased by hand, and commands that pass", func(t *testing.T) {
		t.Parallel()
		command, file := lxMarker(t)
		s := cgByHand(t, cgHow{set: map[string]string{"test": command}}, nil)
		cgRetro(t, s.wt)
		if got := ranOn(t, file); got == s.head {
			t.Fatalf("precondition: commands.test had already run on %s before the stop", got)
		}

		landed := otLands(t, s.main, s.wt, s.on)
		if got := ranOn(t, file); got != landed.tip {
			t.Errorf("commands.test last ran on %s, want the tree that landed, %s", got, landed.tip)
		}
	})
	t.Run("AC-3 put onto trunk by the stop's own replay and reworked", func(t *testing.T) {
		t.Parallel()
		command, file := lxMarker(t)
		s := cgByTheStop(t, cgHow{set: map[string]string{"build": command}})
		cgRetro(t, s.wt)
		if got := ranOn(t, file); got == s.head {
			t.Fatalf("precondition: commands.build had already run on %s before the stop", got)
		}

		landed := otLands(t, s.main, s.wt, s.on)
		if got := ranOn(t, file); got != landed.tip {
			t.Errorf("commands.build last ran on %s, want the tree that landed, %s", got, landed.tip)
		}
	})
}

// AC-3 and the person's first answer: a change committed after the gate run
// again is not the one reviewed. The stop is refused with SDLC-E0029 naming
// the reviews to run again and trunk stays where it was; recorded again on
// the change as it is, they let it land.
func TestAC3AStopAfterTheGateRunAgainRefusesAChangeThatIsNotTheOneReviewed(t *testing.T) {
	t.Parallel()
	s := cgByHand(t, cgHow{}, nil)
	cgRetro(t, s.wt)
	lxChangeAfterTheReviews(t, s.wt)

	for _, stop := range []string{"the stop", "a second stop"} {
		out := swRefused(t, ldStale, s.wt, ldSession, "stop")
		said, fix := ldRefusal(t, out, ldStale)
		suSays(t, said+fix, "verifier", "code-reviewer")
		if got := ldTrunk(t, s.main); got != s.on {
			t.Fatalf("%s moved trunk to %s with a change nobody reviewed", stop, got)
		}
	}
	ldStillHeld(t, s.main, s.wt)

	ldReviewAgain(t, s.wt)
	landed := otLands(t, s.main, s.wt, s.on)
	if got, _ := otAt(t, s.main, landed.tip, "refund.go"); !strings.Contains(got, "Amount int") {
		t.Errorf("trunk has refund.go %q, want the change that was reviewed again", got)
	}
}

// AC-3 and the person's first answer, with the non-goal on SDLC-E0037: a
// finished story in a tier that pauses, rebased by hand, is not committed
// again or landed on the approval it had. The commit gate run again is
// refused with SDLC-E0037, so it stores nothing new, and so is the stop.
func TestAC3AFinishedStoryInATierThatPausesRebasedByHandIsRefusedAtTheGateRunAgainAndAtTheStop(t *testing.T) {
	t.Parallel()
	main, wt, cut := otWalked(t, ldWalk{tier: "high"})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, "")
	ldBookkeeping(t, wt)
	before := swRecord(t, main, "US-001")
	on := cgTrunkMoves(t, main)
	ldRebase(t, wt)
	ldReviewAgain(t, wt)

	swRefused(t, ldNotApproved, wt, ldSession, "gate", "commit", "pass", "--note", "on the approval it had")
	if r := swRecord(t, main, "US-001"); r.CommitBase != before.CommitBase || r.Commit != before.Commit ||
		r.ChangeBase != before.ChangeBase || r.Change != before.Change || r.ChangeBase != cut {
		t.Errorf("the refused gate changed what the record measures the story from: %s..%s, change from %s",
			r.CommitBase, r.Commit, r.ChangeBase)
	}
	out := swRefused(t, ldNotApproved, wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	suSays(t, fix, `"sdlc approve US-001"`, worktreeIn("US-001"))
	if got := ldTrunk(t, main); got != on {
		t.Fatalf("trunk moved to %s on an approval of the work before somebody rebased it", got)
	}
	ldStillHeld(t, main, wt)
}

// AC-3 and the person's first answer, with the non-goal on SDLC-E0037: a
// story in a tier that pauses, rebased by hand while its retro is still to
// come, is refused at the commit gate run again with SDLC-E0037 until a
// person approves the work as it now is. Approved, the gate passes, and the
// stop lands the story on that approval: nobody is asked a second time.
func TestAC3AStoryInATierThatPausesLandsOnTheApprovalGivenBeforeTheGateRunAgain(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{tier: "high"})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, model.GateRetro)
	on := cgTrunkMoves(t, main)
	head := ldRebase(t, wt)
	ldReviewAgain(t, wt)

	swRefused(t, ldNotApproved, wt, ldSession, "gate", "commit", "pass", "--note", "on the approval it had")
	ldApproved(t, wt)
	approvals := len(swRecord(t, main, "US-001").Approvals)
	cgGateAgain(t, wt)
	if r := swRecord(t, main, "US-001"); r.Commit != head {
		t.Errorf("the gate run again after the approval records a range that ends at %s, want the story's tip %s", r.Commit, head)
	}
	cgRetro(t, wt)

	landed := otLands(t, main, wt, on)
	if landed.tip != head {
		t.Errorf("trunk is at %s after the landing, want the story's tip %s by fast-forward", landed.tip, head)
	}
	r := swRecord(t, main, "US-001")
	if got := len(r.Approvals); got != approvals {
		t.Errorf("the record has %d approvals after the landing, want the %d it had: none asked for again", got, approvals)
	}
	if pending, ok := r.PendingEscalation(); ok {
		t.Errorf("the landing left a question to a person open: %+v", pending)
	}
}

// AC-3 and the person's first answer: a story the stop itself put onto the
// moved trunk, reworked after SDLC-E0055 and taken through its gates again,
// lands at the next stop on the trunk commit it sits on, with trunk's change
// to the loop's configuration and the story's rework both on trunk.
func TestAC3AStopAfterTheGateRunAgainLandsAStoryTheStopsOwnReplayPutOntoTrunk(t *testing.T) {
	t.Parallel()
	s := cgByTheStop(t, cgHow{})
	cgRetro(t, s.wt)

	landed := otLands(t, s.main, s.wt, s.on)
	if landed.tip != s.head {
		t.Errorf("trunk is at %s after the landing, want the story's tip %s by fast-forward", landed.tip, s.head)
	}
	if _, ok := otAt(t, s.main, landed.tip, "FIXED.md"); !ok {
		t.Error("trunk does not hold the story's rework after the landing")
	}
	r := swRecord(t, s.main, "US-001")
	if r.CommitBase != s.on || r.Commit != landed.tip {
		t.Errorf("the record names %s..%s as what landed, want %s..%s", r.CommitBase, r.Commit, s.on, landed.tip)
	}
}
