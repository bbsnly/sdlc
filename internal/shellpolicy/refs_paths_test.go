package shellpolicy

// US-029.3, coverage: the branch rule's own corners that the acceptance table
// (refs_test.go) and the rebase table (rebase_test.go) do not reach --
// `firstWord`'s fallback when nothing names a subcommand, the trunk reader the
// hook left unset or empty, symbolic-ref's other ways to write a ref, a reset
// placed in another checkout by an unliteral path or by the environment, and
// the work bound (perf 1) actually being crossed within one Inspect call
// rather than merely approached.
//
// Helpers carry an lq prefix.

import (
	"strings"
	"testing"
)

// lqFiller is cheap, harmless segments, enough of them to push the branch
// hold's work counter past maxHeldWork within a single Inspect call once a
// branch move is found and held.
func lqFiller(n int) string {
	return strings.Repeat("git -c a.b=c -c d.e=f status --short; ", n)
}

// `git remote` only refuses where its first non-option word is "update",
// which is the one form that writes refs (T2); every other remote command is
// a read the branch rule leaves alone. This also holds firstWord to reading
// past an option and to answering "" where nothing names a word.
func TestFirstWordNamesTheWordThatIsNotAnOptionAndFallsBackToEmpty(t *testing.T) {
	s := rfFinished()
	wtRefusedAs(t, "git remote update", s, branchRule)
	allowed(t, "git remote -v show origin", s)
	allowed(t, "git remote --verbose", s)
}

// Perf 1: past the work bound, a held finding answers without reading
// further. rebase_test.go's long-command test stays under the bound and only
// proves the rule still wins; this one is sized to cross maxHeldWork
// (1<<20) within a single Inspect call, so the immediate "already held, and
// past the bound" answer is what is exercised, not just the ordinary one.
func TestPastTheWorkBoundTheHeldFindingAnswersWithoutReadingFurther(t *testing.T) {
	inParallel(t)
	s := rbState(t, true, true, "main", rbTrunkIs("main"))
	// rebase_test.go's TestAC9ALongCommandAroundABranchMoveIsStillRefusedForIt
	// stays well under maxHeldWork (1<<20); this filler is sized to cross it
	// within the one Inspect call, so the bound is actually exercised.
	command := "git branch -f main HEAD; " + lqFiller(300000)
	f, ok := Inspect(command, s)
	if !ok {
		t.Fatalf("a %d-byte command moving main was allowed", len(command))
	}
	if f.Rule != branchRule {
		t.Errorf("a %d-byte command moving main was refused by %s, want %s", len(command), f.Rule, branchRule)
	}
}

// rebaseAllowed's own place is only a -C into its own checkout: any other
// global option, even one that changes nothing about branches, is not the one
// rebase a held session may run.
func TestRebaseAllowedRefusesAGlobalOptionEvenOneThatMovesNothing(t *testing.T) {
	s := rbState(t, true, true, "main", rbTrunkIs("main"))
	wtRefusedAs(t, "git --no-pager rebase main", s, branchRule)
}

// Gaming row "IsTrunk as a string compare": isTrunk answers false, never
// panics or matches by accident, where the hook has not wired Trunk at all,
// and where it answers empty -- both states a hook bug could produce.
func TestIsTrunkIsFalseWhenTheHookNamesNoTrunkOrAnEmptyOne(t *testing.T) {
	t.Run("Trunk left unset", func(t *testing.T) {
		s := heldInA1(State{CommitReady: true, StoryFinished: true})
		rbSet(t, &s, "Committed", true)
		wtRefusedAs(t, "git rebase main", s, branchRule)
	})
	t.Run("Trunk answers empty", func(t *testing.T) {
		s := rbState(t, true, true, "", func(string) bool { return true })
		wtRefusedAs(t, "git rebase main", s, branchRule)
	})
}

// `git symbolic-ref` writes HEAD by deleting it, by a short flag bundling `d`
// or `m`, or by naming it and its target after `--`; every form is a write,
// not the read the branch rule leaves alone.
func TestSymbolicRefWritesCoversDeleteShortFlagsAndNamesAfterADoubleDash(t *testing.T) {
	s := rfFinished()
	for _, command := range []string{
		"git symbolic-ref --delete HEAD",
		"git symbolic-ref -d HEAD",
		"git symbolic-ref -m 'why' HEAD refs/heads/main",
		"git symbolic-ref -- HEAD refs/heads/main",
	} {
		wtRefusedAs(t, command, s, branchRule)
	}
}

// A `git reset` naming a commit is another checkout's concern -- and so this
// rule's -- however that checkout is named: by a path the shell has not
// expanded to a literal, which is read as possibly another checkout, or by
// GIT_DIR in the environment pointing at one this repository already has.
func TestResetInAnotherCheckoutIsCaughtByAnUnliteralPathAndByTheEnvironment(t *testing.T) {
	s := rfFinished()
	wtRefusedAs(t, "git -C $(pwd)/elsewhere reset HEAD~1", s, branchRule)
	wtRefusedAs(t, "GIT_DIR=/work/main/.git git reset HEAD~1", s, branchRule)
}
