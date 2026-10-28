package store

// US-029.2: the worktree `sdlc start` makes sits inside the main checkout, at
// .sdlc/worktrees/<ID>, on a branch story/<ID>. Nested there, it is still a
// checkout of its own: it holds its story and binds its session, while the
// backlog, the records, the claims and the lock stay the main checkout's
// (the analysis gate's seventh question). The worktree here is made with git,
// as start makes it, so these hold the store to the layout and not to how
// start gets there.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
)

// swNested is a main checkout with a story worktree for US-001 inside it.
func swNested(t *testing.T) (main, wt string) {
	t.Helper()
	main = clTempDir(t)
	initGit(t, main)
	clBacklog(t, main, map[string]model.Status{"US-001": model.StatusInProgress})
	commitAll(t, main, "the project")
	wt = filepath.Join(main, ".sdlc", "worktrees", "US-001")
	runGit(t, main, "worktree", "add", "--quiet", "-b", "story/US-001", wt)
	return main, wt
}

func TestAC1AStoryWorktreeInsideTheMainCheckoutSharesTheMainCheckoutsBacklogRecordsAndLock(t *testing.T) {
	t.Parallel()
	main, wt := swNested(t)
	s := clStore(wt)
	if err := s.SharedErr(); err != nil {
		t.Fatal(err)
	}
	dir, err := s.StoryDir("US-001")
	if err != nil {
		t.Fatal(err)
	}
	for got, want := range map[string]string{
		s.CheckoutPath():         wt,
		s.BacklogPath():          filepath.Join(main, "user_stories.json"),
		s.LockRoot():             main,
		dir:                      filepath.Join(main, ".sdlc", "stories", "US-001"),
		clStore(main).LockRoot(): main,
	} {
		if !config.SamePath(got, want) {
			t.Errorf("got %s, want %s", got, want)
		}
	}
	if co, err := s.Checkout(); err != nil || !config.SamePath(co.Main, main) || !config.SamePath(co.Root, wt) {
		t.Errorf("Checkout() = %+v, %v; want %s in the main checkout %s", co, err, wt, main)
	}
	// Nested is not the same as being the main checkout: the main checkout
	// does not read the worktree's state as its own.
	clState(t, wt, "US-001", "sess-wt")
	if active, err := clStore(main).Active(); err != nil || active != "" {
		t.Errorf("the main checkout's active reads %q, %v; the story is the worktree's", active, err)
	}
}

func TestAC8AStoryWorktreeInsideTheMainCheckoutHoldsItsStoryAndItsSession(t *testing.T) {
	t.Parallel()
	main, wt := swNested(t)
	clState(t, wt, "US-001", "sess-wt")
	if err := clStore(wt).Claim("US-001"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(clClaimFile(main, "US-001"))
	if err != nil {
		t.Fatal(err)
	}
	var c claim
	if err := json.Unmarshal(raw, &c); err != nil || c.Story != "US-001" || !config.SamePath(c.Checkout, wt) {
		t.Errorf("the claim is %s, want US-001 held by %s", raw, wt)
	}

	fromMain := clHolders(t, clStore(main))["US-001"]
	if !config.SamePath(fromMain.Checkout, wt) || !fromMain.Live || fromMain.Here || !fromMain.Claimed {
		t.Errorf("from the main checkout, US-001 is %+v; want held, live, by %s", fromMain, wt)
	}
	fromWorktree := clHolders(t, clStore(wt))["US-001"]
	if !config.SamePath(fromWorktree.Checkout, wt) || !fromWorktree.Here {
		t.Errorf("from the worktree, US-001 is %+v; want held here", fromWorktree)
	}
	if dir, story := clStore(main).SessionHolder("sess-wt", 0); !config.SamePath(dir, wt) || story != "US-001" {
		t.Errorf("SessionHolder from the main checkout = %q, %q; want %s holding US-001", dir, story, wt)
	}
	if s := clStore(main); !s.ShowsHolders(clHolders(t, s)) || !s.HasLinkedWorktree() {
		t.Error("a repository with a story worktree does not name the checkout holding each story")
	}
}
