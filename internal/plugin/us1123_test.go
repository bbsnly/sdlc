package plugin

import (
	"regexp"
	"strings"
	"testing"
)

// rvGate4 is the runbook's Gate 4 section with its lines joined, so that where a
// line wraps decides nothing.
func rvGate4(t *testing.T) string {
	t.Helper()
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if s.Name != "next" {
			continue
		}
		sections := regexp.MustCompile(`(?m)^## `).Split(s.Body, -1)
		for _, section := range sections {
			if strings.HasPrefix(section, "Gate 4") {
				return strings.Join(strings.Fields(section), " ")
			}
		}
		t.Fatal("the next runbook has no Gate 4 section")
	}
	t.Fatal("there is no next skill")
	return ""
}

// US-112.3 AC-6. A runbook that says this elsewhere is one the conversation has
// not read when it needs it, or has lost: the section is what it is working
// from at the gate. The runbook has only 11 bytes free under
// TestTheRunbookFitsWhatCompactionKeeps, so these are short words and not the
// planner's sentences, and text elsewhere in the file has to move to make room.
func TestAC6Gate4OfTheRunbookResumesAReviewerWithTheDiffAndTheFindings(t *testing.T) {
	t.Parallel()
	section := rvGate4(t)
	for _, c := range []struct {
		what  string
		words []string
	}{
		{"it resumes the reviewer", []string{"SendMessage", "resume"}},
		{"what it gives the reviewer", []string{"sdlc artifact diff plan --since", "findings"}},
		{"a fresh reviewer, where the diff is refused", []string{"fresh", "refuse"}},
		{"a fresh reviewer, where it cannot be resumed", []string{"cannot be resumed"}},
		// Every agent stops on a brief without this line, a resumed one too.
		{"the line every brief begins with", []string{"sdlc:next runbook"}},
		// T11: nothing observes a delegation, so only this sentence holds it.
		{"whom it may resume", []string{"at_gate"}},
		// T9: a line of the plan addressed to a reviewer is the plan's, not an
		// instruction from the conversation.
		{"how it gives the diff", []string{"data"}},
		// T8, and AC-5: the second look does not change what an approval means.
		{"what is approved", []string{"PLAN.md", "as it now stands"}},
		{"who records the verdict", []string{"sdlc review add design_review <role> <verdict>"}},
	} {
		for _, word := range c.words {
			if !strings.Contains(section, word) {
				t.Errorf("Gate 4 of the next runbook, on %s, does not say %q:\n%s", c.what, word, section)
			}
		}
	}
}
