package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// The loop's record is read, changed and written back, and until this existed
// nothing stopped two `sdlc` processes doing that at once. The last writer won
// and the other change vanished -- having reported success.
//
// That is not a background-mode curiosity. The runbook says to delegate the
// design and code reviewers "in parallel", and each of them records its verdict
// with `sdlc review add`. Five reviewers approving at the same time left one
// review in the record; the other four printed "approve (round 1)", wrote their
// review file, and were dropped. `sdlc review list` then said "not reviewed"
// for four agents that had each reported, the gate refused, and the session ran
// them all again.
//
// So every command that can write loop state takes this first. They are
// short-lived -- a few file reads and one atomic write -- so the waiting is not
// measurable, and correctness here is worth more than concurrency.

// The lock lives outside the repository, in the user's cache directory, keyed
// on the project's path.
//
// It is a mutex between two processes on one machine, not loop state, and it
// would do harm in the working tree: the commit gate refuses while anything is
// uncommitted, so a lock held during that check -- which is exactly when it is
// held -- would fail the gate it was protecting. Users would also find
// themselves committing it, since `.sdlc/state` is committed with the story.
//
// It is created with os.Mkdir, which fails if the directory already exists on
// every filesystem sdlc supports. That is the whole mechanism: no flock, no new
// dependency, and nothing that behaves differently on Windows.

// How long to wait for another process, and how long before an existing lock is
// assumed to belong to one that died. A command that takes longer than
// guardStale to do its work does not exist; a laptop that slept in the middle
// of one does.
const (
	guardWait  = 10 * time.Second
	guardStale = 2 * time.Minute
	guardPoll  = 10 * time.Millisecond
)

// Guard is a held lock. Release it when the command is done.
type Guard struct {
	path string
}

type guardOwner struct {
	PID  int    `json:"pid"`
	At   string `json:"at"`
	What string `json:"what"`
}

// Lock takes the project's loop-state lock, waiting for whoever has it.
//
// It is the project that is locked, not one file: a command changes the gate
// record, the backlog and the active marker together, and a reader that saw one
// of those without the others would see a story that never existed.
func Lock(root, what string) (*Guard, error) {
	path, err := guardPath(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, sdlcerr.New(sdlcerr.StateUnwritable,
			"the lock directory could not be created",
			filepath.Dir(path)+" is not writable").WithCause(err)
	}

	deadline := time.Now().Add(guardWait)
	for {
		err := os.Mkdir(path, 0o755)
		if err == nil {
			g := &Guard{path: path}
			g.describe(what)
			return g, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, sdlcerr.New(sdlcerr.StateUnwritable,
				"the loop's state could not be locked",
				path+" could not be created").WithCause(err)
		}
		if breakIfStale(path) {
			continue
		}
		if time.Now().After(deadline) {
			return nil, sdlcerr.New(sdlcerr.StateUnwritable,
				"another sdlc command is still running",
				heldBy(path)+" has held the loop's state for longer than "+
					guardWait.String()+", and two commands writing it at once "+
					"would lose one of the changes").
				WithFix("wait for it to finish, or delete " + path +
					" if nothing is running")
		}
		time.Sleep(guardPoll)
	}
}

// Release gives the lock up. It is safe to call more than once.
func (g *Guard) Release() {
	if g == nil || g.path == "" {
		return
	}
	_ = os.RemoveAll(g.path)
	g.path = ""
}

// describe records who holds the lock, so that a lock left behind by a crash
// says what it was and when rather than only that it is there.
func (g *Guard) describe(what string) {
	owner := guardOwner{PID: os.Getpid(), At: time.Now().UTC().Format(time.RFC3339), What: what}
	raw, err := json.Marshal(owner)
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(g.path, "owner.json"), append(raw, '\n'), 0o644)
}

// breakIfStale removes a lock whose holder is long gone, and reports whether it
// did. A process killed between taking the lock and releasing it would
// otherwise wedge the project for good, and telling somebody to delete a
// directory by hand is not a recovery story.
//
// The age of the directory is what decides it, not whether the pid is alive: a
// pid says nothing useful once it has been reused, and on Windows it says less.
func breakIfStale(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		// Gone between the Mkdir failing and now, which is the ordinary way a
		// contended lock is released. Retrying is the right move.
		return errors.Is(err, fs.ErrNotExist)
	}
	if time.Since(info.ModTime()) < guardStale {
		return false
	}
	return os.RemoveAll(path) == nil
}

// heldBy names whoever is holding the lock, for the one message that has to
// explain a wait. A lock with no readable owner still gets a sentence.
func heldBy(path string) string {
	raw, err := os.ReadFile(filepath.Join(path, "owner.json"))
	if err != nil {
		return "another command"
	}
	var o guardOwner
	if err := json.Unmarshal(raw, &o); err != nil || o.What == "" {
		return "another command"
	}
	return "`sdlc " + o.What + "` (pid " + strconv.Itoa(o.PID) + ", started " + o.At + ")"
}

// guardPath is where this project's lock lives: one directory per project,
// named for its path so that two clones of the same repository lock
// separately and two commands in one clone do not.
//
// The user's cache directory rather than the system temp directory, because
// that one is per-user and not world-writable: a lock in a shared /tmp is a
// directory anybody on the machine can create first.
func guardPath(root string) (string, error) {
	// Resolved, so that /tmp and /private/tmp on macOS -- or any other symlink
	// on the way to the project -- are one project and not two.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	sum := sha256.Sum256([]byte(root))
	name := hex.EncodeToString(sum[:16])

	base, err := os.UserCacheDir()
	if err != nil {
		// No cache directory is not a reason to stop: an unlocked command is
		// worse than a lock in a less private place.
		base = os.TempDir()
	}
	return filepath.Join(base, "sdlc", "locks", name), nil
}
