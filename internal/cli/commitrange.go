package cli

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// landing is the range a commit pass recorded, as git reads it: the base in
// full, and the commits after it up to and including the record's end. Held is
// empty when there is no range, or none git could read.
type landing struct {
	base string
	held []gitx.Commit
}

// placeCommits works out, at the commit gate's pass, which commits are the
// story's, and writes the answer over whatever the last pass left: a range, or
// the marker that says deliberately there is none. head is HEAD, which names a
// commit.
//
// The gate recorded HEAD, so every commit anybody made between the code review
// and the gate was the story's -- US-004's record named a chore made after the
// work. The hook now writes down the head the session was on each time it
// allowed a commit (store.ObserveCommit), and the story's commits are the ones
// made on a head it watched.
//
// What this checks is that the pair it chooses is internally consistent, never
// that it holds everything it should. A commit the hook did not watch is
// missing from either end of the range and leaves no sign, an observation
// somebody forged is believed, and history rewritten under the loop can hand a
// person's commit to the story. The docs say each of those in so many words.
func placeCommits(ctx context.Context, s *store.Store, id string, record *model.Record, head string) landing {
	record.RangeUnknown, record.HeadPastCommit = false, ""
	observed, err := s.ObservedCommits(ctx, id)
	if record.CommitBase == "" || err == nil && len(observed) == 0 {
		// Nothing watched: a project without the hook, enforcement off, or a
		// story already in flight when this version shipped. What the released
		// tool records, the whole window, rather than "unknown": answered
		// wherever the hook is absent, unknown is never already read, and
		// `sdlc log` would list every such story for ever.
		record.Commit = head
		if record.CommitBase == "" {
			// The record names head and nothing before it, so head is the
			// range a note is held to: returned empty, a note could name any
			// commit at all beside the one the record names (AC-4).
			return landing{held: []gitx.Commit{{Hash: head}}}
		}
		base, window, _ := windowOf(ctx, s.Root(), record.CommitBase, head)
		return landing{base: base, held: window}
	}
	if err == nil {
		if start, story, ok := watchedRange(ctx, s.Root(), record.CommitBase, head, observed); ok {
			record.CommitBase, record.Commit = start, story[0].Hash
			if record.Commit != head {
				record.HeadPastCommit = head
			}
			return landing{base: start, held: story}
		}
	}
	// CommitBase is left as it is, which is the code review's HEAD unless an
	// earlier pass recorded a range and moved it to that range's start: a later
	// pass works out the story's commits from there.
	record.Commit, record.RangeUnknown = "", true
	return landing{}
}

// noSingleRange is what a record with no range says about it, wherever it is
// printed. It is not the older unknown, a record that names no commit at all.
const noSingleRange = "the loop watched this story's commits, but no single range holds only them"

// windowOf is every commit after the record's base up to and including end.
// The base is text in the record, which an older sdlc or a person could have
// written, so only what git prints for it reaches git again. A base git cannot
// read is a story whose commits cannot be worked out, not a refusal at the
// last gate on work already committed.
func windowOf(ctx context.Context, root, commitBase, end string) (string, []gitx.Commit, bool) {
	base, err := gitx.Resolve(ctx, root, commitBase)
	if err != nil {
		return "", nil, false
	}
	window, err := gitx.CommitsBetween(ctx, root, base, end)
	return base, window, err == nil
}

// watchedRange is the one pair of commits that holds exactly the commits the
// loop watched the session land, and those commits, newest first; or not ok
// where no pair does.
func watchedRange(ctx context.Context, root, commitBase, head string,
	observed []string) (start string, story []gitx.Commit, ok bool) {
	_, window, ok := windowOf(ctx, root, commitBase, head)
	if !ok {
		return "", nil, false
	}

	// S is every commit in the window whose FIRST parent is a head the loop
	// watched. Not a walk and not a run: each commit is tested on its own, and
	// whether one is in does not depend on any other. Both walks fail a frozen
	// test. Walked back from HEAD, stopping at the first commit whose parent
	// was not watched, a person's commit after the story's stops it at once
	// (TestAC1TheRangeEndsAtTheLastCommitTheLoopWatchedAndNotAtHead). Walked
	// back from the newest commit whose parent was, a person's commit between
	// two of the story's leaves a range that verifies and hides the first
	// (TestAC3CommitsWatchedWithSomebodyElsesBetweenThemAreRecordedAsNoRange).
	// The heads are compared as strings and never become arguments.
	watched := map[string]bool{}
	for _, h := range observed {
		watched[h] = true
	}
	for _, c := range window {
		if watched[c.Parent] {
			story = append(story, c)
		}
	}
	if len(story) == 0 {
		return "", nil, false
	}
	// A root commit has no parent and so no watched one: the start is always
	// a commit.
	start, end := story[len(story)-1].Parent, story[0].Hash

	// Put the pair back to git before it is believed. A merge the session made
	// has the branch it was made on as its first parent, so every commit of
	// the merged side branch sits inside the pair; and rev-list orders by date,
	// so "between" over a history that is not a line is not what it looks
	// like. What git lists over the pair has to be exactly S, as a set, or no
	// pair holds only the story's commits.
	held, err := gitx.CommitsBetween(ctx, root, start, end)
	if err != nil || len(held) != len(story) {
		return "", nil, false
	}
	inStory := map[string]bool{}
	for _, c := range story {
		inStory[c.Hash] = true
	}
	for _, c := range held {
		if !inStory[c.Hash] {
			return "", nil, false
		}
	}
	return start, story, true
}

// hexRun is what might be a commit in a note: seven to forty hexadecimal
// characters standing on their own.
var hexRun = regexp.MustCompile(`\b[0-9a-fA-F]{7,40}\b`)

// maxNoteShas bounds the git calls one note can cause. A note is written by a
// model, and one full of hexadecimal words would otherwise turn the last gate
// into thousands of subprocesses. Past it the rest are ignored, never refused:
// a long note is not an error.
const maxNoteShas = 24

// requireNoteInRange refuses a note that names a commit the range just
// recorded does not hold (AC-4). The note and the range both say which commits
// landed, and US-004's record said two things: its note named the commit that
// carried the work, and its range ended at a chore made afterwards. Any commit
// in the range will do, not only its end.
//
// It is drawn narrowly, because it falls at the last gate on work already
// committed: a run git has never heard of is prose, and so is a branch or tag
// that happens to be spelled in hexadecimal, since a sha is a prefix of the
// hash git resolves it to. It runs only when there is a range git could read,
// so it is a consistency check between the note and the record, never evidence
// of which commits are the story's. The note's text is never echoed.
func requireNoteInRange(ctx context.Context, root string, landed landing, note string) error {
	if len(landed.held) == 0 {
		return nil
	}
	var runs []string
	for _, run := range hexRun.FindAllString(note, -1) {
		// git prints object names in lower case and resolves either, so an
		// upper-case sha compared as written would read as prose.
		run = strings.ToLower(run)
		if !slices.Contains(runs, run) {
			runs = append(runs, run)
		}
		if len(runs) == maxNoteShas {
			break
		}
	}
	for _, run := range runs {
		if heldBy(landed.held, run) {
			continue
		}
		commit, err := gitx.Resolve(ctx, root, run)
		if err != nil || !strings.HasPrefix(commit, run) {
			continue
		}
		where := "the range " + shortCommit(landed.base) + ".." + shortCommit(landed.held[0].Hash)
		if landed.base == "" {
			where = "the record's commit " + shortCommit(landed.held[0].Hash)
		}
		return sdlcerr.New(sdlcerr.NoteOutsideRange,
			"the note names "+shortCommit(commit)+", which "+where+" does not hold",
			"the commit gate's record and its note must agree about which commits landed")
	}
	return nil
}

// heldBy reports whether sha abbreviates one of commits.
func heldBy(commits []gitx.Commit, sha string) bool {
	for _, c := range commits {
		if strings.HasPrefix(c.Hash, sha) {
			return true
		}
	}
	return false
}
