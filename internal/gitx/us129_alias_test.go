package gitx

// US-129 AC-2 for a HEAD that names a ref the files cannot vouch for. Head and
// Branch read the checkout's own HEAD file, and took the branch from its ref:
// line without asking what that ref was. `git checkout alias`, where
// refs/heads/alias is a symbolic ref to main, writes HEAD as
// `ref: refs/heads/alias`, and git says the checkout is on main; the file read
// said alias, and `start` refused as not on trunk. A loose branch file that
// holds no hash made git fail on HEAD altogether, where the file read said no
// commits (US-129 verification, round 6). Each answer is the base's git
// calls', with the invocation's memo and without it.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAC2HeadOnABranchAliasIsOnTheBranchItPointsAt(t *testing.T) {
	root := t.TempDir()
	us129Run(t, root, nil, "", "init", "--quiet", "--initial-branch=main")
	us129Write(t, root, "a.txt", "a\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "one")
	commit := strings.TrimSpace(us129Run(t, root, nil, "", "rev-parse", "HEAD"))
	us129Run(t, root, nil, "", "symbolic-ref", "refs/heads/alias", "refs/heads/main")
	us129Run(t, root, nil, "", "checkout", "--quiet", "alias")

	wantBranch, branchOK := us129BaseBranch(t, root)
	wantHead, headOK := us129BaseHead(t, root)
	if !branchOK || wantBranch != "main" || !headOK || wantHead != commit {
		t.Fatalf("git gives branch %q (%v) and head %q (%v), want main and %q", wantBranch, branchOK, wantHead, headOK, commit)
	}
	for _, memo := range []bool{false, true} {
		name := "AC-2 without a memo"
		ctx := context.Background()
		if memo {
			name = "AC-2 with a memo"
			ctx = WithMemo(ctx)
		}
		t.Run(name, func(t *testing.T) {
			defer Close(ctx)
			branch, err := Branch(ctx, root)
			if err != nil || branch != wantBranch {
				t.Errorf("AC-2 Branch is %q (%v), git says %q", branch, err, wantBranch)
			}
			head, err := Head(ctx, root)
			if err != nil || head != wantHead {
				t.Errorf("AC-2 Head is %q (%v), git says %q", head, err, wantHead)
			}
		})
	}
}

func TestAC2HeadOnABranchFileHoldingNoHashFailsAsGitDoes(t *testing.T) {
	root := t.TempDir()
	us129Run(t, root, nil, "", "init", "--quiet", "--initial-branch=main")
	us129Write(t, root, "a.txt", "a\n")
	us129Run(t, root, nil, "", "add", "-A")
	us129Run(t, root, nil, "", "commit", "--quiet", "-m", "one")
	gitDir := filepath.Join(root, ".git")
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "refs", "heads", "broken"), []byte("not a hash\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The premise: git itself fails on this HEAD, so the base's Branch and
	// Head returned its error and never NoCommits.
	if _, ok := us129BaseBranch(t, root); ok {
		t.Fatal("git answers Branch for a branch file holding no hash; the row's premise is gone")
	}
	if _, ok := us129BaseHead(t, root); ok {
		t.Fatal("git answers Head for a branch file holding no hash; the row's premise is gone")
	}
	for _, memo := range []bool{false, true} {
		name := "AC-2 without a memo"
		ctx := context.Background()
		if memo {
			name = "AC-2 with a memo"
			ctx = WithMemo(ctx)
		}
		t.Run(name, func(t *testing.T) {
			defer Close(ctx)
			branch, err := Branch(ctx, root)
			if err == nil {
				t.Errorf("AC-2 Branch is %q with no error, git fails on this HEAD", branch)
			}
			head, err := Head(ctx, root)
			if err == nil {
				t.Errorf("AC-2 Head is %q with no error, git fails on this HEAD", head)
			}
			if head == NoCommits {
				t.Errorf("AC-2 Head says no commits for a branch file that holds no hash")
			}
		})
	}
}
