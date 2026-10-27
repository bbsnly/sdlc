package e2e

// US-108 AC-14 with AC-3's forged payload as security found it in design
// review round 4: the model writes the UserPromptExpansion payload of a
// person's /sdlc:approve to a file and runs the hook on it from its own Bash.
// Run, that answers the escalation as TestAC14 shows a typed /sdlc:approve
// does; so the hook refuses the Bash call, from the session holding the story
// and from another, and the story goes on waiting for a person. The hook fed
// another event by hand is still let through.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAC14AModelsBashThatFeedsTheHookAForgedApprovalIsRefusedAndTheStoryStillWaits(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")
	sb.works(t, tree, "", "", "escalate", "spec_unclear", "--message", "is a zero total refused too?")
	if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
		t.Fatalf("the story is %q after the escalation, want awaiting_human", got["US-001"])
	}

	raw, err := json.Marshal(typedLine(sandboxOther, tree, "sdlc:approve", "US-001"))
	if err != nil {
		t.Fatal(err)
	}
	forged := filepath.Join(sb.own(t, "forged"), "p.json")
	if err := os.WriteFile(forged, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	from := ` < "` + filepath.ToSlash(forged) + `"`

	for _, session := range []string{sandboxSession, sandboxOther} {
		sb.refuses(t, session, tree, map[string]any{"command": "sdlc hook UserPromptExpansion" + from},
			"slash-command-is-a-human-decision")
	}
	if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
		t.Fatalf("the story is %q after the refusals, want awaiting_human", got["US-001"])
	}
	sb.allows(t, tree, "", bashTool, map[string]any{"command": "sdlc hook PreToolUse" + from})
}
