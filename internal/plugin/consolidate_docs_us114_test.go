package plugin

// US-114 AC-9: the pages that describe the nudge, the setting, the review
// evidence, the length check and the memory of declines say so. They are held
// the way the skill is (see consolidate_us114_test.go): each rule is a set of
// words that one paragraph or table row has to carry, and not a word found
// anywhere on the page.
//
// What reproduces command output, and what names every setting, is held by
// the tests in internal/cli (TestEverySettingIsDocumented and the pages that
// quote a command's text); these hold that the new behaviour is described.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func us114Page(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// us114Section is the part of a page from the heading that starts with
// heading to the next heading of the same level, or the end of the page.
func us114Section(t *testing.T, page, heading string) string {
	t.Helper()
	level := heading[:strings.IndexByte(heading, ' ')]
	var out []string
	in := false
	for line := range strings.SplitSeq(strings.ReplaceAll(page, "\r\n", "\n"), "\n") {
		switch {
		case !in && strings.HasPrefix(line, heading):
			in = true
		case in && strings.HasPrefix(line, level+" "):
			return strings.Join(out, "\n")
		}
		if in {
			out = append(out, line)
		}
	}
	if !in {
		t.Fatalf("there is no %q heading", heading)
	}
	return strings.Join(out, "\n")
}

// AC-9, AC-5: the configuration page says what consolidate.nudge_after is, that
// it is 3 when absent, and that 0 turns the nudge off.
func TestAC9TheConfigurationPageDescribesNudgeAfter(t *testing.T) {
	page := us114Page(t, "docs/configuration.md")
	us114Hold(t, "docs/configuration.md", page, []us114Rule{
		{"AC-9 what the setting counts, and what it sets off", []string{
			"nudge_after",
			"consolidate",
			"stories|story",
			"status",
			"stop",
		}},
		{"AC-9 it is 3 when the file says nothing", []string{
			"nudge_after",
			"3",
			"default|absent|unset|left out|not set|nothing",
		}},
		{"AC-9 0 turns the line and the field off", []string{
			"nudge_after",
			"0",
			"off|turns|disables|never",
		}},
	})
}

// AC-9: docs/commands.md says what status prints at the threshold and what its
// --json carries, and that stop ends with the same line.
func TestAC9TheCommandsPageDescribesTheNudgeInStatusAndStop(t *testing.T) {
	page := us114Page(t, "docs/commands.md")
	status := us114Section(t, page, "## `sdlc status`")
	us114Hold(t, "the status section of docs/commands.md", status, []us114Rule{
		{"AC-9 status ends with a line when stories have finished since the last consolidation", []string{
			"/sdlc:consolidate",
			"nudge_after",
			"finished",
			"line",
		}},
		{"AC-9 status --json carries the count and the last consolidation's date, null when there has been none", []string{
			"`consolidate`",
			"finished",
			"last",
			"null",
		}},
		{"AC-9 below the threshold, and with the setting 0, status is as it was and the field is absent", []string{
			"nudge_after",
			"absent|left out|not there|no field|without",
			"0",
		}},
	})
	stop := us114Section(t, page, "## `sdlc stop`")
	us114Hold(t, "the stop section of docs/commands.md", stop, []us114Rule{
		{"AC-9 a stop that ends an iteration ends with the same line at the threshold", []string{
			"/sdlc:consolidate",
			"line",
			"nudge_after|threshold|status",
		}},
		{"AC-9 the line is text only, and stop --json is unchanged", []string{
			"--json",
			"consolidate|line",
			"not|no |nothing|only|never|without|unchanged",
		}},
	})
}

// AC-9: the guide describes the nudge, the review evidence, the length check,
// the memory of declines and the empty run, and says where the file belongs.
func TestAC9TheGuideDescribesWhatConsolidateNowDoes(t *testing.T) {
	page := us114Page(t, "docs/guides/consolidating-what-the-retros-say.md")
	us114Hold(t, "the consolidating guide", page, []us114Rule{
		{"AC-9 the nudge: status and stop say when stories are waiting, and nudge_after sets when", []string{
			"nudge_after",
			"status",
			"stop",
			"/sdlc:consolidate",
		}},
		{"AC-9 it reads each story's review files, and what two stories' reviews raise on the same matter is evidence", []string{
			"review files|/reviews",
			"two",
			"stories",
			"same",
		}},
		{"AC-9 a proposal states the contract's length, and over 150 lines it proposes what to cut", []string{
			"150",
			"too long",
			"cut",
		}},
		{"AC-9 a proposal that was declined is not made again until a story has finished since", []string{
			"declined",
			"again|same|repeat",
			"finished after|finished since|since|new evidence",
		}},
		{"AC-9 a run that finds nothing still writes the dated file, which ends the nudge", []string{
			"nothing",
			"still writes|still write|writes the file|writes a file|leaves a file|leaves the file",
			"consolidation-",
			"nudge|status",
		}},
		{"AC-9 the file is left in .sdlc, whose date the count reads", []string{
			"leave|keep|stay",
			".sdlc",
			"consolidation-",
			"date|nudge|count",
		}},
		{"AC-9 if the file is committed it is committed on trunk and not in a story", []string{
			"commit",
			"trunk",
			"story",
			"consolidation-|proposals file",
		}},
	})
}
