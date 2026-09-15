package shellpolicy

import "testing"

// A pathspec that names the whole tree names it for every git command that
// throws work away, not only for a stash, and `..` from where a command runs is
// where it points: `git clean -fdx :/` and `cd docs && git stash push -u -- ..`
// took the loop's record.
func TestAWholeTreePathspecDiscardsTheWorkTree(t *testing.T) {
	for _, command := range []string{
		"git clean -fdx :/",
		"cd docs && git clean -fdx :/",
		"git clean -fd -- ':!build'",
		"git clean -fd ':(exclude)build'",
		"git restore -- :/",
		"git checkout -- :/",
		"cd docs && git stash push -u -- ..",
		"cd build && rm -rf ..",
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
	for _, command := range []string{
		"git clean -fd build",
		"cd docs && git clean -fdx",
		"git clean -n :/",
		"git restore --staged :/",
		"git restore docs/notes.md",
		"git checkout main",
		"git checkout",
		"cd build && rm -rf .",
	} {
		allowed(t, command, ready)
	}
}
