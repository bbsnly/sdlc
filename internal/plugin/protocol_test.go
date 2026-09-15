package plugin

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/version"
)

// A runbook that reads `sdlc status --json` checks the protocol there first, so
// that one newer than the sdlc on PATH stops instead of working gates that build
// does not enforce. A minimum above what this build speaks would stop it against
// the sdlc it is released with.
func TestTheRunbookAsksForAProtocolThisBuildSpeaks(t *testing.T) {
	floor := regexp.MustCompile("`protocol` of at least ([0-9]+)")
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, s := range skills {
		if !strings.Contains(s.Body, "sdlc status --json") {
			continue
		}
		m := floor.FindStringSubmatch(s.Body)
		if m == nil {
			t.Errorf("%s reads sdlc status --json without checking its protocol", s.Path)
			continue
		}
		if n, _ := strconv.Atoi(m[1]); n < 1 || n > version.Protocol {
			t.Errorf("%s asks for protocol %d, and this build speaks %d", s.Path, n, version.Protocol)
		}
		checked++
	}
	if checked == 0 {
		t.Error("no runbook reads sdlc status --json, so nothing checks the protocol")
	}
}
