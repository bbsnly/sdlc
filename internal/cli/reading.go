package cli

import (
	"context"
	"slices"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// reading is what one command has asked git about the checkout it measures:
// the tree its reviews are stamped with, and HEAD. Each answer costs git
// processes, five for the tree, and the same command asked again with nothing
// written in between: `gate commit pass` hashed the same tree up to three
// times, it and `gate code_review pass` each read HEAD twice, and `review list`
// hashed the tree once for every reviewer it listed (US-055).
//
// It belongs to one command. It is made where that command starts asking, and
// is not asked again once the command has written the tree or moved HEAD: a
// command that needs to ask after that makes a new one. It is never package
// state, because tests run many commands at once in one process, and a tree
// changed between two commands is the one the second reads.
//
// A failure is returned and not kept, so the next ask runs git again, as every
// ask did before there was a reading.
type reading struct {
	s              *store.Store
	tree, head     string
	treeOK, headOK bool
	// changed is every path the story has changed, asked only where a staffing
	// has a reviewer a changed path could bring back. unreadRange is set where
	// the story's commits could not be among them.
	changed     []string
	changedOK   bool
	unreadRange bool
	// trunkTests is what the gate's check of the freeze accepted as trunk's
	// own, and judged whether this gate checked the freeze at all.
	trunkTests *model.TrunkTests
	judged     bool
}

func readingOf(s *store.Store) *reading { return &reading{s: s} }

// subject is the store's review subject, asked once.
func (r *reading) subject(ctx context.Context) (string, error) {
	if r.treeOK {
		return r.tree, nil
	}
	tree, err := r.s.ReviewSubject(ctx)
	if err != nil {
		return "", err
	}
	r.tree, r.treeOK = tree, true
	return tree, nil
}

// changedPaths is every path the story has changed in the store's checkout,
// asked once: what git status lists, with both names of a file that moved and
// every file of a directory git does not track yet, and what the story has
// committed since code review first passed. Before that pass nothing of the
// story is committed, because every earlier gate is held to a HEAD that has
// not moved. Three git calls, whatever a staffing lists.
//
// Where code review has passed and the record does not say where HEAD was
// then, the story's commits cannot be read, and unreadRange says so: a
// reviewer a path could bring back is then brought back.
func (r *reading) changedPaths(ctx context.Context, record *model.Record) ([]string, error) {
	if r.changedOK {
		return r.changed, nil
	}
	root := r.s.Root()
	listed, err := gitx.Changes(ctx, root)
	if err != nil {
		return nil, err
	}
	files, err := gitx.Uncommitted(ctx, root, true)
	if err != nil {
		return nil, err
	}
	head, err := r.headOf(ctx)
	if err != nil {
		return nil, err
	}
	committed, err := r.committedSince(ctx, record, head)
	if err != nil {
		return nil, err
	}
	r.changed, r.changedOK = slices.Concat(listed, files, committed), true
	return r.changed, nil
}

// committedSince is what the story has committed since code review first
// passed, as far as the record says where that was.
func (r *reading) committedSince(ctx context.Context, record *model.Record, head string) ([]string, error) {
	root := r.s.Root()
	var committed []string
	var err error
	switch {
	case head == gitx.NoCommits || !record.Pass(model.GateCodeReview) && record.CommitBase == "":
	case record.CommitBase == "":
		r.unreadRange = true
	case head != record.CommitBase:
		from := record.CommitBase
		// A story rebased in its own worktree sits on a later trunk commit
		// than the one HEAD was at then, and what trunk gained in between is
		// not the story's.
		if _, ok := r.s.StoryWorktreeOf(); ok && record.Cut != "" {
			if on := sittingOn(ctx, r.s, head); on != "" && on != record.Cut && on != head {
				from = on
			}
		}
		if committed, err = gitx.ChangedBetween(ctx, root, from, head); err != nil {
			return nil, err
		}
	}
	return committed, nil
}

// headOf is HEAD in the store's checkout, asked once.
func (r *reading) headOf(ctx context.Context) (string, error) {
	if r.headOK {
		return r.head, nil
	}
	head, err := gitx.Head(ctx, r.s.Root())
	if err != nil {
		return "", err
	}
	r.head, r.headOK = head, true
	return head, nil
}
