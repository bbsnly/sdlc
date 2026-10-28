package gitx

import (
	"path/filepath"
	"sync"
)

// answers keeps, for the life of the process, what a sealed git said of
// commits named by their full hashes: whether one is an ancestor of another,
// their merge base, whether a merge lies between them, the first commit above
// one on the other's line, the paths that differ between them, and a file as
// one of them has it. A full hash names an object by its content, and a
// commit's parents and tree are part of its content, so each of these is the
// same answer every time git gives it. Nothing that reads a ref, the index,
// the worktree or the configuration is kept: what a name or a file stands for
// can change between two questions, and the next question reads it again.
//
// The landing asks the same questions of the same commits in each check it
// makes, and the commit gate, the freeze and the stop ask them again: in a run
// of the cli tests, 5,500 of git's 9,400 merge-base processes and 2,600 of its
// 3,800 diff-tree --name-only ones repeated a call already made in the same
// test, on systems where a process costs milliseconds.
//
// Only an answer is kept. A failure, such as a commit git does not have yet,
// is asked again, as before. Sealed calls read no graft file and no
// replacement, either of which could make one commit answer two ways. A
// shallow file can hide ancestry, and an answer kept from before one was
// written, or after it was taken away, is one git gave about the same
// objects in this process: a repository that can be made to answer that way
// once could have been left that way.
//
// Nothing is dropped: every sdlc process is one command or one hook call, so
// what is kept is bounded by the questions one command asks.
var answers sync.Map

// answerKey is one question in answers: what was asked, of which commits, in
// the repository at root, as an absolute path.
type answerKey struct {
	ask, root, a, b string
}

// keyOf is the answerKey for ask of a and b in root, and false where root has
// no absolute path to keep it under.
func keyOf(ask, root, a, b string) (answerKey, bool) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return answerKey{}, false
	}
	return answerKey{ask: ask, root: abs, a: a, b: b}, true
}

// recall is the answer kept for ask of a and b in root, where there is one,
// and otherwise what find says, kept where it is an answer and not a failure.
// A caller that hands out something it can change, a slice, hands out a copy.
func recall[T any](ask, root, a, b string, find func() (T, error)) (T, error) {
	key, keep := keyOf(ask, root, a, b)
	if keep {
		if said, ok := answers.Load(key); ok {
			if v, ok := said.(T); ok {
				return v, nil
			}
		}
	}
	v, err := find()
	if err == nil && keep {
		answers.Store(key, v)
	}
	return v, err
}
