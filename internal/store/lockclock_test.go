package store

// US-107: the wait for the project's lock measured on a clock a test gives,
// so that a test can see a stop held past the whole wait without spending ten
// real seconds and without loading the machine (AC-1). The AC numbers in this
// file are US-107's.
//
// Only the wait moves to the given clock. How a lock is taken, and when one
// left by a dead process is broken open, stay on the real clock: a lock aged
// by a test's clock would be broken open under a holder that is still writing,
// which is the lost change the lock exists to prevent (THREATS.md).
//
// Every test here sets the process's environment through cacheDir, so none of
// them is parallel.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// lcPatience bounds how long a test waits for a lock to be refused or taken;
// a deadline, not a pause.
const lcPatience = 2 * time.Minute

// lcStepClock is a Clock that moves only when it is slept on, by whatever it
// is asked to sleep plus jump, and at once. A wait measured on it takes no
// time on the wall.
type lcStepClock struct {
	mu      sync.Mutex
	start   time.Time
	now     time.Time
	jump    time.Duration
	slept   int
	longest time.Duration
	// onSleep, where set, is called with the number of the sleep, from 1,
	// before the clock moves.
	onSleep func(n int)
}

func newStepClock(at time.Time, jump time.Duration) *lcStepClock {
	return &lcStepClock{start: at, now: at, jump: jump}
}

func (c *lcStepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *lcStepClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.slept++
	n, on := c.slept, c.onSleep
	c.mu.Unlock()
	if on != nil {
		on(n)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d + c.jump)
	c.longest = max(c.longest, d+c.jump)
}

// waited is how far the clock has moved since it was made, and how many times
// it was slept on.
func (c *lcStepClock) waited() (time.Duration, int, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now.Sub(c.start), c.slept, c.longest
}

// lcLockOn is LockOn, given lcPatience to answer.
func lcLockOn(t *testing.T, root, what string, clock Clock) (*Guard, error) {
	t.Helper()
	type taken struct {
		g   *Guard
		err error
	}
	got := make(chan taken, 1)
	go func() {
		g, err := LockOn(root, what, clock)
		got <- taken{g, err}
	}()
	var r taken
	select {
	case r = <-got:
	case <-time.After(lcPatience):
		t.Fatalf("LockOn neither took the lock nor refused within %s", lcPatience)
	}
	if r.g != nil {
		t.Cleanup(r.g.Release)
	}
	return r.g, r.err
}

// lcRefusal is err as the refusal of a lock held past the whole wait, or the
// test's end where it is anything else.
func lcRefusal(t *testing.T, err error) *sdlcerr.Error {
	t.Helper()
	if err == nil {
		t.Fatal("the lock was taken while another command held it")
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an sdlcerr.Error", err)
	}
	if e.Code != sdlcerr.StateUnwritable {
		t.Errorf("code = %s, want %s", e.Code, sdlcerr.StateUnwritable)
	}
	return e
}

// AC-1: a wait for the project's lock measured on a clock the caller gives
// runs out where ten seconds of that clock have passed, and says so in the
// words a person has always read: another command is still running, which
// one, and for longer than 10s. The holder keeps its lock.
func TestAC1AWaitOnAGivenClockRunsOutAtTenSecondsOfThatClock(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()
	holder, err := Lock(root, "stop")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()

	clock := newStepClock(time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC), 0)
	_, err = lcLockOn(t, root, "stop", clock)
	e := lcRefusal(t, err)
	if e.What != "another sdlc command is still running" {
		t.Errorf("what = %q, want the refusal of a wait for the loop's state run out", e.What)
	}
	for _, want := range []string{"`sdlc stop`", "longer than 10s"} {
		if !strings.Contains(e.Why, want) {
			t.Errorf("why = %q, want it to say %q", e.Why, want)
		}
	}
	waited, slept, longest := clock.waited()
	if slept == 0 {
		t.Fatal("the given clock was never slept on: the wait between tries was not measured on it")
	}
	// Refused at the first look at or past ten seconds of the clock, and not
	// one sleep later or earlier.
	if waited < 10*time.Second || waited-10*time.Second >= longest {
		t.Errorf("the wait ran out after %s of the given clock (%d sleeps), want it at the first try at or past 10s", waited, slept)
	}
	if !ownedBy(holder.path, holder.token) {
		t.Error("the holder's lock is no longer its own after another command gave up waiting for it")
	}
}

// AC-1: a lock let go while the given clock is still short of the wait is
// taken, by whoever was waiting, and says whose it now is.
func TestAC1ALockLetGoBeforeTheGivenClockRunsOutIsTaken(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()
	holder, err := Lock(root, "stop")
	if err != nil {
		t.Fatal(err)
	}
	clock := newStepClock(time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC), 0)
	clock.onSleep = func(n int) {
		if n == 3 {
			holder.Release()
		}
	}

	g, err := lcLockOn(t, root, "review add", clock)
	if err != nil {
		t.Fatalf("a lock let go while the wait had time left was refused: %v", err)
	}
	if waited, _, _ := clock.waited(); waited >= 10*time.Second {
		t.Errorf("the lock was taken after %s of the given clock, want it as soon as it was let go", waited)
	}
	if got := heldBy(g.path); !strings.Contains(got, "`sdlc review add`") {
		t.Errorf("the lock taken says it is held by %s, want `sdlc review add`", got)
	}
}

// AC-1, THREATS: the given clock measures the wait and nothing else. A lock a
// moment old, naming a process that has exited, is not abandoned by the real
// clock's reckoning; a clock that leaps an hour at every sleep must not make
// it so, or a test's clock would break open a lock under a live writer.
func TestAC1AGivenClockMeasuresTheWaitAndNotTheAgeOfTheLock(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()
	holder, err := Lock(root, "gate")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()
	// Owned by a process that has exited, and touched just now.
	raw, err := json.Marshal(guardOwner{PID: deadPID(t), What: "gate", Token: holder.token})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(holder.path, ownerFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := os.Chtimes(holder.path, now, now); err != nil {
		t.Fatal(err)
	}

	clock := newStepClock(time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC), time.Hour)
	_, err = lcLockOn(t, root, "start", clock)
	if e := lcRefusal(t, err); e.What != "another sdlc command is still running" {
		t.Errorf("what = %q, want the wait run out", e.What)
	}
	if !ownedBy(holder.path, holder.token) {
		t.Error("a lock touched a moment ago was broken open because the given clock had leapt past its age")
	}
}

// AC-1, THREATS: and the other way about. A lock left by a process that died
// holding it, older than guardStale on the real clock, is broken open and
// taken as it always was, though the given clock stands decades before it.
func TestAC1ALockLeftByADeadProcessIsBrokenOpenWhateverTheGivenClockSays(t *testing.T) {
	cacheDir(t)
	root := t.TempDir()
	abandoned, err := Lock(root, "gate")
	if err != nil {
		t.Fatal(err)
	}
	defer abandoned.Release()
	abandon(t, abandoned, deadPID(t))

	clock := newStepClock(time.Unix(0, 0).UTC(), 0)
	if _, err := lcLockOn(t, root, "start", clock); err != nil {
		t.Fatalf("a lock left by a dead process was not broken open under a given clock: %v", err)
	}
}

// AC-1, THREATS: on Windows a lock being let go of reads as a denial, which
// is waited for as long as a holder is; that wait is on the given clock too,
// and once it has run out the lock is reported as one that could not be made.
func TestAC1ADenialIsWaitedForOnTheGivenClock(t *testing.T) {
	cacheDir(t)
	mkdir = func(path string, _ fs.FileMode) error {
		return &fs.PathError{Op: "mkdir", Path: path, Err: fs.ErrPermission}
	}
	t.Cleanup(func() { mkdir = os.Mkdir })

	clock := newStepClock(time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC), 0)
	_, err := lcLockOn(t, t.TempDir(), "start", clock)
	if e := lcRefusal(t, err); e.What != stateNotLocked {
		t.Errorf("what = %q, want %q", e.What, stateNotLocked)
	}
	if waited, slept, _ := clock.waited(); slept == 0 || waited <= 10*time.Second {
		t.Errorf("a denial was given up on after %s of the given clock (%d sleeps), want past 10s of it", waited, slept)
	}
}

// AC-1, THREATS, the default path: with no clock given the wait is on the real
// clock, as Lock's is. The wait is shortened here, as the test of the refusal
// itself shortens it, so that seeing it run out costs a moment and not ten
// seconds; a clock that stood still would never run out at all.
func TestAC1WithNoClockGivenTheWaitIsOnTheRealClock(t *testing.T) {
	cacheDir(t)
	guardWait = 50 * time.Millisecond
	t.Cleanup(func() { guardWait = 10 * time.Second })

	root := t.TempDir()
	holder, err := Lock(root, "stop")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Release()

	began := time.Now()
	_, err = lcLockOn(t, root, "start", nil)
	took := time.Since(began)
	e := lcRefusal(t, err)
	if !strings.Contains(e.Why, "longer than "+guardWait.String()) {
		t.Errorf("why = %q, want it to say the wait of %s ran out", e.Why, guardWait)
	}
	if took < guardWait {
		t.Errorf("a wait with no clock given ran out after %s on the wall, want at least the %s of the real clock", took, guardWait)
	}
}
