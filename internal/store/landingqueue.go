package store

import (
	"cmp"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A stop that waits for the landing lock says so beside the locks, in the
// user's cache and never in the project: a record of its own, a directory
// with an owner file, as a lock is. Before there was one, `sdlc status` in
// another checkout could not tell a stop that waited its turn from one that
// had hung, and nor could the person who ran it. The record orders nothing
// and blocks nothing: stories land in whatever order the lock is taken in.

// waitingInfix is what a waiter's record adds to the project's lock path,
// before the story and the waiting process.
const waitingInfix = "-waiting-"

// WaitToLand records that this process's stop of id waits for the landing
// lock. See WaitToLandAs.
func (s *Store) WaitToLand(id string) (*Guard, error) {
	return s.WaitToLandAs(LandingStop, id)
}

// WaitToLandAs records that this process's command of story id waits for the
// landing lock, until the guard it returns is released. Like a lock, the
// record is let go of on a signal (ReleaseHeld), and a record whose waiter is
// gone is removed by the next command to take the landing lock.
//
// The pid in its name keeps two processes waiting for one story apart; a
// record of this pid that is already there can only be a dead process's whose
// number came round, which is broken open by age.
func (s *Store) WaitToLandAs(command LandingCommand, id string) (*Guard, error) {
	if err := CheckID(id); err != nil {
		return nil, err
	}
	path := guardPath(s.LockRoot()) + waitingInfix + id + "-" + strconv.Itoa(os.Getpid())
	return take(path, string(command)+" "+id, 0, realClock{}, breakIfStale, func() error {
		return lockNotCreated(path, fs.ErrExist)
	})
}

// Landings is what `sdlc status` shows of the project's landing: who holds
// the landing lock, and who waits for it. Waiting is in the order they began
// to wait, which is not the order they land in.
type Landings struct {
	// Landing is nil where nothing holds the landing lock.
	Landing *LandingHolder
	Waiting []LandingWaiter
}

// LandingHolder is whoever holds the landing lock. Story, Command and Since
// are set only together, where the lock's label is a stop's or a remap's and
// its time is one; PID wherever the owner names a process.
type LandingHolder struct {
	Story   string
	Command LandingCommand
	Since   string
	PID     int
	// Gone is a holder whose process is not running, and From the RFC 3339
	// time a stop can take its lock from.
	Gone bool
	From string
}

// LandingWaiter is a stop or a remap that waits for the landing lock. For is
// the story of the stop it waits for, where a stop that is running holds the
// lock, and "" where anything else does.
type LandingWaiter struct {
	Story   string
	Command LandingCommand
	For     string
	Since   string
	PID     int
}

// LandingQueue is who holds the landing lock and who waits for it, read from
// any checkout of the project. It reads and never writes, and never fails: a
// lock or record that cannot be read, or that is not what one looks like, is
// shown as nothing it can trust, and a waiter is shown only while it runs and
// its record is kept young.
func (s *Store) LandingQueue() Landings {
	project := guardPath(s.LockRoot())
	var q Landings
	if info, err := os.Stat(project + "-landing"); err == nil {
		q.Landing = landingHolder(project+"-landing", info.ModTime())
	}
	now := s.ages()
	for _, w := range waiterRecords(project) {
		o, named := readOwner(w.path)
		command, id, trusted := landingLabel(o.What)
		if !named || !trusted || !isTime(o.At) || !processAlive(o.PID) || now().Sub(w.mtime) >= guardStale {
			continue
		}
		waiter := LandingWaiter{Story: id, Command: command, Since: o.At, PID: o.PID}
		if l := q.Landing; l != nil && l.Command == LandingStop && !l.Gone {
			waiter.For = l.Story
		}
		q.Waiting = append(q.Waiting, waiter)
	}
	slices.SortFunc(q.Waiting, func(a, b LandingWaiter) int {
		at, _ := time.Parse(time.RFC3339, a.Since)
		bt, _ := time.Parse(time.RFC3339, b.Since)
		return cmp.Or(at.Compare(bt), strings.Compare(a.Story, b.Story), cmp.Compare(a.PID, b.PID))
	})
	return q
}

// landingHolder is the holder of the landing lock at path, last refreshed at
// mtime, as far as its owner file can be trusted.
func landingHolder(path string, mtime time.Time) *LandingHolder {
	h := &LandingHolder{}
	o, named := readOwner(path)
	if !named {
		return h
	}
	h.PID = o.PID
	if h.Gone = !processAlive(o.PID); h.Gone {
		h.From = mtime.Add(guardStale).UTC().Format(time.RFC3339)
	}
	if command, id, trusted := landingLabel(o.What); trusted && isTime(o.At) {
		h.Story, h.Command, h.Since = id, command, o.At
	}
	return h
}

// sweepWaiters removes every waiter's record of the project whose waiter has
// gone, however young: a stop killed while it waited left one, and nothing
// else would ever remove it. A record that names no process is judged by its
// age alone, as a lock is: it may be one a waiter has only just made.
func (s *Store) sweepWaiters() {
	now := s.ages()
	for _, w := range waiterRecords(guardPath(s.LockRoot())) {
		if o, named := readOwner(w.path); now().Sub(w.mtime) >= guardStale || (named && !processAlive(o.PID)) {
			_ = os.RemoveAll(w.path)
		}
	}
}

// waiterRecord is a waiter's record, and when it was last refreshed.
type waiterRecord struct {
	path  string
	mtime time.Time
}

// waiterRecords is every waiter's record of the project whose lock is at
// project. One that cannot be read is not one.
func waiterRecords(project string) []waiterRecord {
	dir, prefix := filepath.Dir(project), filepath.Base(project)+waitingInfix
	entries, _ := os.ReadDir(dir)
	var out []waiterRecord
	for _, e := range entries {
		// The name first: the directory holds every project's locks, and only
		// this project's waiters are worth a look at the disk.
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if info, err := e.Info(); err == nil {
			out = append(out, waiterRecord{path: filepath.Join(dir, e.Name()), mtime: info.ModTime()})
		}
	}
	return out
}
