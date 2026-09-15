package cli

import (
	"testing"

	"github.com/bbsnly/sdlc/internal/version"
)

// The runbook reads the protocol from status to tell when the sdlc it drives is
// older than it. Missing, it reads as zero: an sdlc too old to say.
func TestStatusSaysWhichProtocolItSpeaks(t *testing.T) {
	gitProject(t)
	mustRun(t, "init")
	if got := decode[statusPayload](t, mustRun(t, "status", "--json")).Protocol; got != version.Protocol {
		t.Errorf("status --json carries protocol %d, want %d", got, version.Protocol)
	}
}
