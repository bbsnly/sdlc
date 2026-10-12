package cli

// US-059, the words: how many times a refused landing is stopped again before
// a person is asked.
//
// The runbook told the session to stop again once and to escalate on "a
// second refusal". It now says up to three times, for the three ways that are
// stopped again -- SDLC-E0006, SDLC-E0054 saying stop again, and a stop cut
// short -- and for nothing else (AC-7). The loop page and the stopping guide
// give the same number, and SDLC-E0054's own fix, the catalogue's and its
// troubleshooting entry say it too, where they said to hand over on the second
// refusal (AC-9).
//
// These hold the meaning and not a sentence: the number, written as the word
// "three", beside the stopping again and the person it ends with, in the
// passage that is about it. Helpers carry an lt prefix, for landing text.

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

var (
	ltThree = regexp.MustCompile(`\bthree\b`)
	ltStop  = regexp.MustCompile(`\bstop`)
	ltAgain = regexp.MustCompile(`\bagain\b`)
	ltWhom  = regexp.MustCompile(`\bperson\b|\bescalat`)
	// ltOnce is the bound the story takes away, however it is put.
	ltOnce = regexp.MustCompile(`\bsecond\b|\btwice\b|\bonce more\b`)
)

// ltReach is how far either side of the number the stopping again and the
// person have to be found: about three lines of a page.
const ltReach = 240

// ltFlat is text in lower case with its white space collapsed, so that a line
// break in a page is not a difference.
func ltFlat(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// ltSaysThree reports whether a passage says a refused landing is stopped
// again three times before a person is asked: the word three, with stopping
// again and the person, or the escalation, within ltReach of it. A three
// about something else, further off, does not pass for it.
func ltSaysThree(passage string) bool {
	flat := ltFlat(passage)
	for _, at := range ltThree.FindAllStringIndex(flat, -1) {
		near := flat[max(0, at[0]-ltReach):min(len(flat), at[1]+ltReach)]
		if ltStop.MatchString(near) && ltAgain.MatchString(near) && ltWhom.MatchString(near) {
			return true
		}
	}
	return false
}

// ltSection is the part of a page under heading, up to the next heading of
// the same depth or less.
func ltSection(t *testing.T, text, heading string) string {
	t.Helper()
	depth := len(heading) - len(strings.TrimLeft(heading, "#"))
	var out []string
	in := false
	for line := range strings.SplitSeq(text, "\n") {
		if in && strings.HasPrefix(line, "#") {
			if d := len(line) - len(strings.TrimLeft(line, "#")); d <= depth && strings.HasPrefix(line[d:], " ") {
				break
			}
		}
		if !in && (line == heading || strings.HasPrefix(line, heading+" ")) {
			in = true
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	if !in {
		t.Fatalf("there is no %q section", heading)
	}
	return strings.Join(out, "\n")
}

// ltItems is every item of the lists in a passage of markdown, each with the
// lines that continue it, flattened.
func ltItems(passage string) []string {
	var items []string
	open := false
	for line := range strings.SplitSeq(passage, "\n") {
		switch {
		case strings.HasPrefix(line, "- "):
			items = append(items, line[2:])
			open = true
		case open && strings.HasPrefix(line, "  ") && strings.TrimSpace(line) != "":
			items[len(items)-1] += " " + line
		default:
			open = false
		}
	}
	for i := range items {
		items[i] = ltFlat(items[i])
	}
	return items
}

// ltItem is the one list item that names needle.
func ltItem(t *testing.T, what string, items []string, needle string) string {
	t.Helper()
	var found []string
	for _, item := range items {
		if strings.Contains(item, strings.ToLower(needle)) {
			found = append(found, item)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s has %d list items naming %q, want the one that says what to do about it:\n%s",
			what, len(found), needle, strings.Join(items, "\n"))
	}
	return found[0]
}

// ltRunbook is the /sdlc:next runbook, and its Gate 9, where the landing is.
func ltRunbook(t *testing.T) (raw, gate9 string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "plugin", "skills", "next", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b), ltSection(t, string(b), "## Gate 9")
}

// ltStoppedAgain is the three ways a stop is refused, or ends, that the
// runbook answers by stopping again, as it names each.
var ltStoppedAgain = []string{"SDLC-E0006", "SDLC-E0054", "cut short"}

// AC-7: the runbook tells the session to stop again up to three times before
// it escalates, for SDLC-E0006, SDLC-E0054 saying stop again and a stop cut
// short; it no longer escalates on a second refusal; SDLC-E0075 and any other
// code are escalated at once; and it is still within the bytes compaction
// keeps.
func TestAC7TheRunbookStopsARefusedLandingAgainUpToThreeTimesBeforeItEscalates(t *testing.T) {
	raw, gate9 := ltRunbook(t)
	items := ltItems(gate9)

	t.Run("AC-7 each way that is stopped again is stopped up to three times and then escalated", func(t *testing.T) {
		for _, way := range ltStoppedAgain {
			item := ltItem(t, "the runbook's Gate 9", items, way)
			if !ltThree.MatchString(item) || !ltStop.MatchString(item) || !ltAgain.MatchString(item) {
				t.Errorf("for %s the runbook does not say to stop again up to three times:\n%s", way, item)
			}
			if !strings.Contains(item, "escalat") {
				t.Errorf("for %s the runbook does not say to escalate once the three are spent:\n%s", way, item)
			}
			if ltOnce.MatchString(item) {
				t.Errorf("for %s the runbook still gives the bound it had, one stop more:\n%s", way, item)
			}
		}
	})
	t.Run("AC-7 a second refusal of that kind is no longer what escalates", func(t *testing.T) {
		for _, item := range items {
			if ltOnce.MatchString(item) {
				t.Errorf("the runbook's Gate 9 still escalates on a second refusal:\n%s", item)
			}
		}
	})
	t.Run("AC-7 a conflict and any other code are escalated at once", func(t *testing.T) {
		item := ltItem(t, "the runbook's Gate 9", items, "SDLC-E0075")
		if !strings.Contains(item, "escalat") {
			t.Errorf("the runbook does not escalate SDLC-E0075:\n%s", item)
		}
		if !regexp.MustCompile(`\b(another|any other|other) codes?\b`).MatchString(item) {
			t.Errorf("the runbook does not say that any other code is escalated as SDLC-E0075 is:\n%s", item)
		}
		if ltThree.MatchString(item) || ltAgain.MatchString(item) {
			t.Errorf("the runbook stops again on SDLC-E0075 or another code, which are escalated at once:\n%s", item)
		}
	})
	t.Run("AC-7 the three are not for the reviews run again or for a failed command", func(t *testing.T) {
		counted := ltItem(t, "the runbook's Gate 9", items, "SDLC-E0006")
		for _, code := range []string{"SDLC-E0029", "SDLC-E0055"} {
			item := ltItem(t, "the runbook's Gate 9", items, code)
			if item == counted || ltThree.MatchString(item) {
				t.Errorf("the runbook counts %s among the three stops, and it is not one of them:\n%s", code, item)
			}
		}
	})
	t.Run("AC-7 the runbook is within the bytes compaction keeps", func(t *testing.T) {
		// The cap TestTheRunbookFitsWhatCompactionKeeps holds, in
		// internal/plugin.
		const maxBytes = 17500
		if len(raw) > maxBytes {
			t.Errorf("the next runbook is %d bytes, over %d", len(raw), maxBytes)
		}
	})
}

// AC-9: docs/the-loop.md, where it says a landing is stopped again, and the
// stopping guide, where it says what a refused landing leaves, give the number
// of times before a person is asked, and it is the runbook's.
func TestAC9TheLoopPageAndTheStoppingGuideGiveTheRunbooksNumberOfStops(t *testing.T) {
	_, gate9 := ltRunbook(t)
	if item := ltItem(t, "the runbook's Gate 9", ltItems(gate9), "SDLC-E0006"); !ltThree.MatchString(item) {
		t.Errorf("the runbook gives no three for the pages to agree with:\n%s", item)
	}
	for _, p := range []struct{ name, page, heading string }{
		{"AC-9 docs/the-loop.md, Done when it lands", "the-loop.md", "### Done when it lands"},
		{"AC-9 docs/guides/stopping-and-resuming-a-story.md, When the story is finished",
			filepath.Join("guides", "stopping-and-resuming-a-story.md"), "## When the story is finished"},
	} {
		t.Run(p.name, func(t *testing.T) {
			if !ltSaysThree(ltSection(t, page(t, p.page), p.heading)) {
				t.Errorf("docs/%s, under %q, does not say a refused landing is stopped again up to three times "+
					"before a person is asked, as the runbook does", filepath.ToSlash(p.page), p.heading)
			}
		})
	}
}

// AC-7 and AC-9: SDLC-E0006 is one of the three ways the runbook stops again
// up to three times, and the refusal a stop meets it with is "another story is
// still landing". Its fix, and the passage of docs/troubleshooting.md about
// another story landing, told an assistant to run the stop once more and to
// hand the story over when refused again, which the runbook would then
// contradict. Both say three.
//
// SDLC-E0006's catalogue fix, the fix where another stop is landing the same
// story, and the line a waiting stop prints about a stop cut short give no
// count of further stops, so nothing here holds them to one.
func TestAC7AndAC9TheRefusalForAnotherStoryStillLandingSaysThreeAsItsTroubleshootingPassageDoes(t *testing.T) {
	t.Parallel()
	t.Run("AC-7 and AC-9 the fix of the refusal", func(t *testing.T) {
		t.Parallel()
		main := ldRepository(t, nil)
		otLandingInFront(t, main)
		cfg, err := config.Load(main)
		if err != nil {
			t.Fatal(err)
		}
		// Asked at once, as a stop asks before it waits: the refusal a stop
		// ends with after its wait is this one, with what happened reworded.
		_, err = store.New(&config.Project{Root: main, Config: cfg}).LockLandings("US-001", 0)
		var refusal *sdlcerr.Error
		if !errors.As(err, &refusal) || refusal.Code != sdlcerr.StateUnwritable {
			t.Fatalf("a landing asked for while another holds the lock gave %v, want SDLC-E0006", err)
		}
		if !ltSaysThree(refusal.Fix) {
			t.Errorf("the fix does not say to stop again up to three times before the story goes to a person:\n%s", refusal.Fix)
		}
		if ltOnce.MatchString(ltFlat(refusal.Fix)) {
			t.Errorf("the fix still hands the story over after one further stop:\n%s", refusal.Fix)
		}
	})
	t.Run("AC-7 and AC-9 docs/troubleshooting.md, another story is landing", func(t *testing.T) {
		t.Parallel()
		items := ltItems(ltSection(t, page(t, "troubleshooting.md"), "### SDLC-E0006"))
		landing := ltItem(t, "docs/troubleshooting.md, SDLC-E0006", items, "another story is landing")
		if !ltSaysThree(landing) {
			t.Errorf("the passage does not say to stop again up to three times before the story goes to a person:\n%s", landing)
		}
		if ltOnce.MatchString(landing) {
			t.Errorf("the passage still hands the story over after one further stop:\n%s", landing)
		}
	})
}

// AC-9 and the person's fifth answer: SDLC-E0054 where trunk moved while the
// landing ran is the refusal that says stop again. Its fix as the stop prints
// it, the catalogue's fix and its entry in docs/troubleshooting.md all said, or
// left unsaid, when to hand over; each now says three, and only for that case.
func TestAC9TheRefusalThatSaysStopAgainSaysThreeAsItsCatalogueFixAndTroubleshootingEntryDo(t *testing.T) {
	t.Parallel()
	t.Run("AC-9 the fix the stop prints", func(t *testing.T) {
		t.Parallel()
		// A configured command moves trunk under the landing, as
		// TestALandingCommandThatMovesTrunkIsCaughtWhenEverythingIsAskedAgain
		// has it.
		main := lxRepository(t, map[string]string{"build": "git update-ref refs/heads/main HEAD && exit 0"}, nil)
		wt := ldFinished(t, main)
		ldBookkeeping(t, wt)
		lqForgetChange(t, main)
		ldReviewAgain(t, wt)

		out := swRefused(t, ldBehindTrunk, wt, ldSession, "stop")
		said, fix := ldRefusal(t, out, ldBehindTrunk)
		if !strings.Contains(said, "moved while") {
			t.Fatalf("precondition: the refusal is not the one for a trunk that moved under the landing:\n%s", out)
		}
		if !ltSaysThree(fix) {
			t.Errorf("the fix does not say to stop again up to three times before the story goes to a person:\n%s", fix)
		}
		if ltOnce.MatchString(ltFlat(fix)) || strings.Contains(ltFlat(fix), "the same way again, hand") {
			t.Errorf("the fix still hands the story over on the second refusal:\n%s", fix)
		}
	})
	t.Run("AC-9 the catalogue's fix", func(t *testing.T) {
		t.Parallel()
		fix := ltFlat(sdlcerr.New(sdlcerr.BranchBehindTrunk, "", "").Fix)
		at := strings.LastIndex(fix, "trunk moved")
		if at < 0 {
			t.Fatalf("the catalogue's fix no longer says what to do where trunk moved while the landing ran:\n%s", fix)
		}
		if !ltSaysThree(fix[at:]) {
			t.Errorf("where trunk moved while the landing ran, the catalogue's fix does not say to stop again up to "+
				"three times before the story goes to a person:\n%s", fix[at:])
		}
		if ltThree.MatchString(fix[:at]) {
			t.Errorf("the catalogue's fix gives three for a state that is a person's at once:\n%s", fix[:at])
		}
	})
	t.Run("AC-9 docs/troubleshooting.md", func(t *testing.T) {
		t.Parallel()
		items := ltItems(ltSection(t, page(t, "troubleshooting.md"), "### SDLC-E0054"))
		moved := ltItem(t, "docs/troubleshooting.md, SDLC-E0054", items, "trunk moved while the landing ran")
		if !ltSaysThree(moved) {
			t.Errorf("the entry does not say to stop again up to three times before the story goes to a person:\n%s", moved)
		}
		if ltOnce.MatchString(moved) {
			t.Errorf("the entry still hands the story over on the second refusal:\n%s", moved)
		}
		// Within SDLC-E0054's own entry only: SDLC-E0006's says three too,
		// for another story landing, and is held to it above.
		for _, item := range items {
			if item != moved && ltThree.MatchString(item) {
				t.Errorf("SDLC-E0054's entry gives three for a state that is a person's at once:\n%s", item)
			}
		}
	})
}
