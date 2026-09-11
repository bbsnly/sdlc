package store

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Every test here points the lock at a directory of its own, because the real
// one is keyed on the project path and shared by every process on the machine.
func cacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir) // Linux
	t.Setenv("HOME", dir)           // macOS
	t.Setenv("LocalAppData", dir)   // Windows
	return dir
}

func TestOnlyOneHolderAtATime(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()

	first, err := Lock(root, "gate")
	if err != nil {
		t.Fatal(err)
	}

	// A second attempt has to wait, so it is given a moment and then the
	// first one lets go. That it eventually succeeds is the whole contract:
	// waiting, rather than carrying on and losing somebody's write.
	got := make(chan error, 1)
	go func() {
		g, err := Lock(root, "review add")
		if g != nil {
			g.Release()
		}
		got <- err
	}()

	select {
	case err := <-got:
		t.Fatalf("the lock was handed out twice: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	first.Release()
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("the second command never got the lock: %v", err)
		}
	case <-time.After(guardWait):
		t.Fatal("the lock was never handed on")
	}
}

// Two projects are two locks. A developer with several repositories open would
// otherwise find one of them waiting on the other for no reason.
func TestTwoProjectsDoNotWaitOnEachOther(t *testing.T) {
	cacheDir(t)
	one, err := Lock(t.TempDir(), "gate")
	if err != nil {
		t.Fatal(err)
	}
	defer one.Release()

	two, err := Lock(t.TempDir(), "gate")
	if err != nil {
		t.Fatalf("a second project had to wait for the first: %v", err)
	}
	two.Release()
}

// A process killed while holding the lock must not wedge the project. Telling
// somebody to delete a directory by hand is not a recovery story.
func TestALockLeftByADeadProcessIsBrokenOpen(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()

	abandoned, err := Lock(root, "gate")
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * guardStale)
	if err := os.Chtimes(abandoned.path, old, old); err != nil {
		t.Fatal(err)
	}

	g, err := Lock(root, "start")
	if err != nil {
		t.Fatalf("a lock older than %s was never broken open: %v", guardStale, err)
	}
	g.Release()
}

// Nothing to do with the repository: the lock is a mutex between processes,
// and the commit gate refuses while anything in the tree is uncommitted --
// which is exactly when the lock is held.
func TestTheLockIsNotInTheProject(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()

	g, err := Lock(root, "gate")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Release()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		t.Errorf("holding the lock put %s in the project", e.Name())
	}
}

func TestReleasingTwiceIsHarmless(t *testing.T) {
	cacheDir(t)
	g, err := Lock(t.TempDir(), "gate")
	if err != nil {
		t.Fatal(err)
	}
	g.Release()
	g.Release()
	var none *Guard
	none.Release()
}

// The thing the lock exists for, in one process: many goroutines doing
// read-change-write, and every change surviving.
func TestNoChangeIsLostUnderContention(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()
	counter := filepath.Join(root, "count")
	if err := os.WriteFile(counter, []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}

	const writers = 12
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, err := Lock(root, "gate")
			if err != nil {
				t.Error(err)
				return
			}
			defer g.Release()
			raw, err := os.ReadFile(counter)
			if err != nil {
				t.Error(err)
				return
			}
			n := len(raw)
			// A deliberately slow read-change-write: without the lock the
			// interleaving is what loses the update.
			time.Sleep(time.Millisecond)
			if err := os.WriteFile(counter, make([]byte, n+1), 0o644); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	raw, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 1+writers {
		t.Errorf("%d of %d changes survived", len(raw)-1, writers)
	}
}
