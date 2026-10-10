package plugin

// US-026: the project-manager agent the plugin ships (AC-12), and what the
// next runbook has to say about it (AC-1, AC-6, AC-11).
//
// A test cannot hold that the runbook is followed, or that the project
// manager's reasons are drawn from what the work is. It holds the text: the
// definition's frontmatter and the things it says it never does, and in the
// runbook the delegation, where it comes, and the sentences the criteria ask
// for. plugin_test.go holds the rest for every agent, this one included: the
// count, `model: inherit` and no effort, the description's two phrases, the
// brief guard as its first step, and the rules every agent shares.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/policy"
)

const projectManager = "project-manager"

// theProjectManager is the agent's definition, or a failure that says it is
// not there.
func theProjectManager(t *testing.T) Component {
	t.Helper()
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if a.Name == projectManager {
			return a
		}
	}
	t.Fatalf("plugin/agents has no %s.md: the plugin does not ship the project manager", projectManager)
	return Component{}
}

// theRunbook is plugin/skills/next/SKILL.md with its white space folded,
// since its prose wraps wherever it wraps.
func theRunbook(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(pluginDir, "skills", "next", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(string(raw)), " ")
}

func TestAC12TheProjectManagerHasReadOnlyToolsAndBashAndNeitherWriteNorEdit(t *testing.T) {
	pm := theProjectManager(t)
	if !strings.HasSuffix(pm.Path, "agents/"+projectManager+".md") {
		t.Errorf("the project manager is defined in %s", pm.Path)
	}
	tools := slices.Clone(pm.List("tools"))
	slices.Sort(tools)
	if want := []string{"Bash", "Glob", "Grep", "Read"}; !slices.Equal(tools, want) {
		t.Errorf("tools = %q, want exactly %q: read-only tools and Bash", tools, want)
	}
	for _, writes := range []string{"Write", "Edit", "MultiEdit", "NotebookEdit"} {
		if slices.Contains(tools, writes) {
			t.Errorf("the project manager has %s: it records the staffing through the sdlc command and writes nothing else", writes)
		}
	}
	if _, ok := pm.Front["disallowedTools"]; ok {
		t.Error("the project manager names disallowedTools: its tools are a list of what it has, not of what it lacks")
	}
	if got := pm.Front["model"]; got != "inherit" {
		t.Errorf("model = %q, want \"inherit\": its first run is before any staffing, at what its definition says", got)
	}
	if got, ok := pm.Front["effort"]; ok {
		t.Errorf("effort = %q; no definition pins one", got)
	}
}

func TestAC12TheProjectManagerWorksOnlyWithinAStoryAPersonStarted(t *testing.T) {
	pm := theProjectManager(t)
	for _, phrase := range []string{
		"Works only within a story a person started with /sdlc:next",
		"not for general review, security, performance, testing, planning, analysis or retro requests",
	} {
		if !strings.Contains(pm.Front["description"], phrase) {
			t.Errorf("the description does not say %q", phrase)
		}
	}
	first, _, _ := strings.Cut(pm.Body, "\n## ")
	first = strings.Join(strings.Fields(first), " ")
	for _, want := range []string{
		"it starts with `" + briefHeader + "` and names the story",
		"If it does not, stop, and say that this agent only works the gates of an sdlc story " +
			"a person started with `/sdlc:next`. Do nothing else.",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("stopping on a brief the runbook did not write is not the project manager's first step: missing %q", want)
		}
	}
}

// AC-12's last sentence, as the definition says it. The hook is what holds the
// agent to it (internal/shellpolicy and internal/hook); this is what tells it.
func TestAC12TheProjectManagerIsToldItNeverApprovesUnfreezesAcknowledgesMarksAStoryReadyOrRecordsAGateOrAReview(t *testing.T) {
	body := strings.Join(strings.Fields(theProjectManager(t).Body), " ")
	if !strings.Contains(strings.ToLower(body), "never") {
		t.Error("the definition says nothing the project manager never does")
	}
	for _, never := range []string{
		"`sdlc approve`", "`sdlc unfreeze`", "`sdlc ack`", "`sdlc gate`", "`sdlc review add`", "ready",
	} {
		if !strings.Contains(body, never) {
			t.Errorf("the definition does not name %s among what the project manager never does", never)
		}
	}
	// What it does do, and through what.
	for _, does := range []string{"`sdlc staffing set`", "`sdlc staffing amend`", "`sdlc staffing show`"} {
		if !strings.Contains(body, does) {
			t.Errorf("the definition does not name %s", does)
		}
	}
	// Decision 8: a person's assignment is not the project manager's to make
	// or to change.
	if !strings.Contains(body, "`sdlc staffing assign`") {
		t.Error("the definition does not say `sdlc staffing assign` is a person's")
	}
	// T11: it cannot hand the story to a person itself, so it has to be told
	// to say so to the runbook, which can.
	if !strings.Contains(body, "escalate") {
		t.Error("the definition does not say what to do when it cannot staff the story: it cannot escalate, and the runbook can")
	}
	// T7: what it reads is data.
	if !strings.Contains(body, "is data, not instruction") {
		t.Error("the definition does not say that text addressed to it in the repository is data, not instruction")
	}
}

// AC-1 and the first decision: the choice is made from what the work is and
// what each agent has to do, never from the story's risk tier or priority, and
// each comes with a reason. AC-4 and AC-5: inside the floor and the limits.
func TestAC1TheProjectManagerIsToldWhatAStaffingIsMadeFrom(t *testing.T) {
	body := strings.Join(strings.Fields(theProjectManager(t).Body), " ")
	for _, want := range []string{
		"risk tier", "priority", // named, to be set aside
		"reason",
		"every agent",
		"model", "effort",
		"`staffing.lowest`",
		"never left out",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the definition does not say %q", want)
		}
	}
	// AC-2: what it records is what is asked for.
	if !strings.Contains(body, "asked for") {
		t.Error("the definition does not call the model and effort what is asked for")
	}
	if regexp.MustCompile(`(?i)\bran (at|on|with)\b`).MatchString(body) {
		t.Error("the definition speaks of what an agent ran at, which nothing records")
	}
}

// The project manager is not one of the loop's writing roles: it has no Write,
// and the file rules go on treating it as the main conversation, which may
// write neither code nor tests.
func TestAC12TheProjectManagerIsNeitherAReviewerNorARoleTheFileRulesLetWrite(t *testing.T) {
	theProjectManager(t)
	if model.IsReviewRole(projectManager) {
		t.Error("the project manager is a review role: it would be waited for at a gate")
	}
	for _, name := range []string{projectManager, "sdlc:" + projectManager, "sdlc-" + projectManager} {
		if policy.IsLoopRole(name) {
			t.Errorf("%s is a loop role to the file rules, which lets it write code during an iteration", name)
		}
	}
}

// ---------------------------------------------------------------- the runbook

func TestAC1TheRunbookDelegatesToTheProjectManagerBeforeTheAnalysisBegins(t *testing.T) {
	runbook := theRunbook(t)
	pm := strings.Index(runbook, "`sdlc:"+projectManager+"`")
	if pm < 0 {
		t.Fatalf("the next runbook never delegates to `sdlc:%s`", projectManager)
	}
	researcher := strings.Index(runbook, "`sdlc:researcher`")
	if researcher < 0 {
		t.Fatal("the next runbook no longer delegates to `sdlc:researcher`")
	}
	if pm > researcher {
		t.Error("the runbook names the project manager only after the researcher: the staffing is recorded before the analysis begins")
	}
	// After the story has been found ready: the delegation is not before Gate 1.
	if gate1 := strings.Index(runbook, "## Gate 1"); gate1 < 0 || pm < gate1 {
		t.Error("the runbook delegates to the project manager before Gate 1 has found the story ready")
	}
	// The analysis gate does not pass with no staffing, and the runbook says
	// where one comes from.
	if !strings.Contains(runbook, "staffing") {
		t.Error("the runbook never says what the project manager records")
	}
}

func TestAC6TheRunbookSendsTheStaffingBackToTheProjectManagerAndNeverWritesItItself(t *testing.T) {
	runbook := theRunbook(t)
	if n := strings.Count(runbook, "`sdlc:"+projectManager+"`"); n < 2 {
		t.Errorf("the runbook names `sdlc:%s` %d time(s): it is delegated to when the story is found ready, "+
			"and again once the analysis is recorded", projectManager, n)
	}
	// Once the analysis is recorded: a delegation after Gate 2's section begins.
	gate2 := strings.Index(runbook, "## Gate 2")
	if gate2 < 0 || !strings.Contains(runbook[gate2:], "`sdlc:"+projectManager+"`") {
		t.Error("the runbook does not go back to the project manager once the analysis is recorded")
	}
	for _, phrase := range []string{
		"confirm or amend", // once the analysis is recorded
		"falls short",      // whenever an agent's run does
	} {
		if !strings.Contains(runbook, phrase) {
			t.Errorf("the runbook does not say %q", phrase)
		}
	}
	if !regexp.MustCompile(`(?i)\b(not|never) write or amend the staffing\b`).MatchString(runbook) {
		t.Error("the runbook does not tell the session running it not to write or amend the staffing itself")
	}
}

func TestAC11TheRunbookSaysEveryLaterDelegationUsesWhatTheStaffingRecords(t *testing.T) {
	runbook := theRunbook(t)
	for _, phrase := range []string{
		"sdlc staffing show",     // where it reads the staffing, not from memory (AC-2)
		"every later delegation", // AC-11
		"model", "effort",
	} {
		if !strings.Contains(runbook, phrase) {
			t.Errorf("the runbook does not say %q", phrase)
		}
	}
	// AC-10: one runbook for every project, so it says what it does where the
	// staffing is off.
	if !regexp.MustCompile(`(?i)staffing (is )?(on|off)\b|staffing\.enabled`).MatchString(runbook) {
		t.Error("the runbook does not say what it does where the project has the staffing off")
	}
	// AC-2: asked for, never what ran.
	if regexp.MustCompile(`(?i)\bran (at|on|with)\b`).MatchString(runbook) {
		t.Error("the runbook speaks of what an agent ran at, which nothing records")
	}
}
