package shellpolicy

import "testing"

// watching is the state the hook is in once every gate before the commit has
// passed: the command goes through, and the recorder wants to know whether it
// was a commit being allowed.
func watching(fired *int) State {
	return State{CommitReady: true, Commits: func() { *fired++ }}
}

// The recorder asks this package the same question the refusal asks, rather
// than reading "is this a commit" a second time somewhere else: a second
// reading is a hole by construction, and the frozen suite lands a commit with
// `git revert`, which only this package calls a commit.
//
// But the refusal's question and the recorder's are not the same one. The
// refusal asks whether a command could put a commit on the branch, now or
// later; the recorder asks whether this command commits NOW. `git config
// alias.ci commit` and `git commit --dry-run` are both allowed once the gates
// have passed and neither makes a commit. An observation planted by either says
// the session was at this HEAD about to commit, so the next commit made on that
// HEAD is recorded as the story's -- including a person's, made in their own
// terminal, which is the defect this story exists to fix, reproduced on demand
// by one innocuous allowed command.
func TestTheRecorderIsToldOnlyAboutACommandThatCommitsNow(t *testing.T) {
	for _, tc := range []struct {
		command string
		commits bool
	}{
		{`git commit -m "the work"`, true},
		{"git commit -a -m done", true},
		{"git commit --amend --no-edit", true},
		{"git merge --no-ff side", true},
		{"git cherry-pick 1234abc", true},
		{"git revert --no-edit 1234abc", true},
		{"git am patch.mbox", true},

		// Commits nothing now: the alias is a commit somebody makes later, and
		// the rehearsal is the gate's own question asked without answering it.
		{"git config alias.ci commit", false},
		{`git config --global alias.ci "commit -a"`, false},
		{`git config --local alias.up "merge --ff-only"`, false},
		{"git commit --dry-run", false},
		{"git commit --dry-run --short", false},
		{"git commit -m done --dry-run", false},

		// Already outside the question this package answers, and they stay
		// outside it: each of these leaves the change in the working tree for
		// a later `git commit` to meet the gate with.
		{"git merge --no-commit side", false},
		{"git merge --squash side", false},
		{"git cherry-pick -n 1234abc", false},
		{"git revert -n 1234abc", false},
		{"git merge --abort", false},
		{"git rebase --abort", false},
		{"git status --porcelain", false},
		{"git log --grep commit", false},
		{"ls -la", false},

		// The evasion the exclusion invites, and the reason it belongs in this
		// package's own table rather than in a second reading somewhere else: a
		// flag that takes the next word as its value means the word after it is
		// not a flag. Each of these commits, with `--dry-run` as a message or a
		// file name, and an observation lost here is a commit of the story's
		// own falling silently out of its range.
		{"git commit -m --dry-run", true},
		{"git commit --message --dry-run", true},
		{"git commit -F --dry-run", true},
	} {
		t.Run(tc.command, func(t *testing.T) {
			fired := 0
			if f, refused := Inspect(tc.command, watching(&fired)); refused {
				t.Fatalf("the command was refused with every gate passed: %+v", f)
			}
			if (fired > 0) != tc.commits {
				t.Errorf("the recorder was told %d times, want it told %v", fired, tc.commits)
			}
		})
	}
}

// The recorder's sharper question must not narrow what the refusal covers. An
// alias set before the gates have passed is a commit after them, and a commit
// rehearsed before them is the rehearsal of exactly what the gate is holding
// back; both were refused before this story and both still are. One classifier
// answering two questions is the point -- one classifier answering a smaller
// question is a hole.
func TestTheSpellingsTheRecorderIgnoresAreStillRefusedBeforeTheGates(t *testing.T) {
	for _, command := range []string{
		"git config alias.ci commit",
		`git config --global alias.ci "commit -a"`,
		"git commit --dry-run",
		"git commit -m done --dry-run",
	} {
		refused(t, command, notReady, "commit-gate")
	}
}

// A commit in another repository is not this story's to hold back, and it is
// not this story's to write down either: that repository's HEAD is not a commit
// anything in this project will ever be a child of, and a range built from it
// would name commits this repository does not have.
func TestACommitInAnotherRepositoryIsNotWrittenDown(t *testing.T) {
	fired := 0
	elsewhere := State{
		CommitReady: false, CommitWhy: "code_review has not passed",
		Commits: func() { fired++ },
		Resolve: func(string) string { return "" },
	}
	if f, refused := Inspect(`git commit -m "the work"`, elsewhere); refused {
		t.Fatalf("a commit in another repository was refused: %+v", f)
	}
	if fired != 0 {
		t.Errorf("the recorder was told about a commit in another repository %d times", fired)
	}

	// The control: the same command in this repository, so that nothing above
	// is the rule answering rather than a recorder nobody ever asks.
	here := 0
	if _, refused := Inspect(`git commit -m "the work"`, watching(&here)); refused {
		t.Fatal("a commit in this repository was refused with every gate passed")
	}
	if here == 0 {
		t.Error("the recorder is told about no commit at all, so the case above proves nothing")
	}
}
