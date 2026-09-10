// Package store owns everything the loop keeps on disk.
//
// Two rules run through it. Every write is atomic -- a temporary file beside
// the target, then a rename -- so an interrupted run can never leave a record
// half-written and unparseable. And every story id is checked before it becomes
// part of a path, because ids come from a JSON file that an assistant may have
// written, and "../../.." is a perfectly good JSON string.
package store

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// Paths under the repository root, relative and slash-separated. They are the
// same paths the shell kit used, so an existing project needs no migration.
const (
	stateDir   = config.Dir + "/state"
	storiesDir = config.Dir + "/stories"
	activeFile = stateDir + "/active"
	recordFile = model.RecordFile
)

// safeID is what a story id may contain, given that it becomes a directory
// name. It is deliberately narrower than "what a filesystem accepts": the
// backlog schema asks for AUTH-3, and anything in this set is safe on Windows,
// macOS and Linux alike.
var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Store reads and writes one project's loop state.
type Store struct {
	root string
	cfg  config.Config
	now  func() time.Time
}

// New opens the store for a project.
func New(p *config.Project) *Store {
	return &Store{root: p.Root, cfg: p.Config, now: time.Now}
}

// WithClock returns a copy that reads the time from now, for tests and for any
// caller that needs every timestamp in one operation to match.
func (s *Store) WithClock(now func() time.Time) *Store {
	out := *s
	out.now = now
	return &out
}

// Root is the repository root the store writes under.
func (s *Store) Root() string { return s.root }

// CheckID rejects a story id that cannot safely become a directory name.
func CheckID(id string) error {
	if safeID.MatchString(id) && id != "." && id != ".." {
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
	return filepath.Join(s.root, filepath.FromSlash(storiesDir), id), nil
}

// ---------------------------------------------------------------- backlog

// Backlog reads the project's stories.
func (s *Store) Backlog() (*model.Backlog, error) {
	path := s.cfg.BacklogPath(s.root)
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, sdlcerr.New(sdlcerr.BacklogMissing,
			"there is no backlog to read",
			"backlog.path in "+config.File+" names "+relative(s.root, path)+", which does not exist")
	case err != nil:
		return nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
			relative(s.root, path)+" could not be read",
			"opening it failed").WithCause(err)
	}

	var b model.Backlog
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
			relative(s.root, path)+" is not valid JSON",
			"the loop reads every story from this file, so it stops rather than guess").WithCause(err)
	}
	for i, story := range b.Stories {
		switch {
		case story.ID == "":
			return nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
				"a story in "+relative(s.root, path)+" has no id",
				"it is story number "+strconv.Itoa(i+1)+" in the file, and every story needs an id")
		case story.Title == "":
			return nil, sdlcerr.New(sdlcerr.BacklogUnreadable,
				"the story "+quote(story.ID)+" has no title",
				"every story needs a title: it is what the commit message and the retro refer to")
		}
		if err := CheckID(story.ID); err != nil {
			return nil, err
		}
	}
	return &b, nil
}

// SaveBacklog writes the stories back, preserving the file's own fields.
func (s *Store) SaveBacklog(b *model.Backlog) error {
	return s.writeJSON(s.cfg.BacklogPath(s.root), b)
}

// Story reads one story from the backlog.
func (s *Store) Story(id string) (*model.Story, *model.Backlog, error) {
	b, err := s.Backlog()
	if err != nil {
		return nil, nil, err
	}
	found, ok := b.Find(id)
	if !ok {
		return nil, nil, sdlcerr.New(sdlcerr.StoryNotFound,
			"no story with the id "+quote(id),
			describeBacklog(b))
	}
	return found, b, nil
}

// SetStoryStatus moves a story to a new status and saves the backlog.
func (s *Store) SetStoryStatus(id string, status model.Status) error {
	found, b, err := s.Story(id)
	if err != nil {
		return err
	}
	found.Status = status
	found.Updated = model.Timestamp(s.now())
	return s.SaveBacklog(b)
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
			activeFile+" exists but could not be opened").WithCause(err)
	}
	id := strings.TrimSpace(string(raw))
	if id == "" {
		return "", nil
	}
	if err := CheckID(id); err != nil {
		return "", err
	}
	return id, nil
}

// SetActive marks a story as the one being worked on.
func (s *Store) SetActive(id string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	return s.writeFile(filepath.Join(s.root, filepath.FromSlash(activeFile)), []byte(id+"\n"))
}

// ClearActive ends the iteration.
func (s *Store) ClearActive() error {
	path := filepath.Join(s.root, filepath.FromSlash(activeFile))
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"the iteration could not be ended",
			activeFile+" could not be removed").WithCause(err)
	}
	return nil
}

// ---------------------------------------------------------------- record

// Record reads a story's gate record, starting a fresh one if there is none.
func (s *Store) Record(id string) (*model.Record, error) {
	dir, err := s.StoryDir(id)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, recordFile))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return model.NewRecord(id, s.now()), nil
	case err != nil:
		return nil, sdlcerr.New(sdlcerr.StateUnreadable,
			"the record for "+quote(id)+" could not be read",
			storiesDir+"/"+id+"/"+recordFile+" exists but could not be opened").WithCause(err)
	}

	var r model.Record
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, sdlcerr.New(sdlcerr.StateUnreadable,
			"the record for "+quote(id)+" is not valid JSON",
			"this file is written by sdlc, so a corrupt one is a bug rather than an edit").WithCause(err)
	}
	if r.Gates == nil {
		r.Gates = map[model.Gate]model.GateResult{}
	}
	r.Story = id
	return &r, nil
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
// and returns the path it was written to, relative to the repository root.
func (s *Store) WriteArtifact(story string, a model.Artifact, content []byte) (string, error) {
	dir, err := s.StoryDir(story)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, a.File)
	if err := s.writeFile(path, content); err != nil {
		return "", err
	}
	return relative(s.root, path), nil
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
func (s *Store) writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			relative(s.root, path)+" could not be written",
			"the value could not be encoded as JSON").WithCause(err)
	}
	return s.writeFile(path, append(data, '\n'))
}

// writeFile writes atomically, so an interrupted write leaves the old file
// intact rather than a truncated one. These files are the loop's memory: a
// half-written record is worse than no record, because the loop would read it
// and believe it.
func (s *Store) writeFile(path string, data []byte) error {
	if err := fsx.WriteFileAtomic(path, data, 0o644); err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			relative(s.root, path)+" could not be written",
			"its directory may not be writable, or the disk may be full").WithCause(err)
	}
	return nil
}

// ---------------------------------------------------------------- helpers

func quote(s string) string { return `"` + s + `"` }

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
