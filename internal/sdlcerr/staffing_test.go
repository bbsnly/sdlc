package sdlcerr

// US-026: the staffing adds a code for each way it refuses, and each is a
// heading a person is sent to from the refusal, with a fix that says what to
// do next (AC-4, AC-5, AC-8, decisions 8 and 9).

import (
	"strings"
	"testing"
)

// registered is the catalogue's code with this number, or one that renders as
// nothing where the catalogue has none. The numbers are what is released; the
// names the source gives them are the implementation's to choose.
func registered(id string) Code {
	for _, e := range catalogue {
		if e.code.id == id {
			return e.code
		}
	}
	return Code{}
}

func TestAC4AC5AC8TheStaffingCodesAreDocumentedAndSayWhatToDo(t *testing.T) {
	documented := documentedCodes(t)
	for _, c := range []struct {
		id, refuses string
		// fix is what the fix has to name for a person, or an agent, to act on
		// it without reading the source.
		fix []string
	}{
		{"SDLC-E0067", "a staffing command where the staffing is off (AC-10)", []string{"staffing.enabled"}},
		{"SDLC-E0068", "the analysis gate, or an amendment, with no staffing recorded (AC-1)", []string{"sdlc:project-manager"}},
		{"SDLC-E0069", "a staffing that is not whole, or a second one (AC-1)", []string{"sdlc staffing amend"}},
		{"SDLC-E0070", "leaving out an agent that is never left out (AC-4)", []string{"model", "effort"}},
		{"SDLC-E0071", "a model or effort outside what the configuration allows (AC-5)", []string{"staffing"}},
		{"SDLC-E0072", "changing what a person assigned (AC-8)", []string{"sdlc staffing assign", "person"}},
		{"SDLC-E0073", "leaving out a reviewer whose latest review blocks (decision 9)", []string{"rework", "person"}},
		{"SDLC-E0074", "staffing assign from a Claude Code session (decision 8)", []string{"person", "terminal"}},
	} {
		code := registered(c.id)
		if code.String() != c.id {
			t.Errorf("the catalogue has no %s, for %s", c.id, c.refuses)
			continue
		}
		if body, ok := documented[c.id]; !ok || isRetired(body) {
			t.Errorf("%s has no live `### %s` section in %s", c.id, c.id, troubleshootingPath)
		}
		fix := fixFor(code)
		for _, want := range c.fix {
			if !strings.Contains(fix, want) {
				t.Errorf("%s's fix does not say %q: %s", c.id, want, fix)
			}
		}
	}
}

// The refusal a session meets when it runs the person's command tells it to
// stop: a session that reads "run sdlc staffing assign in a terminal" as its
// own next step has been told to do the one thing the code refuses. SDLC-E0061
// says the same of `sdlc parallel`, in the same words.
func TestDecision8TheFixForAPersonsCommandDoesNotSendAnAssistantToRunIt(t *testing.T) {
	const id = "SDLC-E0074"
	code := registered(id)
	if code.String() != id {
		t.Fatalf("the catalogue has no %s", id)
	}
	if fix := fixFor(code); !strings.Contains(fix, "an assistant stops") {
		t.Errorf("%s's fix does not say an assistant stops and tells the person, as the other person-only fixes do: %s", id, fix)
	}
}
