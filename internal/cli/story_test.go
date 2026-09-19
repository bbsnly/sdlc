package cli

import (
	"strings"
	"testing"
)

// Run with no subcommand, story explains itself rather than doing nothing.
func TestStoryWithNoSubcommandPrintsHelp(t *testing.T) {
	project(t)
	r := run(t, "story")
	if r.code != 0 {
		t.Fatalf("sdlc story failed with %d:\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout, "Usage:") || !strings.Contains(r.stdout, "list") {
		t.Errorf("stdout does not read like help:\n%s", r.stdout)
	}
}
