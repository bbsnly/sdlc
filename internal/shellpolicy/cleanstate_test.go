package shellpolicy

import "testing"

// What a command takes away is read as the loop state and protected paths are,
// not only as the frozen tests are: `git clean -fdx .sdlc` wrote nothing and
// emptied the loop's record.
func TestTakingLoopStateAwayIsRefused(t *testing.T) {
	for command, rule := range map[string]string{
		"git clean -fdx .sdlc":       "loop-state-through-the-tool",
		"git clean -fdx .sdlc/state": "loop-state-through-the-tool",
		"git -C .sdlc clean -fdx":    "loop-state-through-the-tool",
		"cd .sdlc && git clean -fdx": "loop-state-through-the-tool",
		"git clean -fdx .claude":     "protected-path-through-the-tool",
		"git clean -fd CLAUDE.md":    "protected-path-through-the-tool",
	} {
		refused(t, command, ready, rule)
	}
	for _, command := range []string{
		"git clean -fd build",
		"git clean -n .sdlc",
		"git stash push -- docs/notes.md",
	} {
		allowed(t, command, ready)
	}
}
