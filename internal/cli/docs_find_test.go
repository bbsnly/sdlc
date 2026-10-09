package cli

import (
	"strings"
	"testing"
)

// The hook reads the operators between a find's tests, and a reader of
// docs/enforcement.md has to be able to tell which: a refusal for what a
// negated or alternated test picks names a file the command never spelled,
// and the section for the rule is where they look it up.
func TestAC5TheEnforcementPageSaysWhichOfFindsOperatorsAreRead(t *testing.T) {
	t.Parallel()
	doc := page(t, "enforcement.md")
	_, section, found := strings.Cut(doc, "### `loop-state-through-the-tool`")
	if !found {
		t.Fatal("docs/enforcement.md has no section for the loop-state-through-the-tool rule")
	}
	if next := strings.Index(section, "\n### "); next >= 0 {
		section = section[:next]
	}
	for _, operator := range []string{"`!`", "`-not`", "`-o`", "`-or`"} {
		if !strings.Contains(section, operator) {
			t.Errorf("the loop-state-through-the-tool section of docs/enforcement.md never mentions find's %s", operator)
		}
	}
}
