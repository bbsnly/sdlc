package sdlcerr

// US-089 AC-6: where a landed story's change to CODEMAP.md could not be made
// or committed, the stop says so under a code of its own. The landing stands,
// so the code is a warning there, and its fix is the whole of what a person
// is told: which command to run and what its two other forms do.
//
// The code is found by what its fix says and not by its number. Other stories
// are under way beside this one and may take the next number first; the
// number is whichever the catalogue gives it above SDLC-E0075, the last one
// released before the story.

import (
	"strconv"
	"strings"
	"testing"
)

// mapChangeCodes is every code above SDLC-E0075 whose fix names the repair.
func mapChangeCodes() []Code {
	var out []Code
	for _, e := range catalogue {
		n, err := strconv.Atoi(strings.TrimPrefix(e.code.id, "SDLC-E"))
		if err != nil || n <= 75 {
			continue
		}
		if strings.Contains(fixFor(e.code), "sdlc remap") {
			out = append(out, e.code)
		}
	}
	return out
}

func TestAC6TheMapChangeThatDidNotReachTrunkHasACodeThatSaysWhatAPersonRuns(t *testing.T) {
	codes := mapChangeCodes()
	if len(codes) == 0 {
		t.Fatal("the catalogue has no code above SDLC-E0075 whose fix names `sdlc remap`: " +
			"a map change that did not reach trunk has nothing to be said under")
	}
	documented := documentedCodes(t)
	said := false
	for _, code := range codes {
		id := code.String()
		body, ok := documented[id]
		if !ok || isRetired(body) {
			t.Errorf("%s has no live `### %s` section in %s", id, id, troubleshootingPath)
			continue
		}
		for _, want := range []string{"CODEMAP.md", "sdlc remap"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s's section in %s does not say %q", id, troubleshootingPath, want)
			}
		}
		fix := fixFor(code)
		if strings.Contains(fix, "--replace") && strings.Contains(fix, "--drop") {
			said = true
		}
		// The command is a person's, and the fix is read by a session too:
		// one that takes "run sdlc remap" as its own next step is sent to the
		// one thing the hook refuses it (as SDLC-E0074's fix learnt).
		lower := strings.ToLower(fix)
		if !strings.Contains(lower, "person") && !strings.Contains(lower, "terminal") {
			t.Errorf("%s's fix does not say the command is a person's, in their own terminal: %s", id, fix)
		}
		if strings.Contains(fix, "\\") {
			t.Errorf("%s's fix prints a backslash: %s", id, fix)
		}
	}
	if !said {
		t.Error("no code's fix names both --replace and --drop: a person whose section no longer applies is not told what they may choose")
	}
}

// The troubleshooting page is where the three forms are told apart: what the
// plain one does, what --replace puts over what, and that --drop commits
// nothing.
func TestAC9TheTroubleshootingPageTellsTheThreeFormsOfTheRepairApart(t *testing.T) {
	codes := mapChangeCodes()
	if len(codes) == 0 {
		t.Fatal("the catalogue has no code above SDLC-E0075 whose fix names `sdlc remap`")
	}
	documented := documentedCodes(t)
	var page strings.Builder
	for _, code := range codes {
		page.WriteString(documented[code.String()])
	}
	for _, want := range []string{"sdlc remap", "--replace", "--drop", "CODEMAP.md", ".git/sdlc/codemap"} {
		if !strings.Contains(page.String(), want) {
			t.Errorf("the sections for the map change's codes in %s do not say %q", troubleshootingPath, want)
		}
	}
}
