package gitx

// US-129 AC-4: an answer this process reuses is dropped when the process
// itself changes what the answer depends on. Each row asks, changes the
// repository through this package as a command does, and asks again in the
// same process, with nothing in between: HEAD after a commit and a
// fast-forward, the list of worktrees after one is added and after it is
// removed. They pass at the base, which keeps no answer; they are what a
// reuse layer in this package has to keep passing.

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func us129Git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=Test"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func us129Repository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	us129Git(t, root, "init", "--quiet", "--initial-branch=main")
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	us129Git(t, root, "add", "-A")
	us129Git(t, root, "commit", "--quiet", "-m", "first")
	return root
}

func TestAC4HEADReadAfterTheProcesssOwnCommitAndFastForwardOfTrunkIsTheNewCommit(t *testing.T) {
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	ctx := t.Context()
	root := us129Repository(t)
	before, err := Head(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	next, err := CommitFile(ctx, root, before, "b.txt", []byte("b\n"), "second")
	if err != nil {
		t.Fatal(err)
	}
	if err := FastForward(ctx, root, "main", before, next); err != nil {
		t.Fatal(err)
	}
	if got, err := Head(ctx, root); err != nil || got != next {
		t.Errorf("AC-4 HEAD after the process's own commit and fast-forward is %q (%v), want the new commit %s; before it was %s",
			got, err, next, before)
	}
	if got, err := Branch(ctx, root); err != nil || got != "main" {
		t.Errorf("AC-4 the checkout is on %q (%v) after the fast-forward, want main, which moved", got, err)
	}
}

func TestAC4TheWorktreesListedAfterTheProcessAddsOrRemovesOneAreTheNewList(t *testing.T) {
	ctx := t.Context()
	root := us129Repository(t)
	paths := func() []string {
		t.Helper()
		list, err := Worktrees(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, w := range list {
			out = append(out, filepath.Clean(w.Path))
		}
		return out
	}
	if got := paths(); len(got) != 1 {
		t.Fatalf("a new repository lists %v, want its one checkout", got)
	}
	dir := filepath.Join(root, ".sdlc", "worktrees", "US-001")
	if err := AddWorktree(ctx, root, dir, "story/US-001", "main"); err != nil {
		t.Fatal(err)
	}
	if got := paths(); !slices.Contains(got, dir) {
		t.Errorf("AC-4 after the process added %s, the worktrees listed are %v", dir, got)
	}
	if err := RemoveWorktree(ctx, root, dir, "story/US-001"); err != nil {
		t.Fatal(err)
	}
	if got := paths(); slices.Contains(got, dir) {
		t.Errorf("AC-4 after the process removed %s, the worktrees listed still hold it: %v", dir, got)
	}
}
