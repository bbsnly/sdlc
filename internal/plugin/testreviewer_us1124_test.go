package plugin

// US-112.4: the test reviewer's definition (AC-1, AC-6), the runbook's Gate 3
// (AC-5) and the pages that describe it (AC-10). These hold the meaning in the
// passage that is about it, not a sentence, so each may be reworded freely as
// long as it still says so. The contract tests hold the definition to the shape
// every agent shares; these hold what is this agent's own.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const trName = "test-reviewer"

// trFold is text in lower case with its white space collapsed.
func trFold(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func trAgent(t *testing.T) Component {
	t.Helper()
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.Name == trName {
			return a
		}
	}
	t.Fatalf("plugin/agents ships no %s.md", trName)
	return Component{}
}

// trPage is a page of the repository, with Windows line ends made Unix ones.
func trPage(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

// trSection is the part of doc under the heading that starts with heading, down
// to the next heading of its level or a higher one.
func trSection(t *testing.T, doc, heading string) string {
	t.Helper()
	level := len(heading) - len(strings.TrimLeft(heading, "#"))
	at := strings.Index("\n"+doc, "\n"+heading)
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

// ---------------------------------------------------------------------- AC-1

func TestAC1TheTestReviewerShipsAsAReviewerOfGate3BlockingAndOnByDefault(t *testing.T) {
	a := trAgent(t)
	if got := a.Front["sdlc-kind"]; got != "reviewer" {
		t.Errorf("AC-1 sdlc-kind is %q, want reviewer", got)
	}
	if got := strings.TrimSpace(a.Front["sdlc-gates"]); got != "tests_frozen" {
		t.Errorf("AC-1 sdlc-gates is %q, want tests_frozen alone", got)
	}
	var team map[string]any
	if err := json.Unmarshal([]byte(a.Front["sdlc-team"]), &team); err != nil {
		t.Fatalf("AC-1 sdlc-team is not JSON: %v", err)
	}
	if team["enabled"] != true || team["can_block"] != true || team["instances"] != float64(1) || len(team) != 3 {
		t.Errorf("AC-1 sdlc-team is %v, want enabled and can_block true, instances 1", team)
	}
}

// ---------------------------------------------------------------------- AC-6

func TestAC6TheTestReviewerSaysWhatItBlocksOnAndThatItRunsTheTestsAndEditsNone(t *testing.T) {
	a := trAgent(t)
	body := trFold(a.Body)
	for _, c := range []struct {
		what string
		any  []string
	}{
		{"a test that contradicts a criterion", []string{"contradict"}},
		{"one that cannot pass under a criterion as written", []string{"cannot pass"}},
		{"a criterion with no test that would fail without the behaviour", []string{"no test"}},
		{"a test that passes for the wrong reason", []string{"wrong reason"}},
		{"one the code already satisfies", []string{"already does", "already satisf"}},
		{"one a stub satisfies", []string{"stub"}},
		{"the clock", []string{"clock"}},
		{"the network", []string{"network"}},
		{"an order", []string{"order"}},
		{"the developer's own git configuration", []string{"git configuration"}},
		{"against the project's test conventions", []string{"test conventions"}},
		{"a criterion id missing from the test names", []string{"criterion id", "ac-n"}},
		{"that it runs the tests", []string{"run them", "runs them", "run the tests", "commands.test"}},
	} {
		found := false
		for _, word := range c.any {
			found = found || strings.Contains(body, word)
		}
		if !found {
			t.Errorf("AC-6 the definition does not say %s (any of %q)", c.what, c.any)
		}
	}
	_, never, _ := strings.Cut(a.Body, "\n## Never")
	never, _, _ = strings.Cut(never, "\n## ")
	if !strings.Contains(trFold(never), "test") {
		t.Errorf("AC-6 the Never section does not say the test reviewer edits no test:\n%s", never)
	}
	for _, tool := range regexp.MustCompile(`[A-Za-z]+`).FindAllString(a.Front["tools"], -1) {
		switch tool {
		case "Edit", "Write", "MultiEdit", "NotebookEdit":
			t.Errorf("AC-6 the test reviewer is given %s, and it edits no test", tool)
		}
	}
}

// ---------------------------------------------------------------------- AC-5

// trGate3 is the runbook's Gate 3 section with its lines joined.
func trGate3(t *testing.T) string {
	t.Helper()
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		if s.Name == "next" {
			for _, section := range regexp.MustCompile(`(?m)^## `).Split(s.Body, -1) {
				if strings.HasPrefix(section, "Gate 3") {
					return strings.Join(strings.Fields(section), " ")
				}
			}
			t.Fatal("the next runbook has no Gate 3 section")
		}
	}
	t.Fatal("there is no next skill")
	return ""
}

func TestAC5Gate3DelegatesToTheTestReviewerBeforeTheFreezeAndSendsABlockBackToTheTestAuthor(t *testing.T) {
	t.Parallel()
	section := trGate3(t)
	delegated := strings.Index(section, "sdlc:test-reviewer")
	frozen := strings.Index(section, "sdlc freeze")
	if delegated < 0 || frozen < 0 || delegated > frozen {
		t.Fatalf("AC-5 Gate 3 does not delegate to sdlc:test-reviewer before it runs sdlc freeze:\n%s", section)
	}
	for _, c := range []struct{ what, word string }{
		{"what the reviewer is given", "TEST-PLAN.md"},
		{"what the reviewer is given", "ANALYSIS.md"},
		{"what the reviewer is given", "THREATS.md"},
		{"what the reviewer is given", "acceptance criteria"},
		{"where it is staffed", "staffed"},
		{"what a block does", "block"},
		{"who corrects the tests", "sdlc:sdet"},
		{"the rounds", "max_rounds"},
		{"the rounds where it has none", "loop.max_review_rounds"},
	} {
		if !strings.Contains(section, c.word) {
			t.Errorf("AC-5 Gate 3 of the runbook, on %s, does not say %q:\n%s", c.what, c.word, section)
		}
	}
	after := section[delegated:]
	if !strings.Contains(after, "sdlc:sdet") || !strings.Contains(after, "TEST-PLAN.md") {
		t.Errorf("AC-5 after the test reviewer, Gate 3 does not send its findings to sdlc:sdet to correct the tests and TEST-PLAN.md:\n%s", after)
	}
}

// ---------------------------------------------------------------------- AC-10

func TestAC10ThePagesDescribeTheTestReviewerAndThatTheFreezeWaitsForIt(t *testing.T) {
	t.Parallel()
	agents := trPage(t, "docs/the-agents.md")
	row := regexp.MustCompile("(?m)^\\| `sdlc:test-reviewer` \\| (?:Gate )?3 \\| [^|]*Yes[^|]*\\|").FindString(agents)
	if row == "" {
		t.Error("AC-10 docs/the-agents.md has no row for sdlc:test-reviewer at Gate 3 that says it can block")
	}
	for _, c := range []struct{ page, heading string }{
		{"docs/the-loop.md", "### Gate 3: freeze the tests"},
		{"docs/commands.md", "## `sdlc freeze`"},
	} {
		section := trFold(trSection(t, trPage(t, c.page), c.heading))
		if !strings.Contains(section, "test-reviewer") || !strings.Contains(section, "wait") || !strings.Contains(section, "staffed") {
			t.Errorf("AC-10 %s, under %s, does not say sdlc freeze waits for the test reviewer where it is staffed:\n%s", c.page, c.heading, section)
		}
	}
	config := trPage(t, "docs/configuration.md")
	entry := regexp.MustCompile(`"test-reviewer":\s*\{\s*"enabled":\s*true,\s*"can_block":\s*true,\s*"instances":\s*1\s*\}`)
	if !entry.MatchString(config) {
		t.Error("AC-10 docs/configuration.md lists no team entry for test-reviewer with its defaults")
	}
	if !strings.Contains(trFold(trSection(t, config, "### An agent's entry")), "test reviewer") {
		t.Error("AC-10 docs/configuration.md's agent entry does not say what can_block is for the test reviewer by default")
	}
}
