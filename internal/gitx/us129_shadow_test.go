package gitx

// US-129 AC-2 and AC-4 rows from the code review's security notes.
//
// A name that only looks like a branch. `cat-file --batch-command` resolves
// refs/heads/X by git's guessing rules, which also try refs/tags/refs/heads/X
// and refs/heads/refs/heads/X; the base asked `show-ref --verify` and
// `rev-parse`, which, for a branch that is not there, said so. With the
// session, BranchTip of an absent branch gave the shadowing tag's commit, and
// Head on an unborn branch the same: a tag any process can make chose the tip
// the landing reads.
//
// A HEAD that is a symbolic link to refs/heads/main. Git reads it as on main;
// the HEAD file reader followed the link, found a hash and called the checkout
// detached, so start refused as not on trunk.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// us129Both is what f says with no memo and with one, each as us129Said.
func us129Both(f func(ctx context.Context) (any, error)) (plain, memo us129Answer) {
	v, err := f(context.Background())
	plain = us129Said(v, err)
	ctx := WithMemo(context.Background())
	defer Close(ctx)
	v, err = f(ctx)
	return plain, us129Said(v, err)
}

func TestAC2ABranchThatIsNotThereIsNotAnsweredByARefThatShadowsItsName(t *testing.T) {
	root, first, _ := us129Repo(t)
	// ghost: a tag under refs/tags/refs/heads/; spook: a branch under
	// refs/heads/refs/heads/. Neither refs/heads/ghost nor refs/heads/spook
	// is there.
	us129Run(t, root, nil, "", "update-ref", "refs/tags/refs/heads/ghost", first)
	us129Run(t, root, nil, "", "update-ref", "refs/heads/refs/heads/spook", first)
	for _, name := range []string{"ghost", "spook"} {
		t.Run("AC-2 "+name, func(t *testing.T) {
			if _, err := us129Try(t, root, "show-ref", "--verify", "--quiet", "refs/heads/"+name); err == nil {
				t.Fatalf("git has refs/heads/%s", name)
			}
			ask := map[string]func(ctx context.Context) (any, error){
				"BranchTip": func(ctx context.Context) (any, error) { return BranchTip(ctx, root, name) },
				"Resolve": func(ctx context.Context) (any, error) {
					return Resolve(ctx, root, "refs/heads/"+name)
				},
				"Holds": func(ctx context.Context) (any, error) {
					return Holds(ctx, root, "refs/heads/"+name, "a.txt")
				},
				"AddWorktree": func(ctx context.Context) (any, error) {
					dir := filepath.Join(t.TempDir(), "wt")
					err := AddWorktree(ctx, root, dir, name, "main")
					if err == nil {
						// Taken away again, so that the next run of the question
						// finds the branch as absent as this one did.
						if removeErr := RemoveWorktree(ctx, root, dir, name); removeErr != nil {
							t.Errorf("the worktree the question made could not be taken away: %v", removeErr)
						}
					}
					return nil, err
				},
			}
			for what, f := range ask {
				plain, memo := us129Both(f)
				if fmt.Sprint(plain) != fmt.Sprint(memo) {
					t.Errorf("AC-2 %s of the absent branch %s with the memo is %v (%q), with none, as at the base, %v (%q)",
						what, name, memo.value, memo.err, plain.value, plain.err)
				}
			}
			// HEAD on the absent branch: no commits yet, as the base read it.
			headPath := filepath.Join(root, ".git", "HEAD")
			if err := os.WriteFile(headPath, []byte("ref: refs/heads/"+name+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0o644) }()
			for what, f := range map[string]func(ctx context.Context) (any, error){
				"Head":   func(ctx context.Context) (any, error) { return Head(ctx, root) },
				"Branch": func(ctx context.Context) (any, error) { return Branch(ctx, root) },
			} {
				plain, memo := us129Both(f)
				if fmt.Sprint(plain) != fmt.Sprint(memo) {
					t.Errorf("AC-2 %s on the unborn branch %s with the memo is %v (%q), with none, as at the base, %v (%q)",
						what, name, memo.value, memo.err, plain.value, plain.err)
				}
			}
			if head, err := Head(context.Background(), root); err != nil || head != NoCommits {
				t.Errorf("Head on the unborn branch with no memo is %s (%v), want NoCommits", head, err)
			}
		})
	}
}

func TestAC2AHEADThatIsALinkToABranchReadsAsOnThatBranchAsGitReadsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a symbolic link needs a privilege Windows does not give by default, and git there writes HEAD as a file")
	}
	root, _, second := us129Repo(t)
	headPath := filepath.Join(root, ".git", "HEAD")
	if err := os.Remove(headPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("refs", "heads", "main"), headPath); err != nil {
		t.Fatal(err)
	}
	if ref, err := us129Try(t, root, "symbolic-ref", "HEAD"); err != nil || ref != "refs/heads/main" {
		t.Fatalf("git reads the linked HEAD as %q (%v)", ref, err)
	}
	for _, memo := range []bool{false, true} {
		ctx := context.Background()
		if memo {
			ctx = WithMemo(ctx)
		}
		branch, branchErr := Branch(ctx, root)
		head, headErr := Head(ctx, root)
		Close(ctx)
		if branchErr != nil || branch != "main" {
			t.Errorf("AC-2 with HEAD a link to refs/heads/main, memo %v: Branch is %q (%v), git says main", memo, branch, branchErr)
		}
		if headErr != nil || head != second {
			t.Errorf("AC-2 with HEAD a link to refs/heads/main, memo %v: Head is %q (%v), want %s", memo, head, headErr, second)
		}
	}
}
