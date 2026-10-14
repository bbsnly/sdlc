package parallel

import (
	"slices"
	"strings"
	"testing"
)

// US-089: a story worked unattended by `sdlc parallel` has its tools approved
// beforehand from the built-in list, and nobody is there to approve another.
// Its bookkeeper stores the story's map sections with `sdlc codemap write`, so
// that command is on the list, or every unattended story would stall at its
// retro. The repair, `sdlc remap`, is a person's and is never on it.
func TestAC1TheUnattendedAllowlistLetsTheBookkeeperStoreItsSectionsAndNeverTheRepair(t *testing.T) {
	list := Allowlist()
	if !slices.Contains(list, "Bash(sdlc codemap:*)") {
		t.Errorf("the list has no Bash(sdlc codemap:*) entry, so an unattended bookkeeper cannot store its sections: %q", list)
	}
	for _, e := range list {
		if strings.Contains(strings.ToLower(e), "remap") {
			t.Errorf("%q is on the list: the repair is a person's command", e)
		}
	}
}
