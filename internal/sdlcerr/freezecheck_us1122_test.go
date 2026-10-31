package sdlcerr

// US-112.2: a freeze check that refuses has one code, whichever way it refused
// (decision 12). The catalogue's fix is what a reader gets where no refusal
// built its own, so it says the thing that is true of all of them: the freeze
// is taken by running it again.

import (
	"strings"
	"testing"
)

func TestAC2TheFreezeCheckCodeIsRegisteredAndItsFixSaysToFreezeAgain(t *testing.T) {
	c := registered("SDLC-E0085")
	if c == (Code{}) {
		t.Fatal("SDLC-E0085 is not in the catalogue")
	}
	fix := fixFor(c)
	for _, want := range []string{"commands.freeze_check", "sdlc freeze"} {
		if !strings.Contains(fix, want) {
			t.Errorf("the fix of SDLC-E0085 does not say %q: %s", want, fix)
		}
	}
	if _, ok := documentedCodes(t)["SDLC-E0085"]; !ok {
		t.Errorf("SDLC-E0085 has no heading in %s", troubleshootingPath)
	}
}
