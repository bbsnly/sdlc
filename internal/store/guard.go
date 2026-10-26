package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
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
	guardStale = 2 * time.Minute
	guardPoll  = 10 * time.Millisecond
)

// ownerFile is the record inside a lock directory of who holds it, and
// stateNotLocked the summary of every refusal to take the lock.
const (
	ownerFile      = "owner.json"
	stateNotLocked = "the loop's state could not be locked"
)

// guardWait is a variable only so that a test can see a lock held past the
// whole wait without spending the wait: the refusal that explains it was the
// one message here no test reached, because reaching it took ten real seconds.
var guardWait = 10 * time.Second

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

// Clock is the time a wait for a lock is measured on. It is for the wait alone:
// a lock's age, its holder's liveness and its heartbeat stay on the real clock,
// because a clock that ran ahead there would break open a lock whose holder is
// alive, and one of the two commands' changes would be lost -- the bug the lock
// exists to prevent.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time        { return time.Now() }
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// Lock takes the project's loop-state lock, waiting for whoever has it, on the
// real clock. See LockOn.
//
// It is the project that is locked, not one file: a command changes the gate
// record, the backlog and the active marker together, and a reader that saw one
// of those without the others would see a story that never existed.
func Lock(root, what string) (*Guard, error) {
	return LockOn(root, what, nil)
}

// LockOn is Lock with the wait measured on clock, where nil is the real one. It
// exists so that a test can show a lock held past the whole wait without
// spending the wait: under load a real ten seconds is too short a margin for a
// test that only needs the wait to run out, and too long to spend on every one.
func LockOn(root, what string, clock Clock) (*Guard, error) {
	if clock == nil {
		clock = realClock{}
	}
	path := guardPath(root)
	return take(path, what, guardWait, clock, breakIfStale, func() error {
		// A holder that is gone is said to be, with the time its lock is taken
		// from: told only that a command was still running and to delete the
		// lock if nothing was, people deleted it by hand, and a model would.
		if gone := leftBy(path); gone != "" {
			return sdlcerr.New(sdlcerr.StateUnwritable,
				"another sdlc command is still running",
				heldBy(path)+" left the loop's state locked, and two commands writing it at once would lose one "+
					"of the changes; "+gone).
				WithFix("run the command again at or after the time given above, and it takes the lock; " +
					"nothing is removed by hand")
		}
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"another sdlc command is still running",
			heldBy(path)+" has held the loop's state for longer than "+
				guardWait.String()+", and two commands writing it at once "+
				"would lose one of the changes").
			WithFix("wait for it to finish, and run the command again: the lock at " + path + " is kept for as long " +
				"as its holder runs, and is taken once it has gone " + staleWords() + " without a refresh " +
				"after that. Only where the pid named above is not an sdlc command at all, its number having come " +
				"round to another program, does a person remove that directory by hand; an assistant never removes " +
				"it, and where the command is refused again it hands the story to a person with \"sdlc escalate\" " +
				"and stops")
	})
}

// LandingCommand is a command that takes the landing lock, as its lock names
// it. The landing lock is only ever held by one of these, so that what holds
// it reads back as a stop's landing or a remap, and as nothing else.
type LandingCommand string

// The commands that take the landing lock.
const (
	LandingStop  LandingCommand = "stop"
	LandingRemap LandingCommand = "remap"
)

// LockLanding takes the lock one sdlc stop holds while it lands story id,
// from its first check to its last write, the configured commands included.
// It is the project's lock at a path of the story's own beside it, keyed on
// the main checkout, so that another story's landing and every other command
// go on meanwhile. It is taken without waiting: a second stop of the same
// story would only run its checks again on what the first one is changing.
//
// Taken by a process that holds the landing lock, a story lock whose holder is
// not running is taken whatever its age. That is safe only because this lock
// is taken inside the landing lock and let go before it (landIfFinished):
// whoever holds it held the landing lock as they took it, and the landing lock
// is taken from a holder that has gone only by age, so by the time this
// process holds it, that holder has been waited out already. Without the rule,
// a stop killed as it landed left this lock a moment younger than the landing
// lock, and the stop that took the landing lock was refused this one at once.
// A caller that takes it outside the landing lock does not get the rule.
func (s *Store) LockLanding(id string) (*Guard, error) {
	if err := CheckID(id); err != nil {
		return nil, err
	}
	project := guardPath(s.LockRoot())
	path := project + "-landing-" + id
	rule := staleness{now: s.ages(), gone: holds(project + "-landing")}
	return take(path, string(LandingStop)+" "+id, 0, realClock{}, rule.breakOpen, func() error {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"another sdlc stop is landing "+id,
			landingHeldBy(path)+" holds the lock at "+sdlcerr.QuotePath(path)+", and a story is landed by one stop at a time").
			// Reached under the landing lock only with a holder that runs, or a
			// lock that does not say whose it is yet: one left by a stop that is
			// no longer running is taken at once there.
			WithFix("wait for that stop to finish, then run \"sdlc stop\" again if the story has not landed: the " +
				"lock is kept for as long as the stop that took it runs, and one that does not yet say which stop " +
				"took it is taken once it has gone " + staleWords() + " without a refresh")
	})
}

// LockLandings takes the one lock every landing of the project holds, as a
// stop. See LockLandingsAs.
func (s *Store) LockLandings(id string, wait time.Duration) (*Guard, error) {
	return s.LockLandingsAs(LandingStop, id, wait)
}

// LockLandingsAs takes the one lock every landing of the project holds, for
// command of story id, from before its first read of trunk to its last write,
// so that stories land one after another and the second finds trunk where the
// first left it. Without it two stops that began together each ran the
// project's commands and one lost the race for trunk, to run them all again.
//
// It is beside the project's lock and each story's own, and none of them. It
// is tried for up to wait, and at once where wait is nothing; the refusal
// names whoever holds it, so that a stop can say whom it waits for.
//
// That refusal alone wraps ErrLandingHeld. A lock that could not be made at
// all is refused with the same code, and has no holder to wait for.
func (s *Store) LockLandingsAs(command LandingCommand, id string, wait time.Duration) (*Guard, error) {
	if err := CheckID(id); err != nil {
		return nil, err
	}
	path := guardPath(s.LockRoot()) + "-landing"
	g, err := take(path, string(command)+" "+id, wait, realClock{}, staleness{now: s.ages()}.breakOpen, func() error {
		return landingHeld(path)
	})
	if err == nil {
		s.sweepWaiters()
	}
	return g, err
}

// landingHeld is the refusal of the landing lock at path, where somebody
// holds it.
func landingHeld(path string) error {
	if gone := leftBy(path); gone != "" {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"another story is still landing",
			landingHeldBy(path)+" left the landing lock at "+sdlcerr.QuotePath(path)+", and stories land on trunk "+
				"one at a time; "+gone).
			WithFix("nothing has changed, so run \"sdlc stop\" again at or after the time given above, and it takes " +
				"the lock; nothing is removed by hand. " + assistantStopsAgain).
			WithCause(ErrLandingLeft)
	}
	return sdlcerr.New(sdlcerr.StateUnwritable,
		"another story is still landing",
		landingHeldBy(path)+" holds the landing lock at "+sdlcerr.QuotePath(path)+", and stories land on trunk one at a time").
		WithFix("nothing has changed, so run \"sdlc stop\" again once that landing has finished. Where it is hung, " +
			"a person ends it -- Ctrl+C in its terminal, or ending the process named above where that is an sdlc " +
			"command -- and it gives the lock back as it goes; one killed outright leaves the lock, which is taken " +
			"once it has gone " + staleWords() + " without a refresh with its holder gone. The lock is kept " +
			"for as long as that process runs: only where it is not an sdlc command at all, its number having come " +
			"round to another program, does a person remove the lock by hand. " + assistantStopsAgain).
		WithCause(ErrLandingHeld)
}

// assistantStopsAgain is what an assistant does where the landing lock is
// held: the runbook stops again up to three times, and so does the refusal.
const assistantStopsAgain = `An assistant never removes the lock: it runs "sdlc stop" again up to three times ` +
	`and, where the third is refused too, hands the story to a person with "sdlc escalate" and stops`

// ErrLandingHeld is the cause of LockLandings' refusal where another stop
// holds the landing lock.
// It is a value of its own type, never shown: the refusal it is the cause of
// says everything a person reads.
var ErrLandingHeld error = landingHeldError{}

type landingHeldError struct {
	// gone is a holder that is not running, whose lock is taken by age.
	gone bool
}

func (landingHeldError) Error() string { return "another stop holds the landing lock" }

// Is makes the refusal ErrLandingHeld whether its holder runs or has gone, as
// a stop waits for either, and ErrLandingLeft only where it has gone.
func (e landingHeldError) Is(target error) bool {
	t, ok := target.(landingHeldError)
	return ok && (!t.gone || e.gone)
}

// ErrLandingLeft is the cause of LockLandings' refusal where the holder of the
// landing lock is not running, and its lock is taken once it is old enough.
// It is ErrLandingHeld as well.
var ErrLandingLeft error = landingHeldError{gone: true}

// take takes the lock at path, waiting up to wait for whoever has it, and
// returns busy's refusal once it has waited that long, as clock counts it.
// brk breaks open a lock its holder left, and reports whether to try again.
func take(path, what string, wait time.Duration, clock Clock, brk func(string) bool, busy func() error) (*Guard, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, sdlcerr.New(sdlcerr.StateUnwritable,
			"the lock directory could not be created",
			filepath.Dir(path)+" is not writable").WithCause(err)
	}

	start := clock.Now()
	for {
		err := mkdir(path, 0o755)
		if err == nil {
			return newGuard(path, what)
		}
		// A denial is waited for rather than failed, however short the wait for
		// a holder: on Windows it is how a lock being released looks (see
		// above). One that outlasts the wait is not that, and says what it
		// really is.
		denied := errors.Is(err, fs.ErrPermission)
		if !denied && !errors.Is(err, fs.ErrExist) {
			return nil, lockNotCreated(path, err)
		}
		if !denied && brk(path) {
			continue
		}
		if err := waitedTooLong(path, err, denied, clock.Now().Sub(start), wait, busy); err != nil {
			return nil, err
		}
		clock.Sleep(guardPoll)
	}
}

// waitedTooLong is the refusal once the wait for a lock is over, and nil while
// there is time to wait longer.
func waitedTooLong(path string, cause error, denied bool, waited, wait time.Duration, busy func() error) error {
	switch {
	case denied && waited > guardWait:
		return lockNotCreated(path, cause)
	case !denied && waited >= wait:
		return busy()
	}
	return nil
}

// lockNotCreated is the refusal where the lock at path could not be created
// for a reason other than somebody holding it.
func lockNotCreated(path string, cause error) error {
	return sdlcerr.New(sdlcerr.StateUnwritable,
		stateNotLocked,
		path+" could not be created").WithCause(cause)
}

// newGuard is the guard for the lock directory this process has just made at
// path: it says whose it is, and is kept alive until released.
func newGuard(path, what string) (*Guard, error) {
	g := &Guard{path: path, token: rand.Text(), stop: make(chan struct{}), done: make(chan struct{})}
	// A lock that does not say whose it is could never be let go of: its
	// holder removes it only on seeing its own token there.
	if err := g.describe(what); err != nil {
		_ = os.RemoveAll(path)
		return nil, sdlcerr.New(sdlcerr.StateUnwritable,
			stateNotLocked,
			path+couldNotBeWritten).WithCause(err)
	}
	go g.beat()
	held.Lock()
	held.guards[g] = struct{}{}
	held.Unlock()
	return g, nil
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
// once it is no longer this holder's to keep: gone, or another holder's. A read
// of the owner file that fails any other way is not taken for that. Stopping on
// one, a live holder's landing lock aged out under it while its story's own
// lock went on beating, and the stop that took the landing lock took that one
// too, a moment old.
func (g *Guard) beat() {
	defer close(g.done)
	ticker := time.NewTicker(guardBeat)
	defer ticker.Stop()
	for {
		select {
		case <-g.stop:
			return
		case <-ticker.C:
			if lost(g.path, g.token) {
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
	return os.WriteFile(filepath.Join(g.path, ownerFile), append(raw, '\n'), 0o644) //nolint:gosec // G306: git hooks and other checkouts read the owner file
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
	return staleness{now: time.Now}.breakOpen(path)
}

// staleness is how a lock is judged left by its holder: by its age on now, as
// a lock's age is always judged, and, with gone set, by its holder alone.
type staleness struct {
	now func() time.Time
	// gone takes a lock whose owner names a process that is not running,
	// whatever its age. Only LockLanding sets it, and only for a process
	// that holds the landing lock (see there).
	gone bool
}

// The rules a lock is broken open by, as the debug log names them.
const (
	ruleAge  = "old, and its holder gone"
	ruleGone = "its holder gone, under the landing lock"
)

// left reports whether the lock at path was left by its holder, and the rule
// that says so; err is why the lock could not be looked at.
//
// A lock that does not name a process -- taken a moment ago and not yet
// described, or described by a process that died doing it -- is judged by its
// age, gone or not: under gone alone such a lock was never broken open.
func (st staleness) left(path string) (rule string, left bool, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if st.gone {
		if o, ok := readOwner(path); ok {
			return ruleGone, !processAlive(o.PID), nil
		}
	}
	return ruleAge, st.now().Sub(info.ModTime()) >= guardStale && !holderAlive(path), nil
}

// breakOpen removes the lock at path where its holder left it, and reports
// whether it is worth trying to take the lock again. See breakIfStale.
func (st staleness) breakOpen(path string) bool {
	_, left, err := st.left(path)
	if err != nil {
		// Gone between the Mkdir failing and now, which is the ordinary way a
		// contended lock is released. Retrying is the right move.
		return errors.Is(err, fs.ErrNotExist)
	}
	if !left {
		return false
	}

	// Broken open by one command at a time. Several can find one lock abandoned
	// at once, and each removing it where it stood let a later one remove the
	// lock an earlier one had just taken in its place: both went on to write.
	// Holding the break, a command looks again, and a lock that is still left
	// then is the abandoned one: nobody can create a new one while it stands,
	// and nobody else can remove it while the break is held.
	breaking := path + ".break"
	if err := mkdir(breaking, 0o755); err != nil {
		// Somebody else is breaking it, and is waited for -- unless they died
		// doing it, which would leave the break standing for good.
		if b, err := os.Stat(breaking); err == nil && st.now().Sub(b.ModTime()) >= guardStale {
			_ = os.Remove(breaking)
		}
		return false
	}
	defer func() { _ = os.Remove(breaking) }()

	rule, left, err := st.left(path)
	if err != nil || !left {
		return true
	}
	removed := os.RemoveAll(path) == nil
	if removed {
		// Said, because a lock broken open is the one decision here that can
		// let two writers in, and the rule it was broken by is what a person
		// needs to tell a mistaken break from a right one.
		slog.Debug("broke open a lock its holder left", "path", path, "rule", rule)
	}
	return removed
}

// holds reports whether this process holds the lock at path, and still has it:
// a lock broken open under it and taken by another is not its own.
func holds(path string) bool {
	held.Lock()
	var tokens []string
	for g := range held.guards {
		if g.path == path {
			tokens = append(tokens, g.token)
		}
	}
	held.Unlock()
	return slices.ContainsFunc(tokens, func(token string) bool { return ownedBy(path, token) })
}

// ownerAt is the owner file of the lock at path, as it reads.
func ownerAt(path string) (guardOwner, error) {
	raw, err := os.ReadFile(filepath.Join(path, ownerFile))
	if err != nil {
		return guardOwner{}, err
	}
	var o guardOwner
	err = json.Unmarshal(raw, &o)
	return o, err
}

// readOwner is the owner of the lock at path, and whether it names a process.
// A pid of 0 or less names none: to a signal they mean a process group, which
// read as running on Unix and as not running on Windows. Nor does one past
// MaxInt32: Unix's kill truncates it to 32 bits, to 0, -1 or another process,
// where Windows reads it as not running. A lock that says either is judged as
// one that says nothing, the same on every platform.
func readOwner(path string) (guardOwner, bool) {
	o, err := ownerAt(path)
	return o, err == nil && o.PID > 0 && o.PID <= math.MaxInt32
}

// holderAlive reports whether the process that took the lock is still running.
// A lock that does not say whose it is -- taken a moment ago and not yet
// described, or described by a process that died doing it -- is judged by its
// age alone.
func holderAlive(path string) bool {
	o, ok := readOwner(path)
	return ok && processAlive(o.PID)
}

// ownedBy reports whether the lock at path is still the one token took.
func ownedBy(path, token string) bool {
	o, err := ownerAt(path)
	return err == nil && o.Token == token
}

// lost reports whether the lock at path is known to be no longer token's: it
// is gone, or another holder's.
func lost(path, token string) bool {
	o, err := ownerAt(path)
	return errors.Is(err, fs.ErrNotExist) || (err == nil && o.Token != token)
}

// leftBy is what a refusal says of a lock whose owner names a process that is
// not running, and "" for any other: that it is not, and when the lock is
// taken. Nothing of the owner file but its pid is printed, and no host: the
// lock may be shared with one where the process does run.
func leftBy(path string) string {
	o, ok := readOwner(path)
	if !ok || processAlive(o.PID) {
		return ""
	}
	said := "pid " + strconv.Itoa(o.PID) + " is not running on this machine, so its lock can be taken once it " +
		"has gone " + staleWords() + " without a refresh"
	if info, err := os.Stat(path); err == nil {
		said += ", at about " + info.ModTime().Add(guardStale).UTC().Format(time.RFC3339)
	}
	return said
}

// staleWords is guardStale as a person reads it, in whole minutes: Go's 2m0s
// is not how anybody says it.
func staleWords() string {
	return strconv.Itoa(int(guardStale/time.Minute)) + " minutes"
}

// heldBy names whoever is holding the lock, for the one message that has to
// explain a wait. A lock with no readable owner still gets a sentence.
func heldBy(path string) string {
	return holderWords(path, commandLabel)
}

// landingHeldBy is heldBy for a landing lock, which only a stop or a remap of
// a story holds.
func landingHeldBy(path string) string {
	return holderWords(path, func(what string) bool {
		_, _, ok := landingLabel(what)
		return ok
	})
}

// holderWords names the holder of the lock at path by what of its owner file
// it can trust. The lock is in a directory any process of the user's can
// write, and a refusal is read by a model as the tool's own words: a label
// is printed only where trusted says it is one this code writes, and a time
// only where it is one.
func holderWords(path string, trusted func(string) bool) string {
	o, ok := readOwner(path)
	if !ok {
		return "another command"
	}
	process := "pid " + strconv.Itoa(o.PID)
	if isTime(o.At) {
		process += ", started " + o.At
	}
	if trusted(o.What) {
		return "`sdlc " + o.What + "` (" + process + ")"
	}
	return "another command (" + process + ")"
}

// isTime reports whether at is an RFC 3339 time, as a holder writes it.
func isTime(at string) bool {
	_, err := time.Parse(time.RFC3339, at)
	return err == nil
}

// commandLabel reports whether what is a label this code writes: the path of
// a command that takes the project's lock (openStoreForWriting), the Stop
// hook's, and for the commands that take a lock for one story, that story --
// start's own lock on the story's worktree (LockStory), and a stop's or a
// remap's landing lock. A command that comes to take a lock later and is not
// named here is shown as another command, which says less and never anything
// untrue.
func commandLabel(what string) bool {
	switch what {
	case "ack", "approve", "escalate", "freeze", "gate", "release", "unfreeze", "hook Stop",
		"artifact write", "codemap write", "cost add", "review add", "staffing set", "staffing amend",
		"staffing assign":
		return true
	}
	command, id, _ := strings.Cut(what, " ")
	return (command == string(LandingStop) || command == string(LandingRemap) || command == "start") &&
		(id == "" || CheckID(id) == nil)
}

// landingLabel is the command and story of a landing lock's label, where it
// is one a stop or a remap writes.
func landingLabel(what string) (command LandingCommand, id string, ok bool) {
	word, id, _ := strings.Cut(what, " ")
	command = LandingCommand(word)
	if (command != LandingStop && command != LandingRemap) || CheckID(id) != nil {
		return "", "", false
	}
	return command, id, true
}

// guardPath is where this project's lock lives: one directory per project,
// named for its path so that two clones of the same repository lock
// separately and two commands in one clone do not.
//
// The user's cache directory rather than the system temp directory, because
// that one is per-user and not world-writable: a lock in a shared /tmp is a
// directory anybody on the machine can create first.
func guardPath(root string) string {
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
	return filepath.Join(base, "sdlc", "locks", name)
}
