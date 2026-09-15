package shellpolicy

import "testing"

// Unstaging changes the index and leaves the files where they are. Gate 8 has
// the session run `git add -A`, and taking back what should not go in is the
// next thing anyone does.
func TestUnstagingLeavesTheFilesAlone(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		"git restore --staged .",
		"git restore --staged internal",
		"git restore -S internal/calc/add_test.go",
		"git restore --staged .sdlc/state/active",
		"git rm -r --cached .",
		"git rm -r --cached internal",
		"git rm --cached internal/calc/add_test.go",
		"git rm --cached .sdlc/state/active",
	} {
		allowed(t, command, s)
	}

	// Asked to put the file back as well, they do take it away.
	for _, command := range []string{
		"git restore --staged --worktree internal",
		"git restore -SW internal",
		"git restore -W internal",
		"git restore internal/calc/add_test.go",
		"git rm internal/calc/add_test.go",
	} {
		refused(t, command, s, "frozen-test-through-the-tool")
	}
	refused(t, "git restore --staged --worktree .", s, "loop-state-through-the-tool")
	refused(t, "git rm -r .sdlc/state", s, "loop-state-through-the-tool")
}
