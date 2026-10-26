package plugin

import (
	"strings"
	"testing"
)

// AC-13 (ANALYSIS "AC-13 as it stands", Gate 7 reviewers): an agent that
// names the contract as in CLAUDE.md or AGENTS.md says how it finds which. The
// runbook names the file only in the Gate 2 brief, so "(the brief names
// which)" leaves every agent after Gate 2 without a way to tell. Today eight
// agents (architect, code-reviewer, human-advocate, implementer, perf,
// researcher, sdet and security) carry that parenthesis and no other way.
func TestAC13AnAgentThatNamesTheContractInEitherFileSaysHowItFindsWhichWithoutTheBrief(t *testing.T) {
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	naming := 0
	for _, c := range agents {
		for _, b := range agBlocks(c.Body) {
			if !strings.Contains(b, "SDLC Contract") || !strings.Contains(b, "CLAUDE.md") || !strings.Contains(b, "AGENTS.md") {
				continue
			}
			naming++
			lower := strings.ToLower(b)
			if strings.Contains(lower, "the brief names which") || strings.Contains(lower, "the brief says which") {
				t.Errorf("AC-13 %s leaves which file holds the contract to the brief, which names it only at Gate 2:\n%s", c.Name, b)
			}
			// How it finds which: it looks in both, or takes whichever holds
			// the section.
			found := false
			for _, how := range []string{"whichever", "both", "either", "the one that holds", "the file that holds", "holds the section", "has the section"} {
				found = found || strings.Contains(lower, how)
			}
			if !found {
				t.Errorf("AC-13 %s names the contract as in CLAUDE.md or AGENTS.md and does not say how it finds which:\n%s", c.Name, b)
			}
		}
	}
	// The eight agents above name it; a fix that drops the naming from them all
	// does not pass by leaving nothing to check.
	if naming < 8 {
		t.Errorf("AC-13 only %d agent blocks name the contract as in CLAUDE.md or AGENTS.md; want at least the eight that did", naming)
	}
}
