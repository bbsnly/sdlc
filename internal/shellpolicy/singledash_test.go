package shellpolicy

// US-035 (from US-103), AC-10 and AC-11: `touch`, `cp`, `sed` or `tee` and one
// single-dash word of letters took about 17.5 s at 8 KB against about 1.1 s for
// the same word after `perl`. The cause is namedBy: a word of n letters is n-1
// spellings, each a suffix of it, and each spelling of a tool that changes a
// file is looked up on disk (Resolve) and matched against the tests
// (NewTest, ImplementerTest), where perl's are only looked up. That is n²/2
// bytes, three times over.
//
// Time is held by `./task hook-time`, which is a program because the race
// detector makes these commands twenty times slower. Here the work is counted
// instead of timed: the bytes the classifier hands its caller, which is what
// the caller then reads. A tool may hand over no more than the perl shape hands
// over for a command as long, or else grow no faster than linearly with the
// word. And a bound on the spellings must not drop a short option's attached
// value: a long word whose tail is a protected name stays refused (THREATS T4).
//
// Nothing here is new API: State.Resolve, NewTest and ImplementerTest are
// counted as the hook would call them.

import (
	"fmt"
	"strings"
	"testing"
)

// sdMaxBytes is where a count is stopped, so that a classifier that has grown
// quadratic again fails in a moment rather than in minutes under the race
// detector.
const sdMaxBytes = 32 << 20

// sdCount is Inspect of command for the implementer under a freeze, and the
// bytes it handed to the State's callbacks. Past limit it stops the test.
func sdCount(t *testing.T, command string, limit int) (refused bool, handed int) {
	t.Helper()
	charge := func(word string) {
		handed += len(word)
		if handed > limit {
			t.Fatalf("%s...: the classifier handed its caller more than %d bytes for a command of %d",
				command[:min(len(command), 24)], limit, len(command))
		}
	}
	s := State{
		Agent:  "implementer",
		Frozen: []string{"internal/alpha/alpha_test.go", "internal/beta/beta_test.go", "test/e2e/gamma_test.go"},
		Resolve: func(word string) string {
			charge(word)
			return ""
		},
		NewTest: func(p string) bool {
			charge(p)
			return false
		},
		ImplementerTest: func(p string) bool {
			charge(p)
			return false
		},
	}
	_, refused = Inspect(command, s)
	return refused, handed
}

// sdCommand is tool, then one single-dash word of the letter a, then tail, n
// bytes in all.
func sdCommand(tool string, n int, tail string) string {
	head := tool + " -"
	return head + strings.Repeat("a", n-len(head)-len(tail)) + tail
}

// sdAllowance is what a command of tool, long at n bytes, may hand its caller:
// what perl's hands over for a command as long, or what one a quarter as long
// hands over grown linearly, with room for the rest of the command.
func sdAllowance(t *testing.T, tool string, n int, tail string) int {
	t.Helper()
	_, perl := sdCount(t, sdCommand("perl", n, " file"), sdMaxBytes)
	_, quarter := sdCount(t, sdCommand(tool, n/4, tail), sdMaxBytes)
	return max(perl, 8*quarter+64<<10)
}

// AC-10: the four tools, at two sizes four times apart.
func TestAC10ASingleDashWordAfterTheFourToolsCostsNoMoreThanAfterPerl(t *testing.T) {
	const n = 2048
	for _, tool := range []string{"touch", "cp", "sed", "tee"} {
		t.Run("AC-10 "+tool, func(t *testing.T) {
			allowance := sdAllowance(t, tool, n, " file")
			refused, handed := sdCount(t, sdCommand(tool, n, " file"), allowance)
			if refused {
				t.Errorf("%s with one long single-dash word is refused; it was allowed and AC-11 keeps it so", tool)
			}
			if handed > allowance {
				t.Errorf("%s: handed %d bytes, over %d", tool, handed, allowance)
			}
		})
	}
}

// AC-10 and AC-11 (THREATS T4): a bound on the spellings of a long word is not
// a way past the rules. Its tail, the loop's state or a protected file, is
// still found after letters repeated well past any bound, and the work stays
// within the allowance all the same.
func TestAC10ALongSingleDashWordWhoseTailIsAProtectedPathIsStillRefused(t *testing.T) {
	const n = 2048
	for _, tc := range []struct{ tail, rule string }{
		{".sdlc/state/active", ruleLoopState},
		{"CLAUDE.md", "protected-path-through-the-tool"},
	} {
		for _, tool := range []string{"touch", "cp", "tee"} {
			t.Run(fmt.Sprintf("AC-10 %s -aaa...a%s", tool, tc.tail), func(t *testing.T) {
				allowance := sdAllowance(t, tool, n, tc.tail)
				command := sdCommand(tool, n, tc.tail)
				refused, handed := sdCount(t, command, allowance)
				if !refused {
					t.Errorf("%s -aaa...a%s, %d bytes, was allowed: the bound dropped the value attached to the option", tool, tc.tail, n)
				}
				if handed > allowance {
					t.Errorf("handed %d bytes, over %d", handed, allowance)
				}
				if f, _ := Inspect(command, State{Agent: "implementer"}); refused && f.Rule != tc.rule {
					t.Errorf("%s -aaa...a%s was refused under %s, want %s", tool, tc.tail, f.Rule, tc.rule)
				}
			})
		}
	}
}
