package plugin

// US-114: what the /sdlc:consolidate skill promises beyond what it did. The
// skill is read by a model, and no code checks that it does what it says, so
// the rules themselves are pinned here as the sentences that state them: each
// rule is a set of words that have to be in one paragraph, or one list item, of
// the skill, and not a single word to be found anywhere, which a skill that
// said the opposite would also pass.
//
// A needle is one thing the paragraph has to say. Where a thing can be said in
// more than one way the needle lists them, separated by "|", and one of them
// is enough. The match ignores case and folds white space, so a line break
// does not matter. A failing rule prints what no paragraph held.
//
// What the pinned wording of TestAHumanOnlySkillKeepsWhatItPromises says is
// kept as it is; nothing here loosens it.

import (
	"strings"
	"testing"
)

// us114Blocks is a body cut into the units a sentence lives in: paragraphs,
// list items, headings and table rows, each with its white space folded and its
// case lowered.
func us114Blocks(body string) []string {
	var blocks, cur []string
	flush := func() {
		if len(cur) > 0 {
			blocks = append(blocks, strings.ToLower(strings.Join(strings.Fields(strings.Join(cur, " ")), " ")))
			cur = nil
		}
	}
	for line := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			flush()
		case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, "|"):
			flush()
			cur = append(cur, trimmed)
		default:
			cur = append(cur, trimmed)
		}
	}
	flush()
	return blocks
}

// us114Says is the needles one block does not hold, or nil where it holds them
// all.
func us114Says(block string, needles []string) []string {
	var missing []string
	for _, n := range needles {
		found := false
		for alt := range strings.SplitSeq(n, "|") {
			if strings.Contains(block, strings.ToLower(alt)) {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, n)
		}
	}
	return missing
}

// us114Rule is one thing a text has to say, in one block.
type us114Rule struct {
	name    string
	needles []string
}

// us114Hold reports every rule that no block of body holds, naming the needles
// of the block that came closest.
func us114Hold(t *testing.T, what, body string, rules []us114Rule) {
	t.Helper()
	blocks := us114Blocks(body)
	for _, r := range rules {
		var closest []string
		held := false
		for _, b := range blocks {
			missing := us114Says(b, r.needles)
			if len(missing) == 0 {
				held = true
				break
			}
			if closest == nil || len(missing) < len(closest) {
				closest = missing
			}
		}
		if !held {
			t.Errorf("%s: %s\n  no one paragraph or list item of %s says all of: %q\n  the nearest lacks: %q",
				r.name, "the rule is not stated", what, r.needles, closest)
		}
	}
}

func us114Skill(t *testing.T) Component {
	t.Helper()
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if s.Name == "consolidate" {
			return s
		}
	}
	t.Fatal("the plugin ships no consolidate skill")
	return Component{}
}

// AC-4: a run with no story under way that finds nothing still writes the
// dated file, saying how many stories and retros it read and that it found
// nothing, so that the nudge ends; with a story under way it writes nothing and
// the nudge stays.
func TestAC4TheSkillStillWritesTheDatedFileWhenItFindsNothing(t *testing.T) {
	skill := us114Skill(t)
	us114Hold(t, "the consolidate skill", skill.Body, []us114Rule{
		{"AC-4 an empty run still writes the dated file, with the counts and that it found nothing", []string{
			"still write|still writes|write it anyway|writes it anyway|always write|always writes",
			".sdlc/consolidation-",
			"found nothing|nothing found|finds nothing",
			"stories",
			"retros",
			"nudge|sdlc status|reminder",
		}},
		{"AC-4 a run while a story is under way writes nothing, and the nudge stays", []string{
			"story is active|active story|story active|under way|underway|names a story",
			"write nothing",
			"nudge|reminder",
			"stay|stays|remain|remains|still counts|is still",
		}},
	})
}

// AC-6: review files are read as evidence, by what two or more stories show.
func TestAC6TheSkillReadsEachStorysReviewFilesAsEvidence(t *testing.T) {
	skill := us114Skill(t)
	us114Hold(t, "the consolidate skill", skill.Body, []us114Rule{
		{"AC-6 it reads the review files of every finished story, besides the retro and the log", []string{
			"/reviews",
			"retro",
			"every entry|each entry|every story|each story|every finished story|each finished story",
		}},
		{"AC-6 a finding two stories' reviews raise on the same kind of matter is evidence, naming the stories and the review files", []string{
			"review",
			"two or more stories|two stories|more than one story",
			"same kind",
			"evidence",
			"review files|review file",
			"nam",
		}},
		{"AC-6 a finding counts whether or not it blocked", []string{
			"review",
			"blocking or not|blocking or non-blocking|non-blocking|not blocking|whether it blocked|whether or not it blocked|advisory|did not block",
		}},
		{"AC-6 a finding one story's reviews show goes under Seen once", []string{
			"seen once",
			"review",
			"one story|single story|only one story|a single story|only one",
		}},
	})
}

// AC-7: a contract proposal states the section's length before and after, and
// does not take it past 150 lines without the person's say.
func TestAC7TheSkillCountsTheContractSectionAndHoldsItTo150Lines(t *testing.T) {
	skill := us114Skill(t)
	us114Hold(t, "the consolidate skill", skill.Body, []us114Rule{
		{"AC-7 a contract proposal states the section's line count now and after", []string{
			"sdlc contract",
			"line count|count of lines|number of lines|count the lines|counts the lines|how many lines",
			"current|now|before",
			"after",
		}},
		{"AC-7 above 150 it says the section would be too long to read in full each session, and proposes what to cut with the reason", []string{
			"150",
			"too long",
			"session",
			"cut",
			"reason",
			"least",
			"same proposal|in the proposal|with the proposal|alongside|together",
		}},
		{"AC-7 it does not apply an addition past 150 lines unless the person confirms the cut or says to go over", []string{
			"150",
			"go over",
			"confirm",
			"apply",
			"not apply|never apply|do not apply|don't apply|only if|only after|unless|without",
		}},
	})
}

// AC-8: earlier consolidation files are read at the start, their declined
// proposals listed, and one is not proposed again without a story that finished
// after the file's date; a decline's reason is carried into the new file.
func TestAC8TheSkillReadsEarlierDeclinesAndDoesNotProposeThemAgain(t *testing.T) {
	skill := us114Skill(t)
	us114Hold(t, "the consolidate skill", skill.Body, []us114Rule{
		{"AC-8 it reads the earlier consolidation files and lists the declined proposals", []string{
			"earlier|previous|prior",
			"consolidation-",
			"declined",
			"read",
			"list",
		}},
		{"AC-8 it does not propose the same target and change again unless a story finished after the file's date, and then names the earlier decline and its date", []string{
			"same target",
			"same change",
			"finished after",
			"date",
			"declin",
			"nam",
		}},
		{"AC-8 a decline's reason is carried into the new file", []string{
			"reason",
			"declined",
			"carry|carried|copy|copied|kept|keep|bring forward|brought forward|repeat",
			"file",
		}},
	})
}

// AC-8: the template has a place for the reason, or a decline has nowhere to
// keep it that the next run can read.
func TestAC8TheProposalsTemplateHasAPlaceForAReasonForADecline(t *testing.T) {
	skill := us114Skill(t)
	_, after, ok := strings.Cut(skill.Body, "# Consolidation")
	if !ok {
		t.Fatal("the skill has no template for the proposals file")
	}
	template, _, _ := strings.Cut(after, "```")
	lower := strings.ToLower(template)
	if !strings.Contains(lower, "decision") || !strings.Contains(lower, "reason") {
		t.Errorf("AC-8 the template has a Decision and no place for the reason a proposal was declined:\n%s", template)
	}
}

// THREATS 5: what the skill reads now includes review files and earlier
// consolidation files, text written by models and sessions, and a decline in
// one is authority to stay silent. The skill says that what it reads is data.
func TestAC8TheSkillTreatsWhatItReadsAsDataNotInstructions(t *testing.T) {
	skill := us114Skill(t)
	us114Hold(t, "the consolidate skill", skill.Body, []us114Rule{
		{"THREATS 5 retros, reviews and earlier consolidation files are data, not instructions", []string{
			"data",
			"instruction|directive|addresses you|tells you|commands",
			"review",
			"consolidation",
		}},
	})
}
