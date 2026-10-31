package cli

// US-112.1 AC-5: what a person reads says what a bare `sdlc start` takes. The
// three places the criterion names have to say it; the others that said a todo
// story is picked have to stop. Neither is held to a sentence: a page may be
// reworded as it likes, as long as it says it, and does not say the opposite.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// us1121Source is a file a reader sees, by its path from the repository root.
func us1121Source(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// us1121Section is the part of doc under the heading that starts with
// heading, down to the next heading of the same level or a higher one.
func us1121Section(t *testing.T, doc, heading string) string {
	t.Helper()
	level := len(heading) - len(strings.TrimLeft(heading, "#"))
	at := -1
	if strings.HasPrefix(doc, heading) {
		at = 0
	} else if i := strings.Index(doc, "\n"+heading); i >= 0 {
		at = i + 1
	}
	if at < 0 {
		t.Fatalf("no heading %q", heading)
	}
	rest := doc[at+len(heading):]
	for _, line := range strings.SplitAfter(rest, "\n") {
		if n := len(line) - len(strings.TrimLeft(line, "#")); n >= 2 && n <= level && strings.HasPrefix(line[n:], " ") {
			return doc[at : at+len(heading)+strings.Index(rest, line)]
		}
	}
	return doc[at:]
}

// us1121Paragraphs is text a paragraph at a time, each on one line.
func us1121Paragraphs(text string) []string {
	var out []string
	for p := range strings.SplitSeq(text, "\n\n") {
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// us1121Units is what a statement is checked against: a table row alone, and
// otherwise a sentence, or the clause before a semicolon.
func us1121Units(text string) []string {
	var out []string
	var prose []string
	for line := range strings.Lines(text) {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			out = append(out, strings.TrimSpace(line))
			continue
		}
		prose = append(prose, line)
	}
	for _, p := range us1121Paragraphs(strings.Join(prose, "")) {
		out = append(out, regexp.MustCompile(`[.;!?] `).Split(p, -1)...)
	}
	return out
}

var (
	us1121Excludes = regexp.MustCompile("(?i)\\b(only|never)\\b|passed over|passes over|pass over|skips|skipped|ignores|" +
		"\\bnot\\b[^.;]{0,25}\\b(picked|pick|picks|taken|take|takes|started|starts)\\b|no longer|" +
		"`todo`[^.;,]{0,25}\\b(is|are) not\\b")
	us1121Picked = regexp.MustCompile("(?i)\\b(picked|picks|taken|takes)\\b|like `ready`|" +
		"`(ready|todo)` (or|and) `(ready|todo)`")
)

// us1121ClaimsTodoIsPicked is whether the unit says a todo story is taken by a
// bare start: it names the status, says something is picked or lists it beside
// ready, and does not say it is passed over.
func us1121ClaimsTodoIsPicked(unit string) bool {
	if !strings.Contains(unit, "`todo`") {
		return false
	}
	if strings.HasPrefix(unit, "|") {
		cells := strings.Split(strings.Trim(unit, "| "), "|")
		return len(cells) >= 2 && strings.TrimSpace(cells[0]) == "`todo`" &&
			strings.HasPrefix(strings.ToLower(strings.TrimSpace(cells[1])), "yes")
	}
	return us1121Picked.MatchString(unit) && !us1121Excludes.MatchString(unit)
}

// ----------------------------------------------------------------- AC-5

func TestAC5TheCommandPageTheLoopPageAndTheTroubleshootingPageSayABareStartTakesOnlyReadyStories(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ file, heading string }{
		{"docs/commands.md", "## `sdlc start`"},
		{"docs/the-loop.md", "## Gate 1 — `dor`, definition of ready"},
		{"docs/troubleshooting.md", "### SDLC-E0010"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			section := us1121Section(t, us1121Source(t, tc.file), tc.heading)
			for _, p := range us1121Paragraphs(section) {
				if strings.Contains(p, "`ready`") && strings.Contains(p, "`todo`") && us1121Excludes.MatchString(p) {
					return
				}
			}
			t.Errorf("%s, under %q, has no paragraph that names `ready` and `todo` and says a bare start takes only the first", tc.file, tc.heading)
		})
	}
}

// The E0010 page tells the reader what to do about the new case, as the
// refusal does.
func TestAC5TheTroubleshootingPageSaysWhatToDoWhenOnlyTodoStoriesAreLeft(t *testing.T) {
	t.Parallel()
	section := strings.ToLower(us1121Source(t, "docs/troubleshooting.md"))
	section = us1121Section(t, section, "### sdlc-e0010")
	flat := strings.Join(strings.Fields(section), " ")
	for _, want := range []string{"mark", "`ready`", "sdlc start <id>"} {
		if !strings.Contains(flat, want) {
			t.Errorf("the SDLC-E0010 section does not say %q: marking a story ready, or naming it with `sdlc start <ID>`", want)
		}
	}
}

// The pages and skills that said a todo story is picked no longer do. The
// detector is held to the sentences that were false, so a rewrite that says
// the opposite, or says nothing, passes.
func TestAC5NoPageOrSkillSaysABareStartPicksATodoStory(t *testing.T) {
	t.Parallel()
	for _, file := range []string{
		"docs/guides/writing-stories.md",
		"docs/getting-started.md",
		"docs/guides/reviewing-what-landed-on-trunk.md",
		"plugin/skills/trunk-review/SKILL.md",
		"plugin/skills/next/SKILL.md",
		"docs/commands.md",
		"docs/the-loop.md",
		"docs/troubleshooting.md",
	} {
		t.Run(file, func(t *testing.T) {
			t.Parallel()
			for _, unit := range us1121Units(us1121Source(t, file)) {
				if us1121ClaimsTodoIsPicked(unit) {
					t.Errorf("%s says a todo story is picked: %q", file, unit)
				}
			}
		})
	}
}

// The detector itself: the sentences that were false are found, and the ways
// of putting it right are not.
func TestAC5TheDetectorOfAFalseClaimFindsTheOldSentencesAndNotTheNewOnes(t *testing.T) {
	t.Parallel()
	for _, wrong := range []string{
		"| `todo` | yes, exactly like `ready` | you |",
		"Because `todo` is picked like `ready`, mark a story you are not ready to run as `blocked` until you are.",
		"Otherwise, it considers stories that are `ready` or `todo` and whose every `depends_on` entry is `done`.",
		"A story is picked up when its `status` is `todo` or `ready` and the stories it depends on are done.",
		"A `todo` or `ready` story is picked next",
		"`todo` and `ready` are picked next",
	} {
		if !us1121ClaimsTodoIsPicked(wrong) {
			t.Errorf("the detector missed %q", wrong)
		}
	}
	for _, fine := range []string{
		"| `todo` | no, only by name | you |",
		"Because `todo` is not picked, a story you are not ready to run can stay `todo`.",
		"A `ready` story is picked next",
		"A `ready` story is picked next, and a `todo` one is not",
		"A bare start takes only `ready` stories and passes over a `todo` one",
		"`blocked` keeps `sdlc start` from picking the story up",
	} {
		if us1121ClaimsTodoIsPicked(fine) {
			t.Errorf("the detector flagged %q", fine)
		}
	}
}

// The runbook stops calling SDLC-E0010 plain nothing to work on: with todo
// stories left there is something, and a person has to say it is ready or name
// it. The skill shows the why and stops, and picks no todo story for them.
func TestAC5TheNextSkillTellsTheSessionWhatToDoWhenOnlyTodoStoriesAreLeft(t *testing.T) {
	t.Parallel()
	var para string
	for _, p := range us1121Paragraphs(us1121Source(t, "plugin/skills/next/SKILL.md")) {
		if strings.Contains(p, "`SDLC-E0010`") {
			para = p
		}
	}
	if para == "" {
		t.Fatal("plugin/skills/next/SKILL.md has no paragraph on SDLC-E0010")
	}
	lower := strings.ToLower(para)
	for _, want := range []string{"`todo`", "why", "stop"} {
		if !strings.Contains(lower, want) {
			t.Errorf("the SDLC-E0010 paragraph of the next skill never says %q: %s", want, para)
		}
	}
	// It says the session does not pick one: not, never or do not, before pick,
	// start, name or choose.
	if !regexp.MustCompile(`(?i)\b(never|not|don't)\b[^.]{0,40}\b(pick|picks|start|starts|name|names|choose|chooses|take|takes)\b`).MatchString(para) {
		t.Errorf("the SDLC-E0010 paragraph of the next skill does not forbid picking a todo story: %s", para)
	}
}
