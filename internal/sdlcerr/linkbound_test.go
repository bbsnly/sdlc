package sdlcerr

// US-035, AC-12 (decided D2): a command the hook refuses at a bound on the
// links it follows names a code with a heading in docs/troubleshooting.md. Two
// codes, because the two refusals have two different next steps: a word
// through more than 64 links (AC-5, AC-7) is fixed by naming the place it
// leads to directly, and a command over its whole budget (AC-6) by splitting
// it. They are the next numbers free after SDLC-E0080, which another story
// holds, and are pinned ahead of their codes in TestEveryCodeKeepsTheNumberItWasReleasedWith
// as SDLC-E0075 to SDLC-E0080 were: if a story lands on trunk first with
// either number, these take the next on the rebase, before they are released.

import (
	"strings"
	"testing"
)

func TestAC12TheLinkBoundCodesAreDocumentedAndSayWhatToDo(t *testing.T) {
	documented := documentedCodes(t)
	for _, c := range []struct {
		id, refuses string
		// fix is a word the fix has to carry for a person to act on it.
		fix string
	}{
		{"SDLC-E0081", "a word that goes through more than 64 links", "directly"},
		{"SDLC-E0082", "a command whose words follow more links than its budget", "split"},
		// From the security review: a path longer than the system looks up in
		// one go, where the walk read the rest by name. Its fix is a shorter
		// full path; a nearer directory names the same path.
		{"SDLC-E0083", "a path longer than the system looks up in one go", "shorter"},
	} {
		t.Run("AC-12 "+c.id, func(t *testing.T) {
			code := registered(c.id)
			if code == (Code{}) {
				t.Fatalf("the catalogue has no %s for %s", c.id, c.refuses)
			}
			if body, ok := documented[c.id]; !ok || isRetired(body) {
				t.Errorf("%s has no live `### %s` section in %s", c.id, c.id, troubleshootingPath)
			}
			if fix := fixFor(code); !strings.Contains(fix, c.fix) {
				t.Errorf("%s's fix does not say %q: %s", c.id, c.fix, fix)
			}
		})
	}
}
