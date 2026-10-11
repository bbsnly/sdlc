package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/bbsnly/sdlc/internal/fsx"
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
// An escalation that is refused leaves the record, the backlog and the
// iteration as they were (see HandOver).
//
// The caller holds the project's lock.
func (s *Store) Escalate(ctx context.Context, id, kind, message string) (string, error) {
	return s.HandOver(ctx, HandOver{Story: id, Kind: kind, Message: message})
}

// ReviewDocument is a reviewer's document a command stores with its record.
type ReviewDocument struct {
	Gate    model.Gate
	Role    string
	Round   int
	Content []byte
}

// HandOver is a story going to a person. Record is the story's record as the
// command would have saved it, nil for the record as it is on disk; Review is
// the document that record lists, nil for none.
type HandOver struct {
	Story, Kind, Message string
	Record               *model.Record
	Review               *ReviewDocument
}

// HandOver hands the story to a person, as Escalate describes, and writes what
// the command was recording in the same save of the record as the question.
//
// It is all or nothing for a refusal. A gate failure used to be saved first and
// the story handed over second, so a hand-over that git or the backlog refused
// left the failure counted and nobody asked, and the command run again counted
// it a second time. Everything that can refuse without writing comes first, and
// what is written before a later step fails is put back from the bytes read
// here. That holds against an error, not against a process that is killed
// between two writes: the record is saved first so that what is left then is a
// question on the record, which `sdlc approve` can answer.
//
// The caller holds the project's lock, which is what makes putting bytes back
// safe: nothing else of the loop's wrote between the read and the write.
func (s *Store) HandOver(ctx context.Context, h HandOver) (string, error) {
	id := h.Story
	// The backlog is read before anything is written. A story it no longer
	// holds cannot be marked as waiting for a person, and finding that out
	// after the record was saved left a question on it that nothing listed,
	// with the iteration still running.
	backlogWas, b, err := s.readBacklog()
	if err != nil {
		return "", err
	}
	story, ok := b.Find(id)
	if !ok {
		return "", storyNotFound(b, id)
	}
	// Another checkout's story is measured on that checkout's tree, and its
	// question asked there.
	if holder, elsewhere, err := s.HeldElsewhere(id); err != nil {
		return "", err
	} else if elsewhere {
		return "", HeldElsewhereError(id, holder, "a story is handed to a person from the checkout working it")
	}
	tree, err := s.ReviewSubject(ctx)
	if err != nil {
		return "", err
	}
	onDisk, recordWas, err := s.readRecord(id)
	if err != nil {
		return "", err
	}
	record := h.Record
	if record == nil {
		record = onDisk
	}
	dir, err := s.StoryDir(id)
	if err != nil {
		return "", err
	}
	// The reviewer's document is read here, before the claim is written, so
	// that one that is there and cannot be read refuses with nothing to take
	// back. Read as no file, it would be removed by a take-back and not
	// restored. A path that leaves the repository is left to the write below,
	// which refuses it as it always has.
	var reviewPath string
	var reviewWas []byte
	if doc := h.Review; doc != nil {
		reviewPath = filepath.Join(dir, reviewsDir, reviewName(doc.Gate, doc.Role, doc.Round))
		reviewWas, err = os.ReadFile(reviewPath)
		if err != nil && !errors.Is(err, fs.ErrNotExist) && !s.leaves(reviewPath) {
			return "", sdlcerr.New(sdlcerr.StateUnreadable,
				"the review that is already at "+ReviewPath(id, doc.Gate, doc.Role, doc.Round)+" could not be read",
				"the file exists but could not be opened, and the hand-over has to be able to put it back as it was").
				WithFix("make the file readable, or move it aside if the story's record does not list it").
				WithCause(err)
		}
	}
	active, err := s.Active()
	if err != nil {
		return "", err
	}
	if active == id {
		// The story's uncommitted work is in this checkout, and after a person's
		// answer the story resumes here. The claim is written before the record
		// and is not taken back: it says what the active file already said.
		if err := s.keepClaim(id); err != nil {
			return "", err
		}
	}

	var done []written
	if doc := h.Review; doc != nil {
		if _, err := s.WriteReview(id, doc.Gate, doc.Role, doc.Round, doc.Content); err != nil {
			return "", err
		}
		done = append(done, written{"the reviewer's document", func() error { return s.putBack(reviewPath, reviewWas) }})
	}
	record.Escalate(h.Kind, h.Message, tree, s.Now())
	record.Append("escalate", h.Kind+": "+h.Message, s.Now())
	if err := s.SaveRecord(record); err != nil {
		return "", s.takenBack(id, err, done)
	}
	done = append(done, written{"the gate record", func() error { return s.putBack(filepath.Join(dir, recordFile), recordWas) }})
	if err := s.SetStoryStatus(id, model.StatusAwaitingHuman); err != nil {
		return "", s.takenBack(id, err, done)
	}
	// A story already waiting was not written, and is not written back.
	if story.Status != model.StatusAwaitingHuman {
		done = append(done, written{"the backlog", func() error { return s.replaceBacklog(s.BacklogPath(), backlogWas) }})
	}
	if active == id {
		// Ending the iteration removes the session and then the active file, and
		// the second can fail after the first: a file another process holds open
		// on Windows. The session is put back with the rest, where it went.
		sessionPath := filepath.Join(s.root, filepath.FromSlash(sessionFile))
		session, _ := os.ReadFile(sessionPath)
		done = append(done, written{"the session working the story", func() error {
			if session == nil || fsx.Exists(sessionPath) {
				return nil
			}
			return s.writeFile(sessionPath, session)
		}})
		if err := s.ClearActive(); err != nil {
			return "", s.takenBack(id, err, done)
		}
	}
	return tree, nil
}

// written is one thing a hand-over wrote, and how it is put back.
type written struct {
	what string
	back func() error
}

// takenBack undoes what a refused hand-over wrote, last first, and returns the
// refusal as it was: its code and its words are the ones the step that failed
// gave, whatever was written before it.
//
// It stops at the first thing it cannot put back, because what was written
// before that then agrees with what is left: a record that holds the question
// beside a backlog that says awaiting_human is a story `sdlc approve` answers.
func (s *Store) takenBack(id string, failed error, done []written) error {
	for i := len(done) - 1; i >= 0; i-- {
		if err := done[i].back(); err != nil {
			slog.Debug("a refused hand-over could not be taken back", "story", id, "what", done[i].what, "err", err)
			return partlyHandedOver{sdlcerr.New(sdlcerr.StateUnwritable,
				quote(id)+" is only partly handed over to a person",
				"the hand-over stopped part-way, and "+done[i].what+" could not be put back as it was, "+
					"so the story's record may hold a question for a person that the backlog or the "+
					"iteration does not show").
				WithFix(`do not run the command again yet: a person runs "sdlc status" to see where the story is, ` +
					`and answers a question the record holds with "sdlc approve"`).
				WithCause(errors.Join(failed, err))}
		}
	}
	return failed
}

// putBack makes the file at path hold what it held before a write that is being
// taken back, and removes it where there was none. It writes as the store
// writes everything, so a path that leads outside the repository is refused on
// the way back as it was on the way in; and what it removes is a file the same
// checked write has just made, under the lock, at this path.
func (s *Store) putBack(path string, was []byte) error {
	if was != nil {
		return s.writeFile(path, was)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			relative(s.root, path)+" could not be removed",
			"its directory may not be writable").WithCause(err)
	}
	return nil
}

// partlyHandedOver marks the one refusal of a hand-over after which the story
// is not as it was. It is a type and not a value built when the package loads,
// which the hook pays for on every tool call.
type partlyHandedOver struct{ err *sdlcerr.Error }

func (p partlyHandedOver) Error() string { return p.err.Error() }
func (p partlyHandedOver) Unwrap() error { return p.err }

// PartlyHandedOver reports whether err is a hand-over that could not be taken
// back: a caller that says nothing was recorded must not say it of this one.
func PartlyHandedOver(err error) bool {
	var p partlyHandedOver
	return errors.As(err, &p)
}
