package scaffold

import (
	"strings"
	"testing"
)

// US-131 AC-8 and the settled IQ-9: the contract `sdlc init` writes says, under
// Allowed dependencies, that the loop may add an open-source dependency whose
// licence the project's licence check accepts, recording the choice and the
// alternatives it weighed with `sdlc decide`; and it keeps a TBD naming that
// check, as the template's other lines do. A project whose contract says
// otherwise keeps its own rule, since the agents read the project's contract.
func TestAC8TheContractTemplateLetsTheLoopAddALicensedDependencyAndRecordIt(t *testing.T) {
	t.Parallel()
	raw, err := templates.ReadFile("templates/contract.md")
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "- Allowed dependencies:") {
			continue
		}
		parts = append(parts, line)
		for _, more := range lines[i+1:] {
			if !strings.HasPrefix(more, "  ") {
				break
			}
			parts = append(parts, strings.TrimSpace(more))
		}
	}
	item := strings.Join(parts, " ")
	if item == "" {
		t.Fatal("AC-8 the contract template has no Allowed dependencies line")
	}
	lower := strings.ToLower(item)
	for _, word := range []string{"open-source", "licence", "sdlc decide", "alternative", "tbd"} {
		if !strings.Contains(lower, word) {
			t.Errorf("AC-8 the Allowed dependencies line does not say %q: %s", word, item)
		}
	}
}
