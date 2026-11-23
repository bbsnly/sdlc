package sdlcerr

// US-134 AC-2: the implementation gate refused because a command it ran failed
// has a code of its own, the next after SDLC-E0093, with a heading in the
// troubleshooting guide and a fix that sends the reader to the output file and
// to rework. The landing's SDLC-E0055 is not it: the landing keeps that code for
// its own run (ANALYSIS 6.2), and its fix is the landing's.

import (
	"strings"
	"testing"
)

func TestAC2TheGateRunCodeIsDocumentedAndSaysWhatToDo(t *testing.T) {
	c := registered("SDLC-E0095")
	if c == (Code{}) {
		t.Fatal("AC-2 SDLC-E0095 is not in the catalogue")
	}
	fix := fixFor(c)
	for _, want := range []string{".sdlc/stories/", "sdlc gate implementation pass"} {
		if !strings.Contains(fix, want) {
			t.Errorf("AC-2 the fix of SDLC-E0095 does not say %q: %s", want, fix)
		}
	}
	if fix == fixFor(LandingCheckFailed) {
		t.Error("AC-2 SDLC-E0095 gives the landing's fix, which is about a tree that was to land")
	}
	body, ok := documentedCodes(t)["SDLC-E0095"]
	if !ok || isRetired(body) {
		t.Fatalf("AC-2 SDLC-E0095 has no live heading in %s", troubleshootingPath)
	}
	for _, want := range []string{"sdlc gate implementation pass", ".sdlc/stories/", "loop.max_rework_rounds", "SDLC_RUN_DIR"} {
		if !strings.Contains(body, want) {
			t.Errorf("AC-2 and AC-8 the SDLC-E0095 heading in %s does not say %q", troubleshootingPath, want)
		}
	}
}
