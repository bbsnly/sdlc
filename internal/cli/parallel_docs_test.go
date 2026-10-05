package cli

import (
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/parallel"
)

// US-058 AC-9 and AC-5: what a person reads says what the command does and
// what the unattended flag allows, and says it from the code: the page's list
// is the one the launcher grants, so the two cannot drift.

func TestAC9TheCommandPageDescribesParallelItsFlagsAndSendsPeopleToTheCodes(t *testing.T) {
	t.Parallel()
	doc := page(t, "commands.md")
	at := strings.Index(doc, "## `sdlc parallel")
	if at < 0 {
		t.Fatal("docs/commands.md has no section for `sdlc parallel`")
	}
	section := doc[at:]
	if next := strings.Index(section[3:], "\n## "); next >= 0 {
		section = section[:next+3]
	}
	for _, want := range []string{"--max", "--unattended", "waiting", "not interactive", "person"} {
		if !strings.Contains(strings.ToLower(section), want) {
			t.Errorf("the sdlc parallel section never says %q", want)
		}
	}
	for _, code := range []string{
		"SDLC-E0057", "SDLC-E0058", "SDLC-E0059", "SDLC-E0060", "SDLC-E0061",
		"SDLC-E0062", "SDLC-E0063", "SDLC-E0064", "SDLC-E0065", "SDLC-E0066",
	} {
		if !strings.Contains(doc, code) {
			t.Errorf("docs/commands.md never mentions %s, a way sdlc parallel refuses", code)
		}
	}
}

func TestAC9TheLoopPageSaysHowSeveralStoriesAreWorkedAtOnce(t *testing.T) {
	t.Parallel()
	doc := page(t, "the-loop.md")
	for _, want := range []string{"sdlc parallel", "worktree", "session"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/the-loop.md never mentions %q in describing parallel work", want)
		}
	}
}

// The allowlist is stated on the enforcement page exactly as the code builds
// it, with what the hook still refuses in an unattended session and what it
// cannot stop (AC-5, and the threats the story was assessed against).
func TestAC5TheEnforcementPageStatesTheAllowlistTheCodeGrants(t *testing.T) {
	t.Parallel()
	doc := page(t, "enforcement.md")
	for _, entry := range parallel.Allowlist() {
		if !strings.Contains(doc, entry) {
			t.Errorf("docs/enforcement.md does not state the allowlist entry %q", entry)
		}
	}
	flat := strings.ToLower(strings.Join(strings.Fields(doc), " "))
	for _, want := range []string{"--unattended", "parallel.allow", "not interactive", "never"} {
		if !strings.Contains(flat, want) {
			t.Errorf("docs/enforcement.md never says %q about unattended sessions", want)
		}
	}
	if !strings.Contains(doc, "### `parallel-is-a-human-decision`") {
		t.Error("docs/enforcement.md has no section for the parallel-is-a-human-decision rule")
	}
}

func TestAC5TheConfigurationPageNamesTheNarrowingList(t *testing.T) {
	t.Parallel()
	doc := page(t, "configuration.md")
	for _, want := range []string{"parallel", "allow"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/configuration.md never mentions %q", want)
		}
	}
	if !strings.Contains(strings.ToLower(doc), "narrow") {
		t.Error("docs/configuration.md does not say the list can only be narrowed")
	}
}
