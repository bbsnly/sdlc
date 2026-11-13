package gitx

// US-129 AC-4, for a name each checkout answers for itself: HEAD belongs to
// one worktree, and `start` run in the main checkout asks Head of main and
// then of the story's worktree in one invocation. One git session for the
// invocation, started where the first question was asked, answers HEAD with
// that checkout's commit for every root, and a wrong HEAD is recorded as the
// story's base or commit (US-129 security review 2, F-9). A branch's tip is
// the same from every checkout, and is asked here beside it so that a reuse
// layer that shares it is held to the same answer. It passes at the base,
// which keeps no answer; like the stdin row, it asks everything in one
// context in one process.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestAC4HEADAskedOfTwoCheckoutsInOneInvocationIsEachCheckoutsOwn(t *testing.T) {
	main := t.TempDir()
	us129Run(t, main, nil, "", "init", "--quiet", "--initial-branch=main")
	us129Write(t, main, "a.txt", "on trunk\n")
	us129Run(t, main, nil, "", "add", "-A")
	us129Run(t, main, nil, "", "commit", "--quiet", "-m", "trunk")
	trunk := strings.TrimSpace(us129Run(t, main, nil, "", "rev-parse", "HEAD"))
	wt := filepath.Join(t.TempDir(), "story")
	us129Run(t, main, nil, "", "worktree", "add", "--quiet", "-b", "story/US-001", wt, "main")
	us129Write(t, wt, "a.txt", "on the story\n")
	us129Run(t, wt, nil, "", "commit", "--quiet", "-a", "-m", "story")
	story := strings.TrimSpace(us129Run(t, wt, nil, "", "rev-parse", "HEAD"))
	if story == trunk {
		t.Fatal("the worktree's commit is trunk's")
	}
	own := map[string]string{main: trunk, wt: story}
	ctx := context.Background()
	for _, order := range [][]string{{main, wt}, {wt, main}, {main, wt}} {
		for _, root := range order {
			got, err := Head(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			if got != own[root] {
				t.Errorf("AC-4 Head of %s, asked in the order %s then %s, is %s, want its own commit %s",
					filepath.Base(root), filepath.Base(order[0]), filepath.Base(order[1]), got, own[root])
			}
		}
	}
	for branch, want := range map[string]string{"main": trunk, "story/US-001": story} {
		for _, root := range []string{main, wt, main} {
			got, err := BranchTip(ctx, root, branch)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Errorf("AC-4 BranchTip of %s asked in %s is %s, want %s from every checkout", branch, filepath.Base(root), got, want)
			}
		}
	}
}
