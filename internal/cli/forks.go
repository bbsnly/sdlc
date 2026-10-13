package cli

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// The story's own commits. A test file is trunk's own where trunk changed it
// under the commit the story sits on, and that commit is where the story's
// branch meets trunk's tip. A person who pointed trunk's branch at a commit of
// the story's own that was not its tip moved it: every test file at or under
// that commit read as trunk's, the commit gate printed "accepted as" for a test
// added after the freeze, and the stop landed it. The commits themselves cannot
// say whose they were, because a story put onto a commit trunk gained has the
// same shape. So the record keeps a list, written while the two can still be
// told apart, and the freeze is judged from under whatever the list names.

// forksOf is the story's list of its own commits, and nothing where the record
// has none that reads whole: no value, one of another kind, an empty list, or
// an entry that names no trunk commit. A list read in part would be trusted for
// what was left of it.
func forksOf(record *model.Record) []model.Fork {
	var forks []model.Fork
	if json.Unmarshal(record.Forks, &forks) != nil || slices.ContainsFunc(forks, func(f model.Fork) bool { return f.From == "" }) {
		return nil
	}
	return forks
}

// setForks puts the list on the record.
func setForks(record *model.Record, forks []model.Fork) {
	if raw, err := json.Marshal(forks); err == nil {
		record.Forks = raw
	}
}

// noteFork is the list with an entry for the branch at tip: the trunk commit it
// sits on, and its lowest commit above that. The list comes back as it was
// where trunk holds tip, where the two cannot be told, and where the list has
// the entry already.
//
// An entry is never replaced while git still has its commit. Replaced by the
// entry for the same trunk commit, a commit that a gate had seen and that was
// then made again was forgotten, and trunk's branch pointed at it afterwards
// made its test files trunk's. One git no longer has is dropped: it cannot be
// at or under a commit trunk holds, so it has nothing left to say, and the
// list stays as long as what git keeps.
func noteFork(ctx context.Context, s *store.Store, forks []model.Fork, tip string) ([]model.Fork, error) {
	from := sittingOn(ctx, s, tip)
	if from == "" || from == tip {
		return forks, nil
	}
	first, err := gitx.FirstAbove(ctx, s.Root(), from, tip)
	entry := model.Fork{From: from, First: first}
	if err != nil || first == "" || slices.Contains(forks, entry) {
		return forks, err
	}
	kept := slices.DeleteFunc(slices.Clone(forks), func(f model.Fork) bool {
		if f.From != from {
			return false
		}
		// The entry a list starts with names no commit, and goes with the
		// first that does. For any other, only git's own no drops it: a call
		// that failed says nothing about what git has.
		has, err := gitx.HasCommit(ctx, s.Root(), f.First)
		return f.First == "" || err == nil && !has
	})
	slog.Debug("noted commits as the story's own", "from", from, "first", first)
	return append(kept, entry), nil
}

// saveNoted is noteFork for a record that has a list, saved where the list
// grew. A record with no list gets none here: whatever is missing from it is
// missing for the commits made before now as well, and a list begun here would
// be trusted for them.
func saveNoted(ctx context.Context, s *store.Store, record *model.Record, tip string) error {
	forks := forksOf(record)
	if len(forks) == 0 {
		return nil
	}
	noted, err := noteFork(ctx, s, forks, tip)
	if err != nil || slices.Equal(noted, forks) {
		return err
	}
	setForks(record, noted)
	return s.SaveRecord(record)
}

// noteForks is saveNoted at a gate, for a story in its own worktree, before the
// gate checks anything: a gate that is refused has seen the story's commits as
// much as one that passes, and the commits a person then points trunk at are
// made after the last gate that passed. The record is saved as it was read,
// with the list and nothing else.
func noteForks(ctx context.Context, s *store.Store, rd *reading, id string, record *model.Record) error {
	if !inItsWorktree(s, id) {
		return nil
	}
	if len(forksOf(record)) == 0 {
		slog.Debug("the story's record has no list of its own commits, so no test file is taken as trunk's", "story", id)
		return nil
	}
	head, _ := rd.headOf(ctx)
	return saveNoted(ctx, s, record, head)
}

// restartForks starts the story's list again at a freeze that takes every test
// file in the worktree: from the commit frozen at, with the entry for the
// commits the branch holds already, so that the list is never without them.
// Nothing under those needs telling apart any longer, because every test file
// as it is now was frozen in a person's sight; a record that had no list has
// one from here. Where HEAD or the entry cannot be read the record is left with
// no list, which takes nothing as trunk's.
//
// The list is left exactly as it was where the freeze last lifted was lifted by
// the stop that finished the story, and not by a person. A story that landed
// and kept its worktree can be reopened there and frozen again by a session
// alone, and that freeze would have reset a list, or given one to a record
// that had none, with no person's act.
func restartForks(ctx context.Context, s *store.Store, id string, record *model.Record) {
	if !inItsWorktree(s, id) || liftedByTheStop(record) {
		return
	}
	head, _ := gitx.Head(ctx, s.Root())
	forks, err := noteFork(ctx, s, []model.Fork{{From: head}}, head)
	record.Forks = nil
	if err == nil {
		setForks(record, forks)
	}
}

// liftedByTheStop reports whether the story's freeze was last lifted by the
// stop that finished it. The record's unfreeze events are written by two
// commands only: `sdlc unfreeze`, a person's, with the reason they give, and
// the stop, with its own sentence. One with any other sentence is a person's.
func liftedByTheStop(record *model.Record) bool {
	for _, e := range slices.Backward(record.Events) {
		if e.Type == unfreezeEvent {
			return e.Message == finishedUnfreeze
		}
	}
	return false
}

// under reports whether git has commit and it is at or under on. A commit git
// no longer has is a no: nothing at or under a commit trunk holds is ever
// pruned, so it is no evidence either way. A call that failed is not that, and
// is returned: read as a no, one failed call gave the answer from before there
// was a list.
func under(ctx context.Context, root, commit, on string) (bool, error) {
	if commit == "" {
		return false, nil
	}
	has, err := gitx.HasCommit(ctx, root, commit)
	if err == nil && has {
		has, err = gitx.IsAncestor(ctx, root, commit, on)
	}
	return has, err
}

// trunkPart is the entry that says where trunk's own commits end under on, the
// trunk commit a story sits on: of the entries whose commit on holds, the one
// noted on the lowest trunk commit. It has no From where the list names nothing
// on holds. Every entry is asked: judged by the last alone, a commit noted
// before a later one was forgotten.
func trunkPart(ctx context.Context, root string, forks []model.Fork, on string) (model.Fork, error) {
	var low model.Fork
	for _, f := range forks {
		lower, err := under(ctx, root, f.First, on)
		if err == nil && lower && low.From != "" {
			lower, err = gitx.IsAncestor(ctx, root, f.From, low.From)
		}
		if err != nil {
			return model.Fork{}, err
		}
		if lower {
			low = f
		}
	}
	return low, nil
}
