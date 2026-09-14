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
type StopCount struct {
	Story    string `json:"story"`
	Blocks   int    `json:"blocks"`
	Progress string `json:"progress"`
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
	if err := CheckID(id); err != nil {
		return "", err
	}
	return s.HashFile(storiesDir + "/" + id + "/" + recordFile)
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
		if err := s.ClearActive(); err != nil {
			return "", err
		}
	}
	return tree, nil
}
