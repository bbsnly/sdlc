package shellpolicy

// US-108 AC-6, the spellings of claude the planner found while writing plan
// Revision 5 (step 13.4): a name split by quotes, a backslash or a brace
// expansion holds no "claude" in its raw text, and the second-session reading
// returned before it lexed the line, so a session that holds no story could
// start Claude Code on a person's slash command. The shell reads each as
// claude. Only the reproduced spellings are here.

import "testing"

// splitClaude are the spellings reproduced through HumanDecisions, each
// starting a session whose prompt is a person's command.
var splitClaude = []string{
	`cl""aude -p '/sdlc:approve US-1'`,
	`cl''aude -p '/sdlc:approve US-1'`,
	`c\laude -p '/sdlc:approve US-1'`,
	`cl{a,}ude -p '/sdlc:approve US-1'`,
}

// AC-6: a second session of the project is refused a session started under a
// split spelling of claude, as it is refused one started under the plain name.
func TestAC6ASecondSessionIsRefusedClaudeSpelledWithQuotesABackslashOrBraces(t *testing.T) {
	tempHomeState(t)
	none := func(string) bool { return false }
	for _, command := range splitClaude {
		f, ok := HumanDecisions(command, false, none)
		if !ok || f.Rule != "story-stays-in-its-session" || f.Reason == "" || f.Route == "" {
			t.Errorf("AC-6 %q = %+v, %v; want it refused to a second session as story-stays-in-its-session, with a reason and a route", command, f, ok)
		}
	}
}

// AC-6: the session holding the story already refuses these spellings; they
// stay refused there.
func TestAC6TheSessionHoldingTheStoryRefusesClaudeSpelledWithQuotesABackslashOrBraces(t *testing.T) {
	for _, command := range splitClaude {
		refused(t, command, ready, "story-stays-in-its-session")
	}
}

// AC-6: reading past the quotes is not refusing every line that has some. A
// line with quotes and no claude, and a split claude that names no person's
// command, are let through to a second session as their plain spellings are.
func TestAC6ASecondSessionIsStillAllowedALineWithQuotesThatStartsNoSessionOnAPersonsCommand(t *testing.T) {
	tempHomeState(t)
	none := func(string) bool { return false }
	for _, command := range []string{
		"echo hello",
		`echo "hel""lo" 'wor''ld' \!`,
		`cl""aude --version`,
	} {
		if f, ok := HumanDecisions(command, false, none); ok {
			t.Errorf("AC-6 %q was refused to a second session: %+v", command, f)
		}
	}
}
