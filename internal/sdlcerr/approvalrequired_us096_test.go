package sdlcerr

// US-096 AC-9 and AC-10 (from US-097): the fix of SDLC-E0037 says what a person
// types and, apart from it, what an assistant does, with no literal "..." in a
// command a person would paste, and docs/troubleshooting.md quotes it; the
// page's passage on merging a kept worktree by hand rebases onto the trunk by
// name. The story adds no code and retires none, so no `released` row changes.

import (
	"regexp"
	"strings"
	"testing"
)

// assistantSentence is where a fix turns to the assistant: a sentence, or a
// clause after a semicolon, that begins with it.
var assistantSentence = regexp.MustCompile(`(?:^|[.;] )[Aa]n assistant\b[^.]*(?:\.|$)`)

// personAndAssistant is fix split into the part an assistant is told, and the
// rest, which is the person's; ok is false where no part is the assistant's.
func personAndAssistant(fix string) (person, assistant string, ok bool) {
	flat := strings.Join(strings.Fields(fix), " ")
	at := assistantSentence.FindStringIndex(flat)
	if at == nil {
		return flat, "", false
	}
	return flat[:at[0]] + flat[at[1]:], flat[at[0]:at[1]], true
}

// AC-9: the catalogue's fix, which `sdlc gate commit pass` gives, says what a
// person types -- the slash command first, then the terminal command, as US-108
// AC-12 has it -- and apart from it what an assistant does, which is to hand
// the story over with `sdlc escalate` and stop. Nothing in it is a literal
// "...", which a person pastes as it stands.
func TestAC9E0037sFixSaysWhatAPersonTypesAndApartFromItWhatAnAssistantDoes(t *testing.T) {
	fix := fixFor(ApprovalRequired)
	for _, dots := range []string{"...", "…"} {
		if strings.Contains(fix, dots) {
			t.Errorf("SDLC-E0037's fix prints a literal %q, which a person pastes as it stands:\n%s", dots, fix)
		}
	}
	person, assistant, ok := personAndAssistant(fix)
	if !ok {
		t.Fatalf("SDLC-E0037's fix has no sentence of its own for an assistant:\n%s", fix)
	}
	if !strings.Contains(assistant, `"sdlc escalate pre_commit_approval --message `) || !strings.Contains(assistant, "stop") {
		t.Errorf("SDLC-E0037's fix does not tell an assistant to hand the story over with sdlc escalate and stop:\n%s", assistant)
	}
	for _, never := range []string{"sdlc approve", "/sdlc:approve"} {
		if strings.Contains(assistant, never) && !strings.Contains(assistant, "not") {
			t.Errorf("SDLC-E0037's fix sends an assistant to %s, a person's command:\n%s", never, assistant)
		}
	}
	if !strings.Contains(person, `"/sdlc:approve <ID>", or "sdlc approve <ID>"`) || !strings.Contains(strings.ToLower(person), "a person") {
		t.Errorf("SDLC-E0037's fix does not say what a person types, slash command first:\n%s", person)
	}
	if strings.Contains(person, "sdlc escalate") {
		t.Errorf("SDLC-E0037's fix sets the assistant's sdlc escalate among what a person does:\n%s", person)
	}
}

// AC-9: docs/troubleshooting.md quotes the fix, word for word, under
// `### SDLC-E0037`, and no command the section gives a person to paste carries
// a literal "...".
func TestAC9TheTroubleshootingPageQuotesE0037sFixAndPastesNoDots(t *testing.T) {
	body, ok := documentedCodes(t)["SDLC-E0037"]
	if !ok {
		t.Fatalf("%s has no `### SDLC-E0037` section", troubleshootingPath)
	}
	var unquoted []string
	for line := range strings.SplitSeq(body, "\n") {
		unquoted = append(unquoted, strings.TrimPrefix(strings.TrimSpace(line), ">"))
	}
	flat := strings.Join(strings.Fields(strings.Join(unquoted, "\n")), " ")
	if fix := strings.Join(strings.Fields(fixFor(ApprovalRequired)), " "); !strings.Contains(flat, fix) {
		t.Errorf("the SDLC-E0037 section of %s does not quote the fix word for word:\n%s", troubleshootingPath, fix)
	}
	for _, command := range pastedCommands(body) {
		if strings.Contains(command, "...") || strings.Contains(command, "…") {
			t.Errorf("the SDLC-E0037 section gives a person a command with a literal \"...\" to paste: %s", command)
		}
	}
}

// pastedCommands is every command a section gives a person to paste: a code
// span or a fenced line that runs sdlc or git.
func pastedCommands(body string) []string {
	var out []string
	inFence := false
	for line := range strings.SplitSeq(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			if c, ok := strings.CutPrefix(trimmed, "$ "); ok {
				out = append(out, c)
			}
		}
	}
	folded := strings.Join(strings.Fields(fenced.ReplaceAllString(body, " ")), " ")
	for _, m := range regexp.MustCompile("`([^`]+)`").FindAllStringSubmatch(folded, -1) {
		if strings.HasPrefix(m[1], "sdlc ") || strings.HasPrefix(m[1], "git ") || strings.HasPrefix(m[1], "/sdlc:") {
			out = append(out, m[1])
		}
	}
	return out
}

// AC-10: the passage on merging a kept worktree by hand gives the rebase onto
// the trunk as a name to fill in, `git rebase <trunk>`, and nowhere does the
// page tell a person to rebase onto a literal main.
func TestAC10TheByHandMergeRebasesOntoTheTrunkByName(t *testing.T) {
	body, ok := documentedCodes(t)["SDLC-E0053"]
	if !ok {
		t.Fatalf("%s has no `### SDLC-E0053` section, which holds the passage on merging a kept worktree by hand", troubleshootingPath)
	}
	flat := strings.Join(strings.Fields(body), " ")
	at := strings.Index(flat, "cannot fast-forward")
	if at < 0 {
		t.Fatalf("the SDLC-E0053 section no longer says what to do when git cannot fast-forward the merge")
	}
	if passage := flat[at:min(len(flat), at+300)]; !strings.Contains(passage, "`git rebase <trunk>`") {
		t.Errorf("the by-hand merge does not give the rebase as `git rebase <trunk>`:\n%s", passage)
	}
	for id, section := range documentedCodes(t) {
		if strings.Contains(section, "git rebase main") {
			t.Errorf("the %s section of %s tells a person to run `git rebase main`, a literal trunk", id, troubleshootingPath)
		}
	}
}
