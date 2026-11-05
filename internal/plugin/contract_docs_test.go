package plugin

// US-110: the runbook reads the contract's hand-back (AC-10), and the agents
// page defines the contract and says what the moved definitions say (AC-1,
// AC-6, AC-15).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// ------------------------------------------------------------------- the runbook

const runbookPath = "plugin/skills/next/SKILL.md"

// The fields the runbook reads from what an agent hands back, which belong in
// `details` (AC-9 names these four). The runbook read them from the top level.
var runbookFields = []string{"security_sensitive", "open_questions", "untestable", "cannot_staff"}

var (
	detailsRef  = regexp.MustCompile(`details\.([a-z_]+)`)
	delegateRef = regexp.MustCompile(`sdlc:([a-z-]+)`)
)

// checkRunbook holds the runbook to the hand-back fields of the agents it reads
// (AC-10): a `details.<field>` in a section is a field of an agent the section
// delegates to, and a field the runbook read from the top level before is not
// read there now. Only a bare field in backticks counts: `sdlc escalate
// untestable` names an escalation type, not a field.
func checkRunbook(runbook string, agents []Component) []breach {
	details := map[string][]string{}
	for _, a := range agents {
		details[fileBase(a)] = detailKeys(a)
	}
	f := &found{file: runbookPath}
	head, secs := splitBody(runbook)
	all := append([]section{{Heading: "the start", Text: head}}, secs...)
	for _, s := range all {
		var named []string
		for _, m := range delegateRef.FindAllStringSubmatch(s.Text, -1) {
			if _, ok := details[m[1]]; ok && !slices.Contains(named, m[1]) {
				named = append(named, m[1])
			}
		}
		if len(named) == 0 {
			named = sortedKeys(details)
		}
		for _, m := range detailsRef.FindAllStringSubmatch(s.Text, -1) {
			if !slices.ContainsFunc(named, func(a string) bool { return slices.Contains(details[a], m[1]) }) {
				f.addf(ruleRunbook, "`## %s` reads `details.%s`, and %s hand%s back no such field in `details`: read a field the agent hands back, "+
					"or add the field to its Hand-back example", s.Heading, m[1], strings.Join(named, ", "), plural(len(named)))
			}
		}
		for _, field := range runbookFields {
			if regexp.MustCompile("`" + field + `\b`).MatchString(s.Text) {
				f.addf(ruleRunbook, "`## %s` reads `%s` from the top of what an agent hands back: it is in `details`, so write `details.%s`",
					s.Heading, field, field)
			}
		}
	}
	return f.list
}

func plural(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}

func sortedKeys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func theNextRunbook(t *testing.T) Component {
	t.Helper()
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if s.Name == "next" {
			return s
		}
	}
	t.Fatal("the plugin ships no next skill")
	return Component{}
}

func TestAC10TheRunbookReadsOnlyFieldsTheAgentItIsReadingHandsBack(t *testing.T) {
	for _, b := range checkRunbook(theNextRunbook(t).Body, shippedAgents(t)) {
		t.Error(b.String())
	}
}

// AC-10: the decisions the runbook makes today, from the contract's fields. Each
// is looked for in the section of the gate where the runbook makes it, since a
// field named at the other end of the runbook is not the gate's decision.
func TestAC10TheRunbookMakesItsDecisionsFromTheContractsFields(t *testing.T) {
	_, secs := splitBody(theNextRunbook(t).Body)
	for _, tc := range []struct{ gate, field string }{
		{"Gate 1 — Select the story", "details.cannot_staff"},
		{"Gate 2 — Analysis and threats", "details.security_sensitive"},
		{"Gate 2 — Analysis and threats", "details.open_questions"},
		{"Gate 3 — Acceptance tests, then frozen", "details.untestable"},
	} {
		s, ok := sectionNamed(secs, tc.gate)
		switch {
		case !ok:
			t.Errorf("the runbook has no section %q", tc.gate)
		case !strings.Contains(s.Text, tc.field):
			t.Errorf("`## %s` of the runbook does not read `%s`: decide from the field the contract puts it in", tc.gate, tc.field)
		}
	}
	runbook := fold(theNextRunbook(t).Body)
	for _, field := range []string{"`status`", "`blocked_on`"} {
		if !strings.Contains(runbook, field) {
			t.Errorf("the runbook never reads %s from what an agent hands back", field)
		}
	}
}

// AC-10: a final message that is not the contract's JSON is a run that returned
// nothing usable, which is what `sdlc team failure` counts for team.retries.
func TestAC10TheRunbookCountsAMessageThatIsNotTheContractsJSONAsARunThatReturnedNothingUsable(t *testing.T) {
	duty := rpDuty{"to count a final message that is not one JSON object with the contract's keys as a run that returned nothing usable",
		[][]string{{"json"}, {"nothing usable"}}}
	if !slices.ContainsFunc(rpSentences(theNextRunbook(t).Body), duty.holds) {
		t.Errorf("the runbook does not say %s", duty.name)
	}
}

// The check itself: a field the agent does not hand back is named, with the
// section and the agent, and a field it does hand back is not.
func TestAC10ARunbookFieldTheAgentItReadsDoesNotHandBackIsRefused(t *testing.T) {
	view := theBinary(t)
	texts := fixtureSet(view)
	texts["researcher"] = mutate(t, texts["researcher"], `"details": {"checked": 0}`, `"details": {"open_questions": 0, "security_sensitive": false}`)
	agents := loadFixtures(t, texts)

	runbook := "# Work\n\n## Gate 2 — Analysis\n\nDelegate to `sdlc:researcher`. If it reports `details.open_questions` above zero, stop.\n"
	if got := checkRunbook(runbook, agents); len(got) != 0 {
		t.Errorf("a field the researcher hands back was refused: %v", got)
	}

	runbook = "# Work\n\n## Gate 2 — Analysis\n\nDelegate to `sdlc:researcher`. If it reports `details.untestable`, stop.\n"
	got := checkRunbook(runbook, agents)
	if len(got) != 1 || got[0].Rule != ruleRunbook || got[0].File != runbookPath {
		t.Fatalf("breaches = %v, want one against the runbook", got)
	}
	for _, part := range []string{"Gate 2", "details.untestable", "researcher"} {
		if !strings.Contains(got[0].Change, part) {
			t.Errorf("the breach %q does not say %q", got[0], part)
		}
	}
}

// AC-10: a field only another gate's agent hands back is not the field of this
// gate's, even though some agent does hand it back.
func TestAC10AFieldOnlyAnotherGatesAgentHandsBackIsRefusedAtThisGate(t *testing.T) {
	view := theBinary(t)
	texts := fixtureSet(view)
	texts["sdet"] = mutate(t, texts["sdet"], `"details": {"checked": 0}`, `"details": {"untestable": []}`)
	agents := loadFixtures(t, texts)
	runbook := "# Work\n\n## Gate 2 — Analysis\n\nDelegate to `sdlc:researcher`. If it reports `details.untestable`, stop.\n\n" +
		"## Gate 3 — Tests\n\nDelegate to `sdlc:sdet`. If it reports `details.untestable`, stop.\n"
	got := checkRunbook(runbook, agents)
	if len(got) != 1 || !strings.Contains(got[0].Change, "Gate 2") {
		t.Errorf("breaches = %v, want only Gate 2's", got)
	}
}

// AC-10: the top-level spelling of a field the contract moves into details.
func TestAC10AFieldReadFromTheTopOfTheHandBackIsRefused(t *testing.T) {
	view := theBinary(t)
	agents := loadFixtures(t, fixtureSet(view))
	for _, field := range runbookFields {
		runbook := "# Work\n\n## Gate 2 — Analysis\n\nIf the agent reports `" + field + "` above zero, stop.\n"
		got := checkRunbook(runbook, agents)
		if len(got) != 1 || !strings.Contains(got[0].Change, "details."+field) {
			t.Errorf("a runbook that reads %s from the top: breaches = %v", field, got)
		}
	}
}

// ------------------------------------------------------------------ the page

func agentsPage(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "the-agents.md"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

func pageSection(t *testing.T, heading string) string {
	t.Helper()
	_, secs := splitBody(agentsPage(t))
	s, ok := sectionNamed(secs, heading)
	if !ok {
		t.Fatalf("docs/the-agents.md has no `## %s` section", heading)
	}
	return s.Text
}

// AC-1: the section replaces Writing your own, and the table of contents follows.
func TestAC1TheAgentsPageDefinesTheAgentContractInPlaceOfWritingYourOwn(t *testing.T) {
	page := agentsPage(t)
	_, secs := splitBody(page)
	if _, ok := sectionNamed(secs, "The agent contract"); !ok {
		t.Error("docs/the-agents.md has no `## The agent contract` section: add it where `## Writing your own` was")
	}
	if _, ok := sectionNamed(secs, "Writing your own"); ok {
		t.Error("docs/the-agents.md still has `## Writing your own`: the agent contract replaces it")
	}
	if !strings.Contains(page, "(#the-agent-contract)") {
		t.Error("the table of contents of docs/the-agents.md does not link `#the-agent-contract`")
	}
	if strings.Contains(page, "(#writing-your-own)") {
		t.Error("the table of contents of docs/the-agents.md still links `#writing-your-own`")
	}
}

// AC-1: no model, tier or provider in the section; runtime tools only in the one
// table that maps each capability to the tool that grants it.
func TestAC1TheAgentContractNamesNoModelAndRuntimeToolsOnlyInOneTable(t *testing.T) {
	section := pageSection(t, "The agent contract")
	if m := modelWords.FindString(section); m != "" {
		t.Errorf("the agent contract section names %q: it names no model, model tier or provider", m)
	}
	tool := regexp.MustCompile("`(Read|Grep|Glob|Bash|Write|Edit|MultiEdit|NotebookEdit|Task|Agent|WebFetch|WebSearch)`")
	var tables [][]string
	var run []string
	flush := func() {
		if len(run) > 0 {
			tables = append(tables, run)
			run = nil
		}
	}
	for line := range strings.SplitSeq(section, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			run = append(run, line)
			continue
		}
		flush()
		if m := tool.FindString(line); m != "" {
			t.Errorf("the agent contract names the runtime tool %s outside its one table: %q", m, strings.TrimSpace(line))
		}
	}
	flush()
	var toolTables [][]string
	for _, tb := range tables {
		if tool.MatchString(strings.Join(tb, "\n")) {
			toolTables = append(toolTables, tb)
		}
	}
	if len(toolTables) != 1 {
		t.Fatalf("the agent contract has %d tables that name runtime tools, want one that maps each capability to the tool that grants it", len(toolTables))
	}
	rows := toolTables[0]
	for _, capability := range []string{"read", "search", "command", "write", "edit", "delegat"} {
		ok := slices.ContainsFunc(rows, func(row string) bool {
			cells := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")
			return len(cells) >= 2 && strings.Contains(strings.ToLower(cells[0]), capability) && tool.MatchString(cells[1])
		})
		if !ok {
			t.Errorf("the table of tools has no row that maps the capability %q to a tool", capability)
		}
	}
}

// AC-1: the section says a new agent must conform and that the tests refuse one
// that does not.
func TestAC1TheAgentContractSaysANewAgentMustConformAndTheTestsRefuseOneThatDoesNot(t *testing.T) {
	duty := rpDuty{"that a new agent under plugin/agents must conform and the plugin's tests refuse one that does not",
		[][]string{{"plugin/agents"}, {"conform"}, {"test"}, {"refuse"}}}
	if !slices.ContainsFunc(rpSentences(pageSection(t, "The agent contract")), duty.holds) {
		t.Errorf("the agent contract section does not say %s", duty.name)
	}
}

// AC-1: the section defines everything AC-2 to AC-11 set, naming each key,
// section and column.
func TestAC1TheAgentContractDefinesTheFrontmatterTheSectionsTheHandBackAndTheReviewDocument(t *testing.T) {
	section := fold(pageSection(t, "The agent contract"))
	var want []string
	for _, key := range []string{"name", "description", "tools", "model", "maxTurns", "sdlc-kind", "sdlc-gates", "sdlc-team", "skills"} {
		want = append(want, "`"+key+"`")
	}
	for _, kind := range []string{"worker", "reviewer", "advisor"} {
		want = append(want, "`"+kind+"`")
	}
	for _, name := range requiredSections {
		want = append(want, "`"+name+"`")
	}
	for _, key := range append(slices.Clone(baseHandBackKeys), reviewerHandBackKeys...) {
		want = append(want, "`"+key+"`")
	}
	want = append(want, "`approve`", "`block`", "Finding", "Where", "Why it matters", "What would fix it", "Vector", "`team`")
	for _, w := range want {
		if !strings.Contains(section, w) {
			t.Errorf("the agent contract section does not mention %s", w)
		}
	}
}

// AC-6: a person who has disabled a worker is sent to its definition's Outputs.
func TestAC6ThePageSendsAPersonWhoDisabledAWorkerToTheOutputsOfItsDefinition(t *testing.T) {
	duty := rpDuty{"to send a person who has disabled a worker to the Outputs section of its definition",
		[][]string{{"disabled"}, {"outputs"}, {"definition"}, {"person", "you "}}}
	if !slices.ContainsFunc(rpSentences(agentsPage(t)), duty.holds) {
		t.Errorf("docs/the-agents.md does not say %s", duty.name)
	}
}

// AC-15: Who does what says what the moved definitions say. A reviewer that a
// person may let block is not a dash in the table, whichever reviewer it is.
func TestAC15TheWhoDoesWhatTableSaysTheRedTeamAndTheHumanAdvocateCanBlockWhenTheProjectSaysSo(t *testing.T) {
	table := pageSection(t, "Who does what")
	for _, agent := range []string{"red-team", "human-advocate"} {
		found := false
		for row := range strings.SplitSeq(table, "\n") {
			cells := strings.Split(strings.Trim(strings.TrimSpace(row), "|"), "|")
			if len(cells) < 4 || !strings.Contains(cells[0], "`sdlc:"+agent+"`") {
				continue
			}
			found = true
			if !strings.Contains(cells[2], "`can_block`") {
				t.Errorf("the row of %s says %q under \"Can it block?\": a person may set `can_block` true, and the row says so", agent, strings.TrimSpace(cells[2]))
			}
		}
		if !found {
			t.Errorf("Who does what has no row for sdlc:%s", agent)
		}
	}
}

// AC-12 and AC-15: the page no longer says that a reviewer records a note, or
// that some reviewers never block.
func TestAC15ThePageNoLongerSaysAReviewerRecordsANoteOrThatSomeNeverBlock(t *testing.T) {
	page := fold(agentsPage(t))
	for _, stale := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bnever block\b`),
		regexp.MustCompile("(?i)stays a note"),
		regexp.MustCompile("(?i)or a `note`"),
		regexp.MustCompile("(?i)where that role's own command line gives one"),
	} {
		if m := stale.FindString(page); m != "" {
			t.Errorf("docs/the-agents.md still says %q, which the moved definitions no longer do", m)
		}
	}
}

// AC-15: What they are told carries what the definitions now share and say.
func TestAC15WhatTheyAreToldSaysWhatTheMovedDefinitionsSay(t *testing.T) {
	told := fold(pageSection(t, "What they are told"))
	for _, want := range []string{"`unchecked`", "`addressed_me`", "`approve`", "`block`", "`can_block`"} {
		if !strings.Contains(told, want) {
			t.Errorf("What they are told does not mention %s", want)
		}
	}
	duty := rpDuty{"that a reviewer records approve or block whether or not the project lets its block stop the gate",
		[][]string{{"approve"}, {"block"}, {"whether or not", "even where", "even when", "whatever"}, {"stop"}}}
	if !slices.ContainsFunc(rpSentences(pageSection(t, "What they are told")), duty.holds) {
		t.Errorf("What they are told does not say %s", duty.name)
	}
	if injection := fold(pageSection(t, "Prompt injection")); !strings.Contains(injection, "`addressed_me`") {
		t.Error("Prompt injection does not say the agents list what addressed them in `addressed_me`")
	}
}

// An example the agent contract gives in JSON parses, since a reader copies it.
func TestAC1AnyJSONExampleInTheAgentContractParses(t *testing.T) {
	for _, ex := range fencedBlocks(pageSection(t, "The agent contract"), "json") {
		var v any
		if err := json.Unmarshal([]byte(ex), &v); err != nil {
			t.Errorf("a ```json example in the agent contract does not parse (%v):\n%s", err, ex)
		}
	}
}
