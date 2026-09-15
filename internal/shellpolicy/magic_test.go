package shellpolicy

import "testing"

// git's long pathspec magic is read as its short spelling. Split at its
// parenthesis, `git stash push -- ':(exclude)build' src` was a stash of the
// whole tree with src left over as a command, and `:(top)docs` was the top.
func TestLongPathspecMagicIsReadAsShort(t *testing.T) {
	for _, command := range []string{
		"git stash push -- ':(exclude)build' src",
		"git stash push -- ':(top)docs'",
		"git stash push -- ':(top'",
	} {
		allowed(t, command, ready)
	}
	for command, rule := range map[string]string{
		"git stash push -- ':(top,exclude)build'":             "loop-state-through-the-tool",
		"git stash push -- ':(exclude)build'":                 "loop-state-through-the-tool",
		"git stash push -- ':(exclude)x' src && rm CLAUDE.md": "protected-path-through-the-tool",
		"echo ':(' && rm .sdlc/state/active && (true)":        "loop-state-through-the-tool",
		"rm .sdlc/state/active ':( x)'":                       "loop-state-through-the-tool",
	} {
		refused(t, command, ready, rule)
	}
	frozen := ready
	frozen.Frozen = []string{"internal/calc/add_test.go"}
	refused(t, "echo ':(' && rm internal/calc/add_test.go && (true)", frozen, "frozen-test-through-the-tool")
}
