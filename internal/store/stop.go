package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// stopFile counts the stops the Stop hook has sent back on the story being
// worked on.
const stopFile = stateDir + "/stop-blocks.json"

// StopCount is how many times in a row the session has tried to stop mid-story
// with nothing new on the story's record. Progress is the record by content when
// the count was taken, so recording anything on the story starts it again.
//
// WaitingSince is when the first stop at this progress was let through because
// a subagent was still running: RFC 3339 text in UTC, and left out of the file
// until there is one. The store keeps the text as it was given and reads
// nothing into it. A value that is no time at all has to reach the hook as it
// is: read here as "no wait begun", it would start a new wait where the hook
// counts the stop.
type StopCount struct {
	Story        string `json:"story"`
	Blocks       int    `json:"blocks"`
	Progress     string `json:"progress"`
	WaitingSince string `json:"waiting_since,omitempty"`
}

// StopCount reads the count. One that is missing or will not read counts
// nothing, and the next count written replaces it. So does one below zero,
// which no stop wrote: the count only grows from there, and a count far enough
// below zero never reached the limit, so the story was never handed over.
func (s *Store) StopCount() StopCount {
	var c StopCount
	raw, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(stopFile)))
	if err != nil || json.Unmarshal(raw, &c) != nil || c.Blocks < 0 {
		return StopCount{}
	}
	return c
}

// SaveStopCount writes the count.
func (s *Store) SaveStopCount(c StopCount) error {
	return s.writeJSON(filepath.Join(s.root, filepath.FromSlash(stopFile)), c)
}

// ClearStopCount removes the count.
func (s *Store) ClearStopCount() error {
	path := filepath.Join(s.root, filepath.FromSlash(stopFile))
	if s.leaves(path) {
		return sdlcerr.New(sdlcerr.StateUnwritable, stopFile+" could not be removed", leavesWhy).WithFix(leavesFix)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			stopFile+" could not be removed",
			"the loop could not reset the count of stops it sent back").WithCause(err)
	}
	return nil
}

// RecordProgress is the story's gate record by content. Every command that
// moves a story writes to its record, so a different answer means something
// happened.
func (s *Store) RecordProgress(id string) (string, error) {
	dir, err := s.StoryDir(id)
	if err != nil {
		return "", err
	}
	return hashFile(filepath.Join(dir, recordFile), storiesDir+"/"+id+"/"+recordFile,
		"the story's gate record could not be opened; check that gate-record.json "+
			"is there and that you may read it")
}

// Escalate hands the story to a person: the question goes on its record, bound
// to the work as it stands, and the story becomes awaiting_human. When it is
// the story being worked on, the iteration ends; handing over another story
// leaves the iteration alone. It returns the tree the question was asked about.
//
// The caller holds the project's lock.
func (s *Store) Escalate(ctx context.Context, id, kind, message string) (string, error) {
	// The backlog is read before anything is written. A story it no longer
	// holds cannot be marked as waiting for a person, and finding that out
	// after the record was saved left a question on it that nothing listed,
	// with the iteration still running.
	if _, _, err := s.Story(id); err != nil {
		return "", err
	}
	// Another checkout's story is measured on that checkout's tree, and its
	// question asked there.
	if h, elsewhere, err := s.HeldElsewhere(id); err != nil {
		return "", err
	} else if elsewhere {
		return "", HeldElsewhereError(id, h, "a story is handed to a person from the checkout working it")
	}
	tree, err := s.ReviewSubject(ctx)
	if err != nil {
		return "", err
	}
	record, err := s.Record(id)
	if err != nil {
		return "", err
	}
	record.Escalate(kind, message, tree, s.Now())
	record.Append("escalate", kind+": "+message, s.Now())
	if err := s.SaveRecord(record); err != nil {
		return "", err
	}
	if err := s.SetStoryStatus(id, model.StatusAwaitingHuman); err != nil {
		return "", err
	}
	active, err := s.Active()
	if err != nil {
		return "", err
	}
	if active == id {
		// The story's uncommitted work is in this checkout, and after a person's
		// answer the story resumes here.
		if err := s.keepClaim(id); err != nil {
			return "", err
		}
		if err := s.ClearActive(); err != nil {
			return "", err
		}
	}
	return tree, nil
}
