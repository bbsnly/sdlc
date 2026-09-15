package shellpolicy

import "testing"

// find writes the file -fprint, -fprint0, -fprintf and -fls name. Unread,
// `find . -fprint .sdlc/state/active` emptied the loop's record past every
// rule. Plan: changesFiles and changesAFile count these options; writtenTo's
// find case returns only their values unless find also deletes or runs a
// writer, so what find merely lists into a file elsewhere is not a write to
// what it lists.
func TestFindWritesTheFileItPrintsTo(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for command, rule := range map[string]string{
		"find . -fprint .sdlc/state/active":                       "loop-state-through-the-tool",
		"find . -name x -fprint0 .sdlc/state/tests.lock":          "loop-state-through-the-tool",
		"find . -fprintf .sdlc/state/active %p":                   "loop-state-through-the-tool",
		"find . -fls CLAUDE.md":                                   "protected-path-through-the-tool",
		"find internal -name x -fprint internal/calc/add_test.go": "frozen-test-through-the-tool",
		"find .sdlc/state -delete":                                "loop-state-through-the-tool",
	} {
		refused(t, command, s, rule)
	}
	for _, command := range []string{
		"find . -name '*.md' -fprint /tmp/list",
		"find .sdlc -fprint /tmp/list",
		"find internal -name '*_test.go' -fls /tmp/tests",
		"find . -fprint",
	} {
		allowed(t, command, s)
	}
}
