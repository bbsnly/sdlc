package shellpolicy

// US-029.2 and US-029.1's recorder: where a git command's -C, --git-dir and
// --work-tree put it, as the recorder and the discard rule read them. The
// recorder writes down a HEAD only for a commit the words place in the
// project's own repository; the discard rule refuses throwing away what
// another checkout's work tree holds, wherever the command runs from.

import (
	"slices"
	"testing"
)

// AC-6: the recorder is told about a commit only where every -C and --git-dir
// in front of it is spelled out and names the project's own repository. A
// place the shell has yet to expand could be anywhere, and a repository
// outside the project is another one's.
func TestAC6TheRecorderIsToldOnlyAboutACommitItsWordsPlaceInTheProject(t *testing.T) {
	for _, tc := range []struct {
		command string
		commits bool
	}{
		{"git -C /work/project commit -m x", true},
		{"git -C sub commit -m x", true},
		{"git --git-dir=.git commit -m x", true},
		{"git --git-dir /work/project/.git commit -m x", true},
		{"git -C sub --git-dir=../.git commit -m x", true},
		{"git -c user.name=me --work-tree=. commit -m x", true},
		{`git -C "$X" commit -m x`, false},
		{`git -C "$(mktemp -d)" commit -m x`, false},
		{`git --git-dir="$G" commit -m x`, false},
		{"git --git-dir=sub/.git commit -m x", false},
	} {
		t.Run(tc.command, func(t *testing.T) {
			fired := 0
			s := watching(&fired)
			s.Resolve = func(word string) string {
				if rest, ok := cutProject(word); ok {
					return rest
				}
				if isAbsolute(word) {
					return ""
				}
				return word
			}
			if f, refused := Inspect(tc.command, s); refused {
				t.Fatalf("the command was refused with every gate passed: %+v", f)
			}
			if (fired > 0) != tc.commits {
				t.Errorf("the recorder was told %d times, want it told %v", fired, tc.commits)
			}
		})
	}
}

// AC-6 and T2: a work tree given with --work-tree is the checkout the command
// discards from, relative to where the command runs, even where the command
// runs outside this repository.
func TestAC6DiscardingAnotherCheckoutsWorkTreeFromOutsideTheProjectIsRefused(t *testing.T) {
	s := withShared(ready, sharedLandings)
	for _, command := range []string{
		"cd /tmp/other && git --work-tree /work/main reset --hard",
		"cd /tmp/other && git --work-tree=../../work/main checkout -f",
	} {
		wtRefusedAs(t, command, s, "loop-state-through-the-tool")
	}
	allowed(t, "cd /tmp/other && git --work-tree /tmp/tree reset --hard", s)
	allowed(t, "cd /tmp/other && git reset --hard", s)
}

// AC-6 and T4: a place of a kind a rule does not ask about is passed over:
// the project's own git directory named for an add is not another checkout's
// work tree or index, and the commit after it is the project's.
func TestAC6AGitDirectoryIsNotAWorkTreeOrAnIndex(t *testing.T) {
	s := withShared(ready, sharedLandings)
	allowed(t, "GIT_DIR=/work/own/gitdir git add -A && git commit -m x", s)
	wtRefusedAs(t, "GIT_INDEX_FILE=/work/main/.git/index git add -A && git commit -m x", s, "commit-in-another-checkout")
	// An alias set with --config-env for a variable nothing sets cannot be
	// read, and is refused where it is given whether or not it is run.
	grRefusedSaying(t, "git --config-env=alias.x=V status", s, "git-command-cannot-be-read",
		"the alias `alias.x`, whose value cannot be read")
}

// AC-6 and T1: a brace list in git's subcommand is read once for each word it
// makes, and an empty word is dropped as bash drops it.
func TestAC6ABraceListInTheSubcommandIsReadOnceForEachWord(t *testing.T) {
	lookup := func(string) (string, bool, bool) { return "", false, false }
	var subs [][]string
	for _, g := range gitReadings([]string{"-C", "x", "{worktree,}", "prune"}, nil, lookup, false) {
		subs = append(subs, g.args)
	}
	want := [][]string{{"-C", "x", "worktree", "prune"}, {"-C", "x", "prune"}}
	if !slices.EqualFunc(subs, want, slices.Equal[[]string]) {
		t.Errorf("gitReadings of `git -C x {worktree,} prune` = %q, want %q", subs, want)
	}
	if g := gitReadings([]string{"status"}, nil, lookup, false); len(g) != 1 || g[0].sub != "status" {
		t.Errorf("gitReadings of `git status` = %+v, want one reading of status", g)
	}
}

// AC-6 and T1: a word is taken away only once however often it is named, and
// a variable whose name starts with PWD is not the shell's name for where it
// is.
func TestAC6ARepeatedWordOrALookalikeOfPWDIsNotTheCheckout(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		"rm -rf build build",
		"echo build build | xargs rm -rf",
		`rm -rf "$PWDX"`,
		"find .. -name B-2 -empty -delete",
	} {
		allowed(t, command, s)
	}
	taRefusedNaming(t, "find ../B-2 -name B-2 -delete", s, a1Other)
	taRefusedNaming(t, "echo build ../B-2 build | xargs rm -rf", s, a1Other)
	allowed(t, "find .. -name B-2 -delete", ready)
	// Outside every directory the tree has there is nothing to reach.
	tree := newCheckoutTree(CheckoutSet{Checkouts: []string{"rel/a"}})
	if n, ok := tree.reach("elsewhere", []string{"*"}); ok {
		t.Errorf("a glob outside the tree reached %s", n.path)
	}
}
