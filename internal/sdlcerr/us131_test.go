package sdlcerr

import (
	"strings"
	"testing"
)

// US-131 AC-4 and AC-10: a malformed `sdlc decide` is refused under a code of its
// own, found by number so that this package keeps compiling until it is
// registered. The catalogue's fix is the general one, and names every part a
// decision needs, since the one missing is said by the refusal itself
// (internal/cli/us131_test.go holds that); its troubleshooting section says the
// same, and that nothing was recorded.
func TestAC4TheDecisionCodeIsDocumentedAndItsFixNamesEachPart(t *testing.T) {
	t.Parallel()
	const id = "SDLC-E0094"
	code := registered(id)
	if code.String() != id {
		t.Fatalf("AC-4 the catalogue has no %s", id)
	}
	fix := strings.ToLower(fixFor(code))
	for _, word := range []string{"--question", "--option", "--chosen", "--reason"} {
		if !strings.Contains(fix, word) {
			t.Errorf("AC-4 the fix for %s does not name %s: %s", id, word, fix)
		}
	}
	body, ok := documentedCodes(t)[id]
	if !ok || isRetired(body) {
		t.Fatalf("AC-10 %s has no live `### %s` section in %s", id, id, troubleshootingPath)
	}
	flat := strings.ToLower(strings.Join(strings.Fields(body), " "))
	for _, word := range []string{"sdlc decide", "--question", "--option", "--chosen", "--reason", "nothing"} {
		if !strings.Contains(flat, word) {
			t.Errorf("AC-10 the section for %s does not say %q:\n%s", id, word, body)
		}
	}
}
