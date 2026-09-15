package shellpolicy

import "testing"

// git takes any start of a long option that names only one. Matched only as
// spelled out, `git reset --ha` threw away the work tree as a reset that keeps
// it, and `git restore --staged --work` put back a frozen test as a restore of
// the index alone.
func TestAGitOptionCutShortIsTheOption(t *testing.T) {
	for _, command := range []string{
		"git reset --ha",
		"git reset --mer HEAD~1",
		"git reset --ke",
		"git checkout --forc main",
		"git switch --discard main",
		"git clean --forc -d",
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
	frozen := ready
	frozen.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		"git restore --staged --work internal/calc/add_test.go",
		"git restore --staged --w internal/calc/add_test.go",
	} {
		refused(t, command, frozen, "frozen-test-through-the-tool")
	}
	for _, command := range []string{
		"git restore --stag internal/calc/add_test.go",
		"git rm --cach internal/calc/add_test.go",
		"git checkout -- internal/calc/other.go",
		"git reset --so HEAD~1",
	} {
		allowed(t, command, frozen)
	}
}
