package pathrules

import (
	"strings"
	"testing"
)

// CaseClause ends a refusal of a path that holds a protected name only as the disk
// folds case, and is empty where the name is spelt as written or there is none, so a
// refusal of AGENTS.md never calls docs/agents.md an instruction file without saying why.
func TestCaseClauseSaysSoOnlyWhereTheNameMatchesByCaseAlone(t *testing.T) {
	for _, tc := range []struct {
		path, name string
		says       bool
	}{
		{"docs/agents.md", "AGENTS.md", true},
		{"Claude.MD", "CLAUDE.md", true},
		{"docs/AGENTS.md", "AGENTS.md", false},
		{"AGENTS.md", "AGENTS.md", false},
		{"docs/other.md", "AGENTS.md", false},
		{"docs/agents.md", "", false},
	} {
		got := CaseClause(tc.path, tc.name)
		if tc.says != (got != "") {
			t.Errorf("CaseClause(%q, %q) = %q, want a clause: %v", tc.path, tc.name, got, tc.says)
			continue
		}
		if tc.says && (!strings.Contains(got, tc.path) || !strings.Contains(got, tc.name) || !strings.Contains(got, "case")) {
			t.Errorf("CaseClause(%q, %q) = %q: want it to name the path, the name and case", tc.path, tc.name, got)
		}
	}
}
