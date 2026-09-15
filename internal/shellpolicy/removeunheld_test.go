package shellpolicy

import (
	"strings"
	"testing"
)

// SDLC-E0043 says to remove the test files the freeze does not hold, and the
// shell refused that as adding one: `rm internal/calc/extra_test.go` was a new
// test file after the freeze.
func TestATestTheFreezeDoesNotHoldCanBeRemoved(t *testing.T) {
	state := State{
		CommitReady: true,
		Frozen:      []string{"internal/calc/add_test.go"},
		NewTest: func(p string) bool {
			return strings.HasSuffix(p, "_test.go") && p != "internal/calc/add_test.go"
		},
	}
	for _, command := range []string{
		"rm internal/calc/extra_test.go",
		"rm -f internal/calc/extra_test.go",
		"unlink internal/calc/extra_test.go",
		"Remove-Item internal/calc/extra_test.go",
		"ri internal/calc/extra_test.go",
		"del internal/calc/extra_test.go",
		"erase internal/calc/extra_test.go",
		"git rm -f internal/calc/extra_test.go",
		"cd internal/calc && rm extra_test.go",
	} {
		allowed(t, command, state)
	}
	for command, rule := range map[string]string{
		"rm internal/calc/add_test.go":                                         "frozen-test-through-the-tool",
		"git rm internal/calc/add_test.go":                                     "frozen-test-through-the-tool",
		"rm internal/calc/extra_test.go > internal/calc/new_test.go":           "no-new-test-after-the-freeze",
		"rm internal/calc/extra_test.go > internal/calc/extra_test.go":         "no-new-test-after-the-freeze",
		"rm internal/calc/extra_test.go; echo x > internal/calc/extra_test.go": "no-new-test-after-the-freeze",
		"git checkout main -- internal/calc/extra_test.go":                     "no-new-test-after-the-freeze",
		"mv /tmp/passing.go internal/calc/extra_test.go":                       "no-new-test-after-the-freeze",
	} {
		refused(t, command, state, rule)
	}
	implementer := State{
		CommitReady:     true,
		ImplementerTest: func(p string) bool { return strings.HasSuffix(p, "_test.go") },
	}
	refused(t, "rm internal/calc/extra_test.go", implementer, "implementer-does-not-write-tests")
}
