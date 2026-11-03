package sdlcerr

// US-116 AC-12 and AC-7: SDLC-E0079 is raised for six of a person's commands,
// and its fix, which `sdlc help` and the docs read, and its section in the
// troubleshooting page tell a person what to do for exactly those six: the
// slash command in Claude Code, a "!" line in Claude Desktop, or a terminal
// outside Claude Code. The tests hold what is named to the six commands the
// code is raised for (personsCommands), whatever a command is called, so they
// say nothing of any one word.

import (
	"regexp"
	"strings"
	"testing"
)

// quotedCommand is a terminal command the fix names in quotes: "sdlc approve",
// "sdlc staffing assign".
var quotedCommand = regexp.MustCompile(`"(sdlc [a-z]+(?: assign)?)"`)

// backticked is each code span of a page.
var backticked = regexp.MustCompile("`([^`]+)`")

// commandsNamedIn is each sdlc command a page names in code spans, as the
// terminal form the code is raised for is spelt: "sdlc approve", "sdlc staffing
// assign". A span is read by its first word being sdlc, with a "!" before it
// where a person types it as a line in Claude Desktop; the word after it is the
// command, and the word after that only for staffing. Arguments and flags are
// not read. A span that is sdlc alone is the tool, not a command, and
// "/sdlc:approve" or "npx @bbsnly/sdlc install" does not begin with the word.
func commandsNamedIn(text string) []string {
	var named []string
	for _, m := range backticked.FindAllStringSubmatch(text, -1) {
		words := strings.Fields(strings.TrimPrefix(m[1], "!"))
		if len(words) < 2 || words[0] != "sdlc" {
			continue
		}
		command := "sdlc " + words[1]
		if words[1] == "staffing" && len(words) > 2 {
			command += " " + words[2]
		}
		named = append(named, command)
	}
	return named
}

func TestAC12TheFixOfSDLCE0079SaysWhatAPersonDoesForExactlyTheSixCommandsTheCodeIsRaisedFor(t *testing.T) {
	fix := fixFor(registered(inClaudeCodeID))
	if fix == "" {
		t.Fatalf("the catalogue has no %s", inClaudeCodeID)
	}
	raisedFor := map[string]bool{}
	for _, c := range personsCommands {
		raisedFor[c.terminal] = true
		if !strings.Contains(fix, `"`+c.slash+`"`) {
			t.Errorf("%s's fix does not name %s, the command a person types in Claude Code:\n%s", inClaudeCodeID, c.slash, fix)
		}
	}
	for _, want := range []string{
		`"sdlc approve"`,
		`"sdlc staffing assign"`,
		"with the same arguments",
		"Claude Desktop",
		`"!sdlc approve US-001"`,
		"a terminal outside Claude Code",
		"an assistant stops here and says what it would have run",
	} {
		if !strings.Contains(fix, want) {
			t.Errorf("%s's fix does not say %q:\n%s", inClaudeCodeID, want, fix)
		}
	}
	named := quotedCommand.FindAllStringSubmatch(fix, -1)
	if len(named) == 0 {
		t.Fatalf("%s's fix names no terminal command in quotes, so the pattern here reads nothing:\n%s", inClaudeCodeID, fix)
	}
	for _, m := range named {
		if !raisedFor[m[1]] {
			t.Errorf("%s's fix names %q, which the code is not raised for; it is raised for %d commands, each with a slash command:\n%s",
				inClaudeCodeID, m[1], len(personsCommands), fix)
		}
	}
}

func TestAC7TheSectionOfSDLCE0079IsLiveAndNamesExactlyTheSixCommandsTheCodeIsRaisedFor(t *testing.T) {
	body, ok := documentedCodes(t)[inClaudeCodeID]
	if !ok || isRetired(body) {
		t.Fatalf("%s has no live `### %s` section in %s", inClaudeCodeID, inClaudeCodeID, troubleshootingPath)
	}
	// The reading itself, on spellings the section uses: arguments after the
	// command word, a "!" line, sdlc alone, and a slash command.
	got := commandsNamedIn("`sdlc` runs `sdlc ack --through <commit>`, `!sdlc approve <ID>`, `/sdlc:assign`, `sdlc staffing assign <agent>` and `npx @bbsnly/sdlc install`")
	if want := "sdlc ack|sdlc approve|sdlc staffing assign"; strings.Join(got, "|") != want {
		t.Fatalf("commandsNamedIn read %q, want %q", got, want)
	}

	raisedFor := map[string]bool{}
	named := map[string]bool{}
	for _, c := range personsCommands {
		raisedFor[c.terminal] = true
		if !strings.Contains(body, c.slash) {
			t.Errorf("%s's section does not name %s", inClaudeCodeID, c.slash)
		}
	}
	for _, command := range commandsNamedIn(body) {
		named[command] = true
		if !raisedFor[command] {
			t.Errorf("%s's section names `%s`, which the code is not raised for; it is raised for %d commands", inClaudeCodeID, command, len(personsCommands))
		}
	}
	for _, c := range personsCommands {
		if !named[c.terminal] {
			t.Errorf("%s's section does not name `%s`", inClaudeCodeID, c.terminal)
		}
	}
	for _, want := range []string{"`!sdlc approve <ID>`", "outside Claude Code", "If you are an assistant, stop here"} {
		if !strings.Contains(body, want) {
			t.Errorf("%s's section does not say %q", inClaudeCodeID, want)
		}
	}
}
