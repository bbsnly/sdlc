package sdlcerr

// US-108 AC-15: approve, unfreeze, ack, remap, release, parallel and staffing
// assign refuse in a process whose environment carries Claude Code's markers,
// under a code of their own. Its fix is read by a person who typed `!sdlc
// approve` in a session, and by a model whose Bash ran it, so it sends the
// first to the slash command and to a terminal outside Claude Code, in that
// order, and the second nowhere it could follow.
//
// The code is SDLC-E0079, the next after the last one released before the
// story; TestEveryCodeKeepsTheNumberItWasReleasedWith pins the number.

import (
	"strings"
	"testing"
)

// inClaudeCodeID is the code AC-15's refusal carries.
const inClaudeCodeID = "SDLC-E0079"

func TestAC15APersonsCommandRunInsideClaudeCodeHasACodeWhoseFixNamesTheSlashCommandFirstAndATerminalOutsideClaudeCodeSecond(t *testing.T) {
	code := registered(inClaudeCodeID)
	if code == (Code{}) {
		t.Fatalf("the catalogue has no %s: a person's command run inside Claude Code has no code to be refused under", inClaudeCodeID)
	}
	fix := fixFor(code)
	slash := strings.Index(fix, "/sdlc:")
	outside := strings.Index(fix, "outside Claude Code")
	switch {
	case slash < 0:
		t.Errorf("%s's fix names no slash command, so a person in Claude Desktop has nowhere to go:\n%s", inClaudeCodeID, fix)
	case outside < 0:
		t.Errorf("%s's fix does not send a person to a terminal outside Claude Code:\n%s", inClaudeCodeID, fix)
	case outside < slash:
		t.Errorf("%s's fix names the terminal before the slash command (AC-12):\n%s", inClaudeCodeID, fix)
	}
	// The other two codes for a session stay as they were (AC-8), and this one
	// is not either of them under a new number.
	for _, other := range []string{"SDLC-E0061", "SDLC-E0074"} {
		if fixFor(registered(other)) == fix {
			t.Errorf("%s has %s's fix word for word", inClaudeCodeID, other)
		}
	}

	documented := documentedCodes(t)
	body, ok := documented[inClaudeCodeID]
	if !ok || isRetired(body) {
		t.Fatalf("%s has no live `### %s` section in %s", inClaudeCodeID, inClaudeCodeID, troubleshootingPath)
	}
	for _, want := range []string{"/sdlc:", "outside Claude Code"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s's section in %s does not say %q", inClaudeCodeID, troubleshootingPath, want)
		}
	}
}
