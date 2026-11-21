// Package store owns everything the loop keeps on disk.
//
// Two rules run through it. Every write is atomic -- a temporary file beside
// the target, then a rename -- so an interrupted run can never leave a record
// half-written and unparseable. And every story id is checked before it becomes
// part of a path, because ids come from a JSON file that an assistant may have
// written, and "../../.." is a perfectly good JSON string.
package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// Paths under the repository root, relative and slash-separated. They are the
// same paths the shell kit used, so an existing project needs no migration.
// The endings of the summaries a failed read, write or removal gives, which the
// codes for them share.
const (
	couldNotBeRead        = " could not be read"
	couldNotBeWritten     = " could not be written"
	couldNotBeWrittenDown = " could not be written down"
	couldNotBeRemoved     = " could not be removed"
	isNotValidJSON        = " is not valid JSON"
	existsButNotOpened    = " exists but could not be opened"
)

const (
	stateDir   = config.Dir + "/state"
	storiesDir = config.Dir + "/stories"
	activeFile = stateDir + "/" + model.ActiveFile
	// sessionFile names the Claude Code session working the story: the one
	// `sdlc start` last ran in. The hook holds that session to the story, and
	// leaves every other session in the repository alone.
	sessionFile = stateDir + "/" + model.SessionFile
	// AcknowledgedFile names the last commit a person has read the log up to.
	// `sdlc log` starts after it.
	AcknowledgedFile = stateDir + "/" + model.AcknowledgedFile
	lockFile         = stateDir + "/" + model.FreezeFile
	reviewsDir       = "reviews"
	recordFile       = model.RecordFile
)

// safeID is what a story id may contain, given that it becomes a directory
// name. It is deliberately narrower than "what a filesystem accepts": the
// backlog schema asks for AUTH-3, and anything in this set is safe on Windows,
// macOS and Linux alike. It does not end in a dot: Windows drops a trailing
// dot from a directory name, so `A-1.` and `A-1` were two stories sharing one
// directory, and starting one overwrote the other's record.
//
// Spelt out byte by byte rather than as the regular expression it was,
// `^[A-Za-z0-9](?:[A-Za-z0-9._-]{0,62}[A-Za-z0-9_-])?$`: every hook call
// checks an id or two, and the expression's match state cost each call 36 KB
// (US-129, AC-8). The two were held equal over every string of up to three
// printable characters and the lengths at the edge (round 3's record).
func safeID(id string) bool {
	n := len(id)
	if n == 0 || n > 64 || !idAlnum(id[0]) {
		return false
	}
	for i := 1; i < n; i++ {
		switch c := id[i]; {
		case idAlnum(c), c == '_', c == '-':
		case c == '.' && i < n-1:
		default:
			return false
		}
	}
	return true
}

// idAlnum reports whether c is an ASCII letter or digit.
func idAlnum(c byte) bool {
	return 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9'
}

// Store reads and writes one project's loop state.
//
// A checkout keeps its own iteration: the story it is working, the session, the
// freeze and the Stop hook's count. The backlog and the story records are the
// repository's, and in a linked worktree they are the main checkout's, which
// is where every checkout reads and writes them.
type Store struct {
	root   string
	cfg    config.Config
	now    func() time.Time
	getenv func(string) string
	place  *placement
	// lockNow is the time the store's locks are aged on, nil for the real
	// clock. Only lockAgesOn sets it, and only a test calls that.
	lockNow func() time.Time
	// reads is what the invocation has read of the loop's files, shared by
	// every store opened for it (Reads).
	reads *Reads
	// configs reads another checkout's configuration, as the caller has it
	// already; nil reads the file (ReadingConfigs).
	configs func(root string) (config.Config, error)
}

// placement is where the checkout the store was opened in sits in its
// repository, found the first time something needs it: most calls the hook
// makes read only the checkout's own state, and pay nothing for it.
type placement struct {
	once sync.Once
	co   config.Checkout
	err  error
}

// New opens the store for a project.
func New(p *config.Project) *Store {
	return &Store{root: p.Root, cfg: p.Config, now: time.Now, getenv: os.Getenv, place: &placement{}, reads: NewReads()}
}

// NewIn opens the store for a project whose checkout the caller has already
// found, so that one call finds it once.
func NewIn(p *config.Project, co config.Checkout, err error) *Store {
	s := New(p)
	s.place.once.Do(func() { s.place.co, s.place.err = co, err })
	return s
}

// Reading is a copy of the store that answers from reads: what the caller's
// invocation has read already, shared so that two stores opened for one call
// read each file once between them (US-129, AC-5).
func (s *Store) Reading(reads *Reads) *Store {
	out := *s
	if reads != nil {
		out.reads = reads
	}
	return &out
}

// ReadingConfigs is a copy of the store that reads another checkout's
// configuration through configs, which reads it as config.ForRules does: the
// hook keeps every configuration its call has read, and the pause reading the
// main checkout's file again opened it twice in one call (US-129, AC-5).
func (s *Store) ReadingConfigs(configs func(root string) (config.Config, error)) *Store {
	out := *s
	out.configs = configs
	return &out
}

// configAt is the configuration of the checkout at root, for the rules.
func (s *Store) configAt(root string) (config.Config, error) {
	if s.configs != nil {
		return s.configs(root)
	}
	cfg, _, err := config.ForRules(root)
	return cfg, err
}

// Reads is what the store has read for its invocation, for another store or
// a reader that shares it.
func (s *Store) Reads() *Reads { return s.reads }

// checkout is where the store's checkout sits in its repository.
func (s *Store) checkout() (config.Checkout, error) {
	s.place.once.Do(func() { s.place.co, s.place.err = config.FindCheckout(s.root) })
	return s.place.co, s.place.err
}

// shared is the directory the backlog and the story records are kept under:
// the main checkout, which in any checkout that is not a linked worktree is the
// store's own root, spelled as it was opened.
func (s *Store) shared() (string, error) {
	co, err := s.checkout()
	switch {
	case err != nil:
		return "", err
	case co.Linked():
		return co.Main, nil
	}
	return s.root, nil
}

// SharedErr is why the backlog and the story records cannot be reached from
// this checkout, and nil when they can.
func (s *Store) SharedErr() error {
	_, err := s.shared()
	return err
}

// LockRoot is the directory the repository's one lock is keyed on: the main
// checkout, so that every checkout of a repository takes the same lock over
// the one backlog, or this checkout where the main checkout cannot be found.
func (s *Store) LockRoot() string {
	if shared, err := s.shared(); err == nil {
		return shared
	}
	return s.root
}

// BacklogPath is the backlog file this checkout reads and writes.
func (s *Store) BacklogPath() string {
	if shared, err := s.shared(); err == nil {
		return s.cfg.BacklogPath(shared)
	}
	return s.cfg.BacklogPath(s.root)
}

// Checkout is where the store's checkout sits in its repository, and why the
// main checkout could not be found where it could not.
func (s *Store) Checkout() (config.Checkout, error) { return s.checkout() }

// WithClock returns a copy that reads the time from now, for tests and for any
// caller that needs every timestamp in one operation to match.
func (s *Store) WithClock(now func() time.Time) *Store {
	out := *s
	out.now = now
	return &out
}

// lockAgesOn returns a copy whose locks are aged on now, so that a test can
// stand a lock on the age it is taken at without waiting the age out. It is
// neither the record's clock (WithClock) nor a wait's (Clock): either one
// running ahead would break open a lock whose holder is alive, and one of two
// commands' changes would be lost. No code outside the tests calls it.
func (s *Store) lockAgesOn(now func() time.Time) *Store {
	out := *s
	out.lockNow = now
	return &out
}

// ages is the clock the store's locks are aged on: the real one, unless a test
// made the store with lockAgesOn.
func (s *Store) ages() func() time.Time {
	if s.lockNow != nil {
		return s.lockNow
	}
	return time.Now
}

// WithEnv has the store read the command's environment through getenv, for a
// command that is not the process's own: tests run many at once in one
// process, each with its own session.
func (s *Store) WithEnv(getenv func(string) string) *Store {
	s.getenv = getenv
	return s
}

// Root is the repository root the store writes under.
func (s *Store) Root() string { return s.root }

// CommitPause reports the story's risk tier, and whether the project holds a
// story of that tier for a person's approval before it is committed: by the
// tier alone, as every configuration the story cannot narrow has it
// (PauseGates). One that does not read is an error, not this checkout's tiers
// alone, which would be a pause taken away (US-133, verifier F-3). What its
// change touches is PauseFor's to say, which is what the gates ask.
func (s *Store) CommitPause(id string) (string, bool, error) {
	story, _, err := s.Story(id)
	if err != nil {
		return "", false, err
	}
	tier := strings.TrimSpace(story.RiskTier)
	if tier == "" {
		tier = config.DefaultRiskTier
	}
	record, err := s.Record(id)
	if err != nil {
		record = nil
	}
	gates, err := s.PauseGates(context.Background(), record)
	return tier, gates.PausesBeforeCommit(tier), err
}

// StorySubject is the story as a reviewer of it reads it, by content: its entry
// in the backlog, without the status and updated fields the loop writes as the
// story moves. Change what the story asks for, and a review of it is stale.
func (s *Store) StorySubject(id string) (string, error) {
	story, _, err := s.Story(id)
	if err != nil {
		return "", err
	}
	subject := *story
	subject.Status, subject.Updated = "", ""
	raw, err := json.Marshal(subject)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// Config is the project's configuration, as the store was opened with it.
func (s *Store) Config() config.Config { return s.cfg }

// CheckID rejects a story id that cannot safely become a directory name.
func CheckID(id string) error {
	// ".." anywhere, not only on its own: the hook refuses such an id and
	// turns every rule off, so an id the CLI accepted and the hook did not was
	// an iteration that ran with nothing enforced.
	if safeID(id) && !strings.Contains(id, "..") {
		return nil
	}
	shown := id
	if shown == "" {
		shown = "(empty)"
	}
	return sdlcerr.New(sdlcerr.UnsafeStoryID,
		"the story id "+quote(shown)+" cannot be used",
		"a story id becomes a directory under "+storiesDir+", so it must be letters, digits, "+
			"dots, dashes and underscores, starting with a letter or a digit")
}

// StoryDir is where everything about one story is kept.
func (s *Store) StoryDir(id string) (string, error) {
	if err := CheckID(id); err != nil {
		return "", err
	}
	shared, err := s.shared()
	if err != nil {
		return "", err
	}
	return filepath.Join(shared, filepath.FromSlash(storiesDir), id), nil
}

// ---------------------------------------------------------------- backlog

// Backlog reads the project's stories.
func (s *Store) Backlog() (*model.Backlog, error) {
	_, b, err := s.readBacklog()
	return b, err
}

// readBacklog returns the backlog's bytes with the stories read from them, so
// that an edit is made to the same bytes that were checked.
func (s *Store) readBacklog() ([]byte, *model.Backlog, error) {
	shared, err := s.shared()
	if err != nil {
		return nil, nil, err
	}
	path := s.cfg.BacklogPath(shared)
	raw, err := s.reads.File(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil, sdlcerr.New(sdlcerr.BacklogMissing,
			"there is no backlog to read",
			"backlog.path in "+config.File+" names "+relative(s.root, path)+", which does not exist")
	case err != nil:
		return nil, nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
			relative(s.root, path)+couldNotBeRead,
			"opening it failed").WithCause(err)
	}

	var b model.Backlog
	if err := json.Unmarshal(bytes.TrimPrefix(raw, byteOrderMark), &b); err != nil {
		return nil, nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
			relative(s.root, path)+isNotValidJSON,
			"the loop reads every story from this file, so it stops rather than guess").WithCause(err)
	}
	// Ids are compared as a case-insensitive filesystem compares them: each one
	// becomes a directory name, and on macOS and Windows two ids that differ only
	// in case are one directory.
	seen := make(map[string]int, len(b.Stories))
	for i, story := range b.Stories {
		switch {
		case story.ID == "":
			return nil, nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
				"a story in "+relative(s.root, path)+" has no id",
				"it is story number "+strconv.Itoa(i+1)+" in the file, and every story needs an id")
		case story.Title == "":
			return nil, nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
				"the story "+quote(story.ID)+" has no title",
				"every story needs a title: it is what the commit message and the retro refer to")
		case !story.Status.Valid():
			// A status the loop does not know was read as no status at all:
			// "Ready" was never picked, never listed as waiting, and nothing
			// said why.
			known := make([]string, len(model.Statuses))
			for i, st := range model.Statuses {
				known[i] = string(st)
			}
			return nil, nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
				"the story "+quote(story.ID)+" has the status "+quote(string(story.Status)),
				"a status is one of "+strings.Join(known, ", ")+", and a story with any other "+
					"is one the loop would never pick")
		}
		if err := CheckID(story.ID); err != nil {
			return nil, nil, err
		}
		// A second story with an id was never read: every lookup found the first,
		// so starting it moved the other one, and doctor called the backlog fine.
		if first, ok := seen[strings.ToLower(story.ID)]; ok {
			return nil, nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
				"two stories in "+relative(s.root, path)+" have the id "+quote(story.ID),
				"they are stories number "+strconv.Itoa(first+1)+" and "+strconv.Itoa(i+1)+" in the file, "+
					"and every story needs an id of its own, whatever its case: the loop would work on one "+
					"and move the other").
				WithFix("give one of them another id")
		}
		seen[strings.ToLower(story.ID)] = i
	}
	return raw, &b, nil
}

// Story reads one story from the backlog.
func (s *Store) Story(id string) (*model.Story, *model.Backlog, error) {
	b, err := s.Backlog()
	if err != nil {
		return nil, nil, err
	}
	found, ok := b.Find(id)
	if !ok {
		return nil, nil, storyNotFound(b, id)
	}
	return found, b, nil
}

func storyNotFound(b *model.Backlog, id string) error {
	return sdlcerr.New(sdlcerr.StoryNotFound,
		"no story with the id "+quote(id),
		describeBacklog(b))
}

// SetStoryStatus moves a story to a new status and saves the backlog.
//
// It edits the status and the updated time of that one story where they are
// in the file, and leaves every other byte alone (see editStory).
//
// Moving a story to the status it already has is not a move, and this writes
// nothing. That is not an optimisation. The backlog is a tracked file, and the
// whole tree is what the verifier's and the code reviewer's approvals are
// stamped with -- so a write that changed nothing but the timestamp would send
// both back to re-review work that had not changed.
func (s *Store) SetStoryStatus(id string, status model.Status) error {
	raw, b, err := s.readBacklog()
	if err != nil {
		return err
	}
	found, ok := b.Find(id)
	if !ok {
		return storyNotFound(b, id)
	}
	if found.Status == status {
		return nil
	}

	path := s.BacklogPath()
	edited, ok := editStory(raw, id,
		change{"status", jsonString(string(status))},
		change{"updated", jsonString(model.Timestamp(s.now()))})
	if !ok {
		return sdlcerr.New(sdlcerr.BacklogUnreadable,
			relative(s.root, path)+" could not be edited in place",
			"it was read, but the story "+quote(id)+" could not be found in its bytes; this is a bug")
	}
	return s.replaceBacklog(path, edited)
}

// ---------------------------------------------------------------- iteration

// Active returns the story the loop is currently working on, or "" if no
// iteration is running.
func (s *Store) Active() (string, error) {
	raw, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(activeFile)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", sdlcerr.New(sdlcerr.StateUnreadable,
			"the loop's state could not be read",
			activeFile+existsButNotOpened).WithCause(err)
	}
	id := strings.TrimSpace(string(raw))
	// An empty file is not one the tool leaves: ending an iteration removes
	// it. The hook reads an empty one as naming no story and turns every rule
	// off, so this refuses it too, rather than report that nothing is running.
	//
	// Its fix is not CheckID's: there is no story by that name to rename, and
	// what clears the file is ending the iteration.
	if err := CheckID(id); err != nil {
		shown := id
		if shown == "" {
			shown = "nothing"
		}
		return "", sdlcerr.New(sdlcerr.UnsafeStoryID,
			activeFile+" does not name a story",
			"it holds "+quote(shown)+", and sdlc start only ever writes a story id there").
			WithFix(`a person runs "sdlc stop" to remove it and end the iteration, then "sdlc start" or types /sdlc:next in Claude Code to begin again`).
			WithCause(err)
	}
	return id, nil
}

// Acknowledged is the commit a person last read the log up to, as the file
// names it, and whether there is a file at all. What it names is for git to
// read: a file that names nothing is not the same as no file.
func (s *Store) Acknowledged() (string, bool, error) {
	raw, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(AcknowledgedFile)))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	case err != nil:
		return "", false, sdlcerr.New(sdlcerr.StateUnreadable,
			AcknowledgedFile+couldNotBeRead,
			"it exists but could not be opened").WithCause(err)
	}
	return strings.TrimSpace(string(raw)), true, nil
}

// Acknowledge records the commit a person has read the log up to.
func (s *Store) Acknowledge(sha string) error {
	return s.writeFile(filepath.Join(s.root, filepath.FromSlash(AcknowledgedFile)), []byte(sha+"\n"))
}

// SetActive marks a story as the one being worked on.
func (s *Store) SetActive(id string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	return s.writeFile(filepath.Join(s.root, filepath.FromSlash(activeFile)), []byte(id+"\n"))
}

// SessionEnv is the variable Claude Code sets, in the Bash tool and in hooks, to
// the id of the session they run in. In a hook it matches the payload's
// session_id.
const SessionEnv = "CLAUDE_CODE_SESSION_ID"

// AgentEnv is the variable Claude Code labels the processes it starts with:
// claude-code_<version>_agent in a model's Bash or PowerShell and everything
// they start, and claude-code_<version>_harness in a process it starts on its
// own behalf, every hook among them (2.1.321 in Claude Desktop, US-130). The
// slash route reads the label in internal/hook/typedby.go, which keeps its own
// copy of the name; recording does not read it (US-112). A `!` line typed in
// Claude Desktop has none of the markers below (seen on 2026-10-29).
const AgentEnv = "AI_AGENT"

// claudeCodeMarkers are the variables Claude Code sets for the processes it
// starts: a model's Bash or PowerShell, anything they start, a `!` command,
// and a hook.
var claudeCodeMarkers = [...]string{"CLAUDECODE", SessionEnv, "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_PID", AgentEnv}

// ClaudeCodeMarkers returns claudeCodeMarkers. The CLI refuses a person's
// command where any is set and the hook hands a typed one none of them, so
// the list is a copy no importer can write to, a test among them.
func ClaudeCodeMarkers() [len(claudeCodeMarkers)]string { return claudeCodeMarkers }

// CheckSession reports whether id can be a Claude Code session id: letters,
// digits, dots, dashes and underscores, one to two hundred of them. Claude
// Code's are UUIDs; this is wider than that, and still nothing that could be a
// path. Byte by byte, as safeID is, for `^[A-Za-z0-9._-]{1,200}$`: the
// expression's match state cost every hook call it ran 12 KB (US-129, AC-8).
func CheckSession(id string) bool {
	if len(id) == 0 || len(id) > 200 {
		return false
	}
	for i := range len(id) {
		if c := id[i]; !idAlnum(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// CommandSession is the Claude Code session the command runs in: SessionEnv,
// read through the getenv WithEnv gave, and otherwise from the process
// environment, which is what every store made outside cli reads, the hook's
// included. A caller with a getenv of its own, as hook.Run has, passes it with
// WithEnv, or the process's value is read instead.
func (s *Store) CommandSession() string { return s.getenv(SessionEnv) }

// BindSession records the Claude Code session this command runs in as the one
// working the story, and returns it: empty when the command runs in none, from
// a terminal or wherever Claude Code does not set SessionEnv.
//
// With no session, beginning an iteration removes whatever record is left,
// which belongs to no iteration, and the story holds no session until one picks
// it up with /sdlc:next. Picking up an iteration already under way leaves the
// record as it is. A start that cannot say which session it runs in cannot say
// the story has moved, and wiping the record turned every rule off in the
// session working it, without a word.
func (s *Store) BindSession(underWay bool) (string, error) {
	id := s.CommandSession()
	if !CheckSession(id) {
		id = ""
	}
	if id == "" && underWay {
		return "", nil
	}
	return id, s.bindSession(id)
}

// WorkingSession is the Claude Code session recorded as working the story, and
// false when there is none the hook would hold to it: no record, one that
// cannot be read, or one that names no session.
func (s *Store) WorkingSession() (string, bool) {
	raw, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(sessionFile)))
	if err != nil {
		return "", false
	}
	id := strings.TrimSpace(string(raw))
	return id, CheckSession(id)
}

func (s *Store) bindSession(id string) error {
	path := filepath.Join(s.root, filepath.FromSlash(sessionFile))
	if CheckSession(id) {
		return s.writeFile(path, []byte(id+"\n"))
	}
	if s.leaves(path) {
		return sdlcerr.New(sdlcerr.StateUnwritable, relative(s.root, path)+couldNotBeRemoved, leavesWhy).
			WithFix(leavesFix)
	}
	s.reads.Forget(path)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"the session working the story could not be recorded",
			sessionFile+couldNotBeRemoved).WithCause(err)
	}
	return nil
}

// ClearActive ends the iteration, and with it the record of the session that
// was working the story. The session goes first: an iteration that could not
// be ended keeps its session, and one left with none holds no session.
func (s *Store) ClearActive() error {
	path := filepath.Join(s.root, filepath.FromSlash(activeFile))
	if s.leaves(path) {
		return sdlcerr.New(sdlcerr.StateUnwritable, "the iteration could not be ended", leavesWhy).WithFix(leavesFix)
	}
	for _, file := range []string{sessionFile, activeFile} {
		s.reads.Forget(filepath.Join(s.root, filepath.FromSlash(file)))
		err := os.Remove(filepath.Join(s.root, filepath.FromSlash(file)))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return sdlcerr.New(sdlcerr.StateUnwritable,
				"the iteration could not be ended",
				file+couldNotBeRemoved).WithCause(err)
		}
	}
	return nil
}

// ---------------------------------------------------------------- record

// ---------------------------------------------------------------- the freeze

// Lock reads the freeze, or nil if the tests have not been frozen.
func (s *Store) Lock() (*model.Lock, error) {
	path := filepath.Join(s.root, filepath.FromSlash(lockFile))
	raw, err := s.reads.File(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // nil is the documented "not frozen"; called from internal/cli, changing with it
	}
	if err != nil {
		return nil, sdlcerr.New(sdlcerr.StateUnreadable,
			"the test freeze could not be read",
			lockFile+existsButNotOpened).WithCause(err)
	}
	var l model.Lock
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, sdlcerr.New(sdlcerr.StateUnreadable,
			"the test freeze could not be read",
			lockFile+isNotValidJSON).WithCause(err)
	}
	return &l, nil
}

// SaveLock writes the freeze.
func (s *Store) SaveLock(l *model.Lock) error {
	return s.writeJSON(filepath.Join(s.root, filepath.FromSlash(lockFile)), l)
}

// ClearLock lifts the freeze. It is a separate, deliberate act: lifting it by
// accident is the one thing that must not be easy.
func (s *Store) ClearLock() error {
	path := filepath.Join(s.root, filepath.FromSlash(lockFile))
	if s.leaves(path) {
		return sdlcerr.New(sdlcerr.StateUnwritable, "the test freeze could not be lifted", leavesWhy).WithFix(leavesFix)
	}
	s.reads.Forget(path)
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"the test freeze could not be lifted",
			lockFile+couldNotBeRemoved).WithCause(err)
	}
	return nil
}

// HashFile is the content of one file, as the freeze records it.
func (s *Store) HashFile(rel string) (string, error) {
	return hashFile(filepath.Join(s.root, filepath.FromSlash(rel)), rel,
		"the freeze records what a test file contains, and this one could not be opened")
}

// HashArtifact is the content of one of a story's documents, where every
// checkout keeps them: in the repository's main checkout.
func (s *Store) HashArtifact(story string, a model.Artifact) (string, error) {
	dir, err := s.StoryDir(story)
	if err != nil {
		return "", err
	}
	return hashFile(filepath.Join(dir, a.File), ArtifactPath(story, a),
		"the "+a.Name+" could not be opened; check that the file is there and that you may read it")
}

// hashFile is the content of the file at path, which messages call rel. The
// caller says why an open failure matters: the files hashed here are not used
// for the same thing, and one shared reason blamed the freeze for a plan.
func hashFile(path, rel, openWhy string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", sdlcerr.New(sdlcerr.StateUnreadable,
			rel+couldNotBeRead,
			openWhy).WithCause(err)
	}
	defer f.Close()

	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", sdlcerr.New(sdlcerr.StateUnreadable,
			rel+couldNotBeRead,
			"reading it stopped part way through").WithCause(err)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// Record reads a story's gate record, starting a fresh one if there is none.
func (s *Store) Record(id string) (*model.Record, error) {
	record, _, err := s.readRecord(id)
	return record, err
}

// readRecord is Record with the bytes the record was read from, nil where there
// is no file: a write that has to be taken back puts the file back as it was,
// not as it would be encoded a second time.
func (s *Store) readRecord(id string) (*model.Record, []byte, error) {
	dir, err := s.StoryDir(id)
	if err != nil {
		return nil, nil, err
	}
	raw, err := s.reads.File(filepath.Join(dir, recordFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return model.NewRecord(id, s.now()), nil, nil
	case err != nil:
		return nil, nil, sdlcerr.New(sdlcerr.StateUnreadable,
			"the record for "+quote(id)+couldNotBeRead,
			storiesDir+"/"+id+"/"+recordFile+existsButNotOpened).WithCause(err)
	}

	var r model.Record
	if err := model.DecodeRecord(raw, &r); err != nil {
		return nil, nil, sdlcerr.New(sdlcerr.StateUnreadable,
			"the record for "+quote(id)+isNotValidJSON,
			storiesDir+"/"+id+"/"+recordFile+" does not parse, and sdlc writes it whole or not "+
				"at all, so it was edited or merged by hand; git has the last committed copy").WithCause(err)
	}
	if r.Gates == nil {
		r.Gates = map[model.Gate]model.GateResult{}
	}
	r.Story = id
	return &r, raw, nil
}

// StoriesOnDisk lists the stories that have a directory under .sdlc/stories,
// in name order. A directory whose name could not be a story id is not one.
func (s *Store) StoriesOnDisk() ([]string, error) {
	shared, err := s.shared()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(shared, filepath.FromSlash(storiesDir))
	entries, err := os.ReadDir(dir)
	// What is there is asked rather than read from the error: on Windows a file
	// here is ERROR_PATH_NOT_FOUND, which errors.Is takes for not there at all,
	// and some filesystems list a file as empty (go.dev/issue/61159), where
	// POSIX says ENOTDIR. Stat follows a link, so a dangling one is no stories.
	if err != nil || len(entries) == 0 {
		if info, statErr := os.Stat(dir); statErr == nil && !info.IsDir() {
			return nil, sdlcerr.New(sdlcerr.StateUnreadable,
				storiesDir+" is not a directory, so no story in it could be listed",
				"something other than a directory is where it belongs").
				WithFix("move whatever is at " + storiesDir + " aside and restore the directory, " +
					"from git where your project commits it")
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, sdlcerr.New(sdlcerr.StateUnreadable,
			storiesDir+" could not be listed",
			"it is there, but reading the directory failed").WithCause(err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && CheckID(e.Name()) == nil {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// SaveRecord writes a story's gate record.
func (s *Store) SaveRecord(r *model.Record) error {
	dir, err := s.StoryDir(r.Story)
	if err != nil {
		return err
	}
	return s.writeJSON(filepath.Join(dir, recordFile), r)
}

// WriteArtifact stores one of a gate's documents inside the story's directory
// and returns the path it was written to, relative to the checkout the story's
// records are kept in. Relative to this one, a linked worktree's, it was the
// main checkout's absolute path, and the record it went into is committed.
func (s *Store) WriteArtifact(story string, a model.Artifact, content []byte) (string, error) {
	dir, err := s.StoryDir(story)
	if err != nil {
		return "", err
	}
	// The revision first: a PLAN.md whose revision could not be kept is a review
	// that could never be read back.
	if a.Gate == model.GatePlan {
		if err := s.keepPlanRevision(dir, content); err != nil {
			return "", err
		}
	}
	path := filepath.Join(dir, a.File)
	if err := s.writeFile(path, content); err != nil {
		return "", err
	}
	return relative(s.rootOf(path), path), nil
}

// ArtifactStored reports whether a gate's document is on disk. An unsafe story
// id answers false rather than failing: the caller is asking a question about a
// file, and every path that could write one has already refused this id.
func (s *Store) ArtifactStored(story string, a model.Artifact) bool {
	dir, err := s.StoryDir(story)
	if err != nil {
		return false
	}
	return fsx.Exists(filepath.Join(dir, a.File))
}

// WriteReview stores one reviewer's document inside the story's own reviews
// directory and returns the path, relative to the checkout the story's records
// are kept in, as WriteArtifact does.
//
// Rounds are kept rather than overwritten: "what did the architect say last
// time" is a question the next round needs answered.
func (s *Store) WriteReview(story string, gate model.Gate, role string, round int, content []byte) (string, error) {
	dir, err := s.StoryDir(story)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, reviewsDir, reviewName(gate, role, round))
	if err := s.writeFile(path, content); err != nil {
		return "", err
	}
	return relative(s.rootOf(path), path), nil
}

// ReviewPath is where a review lives, relative to the repository root and
// slash-separated, whether or not it has been written yet.
func ReviewPath(story string, gate model.Gate, role string, round int) string {
	return storiesDir + "/" + story + "/" + reviewsDir + "/" + reviewName(gate, role, round)
}

func reviewName(gate model.Gate, role string, round int) string {
	return string(gate) + "-" + role + "-" + strconv.Itoa(round) + ".md"
}

// ArtifactPath is where an artifact lives, relative to the repository root and
// slash-separated, whether or not it has been written yet.
func ArtifactPath(story string, a model.Artifact) string {
	return storiesDir + "/" + story + "/" + a.File
}

// Now is the store's clock, so that everything written in one operation carries
// the same timestamp.
func (s *Store) Now() time.Time { return s.now() }

// ---------------------------------------------------------------- writing

// writeJSON writes v as indented JSON with a trailing newline. Indented,
// because these files live in the repository and a user reads them in a diff.
//
// The bytes are json.MarshalIndent's, laid out by encodeIndented into one
// buffer sized by what the file held before, with the newline in the same
// buffer. MarshalIndent grew its compact and its indented copies by doubling
// and the newline's append copied the result once more, which for a 190 KB
// gate record was the largest cost of a subagent's start and stop in the hook
// (US-129, AC-8). Round 3's record holds the byte-for-byte comparison over the
// loop's shapes.
func (s *Store) writeJSON(path string, v any) error {
	out, err := encodeIndented(s.reads.size(path), v)
	if err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			relative(s.root, path)+couldNotBeWritten,
			"the value could not be encoded as JSON").WithCause(err)
	}
	return s.writeFile(path, append(out, '\n'))
}

// indentWriter is an io.Writer that lays out compact JSON, as json.Marshal
// writes it, as json.Indent(src, "", "  ") would have written it: each write
// is the next piece of one value, laid out onto out where the last left off.
type indentWriter struct {
	out []byte
	in  indenter
}

func (w *indentWriter) Write(p []byte) (int, error) {
	for _, c := range p {
		w.out = w.in.next(w.out, c)
	}
	return len(p), nil
}

// indentCounter is indentWriter that keeps the count and not the bytes: the
// room a value whose file was not read is given, so that laying it out grows
// nothing. The scratch holds what one byte appends at any depth a record has;
// deeper than 31 the append spills to the heap and the count is still right.
type indentCounter struct {
	n  int
	in indenter
}

func (w *indentCounter) Write(p []byte) (int, error) {
	var scratch [64]byte
	for _, c := range p {
		w.n += len(w.in.next(scratch[:0], c))
	}
	return len(p), nil
}

// indenter lays out compact JSON, as json.Marshal writes it, a byte at a time
// as json.Indent(src, "", "  ") would have written it: a newline and the
// depth's indent after `{`, `[` and `,`, a space after `:`, the closing
// bracket on a line of its own, `{}` and `[]` left as they are, and what is
// inside a string copied as it is. json.Indent reformats through a buffer of
// its own, grown by doubling, before copying into the caller's (US-129, AC-8);
// round 3's record holds the byte-for-byte comparison over the loop's shapes.
// It is where the layout has got to: the depth, whether it is inside a string
// and after a backslash there, and whether an opening bracket is waiting to
// learn if its object or array is empty.
type indenter struct {
	depth                         int
	inString, escaped, needIndent bool
}

// next appends c to dst as json.Indent writes it.
func (in *indenter) next(dst []byte, c byte) []byte {
	if in.inString {
		return in.inside(dst, c)
	}
	if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
		return dst
	}
	if in.needIndent && c != '}' && c != ']' {
		in.needIndent = false
		in.depth++
		dst = indentLine(dst, in.depth)
	}
	switch c {
	case '"':
		in.inString = true
	case '{', '[':
		in.needIndent = true
	case ',':
		return indentLine(append(dst, c), in.depth)
	case ':':
		return append(dst, c, ' ')
	case '}', ']':
		if in.needIndent {
			// An empty object or array stays on its line.
			in.needIndent = false
		} else {
			in.depth--
			dst = indentLine(dst, in.depth)
		}
	}
	return append(dst, c)
}

// inside appends c, a byte of a string, as it is, and notes where the string
// ends.
func (in *indenter) inside(dst []byte, c byte) []byte {
	switch {
	case in.escaped:
		in.escaped = false
	case c == '\\':
		in.escaped = true
	case c == '"':
		in.inString = false
	}
	return append(dst, c)
}

// indentLine starts a line at depth.
func indentLine(dst []byte, depth int) []byte {
	dst = append(dst, '\n')
	for range depth {
		dst = append(dst, ' ', ' ')
	}
	return dst
}

// leavesWhy and leavesFix explain a state path that resolves outside the
// repository.
const (
	leavesWhy = "it leads outside the repository through a link, and the loop keeps its state inside it"
	leavesFix = "replace the link with a directory of the same name in the repository"
)

// leaves reports whether path, with every link on the way to it followed, is
// outside the repository. A clone brings links with everything else: with
// .sdlc/state committed as a link to another directory, the stop hook wrote its
// count into that directory, and every other write of the loop state would have
// followed it there.
func (s *Store) leaves(path string) bool {
	_, outside := pathrules.Rel(s.rootOf(path), path)
	return outside
}

// rootOf is the checkout a path the store builds belongs to: this one, or the
// main checkout for the backlog and the story records.
func (s *Store) rootOf(path string) string {
	if within(s.root, path) {
		return s.root
	}
	if shared, err := s.shared(); err == nil && within(shared, path) {
		return shared
	}
	return s.root
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// writeFile writes atomically, so an interrupted write leaves the old file
// intact rather than a truncated one. These files are the loop's memory: a
// half-written record is worse than no record, because the loop would read it
// and believe it.
func (s *Store) writeFile(path string, data []byte) error {
	if s.leaves(path) {
		return sdlcerr.New(sdlcerr.StateUnwritable, relative(s.root, path)+couldNotBeWritten, leavesWhy).
			WithFix(leavesFix)
	}
	s.reads.Forget(path)
	info, err := fsx.WriteFileAtomicInfo(path, data, 0o644)
	if err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			relative(s.root, path)+couldNotBeWritten,
			"its directory may not be writable, or the disk may be full").WithCause(err)
	}
	s.reads.keepWritten(path, data, info)
	return nil
}

// replaceBacklog is writeFile for the one file that is the user's rather than
// the loop's. Replacing it changes its bytes and nothing else about it. A
// backlog that is a link is written where the link points: renaming over the
// link replaced it with a copy, and the file it pointed to never changed. And
// it keeps the permissions it was given, rather than becoming readable by
// everybody.
func (s *Store) replaceBacklog(path string, data []byte) error {
	target, perm := path, fs.FileMode(0o644)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	if info, err := os.Stat(target); err == nil {
		perm = info.Mode().Perm()
	}
	s.reads.Forget(path, target)
	written, err := fsx.WriteFileAtomicInfo(target, data, perm)
	if err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			relative(s.root, path)+couldNotBeWritten,
			"its directory may not be writable, or the disk may be full").WithCause(err)
	}
	s.reads.keepWritten(path, data, written)
	return nil
}

// ---------------------------------------------------------------- helpers

// quote escapes as well as quotes: a value read from the backlog can hold a
// quote or a newline, and one that ended the quotation would read as sdlc's.
func quote(s string) string { return strconv.Quote(s) }

// relative shows a path as the user typed it: relative to the repository root
// where possible, absolute when it points somewhere else entirely.
func relative(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return filepath.ToSlash(rel)
}

// describeBacklog says what is in the backlog, so "not found" can be acted on
// without a second command.
func describeBacklog(b *model.Backlog) string {
	ids := b.IDs()
	const shown = 8
	switch {
	case len(ids) == 0:
		return "the backlog has no stories in it"
	case len(ids) <= shown:
		return "the backlog has " + strings.Join(ids, ", ")
	default:
		rest := len(ids) - shown
		noun := " others"
		if rest == 1 {
			noun = " other"
		}
		return "the backlog has " + strings.Join(ids[:shown], ", ") +
			" and " + strconv.Itoa(rest) + noun
	}
}
