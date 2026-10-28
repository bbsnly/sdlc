package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// Every test here points the lock at a directory of its own, because the real
// one is keyed on the project path and shared by every process on the machine.
func cacheDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir) // Linux
	t.Setenv("HOME", dir)           // macOS
	t.Setenv("LocalAppData", dir)   // Windows
}

// deadPID is the pid of a process that has exited: this test binary, run to do
// nothing. It stays the pid of that process for the rest of the test. Windows
// gives a pid out again as soon as nothing holds the process, and a runner
// busy enough started another under it: the lock it "left" had a holder that
// was running, was never broken open, and 25 commands waited on it and failed.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// cmd holds the process until Wait, so this is still the one it started.
	keepPID(t, cmd.Process.Pid)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	return cmd.ProcessState.Pid()
}

// abandon makes g's lock look like one left by a process that died holding it:
// owned by a pid that is no longer running, and older than guardStale.
func abandon(t *testing.T, g *Guard, pid int) {
	t.Helper()
	raw, err := json.Marshal(guardOwner{PID: pid, What: "gate", Token: g.token})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.path, "owner.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * guardStale)
	if err := os.Chtimes(g.path, old, old); err != nil {
		t.Fatal(err)
	}
}

// On Windows, creating the lock while another holder is still removing it fails
// with "Access is denied". Lock took that for a failure, and the command that
// met the release lost its change: CI caught it as 11 of 12 changes surviving.
func TestALockBeingHandedOnIsWaitedForRatherThanFailed(t *testing.T) {
	cacheDir(t)
	denials := 3
	mkdir = func(path string, perm fs.FileMode) error {
		if denials > 0 {
			denials--
			return &fs.PathError{Op: "mkdir", Path: path, Err: fs.ErrPermission}
		}
		return os.Mkdir(path, perm)
	}
	t.Cleanup(func() { mkdir = os.Mkdir })

	g, err := Lock(t.TempDir(), "gate")
	if err != nil {
		t.Fatalf("a lock that was being released was taken for a failure: %v", err)
	}
	g.Release()
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
	defer abandoned.Release()
	abandon(t, abandoned, deadPID(t))

	g, err := Lock(root, "start")
	if err != nil {
		t.Fatalf("a lock older than %s was never broken open: %v", guardStale, err)
	}
	g.Release()
}

// A lock whose owner names no process is judged by its age: there is no holder
// to wait for.
func TestAnOldLockThatNamesNoHolderIsBrokenOpen(t *testing.T) {
	cacheDir(t)
	abandoned, err := Lock(t.TempDir(), "gate")
	if err != nil {
		t.Fatal(err)
	}
	defer abandoned.Release()
	abandon(t, abandoned, 0)
	if !breakIfStale(abandoned.path) {
		t.Error("an old lock that names no holder was not broken open")
	}
}

// An old lock whose holder is still running is still held. A clock set
// forward, or a laptop asleep mid-command, ages a lock its holder is about to
// write under; broken open then, one of the two commands' changes was lost.
func TestALockIsNotBrokenOpenWhileItsHolderIsRunning(t *testing.T) {
	cacheDir(t)
	held, err := Lock(t.TempDir(), "start")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	old := time.Now().Add(-2 * guardStale)
	if err := os.Chtimes(held.path, old, old); err != nil {
		t.Fatal(err)
	}
	if breakIfStale(held.path) {
		t.Error("a lock was broken open while the process holding it was running")
	}
	if _, err := os.Stat(held.path); err != nil {
		t.Errorf("the lock of a running holder is gone: %v", err)
	}
}

// Age is how an abandoned lock is recognised, so a lock that is still held has
// to keep itself young. One held past guardStale -- by a slow smoke test, or a
// document slow to arrive -- was broken open by the next command, and two
// writers ran at once.
func TestALockStillHeldIsNotTakenForAbandoned(t *testing.T) {
	cacheDir(t)
	guardBeat = 5 * time.Millisecond
	t.Cleanup(func() { guardBeat = 30 * time.Second })

	held, err := Lock(t.TempDir(), "artifact write")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	old := time.Now().Add(-2 * guardStale)
	if err := os.Chtimes(held.path, old, old); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		info, err := os.Stat(held.path)
		if err != nil {
			t.Fatal(err)
		}
		if time.Since(info.ModTime()) < guardStale {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("a held lock never refreshed itself, so the next command would break it open")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if breakIfStale(held.path) {
		t.Error("a lock that is still held was broken open")
	}
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
		wg.Go(func() {
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
		})
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

// A lock is abandoned by a command that was interrupted, which is when the
// commands waiting on it arrive at it together. Removing it where it stood let
// one of them remove the lock another had just taken in its place, and two of
// them wrote at once.
func TestCommandsBreakingOpenOneAbandonedLockDoNotAllGetIt(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()
	dead := deadPID(t)

	for range 50 {
		abandoned, err := Lock(root, "gate")
		if err != nil {
			t.Fatal(err)
		}
		abandon(t, abandoned, dead)

		var holding, most atomic.Int32
		var wg sync.WaitGroup
		start := make(chan struct{})
		for range 5 {
			wg.Go(func() {
				<-start
				g, err := Lock(root, "review add")
				if err != nil {
					t.Error(err)
					return
				}
				n := holding.Add(1)
				for m := most.Load(); n > m; m = most.Load() {
					if most.CompareAndSwap(m, n) {
						break
					}
				}
				time.Sleep(time.Millisecond)
				holding.Add(-1)
				g.Release()
			})
		}
		close(start)
		wg.Wait()
		abandoned.Release()
		if n := most.Load(); n > 1 {
			t.Fatalf("%d commands held the lock at once after breaking it open", n)
		}
	}
}

// A holder whose lock was broken open under it -- taken for gone when it was
// not, as a process in another pid namespace can be -- finds it taken by
// another. Letting go must not remove that one's lock too.
func TestLettingGoOfALockTakenOverLeavesTheNewHolderAlone(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()

	slept, err := Lock(root, "gate")
	if err != nil {
		t.Fatal(err)
	}
	abandon(t, slept, deadPID(t))
	took, err := Lock(root, "review add")
	if err != nil {
		t.Fatal(err)
	}
	defer took.Release()

	slept.Release()
	if !ownedBy(took.path, took.token) {
		t.Error("letting go of a lock that was taken over removed the new holder's")
	}
}

// Holding the break, a command looks at the lock again. The one it found
// abandoned may since have been broken open by the command that held the break
// before it, and taken afresh -- and that lock is not abandoned.
func TestALockTakenAfreshBeforeTheBreakIsHeldIsLeftAlone(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()

	abandoned, err := Lock(root, "gate")
	if err != nil {
		t.Fatal(err)
	}
	defer abandoned.Release()
	abandon(t, abandoned, deadPID(t))

	var fresh *Guard
	mkdir = func(path string, perm fs.FileMode) error {
		mkdir = os.Mkdir
		// The command before this one, finishing its break in the gap.
		if err := os.RemoveAll(abandoned.path); err != nil {
			return err
		}
		g, err := Lock(root, "review add")
		if err != nil {
			return err
		}
		fresh = g
		return os.Mkdir(path, perm)
	}
	t.Cleanup(func() { mkdir = os.Mkdir })

	breakIfStale(abandoned.path)
	if fresh == nil {
		t.Fatal("the other command never took the lock")
	}
	defer fresh.Release()
	if !ownedBy(fresh.path, fresh.token) {
		t.Error("a lock taken afresh was removed as abandoned")
	}
}

// A lock is broken open by one command at a time, and one killed while doing
// it must not leave every command after it waiting on a break that never ends.
func TestABreakLeftByADeadProcessDoesNotWedgeTheProject(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()

	abandoned, err := Lock(root, "gate")
	if err != nil {
		t.Fatal(err)
	}
	defer abandoned.Release()
	breaking := abandoned.path + ".break"
	if err := os.Mkdir(breaking, 0o755); err != nil {
		t.Fatal(err)
	}
	abandon(t, abandoned, deadPID(t))
	old := time.Now().Add(-2 * guardStale)
	for _, path := range []string{abandoned.path, breaking} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	g, err := Lock(root, "start")
	if err != nil {
		t.Fatalf("an abandoned break kept the lock from being broken open: %v", err)
	}
	g.Release()
}

// An interrupted command gives the lock back on its way out, rather than leave
// every other command in the project waiting out guardStale.
func TestAnInterruptedCommandGivesTheLockBack(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()

	// One finishing its write is given the time to let go itself.
	finishing, err := Lock(root, "gate")
	if err != nil {
		t.Fatal(err)
	}
	var finished atomic.Bool
	go func() {
		time.Sleep(20 * time.Millisecond)
		finished.Store(true)
		finishing.Release()
	}()
	ReleaseHeld(guardWait)
	if !finished.Load() {
		t.Error("the lock was taken from a command that was still finishing its write")
	}
	if _, err := os.Stat(finishing.path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the process exited before the command let go of the lock: %v", err)
	}

	// One that does not finish in time has it given back for it.
	stuck, err := Lock(root, "review add")
	if err != nil {
		t.Fatal(err)
	}
	ReleaseHeld(10 * time.Millisecond)
	if _, err := os.Stat(stuck.path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("an interrupted command left the lock held: %v", err)
	}
}

// heldBy is the one message that has to explain a wait, and still has to say
// something with no readable owner behind it.
func TestHeldByNamesTheHolderOrSaysAnotherCommand(t *testing.T) {
	t.Parallel()
	if got := heldBy(filepath.Join(t.TempDir(), "missing")); got != "another command" {
		t.Errorf("heldBy on a missing lock = %q, want %q", got, "another command")
	}

	lock := t.TempDir()
	owner := guardOwner{PID: 4242, At: "2026-01-01T00:00:00Z", What: "review add", Token: "t"}
	raw, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lock, "owner.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if want := "`sdlc review add` (pid 4242, started 2026-01-01T00:00:00Z)"; heldBy(lock) != want {
		t.Errorf("heldBy = %q, want %q", heldBy(lock), want)
	}

	// A malformed owner file, or one that names no command, still gets a
	// sentence rather than an empty one.
	if err := os.WriteFile(filepath.Join(lock, "owner.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := heldBy(lock); got != "another command" {
		t.Errorf("heldBy on a malformed owner file = %q, want %q", got, "another command")
	}
}

// holderAlive judges a lock with no readable owner as gone: there is nobody to
// wait for.
func TestHolderAliveIsFalseWithoutAReadableOwnerFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if holderAlive(dir) {
		t.Error("holderAlive is true with no owner file at all")
	}
	if err := os.WriteFile(filepath.Join(dir, "owner.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if holderAlive(dir) {
		t.Error("holderAlive is true with a malformed owner file")
	}
}

func TestBreakIfStaleRetriesWhenTheLockIsAlreadyGone(t *testing.T) {
	t.Parallel()
	if !breakIfStale(filepath.Join(t.TempDir(), "missing")) {
		t.Error("breakIfStale did not say to retry when the lock was already gone by the time it looked")
	}
}

// A lock that is still held keeps itself young by touching its directory;
// once it is no longer this holder's -- taken over after this process was
// judged dead and broken open -- there is nothing left to keep young.
func TestBeatStopsOnceTheLockIsNoLongerThisHoldersToKeep(t *testing.T) {
	cacheDir(t)
	guardBeat = 5 * time.Millisecond
	t.Cleanup(func() { guardBeat = 30 * time.Second })

	g, err := Lock(t.TempDir(), "gate")
	if err != nil {
		t.Fatal(err)
	}
	defer g.Release()
	if err := os.WriteFile(filepath.Join(g.path, "owner.json"),
		[]byte(`{"pid":1,"at":"x","what":"y","token":"someone-else"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-g.done:
	case <-time.After(2 * time.Second):
		t.Fatal("beat kept running after the lock was no longer this holder's")
	}
}

// The one message that explains a wait: a lock held past the whole wait is
// reported rather than taken out from under its holder, and the holder still
// has it afterwards.
func TestALockHeldPastTheWholeWaitIsReportedRatherThanTaken(t *testing.T) {
	cacheDir(t)
	guardWait = 50 * time.Millisecond
	t.Cleanup(func() { guardWait = 10 * time.Second })

	root := t.TempDir()
	holder, err := Lock(root, "review add")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()

	_, err = Lock(root, "start")
	if err == nil {
		t.Fatal("a second command took the lock while the first still held it")
	}
	if got := codeOf(t, err); got != sdlcerr.StateUnwritable {
		t.Errorf("code = %s, want %s", got, sdlcerr.StateUnwritable)
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an sdlcerr.Error", err)
	}
	if !strings.Contains(e.What, "still running") {
		t.Errorf("what = %q, want it to say another command is still running", e.What)
	}
	// Who holds it and for how long is what tells a person whether to wait.
	for _, want := range []string{"review add", guardWait.String()} {
		if !strings.Contains(e.Why, want) {
			t.Errorf("why = %q, want it to name %q", e.Why, want)
		}
	}
	if !strings.Contains(e.Fix, holder.path) {
		t.Errorf("fix = %q, want it to name the lock to delete if nothing is running", e.Fix)
	}
	if !holderAlive(holder.path) {
		t.Error("the holder's lock was broken by the command that gave up waiting for it")
	}
}

// guardPath falls back to the system temp directory rather than fail outright
// when there is no cache directory to put the lock in: an unlocked command is
// worse than one locked somewhere less private.
func TestGuardPathFallsBackToTempDirWithNoCacheDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("LocalAppData", "")

	path := guardPath(t.TempDir())
	if !strings.HasPrefix(path, os.TempDir()) {
		t.Errorf("guardPath = %q, want it under %q", path, os.TempDir())
	}
}

func TestLockFailsWhenTheLockDirectoryCannotBeCreated(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()
	path := guardPath(root)
	// A plain file where the lock's parent directory needs to be.
	if err := os.MkdirAll(filepath.Dir(filepath.Dir(path)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Dir(path), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Lock(root, "gate"); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

func TestLockFailsOnAnUnexpectedMkdirError(t *testing.T) {
	cacheDir(t)
	mkdir = func(string, fs.FileMode) error {
		return errors.New("disk exploded")
	}
	t.Cleanup(func() { mkdir = os.Mkdir })

	if _, err := Lock(t.TempDir(), "gate"); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}

func TestLockFailsWhenTheOwnerFileCannotBeWritten(t *testing.T) {
	skipIfPermissionsDoNotBind(t)
	cacheDir(t)
	mkdir = func(path string, perm fs.FileMode) error {
		if err := os.Mkdir(path, perm); err != nil {
			return err
		}
		// No write permission: describe's write of owner.json will fail.
		return os.Chmod(path, 0o500)
	}
	t.Cleanup(func() { mkdir = os.Mkdir })

	if _, err := Lock(t.TempDir(), "gate"); codeOf(t, err) != sdlcerr.StateUnwritable {
		t.Errorf("code = %v, want %s", err, sdlcerr.StateUnwritable)
	}
}
