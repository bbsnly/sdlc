package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bbsnly/sdlc/internal/fsx"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// An observation is what the hook writes down when it allows a command that
// commits now: the HEAD the session was on as the command went through, which
// is the first parent of the commit it is about to make. The commit gate reads
// the story's observations back to tell its commits from anybody else's. It is
// an observation, never a record: the record is gate-record.json.
//
// Observations live in the repository's git directory, at
// <git-dir>/sdlc/commits/<story>/<head>, and not under .sdlc/. Everything in
// the working tree is seen by `git status`, and the commit gate refuses a tree
// with anything uncommitted in it (SDLC-E0032), so an observation there trapped
// a finished story on the loop's note to itself. The git directory is never
// committed and never pushed, and .git is protected from the session by both
// the file rules and the shell rules.
//
// This writes through fsx rather than writeFile, so it steps round writeFile's
// guard that the loop keeps its state inside the repository: a linked
// worktree's git directory is <main>/.git/worktrees/<name>, which can be
// outside the project root. Three things stand in for that guard. The story id
// passes CheckID before it is part of a path; the file name is a head of forty
// lower-case hexadecimal characters, checked before it is part of a path; and
// the directory is git's own answer rather than anybody's text -- which is only
// as trustworthy as .git itself, since a `gitdir:` pointer makes git name a
// directory elsewhere.
//
// The loop now keeps state in two roots, so anything that later enumerates
// what the loop knows about a story -- sdlc doctor, an audit, sizing per
// commit -- has to look here as well as under .sdlc/. And a linked worktree
// has its own git directory, so a story worked from two worktrees of one
// repository has two sets of observations, and a gate run in either sees only
// its own.

// commitsDir is where observations go, under the git directory.
const commitsDir = "sdlc/commits"

// observationSchema versions an observation, so that one written by a later
// version reads as nothing rather than as evidence.
const observationSchema = "sdlc/commit-observation/1"

// observation is the whole of what is written down: which commits are a
// story's is answered by shas and a story id, and a command line is exactly
// where a path, a token or a shape nobody wants kept would arrive.
type observation struct {
	Schema string `json:"schema"`
	Story  string `json:"story"`
	Head   string `json:"head"`
	At     string `json:"at"`
}

// ObserveCommit writes down that the story's session was about to commit on
// head. Writing the same head twice is not a failure: an observation is a fact
// about a head, not a count, and the file is written only when it is absent.
func (s *Store) ObserveCommit(ctx context.Context, id, head string) error {
	dir, err := s.observationDir(ctx, id)
	if err != nil {
		return err
	}
	// git's all-zero name for "no commits yet" passes the hexadecimal check and
	// is never anybody's first parent.
	if !isHead(head) || head == gitx.NoCommits {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"the commit "+quote(head)+" could not be written down",
			"what the loop writes down is a commit's full name, forty hexadecimal characters")
	}
	path := filepath.Join(dir, head)
	if fsx.Exists(path) {
		return nil
	}
	raw, err := json.Marshal(observation{Schema: observationSchema, Story: id, Head: head,
		At: model.Timestamp(s.now())})
	if err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable, "the commit "+head+" could not be written down",
			"the observation could not be encoded as JSON").WithCause(err)
	}
	if err := fsx.WriteFileAtomic(path, append(raw, '\n'), 0o644); err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable, "the commit "+head+" could not be written down",
			"the repository's git directory may not be writable, or the disk may be full").WithCause(err)
	}
	return nil
}

// ObservedCommits is every head the loop watched the story's session commit
// on, in no particular order. It reads the story's own directory and nothing
// else. The gate believes what it returns, so anything it cannot recognise --
// a leftover temporary file, a body that will not parse, a format this version
// has not heard of, another story's observation, a body that names a head
// other than its file's -- reads as nothing rather than as evidence. It
// returns the file's name, which is the one that was checked.
func (s *Store) ObservedCommits(ctx context.Context, id string) ([]string, error) {
	dir, err := s.observationDir(ctx, id)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, sdlcerr.New(sdlcerr.StateUnreadable,
			"the commits the loop watched for "+id+" could not be read",
			"the repository's git directory could not be listed").WithCause(err)
	}
	var heads []string
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !isHead(name) || name == gitx.NoCommits {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var o observation
		if json.Unmarshal(raw, &o) != nil || o.Schema != observationSchema || o.Story != id || o.Head != name {
			continue
		}
		heads = append(heads, name)
	}
	return heads, nil
}

// ClearObservedCommits forgets everything the loop watched for the story.
// Clearing nothing is not a failure: an iteration ends whether anything was
// watched or not.
func (s *Store) ClearObservedCommits(ctx context.Context, id string) error {
	dir, err := s.observationDir(ctx, id)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return sdlcerr.New(sdlcerr.StateUnwritable,
			"the commits the loop watched for "+id+" could not be forgotten",
			"the repository's git directory may not be writable").WithCause(err)
	}
	return nil
}

// observationDir is the story's directory of observations. The id is checked
// before it is part of a path: it arrives on the hook's commit path from a
// state file.
func (s *Store) observationDir(ctx context.Context, id string) (string, error) {
	if err := CheckID(id); err != nil {
		return "", err
	}
	gitDir, err := gitx.GitDir(ctx, s.root)
	if err != nil {
		return "", err
	}
	return filepath.Join(gitDir, filepath.FromSlash(commitsDir), id), nil
}

// isHead reports whether name is a commit's full name as git prints it.
func isHead(name string) bool {
	if len(name) != 40 {
		return false
	}
	for _, c := range name {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
