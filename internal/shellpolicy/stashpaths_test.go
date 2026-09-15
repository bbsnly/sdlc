package shellpolicy

import "testing"

// A stash of named paths takes only those paths. It is the way out of
// SDLC-E0032, raised while a story is still being worked on: stash what does
// not belong to this story, then record the gate.
func TestAStashOfNamedPathsTakesOnlyThose(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		"git stash push -- docs/notes.md",
		"git stash push -u -- scratch",
		`git stash push -m "not this story" -- docs/notes.md build`,
		"git stash -- docs/notes.md",
	} {
		allowed(t, command, s)
	}

	// The paths are read after `--` only: a message is words of its own, and
	// read as paths, `git stash push -m "wip all"` stashed the whole work tree
	// as if it were a path called all". `git stash save` takes a message, not
	// paths.
	for _, command := range []string{
		`git stash push -m "wip all"`,
		"git stash push docs/notes.md",
		"git stash save -- docs/notes.md",
		"git stash push --pathspec-from-file=paths.txt -- docs/notes.md",
		"git stash push -- .",
		"git stash push -- .sdlc",
		"git stash push -u -- .sdlc/state/active",
	} {
		refused(t, command, s, "loop-state-through-the-tool")
	}
	for _, command := range []string{
		"git stash push -- internal",
		"git stash push -u -- internal/calc/add_test.go",
	} {
		refused(t, command, s, "frozen-test-through-the-tool")
	}
}
