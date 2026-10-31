package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// MaxDocument bounds what will be stored. These are documents a person reads,
// not data files, and a runaway agent should not be able to fill a repository.
const MaxDocument = 1 << 20

// plansDir holds a story's kept revisions of its plan, each named by the SHA-256
// of its bytes: the same hash a design review's subject holds.
const plansDir = "plans"

const (
	revisionNotKeptWhat = "there is no diff for this reviewer, which reads the whole plan this round"
	revisionNotUsable   = "the revision its review was stamped with is not usable: "

	// A plan stored before revisions were kept is the ordinary case of this,
	// for a story in flight across the upgrade, and it wants nothing looked into.
	revisionsWereNotKept = "; a plan stored before revisions were kept has none"

	// The tool wrote this file with bytes that match its name, so something else
	// changed it, and the diff it would have given could have hidden a change
	// from a blocking reviewer. Nothing else records that this happened, so unlike
	// every other cause, starting the reviewer afresh is not all there is to do.
	revisionWasChangedFix = "give this reviewer the whole plan this round: start it afresh with the path to PLAN.md, and its verdict counts the same. " +
		"Something may have changed the file after it was stored, so tell a person"
)

// ContentHash is the hash a plan is reviewed and kept under.
func ContentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// IsPlanRevision reports whether a review's subject can name a kept revision: 64
// lower-case hex characters and nothing else. A subject comes from a record a
// clone can bring, and "../../../net/host/x" joined to a directory would reach
// any .md file on the machine, on a network mount among them, before the hash
// comparison could refuse it.
func IsPlanRevision(subject string) bool {
	return len(subject) == sha256.Size*2 && strings.Trim(subject, "0123456789abcdef") == ""
}

func planRevisionPath(storyDir, hash string) string {
	return filepath.Join(storyDir, plansDir, hash+".md")
}

// keepPlanRevision writes a plan's revision every time, without comparing it,
// because whatever is at the name now is not to be trusted. Only a real directory
// will do at plans: leaves stops a link out of the repository, and a link staying
// inside it put the plan's bytes, when a reviewer tried one, in a directory the
// hook protects because a session loads what is in it.
func (s *Store) keepPlanRevision(storyDir string, content []byte) error {
	plans := filepath.Join(storyDir, plansDir)
	if info, err := os.Lstat(plans); err == nil && !info.IsDir() {
		return sdlcerr.New(sdlcerr.StateUnwritable, relative(s.rootOf(plans), plans)+couldNotBeWritten,
			"it is a link or a file, and a plan's revisions are kept only in a directory of that name").
			WithFix("take away what stands there and store the plan again; sdlc makes the directory")
	}
	return s.writeFile(planRevisionPath(storyDir, ContentHash(content)), content)
}

// PlanRevision is the plan a review was stamped with, read back from its kept
// revision. Any agent can write that directory, and the check of the bytes
// against the name is the whole defence: a reviewer shown a diff from anything
// else approves what it was never shown.
func (s *Store) PlanRevision(story, subject string) ([]byte, error) {
	dir, err := s.StoryDir(story)
	if err != nil {
		return nil, err
	}
	if !IsPlanRevision(subject) {
		return nil, planRevisionNotKept("its review is stamped with no plan revision")
	}
	content, why, cause := ReadBounded(planRevisionPath(dir, subject))
	if why != "" {
		if errors.Is(cause, fs.ErrNotExist) {
			why += revisionsWereNotKept
		}
		return nil, planRevisionNotKept(revisionNotUsable + why).WithCause(cause)
	}
	if ContentHash(content) != subject {
		return nil, planRevisionNotKept(revisionNotUsable + "the file no longer holds the bytes its name is the hash of").
			WithFix(revisionWasChangedFix)
	}
	return content, nil
}

// planRevisionNotKept says nothing of the file or of the subject: the subject
// came from a record, and neither helps a person more than the reason does.
func planRevisionNotKept(why string) *sdlcerr.Error {
	return sdlcerr.New(sdlcerr.PlanRevisionNotKept, revisionNotKeptWhat, why)
}

// ReadBounded reads a regular file of at most MaxDocument bytes. On a refusal it
// returns no content, the reason in words a person can read, and what caused it
// where something did. It returns words and not an error because its two callers
// refuse under different codes.
//
// Lstat and not Stat, so that a link is refused and not followed, and before the
// open, because opening a FIFO blocks until something writes to it.
func ReadBounded(path string) (content []byte, why string, cause error) {
	info, err := os.Lstat(path)
	if err != nil {
		// A directory that cannot be searched fails here too, and has a file in it
		// for all anyone can tell.
		why = "it could not be opened"
		if errors.Is(err, fs.ErrNotExist) {
			why = "there is no file there"
		}
		return nil, why, err
	}
	if !info.Mode().IsRegular() {
		return nil, "it is not a regular file", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "it could not be opened", err
	}
	defer f.Close()

	content, err = io.ReadAll(io.LimitReader(f, MaxDocument+1))
	if err != nil || len(content) > MaxDocument {
		return nil, "it could not be read to the end, or is larger than one document may be", err
	}
	return content, "", nil
}
