package cli

import (
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// linesOf is a file n lines long, for a change of a known size.
func linesOf(n int) string { return strings.Repeat("x\n", n) }

func refusedForSize(t *testing.T, gate model.Gate, size string) {
	t.Helper()
	r := run(t, "gate", string(gate), "pass", "--note", "over the cap")
	if r.code == 0 {
		t.Fatalf("%s passed on a change over thresholds.diff_size_cap", gate)
	}
	for _, want := range []string{"SDLC-E0042", size} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the %s refusal does not say %q:\n%s", gate, want, r.stderr)
		}
	}
}

// thresholds.diff_size_cap was read by the agents that size and plan a story,
// and by nothing that checked the change which came out of them.
func TestAChangeOverTheCapCannotPassTheGatesThatSeeIt(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setConfigOnDisk(t, root, "thresholds", map[string]any{"diff_size_cap": 30})
	mustRun(t, "start")
	reach(t, root, model.GateImplementation)

	// Three lines of acceptance test from Gate 3, and forty of code.
	writeFile(t, root, "internal/invoice.go", linesOf(40))
	refusedForSize(t, model.GateImplementation, "43 lines")

	// The loop's own files are not the change, and neither is the backlog:
	// a whole story added to it still leaves the change under the cap.
	writeFile(t, root, "internal/invoice.go", linesOf(20))
	addStory(t, root, "US-002")
	mustRun(t, "gate", "implementation", "pass", "--note", "23 lines")

	// Rework can grow a change after the gate that first measured it.
	writeFile(t, root, "internal/invoice.go", linesOf(40))
	satisfy(t, root, model.GateVerification)
	refusedForSize(t, model.GateVerification, "43 lines")
	writeFile(t, root, "internal/invoice.go", linesOf(20))
	satisfy(t, root, model.GateVerification)
	mustRun(t, "gate", "verification", "pass", "--note", "23 lines")
	satisfy(t, root, model.GateVerifierReview)
	mustRun(t, "gate", "verifier_review", "pass", "--note", "no gaming found")

	writeFile(t, root, "internal/invoice.go", linesOf(40))
	satisfy(t, root, model.GateCodeReview)
	refusedForSize(t, model.GateCodeReview, "43 lines")
}

func TestACapOfZeroIsNotEnforced(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setConfigOnDisk(t, root, "thresholds", map[string]any{"diff_size_cap": 0})
	mustRun(t, "start")
	reach(t, root, model.GateImplementation)

	// Over the default cap of 500, so that it is the zero doing this.
	writeFile(t, root, "internal/invoice.go", linesOf(600))
	mustRun(t, "gate", "implementation", "pass", "--note", "no cap")
}
