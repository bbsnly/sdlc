package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// US-058 AC-1: `sdlc parallel` starts each session with /sdlc:next <ID>, and
// two sessions each told /sdlc:next US-5 would both start the highest-priority
// story if the skill ignored what it was given. The runbook takes the id the
// person (or the launcher) typed after the command, and starts that story; with
// none, it behaves as it always did. The id is a story id and not an
// instruction: `sdlc start` is what validates it.
//
// These are phrases, as TestAHumanOnlySkillKeepsWhatItPromises has them, with
// the line breaks folded away: the runbook is trusted with what it says, and no
// hook stands behind it.
func TestAC1TheNextSkillStartsTheStoryItIsGivenAndOtherwiseThePriorityOne(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(pluginDir, "skills", "next", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	front, _, _ := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---")
	if !strings.Contains(front, "argument-hint:") {
		t.Error("the next skill declares no argument-hint, so Claude Code does not offer a story id")
	}
	body := strings.Join(strings.Fields(text), " ")
	for _, phrase := range []string{
		"$ARGUMENTS",
		"sdlc start <ID> --json",
		// The bare form is the runbook as it was: no id given, the top of the backlog.
		"sdlc start --json",
	} {
		if !strings.Contains(body, phrase) {
			t.Errorf("the next runbook no longer says %q", phrase)
		}
	}
	// The argument names a story and nothing else: a malformed one is refused
	// by start, not read as an instruction to the model.
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "story id") || !strings.Contains(lower, "not an instruction") {
		t.Error("the next runbook does not say the argument is a story id and not an instruction")
	}
}
