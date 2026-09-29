package shellpolicy

// US-029.2, decisions 4 to 6: the evasions each new rule was tried against.
// These are the ones the words alone decide, with the State the hook already
// gives: git's worktree commands, the shell's own names for where it is, a
// worktree added into a protected path, and a commit pointed at another
// checkout through git's environment or configuration. What needs the disk --
// the worktree's own root named by its absolute path, a directory above it, a
// link -- is held through the hook, in internal/hook/storyworktree_test.go.
//
// The State is shared_test.go's: the project at /work/project, another
// checkout of its repository at /work/main, and the project's own git
// directory at /work/own/gitdir.

import (
	"strings"
	"testing"
)

// wtRefusedAny holds a command to a refusal by some rule that says why and what
// to do instead. The rule is left to the plan where the story names none.
func wtRefusedAny(t *testing.T, command string, s State) {
	t.Helper()
	f, ok := inspect(t, command, s)
	if !ok {
		t.Errorf("allowed: %s", command)
		return
	}
	if f.Rule == "" || f.Reason == "" || f.Route == "" {
		t.Errorf("%s: finding = %+v, want a rule, a reason and a route", command, f)
	}
}

// wtRefusedAs is refused without stopping at the first row, so that a table of
// evasions reports every one that gets through.
func wtRefusedAs(t *testing.T, command string, s State, rule string) {
	t.Helper()
	f, ok := inspect(t, command, s)
	switch {
	case !ok:
		t.Errorf("allowed: %s", command)
	case f.Rule != rule:
		t.Errorf("%s was refused by %s, want %s (%s)", command, f.Rule, rule, f.Reason)
	case f.Reason == "" || f.Route == "":
		t.Errorf("%s: finding = %+v", command, f)
	}
}

func wtPowerShell(s State) State {
	s.PowerShell = true
	return s
}

// ---------------------------------------------------------------- T1, T2

// T1, T2 and decision 4: `git worktree remove`, `move` and `prune` end a hold
// -- the session's own, or another story's -- because the claim names the
// worktree's path. None of them is a session's to run, by any spelling of git,
// of the subcommand or of the command line around it, in a repository with one
// checkout or several.
func TestT1AndT2GitsWorktreeRemoveMoveAndPruneAreRefusedByEverySpelling(t *testing.T) {
	commands := []string{
		"git worktree remove wt",
		"git worktree remove --force .",
		"git worktree remove -f -f /work/main",
		"git worktree move . /tmp/moved",
		"git worktree move /work/main /tmp/moved",
		"git worktree prune",
		"git worktree prune --verbose --expire=now",
		// git's own options in front
		"git -C /work/main worktree remove --force .",
		"git -C .. worktree prune",
		"git --git-dir=/work/main/.git worktree prune",
		"git --git-dir /work/main/.git worktree prune",
		"git -c core.quotepath=off worktree remove wt",
		"git --no-pager worktree prune",
		// aliases, inline and set for later
		"git -c alias.gone='worktree remove --force' gone .",
		`git -c alias.gone="worktree move" gone . /tmp/moved`,
		"git -c alias.gone='!git worktree prune' gone",
		"git config alias.gone 'worktree remove --force'",
		"git config --local alias.gone 'worktree prune'",
		"git config --global alias.gone '!git worktree prune'",
		// the command line around it
		"sh -c 'git worktree prune'",
		`bash -lc "git worktree remove --force ."`,
		"eval 'git worktree prune'",
		"true; git worktree prune",
		"true && git worktree prune",
		"false || git worktree prune",
		"echo y | git worktree prune",
		"(git worktree prune)",
		"{ git worktree prune; }",
		"if true; then git worktree prune; fi",
		`echo "$(git worktree prune)"`,
		"echo . | xargs git worktree remove --force",
		"env git worktree prune",
		"command git worktree prune",
		"nice git worktree prune",
		"/usr/bin/git worktree prune",
		// quoting and escapes
		"git 'worktree' 'prune'",
		`git "worktree" "remove" --force .`,
		`git work\tree prune`,
		`git worktree pru\ne`,
		`g\it worktree prune`,
		"git worktree \\\n  prune",
	}
	for name, s := range map[string]State{
		"one checkout":             ready,
		"one checkout, not ready":  notReady,
		"with another checkout":    withShared(ready, sharedLandings),
		"from a directory below":   func() State { s := withShared(ready, sharedLandings); s.Dir = "internal"; return s }(),
		"from outside the project": func() State { s := withShared(ready, sharedLandings); s.Dir = "/work/main"; return s }(),
	} {
		t.Run("T1 "+name, func(t *testing.T) {
			for _, command := range commands {
				wtRefusedAny(t, command, s)
			}
		})
	}
	ps := wtPowerShell(withShared(ready, sharedLandings))
	for _, command := range []string{
		"git worktree prune",
		"& git worktree remove --force .",
		"git work`tree prune",
		"git worktree remove --force $PWD",
	} {
		wtRefusedAny(t, command, ps)
	}
}

// Reading about worktrees, listing them and saying the words are not running
// the commands.
func TestT1AndT2ListingWorktreesAndMentioningTheCommandsAreAllowed(t *testing.T) {
	for _, s := range []State{ready, withShared(ready, sharedLandings)} {
		for _, command := range []string{
			"git worktree list",
			"git worktree list --porcelain -v",
			"git -C /work/main worktree list",
			"git log --oneline --grep 'worktree prune'",
			"echo git worktree remove --force .",
			`git commit -m "refuse git worktree prune and git worktree remove"`,
			"git help worktree",
			"man git-worktree",
			"grep -rn 'worktree move' docs",
		} {
			allowed(t, command, s)
		}
	}
}

// ---------------------------------------------------------------- T1

// T1 and decision 4: `mv .` was the one spelling of moving the session's own
// checkout that was refused. The shell's own names for where it is -- $PWD,
// ~+, `pwd`, and $OLDPWD or ~- once a cd has moved it -- name the same
// directory, and taking it away takes the loop's record with it.
func TestT1TheShellsNamesForWhereItIsAreTheCheckoutTheyName(t *testing.T) {
	for _, command := range []string{
		`mv "$PWD" /tmp/moved`,
		"mv $PWD /tmp/moved",
		"mv ${PWD} /tmp/moved",
		`mv "${PWD}" /tmp/moved`,
		`mv "$(pwd)" /tmp/moved`,
		"mv `pwd` /tmp/moved",
		"mv ~+ /tmp/moved",
		`rm -rf "$PWD"`,
		`rm -r -f "$PWD"`,
		"rm -rf ~+",
		`cd internal && mv "$OLDPWD" /tmp/moved`,
		"cd internal && rm -rf ~-",
		`cd .. && mv "$OLDPWD" moved`,
		`sh -c 'mv "$PWD" /tmp/moved'`,
		`true; rm -rf "$PWD"`,
	} {
		wtRefusedAny(t, command, ready)
		wtRefusedAny(t, command, withShared(ready, sharedLandings))
	}
	below := withShared(ready, sharedLandings)
	below.Dir = "internal"
	wtRefusedAny(t, `cd .. && rm -rf "$PWD"`, below)
	wtRefusedAny(t, `cd .. && mv "$PWD" /tmp/moved`, below)
	for _, command := range []string{
		"Move-Item $PWD /tmp/moved",
		"Remove-Item -Recurse -Force $PWD",
	} {
		wtRefusedAny(t, command, wtPowerShell(ready))
	}

	// What those names lead to below the checkout, or another variable
	// altogether, is not the checkout.
	for _, command := range []string{
		`echo "$PWD"`,
		`ls "$PWD"`,
		`cp -r "$PWD" /tmp/copy`,
		`rm -rf "$PWD/build"`,
		`mv "$TMPDIR/a" "$TMPDIR/b"`,
	} {
		allowed(t, command, withShared(ready, sharedLandings))
	}
	allowed(t, `mv "$PWD" /tmp/moved`, below)
}

// ---------------------------------------------------------------- T5

// T5 and decision 5: a worktree added inside a checkout's protected paths is
// the nearest checkout for everything beneath it, and what it holds is
// protected by nothing. The path is found past git's options, the branch a
// -b names and a --reason's value, relative to a -C, and by every spelling the
// other rules read.
func TestT5AWorktreeIsNotAddedIntoAProtectedPathOfAnyCheckout(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		// the project's own
		"git worktree add .sdlc/state/x",
		"git worktree add .sdlc/stories/A-1/x",
		"git worktree add .claude/x",
		"git worktree add .git/x",
		// another checkout's
		"git worktree add /work/main/.sdlc/stories/x",
		"git worktree add /work/main/.sdlc/state/x",
		"git worktree add /work/main/.claude/x",
		"git worktree add /work/main/.git/x",
		// every form of add
		"git worktree add -b evil /work/main/.sdlc/stories/x",
		"git worktree add -B evil /work/main/.sdlc/stories/x",
		"git worktree add --detach /work/main/.sdlc/stories/x",
		"git worktree add --orphan -b evil /work/main/.sdlc/stories/x",
		"git worktree add -f --lock --reason why /work/main/.sdlc/stories/x",
		"git worktree add --no-checkout /work/main/.sdlc/stories/x main",
		"git worktree add --quiet --track -b evil /work/main/.sdlc/stories/x origin/main",
		"git worktree add /work/main/.sdlc/stories/x HEAD~1",
		// where it is added from
		"git -C /work/main worktree add .sdlc/stories/x",
		"git --git-dir=/work/main/.git worktree add /work/main/.sdlc/state/x",
		"cd /work/main && git worktree add .sdlc/stories/x",
		// spelled
		`git worktree add ".sdlc/state/x"`,
		`git worktree add .sd\lc/state/x`,
		"sh -c 'git worktree add .sdlc/state/x'",
		"true && git worktree add .sdlc/state/x",
		"(git worktree add /work/main/.claude/x)",
		"env git worktree add .sdlc/state/x",
		"git -c alias.wa='worktree add' wa .sdlc/state/x",
	} {
		wtRefusedAny(t, command, s)
	}
	for _, command := range []string{
		"git worktree add .sdlc/state/x",
		"git worktree add -b evil .claude/x",
	} {
		wtRefusedAny(t, command, ready)
	}
	allowed(t, "git worktree list", s)
}

// ---------------------------------------------------------------- T4

// T4 and decision 6: a commit git is pointed at another checkout with is a
// commit in that checkout, and meets commit-in-another-checkout, however the
// pointing is spelled: GIT_DIR or GIT_WORK_TREE in front of it, exported
// earlier, through env, in a subshell or a script, or set in PowerShell;
// core.worktree by -c, --config-env or git's config variables; an alias that
// carries any of those; a --work-tree add before a plain commit; or a value the
// shell has yet to expand, which could be any of them.
func TestT4ACommitPointedAtAnotherCheckoutIsRefusedByEverySpelling(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		// in front of the commit
		"GIT_DIR=/work/main/.git git commit -m x",
		"GIT_WORK_TREE=/work/main git commit -m x",
		`GIT_DIR="/work/main/.git" git commit -m x`,
		"GIT_WORK_TREE='/work/main' git commit -m x",
		`GIT_DIR=/work/main/.g\it git commit -m x`,
		"GIT_WORK_TREE=/work/main GIT_DIR=/work/own/gitdir git commit -m x",
		"GIT_WORK_TREE=/work/main git --git-dir=/work/own/gitdir commit -m x",
		"GIT_WORK_TREE=/work/main git merge side",
		"git_dir=/work/main/.git git commit -m x",
		"cd /work && GIT_WORK_TREE=main git commit -m x",
		// set earlier in the command
		"export GIT_DIR=/work/main/.git; git commit -m x",
		"export GIT_WORK_TREE=/work/main && git commit -m x",
		"GIT_DIR=/work/main/.git; git commit -m x",
		"GIT_WORK_TREE=/work/main; export GIT_WORK_TREE; git commit -m x",
		"declare -x GIT_WORK_TREE=/work/main; git commit -m x",
		"typeset -x GIT_DIR=/work/main/.git; git commit -m x",
		"set -a; GIT_DIR=/work/main/.git; git commit -m x",
		"export GIT_WORK_TREE=/work/main\ngit commit -m x",
		"(export GIT_WORK_TREE=/work/main; git commit -m x)",
		"export GIT_WORK_TREE=/work/main; (git commit -m x)",
		// through a wrapper
		"env GIT_WORK_TREE=/work/main git commit -m x",
		"env -i GIT_DIR=/work/main/.git git commit -m x",
		"sh -c 'GIT_DIR=/work/main/.git git commit -m x'",
		`bash -c "export GIT_WORK_TREE=/work/main; git commit -m x"`,
		"echo x | GIT_WORK_TREE=/work/main xargs git commit -m",
		// core.worktree
		"git -c core.worktree=/work/main commit -m x",
		"git -c core.WorkTree=/work/main commit -m x",
		"git -c CORE.WORKTREE=/work/main commit -m x",
		`git -c core.worktree="/work/main" commit -m x`,
		`git -c core.worktree=/work/ma\in commit -m x`,
		"git -c user.name=x -c core.worktree=/work/main commit -m x",
		"GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.worktree GIT_CONFIG_VALUE_0=/work/main git commit -m x",
		`GIT_CONFIG_PARAMETERS="'core.worktree'='/work/main'" git commit -m x`,
		// an alias that points it
		"git -c alias.ci='!git --work-tree=/work/main commit' ci -m x",
		"git -c alias.ci='!git -C /work/main commit' ci -m x",
		"git -c alias.ci='!GIT_DIR=/work/main/.git git commit' ci -m x",
		// staged from another checkout's work tree, then committed here
		"git --work-tree=/work/main add -A && git commit -m x",
		"git --work-tree /work/main add . ; git commit -m x",
		"git --work-tree=/work/main stage -A && git commit -m x",
		"git --work-tree=/work/main add -A\ngit commit -m x",
		"(git --work-tree=/work/main add -A) && git commit -m x",
		"GIT_WORK_TREE=/work/main git add -A && git commit -m x",
		// a place the shell has yet to expand
		"GIT_DIR=$(cat f) git commit -m x",
		`GIT_WORK_TREE="$OTHER" git commit -m x`,
		"export GIT_DIR=$G; git commit -m x",
		`git -c core.worktree="$W" commit -m x`,
		"git --config-env=core.worktree=W commit -m x",
		// the commit word escaped
		`GIT_WORK_TREE=/work/main git com\mit -m x`,
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}

	ps := wtPowerShell(s)
	for _, command := range []string{
		"$env:GIT_WORK_TREE = '/work/main'; git commit -m x",
		"$env:git_dir='/work/main/.git'; git commit -m x",
		"Set-Item env:GIT_WORK_TREE /work/main; git commit -m x",
		`Set-Item -Path Env:\GIT_DIR -Value /work/main/.git; git commit -m x`,
		"[Environment]::SetEnvironmentVariable('GIT_WORK_TREE', '/work/main'); git commit -m x",
	} {
		wtRefusedAs(t, command, ps, "commit-in-another-checkout")
	}
}

// T4: pointing git at the project itself, or at another checkout for anything
// but a commit, is not a commit in another checkout, and a mention is not an
// assignment. The project's own commit still meets the gate.
func TestT4GitPointedAtItsOwnCheckoutOrAtAnotherToReadIsNotRefused(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		"git commit -m x",
		"git add -A && git commit -m x",
		"GIT_DIR=/work/own/gitdir git commit -m x",
		"GIT_WORK_TREE=/work/project git commit -m x",
		"git -c core.worktree=/work/project commit -m x",
		"git -c core.autocrlf=false commit -m x",
		"GIT_AUTHOR_NAME=x GIT_COMMITTER_DATE=now git commit -m x",
		"GIT_DIR=/work/main/.git git log --oneline",
		"export GIT_WORK_TREE=/work/main; git status",
		"git --work-tree=/work/main status && git commit -m x",
		"git -c core.worktree=/work/main diff",
		`git commit -m "GIT_DIR=/work/main/.git was the bug"`,
		`git commit -m "set core.worktree=/work/main by hand"`,
	} {
		allowed(t, command, s)
	}
	for _, command := range []string{
		"GIT_DIR=/work/own/gitdir git commit -m x",
		"GIT_WORK_TREE=/work/project git commit -m x",
	} {
		wtRefusedAs(t, command, withShared(notReady, sharedLandings), "commit-gate")
	}
	if f, ok := inspect(t, "GIT_DIR=/work/main/.git git commit -m x", withShared(notReady, sharedLandings)); !ok ||
		!strings.Contains(f.Rule, "commit") {
		t.Errorf("a commit in another checkout before the gates passed = %+v, %v; want it refused", f, ok)
	}
}
