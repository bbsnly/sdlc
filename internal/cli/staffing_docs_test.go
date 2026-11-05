package cli

// US-026, what the documentation has to say: AC-2's wording rule, AC-11's
// three things a reader is told (how the choices are made, where they are
// read, how a person sets one), AC-12's count of agents, and the two risks the
// person accepted in decision 11, which T1 of the threats asks to be said in
// plain words.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// stfDocSection is one `## ` section of a docs page, from its heading to the
// next, with its white space folded: prose wraps wherever it wraps.
func stfDocSection(t *testing.T, name, heading string) string {
	t.Helper()
	_, rest, ok := strings.Cut(page(t, name), "\n"+heading)
	if !ok {
		t.Fatalf("docs/%s has no section headed %q", name, strings.TrimSpace(heading))
	}
	body, _, _ := strings.Cut(rest, "\n## ")
	return strings.Join(strings.Fields(body), " ")
}

func stfFolded(text string) string { return strings.Join(strings.Fields(text), " ") }

func TestAC11TheCommandsPageHasASectionForEachStaffingCommandAndSaysWhoRunsIt(t *testing.T) {
	t.Parallel()
	show := stfDocSection(t, "commands.md", "## `sdlc staffing show")
	for _, want := range []string{"`--gate`", "`--story`", "`--json`"} {
		if !strings.Contains(show, want) {
			t.Errorf("the section for `sdlc staffing show` does not mention %s", want)
		}
	}
	set := stfDocSection(t, "commands.md", "## `sdlc staffing set")
	if !strings.Contains(set, "`sdlc:project-manager`") || !strings.Contains(set, "`--file`") {
		t.Error("the section for `sdlc staffing set` does not say the project manager runs it, or how the staffing is given")
	}
	amend := stfDocSection(t, "commands.md", "## `sdlc staffing amend")
	for _, want := range []string{"`--needed`", "`--left-out`", "`--model`", "`--effort`", "`--reason`", "`sdlc:project-manager`"} {
		if !strings.Contains(amend, want) {
			t.Errorf("the section for `sdlc staffing amend` does not mention %s", want)
		}
	}
	// AC-11: how a person sets one.
	assign := stfDocSection(t, "commands.md", "## `sdlc staffing assign")
	for _, want := range []string{"a person", "terminal", "`--model`", "`--effort`", "`--reason`"} {
		if !strings.Contains(assign, want) {
			t.Errorf("the section for `sdlc staffing assign` does not say %q", want)
		}
	}
	if strings.Contains(assign, "`--left-out`") {
		t.Error("the section for `sdlc staffing assign` offers --left-out, and a person's command cannot leave an agent out (decision 8)")
	}
}

// AC-11: where a person reads the choices. AC-9 puts them in three commands.
func TestAC11TheDocumentationSaysWhereAPersonReadsTheStaffing(t *testing.T) {
	t.Parallel()
	agents := stfFolded(page(t, "the-agents.md"))
	for _, want := range []string{"`sdlc staffing show`", "`sdlc review list`", "`sdlc log`"} {
		if !strings.Contains(agents, want) {
			t.Errorf("docs/the-agents.md does not send a reader to %s for the staffing", want)
		}
	}
	for name, heading := range map[string]string{"review list": "## `sdlc review list`", "log": "## `sdlc log`"} {
		if section := stfDocSection(t, "commands.md", heading); !strings.Contains(section, "staffing") {
			t.Errorf("the section for `sdlc %s` says nothing of the staffing it shows", name)
		}
	}
}

// AC-11: how the choices are made. The project manager makes them, from what
// the work is and not from the story's risk tier or priority (decision 1),
// inside what the configuration allows; some agents are never left out.
func TestAC11TheAgentsPageSaysHowTheStaffingIsChosen(t *testing.T) {
	t.Parallel()
	agents := stfFolded(page(t, "the-agents.md"))
	for _, want := range []string{
		"`sdlc:project-manager`", "staffing", "`team`", "`sdlc staffing assign`",
		"never left out", "risk tier",
	} {
		if !strings.Contains(agents, want) {
			t.Errorf("docs/the-agents.md does not say %q", want)
		}
	}
	// The page said no agent names a model and `/model` moves the whole loop.
	// With the staffing on that is no longer the whole of it.
	models := stfDocSection(t, "the-agents.md", "## Models")
	if !strings.Contains(models, "staffing") {
		t.Error("the Models section of docs/the-agents.md does not say what the staffing changes")
	}
	if loop := page(t, "the-loop.md"); !strings.Contains(loop, "staffing") {
		t.Error("docs/the-loop.md, which owns the glossary, does not define the staffing")
	}
}

// AC-2: the tool records what was asked for. The environment can override
// the model and the effort, and nothing observes a run, so no page may say
// what an agent ran at.
func TestAC2TheDocumentationCallsItWhatWasAskedForAndNeverWhatAnAgentRanAt(t *testing.T) {
	t.Parallel()
	ran := regexp.MustCompile(`(?i)\b(ran|was run|were run) (at|on|with) (the |a |an )?(model|effort|haiku|sonnet|opus|low|medium|high|xhigh|max)\b`)
	for _, said := range []string{"the verifier ran at opus", "ran on the model", "was run at high", "Ran with a model"} {
		if !ran.MatchString(said) {
			t.Fatalf("the pattern cannot see %q", said)
		}
	}
	pages := map[string]string{}
	for _, name := range []string{"the-agents.md", "commands.md", "configuration.md", "the-loop.md", "enforcement.md", "troubleshooting.md"} {
		pages["docs/"+name] = page(t, name)
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	pages["README.md"] = string(readme)
	for name, body := range pages {
		if found := ran.FindString(stfFolded(body)); found != "" {
			t.Errorf("%s says %q: the tool knows what was asked for, not what ran", name, found)
		}
	}
	for _, where := range []struct{ what, text string }{
		{"the section for `sdlc staffing show`", stfDocSection(t, "commands.md", "## `sdlc staffing show")},
		{"the Models section of docs/the-agents.md", stfDocSection(t, "the-agents.md", "## Models")},
		{"the team section of docs/configuration.md", stfDocSection(t, "configuration.md", "## `team`")},
	} {
		if !strings.Contains(where.text, "asked for") {
			t.Errorf("%s does not call the model and effort what was asked for", where.what)
		}
	}
	// And why: the environment can override both.
	if models := stfDocSection(t, "the-agents.md", "## Models"); !strings.Contains(models, "environment") {
		t.Error("the Models section of docs/the-agents.md does not say the environment can override what was asked for")
	}
}

// AC-14: the section replaces `staffing` and its warning, says every agent is on by
// default and who picks, and names what loosens the loop.
func TestAC14TheConfigurationPageDocumentsTheTeamSectionAndWhatLoosensTheLoop(t *testing.T) {
	t.Parallel()
	if strings.Contains(page(t, "configuration.md"), "\n## `staffing`") {
		t.Error("docs/configuration.md still has a `staffing` section")
	}
	section := stfDocSection(t, "configuration.md", "## `team`")
	for _, key := range []string{
		"`retries`", "`models`", "`efforts`", "`agents`", "`enabled`", "`can_block`", "`max_rounds`", "`instances`", "`lowest`", "`bring_back`",
	} {
		if !strings.Contains(section, key) {
			t.Errorf("AC-14 the team section of docs/configuration.md does not document %s", key)
		}
	}
	for _, def := range []string{"haiku", "sonnet", "opus", "low", "medium", "high", "xhigh", "max", "`loop.max_review_rounds`"} {
		if !strings.Contains(section, def) {
			t.Errorf("AC-14 the team section does not give the default %s", def)
		}
	}
	for _, says := range []struct{ why, want string }{
		{"every agent is on by default", "on by default"},
		{"the project manager picks story by story", "story by story"},
		{"a disabled worker's gate is the person's", "by hand"},
		{"what loosens the loop", "loosen"},
		// T4: this file is writable by a session between stories, and the warning this
		// section replaces said so. The fact stays with it.
		{"a session can write the file between stories", "between stories"},
		// T15: the default flips on upgrade with nothing to stop it.
		{"what an upgrade changes", "upgrad"},
		// T3: the tool cannot see that two copies were two runs.
		{"copies are not shown to be independent", "independen"},
		// AC-7 as D-3 settled it: docs and doctor say where the limit does nothing.
		{"max_rounds counts blocks that stop the gate", "stop the gate"},
		{"max_rounds does nothing on an advisory agent", "advisory"},
	} {
		if !strings.Contains(section, says.want) {
			t.Errorf("AC-14 the team section does not say %s (%q)", says.why, says.want)
		}
	}
	for _, loosens := range []string{"`enabled`", "`can_block`"} {
		if !regexp.MustCompile(`(?i)loosen[^.]*` + regexp.QuoteMeta(loosens) + `|` + regexp.QuoteMeta(loosens) + `[^.]*loosen`).MatchString(section) {
			t.Errorf("AC-14 the team section does not say in one sentence that %s loosens the loop", loosens)
		}
	}
	// Decision 11 of US-026, still in plain words: the architect can be left out and
	// nothing brings it back at the design review, where there is no change to read; perf
	// can be left out, and bring_back is all that returns it.
	if !regexp.MustCompile(`architect[^.]*\.?[^.]*design review`).MatchString(section) &&
		!regexp.MustCompile(`design review[^.]*\.?[^.]*architect`).MatchString(section) {
		t.Error("the team section does not say that nothing brings the architect back at the design review")
	}
	if !strings.Contains(section, "perf") {
		t.Error("the team section does not say that perf can be left out and only bring_back returns it")
	}
	// AC-12: a person refused with the new code is sent here, and finds what each old key
	// became. SDLC-E0087 and SDLC-E0088 are named, with the code that stays (D-2).
	for _, want := range []string{
		"`staffing.enabled`", "`team.agents.project-manager`", "`staffing.models`", "`team.models`", "`staffing.efforts`", "`team.efforts`",
		"`staffing.lowest`", "`staffing.bring_back`", "`reviews.gate7_advisory`", "`team.agents.code-reviewer.can_block`",
		"SDLC-E0087", "SDLC-E0088",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("AC-12 the team section does not say %s, for a person the refusal sends here", want)
		}
	}
	// What init writes has the team and no reviews (TestTheConfigurationExampleIsWhatInitWrites
	// holds the example to init).
	_, rest, _ := strings.Cut(page(t, "configuration.md"), "```json\n")
	example, _, _ := strings.Cut(rest, "```")
	if !strings.Contains(example, `"team"`) || strings.Contains(example, `"reviews"`) || strings.Contains(example, `"staffing"`) {
		t.Errorf("AC-2 the example of what init writes is not what init writes now:\n%s", example)
	}
	// D-1: the leftover is documented where the refused value is, under its own heading.
	if leftover := stfDocSection(t, "configuration.md", "## `reviews`"); !strings.Contains(leftover, "`team.agents.code-reviewer.can_block`") {
		t.Errorf("AC-12 the `reviews` section does not say what replaced it: %s", leftover)
	}
}

// AC-14: the pages stop describing the staffing as off unless turned on. The configuration
// page and the troubleshooting page may name the old key, to map it to the new one, and no
// other may send a person to it.
func TestAC14NoPageDescribesTheStaffingAsOffUnlessTurnedOn(t *testing.T) {
	t.Parallel()
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	pages := map[string]string{"README.md": string(readme)}
	for _, name := range []string{
		"commands.md", "configuration.md", "the-agents.md", "the-loop.md", "troubleshooting.md", "enforcement.md",
		"guides/when-a-reviewer-blocks.md",
	} {
		pages["docs/"+name] = page(t, name)
	}
	said := regexp.MustCompile(`(?i)off unless|(turns?|turned|turning) the staffing on|with the staffing (on|off)|the staffing is (on|off)|configuration\.md#staffing`)
	for name, body := range pages {
		if found := said.FindString(stfFolded(body)); found != "" {
			t.Errorf("AC-14 %s still says %q", name, found)
		}
		if name == "docs/configuration.md" || name == "docs/troubleshooting.md" {
			continue
		}
		for _, old := range []string{"staffing.enabled", "staffing.models", "staffing.efforts", "staffing.lowest", "staffing.bring_back", "gate7_advisory"} {
			if strings.Contains(body, old) {
				t.Errorf("AC-14 %s sends a person to %s, which is refused or does nothing", name, old)
			}
		}
	}
	for name, wants := range map[string][]string{
		"the-agents.md":                    {"`can_block`", "by hand"},
		"the-loop.md":                      {"`can_block`", "`team`"},
		"guides/when-a-reviewer-blocks.md": {"`can_block`"},
	} {
		for _, want := range wants {
			if !strings.Contains(stfFolded(page(t, name)), want) {
				t.Errorf("AC-14 docs/%s does not say %s", name, want)
			}
		}
	}
	if !strings.Contains(string(readme), "team") {
		t.Error("AC-14 README.md does not say how the team is set")
	}
}

// AC-9: a command a session runs has its section, and what it counts is said there.
func TestAC9TheCommandsPageHasASectionForTheFailureCommand(t *testing.T) {
	t.Parallel()
	section := stfDocSection(t, "commands.md", "## `sdlc team failure")
	for _, want := range []string{"`--reason`", "`team.retries`", "`--json`", "gate", "`sdlc approve`"} {
		if !strings.Contains(section, want) {
			t.Errorf("AC-9 the section for `sdlc team failure` does not say %s", want)
		}
	}
	// What it cannot do is the point of it (T6).
	if !strings.Contains(section, "substitute") && !strings.Contains(section, "nothing passes") {
		t.Error("T6 the section does not say that a failure passes nothing and nobody takes the agent's place")
	}
	// The guide a person is sent to lists the kinds of hand-over.
	if !strings.Contains(page(t, "guides/human-approval-gates.md"), "team failure") {
		t.Error("AC-9 docs/guides/human-approval-gates.md does not list this hand-over")
	}
	if !strings.Contains(stfDocSection(t, "enforcement.md", "## Rules on shell commands"), "team failure") {
		t.Error("AC-9 docs/enforcement.md does not say who may report a failure")
	}
}

// The findings of the plan's reviews that a person relies on reading. Each row holds that the
// sentence is there, not that it is true: a person is told, or the docs claim more than the
// tool does.
func TestAC14ThePagesSayWhatTheReviewsOfThePlanAskedThemToSay(t *testing.T) {
	t.Parallel()
	team := stfDocSection(t, "configuration.md", "## `team`")
	guide := stfFolded(page(t, "guides/human-approval-gates.md"))
	agentsPage := stfFolded(page(t, "the-agents.md"))
	for _, tc := range []struct {
		why, where, text string
		wants            []string
	}{
		// Red team H1: a session can hand over under any other word, and a person who has
		// read that "failure hand-overs do not count" trusts the look-alike.
		{"the closure is for those two kinds only", "guides/human-approval-gates.md", guide,
			[]string{"`agent_failed`", "`work_by_hand`", "pre-commit"}},
		// Security note 7, T3: one run can record both, and the docs may not claim otherwise.
		{"one run may record both names, and the trace shows the runs", "the-agents.md", agentsPage,
			[]string{"`security-2`", "`security`", "`sdlc trace`"}},
		// Red team C2: the natural reply to a failing agent is to turn it off, and in a worktree
		// the running story does not read that.
		{"a setting reaches stories started after it is on trunk", "configuration.md", team,
			[]string{"started after", "trunk"}},
		// Human advocate 1: the code's own section knows the three new cases.
		{"E0027 covers the reasons the story adds", "troubleshooting.md", stfFolded(page(t, "troubleshooting.md")),
			[]string{"`team.agents", "`instances`"}},
		// Human advocate 4: the page that owns the word.
		{"the glossary owns instance", "the-loop.md", stfFolded(page(t, "the-loop.md")),
			[]string{"instance", "`security-2`"}},
	} {
		for _, want := range tc.wants {
			if !strings.Contains(tc.text, want) {
				t.Errorf("AC-14 docs/%s does not say that %s (%q)", tc.where, tc.why, want)
			}
		}
	}
	if !regexp.MustCompile(`(?i)\b(only|other kind|another kind|every other)\b[^.]*(\x60agent_failed\x60|\x60work_by_hand\x60)|(\x60agent_failed\x60|\x60work_by_hand\x60)[^.]*\b(only|other kind|another kind|every other)\b`).MatchString(guide) {
		t.Error("AC-14 docs/guides/human-approval-gates.md does not say the closure is for those two kinds only")
	}
	if !regexp.MustCompile(`(?i)\b(one|single|same) run\b`).MatchString(agentsPage) {
		t.Error("AC-14 docs/the-agents.md does not say that one run may record both names")
	}
	// The word a person meets beside the setting.
	if found := regexp.MustCompile(`(?i)\bcop(y|ies)\b`).FindString(team); found != "" {
		t.Errorf("AC-8 the team section says %q, and the word is instance", found)
	}
	// Architect note 1: the install offer stays, and the two pages that promise it still do.
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"docs/installation.md": page(t, "installation.md"), "README.md": string(readme)} {
		folded := stfFolded(body)
		if !strings.Contains(folded, "/sdlc:next") || !strings.Contains(folded, "offers to install") {
			t.Errorf("AC-14 %s no longer says /sdlc:next offers to install", name)
		}
	}
}

// AC-12: the count of agents in prose agrees with the plugin.
func TestAC12EveryPageThatCountsTheAgentsSaysTwelve(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(filepath.Join("..", "..", "plugin", "agents"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 12 {
		t.Errorf("plugin/agents holds %d agents, want 12: the eleven and the project manager", len(entries))
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pg := range []struct{ name, body, want string }{
		{"README.md", string(readme), "twelve agents"},
		{"docs/installation.md", page(t, "installation.md"), "twelve agents"},
		{"docs/the-agents.md", page(t, "the-agents.md"), "Twelve agents"},
		{"docs/the-loop.md", page(t, "the-loop.md"), "Twelve agents"},
	} {
		body := stfFolded(pg.body)
		if !strings.Contains(body, pg.want) {
			t.Errorf("%s does not say %q", pg.name, pg.want)
		}
		if strings.Contains(strings.ToLower(body), "eleven agents") {
			t.Errorf("%s still counts eleven agents", pg.name)
		}
	}
	if n := strings.Count(stfFolded(page(t, "installation.md")), "twelve agents"); n < 2 {
		t.Errorf("docs/installation.md counts the agents in two places, and %d say twelve", n)
	}
}

// Decision 10 and AC-12, in the page a refused agent is sent to: who records
// the staffing, and what the project manager may run.
func TestAC12TheEnforcementPageSaysWhoWritesTheStaffingAndWhatTheProjectManagerMayRun(t *testing.T) {
	t.Parallel()
	doc := page(t, "enforcement.md")
	for rule, wants := range map[string][]string{
		"assignment-is-a-human-decision":              {"`sdlc staffing assign`", "terminal"},
		"staffing-is-recorded-by-the-project-manager": {"`sdlc staffing set`", "`sdlc staffing amend`", "`sdlc:project-manager`"},
		"project-manager-only-staffs":                 {"`sdlc:project-manager`", "`sdlc staffing show`"},
	} {
		_, rest, ok := strings.Cut(doc, "### `"+rule+"`")
		if !ok {
			t.Errorf("docs/enforcement.md has no section for the %s rule", rule)
			continue
		}
		body, _, _ := strings.Cut(rest, "\n### ")
		body = stfFolded(body)
		for _, want := range wants {
			if !strings.Contains(body, want) {
				t.Errorf("the section for %s does not mention %s", rule, want)
			}
		}
	}
}
