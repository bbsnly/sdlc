package store

// US-088: stories land one at a time, so that the second of two stops finds
// trunk where the first left it. These hold the lock every landing of a
// project takes, by name and signature:
//
//	(*Store) LockLandings(id string, wait time.Duration) (*Guard, error)
//
// It is one lock for the project, whatever story asks, beside the project's
// own lock and each story's landing lock and none of them. It is tried for up
// to wait, and at once where wait is nothing; the refusal is SDLC-E0006 and
// names whoever holds it, so that a stop can say whom it waits for. A lock
// left by a process that died is broken open by the rule every lock here
// follows, and it is among what ReleaseHeld lets go of.
//
// It is asked for through the store's method set, as LockLanding is in
// landing_test.go, so that the rest of this package's tests run before it
// exists.
//
// Helpers carry an sl prefix, as that file's do.

import (
	"errors"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

type slLandings interface {
	LockLandings(id string, wait time.Duration) (*Guard, error)
}

func slAllLandings(t *testing.T, s *Store) slLandings {
	t.Helper()
	var v any = s
	l, ok := v.(slLandings)
	if !ok {
		t.Fatal("the store has no LockLandings(id string, wait time.Duration) (*Guard, error)")
	}
	return l
}

// slRefusedAtOnce is the refusal a take that must not wait gives, and a
// failure where it took the lock or waited.
func slRefusedAtOnce(t *testing.T, lands slLandings, id string) *sdlcerr.Error {
	t.Helper()
	type taken struct {
		g   *Guard
		err error
	}
	got := make(chan taken, 1)
	go func() {
		g, err := lands.LockLandings(id, 0)
		got <- taken{g, err}
	}()
	select {
	case r := <-got:
		if r.err == nil {
			r.g.Release()
			t.Fatalf("%s took the landing lock while another landing held it", id)
		}
		return clError(t, r.err, sdlcerr.StateUnwritable)
	case <-time.After(slPatience):
		t.Fatal("a take that was told not to wait waited for the lock")
	}
	return nil
}

// One landing at a time for the whole project: a second story's, asked from
// its own worktree without waiting, is refused at once with SDLC-E0006, and
// the refusal names the stop that holds the lock and its process, not the one
// that asked. The project's lock and each story's own landing lock are other
// locks. Let go of, it is taken again.
func TestLockLandingsHoldsOneLandingForTheWholeProjectAndNamesItsHolder(t *testing.T) {
	cacheDir(t)
	main, wt := clRepository(t, map[string]model.Status{})
	lands, fromWorktree := slAllLandings(t, clStore(main)), slAllLandings(t, clStore(wt))

	held, err := lands.LockLandings("US-001", 0)
	if err != nil {
		t.Fatal(err)
	}
	e := slRefusedAtOnce(t, fromWorktree, "US-002")
	said := e.What + " " + e.Why
	for _, want := range []string{"stop US-001", "pid " + strconv.Itoa(os.Getpid()), "landing"} {
		if !strings.Contains(said, want) {
			t.Errorf("the refusal says %q / %q and does not name %q", e.What, e.Why, want)
		}
	}
	if strings.Contains(e.Why, "stop US-002") {
		t.Errorf("the refusal's why names the stop that asked as the holder: %q", e.Why)
	}
	if strings.TrimSpace(e.Fix) == "" {
		t.Error("the refusal says nothing about what to do next")
	}
	slRefusedAtOnce(t, lands, "US-001")

	project, err := Lock(clStore(main).LockRoot(), "gate")
	if err != nil {
		t.Fatalf("the project's lock waited on the landing lock: %v", err)
	}
	project.Release()
	for _, id := range []string{"US-001", "US-002"} {
		own, err := slLanding(t, clStore(main)).LockLanding(id)
		if err != nil {
			t.Fatalf("%s's own landing lock waited on the lock every landing holds: %v", id, err)
		}
		own.Release()
	}

	held.Release()
	again, err := fromWorktree.LockLandings("US-002", 0)
	if err != nil {
		t.Fatalf("the landing lock, let go of, could not be taken again: %v", err)
	}
	again.Release()
	if _, err := os.Stat(again.path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the lock is still on disk after it was released: %v", err)
	}
	if got := slStatus(t, main); got != "" {
		t.Errorf("taking the landing lock changed the repository:\n%s", got)
	}
}

// Given a wait, the second landing waits for the first and then has the lock:
// it is handed on, not refused. The first lets go only once the second has
// been turned away at least once, so the hand-over is seen and not assumed.
func TestLockLandingsWaitsForTheLandingThatHoldsIt(t *testing.T) {
	cacheDir(t)
	main, wt := clRepository(t, map[string]model.Status{})
	lands, fromWorktree := slAllLandings(t, clStore(main)), slAllLandings(t, clStore(wt))

	held, err := lands.LockLandings("US-001", 0)
	if err != nil {
		t.Fatal(err)
	}
	turnedAway := make(chan struct{}, 1)
	mkdir = func(path string, perm fs.FileMode) error {
		err := os.Mkdir(path, perm)
		if path == held.path && errors.Is(err, fs.ErrExist) {
			select {
			case turnedAway <- struct{}{}:
			default:
			}
		}
		return err
	}
	t.Cleanup(func() { mkdir = os.Mkdir })

	type taken struct {
		g   *Guard
		err error
	}
	second := make(chan taken, 1)
	go func() {
		g, err := fromWorktree.LockLandings("US-002", time.Hour)
		second <- taken{g, err}
	}()
	select {
	case <-turnedAway:
	case got := <-second:
		if got.err == nil {
			got.g.Release()
			t.Fatal("the landing lock was handed out twice")
		}
		t.Fatalf("a landing told to wait was refused while the lock was held: %v", got.err)
	case <-time.After(slPatience):
		t.Fatal("the second landing never tried for the lock")
	}
	held.Release()
	select {
	case got := <-second:
		if got.err != nil {
			t.Fatalf("the waiting landing never got the lock: %v", got.err)
		}
		if got.g.path != held.path {
			t.Errorf("two stories' landings took two locks, %s and %s", held.path, got.g.path)
		}
		got.g.Release()
	case <-time.After(slPatience):
		t.Fatal("the lock was never handed on")
	}
}

// A wait that runs out is the same refusal, after the wait and not before:
// the lock is still its holder's.
func TestLockLandingsRefusesOnceItsWaitHasRunOut(t *testing.T) {
	cacheDir(t)
	main, _ := clRepository(t, map[string]model.Status{})
	lands := slAllLandings(t, clStore(main))
	held, err := lands.LockLandings("US-001", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()

	const wait = 60 * time.Millisecond
	began := time.Now()
	g, err := lands.LockLandings("US-002", wait)
	if err == nil {
		g.Release()
		t.Fatal("the landing lock was handed out twice")
	}
	if waited := time.Since(began); waited < wait {
		t.Errorf("refused after %s, before the %s it was told to wait", waited, wait)
	}
	if e := clError(t, err, sdlcerr.StateUnwritable); !strings.Contains(e.What+" "+e.Why, "stop US-001") {
		t.Errorf("the refusal says %q / %q and does not name the holder", e.What, e.Why)
	}
	if !ownedBy(held.path, held.token) {
		t.Error("the lock is no longer its holder's after another landing's wait ran out")
	}
}

// A stop killed outright leaves the lock. It is broken open by the rule every
// lock here follows -- old, and its holder gone -- and not while its holder is
// running, however old it looks.
func TestALandingsLockLeftByADeadStopIsBrokenOpenAndALiveOnesIsNot(t *testing.T) {
	cacheDir(t)
	main, _ := clRepository(t, map[string]model.Status{})
	lands := slAllLandings(t, clStore(main))

	abandoned, err := lands.LockLandings("US-001", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer abandoned.Release()
	old := time.Now().Add(-2 * guardStale)
	if err := os.Chtimes(abandoned.path, old, old); err != nil {
		t.Fatal(err)
	}
	slRefusedAtOnce(t, lands, "US-002")

	abandon(t, abandoned, deadPID(t))
	g, err := lands.LockLandings("US-002", 0)
	if err != nil {
		t.Fatalf("a landing lock older than %s whose holder is gone was never broken open: %v", guardStale, err)
	}
	g.Release()
}

// The lock is one of the guards a signal lets go of, so that a stop
// interrupted while it lands does not hold every other story up.
func TestReleaseHeldLetsGoOfTheLockEveryLandingHolds(t *testing.T) {
	cacheDir(t)
	main, _ := clRepository(t, map[string]model.Status{})
	lands := slAllLandings(t, clStore(main))
	if _, err := lands.LockLandings("US-001", 0); err != nil {
		t.Fatal(err)
	}
	ReleaseHeld(0)
	again, err := lands.LockLandings("US-002", 0)
	if err != nil {
		t.Fatalf("after ReleaseHeld, the landing lock could not be taken: %v", err)
	}
	again.Release()
}
