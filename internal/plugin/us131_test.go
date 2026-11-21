package plugin

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// US-131. The text a model reads: docs/the-loop.md defines the two kinds of
// decision in one section (AC-1), the runbook and every agent point to it and act
// on it (AC-2, AC-5, AC-7, AC-10). Each duty is a group of words in one sentence,
// with US-115's rpSentences and rpDuty, so the words are the criteria's and the
// prose is the implementer's.

// dkSection is the heading and the text of the section of docs/the-loop.md that
// defines both kinds of decision, to the next heading.
func dkSection(t *testing.T) (heading, text string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "the-loop.md"))
	if err != nil {
		t.Fatal(err)
	}
	var found []string
	parts := regexp.MustCompile(`(?m)^#{2,4} `).Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), -1)
	for _, part := range parts[1:] {
		lower := strings.ToLower(part)
		if strings.Contains(lower, "product decision") && strings.Contains(lower, "implementation decision") {
			found = append(found, part)
		}
	}
	if len(found) != 1 {
		t.Fatalf("AC-1 docs/the-loop.md has %d sections that define both a product and an implementation decision, want one", len(found))
	}
	heading, text, _ = strings.Cut(found[0], "\n")
	return strings.TrimSpace(heading), text
}

// dkAnchor is the anchor GitHub gives a heading: lower case, punctuation but
// hyphens dropped, spaces as hyphens.
func dkAnchor(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// dkRunbook is the next runbook's body.
func dkRunbook(t *testing.T) string {
	t.Helper()
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if s.Name == "next" {
			return s.Body
		}
	}
	t.Fatal("there is no next runbook")
	return ""
}

// dkSays reports every duty no sentence of text holds.
func dkSays(text string, duties ...rpDuty) []string {
	var missing []string
	sentences := rpSentences(text)
	for _, d := range duties {
		if !slices.ContainsFunc(sentences, d.holds) {
			missing = append(missing, d.name)
		}
	}
	return missing
}

func TestAC1TheLoopDefinesProductAndImplementationDecisionsInOneSection(t *testing.T) {
	t.Parallel()
	_, text := dkSection(t)
	lower := strings.ToLower(strings.Join(strings.Fields(text), " "))
	for what, words := range map[string][]string{
		"what the user sees or can do":                          {"user"},
		"which of two readings of a criterion is meant":         {"reading"},
		"what is in or out of the story's scope":                {"scope"},
		"what the project's SDLC Contract reserves to a person": {"contract", "person"},
		"how an edge case behaves":                              {"edge case"},
		"whether a suspected bug is real":                       {"bug"},
		"which of two designs meets the criteria":               {"design"},
		"what kind of test to write":                            {"test"},
		"an internal name":                                      {"name"},
		"which open-source dependency to use":                   {"open-source", "dependenc"},
		"that every other decision is an implementation one":    {"every other", "any other", "all other", "everything else"},
	} {
		hit := false
		for _, word := range words {
			if strings.Contains(lower, word) {
				hit = true
			}
		}
		if what == "what the project's SDLC Contract reserves to a person" || what == "which open-source dependency to use" {
			hit = strings.Contains(lower, words[0]) && strings.Contains(lower, words[1])
		}
		if !hit {
			t.Errorf("AC-1 the section does not say %s (%v):\n%s", what, words, text)
		}
	}
}

// AC-1: every agent definition and the runbook point to that one section.
func TestAC1EveryAgentAndTheRunbookPointToTheDefinition(t *testing.T) {
	t.Parallel()
	heading, _ := dkSection(t)
	link := "the-loop.md#" + dkAnchor(heading)
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 12 {
		t.Fatalf("there are %d agents, want 12", len(agents))
	}
	for _, a := range agents {
		if !strings.Contains(a.Body, link) {
			t.Errorf("AC-1 %s does not point to %s", a.Path, link)
		}
	}
	if !strings.Contains(dkRunbook(t), link) {
		t.Errorf("AC-1 the next runbook does not point to %s", link)
	}
}

func TestAC2TheRunbookRecordsAnImplementationDecisionAndDoesNotEscalateIt(t *testing.T) {
	t.Parallel()
	body := dkRunbook(t)
	spelled := false
	for line := range strings.SplitSeq(body, "\n") {
		if strings.Contains(line, "sdlc decide") && strings.Contains(line, "--question") && strings.Contains(line, "--option") &&
			strings.Contains(line, "--chosen") && strings.Contains(line, "--reason") && strings.Contains(line, "--by") {
			spelled = true
		}
	}
	if !spelled {
		t.Error("AC-2 the runbook does not give `sdlc decide` with --question, --option, --chosen, --reason and --by")
	}
	for _, m := range dkSays(body,
		rpDuty{"to take the recommendation of the agent that raised an implementation decision",
			[][]string{{"implementation"}, {"recommend"}}},
		rpDuty{"to take the arbiter's ruling where two agents disagree", [][]string{{"arbiter"}, {"ruling", "rules", "decides"}}},
		rpDuty{"not to escalate an implementation decision",
			[][]string{{"implementation"}, {"escalat"}, {"not", "never", "no "}}},
	) {
		t.Errorf("AC-2 the runbook does not say %s", m)
	}
}

func TestAC5TheResearcherSortsEachOpenQuestionAndRecommends(t *testing.T) {
	t.Parallel()
	body := rpAgentBody(t, "researcher")
	for _, m := range dkSays(body,
		rpDuty{"to mark each open question product or implementation",
			[][]string{{"open question"}, {"product"}, {"implementation"}, {"each", "every"}}},
		rpDuty{"to give the line of AC-1's definition it rests on", [][]string{{"line", "clause", "part"}, {"definition", "the-loop.md"}}},
		rpDuty{"to give a recommendation for each", [][]string{{"recommend"}, {"each", "every"}}},
		rpDuty{"that open_questions counts the product questions", [][]string{{"open_questions"}, {"product"}}},
	) {
		t.Errorf("AC-5 the researcher's definition does not say %s", m)
	}
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.Name != "researcher" {
			continue
		}
		keys := detailKeys(a)
		for _, key := range []string{"open_questions", "implementation_questions"} {
			if !slices.Contains(keys, key) {
				t.Errorf("AC-5 the researcher's hand-back has no details.%s (IQ-10): %v", key, keys)
			}
		}
	}
}

func TestAC5TheRunbookDecidesImplementationQuestionsAndEscalatesTheProductOnesOnce(t *testing.T) {
	t.Parallel()
	gate2 := rpRunbookSection(t, "Gate 2")
	for _, word := range []string{"sdlc decide", "product_decision"} {
		if !strings.Contains(gate2, word) {
			t.Errorf("AC-5 Gate 2 of the runbook does not name %s", word)
		}
	}
	for _, m := range dkSays(gate2,
		rpDuty{"to decide every implementation question", [][]string{{"implementation"}, {"decide", "record"}}},
		rpDuty{"to escalate once for the product questions", [][]string{{"product"}, {"once", "one escalation", "a single"}}},
		rpDuty{"to list every product question", [][]string{{"every", "all", "each"}, {"product question"}}},
		rpDuty{"to give two or three options and a recommendation each", [][]string{{"option"}, {"recommend"}}},
	) {
		t.Errorf("AC-5 Gate 2 of the runbook does not say %s", m)
	}
}

// AC-7: spec_unclear and design_decision are for a product decision only, so the
// paragraph that spells one, or the one just before it, says so.
func TestAC7TheRunbookAndTheAgentsEscalateSpecUnclearAndDesignDecisionOnlyForAProductDecision(t *testing.T) {
	t.Parallel()
	texts := map[string]string{"plugin/skills/next/SKILL.md": dkRunbook(t)}
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		texts[a.Path] = a.Body
	}
	spelled := 0
	for path, text := range texts {
		blocks := regexp.MustCompile(`\n\s*\n`).Split(strings.ReplaceAll(text, "\r\n", "\n"), -1)
		for i, block := range blocks {
			if !strings.Contains(block, "spec_unclear") && !strings.Contains(block, "design_decision") {
				continue
			}
			spelled++
			around := strings.ToLower(block)
			if i > 0 {
				around = strings.ToLower(blocks[i-1]) + "\n" + around
			}
			if !strings.Contains(around, "product") {
				t.Errorf("AC-7 %s escalates spec_unclear or design_decision without saying it is for a product decision:\n%s", path, block)
			}
		}
	}
	if spelled == 0 {
		t.Error("AC-7 neither the runbook nor any agent names spec_unclear or design_decision any longer")
	}
}

// AC-10 and the settled IQ-12: each agent says in its own Rules, in AC-1's words,
// what it recommends on and what it stops for.
func TestAC10EveryAgentSaysInItsRulesWhatItRecommendsOnAndWhatItStopsFor(t *testing.T) {
	t.Parallel()
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		_, secs := splitBody(a.Body)
		rules, ok := sectionNamed(secs, "Rules")
		if !ok {
			t.Errorf("AC-10 %s has no Rules section", a.Path)
			continue
		}
		for _, m := range dkSays(rules.Text,
			rpDuty{"that it recommends on an implementation decision", [][]string{{"implementation decision"}, {"recommend"}}},
			rpDuty{"that it stops for a product decision",
				[][]string{{"product decision"}, {"stop", "escalat", "hand", "blocked"}}},
		) {
			t.Errorf("AC-10 %s's Rules do not say %s", a.Path, m)
		}
	}
}

// AC-6: the runbook sends a reviewer's limit-th block to an arbiter, recorded as
// <role>-arbiter, whose rulings are closed and recorded, and the-loop.md says what
// an arbiter is.
func TestAC6TheRunbookDelegatesTheLimitThBlockToAnArbiter(t *testing.T) {
	t.Parallel()
	body := dkRunbook(t)
	if !strings.Contains(body, "-arbiter") {
		t.Error("AC-6 the runbook does not name the arbiter as <role>-arbiter")
	}
	for _, m := range dkSays(body,
		rpDuty{"to delegate the limit-th block to an arbiter", [][]string{{"max_review_rounds", "max_rounds", "limit"}, {"arbiter"}}},
		rpDuty{"to close each ruling", [][]string{{"arbiter", "ruling"}, {"sdlc finding close"}}},
		rpDuty{"to record each ruling", [][]string{{"arbiter", "ruling"}, {"sdlc decide"}}},
	) {
		t.Errorf("AC-6 the runbook does not say %s", m)
	}
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "the-loop.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(raw)), "arbiter") {
		t.Error("AC-6 docs/the-loop.md does not say what an arbiter is")
	}
}
