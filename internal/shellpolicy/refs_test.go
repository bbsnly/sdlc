package shellpolicy

// US-029.3 AC-9 in the classifier: a held session's command that moves a
// branch -- trunk, its own story's or another story's -- other than by its own
// gated commit is refused, with a rule, a reason and what to do instead.
//
// These run Inspect with the State the hook builds for A-1's worktree
// (heldInA1) and no field the story may add: the rows name trunk and the story
// branches, and the classifier has to refuse them by what the command does to
// a branch, not by knowing which branch is trunk. Where it cannot tell, it
// refuses. internal/hook/landing_test.go holds the same classes through the
// hook with a real repository, where the rebase onto trunk that OQ-3 leaves
// the session is also held.
//
// Helpers carry an rf prefix, for refs.

import "testing"

// rfFinished is A-1's session with every gate passed: after the commit, before
// the landing.
func rfFinished() State {
	return heldInA1(State{CommitReady: true, StoryFinished: true})
}

// The evasions, by class, as THREATS T1 and T2 name them.
var rfMoves = []struct{ class, command string }{
	{"a ref written directly", "git update-ref refs/heads/main HEAD"},
	{"a ref written directly", "git update-ref refs/heads/story/A-1 main"},
	{"a ref written directly", "git update-ref -d refs/heads/story/B-2"},
	{"a ref written directly", "git update-ref --no-deref HEAD main"},
	{"a ref written directly", "echo 'update refs/heads/main HEAD' | git update-ref --stdin"},
	{"a ref written directly", "git -C /work/main update-ref refs/heads/main HEAD"},
	{"a ref written directly", "git --git-dir=/work/main/.git update-ref refs/heads/main HEAD"},
	{"a ref written directly", "GIT_DIR=/work/main/.git git update-ref refs/heads/main HEAD"},
	{"a ref written directly", "cd /work/main && git update-ref refs/heads/main story/A-1"},
	{"a ref written directly", "sh -c 'git update-ref refs/heads/main HEAD'"},
	{"a ref written directly", "env git update-ref refs/heads/main HEAD"},
	{"a ref written directly", "git -c alias.up=update-ref up refs/heads/main HEAD"},
	{"a ref written directly", "git -c 'alias.up=!git update-ref refs/heads/main HEAD' up"},
	{"a branch forced or deleted", "git branch -f main HEAD"},
	{"a branch forced or deleted", "git branch --force story/B-2 HEAD"},
	{"a branch forced or deleted", "git branch -D story/B-2"},
	{"a branch forced or deleted", "git branch -d story/B-2"},
	{"a branch forced or deleted", "git branch --delete story/B-2"},
	{"a branch renamed or copied over", "git branch -M story/A-1 main"},
	{"a branch renamed or copied over", "git branch -m story/B-2 gone"},
	{"a branch renamed or copied over", "git branch -C story/A-1 main"},
	{"a checkout that resets a branch", "git checkout -B main"},
	{"a checkout that resets a branch", "git switch -C story/B-2"},
	{"a checkout that resets a branch", "git switch --force-create main HEAD"},
	{"a branch checked out elsewhere", "git checkout --ignore-other-worktrees main"},
	{"a branch checked out elsewhere", "git switch --ignore-other-worktrees story/B-2"},
	{"HEAD pointed at a branch", "git symbolic-ref HEAD refs/heads/main"},
	{"a push or fetch into its own refs", "git push . HEAD:main"},
	{"a push or fetch into its own refs", "git push . :story/B-2"},
	{"a push or fetch into its own refs", "git push /work/main HEAD:main"},
	{"a push or fetch into its own refs", "git fetch . HEAD:main"},
	{"a push or fetch into its own refs", "git fetch . story/A-1:refs/heads/main"},
	{"a fetch that then merges", "git pull . main"},
	{"a fetch that then merges", "git -C /work/main pull . story/A-1"},
	{"a reset that moves another checkout's branch", "git -C /work/main reset --hard story/A-1"},
	{"a reset that moves another checkout's branch", "git -C /work/main reset --soft story/A-1"},
	{"a merge into trunk", "git -C /work/main merge --ff-only story/A-1"},
}

func TestAC9TheClassifierRefusesEveryClassThatMovesABranch(t *testing.T) {
	for _, state := range []struct {
		name string
		s    State
	}{
		{"AC-9 ready to commit", heldInA1(ready)},
		{"AC-9 finished and not landed", rfFinished()},
	} {
		t.Run(state.name, func(t *testing.T) {
			for _, row := range rfMoves {
				t.Run(row.class, func(t *testing.T) { wtRefusedAny(t, row.command, state.s) })
			}
		})
	}
}

func TestAC9TheClassifierRefusesThemInPowerShellToo(t *testing.T) {
	s := wtPowerShell(rfFinished())
	for _, command := range []string{
		"git update-ref refs/heads/main HEAD",
		"git branch -f main HEAD",
		"& git update-ref -d refs/heads/story/B-2",
		"git push . HEAD:main",
		"$env:GIT_DIR = '/work/main/.git'; git update-ref refs/heads/main HEAD",
	} {
		wtRefusedAny(t, command, s)
	}
}

// What reads branches, and a branch of the session's own that is new, move
// nothing trunk or a story has.
func TestAC9TheClassifierLetsThroughWhatOnlyReadsBranches(t *testing.T) {
	for _, s := range []State{heldInA1(ready), rfFinished()} {
		for _, command := range []string{
			"git branch",
			"git branch --list",
			"git branch -a",
			"git branch -v",
			"git branch --show-current",
			"git rev-parse HEAD",
			"git show-ref",
			"git for-each-ref refs/heads",
			"git log --oneline main..HEAD",
			"git merge-base main HEAD",
			"git diff main...HEAD",
			"git symbolic-ref --short HEAD",
			"git status",
			"git worktree list",
		} {
			allowed(t, command, s)
		}
	}
}
