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
	agents := stfFolded(page(t, "agents.md"))
	for _, want := range []string{"`sdlc staffing show`", "`sdlc review list`", "`sdlc log`"} {
		if !strings.Contains(agents, want) {
			t.Errorf("docs/agents.md does not send a reader to %s for the staffing", want)
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
	agents := stfFolded(page(t, "agents.md"))
	for _, want := range []string{
		"`sdlc:project-manager`", "staffing", "`staffing.enabled`", "`sdlc staffing assign`",
		"never left out", "risk tier",
	} {
		if !strings.Contains(agents, want) {
			t.Errorf("docs/agents.md does not say %q", want)
		}
	}
	// The page said no agent names a model and `/model` moves the whole loop.
	// With the staffing on that is no longer the whole of it.
	models := stfDocSection(t, "agents.md", "## Models")
	if !strings.Contains(models, "staffing") {
		t.Error("the Models section of docs/agents.md does not say what the staffing changes")
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
	for _, name := range []string{"agents.md", "commands.md", "configuration.md", "the-loop.md", "enforcement.md", "troubleshooting.md"} {
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
		{"the Models section of docs/agents.md", stfDocSection(t, "agents.md", "## Models")},
		{"the staffing section of docs/configuration.md", stfDocSection(t, "configuration.md", "## `staffing`")},
	} {
		if !strings.Contains(where.text, "asked for") {
			t.Errorf("%s does not call the model and effort what was asked for", where.what)
		}
	}
	// And why: the environment can override both.
	if models := stfDocSection(t, "agents.md", "## Models"); !strings.Contains(models, "environment") {
		t.Error("the Models section of docs/agents.md does not say the environment can override what was asked for")
	}
}

func TestAC11TheConfigurationPageDocumentsTheStaffingSectionAndWhatTurningItOnLoosens(t *testing.T) {
	t.Parallel()
	section := stfDocSection(t, "configuration.md", "## `staffing`")
	for _, key := range []string{"`enabled`", "`models`", "`efforts`", "`lowest`", "`bring_back`"} {
		if !strings.Contains(section, key) {
			t.Errorf("the staffing section of docs/configuration.md does not document %s", key)
		}
	}
	// T6: the file is writable by a session between stories, and this section
	// lets a gate wait for fewer reviewers. The page says so.
	if !strings.Contains(section, "fewer") && !strings.Contains(section, "loosen") {
		t.Error("the staffing section does not say that turning it on has a gate wait for fewer reviewers")
	}
	// AC-10: absent is off, and the page says what off means.
	if !strings.Contains(section, "off") {
		t.Error("the staffing section does not say the staffing is off unless the project turns it on")
	}
	// Decision 11, in plain words (T1): the architect can be left out and
	// nothing brings it back at the design review, where there is no change to
	// read; perf can be left out, and bring_back is all that returns it.
	if !regexp.MustCompile(`architect[^.]*\.?[^.]*design review`).MatchString(section) &&
		!regexp.MustCompile(`design review[^.]*\.?[^.]*architect`).MatchString(section) {
		t.Error("the staffing section does not say that nothing brings the architect back at the design review")
	}
	if !strings.Contains(section, "perf") {
		t.Error("the staffing section does not say that perf can be left out and only bring_back returns it")
	}
	// What init writes is unchanged, so the example at the top of the page has
	// no staffing section (TestTheConfigurationExampleIsWhatInitWrites holds
	// the example to init).
	_, rest, _ := strings.Cut(page(t, "configuration.md"), "```json\n")
	example, _, _ := strings.Cut(rest, "```")
	if strings.Contains(example, "staffing") {
		t.Error("the example of what init writes has a staffing section, and init writes none")
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
		{"docs/agents.md", page(t, "agents.md"), "Twelve agents"},
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
