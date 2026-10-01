package gitx

// US-029.3: a finished story lands on trunk by fast-forward only, and its
// worktree is cleaned up. These hold the git half of it, which is the only
// place a repository is changed (the contract): moving trunk, reading a branch
// exactly, saying what "the change the reviewers saw" is in a way a rebase
// does not disturb and an implementer cannot game, and taking a landed
// story's worktree and branch away.
//
// The functions these tests hold gitx to:
//
//	FastForward(ctx, root, trunk, from, to string) error
//	Fingerprint(ctx, root, base, tip string, leaveOut ...string) (string, error)
//	BranchTip(ctx, root, branch string) (string, error)
//	RemoveLandedWorktree(ctx, root, dir, branch, landed string) (kept []string, err error)
//
// and the causes FastForward is refused for, which errors.Is finds in what it
// returns: ErrNotFastForward (trunk is not at from, or from is not in to's
// history) and ErrWouldOverwrite (a checkout with trunk checked out has
// uncommitted changes, or untracked files, where the fast-forward writes).
//
// root is any checkout of the repository. FastForward moves refs/heads/<trunk>
// from the commit from, which the caller read, to the commit to, and brings
// the working tree of whichever checkout has trunk checked out along with it;
// where none has, only the ref moves (THREATS T4, T7). Fingerprint is the
// change from base's tree to tip's tree, read from git's objects alone, with
// the paths in leaveOut (slash-separated, repository-relative; a directory
// leaves out everything under it) left out (ANALYSIS 3.2, P1-P6).
// RemoveLandedWorktree takes away the worktree at dir, on branch, and deletes
// branch only while it is at landed; a worktree holding uncommitted or
// untracked files is kept, and those files are returned (AC-7, THREATS T8).
//
// Helpers carry an lg prefix, for landing in gitx.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// lgLines is a Go file of n numbered lines, far enough apart that a change at
// one end of it is out of reach of a diff's context at the other.
func lgLines(n int) []string {
	lines := []string{"package billing", ""}
	for i := 1; i <= n; i++ {
		lines = append(lines, fmt.Sprintf("// line %02d", i))
	}
	return lines
}

// lgInsert is lines with more put in before index at.
func lgInsert(lines []string, at int, more ...string) []string {
	out := slices.Clone(lines[:at])
	out = append(out, more...)
	return append(out, lines[at:]...)
}

func lgFile(lines []string) string { return strings.Join(lines, "\n") + "\n" }

// lgRepo is a repository whose main checkout has trunk, main, checked out, at
// one commit holding billing.go, a.txt and notes.txt.
func lgRepo(t *testing.T) (root, tip string) {
	t.Helper()
	root = repo(t)
	run(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, root, "billing.go", lgFile(lgLines(30)))
	write(t, root, "a.txt", "a\n")
	write(t, root, "notes.txt", "notes\n")
	return root, lgCommit(t, root, "trunk")
}

// lgCommit commits everything in dir, whatever it has checked out, and returns
// the commit.
func lgCommit(t *testing.T, dir, message string) string {
	t.Helper()
	run(t, dir, "add", "-A")
	run(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "--allow-empty", "-m", message)
	return lgRev(t, dir, "HEAD")
}

// lgRev is the commit rev names in dir's repository.
func lgRev(t *testing.T, dir, rev string) string {
	t.Helper()
	return wtGitOut(t, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
}

// lgTempDir is a directory for the test, by the name git prints for it.
func lgTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

// lgStory adds a linked worktree on a new branch cut from main's tip, outside
// the main checkout, and returns it.
func lgStory(t *testing.T, root, branch string) string {
	t.Helper()
	dir := filepath.Join(lgTempDir(t), "story")
	run(t, root, "worktree", "add", "--quiet", "-b", branch, dir, "main")
	return dir
}

// lgStatus is `git status --porcelain` in dir, as git wrote it.
func lgStatus(t *testing.T, dir string) string {
	t.Helper()
	out, err := wtExec(t, dir, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		t.Fatalf("git status in %s: %v", dir, err)
	}
	return string(out)
}

// lgRead is the file at rel under dir, and "" where there is none.
func lgRead(t *testing.T, dir, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(raw)
}

// lgRefs is every ref of the repository with the object it names.
func lgRefs(t *testing.T, dir string) string {
	t.Helper()
	return wtGitOut(t, dir, "for-each-ref", "--format=%(refname) %(objectname)")
}

// lgSays holds err to being an sdlcerr.Error that names each of want in what
// it says, for a person to read.
func lgSays(t *testing.T, err error, want ...string) {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("%v is not an sdlc error a person can read", err)
	}
	for _, w := range want {
		if !strings.Contains(e.What+" "+e.Why, w) {
			t.Errorf("the refusal says %q / %q, and does not name %q", e.What, e.Why, w)
		}
	}
}

// lgStoryWork is a story branch in its own worktree with two commits on it:
// billing.go changed and invoice.go added, then a.txt changed. It returns the
// worktree and the story's tip.
func lgStoryWork(t *testing.T, root string) (wt, tip string) {
	t.Helper()
	wt = lgStory(t, root, "story/US-001")
	write(t, wt, "billing.go", lgFile(lgInsert(lgLines(30), 26, "// story line A", "// story line B")))
	write(t, wt, "invoice.go", "package billing\n")
	lgCommit(t, wt, "US-001: invoices")
	write(t, wt, "a.txt", "a, changed by the story\n")
	return wt, lgCommit(t, wt, "US-001: a")
}

// ---------------------------------------------------------------- FastForward

// AC-1: trunk moves to the story's tip, and the checkout that has trunk
// checked out moves with it, so that the person's working tree shows the
// story's work rather than the story's work reversed (THREATS T1).
func TestAC1FastForwardMovesTrunkAndTheCheckoutThatHasItCheckedOut(t *testing.T) {
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)

	if err := FastForward(t.Context(), root, "main", from, to); err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != to {
		t.Errorf("trunk is at %s, want the story's tip %s", got, to)
	}
	if got := wtGitOut(t, root, "symbolic-ref", "HEAD"); got != "refs/heads/main" {
		t.Errorf("the main checkout is on %s, want refs/heads/main", got)
	}
	if got := lgStatus(t, root); got != "" {
		t.Errorf("the main checkout's working tree is not trunk's after the fast-forward:\n%s", got)
	}
	if got := lgRead(t, root, "invoice.go"); got != "package billing\n" {
		t.Errorf("the main checkout has invoice.go %q, want the story's", got)
	}
	if got := lgRead(t, root, "a.txt"); got != "a, changed by the story\n" {
		t.Errorf("the main checkout has a.txt %q, want the story's", got)
	}
	// AC-8: nothing new was made on trunk, merge commit or otherwise.
	if got := wtGitOut(t, root, "rev-list", "--count", from+"..main"); got != "2" {
		t.Errorf("trunk gained %s commits, want the story's 2 and no more", got)
	}
}

// AC-1 and THREATS T7: where no checkout has trunk checked out, only the ref
// moves, and the checkout that is on another branch is left as it was.
func TestAC1FastForwardWhereNoCheckoutHasTrunkMovesOnlyTheRef(t *testing.T) {
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	run(t, root, "switch", "--quiet", "-c", "spike")
	write(t, root, "spike.txt", "a person's spike\n")
	spike := lgCommit(t, root, "spike")
	write(t, root, "notes.txt", "notes, being edited\n")

	if err := FastForward(t.Context(), root, "main", from, to); err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != to {
		t.Errorf("trunk is at %s, want %s", got, to)
	}
	if got := lgRev(t, root, "HEAD"); got != spike {
		t.Errorf("the checkout on spike moved to %s", got)
	}
	if lgRead(t, root, "invoice.go") != "" || lgRead(t, root, "notes.txt") != "notes, being edited\n" {
		t.Error("the checkout on another branch had its files changed")
	}
}

// AC-8: never a move to a commit that is not a descendant of where trunk was,
// and never a new commit to make one.
func TestAC8FastForwardRefusesACommitThatIsNotADescendantOfTrunk(t *testing.T) {
	root, first := lgRepo(t)
	write(t, root, "notes.txt", "trunk moved on\n")
	lgCommit(t, root, "trunk moves on")
	_, diverged := lgStoryWork(t, root)
	// The story was cut from main's tip; take trunk back to first and on
	// another way, so the story is not on top of it.
	run(t, root, "reset", "--quiet", "--hard", first)
	write(t, root, "other.txt", "another line of work\n")
	from := lgCommit(t, root, "another line of work")

	for _, tc := range []struct{ name, to string }{
		{"AC-8 a commit on another line", diverged},
		{"AC-8 a commit trunk is already past", first},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refs := lgRefs(t, root)
			err := FastForward(t.Context(), root, "main", from, tc.to)
			if !errors.Is(err, ErrNotFastForward) {
				t.Fatalf("FastForward to %s = %v, want ErrNotFastForward", tc.to, err)
			}
			if got := lgRefs(t, root); got != refs {
				t.Errorf("a refused fast-forward changed the refs:\nbefore\n%s\nafter\n%s", refs, got)
			}
			if got := lgStatus(t, root); got != "" {
				t.Errorf("a refused fast-forward changed the main checkout:\n%s", got)
			}
		})
	}
}

// AC-8 and THREATS T4: the move is a compare-and-swap from the commit the
// caller read. Trunk that moved in between is a refusal, never a move onto
// wherever trunk has got to.
func TestAC8AndT4FastForwardRefusesWhenTrunkIsNoLongerWhereItWasRead(t *testing.T) {
	root, read := lgRepo(t)
	_, to := lgStoryWork(t, root)
	write(t, root, "notes.txt", "somebody else landed first\n")
	moved := lgCommit(t, root, "somebody else")

	err := FastForward(t.Context(), root, "main", read, to)
	if !errors.Is(err, ErrNotFastForward) {
		t.Fatalf("FastForward from a trunk that has moved = %v, want ErrNotFastForward", err)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != moved {
		t.Errorf("trunk is at %s, want it left at %s", got, moved)
	}
	if got := lgRead(t, root, "notes.txt"); got != "somebody else landed first\n" {
		t.Errorf("the main checkout's notes.txt is %q", got)
	}
}

// AC-6: uncommitted changes to a file the fast-forward writes, in the checkout
// that has trunk checked out, are a refusal naming the file; trunk and the
// person's changes stay as they were. So is an untracked file where the
// fast-forward puts one, and a change that is only staged (THREATS T7).
func TestAC6FastForwardRefusesOverUncommittedChangesToWhatItWritesAndNamesThem(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, root string)
		named   string
	}{
		{"AC-6 a tracked file changed", func(t *testing.T, root string) {
			write(t, root, "a.txt", "a, a person's edit\n")
		}, "a.txt"},
		{"AC-6 a tracked file changed and staged", func(t *testing.T, root string) {
			write(t, root, "billing.go", "package billing // a person's edit\n")
			run(t, root, "add", "billing.go")
		}, "billing.go"},
		{"T7 an untracked file where the story adds one", func(t *testing.T, root string) {
			write(t, root, "invoice.go", "package billing // a person's own invoice.go\n")
		}, "invoice.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, from := lgRepo(t)
			_, to := lgStoryWork(t, root)
			tc.prepare(t, root)
			status, body := lgStatus(t, root), lgRead(t, root, tc.named)

			err := FastForward(t.Context(), root, "main", from, to)
			if !errors.Is(err, ErrWouldOverwrite) {
				t.Fatalf("FastForward = %v, want ErrWouldOverwrite", err)
			}
			lgSays(t, err, tc.named)
			if got := lgRev(t, root, "refs/heads/main"); got != from {
				t.Errorf("trunk moved to %s", got)
			}
			if got := lgStatus(t, root); got != status {
				t.Errorf("the refused fast-forward changed the main checkout:\nbefore\n%s\nafter\n%s", status, got)
			}
			if got := lgRead(t, root, tc.named); got != body {
				t.Errorf("%s is %q, want the person's %q", tc.named, got, body)
			}
		})
	}
}

// AC-6, the other side: a change to a file the fast-forward does not write is
// not in its way, and is still there afterwards.
func TestAC6FastForwardKeepsUncommittedChangesToWhatItDoesNotWrite(t *testing.T) {
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	write(t, root, "notes.txt", "notes, a person's edit\n")
	write(t, root, "scratch.txt", "a person's untracked file\n")

	if err := FastForward(t.Context(), root, "main", from, to); err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != to {
		t.Errorf("trunk is at %s, want %s", got, to)
	}
	if got := lgRead(t, root, "notes.txt"); got != "notes, a person's edit\n" {
		t.Errorf("notes.txt is %q, want the person's edit kept", got)
	}
	if got := lgRead(t, root, "scratch.txt"); got != "a person's untracked file\n" {
		t.Errorf("scratch.txt is %q, want it kept", got)
	}
	if got := lgRead(t, root, "invoice.go"); got != "package billing\n" {
		t.Errorf("invoice.go is %q, want the story's", got)
	}
}

// THREATS T7: trunk checked out in a linked worktree other than the main
// checkout is held to the same as the main checkout.
func TestT7FastForwardHoldsALinkedWorktreeWithTrunkCheckedOutToTheSame(t *testing.T) {
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	run(t, root, "switch", "--quiet", "-c", "spike")
	trunkTree := filepath.Join(lgTempDir(t), "trunk")
	run(t, root, "worktree", "add", "--quiet", trunkTree, "main")

	write(t, trunkTree, "a.txt", "a, a person's edit in the other checkout\n")
	err := FastForward(t.Context(), root, "main", from, to)
	if !errors.Is(err, ErrWouldOverwrite) {
		t.Fatalf("FastForward over the other checkout's change = %v, want ErrWouldOverwrite", err)
	}
	lgSays(t, err, "a.txt")
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}

	run(t, trunkTree, "checkout", "--quiet", "--", "a.txt")
	if err := FastForward(t.Context(), root, "main", from, to); err != nil {
		t.Fatalf("FastForward with the other checkout clean: %v", err)
	}
	if got := lgStatus(t, trunkTree); got != "" {
		t.Errorf("the checkout with trunk checked out was left behind trunk:\n%s", got)
	}
	if got := lgRead(t, trunkTree, "invoice.go"); got != "package billing\n" {
		t.Errorf("the checkout with trunk checked out has invoice.go %q", got)
	}
}

// AC-8: landing never reaches a remote.
func TestAC8FastForwardNeverPushes(t *testing.T) {
	root, from := lgRepo(t)
	origin := filepath.Join(lgTempDir(t), "origin.git")
	run(t, root, "init", "--quiet", "--bare", origin)
	run(t, root, "remote", "add", "origin", origin)
	run(t, root, "push", "--quiet", "origin", "main")
	run(t, root, "config", "push.default", "current")
	run(t, root, "config", "remote.pushDefault", "origin")
	before := lgRefs(t, origin)
	_, to := lgStoryWork(t, root)

	if err := FastForward(t.Context(), root, "main", from, to); err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	if got := lgRefs(t, origin); got != before {
		t.Errorf("origin changed:\nbefore\n%s\nafter\n%s", before, got)
	}
}

// THREATS T6: the repository's hooks are the session's to write, and none of
// them runs when the tool moves trunk.
func TestT6FastForwardRunsNoHookOfTheRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hooks here are shell scripts marked executable, which Windows does not run as such")
	}
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	hooks, marks := lgTempDir(t), lgTempDir(t)
	for _, name := range []string{"post-merge", "post-checkout", "post-rewrite", "reference-transaction", "pre-push"} {
		body := "#!/bin/sh\necho ran > '" + filepath.ToSlash(filepath.Join(marks, name)) + "'\n"
		if err := os.WriteFile(filepath.Join(hooks, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	run(t, root, "config", "core.hooksPath", filepath.ToSlash(hooks))

	if err := FastForward(t.Context(), root, "main", from, to); err != nil {
		t.Fatalf("FastForward: %v", err)
	}
	ran, err := os.ReadDir(marks)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ran {
		t.Errorf("the repository's %s hook ran on the fast-forward", e.Name())
	}
}

// Every value FastForward hands git has been read by the caller, and one that
// git would read as an option, or none at all, is refused before git sees it.
func TestT10FastForwardRefusesANameOrCommitGitWouldReadAsAnOption(t *testing.T) {
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	refs := lgRefs(t, root)
	for _, tc := range []struct{ trunk, from, to string }{
		{"", from, to},
		{"-main", from, to},
		{"main", "", to},
		{"main", "--all", to},
		{"main", from, ""},
		{"main", from, "--output=" + filepath.Join(lgTempDir(t), "x")},
	} {
		if err := FastForward(t.Context(), root, tc.trunk, tc.from, tc.to); err == nil {
			t.Errorf("FastForward(%q, %q, %q) went ahead", tc.trunk, tc.from, tc.to)
		}
	}
	if got := lgRefs(t, root); got != refs {
		t.Errorf("a refused call changed the refs:\nbefore\n%s\nafter\n%s", refs, got)
	}
}

// ---------------------------------------------------------------- Fingerprint

// lgChanges is a trunk commit, T0, and the story's change made on it in a
// number of ways, each a commit whose parent is T0. The ordinary change adds
// two lines low in billing.go, adds invoice.go and renames notes.txt.
type lgChanges struct {
	root, t0 string
	// story is the ordinary change; the rest differ from it in one way each.
	story, extraLine, trailingSpace, tabbed, movedUp, executable, binary, otherBinary string
}

func lgChangesOn(t *testing.T) lgChanges {
	t.Helper()
	root, _ := lgRepo(t)
	write(t, root, "logo.bin", "\x00\x01\x02binary\x00")
	t0 := lgCommit(t, root, "trunk with a binary file")
	c := lgChanges{root: root, t0: t0}
	lines := lgLines(30)
	variant := func(name string, billing []string, extra func()) string {
		t.Helper()
		run(t, root, "switch", "--quiet", "--detach", t0)
		write(t, root, "billing.go", lgFile(billing))
		write(t, root, "invoice.go", "package billing\n")
		run(t, root, "mv", "notes.txt", "notes.md")
		if extra != nil {
			extra()
		}
		return lgCommit(t, root, name)
	}
	ordinary := lgInsert(lines, 26, "// story line A", "// story line B")
	c.story = variant("the story", ordinary, nil)
	c.extraLine = variant("one more line", lgInsert(lines, 26, "// story line A", "// story line B", "// story line C"), nil)
	c.trailingSpace = variant("a trailing space", lgInsert(lines, 26, "// story line A ", "// story line B"), nil)
	c.tabbed = variant("a tab for a space", lgInsert(lines, 26, "//\tstory line A", "// story line B"), nil)
	c.movedUp = variant("the same lines higher up", lgInsert(lines, 8, "// story line A", "// story line B"), nil)
	c.executable = variant("invoice.go made executable", ordinary, func() {
		// On disk as well as in the index: add -A takes the mode from the
		// file where the file system keeps one.
		if err := os.Chmod(filepath.Join(root, "invoice.go"), 0o755); err != nil {
			t.Fatal(err)
		}
		run(t, root, "add", "invoice.go")
		run(t, root, "update-index", "--chmod=+x", "invoice.go")
	})
	c.binary = variant("a binary change", ordinary, func() { write(t, root, "logo.bin", "\x00\x01\x02binary\x00A") })
	c.otherBinary = variant("another binary change", ordinary, func() { write(t, root, "logo.bin", "\x00\x01\x02binary\x00B") })
	run(t, root, "switch", "--quiet", "main")
	return c
}

func lgFingerprint(t *testing.T, root, base, tip string, leaveOut ...string) string {
	t.Helper()
	fp, err := Fingerprint(t.Context(), root, base, tip, leaveOut...)
	if err != nil {
		t.Fatalf("Fingerprint(%s, %s): %v", base, tip, err)
	}
	if fp == "" {
		t.Fatalf("Fingerprint(%s, %s) is empty", base, tip)
	}
	return fp
}

// AC-4: the same change, rebased onto a trunk that moved, is the change the
// reviewers saw. Trunk changed another file, and a line of billing.go far from
// the story's own, so every line number in the story's hunk moved.
func TestAC4TheSameChangeRebasedOntoAMovedTrunkHasTheSameFingerprint(t *testing.T) {
	root, t0 := lgRepo(t)
	wt := lgStory(t, root, "story/US-001")
	write(t, wt, "billing.go", lgFile(lgInsert(lgLines(30), 26, "// story line A", "// story line B")))
	write(t, wt, "invoice.go", "package billing\n")
	story := lgCommit(t, wt, "US-001")
	reviewed := lgFingerprint(t, root, t0, story)

	write(t, root, "billing.go", lgFile(lgInsert(lgLines(30), 2, "// a header trunk added")))
	write(t, root, "other.go", "package billing\n")
	t1 := lgCommit(t, root, "another story lands")
	run(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "rebase", "--quiet", "main")
	rebased := lgRev(t, wt, "HEAD")
	if rebased == story {
		t.Fatal("precondition: the rebase did not rewrite the story's commit")
	}

	if got := lgFingerprint(t, root, t1, rebased); got != reviewed {
		t.Errorf("the rebased change's fingerprint is %s, want the reviewed %s", got, reviewed)
	}
	if again := lgFingerprint(t, wt, t0, story); again != reviewed {
		t.Errorf("the fingerprint read from the story's worktree is %s, want %s from the main checkout", again, reviewed)
	}
}

// AC-5 and THREATS T3: a change that differs from the reviewed one in any way
// a person would see in the code has another fingerprint -- whitespace, where
// the lines go, a file's mode and binary content included.
func TestAC5AChangeThatDiffersInAnyWayHasAnotherFingerprint(t *testing.T) {
	c := lgChangesOn(t)
	reviewed := lgFingerprint(t, c.root, c.t0, c.story)
	if again := lgFingerprint(t, c.root, c.t0, c.story); again != reviewed {
		t.Fatalf("the same change gave %s and then %s", reviewed, again)
	}
	for _, tc := range []struct{ name, tip string }{
		{"AC-5 one more line", c.extraLine},
		{"AC-5 a trailing space", c.trailingSpace},
		{"AC-5 a tab for a space", c.tabbed},
		{"AC-5 the same lines somewhere else in the file", c.movedUp},
		{"AC-5 a file made executable", c.executable},
		{"AC-5 a binary file changed", c.binary},
		{"AC-5 no change at all", c.t0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := lgFingerprint(t, c.root, c.t0, tc.tip); got == reviewed {
				t.Errorf("the change %q has the reviewed change's fingerprint", tc.name)
			}
		})
	}
	if lgFingerprint(t, c.root, c.t0, c.binary) == lgFingerprint(t, c.root, c.t0, c.otherBinary) {
		t.Error("two different binary changes have one fingerprint")
	}
}

// AC-5, a link and a submodule's commit are content too, set here straight in
// the index so that the test runs where links cannot be made.
func TestAC5ALinksTargetAndASubmodulesCommitArePartOfTheChange(t *testing.T) {
	root, t0 := lgRepo(t)
	blob := func(body string) string {
		t.Helper()
		cmd := wtExec(t, root, "hash-object", "-w", "--stdin")
		cmd.Stdin = strings.NewReader(body)
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(out))
	}
	entry := func(name, mode, object string) string {
		t.Helper()
		run(t, root, "switch", "--quiet", "--detach", t0)
		run(t, root, "update-index", "--add", "--cacheinfo", mode+","+object+","+name)
		run(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", name)
		tip := lgRev(t, root, "HEAD")
		run(t, root, "reset", "--quiet", "--hard", t0)
		return tip
	}
	for _, tc := range []struct{ name, a, b string }{
		{"AC-5 a link", entry("current", "120000", blob("billing.go")), entry("current", "120000", blob("a.txt"))},
		{"AC-5 a submodule", entry("vendor/lib", "160000", strings.Repeat("1", 40)), entry("vendor/lib", "160000", strings.Repeat("2", 40))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.a == tc.b {
				t.Fatal("precondition: the two commits are one")
			}
			if lgFingerprint(t, root, t0, tc.a) == lgFingerprint(t, root, t0, tc.b) {
				t.Errorf("%s pointed at two places has one fingerprint", tc.name)
			}
		})
	}
}

// THREATS T3 and T6: the repository's configuration and attributes are the
// session's to write, and none of them moves the fingerprint of a change or
// makes two changes read as one. Rename detection, prefixes, context,
// algorithm, colour and quoting are the tool's to fix; a textconv driver, an
// external diff or a binary marker would show two changes the same.
func TestT3AndT6TheRepositorysConfigurationDoesNotMoveOrBlurTheFingerprint(t *testing.T) {
	c := lgChangesOn(t)
	reviewed := lgFingerprint(t, c.root, c.t0, c.story)
	other := lgFingerprint(t, c.root, c.t0, c.extraLine)

	for _, kv := range [][2]string{
		{"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"},
		{"diff.srcPrefix", "left/"},
		{"diff.dstPrefix", "right/"},
		{"diff.algorithm", "histogram"},
		{"diff.renames", "false"},
		{"diff.context", "10"},
		{"diff.interHunkContext", "10"},
		{"diff.suppressBlankEmpty", "true"},
		{"diff.indentHeuristic", "false"},
		{"color.ui", "always"},
		{"color.diff", "always"},
		{"core.quotePath", "false"},
		{"diff.external", "true"},
		{"diff.blank.textconv", "true"},
		{"diff.blank.binary", "true"},
	} {
		run(t, c.root, "config", kv[0], kv[1])
	}
	write(t, c.root, ".git/info/attributes", "* diff=blank\n*.go -diff\n")
	t.Setenv("GIT_EXTERNAL_DIFF", "true")

	if got := lgFingerprint(t, c.root, c.t0, c.story); got != reviewed {
		t.Errorf("the repository's configuration moved the fingerprint from %s to %s", reviewed, got)
	}
	if got := lgFingerprint(t, c.root, c.t0, c.extraLine); got != other {
		t.Errorf("the repository's configuration moved another change's fingerprint from %s to %s", other, got)
	}
}

// THREATS T6: git's replacement objects would show the tool one commit, or
// one file, where the ref names another. The fingerprint is of the objects the
// commits really have.
func TestT6TheFingerprintIsOfTheObjectsAndNotOfTheirReplacements(t *testing.T) {
	c := lgChangesOn(t)
	reviewed := lgFingerprint(t, c.root, c.t0, c.story)
	changed := lgFingerprint(t, c.root, c.t0, c.extraLine)

	run(t, c.root, "replace", c.extraLine, c.story)
	if got := lgFingerprint(t, c.root, c.t0, c.extraLine); got != changed {
		t.Errorf("with the changed commit replaced by the reviewed one, its fingerprint became %s (reviewed %s), want %s",
			got, reviewed, changed)
	}
	run(t, c.root, "replace", "-d", c.extraLine)

	blob := func(rev string) string { return wtGitOut(t, c.root, "rev-parse", rev+":billing.go") }
	run(t, c.root, "replace", blob(c.extraLine), blob(c.story))
	if got := lgFingerprint(t, c.root, c.t0, c.extraLine); got != changed {
		t.Errorf("with the changed file replaced by the reviewed one, the fingerprint became %s, want %s", got, changed)
	}
}

// The paths the caller leaves out, and only those: the loop's own directory
// and the bookkeeper's CODEMAP.md at the root, which the retro commits after
// the reviews (the analysis's route R1). A CODEMAP.md anywhere else is code.
func TestAC5TheFingerprintLeavesOutWhatItIsToldToAndNothingElse(t *testing.T) {
	c := lgChangesOn(t)
	leave := []string{".sdlc", "CODEMAP.md"}
	reviewed := lgFingerprint(t, c.root, c.t0, c.story, leave...)
	on := func(files map[string]string) string {
		t.Helper()
		run(t, c.root, "switch", "--quiet", "--detach", c.story)
		for rel, body := range files {
			write(t, c.root, rel, body)
		}
		tip := lgCommit(t, c.root, "after the reviews")
		run(t, c.root, "switch", "--quiet", "main")
		return tip
	}
	bookkeeping := on(map[string]string{
		"CODEMAP.md":             "# CODEMAP\n\nbilling.go keeps invoices.\n",
		".sdlc/config.json":      "{}\n",
		".sdlc/stories/x/doc.md": "prose\n",
	})
	if got := lgFingerprint(t, c.root, c.t0, bookkeeping, leave...); got != reviewed {
		t.Errorf("a change only under what is left out moved the fingerprint to %s, want %s", got, reviewed)
	}
	if got := lgFingerprint(t, c.root, c.t0, bookkeeping); got == lgFingerprint(t, c.root, c.t0, c.story) {
		t.Error("with nothing left out, the bookkeeping is not part of the change")
	}
	nested := on(map[string]string{"docs/CODEMAP.md": "# not the bookkeeper's\n"})
	if got := lgFingerprint(t, c.root, c.t0, nested, leave...); got == reviewed {
		t.Error("a CODEMAP.md below the root was left out with the one at the root")
	}
	sdlcLike := on(map[string]string{".sdlcx/config.json": "{}\n"})
	if got := lgFingerprint(t, c.root, c.t0, sdlcLike, leave...); got == reviewed {
		t.Error(".sdlcx was left out as though it were .sdlc")
	}
}

// P1: the fingerprint is of commits, not of whatever the checkout it is read
// in has uncommitted.
func TestAC5TheFingerprintIsOfTheCommitsNotTheWorkingTree(t *testing.T) {
	c := lgChangesOn(t)
	reviewed := lgFingerprint(t, c.root, c.t0, c.story)
	write(t, c.root, "billing.go", "package billing // uncommitted\n")
	write(t, c.root, "loose.go", "package billing\n")
	if got := lgFingerprint(t, c.root, c.t0, c.story); got != reviewed {
		t.Errorf("uncommitted work moved the fingerprint from %s to %s", reviewed, got)
	}
}

func TestFingerprintRefusesARevisionThatIsNoneOrAnOption(t *testing.T) {
	c := lgChangesOn(t)
	missing := strings.Repeat("0", 39) + "1"
	for _, tc := range []struct{ base, tip string }{
		{"", c.story},
		{c.t0, ""},
		{"--output=" + filepath.Join(lgTempDir(t), "x"), c.story},
		{c.t0, "-R"},
		{missing, c.story},
		{c.t0, missing},
	} {
		if fp, err := Fingerprint(t.Context(), c.root, tc.base, tc.tip); err == nil {
			t.Errorf("Fingerprint(%q, %q) = %q, want a refusal", tc.base, tc.tip, fp)
		}
	}
}

// ---------------------------------------------------------------- BranchTip

// The ref check reads refs/heads/<branch> and nothing else: not a tag that
// shares trunk's name, not an abbreviation git would guess at.
func TestBranchTipIsTheCommitTheBranchIsOnAndNothingElse(t *testing.T) {
	root, first := lgRepo(t)
	write(t, root, "notes.txt", "second\n")
	tip := lgCommit(t, root, "second")
	run(t, root, "tag", "main", first)
	run(t, root, "tag", "v1", first)
	run(t, root, "branch", "story/US-001", first)

	for _, tc := range []struct{ branch, want string }{
		{"main", tip},
		{"story/US-001", first},
	} {
		got, err := BranchTip(t.Context(), root, tc.branch)
		if err != nil || got != tc.want {
			t.Errorf("BranchTip(%q) = %q, %v; want %s", tc.branch, got, err, tc.want)
		}
	}
	for _, name := range []string{"", "-main", "--all", "v1", "HEAD", "story/US-002", "US-001", "heads/main"} {
		if got, err := BranchTip(t.Context(), root, name); err == nil {
			t.Errorf("BranchTip(%q) = %q, want a refusal: it is no branch here", name, got)
		}
	}
}

// ---------------------------------------------------------------- RemoveLandedWorktree

// lgLanded is a story worktree whose work trunk now has, by a fast-forward
// done here with plumbing so that the test does not rest on FastForward.
func lgLanded(t *testing.T) (root, wt, tip string) {
	t.Helper()
	root, _ = lgRepo(t)
	write(t, root, ".gitignore", "/build/\n")
	from := lgCommit(t, root, "ignore build output")
	wt, tip = lgStoryWork(t, root)
	run(t, root, "merge", "--quiet", "--ff-only", tip)
	if got := lgRev(t, root, "refs/heads/main"); got != tip || from == tip {
		t.Fatalf("precondition: trunk is at %s, want the story's tip %s", got, tip)
	}
	return root, wt, tip
}

func lgListed(t *testing.T, root, dir string) bool {
	t.Helper()
	for line := range strings.Lines(wtGitOut(t, root, "worktree", "list", "--porcelain")) {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok && samePath(p, dir) {
			return true
		}
	}
	return false
}

// AC-7: a landed story's worktree goes, and its branch with it, and nothing
// else does. Files git ignores are not somebody's uncommitted work, and do not
// keep it: the loop's own state is among them.
func TestAC7RemoveLandedWorktreeTakesAwayAWorktreeWithNothingUncommittedAndItsBranch(t *testing.T) {
	root, wt, tip := lgLanded(t)
	write(t, wt, "build/out", "build output\n")

	kept, err := RemoveLandedWorktree(t.Context(), root, wt, "story/US-001", tip)
	if err != nil || len(kept) != 0 {
		t.Fatalf("RemoveLandedWorktree = %v, %v; want it removed", kept, err)
	}
	if lgListed(t, root, wt) {
		t.Error("git still lists the worktree")
	}
	if _, err := os.Lstat(wt); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the worktree's directory is still there: %v", err)
	}
	if wuHasBranch(t, root, "story/US-001") {
		t.Error("the story's branch is still there")
	}
	if got := lgRev(t, root, "refs/heads/main"); got != tip {
		t.Errorf("trunk is at %s, want %s", got, tip)
	}
	if got := lgStatus(t, root); got != "" {
		t.Errorf("the main checkout changed:\n%s", got)
	}
}

// AC-7: uncommitted and untracked files keep the worktree, and are said, by
// the name git has for each, however odd.
func TestAC7RemoveLandedWorktreeKeepsAWorktreeHoldingUncommittedOrUntrackedFilesAndSaysWhich(t *testing.T) {
	root, wt, tip := lgLanded(t)
	write(t, wt, "a.txt", "a, changed after the story landed\n")
	write(t, wt, "notes/idea.md", "half a thought\n")
	want := []string{"a.txt", "notes/idea.md"}
	if runtime.GOOS != "windows" {
		write(t, wt, "odd\nname.txt", "a name with a line break\n")
		want = append(want, "odd\nname.txt")
	}

	kept, err := RemoveLandedWorktree(t.Context(), root, wt, "story/US-001", tip)
	if err != nil {
		t.Fatalf("RemoveLandedWorktree: %v", err)
	}
	slices.Sort(kept)
	slices.Sort(want)
	if !slices.Equal(kept, want) {
		t.Errorf("kept for %q, want %q", kept, want)
	}
	if !lgListed(t, root, wt) {
		t.Error("git no longer lists the worktree it kept")
	}
	if got := lgRead(t, wt, "notes/idea.md"); got != "half a thought\n" {
		t.Errorf("the untracked file is %q", got)
	}
	if got := lgRead(t, wt, "a.txt"); got != "a, changed after the story landed\n" {
		t.Errorf("the uncommitted change is %q", got)
	}
}

// THREATS T8: the branch is deleted only at the commit that landed. One that
// moved on holds commits trunk does not, and they are not lost.
func TestT8RemoveLandedWorktreeDeletesTheBranchOnlyAtTheLandedCommit(t *testing.T) {
	root, wt, tip := lgLanded(t)
	write(t, wt, "late.go", "package billing\n")
	late := lgCommit(t, wt, "after the landing")

	if _, err := RemoveLandedWorktree(t.Context(), root, wt, "story/US-001", tip); err == nil {
		t.Error("a worktree whose branch moved past the landed commit was cleaned up without a word")
	}
	if !wuHasBranch(t, root, "story/US-001") || lgRev(t, root, "refs/heads/story/US-001") != late {
		t.Errorf("the branch holding a commit trunk does not have was deleted or moved")
	}
}

// THREATS T8: only the story's own worktree, on its branch, is taken away,
// and never by force.
func TestT8RemoveLandedWorktreeRefusesWhatIsNotALandedStorysWorktree(t *testing.T) {
	t.Run("T8 the main checkout", func(t *testing.T) {
		root, _, tip := lgLanded(t)
		if _, err := RemoveLandedWorktree(t.Context(), root, root, "main", tip); err == nil {
			t.Error("the main checkout was taken for a story's worktree")
		}
		if _, err := os.Stat(filepath.Join(root, "billing.go")); err != nil {
			t.Errorf("the main checkout lost its files: %v", err)
		}
	})
	t.Run("T8 a directory git does not list", func(t *testing.T) {
		root, _, tip := lgLanded(t)
		stray := filepath.Join(lgTempDir(t), "stray")
		write(t, stray, "keep.txt", "somebody's\n")
		if _, err := RemoveLandedWorktree(t.Context(), root, stray, "story/US-001", tip); err == nil {
			t.Error("a directory git does not list was taken for the story's worktree")
		}
		if lgRead(t, stray, "keep.txt") != "somebody's\n" {
			t.Error("the stray directory lost its file")
		}
	})
	t.Run("T8 a worktree on another branch", func(t *testing.T) {
		root, wt, tip := lgLanded(t)
		run(t, wt, "switch", "--quiet", "-c", "renamed")
		if _, err := RemoveLandedWorktree(t.Context(), root, wt, "story/US-001", tip); err == nil {
			t.Error("a worktree on another branch was taken away")
		}
		if !lgListed(t, root, wt) {
			t.Error("git no longer lists it")
		}
	})
	t.Run("T8 a worktree on no branch", func(t *testing.T) {
		root, wt, tip := lgLanded(t)
		run(t, wt, "switch", "--quiet", "--detach")
		if _, err := RemoveLandedWorktree(t.Context(), root, wt, "story/US-001", tip); err == nil {
			t.Error("a worktree on no branch was taken away")
		}
		if !lgListed(t, root, wt) {
			t.Error("git no longer lists it")
		}
	})
	t.Run("T8 a locked worktree", func(t *testing.T) {
		root, wt, tip := lgLanded(t)
		run(t, root, "worktree", "lock", "--reason", "a person is using it", wt)
		if _, err := RemoveLandedWorktree(t.Context(), root, wt, "story/US-001", tip); err == nil {
			t.Error("a locked worktree was cleaned up without a word")
		}
		if !lgListed(t, root, wt) {
			t.Error("a locked worktree was forced away")
		}
		if _, err := os.Stat(wt); err != nil {
			t.Errorf("a locked worktree's directory went: %v", err)
		}
	})
	t.Run("T8 a name or commit git would read as an option", func(t *testing.T) {
		root, wt, tip := lgLanded(t)
		for _, tc := range []struct{ branch, landed string }{
			{"", tip}, {"-f", tip}, {"story/US-001", ""}, {"story/US-001", "--force"},
		} {
			if _, err := RemoveLandedWorktree(t.Context(), root, wt, tc.branch, tc.landed); err == nil {
				t.Errorf("RemoveLandedWorktree(%q, %q) went ahead", tc.branch, tc.landed)
			}
		}
		if !lgListed(t, root, wt) || !wuHasBranch(t, root, "story/US-001") {
			t.Error("a refused call took the worktree or its branch away")
		}
	})
}
