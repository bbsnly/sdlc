package policy

import (
	"strings"
	"testing"
)

// US-131 T-4 and the settled IQ-6: DECISIONS.md is written by `sdlc decide` alone.
// The file tools are refused it for every agent and the main conversation, in
// every story and in every spelling the disk finds the same file by, and the
// refusal sends them to the command. It is not a gate document, so
// `sdlc artifact write` does not take it either (internal/cli holds that).
func TestAC3DecisionsMdIsWrittenOnlyByTheTool(t *testing.T) {
	t.Parallel()
	const rule = "decisions-are-written-by-the-tool"
	for _, agent := range []string{"", "sdlc:researcher", "sdlc:implementer", "sdlc:sdet", "sdlc:architect", "sdlc:bookkeeper", "sdlc:project-manager"} {
		for _, path := range []string{
			".sdlc/stories/A-1/DECISIONS.md",
			".sdlc/stories/OLD-9/DECISIONS.md",
			".sdlc/stories/A-1/decisions.md",
			".sdlc/stories/A-1/DECISIONS.MD",
			".SDLC/stories/A-1/DECISIONS.md",
			".ſdlc/stories/A-1/DECISIONS.md",
		} {
			for _, tool := range []string{"Write", "Edit", "MultiEdit", "NotebookEdit"} {
				v := Evaluate(Request{Tool: tool, Agent: agent, Path: path, Story: "A-1"})
				if v.Allowed || v.Rule != rule {
					t.Errorf("AC-3 %s by %q to %s: allowed %v under %q, want refused under %s", tool, agent, path, v.Allowed, v.Rule, rule)
					continue
				}
				if !strings.Contains(v.Route, "sdlc decide") {
					t.Errorf("AC-3 the refusal of %s does not send the agent to `sdlc decide`: %s", path, v.Route)
				}
			}
		}
	}
	for _, path := range []string{".sdlc/stories/A-1/scratch/DECISIONS.md", ".sdlc/stories/A-1/decisions-notes.md"} {
		if v := Evaluate(Request{Tool: "Write", Agent: "sdlc:researcher", Path: path, Story: "A-1"}); !v.Allowed {
			t.Errorf("AC-3 %s, which is not the decisions file, was refused by %s", path, v.Rule)
		}
	}
}
