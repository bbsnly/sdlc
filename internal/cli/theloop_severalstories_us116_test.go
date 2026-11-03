package cli

// US-116 AC-8: docs/the-loop.md says how a person works several stories at
// once, by hand: open a session in each story's worktree, and run
// `/sdlc:next <ID>` or `sdlc start <ID>` there. The page has said it of a
// command that did it for them; now the person does it, so the page has to say
// it in so many words, and in one place, where a reader looking for "several
// stories" finds all of it.
//
// A docs test can only read words. These are held to one paragraph of the
// section on more than one checkout, and the first half to one sentence, so
// that saying the parts in different places on the page does not pass.

import (
	"regexp"
	"strings"
	"testing"
)

// us116Section is the text under the "## " heading that starts with name, up to
// the next "## " heading.
func us116Section(t *testing.T, doc, name string) string {
	t.Helper()
	_, rest, found := strings.Cut(doc, "\n## "+name)
	if !found {
		t.Fatalf("docs/the-loop.md has no section %q", name)
	}
	if before, _, more := strings.Cut(rest, "\n## "); more {
		return before
	}
	return rest
}

func TestAC8TheLoopPageSaysToOpenASessionInEachStorysWorktreeAndRunNextOrStartThere(t *testing.T) {
	t.Parallel()
	section := us116Section(t, page(t, "the-loop.md"), "More than one checkout")
	sentenceEnd := regexp.MustCompile(`[.!?]\s+`)
	var seen []string
	for paragraph := range strings.SplitSeq(section, "\n\n") {
		flat := strings.Join(strings.Fields(paragraph), " ")
		if !strings.Contains(flat, "`/sdlc:next <ID>`") || !strings.Contains(flat, "`sdlc start <ID>`") {
			continue
		}
		seen = append(seen, flat)
		for _, sentence := range sentenceEnd.Split(flat, -1) {
			lower := strings.ToLower(sentence)
			if strings.Contains(lower, "session") && strings.Contains(lower, "each") &&
				strings.Contains(lower, "story") && strings.Contains(lower, "worktree") {
				return
			}
		}
	}
	if len(seen) == 0 {
		t.Fatal("no paragraph in the-loop.md's section on more than one checkout names both `/sdlc:next <ID>` and `sdlc start <ID>`")
	}
	t.Errorf("a paragraph names `/sdlc:next <ID>` and `sdlc start <ID>`, but none has a sentence that says to open a session in each story's worktree:\n%s",
		strings.Join(seen, "\n"))
}
