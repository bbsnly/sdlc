package policy

import (
	"strings"
	"testing"
)

// AC-12 (ANALYSIS F7, human advocate finding 2): a lowercase name such as
// docs/agents.md is held because names are matched without regard to case, so
// that it is AGENTS.md on a disk that ignores case. The refusal says that, not
// that the file is an instruction file wherever it is: on a disk that keeps
// case, no agent tool reads docs/agents.md. Today every file tool's refusal
// says "it is an instruction file, and Claude Code and other agent tools read
// one as instructions wherever it is in the project" (policy.go:580-582).
func TestAC12ALowercaseInstructionFileNameIsDescribedAsMatchedWithoutRegardToCase(t *testing.T) {
	for _, path := range []string{"docs/agents.md", "sub/claude.md", "a/Claude.md", "Agents.MD", "CLAUDE.local.MD"} {
		for _, tool := range agWriters {
			got := Evaluate(Request{Tool: tool, Agent: "sdlc-implementer", Path: path, Story: "A-1"})
			if got.Rule != "write-protected-path" {
				t.Fatalf("AC-12 %s to %s: rule %q; want write-protected-path", tool, path, got.Rule)
			}
			if said := strings.ToLower(got.Reason); !strings.Contains(said, "case") {
				t.Errorf("AC-12 %s to %s does not say the name is matched without regard to case:\n%s", tool, path, got.Reason)
			}
		}
	}
	// The name spelt as the agent tools spell it is an instruction file, and its
	// refusal says so.
	for _, path := range []string{"AGENTS.md", "sub/CLAUDE.md", "CLAUDE.local.md"} {
		got := Evaluate(Request{Tool: "Write", Agent: "sdlc-implementer", Path: path, Story: "A-1"})
		if got.Rule != "write-protected-path" || !strings.Contains(got.Reason, "instruction") {
			t.Errorf("AC-12 Write to %s: rule %q, reason %q; want write-protected-path naming an instruction file", path, got.Rule, got.Reason)
		}
	}
}
