package sdlcerr

// US-116 AC-5: the ten codes that only `sdlc parallel` raised, SDLC-E0057 to
// SDLC-E0066, are retired the way the catalogue's header says: out of the
// catalogue, and their sections in the troubleshooting page kept, because a
// person reading an old log still lands on the heading. A section that says
// nothing of why the code is retired is no help to that person.
//
// TestEveryTroubleshootingHeadingIsLiveOrRetired holds the rule for every
// heading, including that a section marked retired is not for a code still in
// the catalogue; these tests hold that the ten sections exist, say so, and say
// why. They do not look for a code being raised.

import (
	"fmt"
	"strings"
	"testing"
)

func TestAC5EachOfTheTenCodesOfTheRemovedCommandKeepsItsSectionMarkedRetiredAndSaysWhy(t *testing.T) {
	documented := documentedCodes(t)
	for n := 57; n <= 66; n++ {
		id := fmt.Sprintf("SDLC-E%04d", n)
		t.Run(id, func(t *testing.T) {
			body, ok := documented[id]
			if !ok {
				t.Fatalf("%s has no `### %s` section in %s: a retired code keeps its section", id, id, troubleshootingPath)
			}
			if !isRetired(body) {
				t.Fatalf("%s's section in %s is not marked **Retired.**", id, troubleshootingPath)
			}
			why := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), "**Retired.**"))
			sentence, _, found := strings.Cut(why, ". ")
			if !found && !strings.HasSuffix(why, ".") {
				t.Fatalf("%s's section says nothing after **Retired.** that ends in a sentence: %q", id, why)
			}
			if !strings.Contains(sentence, "sdlc parallel") {
				t.Errorf("%s's retired section does not say which command it came from, in its first sentence: %q", id, sentence)
			}
		})
	}
}
