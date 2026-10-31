package sdlcerr

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// US-112.3 AC-4: the refusal of a diff whose revision cannot be given to a
// reviewer. The identifier is read from the source, not used, so that the rest
// of this package's tests run until it exists: the name is what other code will
// call it by, and the number is what is released.
func TestAC4TheRevisionCodeIsNamedPlanRevisionNotKeptIsDocumentedAndSaysWhatToDo(t *testing.T) {
	const id = "SDLC-E0084"
	raw, err := os.ReadFile("catalogue.go")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^\s*PlanRevisionNotKept\s*=\s*register\("` + id + `"`).Match(raw) {
		t.Errorf("catalogue.go does not register %s as PlanRevisionNotKept", id)
	}
	code := registered(id)
	if code.String() != id {
		t.Fatalf("the catalogue has no %s", id)
	}
	if body, ok := documentedCodes(t)[id]; !ok || isRetired(body) {
		t.Errorf("%s has no live `### %s` section in %s", id, id, troubleshootingPath)
	} else if !strings.Contains(strings.Join(strings.Fields(body), " "), "whole plan") {
		t.Errorf("the section for %s does not say the reviewer reads the whole plan:\n%s", id, body)
	}
	fix := strings.Join(strings.Fields(fixFor(code)), " ")
	if !strings.Contains(fix, "whole plan") {
		t.Errorf("%s's fix does not say what to do next, which is to give the reviewer the whole plan: %s", id, fix)
	}
}
