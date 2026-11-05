package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// WorktreesUpTo lists the checkouts git records under the common directory, each read
// from its gitdir file, absolute or relative to its entry; an entry with no gitdir, or
// an empty one, names no checkout, and a limit stops the list early.
func TestWorktreesUpToReadsEachGitdirAndStopsAtTheLimit(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	common := filepath.Join(root, "repo", ".git")
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(common, "worktrees", rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{filepath.Join(root, "wt1"), filepath.Join(root, "repo", "wt4")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("w1/gitdir", filepath.Join(root, "wt1", ".git")+"\n")
	write("w2/gitdir", "  \n")
	write("w3/locked", "")
	write("w4/gitdir", "../../../wt4/.git")

	c := Checkout{Common: common}
	want := []string{filepath.Join(root, "wt1"), filepath.Join(root, "repo", "wt4")}
	if got := c.WorktreesUpTo(0); !slices.Equal(got, want) {
		t.Errorf("WorktreesUpTo(0) = %q, want %q", got, want)
	}
	if got := c.WorktreesUpTo(1); !slices.Equal(got, want[:1]) {
		t.Errorf("WorktreesUpTo(1) = %q, want %q", got, want[:1])
	}
	if got := (Checkout{}).WorktreesUpTo(0); got != nil {
		t.Errorf("WorktreesUpTo with no common directory = %q, want none", got)
	}
	if got := (Checkout{Common: filepath.Join(root, "nowhere")}).WorktreesUpTo(0); got != nil {
		t.Errorf("WorktreesUpTo with no worktrees directory = %q, want none", got)
	}
}
