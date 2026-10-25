package sdlcerr

// US-099 AC-4: the refusal of a story whose commits add the markers of an
// unresolved conflict has a code of its own, whose fix a person can act on and
// an assistant cannot mistake for a task of its own, and a heading in
// docs/troubleshooting.md.
//
// The code is SDLC-E0080, the next after the last one released before the
// story; TestEveryCodeKeepsTheNumberItWasReleasedWith pins the number. These
// find it by number and not by name, as the staffing's tests do, so the
// identifier is the plan's to choose (ConflictMarkersAdded is the suggestion).

import (
	"strings"
	"testing"
)

const conflictMarkersID = "SDLC-E0080"

func TestAC4AStoryThatAddsConflictMarkersHasACodeWhoseFixSaysWhatAPersonDoesAndWhatAnAssistantDoesNot(t *testing.T) {
	code := registered(conflictMarkersID)
	if code == (Code{}) {
		t.Fatalf("the catalogue has no %s: the refusal of a story that adds the markers of an unresolved conflict has no code of its own", conflictMarkersID)
	}
	fix := fixFor(code)
	for _, want := range []string{
		// What a person does: put the file right, or say the lines are meant.
		"paths.conflict_markers_intended",
		// What an assistant does: hand the story over and stop, as it does
		// for a conflict that stopped the stop's own replay.
		`sdlc escalate`,
	} {
		if !strings.Contains(fix, want) {
			t.Errorf("%s's fix does not say %q:\n%s", conflictMarkersID, want, fix)
		}
	}
	// It is not another code's fix under a new number, and nothing in it
	// invites an assistant to resolve the file or to widen the list itself.
	for _, other := range []string{"SDLC-E0055", "SDLC-E0075"} {
		if fixFor(registered(other)) == fix {
			t.Errorf("%s has %s's fix word for word", conflictMarkersID, other)
		}
	}
	if i, j := strings.Index(fix, "sdlc escalate"), strings.Index(fix, "paths.conflict_markers_intended"); i >= 0 && j >= 0 && i < j {
		t.Errorf("%s's fix tells an assistant to escalate before it says what a person edits, so the sentence reads as the assistant's to carry out:\n%s", conflictMarkersID, fix)
	}

	documented := documentedCodes(t)
	body, ok := documented[conflictMarkersID]
	if !ok || isRetired(body) {
		t.Fatalf("%s has no live `### %s` section in %s", conflictMarkersID, conflictMarkersID, troubleshootingPath)
	}
	if !strings.Contains(body, "conflict_markers_intended") {
		t.Errorf("%s's section in %s does not name the setting that lists the paths where the lines are meant", conflictMarkersID, troubleshootingPath)
	}
}
