package store

// US-029.3 (PLAN.md steps 7 and 8): two things the store does for a story
// that lands from its own worktree.
//
//   - EnsureStateIgnored(wt): a story's worktree ignores the loop's own state
//     directory, whatever trunk's .gitignore says, so that what the loop
//     writes there is never a change in the way of the landing, nor one a
//     story commits. It writes the ignore file once, whole, and leaves a
//     tracked file tracked.
//   - LockLanding(id): one stop lands a story at a time. It is taken without
//     waiting, held per story, beside the project's lock, and is among what
//     ReleaseHeld lets go of when a signal ends the command.
//
// Both are asked for through the store's method set -- EnsureStateIgnored(wt
// string) error and LockLanding(id string) (*Guard, error) -- so that the
// rest of this package's tests run before they exist.
//
// Helpers carry an sl prefix.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

type slIgnorer interface {
	EnsureStateIgnored(wt string) error
}

type slLander interface {
	LockLanding(id string) (*Guard, error)
}

func slIgnoring(t *testing.T, s *Store) slIgnorer {
	t.Helper()
	var v any = s
	i, ok := v.(slIgnorer)
	if !ok {
		t.Fatal("the store has no EnsureStateIgnored(wt string) error")
	}
	return i
}

func slLanding(t *testing.T, s *Store) slLander {
	t.Helper()
	var v any = s
	l, ok := v.(slLander)
	if !ok {
		t.Fatal("the store has no LockLanding(id string) (*Guard, error)")
	}
	return l
}

// slStatus is `git status --porcelain` in dir, every untracked file named.
func slStatus(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "status", "--porcelain", "--untracked-files=all")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git status in %s: %v", dir, err)
	}
	return string(out)
}

// PLAN.md step 8: what the loop writes into a story's worktree is not a
// change git sees there, where trunk's .gitignore says nothing about it.
func TestEnsureStateIgnoredKeepsTheLoopsStateOutOfTheWorktreesChanges(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{})
	ignore := slIgnoring(t, clStore(main))
	if err := ignore.EnsureStateIgnored(wt); err != nil {
		t.Fatal(err)
	}
	clWrite(t, filepath.Join(wt, ".sdlc", "state", "session"), "a session\n")
	clWrite(t, filepath.Join(wt, ".sdlc", "state", "reviews", "US-001.json"), "{}\n")

	if got := slStatus(t, wt); got != "" {
		t.Errorf("git sees the loop's state in the story's worktree:\n%s", got)
	}
	entries, err := os.ReadDir(filepath.Join(wt, ".sdlc", "state"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), "tmp") || strings.HasPrefix(e.Name(), ".") && e.Name() != ".gitignore" {
			t.Errorf("%s was left in the state directory", e.Name())
		}
	}
}

// Asked again, it changes nothing, and an ignore file a person wrote there is
// theirs.
func TestEnsureStateIgnoredWritesOnlyWhereThereIsNone(t *testing.T) {
	main, wt := clRepository(t, map[string]model.Status{})
	ignore := slIgnoring(t, clStore(main))
	if err := ignore.EnsureStateIgnored(wt); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(wt, ".sdlc", "state", ".gitignore")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no ignore file in the worktree's state directory: %v", err)
	}
	if err := ignore.EnsureStateIgnored(wt); err != nil {
		t.Fatalf("asked a second time: %v", err)
	}
	if again, _ := os.ReadFile(path); string(again) != string(first) {
		t.Errorf("the second time rewrote the ignore file:\n%s", again)
	}

	_, other := clRepository(t, map[string]model.Status{})
	clWrite(t, filepath.Join(other, ".sdlc", "state", ".gitignore"), "# a person's\n*\n")
	if err := ignore.EnsureStateIgnored(other); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(other, ".sdlc", "state", ".gitignore")); string(got) != "# a person's\n*\n" {
		t.Errorf("a person's ignore file was written over:\n%s", got)
	}
}

// Ignoring never untracks: a file trunk tracks in the state directory is
// still one git sees change, which is what doctor warns of (PLAN.md step 14).
func TestEnsureStateIgnoredLeavesATrackedFileTracked(t *testing.T) {
	main, _ := clRepository(t, map[string]model.Status{})
	clWrite(t, filepath.Join(main, ".sdlc", "state", "tracked.json"), "{}\n")
	commitAll(t, main, "state trunk tracks")
	wt := clWorktree(t, main, "tracking")
	if err := slIgnoring(t, clStore(main)).EnsureStateIgnored(wt); err != nil {
		t.Fatal(err)
	}
	clWrite(t, filepath.Join(wt, ".sdlc", "state", "tracked.json"), "{\"changed\":true}\n")
	if got := slStatus(t, wt); !strings.Contains(got, ".sdlc/state/tracked.json") {
		t.Errorf("a tracked file under the state directory no longer shows as changed:\n%q", got)
	}
}

// PLAN.md step 7, red team O8: a second stop landing the same story is
// refused at once, with SDLC-E0006 naming the story; another story's landing,
// and the project's lock, go on; let go of, it is taken again.
func TestLockLandingHoldsOneLandingOfAStoryAndNothingElse(t *testing.T) {
	cacheDir(t)
	guardWait = time.Hour
	t.Cleanup(func() { guardWait = 10 * time.Second })
	main, wt := clRepository(t, map[string]model.Status{})
	lands := slLanding(t, clStore(main))

	held, err := lands.LockLanding("US-001")
	if err != nil {
		t.Fatal(err)
	}
	type taken struct {
		g   *Guard
		err error
	}
	fromWorktree := slLanding(t, clStore(wt))
	second := make(chan taken, 1)
	go func() {
		g, err := fromWorktree.LockLanding("US-001")
		second <- taken{g, err}
	}()
	select {
	case got := <-second:
		if got.err == nil {
			got.g.Release()
			t.Fatal("a second landing of US-001 took the lock while the first held it")
		}
		e := clError(t, got.err, sdlcerr.StateUnwritable)
		if !strings.Contains(e.What+" "+e.Why, "US-001") {
			t.Errorf("the refusal says %q / %q and does not name US-001", e.What, e.Why)
		}
	case <-time.After(slPatience):
		t.Fatal("the second landing waited for the lock instead of being refused")
	}

	other, err := lands.LockLanding("US-002")
	if err != nil {
		t.Fatalf("another story's landing waited on US-001's: %v", err)
	}
	other.Release()
	guardWait = 10 * time.Second
	project, err := Lock(clStore(main).LockRoot(), "gate")
	if err != nil {
		t.Fatalf("the project's lock waited on a landing: %v", err)
	}
	project.Release()

	held.Release()
	again, err := lands.LockLanding("US-001")
	if err != nil {
		t.Fatalf("US-001's landing, let go of, could not be taken again: %v", err)
	}
	again.Release()
	if got := slStatus(t, main); got != "" {
		t.Errorf("taking the landing lock changed the repository:\n%s", got)
	}
}

// slPatience bounds how long the test waits on a take that should not
// wait at all; a deadline, not a pause.
const slPatience = 20 * time.Second

// Architect 1: the landing lock is one of the guards a signal lets go of, so
// that a stop killed while its commands ran leaves the story landable.
func TestReleaseHeldLetsGoOfALandingLock(t *testing.T) {
	cacheDir(t)
	main, _ := clRepository(t, map[string]model.Status{})
	lands := slLanding(t, clStore(main))
	if _, err := lands.LockLanding("US-001"); err != nil {
		t.Fatal(err)
	}
	ReleaseHeld(0)
	again, err := lands.LockLanding("US-001")
	if err != nil {
		t.Fatalf("after ReleaseHeld, the landing lock could not be taken: %v", err)
	}
	again.Release()
}
