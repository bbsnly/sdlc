package shellpolicy

// US-029.2, after Gate 6: the spellings the verifier put through the hook from
// a session held in story worktree A-1 that got past worktree-through-the-tool,
// the rule on taking a checkout away, and commit-in-another-checkout
// (VERIFICATION.md, S1 to S3). Each is one the story holds: every `git
// worktree` action but list, however git is reached; a checkout taken away by
// any removal; a commit pointed at another checkout by any spelling. Beside
// each is the everyday form of the same tool, which has to keep working.
//
// Left out on purpose, and owned elsewhere: a command whose words or script
// arrive on standard input (`echo prune | xargs git worktree`, `echo 'git
// worktree prune' | sh`, `echo commit -m x | xargs git -C ../B-2`), which is
// US-042, a script run from a file, US-041, and a program in another language
// (osascript telling the Finder to delete), which the rules read only for the
// paths it names.
//
// The fixture is takeaway_test.go's heldInA1.

import "testing"

// AC-6 and S1: a program that runs the command after its own options wraps
// `git worktree` as env and nice do. Read as the program, taskset or firejail
// hid the prune behind it, and pruning forgets another story's worktree.
func TestAC6AndS1AWrapperTheRulesDoNotNameStillRunsGitWorktree(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		"taskset -c 0 git worktree remove --force ../B-2",
		"taskset 1 git worktree prune",
		"watch -n1 git worktree prune",
		"chronic git worktree prune",
		"ts git worktree prune",
		"firejail git worktree prune",
		"systemd-run --user git worktree prune",
		"unshare -r git worktree prune",
		"nsenter -t 1 git worktree prune",
		"parallel git worktree ::: prune",
	} {
		wtRefusedAs(t, command, s, "worktree-through-the-tool")
	}
	for _, command := range []string{
		"taskset -c 0 go test ./...",
		"taskset -c 0 git worktree list",
		"watch -n1 git status",
		"chronic go build ./...",
		"go test ./... 2>&1 | ts '[%H:%M:%S]'",
		"firejail --noprofile go test ./...",
		"systemd-run --user --scope go build ./...",
		"unshare -r id",
		"nsenter -t 1 -m ls",
		"parallel gofmt -l ::: a.go b.go",
	} {
		allowed(t, command, s)
	}
}

// AC-6 and S2: moving a checkout to the trash, or removing it through
// busybox's rm, takes it away with its story's uncommitted work as rm -rf
// does.
func TestAC6AndS2ATrashToolOrBusyboxTakesACheckoutAwayAsRmDoes(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		"trash ../B-2",
		"gio trash ../B-2",
		"busybox rm -rf ../B-2",
		"trash /work/main",
		`gio trash "$PWD"`,
	} {
		wtRefusedAs(t, command, s, "loop-state-through-the-tool")
	}
	for _, command := range []string{
		"trash build",
		"gio trash notes.txt",
		"gio trash --empty",
		"busybox rm -rf build",
		"busybox ls ../B-2",
	} {
		allowed(t, command, s)
	}
}

// AC-6 and S3: PowerShell sets a variable with `${env:NAME} = value` and with
// Set-Content on the Env: drive as well as with `$env:NAME = value`, and git
// reads GIT_DIR and GIT_WORK_TREE however they were set.
func TestAC6AndS3PowerShellsOtherWaysOfSettingGitsPlaceAreACommitInAnotherCheckout(t *testing.T) {
	s := wtPowerShell(heldInA1(ready))
	for _, command := range []string{
		"${env:GIT_DIR} = '/work/main/.git'; git commit -m x",
		"${env:GIT_WORK_TREE} = '/work/main'; git commit -m x",
		"${Env:GIT_WORK_TREE}='../B-2'; git commit -m x",
		"Set-Content Env:GIT_WORK_TREE '/work/main'; git commit -m x",
		`Set-Content -Path Env:\GIT_WORK_TREE -Value /work/main; git commit -m x`,
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}
	for _, command := range []string{
		"git commit -m x",
		"${env:GIT_DIR} = '/work/main/.git/worktrees/A-1'; git commit -m x",
		"Set-Content Env:GIT_AUTHOR_NAME 'me'; git commit -m x",
		"Set-Content notes.txt 'x'",
		`${env:PATH} = 'C:\tools;' + $env:PATH; git status`,
	} {
		allowed(t, command, s)
	}
}

// AC-6 and S3: bash assigns a variable with `printf -v` and with `read` as
// well as with NAME=value, and once exported git reads it as though it had
// been written in front of the commit.
func TestAC6AndS3PrintfDashVAndReadSetGitsPlaceAsAnAssignmentDoes(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		"printf -v GIT_DIR %s /work/main/.git; export GIT_DIR; git commit -m x",
		"read GIT_DIR <<< /work/main/.git; export GIT_DIR; git commit -m x",
		"printf -v GIT_WORK_TREE '%s' ../B-2; export GIT_WORK_TREE; git commit -m x",
		"read -r GIT_WORK_TREE <<< /work/main; export GIT_WORK_TREE; git commit -m x",
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}
	for _, command := range []string{
		`printf -v stamp '%s' now; git commit -m "$stamp"`,
		"read -r answer <<< yes; git commit -m x",
		`printf '%s\n' done; git commit -m x`,
	} {
		allowed(t, command, s)
	}
}

// AC-6, S2 and S3, found while measuring the new readers rather than by the
// verifier: env -C runs its command in the directory it names, as a cd in
// front of it would. The git rules read it for where `git worktree add` and
// `git config` land, and the commit and take-away rules did not: the commit
// landed in the main checkout, and the rm took another story's worktree.
func TestAC6EnvDashCRunsItsCommandInTheCheckoutItNames(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		"env -C /work/main git commit -m x",
		"env -C ../B-2 git commit -m x",
		"env --chdir=/work/main git commit -m x",
	} {
		wtRefusedAs(t, command, s, "commit-in-another-checkout")
	}
	for _, command := range []string{
		"env -C .. rm -rf B-2",
		"env -C /work/main rm -rf .sdlc/worktrees/B-2",
	} {
		wtRefusedAs(t, command, s, "loop-state-through-the-tool")
	}
	for _, command := range []string{
		"env -C sub git commit -m x",
		"env -C /work/main git status",
		"env -C build rm -rf out",
	} {
		allowed(t, command, s)
	}
}

// AC-6 and T1, found while measuring the new readers rather than by the
// verifier: git hands its -c settings to what a `!` alias runs, through
// GIT_CONFIG_PARAMETERS, so the git inside the alias finds the aliases set on
// the command line around it. Read without them, `git b` inside `!git b` was
// a subcommand called b, and the prune or the commit it stood for met no rule.
func TestAC6AndT1TheGitInsideAShellAliasSeesTheSettingsOfTheCommandAroundIt(t *testing.T) {
	s := heldInA1(ready)
	for _, command := range []string{
		"git -c alias.a='!git b' -c alias.b='worktree prune' a",
		"git -c alias.a='!git b' -c alias.b='!git worktree prune' a",
	} {
		wtRefusedAs(t, command, s, "worktree-through-the-tool")
	}
	wtRefusedAs(t, "git -c alias.a='!git b' -c alias.b='-C ../B-2 commit -m x' a", s, "commit-in-another-checkout")
	for _, command := range []string{
		"git -c alias.a='!git b' -c alias.b=status a",
		"git -c alias.a='!git status' a",
	} {
		allowed(t, command, s)
	}
}

// The spellings the released rules and the story's first pass already refuse
// from the same fixture. They pass today, and are here so that the rows above
// fail for the spelling and not for the fixture.
func TestAC6TheFixtureMeetsTheRulesAsTheHookDoes(t *testing.T) {
	s := heldInA1(ready)
	for _, tc := range []struct{ command, rule string }{
		{"git worktree prune", "worktree-through-the-tool"},
		{"nice git worktree prune", "worktree-through-the-tool"},
		{"rm -rf ../B-2", "loop-state-through-the-tool"},
		{"mv /work/main /tmp/m", "loop-state-through-the-tool"},
		{"git -C ../B-2 commit -m x", "commit-in-another-checkout"},
		{"GIT_DIR=/work/main/.git git commit -m x", "commit-in-another-checkout"},
		{"GIT_WORK_TREE=/work/main; export GIT_WORK_TREE; git commit -m x", "commit-in-another-checkout"},
	} {
		wtRefusedAs(t, tc.command, s, tc.rule)
	}
	wtRefusedAs(t, "$env:GIT_WORK_TREE = '/work/main'; git commit -m x", wtPowerShell(s), "commit-in-another-checkout")
	for _, command := range []string{
		"git worktree list",
		"git commit -m x",
		"GIT_DIR=/work/main/.git/worktrees/A-1 git commit -m x",
		"rm -rf build",
	} {
		allowed(t, command, s)
	}
	wtRefusedAs(t, "git commit -m x", heldInA1(notReady), "commit-gate")
}
