package cli

// US-029.3, coverage: the landing's own corners that landing_test.go and
// landing_states_test.go do not reach -- error branches in the small helpers
// (mainSettingsOf, backlogIn, trunkName, waitingToLand, changeBase, quoteSome,
// keptLine, landedLines, refused, passes, setApart, freezeFix), a story
// committed before this release whose record has no change_base or change,
// trunk's own committed configuration being unreadable both at landing and at
// the commit gate that measures the change, the loop's state reaching trunk
// under .sdlc/state specifically, a change the reviewers have not seen with
// an uncommitted worktree on top, and a command that moves trunk out from
// under a landing that is re-checking everything once the commands have run.
//
// Everything that drives a real repository shares landing_test.go's ld
// helpers. Helpers of this file's own carry an lq prefix.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// lqForgetChange clears change_base and change from a story's record on disk,
// as a record written before this release has neither (PLAN.md: "For a story
// committed before this release, B is the merge base of T and S, and C is
// empty").
func lqForgetChange(t *testing.T, main string) {
	t.Helper()
	var rec map[string]any
	path := wtRecordPath(main, "US-001")
	wtReadJSON(t, path, &rec)
	delete(rec, "change_base")
	delete(rec, "change")
	wtWriteJSON(t, path, rec)
}

// ---------------------------------------------------------------- small helpers

func TestBacklogInDefaultsToTheConfiguredDefaultWhenNoPathIsSet(t *testing.T) {
	t.Parallel()
	got := backlogIn(config.Config{})
	want := filepath.ToSlash(filepath.Clean(config.Default().Backlog.Path))
	if got != want {
		t.Errorf("backlogIn(zero value) = %q, want the configured default %q", got, want)
	}
	if got == "" {
		t.Fatal("the default backlog path must not be empty")
	}
}

func TestWaitingToLandOffersTheReleasedReworkFixOnlyWithNoIterationRunning(t *testing.T) {
	t.Parallel()
	const reworkPhrase = `sdlc gate code_review fail --story US-1`
	running := waitingToLand("US-1", "main", true)
	if strings.Contains(running.Fix, reworkPhrase) {
		t.Errorf("with an iteration running, the fix should not offer to reopen the story: %s", running.Fix)
	}
	held := waitingToLand("US-1", "main", false)
	if !strings.Contains(held.Fix, reworkPhrase) {
		t.Errorf("with no iteration running, the fix should also say how to rework the story: %s", held.Fix)
	}
	if !strings.Contains(held.Fix, `sdlc stop" in`) {
		t.Errorf("the fix should still say how to land it: %s", held.Fix)
	}
}

func TestChangeBaseReturnsTheAncestryErrorWhenTheCutCannotBeChecked(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // not a git repository at all
	hash := strings.Repeat("a", 40)
	if _, err := changeBase(t.Context(), dir, hash, hash, hash); err == nil {
		t.Fatal("changeBase with an unreadable repository and a cut to check = nil error, want one")
	}
}

func TestChangeBaseReturnsTheMergeBaseErrorWhenItCannotBeRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // not a git repository at all
	hash := strings.Repeat("a", 40)
	if _, err := changeBase(t.Context(), dir, "", hash, hash); err == nil {
		t.Fatal("changeBase with no cut and an unreadable repository = nil error, want one")
	}
}

func TestChangeBaseRefusesWhenHeadSharesNoHistoryWithTrunk(t *testing.T) {
	t.Parallel()
	dir := wtTempDir(t)
	wtGit(t, dir, "init", "--quiet", "--initial-branch=main")
	wtCommit(t, dir, "main root")
	trunkTip := wtGit(t, dir, "rev-parse", "HEAD")
	wtGit(t, dir, "switch", "--quiet", "--orphan", "feature")
	wtCommit(t, dir, "feature root")
	head := wtGit(t, dir, "rev-parse", "HEAD")

	_, err := changeBase(t.Context(), dir, "", head, trunkTip)
	var se *sdlcerr.Error
	if !errors.As(err, &se) || se.Code != sdlcerr.RepositoryUnreadable {
		t.Fatalf("changeBase across unrelated histories = %v, want a RepositoryUnreadable error", err)
	}
	if !strings.Contains(se.Why, "shares no history with trunk") {
		t.Errorf("the why = %q, want it to say HEAD shares no history with trunk", se.Why)
	}
}

func TestReopenFixNamesTheBranchTrunkAndTheReleasedWayBackIn(t *testing.T) {
	t.Parallel()
	l := &lander{id: "US-100", m: mainSettings{trunk: "main"}}
	got := l.reopenFix()
	for _, want := range []string{
		"story/US-100", "main",
		`sdlc gate code_review fail --story US-100 --note`,
		"/sdlc:next",
		assistantEscalates,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("reopenFix() = %q, want it to mention %q", got, want)
		}
	}
}

func TestQuoteSomeNamesTheFirstFiveAndCountsTheRest(t *testing.T) {
	t.Parallel()
	names := []string{"a", "b", "c", "d", "e", "f", "g"}
	got := quoteSome(names)
	want := quoteAll(names[:5]) + ", and 2 more"
	if got != want {
		t.Errorf("quoteSome(7 names) = %q, want %q", got, want)
	}
}

func TestKeptLineUsesTheErrorsOwnTextWhenItIsNotAnSdlcError(t *testing.T) {
	t.Parallel()
	got := keptLine("US-1", nil, errors.New("git said something odd"))
	if !strings.Contains(got, "git said something odd.") {
		t.Errorf("keptLine with a plain error = %q, want the error's own text", got)
	}
	if !strings.Contains(got, "sdlc doctor") {
		t.Errorf("keptLine should still point at doctor: %q", got)
	}
}

func TestLandedLinesNamesEveryCheckoutWhenSeveralHaveTrunk(t *testing.T) {
	t.Parallel()
	st := landState{
		tip:      strings.Repeat("a", 40),
		trunkTip: strings.Repeat("b", 40),
		from:     strings.Repeat("b", 40),
		base:     strings.Repeat("b", 40),
	}
	got := strings.Join(landedLines("US-1", "main", st, []string{"/work/main", "/work/other"}, true), "\n")
	if !strings.Contains(got, "which have main checked out, moved with them.") {
		t.Errorf("landedLines with two checkouts = %q, want the plural wording", got)
	}
}

func TestRefusedPassesAPlainErrorThroughUnchanged(t *testing.T) {
	t.Parallel()
	l := &lander{id: "US-1", m: mainSettings{trunk: "main"}}
	plain := errors.New("boom")
	if got := l.refused(plain); got != plain { //nolint:errorlint // exact identity is the point
		t.Errorf("refused(plain error) = %v, want it unchanged", got)
	}
}

func TestPassesFailsWithAnExitCodeBelowZero(t *testing.T) {
	t.Parallel()
	ok, how := passes("build", -1, "", 0)
	if ok {
		t.Fatal("passes(exit -1) = true, want false")
	}
	if how != "it was stopped before it finished" {
		t.Errorf("passes(exit -1) how = %q", how)
	}
}

func TestSetApartShowsANonPrintingByteAsAQuestionMark(t *testing.T) {
	t.Parallel()
	got := setApart("bad\x00line")
	if !strings.Contains(got, "bad?line") {
		t.Errorf("setApart with a NUL byte = %q, want the byte shown as ?", got)
	}
}

func TestFreezeFixLeavesAPlainErrorAndAnUnrelatedCodeUnchanged(t *testing.T) {
	t.Parallel()
	l := &lander{id: "US-1", m: mainSettings{trunk: "main"}}
	t.Run("not an sdlcerr.Error", func(t *testing.T) {
		plain := errors.New("boom")
		if got := l.freezeFix(plain); got != plain { //nolint:errorlint // exact identity is the point
			t.Errorf("freezeFix(plain) = %v, want it unchanged", got)
		}
	})
	t.Run("a code with no landing fix of its own", func(t *testing.T) {
		other := sdlcerr.New(sdlcerr.RepositoryUnreadable, "unrelated", "unrelated")
		if got := l.freezeFix(other); got != error(other) { //nolint:errorlint // exact identity is the point
			t.Errorf("freezeFix(unrelated code) = %v, want it unchanged", got)
		}
	})
}

// ---------------------------------------------------------------- real landings

// L2: the landing reads what passing means from trunk's own committed
// configuration, not the main checkout's disk copy, and refuses where that
// commit cannot be parsed.
func TestReadTrunkRefusesWhenTrunksOwnConfigurationCannotBeParsed(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	orig := wtRead(t, filepath.Join(main, config.File))
	swCommitFile(t, main, config.File, "{not json", "break trunk's configuration")
	// Restore a valid disk copy: mainSettingsOf reads the main checkout's own
	// configuration from disk and must still succeed, so that what fails is
	// specifically trunk's own committed configuration, which readTrunk reads
	// through git.
	writeFile(t, main, config.File, orig)

	out := wtRefused(t, "SDLC-E0045", wt, ldSession, "", "stop")
	said, fix := ldRefusal(t, out, "SDLC-E0045")
	if !strings.Contains(said, "main's "+config.File) || !strings.Contains(said, "could not be read") {
		t.Errorf("the refusal = %q, want it to name main's broken configuration", said)
	}
	if !strings.Contains(fix, "commits a "+config.File+" on main") {
		t.Errorf("the fix = %q, want a person to commit a readable configuration on trunk", fix)
	}
	ldStillHeld(t, main, wt)
}

// A story committed before this release recorded no change_base and no
// change. Where trunk has not moved, the base the landing measures from is
// still trunk's merge base with the tip, and the landing goes ahead exactly
// as for a story that did record them.
func TestALegacyRecordMeasuresItsBaseAsTrunksMergeBaseWithTheTip(t *testing.T) {
	t.Parallel()
	main, wt, cut := ldWalkedFrom(t, ldWalk{finished: true})
	tip := ldBookkeeping(t, wt)
	lqForgetChange(t, main)
	// With no change recorded, the landing cannot tell the tree apart from one
	// nobody reviewed, and asks for the reviews again on it as it stands.
	ldReviewAgain(t, wt)

	ldLanded(t, main, wt, tip)
	if got := swRecord(t, main, "US-001").ChangeBase; got != cut {
		t.Errorf("a legacy record's landed change_base = %q, want trunk's merge base with the tip, %s", got, cut)
	}
}

// Where that same legacy record's branch shares no history at all with
// trunk, the landing refuses with the same SDLC-E0054 a moved trunk gets,
// naming the reason a person -- not the session -- has to decide on.
func TestALegacyRecordWhoseBranchSharesNoHistoryWithTrunkIsRefused(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	lqForgetChange(t, main)

	// Rewrite the story's branch onto an unrelated history, as if it had once
	// been cut from a trunk with none of this repository's commits. The
	// worktree still needs its own .sdlc/config.json and .gitignore to work at
	// all, so those two come along; nothing else does.
	cfg := wtRead(t, filepath.Join(wt, config.File))
	gitignore := wtRead(t, filepath.Join(wt, ".gitignore"))
	swGit(t, wt, "switch", "--quiet", "--orphan", "lq-orphan")
	writeFile(t, wt, config.File, cfg)
	writeFile(t, wt, ".gitignore", gitignore)
	writeFile(t, wt, "orphan.txt", "elsewhere\n")
	swGit(t, wt, "add", "-A")
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "an unrelated history")
	swGit(t, wt, "branch", "-M", "story/US-001", "lq-old-story-US-001")
	swGit(t, wt, "branch", "-M", "lq-orphan", "story/US-001")

	out := wtRefused(t, ldBehindTrunk, wt, ldSession, "", "stop")
	said, fix := ldRefusal(t, out, ldBehindTrunk)
	if !strings.Contains(said, "shares no history with main") {
		t.Errorf("the refusal = %q, want it to say the branch shares no history with trunk", said)
	}
	if !strings.Contains(fix, "a person decides") {
		t.Errorf("the fix = %q, want a person to decide how the commits reach trunk", fix)
	}
	ldStillHeld(t, main, wt)
}

// L5: the loop's state reaching trunk gets an extra sentence in the fix,
// naming that trunk tracks it and has to stop.
func TestRangeChecksNamesStoppingTrackingWhereTheLoopsStateReachesTrunk(t *testing.T) {
	t.Parallel()
	statePath := filepath.Join(config.Dir, "state", "watch.txt")
	main := ldRepository(t, func(root string) { writeFile(t, root, statePath, "watched\n") })
	// The project's own .gitignore keeps .sdlc/state/ out; force-track this one
	// file, as a misconfigured project's trunk might already have it.
	swGit(t, main, "add", "-f", "--", statePath)
	swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit", "--quiet", "-m", "track state by mistake")

	wt := ldStarted(t, main)
	ldWork(t, wt)
	writeFile(t, wt, statePath, "changed by the story\n")
	swPass(t, wt, ldSession, model.GateImplementation, "")
	ldBookkeeping(t, wt)

	out := wtRefused(t, ldCheckFailed, wt, ldSession, "", "stop")
	said, fix := ldRefusal(t, out, ldCheckFailed)
	if !strings.Contains(said, filepath.ToSlash(statePath)) {
		t.Errorf("the refusal = %q, want it to name the tracked state file", said)
	}
	if !strings.Contains(fix, "which main tracks, they stop tracking it on main and ignore it first") {
		t.Errorf("the fix = %q, want it to say main tracks the loop's state and should stop", fix)
	}
	ldStillHeld(t, main, wt)
}

// D1, widened to trunk itself: once the configured commands have run,
// everything is asked again of the record and the repository as they now
// are. A command that moves trunk -- unmoved() only ever watches the story's
// own branch and its tracked files -- is caught here, not left for the
// fast-forward to find out about the hard way.
func TestALandingCommandThatMovesTrunkIsCaughtWhenEverythingIsAskedAgain(t *testing.T) {
	t.Parallel()
	main := lxRepository(t, map[string]string{"build": "git update-ref refs/heads/main HEAD && exit 0"}, nil)
	wt := ldFinished(t, main)
	ldBookkeeping(t, wt)
	lqForgetChange(t, main)
	ldReviewAgain(t, wt)

	// Which code the refusal carries, and its words, are the landing's to
	// choose (US-088 leaves open whether a trunk that moves under the commands
	// is tried again): what is held is that a command moving trunk is not a
	// landing.
	out := wtRefused(t, ldAny, wt, ldSession, "", "stop")
	if !strings.Contains(out, "  fix  ") {
		t.Errorf("the refusal must still say how to go on:\n%s", out)
	}
	suSays(t, out, "Nothing landed")
	lxNotDone(t, main)
	ldStillHeld(t, main, wt)
}

// A worktree story with gates still left is the released stop: it ends the
// iteration and does not try to land anything, whatever the landing's own L0
// checks would otherwise make of the worktree.
func TestStopEndsTheIterationAsReleasedWhileAWorktreeStoryStillHasGatesLeft(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{})

	out := wtMust(t, wt, ldSession, "", "stop").stdout
	if strings.Contains(out, "by fast-forward") || strings.Contains(out, "recorded it as landed") {
		t.Errorf("stop with gates still left tried to land the story: %s", out)
	}
	if got := wtStatuses(t, main)["US-001"]; got == string(model.StatusDone) {
		t.Error("a story with gates left must not be done after an ordinary stop")
	}
}

// land reads the main checkout's own configuration before anything else, and
// a broken one is refused there, with nothing landed, same as any later
// refusal.
func TestLandRefusesWhenTheMainCheckoutsOwnConfigurationCannotBeRead(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	writeFile(t, main, config.File, "{not json")

	out := wtRun(t, wt, ldSession, "", "stop")
	if out.code == 0 {
		t.Fatalf("stop with the main checkout's configuration broken went ahead:\n%s", out.stdout+out.stderr)
	}
	said := out.stdout + out.stderr
	if !strings.Contains(said, config.File) || !strings.Contains(said, "not valid JSON") {
		t.Errorf("the refusal = %q, want it to name the broken configuration", said)
	}
	if !strings.Contains(said, "Nothing landed") {
		t.Errorf("the refusal = %q, want it to end saying nothing landed", said)
	}
	ldStillHeld(t, main, wt)
}

// The commit gate measures the story's change through recordChange, which
// also reads the main checkout's own configuration; a broken one refuses the
// commit gate itself -- not the implementation gate before it -- before any
// change is recorded.
func TestRecordChangeRefusesWhenTheMainCheckoutsOwnConfigurationCannotBeRead(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	satisfy(t, wt, model.GateCommit)
	writeFile(t, main, config.File, "{not json")

	out := runWith(t, "", "gate", string(model.GateCommit), "pass", "--note", "x")
	if out.code == 0 {
		t.Fatalf("gate commit pass with the main checkout's configuration broken went ahead:\n%s", out.stdout+out.stderr)
	}
	said := out.stdout + out.stderr
	if !strings.Contains(said, config.File) || !strings.Contains(said, "not valid JSON") {
		t.Errorf("the refusal = %q, want it to name the broken configuration", said)
	}
}

// L7: a change the reviewers have not seen, with the worktree not clean
// either, is refused for the dirt, and the fix offers to stash it rather than
// commit straight over what the reviewers saw.
func TestChangeChecksRefusesADifferentChangeWhoseWorktreeHasUncommittedChanges(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	lqForgetChange(t, main)
	writeFile(t, wt, "billing.go", ldBilling+"\n// left uncommitted\n")

	out := wtRefused(t, ldStale, wt, ldSession, "", "stop")
	said, fix := ldRefusal(t, out, ldStale)
	if !strings.Contains(said, "has changes nobody committed") {
		t.Errorf("the refusal = %q, want it to name the uncommitted changes", said)
	}
	if !strings.Contains(fix, "git stash push") {
		t.Errorf("the fix = %q, want it to offer to stash them rather than commit over what was reviewed", fix)
	}
	ldStillHeld(t, main, wt)
}
