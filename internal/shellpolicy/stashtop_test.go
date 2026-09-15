package shellpolicy

import "testing"

// A stash limited to pathspecs that still name the whole work tree takes the
// whole work tree. `git stash push -u -- ':/'` stashed the loop's record as a
// stash of one path.
func TestAStashOfTheTopIsTheWholeTree(t *testing.T) {
	for _, command := range []string{
		"git stash push -u -- ':/'",
		"git stash push -- :/",
		"git stash -u -- :/.",
		"git stash push -- ':(top)'",
		"cd internal && git stash push -- :/",
		"git stash push -- ':!build'",
		"git stash push -- ':^docs' ':(exclude)build'",
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
	for _, command := range []string{
		"git stash push -- ':/docs'",
		"git stash push -- build ':!build/keep'",
		"cd internal && git stash push -- .",
	} {
		allowed(t, command, ready)
	}
}
