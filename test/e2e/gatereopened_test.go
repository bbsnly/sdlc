// US-020, driven through the built binary: a gate recorded again says which
// gates that took back, in prose and in --json. The comparison with `sdlc log`
// (AC-3) is in internal/cli, where the story can be brought back to a commit.

package e2e

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// passedGates is the gates `sdlc status --json` says stand passed, in gate order.
func (sb sandbox) passedGates(t *testing.T) []string {
	t.Helper()
	var status struct {
		Gates map[string]string `json:"gates"`
	}
	stdout, stderr, code := sb.outcome(t, sandboxSession, sb.tree(), "", "status", "--json")
	if code != 0 {
		t.Fatalf("sdlc status --json exited %d:\n%s%s", code, stdout, stderr)
	}
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("sdlc status --json printed something that is not JSON: %v\n%s", err, stdout)
	}
	var passed []string
	for _, g := range model.Gates {
		if status.Gates[string(g)] == "pass" {
			passed = append(passed, string(g))
		}
	}
	return passed
}

func finishedInASandbox(t *testing.T) sandbox {
	t.Helper()
	sb := newSandbox(t)
	sb.does(t, sb.repo, "", "", "start")
	sb.worksEveryGate(t)
	return sb
}

// recordsVerificationAgain records verification on a finished story, and
// returns what it printed and the gates the record lost to it, other than
// verification itself, in gate order.
func (sb sandbox) recordsVerificationAgain(t *testing.T, args ...string) (stdout string, lost []string) {
	t.Helper()
	before := sb.passedGates(t)
	stdout = sb.works(t, sb.tree(), "", "", append([]string{"gate", "verification", "pass", "--note", "redone"}, args...)...)
	after := sb.passedGates(t)
	for _, g := range before {
		if g != "verification" && !slices.Contains(after, g) {
			lost = append(lost, g)
		}
	}
	if len(lost) < 2 {
		t.Fatalf("recording verification again took back %v, want two or more gates for this test to mean anything", lost)
	}
	return stdout, lost
}

func TestAC1ARecordedGateNamesWhatItReopenedWhenTheBuiltBinaryIsDriven(t *testing.T) {
	parallel(t)
	t.Run("AC-1 --json names every gate under reopened", func(t *testing.T) {
		stdout, lost := finishedInASandbox(t).recordsVerificationAgain(t, "--json")
		var out struct {
			Reopened []string `json:"reopened"`
		}
		if err := json.Unmarshal([]byte(stdout), &out); err != nil {
			t.Fatalf("not JSON: %v\n%s", err, stdout)
		}
		if !slices.Equal(out.Reopened, lost) {
			t.Errorf("reopened = %v, want %v:\n%s", out.Reopened, lost, stdout)
		}
	})

	t.Run("AC-1 the prose names every gate on one line", func(t *testing.T) {
		stdout, lost := finishedInASandbox(t).recordsVerificationAgain(t)
		var named []string
		var lines int
		for line := range strings.SplitSeq(stdout, "\n") {
			if rest, ok := strings.CutPrefix(line, "  reopened: "); ok {
				lines++
				named = strings.Split(rest, ", ")
			}
		}
		if lines != 1 || !slices.Equal(named, lost) {
			t.Errorf("the prose names %v on %d line(s), want %v on one:\n%s", named, lines, lost, stdout)
		}
	})
}
