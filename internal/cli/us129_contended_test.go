package cli

// US-129 AC-2 for start when another command holds the project's lock (the
// verifier's gap on start's contended branch). The base decided outside the
// lock, so a start that is refused -- off trunk, a story finished and not
// landed -- was refused at once, in start's own words, however long another
// command held the lock. start now decides under the lock, and where the lock
// is held it decides before the wait as well: the same refusals at once, and
// new work waits for the lock and is begun once it is let go.
//
// The lock is held by the test, and start waits on a lock clock that never
// moves, so a wait never runs out: its first sleep lets the lock go. A start
// that refused only after the wait has slept once.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// us129ReleasingClock lets the held lock go at its first sleep.
type us129ReleasingClock struct {
	mu      sync.Mutex
	release func()
	slept   int
}

func (*us129ReleasingClock) Now() time.Time { return time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC) }

func (c *us129ReleasingClock) Sleep(time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.slept++
	if c.release != nil {
		c.release()
		c.release = nil
	}
}

// us129StartAgainstAHeldLock runs start in root while the test holds the
// project's lock, and returns what it said and how often it slept.
func us129StartAgainstAHeldLock(t *testing.T, root string, args ...string) (result, int) {
	t.Helper()
	lockRoot, _ := lwLockRoot(t, root)
	holder, err := store.LockOn(lockRoot, "another command", nil)
	if err != nil {
		t.Fatal(err)
	}
	clock := &us129ReleasingClock{release: holder.Release}
	t.Cleanup(func() {
		clock.mu.Lock()
		defer clock.mu.Unlock()
		if clock.release != nil {
			clock.release()
		}
	})
	var out, errb bytes.Buffer
	code := executeAt(commandEnv{dir: root, getenv: invocationOf(t).getenv, lockClock: clock}, append([]string{"start"}, args...),
		strings.NewReader(""), &out, &errb)
	return result{stdout: out.String(), stderr: errb.String(), code: code}, clock.slept
}

func TestAC2AStartOffTrunkIsRefusedAtOnceInItsOwnWordsWhileAnotherCommandHoldsTheLock(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	wtGit(t, root, "checkout", "--quiet", "-b", "elsewhere")
	r, slept := us129StartAgainstAHeldLock(t, root)
	if r.code == 0 || !strings.Contains(r.stderr, `HEAD is on "elsewhere"`) || strings.Contains(r.stderr, "still running") {
		t.Errorf("AC-2 start off trunk with the lock held exited %d:\n%s\nwant start's own refusal, that HEAD is on \"elsewhere\"", r.code, r.stderr)
	}
	if slept != 0 {
		t.Errorf("AC-2 start off trunk waited for the lock (%d sleeps) before it refused; the base refused at once", slept)
	}
}

func TestAC2AStartOfAFinishedStoryIsRefusedAtOnceInItsOwnWordsWhileAnotherCommandHoldsTheLock(t *testing.T) {
	t.Parallel()
	root := startedAt(t, "")
	alone := run(t, "start")
	if alone.code == 0 {
		t.Fatalf("start of a finished story was not refused with the lock free:\n%s%s", alone.stdout, alone.stderr)
	}
	r, slept := us129StartAgainstAHeldLock(t, root)
	if r.code != alone.code || r.stderr != alone.stderr {
		t.Errorf("AC-2 start of a finished story with the lock held exited %d:\n%s\nwant what it says with the lock free, %d:\n%s",
			r.code, r.stderr, alone.code, alone.stderr)
	}
	if slept != 0 {
		t.Errorf("AC-2 start of a finished story waited for the lock (%d sleeps) before it refused; the base refused at once", slept)
	}
}

func TestAC2AStartOfNewWorkWaitsForTheLockAndBeginsTheStoryOnceItIsLetGo(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	r, slept := us129StartAgainstAHeldLock(t, root)
	if r.code != 0 {
		t.Fatalf("AC-2 start of new work with the lock held, then let go, exited %d:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if slept == 0 {
		t.Error("AC-2 start of new work began the story without waiting for the lock another command held")
	}
	raw, err := os.ReadFile(filepath.Join(root, "user_stories.json"))
	if err != nil {
		t.Fatal(err)
	}
	var b model.Backlog
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if story, ok := b.Find("US-001"); !ok || story.Status != model.StatusInProgress {
		t.Errorf("AC-2 after the wait US-001 is not in progress: %+v", story)
	}
}

// A HEAD that is a symbolic link to refs/heads/main is on main to git, and
// start at the base took it so (code review, security note 2): it does not
// refuse as not on trunk.
func TestAC2AStartWithHEADALinkToTrunksBranchBeginsTheStory(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("a symbolic link needs a privilege Windows does not give by default, and git there writes HEAD as a file")
	}
	root := gitProjectInitialised(t)
	head := filepath.Join(root, ".git", "HEAD")
	if err := os.Remove(head); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("refs", "heads", "main"), head); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "start"); r.code != 0 {
		t.Errorf("AC-2 start with HEAD a link to refs/heads/main exited %d:\n%s%s\nwant the story begun, as git reads HEAD as on main", r.code, r.stdout, r.stderr)
	}
}
