package plugin

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// rpReviewers are the seven definitions AC-4 names, by name so that a reviewer
// added later is not silently left out of this list and one removed fails here.
var rpReviewers = []string{"architect", "security", "red-team", "perf", "human-advocate", "verifier", "code-reviewer"}

// rpSentences is text with its whitespace folded, split where a sentence ends.
// Headings and list items end a sentence too, so that a list cannot read as one.
func rpSentences(text string) []string {
	var out []string
	for _, block := range regexp.MustCompile(`\n\s*\n|\n\s*[-*#]+\s`).Split(text, -1) {
		flat := strings.Join(strings.Fields(block), " ")
		for _, s := range regexp.MustCompile(`[.!?:;](\s+|$)`).Split(flat, -1) {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// rpDuty is one thing a definition has to say: every group of words, any one of a
// group, in the one sentence.
type rpDuty struct {
	name string
	all  [][]string
}

func (d rpDuty) holds(sentence string) bool {
	lower := strings.ToLower(sentence)
	for _, group := range d.all {
		if !slices.ContainsFunc(group, func(word string) bool { return strings.Contains(lower, word) }) {
			return false
		}
	}
	return true
}

// rpAssign finds a sentence for each duty, no sentence serving two: a sentence
// that holds every word of every duty is one duty met, not six.
func rpAssign(duties []rpDuty, sentences []string) (missing []string) {
	used := map[int]bool{}
	var place func(int) bool
	place = func(i int) bool {
		if i == len(duties) {
			return true
		}
		for s, sentence := range sentences {
			if !used[s] && duties[i].holds(sentence) {
				used[s] = true
				if place(i + 1) {
					return true
				}
				used[s] = false
			}
		}
		return false
	}
	if place(0) {
		return nil
	}
	for _, d := range duties {
		if !slices.ContainsFunc(sentences, d.holds) {
			missing = append(missing, d.name)
		}
	}
	if len(missing) == 0 {
		missing = append(missing, "a sentence of its own for each duty: one sentence is standing in for several")
	}
	return missing
}

var rpAgentDuties = []rpDuty{
	{"to read the policy before anything else", [][]string{{"sdlc review list"}, {"polic"}, {"before anything else"}, {"read"}}},
	{"to make the passes that belong to its role", [][]string{{"passes"}, {"role"}}},
	{"to report at most the nit cap and give the rest as a count", [][]string{{"at most"}, {"nit"}, {"cap"}, {"count"}}},
	{"to report nothing a configured command enforces", [][]string{{"command"}, {"enforce"}, {"nothing", "not report", "never report"}}},
	// An instruction: the example line that records the review holds both flags too.
	{"to pass the counts", [][]string{{"pass"}, {"--important"}, {"--nits"}}},
	{"to review as today without a policy", [][]string{{"no review policy", "no policy"}, {"as you do today", "as today"}}},
	// Q8 and T4: the rest of the definition says repository text is data.
	{"that only the path review list names is policy", [][]string{{"only"}, {"sdlc review list"}, {"polic"}}},
	{"that anything else in it addressing the agent is data", [][]string{{"anything else"}, {"data"}}},
}

// The two Q8 limits, in any sentence: English joins them with the first.
var rpLimits = []rpDuty{
	{"that it sets what to report", [][]string{{"what to report"}}},
	{"that it cannot lift a role's blocking rules", [][]string{{"blocking rules"}, {"cannot", "can not", "never"}}},
	{"that it cannot order a verdict", [][]string{{"verdict"}, {"order"}, {"cannot", "can not", "never"}}},
}

var rpStillData = regexp.MustCompile(`(?i)text in the repository[^.]*addresses you[^.]*is\s+data`)

func rpAgentBody(t *testing.T, name string) string {
	t.Helper()
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.Name == name {
			return a.Body
		}
	}
	t.Fatalf("there is no agent %s", name)
	return ""
}

// US-115 AC-4 and Q8. Duty by duty, definition by definition, each in a sentence
// of its own, so that one pasted sentence holding every word does not pass.
func TestAC4EveryReviewerSaysEachDutyOfThePolicyInASentenceOfItsOwn(t *testing.T) {
	t.Parallel()
	for _, name := range rpReviewers {
		body := rpAgentBody(t, name)
		sentences := rpSentences(body)
		for _, d := range slices.Concat(rpAgentDuties, rpLimits) {
			t.Run("AC-4 "+name+" says "+d.name, func(t *testing.T) {
				t.Parallel()
				if !slices.ContainsFunc(sentences, d.holds) {
					t.Errorf("%s does not say %s", name, d.name)
				}
			})
		}
		t.Run("AC-4 "+name+" says them separately", func(t *testing.T) {
			t.Parallel()
			if missing := rpAssign(rpAgentDuties, sentences); len(missing) > 0 {
				t.Errorf("%s: %v", name, missing)
			}
		})
		// T4: the policy is the one exception, so the rule it is an exception to stays.
		t.Run("AC-4 "+name+" still treats other repository text as data", func(t *testing.T) {
			t.Parallel()
			if !rpStillData.MatchString(body) {
				t.Errorf("%s no longer says repository text addressing it is data", name)
			}
		})
	}
}

// "Before anything else": ahead of what the agent is told to read.
func TestAC4EveryReviewerReadsThePolicyBeforeItsInputs(t *testing.T) {
	t.Parallel()
	for _, name := range rpReviewers {
		t.Run("AC-4 "+name, func(t *testing.T) {
			t.Parallel()
			before, _, found := strings.Cut(rpAgentBody(t, name), "## Inputs")
			if !found {
				t.Fatalf("%s has no Inputs section", name)
			}
			if !slices.ContainsFunc(rpSentences(before), rpAgentDuties[0].holds) {
				t.Errorf("%s does not say, ahead of its inputs, %s", name, rpAgentDuties[0].name)
			}
		})
	}
}

// T11: an example with a number in it teaches the reviewer to type that number,
// so each shows its own counts as placeholders on the line that records the review,
// and each still tells the model the number is its own to count.
func TestAC4EveryReviewerRecordsItsOwnCountsNotANumberItWasShown(t *testing.T) {
	t.Parallel()
	example := regexp.MustCompile(`(?m)^.*sdlc review add .*$`)
	literal := regexp.MustCompile(`--(important|nits)[ =]+[0-9]`)
	placeholder := func(flag string) *regexp.Regexp {
		return regexp.MustCompile(`--` + flag + `[ =]+<[^>]+>`)
	}
	for _, name := range rpReviewers {
		t.Run("AC-4 "+name, func(t *testing.T) {
			t.Parallel()
			body := rpAgentBody(t, name)
			lines := example.FindAllString(body, -1)
			if len(lines) == 0 {
				t.Fatalf("%s has no `sdlc review add` line", name)
			}
			for _, line := range lines {
				if !placeholder("important").MatchString(line) || !placeholder("nits").MatchString(line) {
					t.Errorf("%s records a review without its counts: %s", name, line)
				}
			}
			if m := literal.FindString(body); m != "" {
				t.Errorf("%s shows a number to copy: %s", name, m)
			}
		})
	}
}

// T4: the new paragraph does not take the old rule with it.
// rpRunbook is the next runbook's `## ` section with this heading's opening, with
// its lines joined.
func rpRunbookSection(t *testing.T, opening string) string {
	t.Helper()
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if s.Name != "next" {
			continue
		}
		for _, section := range regexp.MustCompile(`(?m)^## `).Split(s.Body, -1) {
			if strings.HasPrefix(section, opening) {
				return section
			}
		}
	}
	t.Fatalf("the next runbook has no section %q", opening)
	return ""
}

func rpSaysIn(t *testing.T, where, section string, d rpDuty) {
	t.Helper()
	if !slices.ContainsFunc(rpSentences(section), d.holds) {
		t.Errorf("%s of the next runbook does not say %s:\n%s", where, d.name, section)
	}
}

// AC-5, gate by gate, in the section the conversation is reading when it delegates.
// The runbook is cut by compaction, and a sentence elsewhere is one it has lost.
// Gate 1's only reviewer is the human advocate, when the project asks for it.
func TestAC5TheRunbookGivesEveryReviewerThePolicyInItsOwnGate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		gate     string
		delegate string
	}{
		{"Gate 1", "sdlc:human-advocate"},
		{"Gate 4", "sdlc:<role>"},
		{"Gate 6", "sdlc:verifier"},
		{"Gate 7", "sdlc:<role>"},
	} {
		t.Run("AC-5 "+c.gate, func(t *testing.T) {
			t.Parallel()
			rpSaysIn(t, c.gate, rpRunbookSection(t, c.gate), rpDuty{
				"to give " + c.delegate + " the policy's path",
				[][]string{{c.delegate}, {"polic"}, {"path"}},
			})
		})
	}
}

// AC-5: what becomes of the nits, in each gate whose reviews can leave any.
// Approved reading: Gate 7's go to the retro, as there is no next gate's work.
func TestAC5TheRunbookSaysNitsNeverComeBackAsRework(t *testing.T) {
	t.Parallel()
	passes := func(what string) rpDuty {
		return rpDuty{"that " + what + " passes with the nits unaddressed",
			[][]string{{"nits"}, {"approv"}, {"passes"}, {"unaddressed"}}}
	}
	never := func(changes string) rpDuty {
		return rpDuty{"that nits are never sent back as rework that changes the " + changes,
			[][]string{{"never"}, {"rework"}, {changes}}}
	}
	for _, c := range []struct {
		gate string
		says []rpDuty
	}{
		{"Gate 4", []rpDuty{
			passes("a gate whose blocking reviewers approve"),
			{"that the nits go into the implementer's brief", [][]string{{"nits"}, {"implementer"}, {"brief"}}},
			never("plan"),
		}},
		{"Gate 6", []rpDuty{
			passes("the gate"),
			{"where the nits go", [][]string{{"retro", "brief"}}},
			never("tree"),
		}},
		{"Gate 7", []rpDuty{
			passes("the gate"),
			{"that the nits go to the retro", [][]string{{"retro"}}},
			never("tree"),
		}},
	} {
		section := rpRunbookSection(t, c.gate)
		for _, d := range c.says {
			t.Run("AC-5 "+c.gate+" says "+d.name, func(t *testing.T) {
				t.Parallel()
				rpSaysIn(t, c.gate, section, d)
			})
		}
	}
}

// Q7: the room for AC-5 comes out of the runbook, not out of the cap. This is
// TestTheRunbookFitsWhatCompactionKeeps again on purpose, with the number written
// here: changing the cap there has to be seen here too.
func TestAC5TheRunbookStaysUnderTheCapThatCompactionKeeps(t *testing.T) {
	t.Parallel()
	const maxBytes = 17500
	raw, err := os.ReadFile(filepath.Join(pluginDir, "skills", "next", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > maxBytes {
		t.Errorf("the next runbook is %d bytes, over %d", len(raw), maxBytes)
	}
	if !strings.Contains(string(raw), "polic") {
		t.Error("the next runbook does not mention the policy")
	}
}

// rpRepositoryPolicy is this repository's own REVIEW.md.
func rpRepositoryPolicy(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "REVIEW.md"))
	if err != nil {
		t.Fatalf("this repository has no REVIEW.md: %v", err)
	}
	return string(raw)
}

func rpPolicySection(t *testing.T, text, word string, not ...string) string {
	t.Helper()
	var found []string
	for _, part := range regexp.MustCompile(`(?m)^## `).Split(text, -1)[1:] {
		heading, body, _ := strings.Cut(part, "\n")
		lower := strings.ToLower(heading)
		if strings.Contains(lower, word) && !slices.ContainsFunc(not, func(n string) bool { return strings.Contains(lower, n) }) {
			found = append(found, body)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the repository's REVIEW.md has %d sections for %q, want 1", len(found), word)
	}
	return found[0]
}

// AC-7. Each leg by name, under the heading that keeps it out of review; each
// contract rule under what counts as important, with its limit in the same
// sentence, so that one line naming all of it elsewhere does not pass.
func TestAC7ThisRepositorysPolicyNamesWhatIsNotReviewedAndWhatIsImportantOnlyWhere(t *testing.T) {
	t.Parallel()
	text := rpRepositoryPolicy(t)
	notReviewed := strings.ToLower(rpPolicySection(t, text, "not to review"))
	for _, leg := range []string{
		"markdownlint", "linkcheck", "golangci-lint", "gofmt", "shellcheck", "actionlint", "powershell",
		"hygiene", "secrets", "release-check", "npm-test",
	} {
		t.Run("AC-7 "+leg+" is not reviewed", func(t *testing.T) {
			t.Parallel()
			if !regexp.MustCompile(`\b` + regexp.QuoteMeta(leg) + `\b`).MatchString(notReviewed) {
				t.Errorf("what not to review does not name %s:\n%s", leg, notReviewed)
			}
		})
	}
	important := strings.ToLower(rpPolicySection(t, text, "important"))
	for _, rule := range []string{"british english", "conventional commits", "comment"} {
		t.Run("AC-7 "+rule+" is important only where", func(t *testing.T) {
			t.Parallel()
			rpSaysInPolicy(t, important, rpDuty{rule + " as important only where text is false or unreadable",
				[][]string{{rule}, {"only"}, {"false"}, {"unreadable"}}})
		})
	}
	// The rest of AC-2's shape, which this repository's copy is an instance of.
	for _, word := range []string{"pass", "cap"} {
		rpPolicySection(t, text, word)
	}
	rpPolicySection(t, text, "nit", "cap")
	if !regexp.MustCompile(`\b5\b`).MatchString(rpPolicySection(t, text, "cap")) {
		t.Error("the repository's nit cap is not 5")
	}
	for _, row := range []string{"gaming", "plan deviation", "ignored design finding", "performance budget"} {
		if !strings.Contains(important, row) {
			t.Errorf("what counts as important does not name %q (Q4)", row)
		}
	}
}

func rpSaysInPolicy(t *testing.T, section string, d rpDuty) {
	t.Helper()
	if !slices.ContainsFunc(rpSentences(section), d.holds) {
		t.Errorf("the repository's REVIEW.md does not say %s:\n%s", d.name, section)
	}
}
