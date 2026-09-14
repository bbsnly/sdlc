package cli

import (
	"strings"
	"testing"
)

type activePayload struct {
	Active *string `json:"active"`
}

// loop.max_rework_rounds was written into every configuration and read by
// nothing, and the runbook counted to three on its own.
func TestAGateThatKeepsFailingIsHandedToAPerson(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setConfigOnDisk(t, root, "loop", map[string]any{"max_rework_rounds": 2, "max_review_rounds": 2, "max_stop_blocks": 3})
	addStory(t, root, "US-002")
	mustRun(t, "start")

	if first := decode[gatePayload](t, mustRun(t, "gate", "dor", "fail", "--note", "AC-2 is not testable", "--json")); first.HandedOver != "" {
		t.Fatalf("the first failure handed the story over: %+v", first)
	}
	second := mustRun(t, "gate", "dor", "fail", "--note", "AC-2 is still not testable")
	for _, want := range []string{"handed to a person", "sdlc approve US-001", "still not testable"} {
		if !strings.Contains(second.stdout, want) {
			t.Errorf("the second failure does not say %q:\n%s", want, second.stdout)
		}
	}
	// The person is told what kept failing, not only that something did.
	if r := run(t, "start"); !strings.Contains(r.stderr, "SDLC-E0036") || !strings.Contains(r.stderr, "gate_failing") ||
		!strings.Contains(r.stderr, "still not testable") {
		t.Fatalf("the story carried on past the hand-over, or the hand-over does not say why:\n%s%s", r.stdout, r.stderr)
	}

	// A person deciding the story should carry on starts the count again.
	mustRun(t, "approve", "US-001")
	mustRun(t, "start")
	if again := decode[gatePayload](t, mustRun(t, "gate", "dor", "fail", "--note", "one more try", "--json")); again.HandedOver != "" {
		t.Fatalf("a failure after a person's decision was counted with the ones before it: %+v", again)
	}

	// Handing another story over leaves the one being worked on where it is.
	mustRun(t, "gate", "dor", "fail", "--story", "US-002")
	if other := decode[gatePayload](t, mustRun(t, "gate", "dor", "fail", "--story", "US-002", "--json")); other.HandedOver != "gate_failing" {
		t.Fatalf("US-002 was not handed over: %+v", other)
	}
	if status := decode[activePayload](t, mustRun(t, "status", "--json")); status.Active == nil || *status.Active != "US-001" {
		t.Errorf("handing US-002 over ended the iteration on US-001: active = %v", status.Active)
	}
}

func TestAReviewerThatKeepsBlockingIsHandedToAPerson(t *testing.T) {
	planned(t)

	// An advisory reviewer's block cannot stop the gate, so it is not a round
	// the rework lost.
	for range 3 {
		if r := decode[reviewPayload](t, mustRunWith(t, "# red team\n",
			"review", "add", "design_review", "red-team", "block", "--json")); r.HandedOver != "" {
			t.Fatalf("an advisory block handed the story over: %+v", r)
		}
	}

	if first := decode[reviewPayload](t, mustRunWith(t, "# architect\n",
		"review", "add", "design_review", "architect", "block", "--note", "AC-3 has no step", "--json")); first.HandedOver != "" {
		t.Fatalf("the first block handed the story over: %+v", first)
	}
	second := decode[reviewPayload](t, mustRunWith(t, "# architect\n",
		"review", "add", "design_review", "architect", "block", "--note", "AC-3 still has no step", "--json"))
	if second.HandedOver != "review_not_converging" {
		t.Fatalf("the second block, at loop.max_review_rounds, did not hand the story over: %+v", second)
	}
	if r := run(t, "start"); !strings.Contains(r.stderr, "SDLC-E0036") || !strings.Contains(r.stderr, "still has no step") {
		t.Fatalf("the story carried on past the hand-over:\n%s%s", r.stdout, r.stderr)
	}
}

func TestALimitOfZeroNeverHandsAStoryOver(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setConfigOnDisk(t, root, "loop", map[string]any{"max_rework_rounds": 0, "max_review_rounds": 0, "max_stop_blocks": 3})
	mustRun(t, "start")

	for range 4 {
		if r := decode[gatePayload](t, mustRun(t, "gate", "dor", "fail", "--json")); r.HandedOver != "" {
			t.Fatalf("a limit of zero handed the story over: %+v", r)
		}
	}
}
