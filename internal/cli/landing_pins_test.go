package cli

// US-029.3 pins what US-029.2's last rounds added to what a story worktree
// says, and left without a frozen test, where this story's landing and
// clean-up work beside it: SDLC-E0012 and SDLC-E0032 as a story worktree has
// them, and what doctor and SDLC-E0053 offer for a worktree whose HEAD holds
// commits on no branch. They pass today and hold the shape, not the wording:
// the look and keep commands, no removal where commits would be lost, never a
// force, and the worktree texts told apart from the single checkout's.
//
// Helpers carry an lp prefix, for landing pins.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

func lpNever(t *testing.T, text string, never ...string) {
	t.Helper()
	for _, n := range never {
		if strings.Contains(text, n) {
			t.Errorf("%q is in:\n%s", n, text)
		}
	}
}

// SDLC-E0012 in a story worktree: the other story is started by a person in
// the main checkout, which gives it a worktree of its own, rather than by
// stopping this one.
func TestSDLCE0012InAStoryWorktreeSendsAPersonToTheMainCheckoutForTheOtherStory(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))

	out := swRefused(t, "SDLC-E0012", wt, swSessionA, "start", "US-002")
	suSays(t, out, "worktree of", `"sdlc start US-002"`, "in the main checkout")
	lpNever(t, out, "the loop works one story at a time", "sdlc stop")
}

// SDLC-E0032 in a story worktree: the gate records the story committed on its
// branch, not on trunk.
func TestSDLCE0032InAStoryWorktreeSaysTheStoryIsCommittedOnItsBranch(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"})
	wt := swCheckStarted(t, main, "US-001", swSessionA, swStarted(t, main, swSessionA))
	swReach(t, wt, swSessionA, model.GateCommit)

	out := swRefused(t, "SDLC-E0032", wt, swSessionA, "gate", "commit", "pass", "--note", "not yet committed")
	suSays(t, out, "story/US-001")
	lpNever(t, out, "reached trunk")
}

// Doctor, for a story worktree nobody holds whose HEAD holds a commit on no
// branch: a command to look at it and one to keep it on a branch, what is lost
// otherwise, and no removal.
func TestDoctorOffersNoRemovalOfAStoryWorktreeWhoseHeadHoldsACommitOnNoBranch(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := swWorktreeOf(main, "US-002")
	swGit(t, main, "worktree", "add", "--quiet", "--detach", wt, "main")
	swCommitFile(t, wt, "work.go", "package work\n", "work on no branch")

	c, ok := suChecked(t, main, "story worktrees")
	if !ok || c.State != stateProblem {
		t.Fatalf("doctor has no story worktrees problem: %+v", c)
	}
	suSays(t, c.Fix, "git -C .sdlc/worktrees/US-002 log", "git -C .sdlc/worktrees/US-002 branch", "lost")
	lpNever(t, c.Fix, "worktree remove .sdlc/worktrees/US-002")
	suNeverForces(t, c.Fix)
}

// Doctor, for a story worktree on a branch a session named: the name is data,
// kept out of every command a person pastes, and nothing is removed.
func TestDoctorKeepsABranchNameOutOfTheCommandsItGives(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"}, wtStory{id: "US-002"})
	wt := swWorktreeOf(main, "US-002")
	swGit(t, main, "worktree", "add", "--quiet", "-b", "odd;touch-x", wt, "main")
	swCommitFile(t, wt, "work.go", "package work\n", "work on another branch")

	c, ok := suChecked(t, main, "story worktrees")
	if !ok || c.State != stateProblem {
		t.Fatalf("doctor has no story worktrees problem: %+v", c)
	}
	lpNever(t, c.Fix, "odd;touch-x", "worktree remove .sdlc/worktrees/US-002")
	suNeverForces(t, c.Fix)
}

// SDLC-E0053 at start, for a leftover worktree in the story's place whose HEAD
// holds a commit on no branch: look, keep, what is lost, and no removal.
func TestSDLCE0053OffersNoRemovalOfALeftoverWorktreeHoldingACommitOnNoBranch(t *testing.T) {
	main := swRepository(t, wtStory{id: "US-001"})
	wt := swWorktreeOf(main, "US-001")
	swGit(t, main, "worktree", "add", "--quiet", "--detach", wt, "main")
	swCommitFile(t, wt, "work.go", "package work\n", "work on no branch")

	out := swRefused(t, swWorktreeNotMade, main, swSessionA, "start")
	suSays(t, out, "git -C .sdlc/worktrees/US-001 log", "git -C .sdlc/worktrees/US-001 branch", "lost")
	lpNever(t, out, "worktree remove .sdlc/worktrees/US-001")
	suNeverForces(t, out)
	if !wtExists(filepath.Join(wt, "work.go")) {
		t.Error("the refused start took the leftover worktree's work away")
	}
}
