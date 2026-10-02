package gitx

// US-029.2, unit level: what worktree_test.go leaves out of the worktree
// commands. AddWorktree's refusals before git is asked to write anything, and
// its rollback when git fails after the branch is cut (AC-4, T6): what it made
// is taken away, what it did not make is kept, and whatever is left is named.
// RemoveWorktree's refusals, and the readers start and doctor describe a story
// worktree with -- Ahead, Holds, Version and Worktrees.
//
// A failure part-way through `git worktree add` is made the way git makes one:
// with the repository's post-checkout hook, which git runs as it checks the
// worktree out, and whose exit status `git worktree add` returns.
//
// Helpers carry a wu prefix, for worktree unit.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// wuHook makes body the repository's post-checkout hook, which git runs in the
// new worktree as `git worktree add` checks it out.
func wuHook(t *testing.T, root, body string) {
	t.Helper()
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, root, "config", "core.hooksPath", filepath.ToSlash(hooks))
}

// wuRefusal is err as SDLC-E0053, the code every worktree that could not be
// made or taken away has.
func wuRefusal(t *testing.T, err error, what string) *sdlcerr.Error {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != sdlcerr.WorktreeNotMade {
		t.Fatalf("%s = %v, want SDLC-E0053", what, err)
	}
	return e
}

// wuSays holds a refusal to naming each of want in what it says.
func wuSays(t *testing.T, e *sdlcerr.Error, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(e.What+" "+e.Why, w) {
			t.Errorf("the refusal says %q / %q, want it to say %q", e.What, e.Why, w)
		}
	}
}

// wuHasBranch reports whether the repository at root has the branch.
func wuHasBranch(t *testing.T, root, branch string) bool {
	t.Helper()
	return !wtGitFails(t, root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
}

// wuSkipIfPermissionsDoNotBind is for a test that takes away a directory's
// permission: not on Windows, which does not carry Unix permission bits, and
// not as root, which ignores them.
func wuSkipIfPermissionsDoNotBind(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this platform does not restrict a directory owner by permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

// wuFilesRefs skips a test that holds a branch's lock file where the
// repository keeps its branches in a reftable, which has no such file.
func wuFilesRefs(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".git", "reftable")); err == nil {
		t.Skip("this repository keeps its refs in a reftable, with no lock file per branch")
	}
}

// wuFakeGit puts a git on PATH that prints out and nothing else, for what git
// says about its own version.
func wuFakeGit(t *testing.T, out string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script on PATH is not a command here")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf '%s\\n' " + strconv.Quote(out) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// ---------------------------------------------------------------- AC-4, T6: refused before anything is made

func TestAC4AddWorktreeRefusesARelativePathAndMakesNothing(t *testing.T) {
	root, _, _ := wtTrunk(t)
	err := AddWorktree(t.Context(), root, filepath.Join(".sdlc", "worktrees", "US-1"), "story/US-1", "main")
	wuSays(t, wuRefusal(t, err, "AddWorktree to a relative path"), "not an absolute path")
	if wuHasBranch(t, root, "story/US-1") {
		t.Error("the branch was cut for a worktree that was refused")
	}
	if got := wtList(t, root); len(got) != 1 {
		t.Errorf("git knows these worktrees: %v", got)
	}
}

// Nothing start makes goes through a link or over a file: git would make the
// worktree wherever the link led.
func TestAC4AndT6AddWorktreeNeverMakesTheWorktreeThroughALinkOrOverAFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		// prepare puts something in the way, and returns the directory a
		// link leads to, where there is one.
		prepare func(t *testing.T, root string) string
		says    string
	}{
		{"AC-4 .sdlc/worktrees is a link", func(t *testing.T, root string) string {
			elsewhere := t.TempDir()
			write(t, root, ".sdlc/keep", "")
			if err := os.Symlink(elsewhere, filepath.Join(root, ".sdlc", "worktrees")); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}
			return elsewhere
		}, "is a link"},
		{"AC-4 the story's directory is a link", func(t *testing.T, root string) string {
			elsewhere := t.TempDir()
			write(t, root, ".sdlc/worktrees/keep", "")
			if err := os.Symlink(elsewhere, wtStoryDir(root, "US-1")); err != nil {
				t.Skipf("no symbolic link here: %v", err)
			}
			return elsewhere
		}, "is a link"},
		{"AC-4 .sdlc is a file", func(t *testing.T, root string) string {
			write(t, root, ".sdlc", "not a directory\n")
			return ""
		}, "is a file"},
		{"AC-4 .sdlc/worktrees is a file", func(t *testing.T, root string) string {
			write(t, root, ".sdlc/worktrees", "not a directory\n")
			return ""
		}, "is a file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, _ := wtTrunk(t)
			elsewhere := tc.prepare(t, root)
			before := wtSnapshot(t, root)

			err := AddWorktree(t.Context(), root, wtStoryDir(root, "US-1"), "story/US-1", "main")
			wuSays(t, wuRefusal(t, err, "AddWorktree "+tc.name), tc.says)
			if !errors.Is(err, ErrNotADirectory) {
				t.Errorf("the refusal is not ErrNotADirectory, which start says to move the link or file for: %v", err)
			}
			if wuHasBranch(t, root, "story/US-1") {
				t.Error("the branch was cut for a worktree that was refused")
			}
			if got := wtList(t, root); len(got) != 1 {
				t.Errorf("git knows these worktrees: %v", got)
			}
			if elsewhere != "" {
				if entries, _ := os.ReadDir(elsewhere); len(entries) > 0 {
					t.Errorf("something was written through the link, into %s", elsewhere)
				}
			}
			if after := wtSnapshot(t, root); after != before {
				t.Errorf("the checkout changed: before %+v, after %+v", before, after)
			}
		})
	}
}

// A worktree asked for outside the checkout is held to the same: the
// directory it goes in is not a link.
func TestT6AddWorktreeOutsideTheCheckoutRefusesALinkAboveIt(t *testing.T) {
	root, _, _ := wtTrunk(t)
	outside := t.TempDir()
	link := filepath.Join(outside, "linked")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("no symbolic link here: %v", err)
	}
	err := AddWorktree(t.Context(), root, filepath.Join(link, "US-1"), "story/US-1", "main")
	wuSays(t, wuRefusal(t, err, "AddWorktree under a link outside the checkout"), "is a link")
	if wuHasBranch(t, root, "story/US-1") {
		t.Error("the branch was cut for a worktree that was refused")
	}
}

// A place start cannot look into may hold somebody's work: it is refused, and
// said, rather than written over.
func TestAC4AddWorktreeRefusesAPlaceItCannotReadAndLeavesItAsItWas(t *testing.T) {
	wuSkipIfPermissionsDoNotBind(t)
	t.Run("AC-4 the story's directory cannot be read", func(t *testing.T) {
		root, _, _ := wtTrunk(t)
		dir := wtStoryDir(root, "US-1")
		write(t, dir, "notes.md", "a person's notes\n")
		if err := os.Chmod(dir, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
		wuSays(t, wuRefusal(t, err, "AddWorktree into a directory it cannot read"), "could not be read")
		if !errors.Is(err, ErrInTheWay) {
			t.Errorf("a directory start cannot read is not in the way: %v", err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if raw, err := os.ReadFile(filepath.Join(dir, "notes.md")); err != nil || string(raw) != "a person's notes\n" {
			t.Errorf("the directory's file is %q, %v; want it kept", raw, err)
		}
		if wuHasBranch(t, root, "story/US-1") {
			t.Error("the branch was cut for a worktree that was refused")
		}
	})
	t.Run("AC-4 .sdlc cannot be searched", func(t *testing.T) {
		root, _, _ := wtTrunk(t)
		sdlc := filepath.Join(root, ".sdlc")
		write(t, sdlc, "keep", "")
		if err := os.Chmod(sdlc, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(sdlc, 0o755) })

		err := AddWorktree(t.Context(), root, wtStoryDir(root, "US-1"), "story/US-1", "main")
		wuSays(t, wuRefusal(t, err, "AddWorktree under a directory it cannot search"), "could not be read")
		if !errors.Is(err, ErrNotADirectory) {
			t.Errorf("the refusal is not ErrNotADirectory: %v", err)
		}
		if wuHasBranch(t, root, "story/US-1") {
			t.Error("the branch was cut for a worktree that was refused")
		}
	})
}

// T6: the branch is created only where there is none. One that another git is
// creating at that moment, which holds its lock, is in the way, and nothing is
// made; the other git's lock is left for it.
func TestAC4AndT6ABranchAnotherGitIsCreatingIsInTheWayAndNothingIsMade(t *testing.T) {
	root, _, _ := wtTrunk(t)
	wuFilesRefs(t, root)
	lock := filepath.Join(root, ".git", "refs", "heads", "story", "US-1.lock")
	write(t, filepath.Dir(lock), filepath.Base(lock), "")
	dir := wtStoryDir(root, "US-1")

	err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
	wuSays(t, wuRefusal(t, err, "AddWorktree while another git holds the branch's lock"), `"story/US-1" already exists`)
	if !errors.Is(err, ErrInTheWay) {
		t.Errorf("a branch another git is making is not in the way: %v", err)
	}
	wtWantNothingMade(t, root, dir, "story/US-1")
	if _, err := os.Stat(lock); err != nil {
		t.Errorf("the other git's lock was taken away: %v", err)
	}
}

// ---------------------------------------------------------------- AC-4, T6: rolled back after the branch is cut

// git cuts nothing back itself: a `git worktree add` that fails after the
// checkout has the worktree registered and the branch made. AddWorktree takes
// both away, and the directory, and the checkout it ran from is as it was.
func TestAC4AWorktreeGitFailsToFinishIsTakenAwayWithItsBranch(t *testing.T) {
	root, _, _ := wtTrunk(t)
	wuHook(t, root, "echo 'post-checkout: refused by the test' >&2\nexit 1")
	before := wtSnapshot(t, root)
	dir := wtStoryDir(root, "US-1")

	err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
	e := wuRefusal(t, err, "AddWorktree with a post-checkout hook that fails")
	wuSays(t, e, "could not be made")
	if strings.Contains(e.Why, "could not all be taken away") {
		t.Errorf("the refusal says something was left, and nothing should be: %q", e.Why)
	}
	for _, cause := range []error{ErrInTheWay, ErrNotADirectory, ErrBranchName, ErrNoTrunk} {
		if errors.Is(err, cause) {
			t.Errorf("a checkout git could not finish reads as %v", cause)
		}
	}
	wtWantNothingMade(t, root, dir, "story/US-1")
	if after := wtSnapshot(t, root); after != before {
		t.Errorf("the checkout changed: before %+v, after %+v", before, after)
	}

	// Nothing left is nothing in the way: with the hook put right, the same
	// worktree is made.
	run(t, root, "config", "--unset", "core.hooksPath")
	if err := AddWorktree(t.Context(), root, dir, "story/US-1", "main"); err != nil {
		t.Errorf("the worktree could not be made once git could finish: %v", err)
	}
}

// An empty directory a person left where the worktree goes is theirs: it is
// there again after the rollback, and empty.
func TestAC4AndT6TheRollbackPutsBackAnEmptyDirectoryThatWasThere(t *testing.T) {
	root, _, _ := wtTrunk(t)
	wuHook(t, root, "exit 1")
	dir := wtStoryDir(root, "US-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
	wuRefusal(t, err, "AddWorktree into an empty directory, with a hook that fails")
	entries, statErr := os.ReadDir(dir)
	if statErr != nil || len(entries) > 0 {
		t.Errorf("the empty directory that was there is %v, %v; want it there and empty", entries, statErr)
	}
	if wuHasBranch(t, root, "story/US-1") {
		t.Error("the branch was left behind")
	}
	if got := wtList(t, root); len(got) != 1 {
		t.Errorf("git knows these worktrees: %v", got)
	}
}

// T6: the branch is deleted only while it is where AddWorktree cut it. One
// something else moved -- here the hook, committing on it -- holds work that
// is not start's, and is kept and named.
func TestAC4AndT6TheRollbackKeepsABranchSomethingElseMovedAndSaysSo(t *testing.T) {
	root, _, tip := wtTrunk(t)
	wuHook(t, root, "git -c user.email=a@b -c user.name=t commit --quiet --allow-empty -m 'the hook'\nexit 1")
	dir := wtStoryDir(root, "US-1")

	err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
	e := wuRefusal(t, err, "AddWorktree whose branch the hook moved")
	wuSays(t, e, "could not all be taken away", `the branch "story/US-1" is still there`)
	if !wuHasBranch(t, root, "story/US-1") {
		t.Fatal("the branch holding the hook's commit was deleted")
	}
	if got := wtGitOut(t, root, "rev-parse", "refs/heads/story/US-1"); got == tip {
		t.Error("the hook did not move the branch, so this tested nothing")
	}
	if got := wtList(t, root); len(got) != 1 {
		t.Errorf("git knows these worktrees: %v", got)
	}
	if _, err := os.Lstat(dir); err == nil {
		t.Errorf("%s was left behind", dir)
	}
}

// A worktree git will not remove -- the hook locked it -- is named as still
// registered, and the directory and branch start made still go.
func TestAC4TheRollbackNamesAWorktreeGitWouldNotRemove(t *testing.T) {
	root, _, _ := wtTrunk(t)
	wuHook(t, root, `git worktree lock --reason "held by the test" .`+"\nexit 1")
	dir := wtStoryDir(root, "US-1")

	err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
	wuSays(t, wuRefusal(t, err, "AddWorktree whose worktree the hook locked"),
		"could not all be taken away", "is still registered with git")
	if _, err := os.Lstat(dir); err == nil {
		t.Errorf("%s was left behind", dir)
	}
	if wuHasBranch(t, root, "story/US-1") {
		t.Error("the branch start cut was left behind")
	}
}

// A directory that cannot be removed is named as still there.
func TestAC4TheRollbackNamesADirectoryThatCouldNotBeRemoved(t *testing.T) {
	wuSkipIfPermissionsDoNotBind(t)
	root, _, _ := wtTrunk(t)
	wuHook(t, root, "mkdir -p stuck/inner && touch stuck/inner/file && chmod 555 stuck/inner\nexit 1")
	dir := wtStoryDir(root, "US-1")
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "stuck", "inner"), 0o755) })

	err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
	wuSays(t, wuRefusal(t, err, "AddWorktree whose worktree cannot be removed"),
		"could not all be taken away", strconv.Quote(filepath.ToSlash(dir))+" is still there")
	if wuHasBranch(t, root, "story/US-1") {
		t.Error("the branch start cut was left behind")
	}
}

// ---------------------------------------------------------------- AC-4, T6: RemoveWorktree's refusals

// The rollback is handed a path and a branch, and a wrong one must not become
// a deletion: each refusal leaves everything where it was.
func TestAC4AndT6RemoveWorktreeRefusesWhatStartDidNotMake(t *testing.T) {
	root, _, tip := wtTrunk(t)
	dir := wtStoryDir(root, "US-1")
	if err := AddWorktree(t.Context(), root, dir, "story/US-1", "main"); err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(root, "notes")
	write(t, plain, "keep.md", "a person's notes\n")

	for _, tc := range []struct {
		name, dir, branch, says string
	}{
		{"T6 an empty branch", dir, "", "cannot name a branch"},
		{"T6 a branch that is an option", dir, "--force", "cannot name a branch"},
		{"T6 a directory git does not list", plain, "story/US-1", "is not a worktree git lists"},
		{"T6 a worktree on another branch", dir, "main", `does not have "main" checked out`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RemoveWorktree(t.Context(), root, tc.dir, tc.branch)
			wuSays(t, wuRefusal(t, err, "RemoveWorktree("+tc.dir+", "+tc.branch+")"), tc.says)
		})
	}
	if got := wtList(t, root); len(got) != 2 {
		t.Errorf("git knows these worktrees after the refusals: %v", got)
	}
	for _, branch := range []string{"main", "story/US-1"} {
		if got := wtGitOut(t, root, "rev-parse", "refs/heads/"+branch); got != tip {
			t.Errorf("%s is at %s after the refusals, want %s", branch, got, tip)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(plain, "keep.md")); err != nil || string(raw) != "a person's notes\n" {
		t.Errorf("a directory git does not list lost its file: %q, %v", raw, err)
	}

	if err := RemoveWorktree(t.Context(), t.TempDir(), dir, "story/US-1"); err == nil {
		t.Error("RemoveWorktree in a directory that is no repository did something")
	}
}

// A worktree git will not remove, and a branch git will not delete, are each
// said, with what is still there.
func TestAC4RemoveWorktreeSaysWhatItCouldNotTakeAway(t *testing.T) {
	t.Run("AC-4 a locked worktree", func(t *testing.T) {
		root, _, _ := wtTrunk(t)
		dir := wtStoryDir(root, "US-1")
		if err := AddWorktree(t.Context(), root, dir, "story/US-1", "main"); err != nil {
			t.Fatal(err)
		}
		run(t, root, "worktree", "lock", dir)

		err := RemoveWorktree(t.Context(), root, dir, "story/US-1")
		wuSays(t, wuRefusal(t, err, "RemoveWorktree of a locked worktree"),
			"could not be taken away", `the branch "story/US-1" are still there`)
		if got := wtList(t, root); len(got) != 2 {
			t.Errorf("git knows these worktrees: %v, want the locked one kept", got)
		}
		if !wuHasBranch(t, root, "story/US-1") {
			t.Error("the branch went, with its worktree still there")
		}
	})
	t.Run("AC-4 a branch another git holds the lock of", func(t *testing.T) {
		root, _, _ := wtTrunk(t)
		wuFilesRefs(t, root)
		dir := wtStoryDir(root, "US-1")
		if err := AddWorktree(t.Context(), root, dir, "story/US-1", "main"); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(root, ".git", "refs", "heads", "story"), "US-1.lock", "")

		err := RemoveWorktree(t.Context(), root, dir, "story/US-1")
		wuSays(t, wuRefusal(t, err, "RemoveWorktree of a branch another git holds"),
			"could not be deleted", "the worktree was taken away, and the branch is still there")
		if !wuHasBranch(t, root, "story/US-1") {
			t.Error("the branch is gone, and the refusal says it is still there")
		}
		if got := wtList(t, root); len(got) != 1 {
			t.Errorf("git knows these worktrees: %v", got)
		}
	})
}

// ---------------------------------------------------------------- readers

// A branch holding commits trunk does not have is a story's work, which start
// and doctor never tell anybody to remove.
func TestAheadCountsTheCommitsABranchHasThatTrunkDoesNot(t *testing.T) {
	ctx := t.Context()
	root, first, tip := wtTrunk(t)
	run(t, root, "branch", "story/US-1", tip)
	run(t, root, "branch", "story/US-2", first)
	if got, err := Ahead(ctx, root, "story/US-1", "main"); err != nil || got != 0 {
		t.Errorf("Ahead of a branch at trunk's tip = %d, %v; want 0", got, err)
	}
	if got, err := Ahead(ctx, root, "story/US-2", "main"); err != nil || got != 0 {
		t.Errorf("Ahead of a branch trunk has moved past = %d, %v; want 0", got, err)
	}
	// HEAD is on feature, one commit past first.
	if got, err := Ahead(ctx, root, "feature", "main"); err != nil || got != 1 {
		t.Errorf("Ahead of a branch with one commit of its own = %d, %v; want 1", got, err)
	}
	commitFile(t, root, "more.txt", "and another")
	if got, err := Ahead(ctx, root, "feature", "main"); err != nil || got != 2 {
		t.Errorf("Ahead of a branch with two commits of its own = %d, %v; want 2", got, err)
	}

	for _, tc := range []struct{ branch, trunk string }{
		{"", "main"}, {"story/US-1", ""}, {"--all", "main"}, {"story/US-1", "-x"},
	} {
		if _, err := Ahead(ctx, root, tc.branch, tc.trunk); err == nil {
			t.Errorf("Ahead(%q, %q) counted", tc.branch, tc.trunk)
		} else {
			wantUnreadable(t, err, fmt.Sprintf("Ahead(%q, %q)", tc.branch, tc.trunk))
		}
	}
	if _, err := Ahead(ctx, root, "story/nobody-made", "main"); err == nil {
		t.Error("Ahead of a branch the repository does not have counted")
	}
}

// What a new worktree cut from trunk has is what trunk commits, not what the
// checkout has on disk.
func TestHoldsIsWhetherACommitHasAFile(t *testing.T) {
	ctx := t.Context()
	root, _, _ := wtTrunk(t)
	write(t, root, "only-on-disk.txt", "not committed\n")
	for _, tc := range []struct {
		rev, path string
		want      bool
	}{
		{"refs/heads/main", "invoice.go", true},
		{"refs/heads/main", "notes.txt", true},
		{"refs/heads/main", "feature.txt", false},
		{"refs/heads/feature", "feature.txt", true},
		{"refs/heads/main", "only-on-disk.txt", false},
		{"refs/heads/main", ".sdlc/config.json", false},
	} {
		if got, err := Holds(ctx, root, tc.rev, tc.path); err != nil || got != tc.want {
			t.Errorf("Holds(%s, %s) = %v, %v; want %v", tc.rev, tc.path, got, err, tc.want)
		}
	}
	if _, err := Holds(ctx, root, "refs/heads/nobody-made", "invoice.go"); err == nil {
		t.Error("Holds answered for a branch the repository does not have")
	}
}

// Version is the git doctor holds to the oldest one the worktree commands
// work with.
func TestVersionIsTheMajorAndMinorVersionOfTheGitOnPath(t *testing.T) {
	major, minor, err := Version(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	said := wtGitOut(t, "", "version")
	if major < 2 || !strings.Contains(said, fmt.Sprintf(" %d.%d.", major, minor)) {
		t.Errorf("Version = %d.%d, and git says %q", major, minor, said)
	}
	if MinimumVersion != [2]int{2, 17} {
		t.Errorf("MinimumVersion = %v, want 2.17, which has git worktree remove", MinimumVersion)
	}
}

func TestVersionReadsWhatEachBuildOfGitPrints(t *testing.T) {
	for _, tc := range []struct {
		name, says   string
		major, minor int
	}{
		{"an old git", "git version 2.16.3", 2, 16},
		{"Apple's", "git version 2.39.5 (Apple Git-154)", 2, 39},
		{"Windows'", "git version 2.45.1.windows.1", 2, 45},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wuFakeGit(t, tc.says)
			major, minor, err := Version(t.Context())
			if err != nil || major != tc.major || minor != tc.minor {
				t.Errorf("git saying %q reads as %d.%d, %v; want %d.%d", tc.says, major, minor, err, tc.major, tc.minor)
			}
		})
	}
	t.Run("no version", func(t *testing.T) {
		wuFakeGit(t, "git version unknown")
		_, _, err := Version(t.Context())
		wantUnreadable(t, err, "Version from a git that names no version")
		var e *sdlcerr.Error
		if errors.As(err, &e) && !strings.Contains(e.Why, "git version unknown") {
			t.Errorf("why = %q, want what git said", e.Why)
		}
	})
	t.Run("no git", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, _, err := Version(t.Context()); err == nil {
			t.Error("Version answered with no git on PATH")
		}
	})
}

// Worktrees is what doctor reads each story worktree from: where it is, its
// branch without refs/heads/, and whether its directory is gone.
func TestWorktreesListsEveryCheckoutWithItsBranchAndWhetherItIsGone(t *testing.T) {
	ctx := t.Context()
	root, first, _ := wtTrunk(t)
	story, detached, gone := wtStoryDir(root, "US-1"), wtStoryDir(root, "US-2"), wtStoryDir(root, "US-3")
	if err := AddWorktree(ctx, root, story, "story/US-1", "main"); err != nil {
		t.Fatal(err)
	}
	run(t, root, "worktree", "add", "--quiet", "--detach", detached, first)
	if err := AddWorktree(ctx, root, gone, "story/US-3", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	got, err := Worktrees(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("Worktrees = %+v, want the main checkout and three", got)
	}
	want := []Worktree{
		{Path: root, Branch: "feature"},
		{Path: story, Branch: "story/US-1"},
		{Path: detached},
		{Path: gone, Branch: "story/US-3", Gone: true},
	}
	for i, w := range want {
		if !sameDir(t, got[i].Path, w.Path) || got[i].Branch != w.Branch || got[i].Gone != w.Gone {
			t.Errorf("Worktrees()[%d] = %+v, want %+v", i, got[i], w)
		}
	}

	if _, err := Worktrees(ctx, t.TempDir()); err == nil {
		t.Error("Worktrees listed a directory that is no repository")
	}
}

// ---------------------------------------------------------------- Behind, Branch

// A remote or trunk that git would read as an option is refused before git
// sees it, and a remote that cannot be fetched is a failure, counted from a
// trunk the repository does have.
func TestAC3BehindRefusesAnOptionAndFailsWhereTheRemoteCannotBeFetched(t *testing.T) {
	ctx := t.Context()
	root := repo(t)
	run(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	commit(t, root, "first")
	for _, tc := range []struct{ remote, branch string }{
		{"", "main"}, {"origin", ""}, {"--upload-pack=touch x", "main"}, {"origin", "-x"},
	} {
		_, err := Behind(ctx, root, tc.remote, tc.branch)
		wantUnreadable(t, err, fmt.Sprintf("Behind(%q, %q)", tc.remote, tc.branch))
	}
	if _, err := os.Stat(filepath.Join(root, "x")); err == nil {
		t.Error("an option given as the remote was run")
	}

	_, err := Behind(ctx, root, filepath.Join(t.TempDir(), "no-remote-here"), "main")
	wantUnreadable(t, err, "Behind against a remote that is not a repository")
}

// HEAD on something that is not a branch is on no branch.
func TestBranchIsNothingWhereHeadIsNotOnABranch(t *testing.T) {
	root := repo(t)
	run(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	commit(t, root, "first")
	run(t, root, "update-ref", "refs/tags/release", "HEAD")
	if !wtGitFails(t, root, "symbolic-ref", "HEAD", "refs/tags/release") {
		if got, err := Branch(t.Context(), root); err != nil || got != "" {
			t.Errorf("with HEAD on a tag, Branch = %q, %v; want no branch", got, err)
		}
	}
}

// ---------------------------------------------------------------- commits

// A revision git would read as an option, or none at all, is refused before
// git sees it.
func TestCommitsBetweenRefusesAnEmptyRevisionOrAnOption(t *testing.T) {
	root := repo(t)
	tip := commit(t, root, "first")
	for _, tc := range []struct{ base, end string }{{"", tip}, {tip, ""}, {"--all", tip}, {tip, "-n1"}} {
		if got, err := CommitsBetween(t.Context(), root, tc.base, tc.end); err == nil {
			t.Errorf("CommitsBetween(%q, %q) = %v, want a refusal", tc.base, tc.end, got)
		} else {
			wantUnreadable(t, err, fmt.Sprintf("CommitsBetween(%q, %q)", tc.base, tc.end))
		}
	}
}
