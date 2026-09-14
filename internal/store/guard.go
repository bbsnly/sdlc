package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"
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
// every filesystem sdlc supports. That is the whole mechanism: no flock, and no
// new dependency.
//
// One thing does behave differently on Windows. A directory another holder is
// in the middle of removing cannot be created again until it is gone, and the
// attempt fails with "Access is denied", not "already exists". That is the lock
// being handed on, so it is waited for like any other holder. Taking it for a
// failure lost the change of whichever command arrived as another let go.

// How long to wait for another process, and how long before an existing lock is
// assumed to belong to one that died. A command that takes longer than
// guardStale to do its work does not exist; a laptop that slept in the middle
// of one does.
const (
	guardWait  = 10 * time.Second
	guardStale = 2 * time.Minute
	guardPoll  = 10 * time.Millisecond
)

// mkdir is os.Mkdir, and a variable only so that a test can make it fail the
// way Windows does, or put another command in the gap before a break is held.
var mkdir = os.Mkdir

// guardBeat is how often a held lock says it is still held, by touching its
// directory. Age is how a lock left by a dead process is recognised, so a live
// one has to stay young: a command that held it longer than guardStale -- a
// slow smoke test, a document that took a while to arrive -- had it broken open
// under it by the next command, and one of the two changes was lost. A
// variable so a test can make it quick.
var guardBeat = 30 * time.Second

// Guard is a held lock. Release it when the command is done.
type Guard struct {
	path  string
	token string
	stop  chan struct{}
	done  chan struct{}
	once  sync.Once
}

type guardOwner struct {
	PID  int    `json:"pid"`
	At   string `json:"at"`
	What string `json:"what"`
	// Token is what this holder, and no other, wrote into the lock. A lock is
	// only ever removed by the holder whose token it still carries.
	Token string `json:"token"`
}

// held is every lock this process holds, so that a process interrupted on its
// way through a command can give them back. See ReleaseHeld.
var held = struct {
	sync.Mutex
	guards map[*Guard]struct{}
}{guards: map[*Guard]struct{}{}}

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
		err := mkdir(path, 0o755)
		if err == nil {
			g := &Guard{path: path, token: rand.Text(), stop: make(chan struct{}), done: make(chan struct{})}
			// A lock that does not say whose it is could never be let go of: its
			// holder removes it only on seeing its own token there.
			if err := g.describe(what); err != nil {
				_ = os.RemoveAll(path)
				return nil, sdlcerr.New(sdlcerr.StateUnwritable,
					"the loop's state could not be locked",
					path+" could not be written").WithCause(err)
			}
			go g.beat()
			held.Lock()
			held.guards[g] = struct{}{}
			held.Unlock()
			return g, nil
		}
		// A denial is waited for rather than failed: on Windows it is how a lock
		// being released looks (see above). One that outlasts the wait is not
		// that, and says what it really is.
		denied := errors.Is(err, fs.ErrPermission)
		if !denied && !errors.Is(err, fs.ErrExist) {
			return nil, sdlcerr.New(sdlcerr.StateUnwritable,
				"the loop's state could not be locked",
				path+" could not be created").WithCause(err)
		}
		if !denied && breakIfStale(path) {
			continue
		}
		if time.Now().After(deadline) {
			if denied {
				return nil, sdlcerr.New(sdlcerr.StateUnwritable,
					"the loop's state could not be locked",
					path+" could not be created").WithCause(err)
			}
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

// Release gives the lock up. It is safe to call more than once, and from more
// than one goroutine.
func (g *Guard) Release() {
	if g == nil {
		return
	}
	g.once.Do(func() {
		close(g.stop)
		<-g.done
		// Only if it is still this holder's. A command that slept past
		// guardStale with the lock wakes to find it broken open and taken by
		// another; removing the directory then would let a third command in
		// beside the second.
		if ownedBy(g.path, g.token) {
			_ = os.RemoveAll(g.path)
		}
		held.Lock()
		delete(held.guards, g)
		held.Unlock()
	})
}

// ReleaseHeld gives back every lock this process holds, for a process about to
// exit on a signal. Left to the default, a command interrupted while it held
// the lock left every other command in the project waiting out guardStale.
//
// A command holding one is given up to wait to finish and release it itself,
// so that what it was writing is written whole. Whatever is still held after
// that is released anyway.
func ReleaseHeld(wait time.Duration) {
	deadline := time.Now().Add(wait)
	for {
		held.Lock()
		remaining := make([]*Guard, 0, len(held.guards))
		for g := range held.guards {
			remaining = append(remaining, g)
		}
		held.Unlock()
		if len(remaining) == 0 {
			return
		}
		if time.Now().After(deadline) {
			for _, g := range remaining {
				g.Release()
			}
			return
		}
		time.Sleep(guardPoll)
	}
}

// beat keeps the lock's directory young for as long as it is held, and stops
// once it is no longer this holder's to keep.
func (g *Guard) beat() {
	defer close(g.done)
	ticker := time.NewTicker(guardBeat)
	defer ticker.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-ticker.C:
			if !ownedBy(g.path, g.token) {
				return
			}
			now := time.Now()
			_ = os.Chtimes(g.path, now, now)
		}
	}
}

// describe records who holds the lock: the token that makes it this holder's,
// and, for a lock left behind by a crash, what it was and when.
func (g *Guard) describe(what string) error {
	owner := guardOwner{PID: os.Getpid(), At: time.Now().UTC().Format(time.RFC3339), What: what, Token: g.token}
	raw, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(g.path, "owner.json"), append(raw, '\n'), 0o644)
}

// breakIfStale removes a lock whose holder is long gone, and reports whether it
// is worth trying to take the lock again. A process killed between taking the
// lock and releasing it would otherwise wedge the project for good, and telling
// somebody to delete a directory by hand is not a recovery story.
//
// Age alone is not enough. An old lock says its holder stopped touching it,
// which a clock set forward, or a laptop asleep mid-command, also does: broken
// open then, the holder carried on and wrote back what it had read before, and
// the other command's change was gone. So its holder has to be gone as well. A
// pid can be reused, and a lock that really was abandoned is then waited on and
// explained rather than broken, which loses nothing.
func breakIfStale(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		// Gone between the Mkdir failing and now, which is the ordinary way a
		// contended lock is released. Retrying is the right move.
		return errors.Is(err, fs.ErrNotExist)
	}
	if time.Since(info.ModTime()) < guardStale || holderAlive(path) {
		return false
	}

	// Broken open by one command at a time. Several can find one lock abandoned
	// at once, and each removing it where it stood let a later one remove the
	// lock an earlier one had just taken in its place: both went on to write.
	// Holding the break, a command looks again, and a lock that is still old
	// then is the abandoned one: nobody can create a new one while it stands,
	// and nobody else can remove it while the break is held.
	breaking := path + ".break"
	if err := mkdir(breaking, 0o755); err != nil {
		// Somebody else is breaking it, and is waited for -- unless they died
		// doing it, which would leave the break standing for good.
		if b, err := os.Stat(breaking); err == nil && time.Since(b.ModTime()) >= guardStale {
			_ = os.Remove(breaking)
		}
		return false
	}
	defer func() { _ = os.Remove(breaking) }()

	info, err = os.Stat(path)
	if err != nil || time.Since(info.ModTime()) < guardStale {
		return true
	}
	return os.RemoveAll(path) == nil
}

// holderAlive reports whether the process that took the lock is still running.
// A lock that does not say whose it is -- taken a moment ago and not yet
// described, or described by a process that died doing it -- is judged by its
// age alone.
func holderAlive(path string) bool {
	raw, err := os.ReadFile(filepath.Join(path, "owner.json"))
	if err != nil {
		return false
	}
	var o guardOwner
	if json.Unmarshal(raw, &o) != nil {
		return false
	}
	return processAlive(o.PID)
}

// ownedBy reports whether the lock at path is still the one token took.
func ownedBy(path, token string) bool {
	raw, err := os.ReadFile(filepath.Join(path, "owner.json"))
	if err != nil {
		return false
	}
	var o guardOwner
	return json.Unmarshal(raw, &o) == nil && o.Token == token
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
