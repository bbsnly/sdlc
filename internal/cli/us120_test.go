package cli

import (
	"strings"
	"testing"
)

// AC-13 (ANALYSIS "AC-13 as it stands"): docs/the-agents.md was docs/agents.md
// until US-023, which held AGENTS.md in any case and so would have held the
// page as an instruction file. A reader who followed a link to the old name
// finds the page and is told, in one place, that it moved and why; today only
// CODEMAP.md says so. The paragraph is checked whole, because the page already
// says "renamed" of the tiers.
func TestAC13TheAgentsPageSaysItWasRenamedFromDocsAgentsmdAndWhy(t *testing.T) {
	body := strings.ReplaceAll(page(t, "the-agents.md"), "\r\n", "\n")
	for para := range strings.SplitSeq(body, "\n\n") {
		if !strings.Contains(para, "docs/agents.md") && !strings.Contains(para, "`agents.md`") {
			continue
		}
		lower := strings.ToLower(strings.Join(strings.Fields(para), " "))
		moved := false
		for _, word := range []string{"renamed", "moved", "was called", "used to be"} {
			moved = moved || strings.Contains(lower, word)
		}
		// Why: AGENTS.md is matched without regard to case, so a page called
		// agents.md would be held as an instruction file.
		if moved && strings.Contains(lower, "case") {
			return
		}
	}
	t.Errorf("AC-13 docs/the-agents.md has no paragraph that names its old name, docs/agents.md, says it moved, and says why (AGENTS.md is matched in any case)")
}
