package cli

import (
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// Whether a command line asks for help or runs the command is the command
// tree's to say, and the shell rules have to agree with it: a line they take
// for help and the tree runs is a person's decision made by an agent. The tree
// is asked here, in a directory that is not a project, where a command that is
// reached stops at once and says so.

// helpVerb is a command, arguments that would run it, and the rule a session
// is refused it under.
type helpVerb struct {
	verb, args []string
	rule       string
}

// helpPersons are the commands only a person runs.
var helpPersons = []helpVerb{
	{[]string{"approve"}, []string{"A-1"}, "approval-is-a-human-decision"},
	{[]string{"unfreeze"}, []string{"--reason", "x"}, "unfreeze-is-a-human-decision"},
	{[]string{"ack"}, []string{"--through", "HEAD"}, "acknowledgement-is-a-human-decision"},
	{[]string{"release"}, []string{"A-1", "--reason", "x"}, "release-is-a-human-decision"},
	{[]string{"parallel"}, []string{"A-1", "B-2"}, "parallel-is-a-human-decision"},
	{[]string{"stop"}, nil, "stop-is-a-human-decision"},
	{[]string{"staffing", "assign"}, []string{"security", "--model", "opus", "--reason", "x"}, "assignment-is-a-human-decision"},
}

// helpManagers are commands the project manager may not run, since it only
// staffs.
var helpManagers = []helpVerb{
	{[]string{"gate"}, []string{"analysis", "pass"}, "project-manager-only-staffs"},
	{[]string{"escalate"}, []string{"spec_unclear", "--message", "x"}, "project-manager-only-staffs"},
	{[]string{"freeze"}, nil, "project-manager-only-staffs"},
	{[]string{"start"}, nil, "project-manager-only-staffs"},
	{[]string{"cost", "add"}, []string{"--usd", "1"}, "project-manager-only-staffs"},
}

// Shapes of a command line with a help flag in it. VERB and ARGS are filled
// in from the command.
var (
	helpAsked = [][]string{
		{"VERB", "--help"}, {"VERB", "-h"}, {"VERB", "ARGS", "--help"}, {"VERB", "ARGS", "-h"},
		{"VERB", "--help", "ARGS"}, {"VERB", "-h", "ARGS"}, {"VERB", "--json", "--help"},
		{"--json", "VERB", "--help"}, {"VERB", "--help", "--json"}, {"VERB", "--help", "--help"},
	}
	// A later value turns an earlier help flag off.
	helpCancelled = [][]string{
		{"VERB", "--help", "--help=false", "ARGS"}, {"VERB", "-h", "-h=false", "ARGS"},
		{"VERB", "--help", "-h=false", "ARGS"}, {"VERB", "-h", "--help=0", "ARGS"},
		{"VERB", "ARGS", "--help", "--help=false"}, {"VERB", "--help", "ARGS", "--help=false"},
	}
	// The flag is off from the start, is the value of another flag, or is
	// not a flag at all.
	helpNeverOn = [][]string{
		{"VERB", "ARGS"},
		{"VERB", "--help=false", "ARGS"}, {"VERB", "--help=0", "ARGS"}, {"VERB", "-h=false", "ARGS"},
		{"VERB", "--help=f", "ARGS"}, {"VERB", "--help=F", "ARGS"}, {"VERB", "--help=False", "ARGS"},
		{"VERB", "--help=FALSE", "ARGS"}, {"VERB", "--help=true", "--help=false", "ARGS"},
		{"VERB", "ARGS", "--", "--help"}, {"VERB", "--", "--help"}, {"VERB", "--", "-h"},
		{"VERB", "help"}, {"VERB", "ARGS", "help"},
		{"VERB", "--reason", "--help"}, {"VERB", "--reject", "--help"}, {"VERB", "--through", "--help"},
		{"VERB", "--note", "--help"}, {"VERB", "--message", "-h"}, {"VERB", "--model", "--help"},
	}
)

func (v helpVerb) line(shape []string) []string {
	var args []string
	for _, word := range shape {
		switch word {
		case "VERB":
			args = append(args, v.verb...)
		case "ARGS":
			args = append(args, v.args...)
		default:
			args = append(args, word)
		}
	}
	return args
}

const (
	treePrintsHelp = iota
	treeRunsIt
	treeRefusesTheLine
)

// askTheTree runs a command line in a directory that is not a project. Help is
// the usage on standard output and nothing wrong; a command that is reached
// says the project is not set up; anything else is the tree refusing the line,
// which neither helps nor runs.
func askTheTree(t *testing.T, args []string) int {
	t.Helper()
	project(t)
	r := run(t, args...)
	switch {
	case r.code == 0 && strings.Contains(r.stdout, "Usage:"):
		return treePrintsHelp
	case strings.Contains(r.stderr, "has not been set up for the loop"):
		return treeRunsIt
	}
	return treeRefusesTheLine
}

func helpState(agent string) shellpolicy.State {
	return shellpolicy.State{CommitWhy: "code_review has not passed", Backlog: "user_stories.json", Agent: agent}
}

// helpRows asks the tree about every shape for every command and hands on
// those it answers as wanted. Fewer than atLeast is a test that has stopped
// asking anything, as it would if the tree's words for "not a project"
// changed.
func helpRows(t *testing.T, verbs []helpVerb, shapes [][]string, want, atLeast int, each func(t *testing.T, v helpVerb, command string)) {
	t.Helper()
	asked := 0
	for _, v := range verbs {
		for _, shape := range shapes {
			args := v.line(shape)
			if askTheTree(t, args) != want {
				continue
			}
			asked++
			command := "sdlc " + strings.Join(args, " ")
			t.Run(command, func(t *testing.T) { each(t, v, command) })
		}
	}
	if asked < atLeast {
		t.Fatalf("the command tree answered %d of these lines as the test expects, fewer than %d, so the shell rules were barely asked", asked, atLeast)
	}
}

func helpAllowed(agent string) func(*testing.T, helpVerb, string) {
	return func(t *testing.T, _ helpVerb, command string) {
		t.Helper()
		if f, ok := shellpolicy.Inspect(command, helpState(agent)); ok {
			t.Errorf("the command tree answers %q with help alone, and it is refused under %s", command, f.Rule)
		}
		for _, waits := range []bool{true, false} {
			if f, ok := shellpolicy.HumanDecisions(command, false, func(string) bool { return waits }); ok {
				t.Errorf("the command tree answers %q with help alone, and HumanDecisions (a story waits: %v) reports %s", command, waits, f.Rule)
			}
		}
	}
}

func helpRefused(agent string, bothEntryPoints bool) func(*testing.T, helpVerb, string) {
	return func(t *testing.T, v helpVerb, command string) {
		t.Helper()
		f, ok := shellpolicy.Inspect(command, helpState(agent))
		if !ok || f.Rule != v.rule {
			t.Errorf("the command tree runs %q, and Inspect answered %q, %v; want a refusal under %s", command, f.Rule, ok, v.rule)
		}
		// A stop is refused to the session that holds the story, which
		// only Inspect knows.
		if bothEntryPoints && v.rule != "stop-is-a-human-decision" {
			f, ok := shellpolicy.HumanDecisions(command, false, func(string) bool { return true })
			if !ok || f.Rule != v.rule {
				t.Errorf("the command tree runs %q, and HumanDecisions answered %q, %v with a story waiting; want %s", command, f.Rule, ok, v.rule)
			}
		}
	}
}

func TestAC3WhatTheCommandTreeAnswersWithHelpAloneIsNotRefusedAsTheCommand(t *testing.T) {
	helpRows(t, helpPersons, helpAsked, treePrintsHelp, len(helpPersons)*len(helpAsked), helpAllowed(""))
}

func TestAC5WhatTheCommandTreeRunsDespiteAHelpFlagIsStillRefused(t *testing.T) {
	shapes := append(append([][]string{}, helpCancelled...), helpNeverOn...)
	helpRows(t, helpPersons, shapes, treeRunsIt, len(helpPersons)*15, helpRefused("", true))
}

func TestAC3HelpTheProjectManagerMayAlreadyReadIsStillAllowed(t *testing.T) {
	helpRows(t, helpManagers, helpAsked, treePrintsHelp, len(helpManagers)*len(helpAsked), helpAllowed("project-manager"))
}

func TestAC3AC5WhatTheCommandTreeRunsBehindAHelpFlagTurnedOffIsRefusedToTheProjectManager(t *testing.T) {
	helpRows(t, helpManagers, helpCancelled, treeRunsIt, len(helpManagers)*len(helpCancelled), helpRefused("project-manager", false))
}

func TestAC5WhatTheCommandTreeRunsWithHelpNeverOnIsStillRefusedToTheProjectManager(t *testing.T) {
	helpRows(t, helpManagers, helpNeverOn, treeRunsIt, len(helpManagers)*9, helpRefused("project-manager", false))
}

// --- AC-4: the page that quotes the rule's route ----------------------------

// docs/enforcement.md says what to do instead for every rule, and a person
// reads it when the hook's own words were not enough. It is held to what the
// route is held to and no more: it names the story's directory, and it does
// not offer the shell.
func TestAC4TheEnforcementPageSaysWhereAFileThatIsNotTheStorysWorkGoes(t *testing.T) {
	doc := page(t, "enforcement.md")
	_, section, found := strings.Cut(doc, "\n### `orchestrator-delegates`\n")
	if !found {
		t.Fatal("docs/enforcement.md has no section for orchestrator-delegates")
	}
	if next := strings.Index(section, "\n### "); next >= 0 {
		section = section[:next]
	}
	if next := strings.Index(section, "\n## "); next >= 0 {
		section = section[:next]
	}
	_, instead, found := strings.Cut(section, "*Instead:*")
	if !found {
		t.Fatal("the section does not say what to do instead")
	}
	if !strings.Contains(instead, ".sdlc/stories/") {
		t.Errorf("the page does not name the story's own directory as where such a file goes:\n%s", instead)
	}
	lower := strings.ToLower(instead)
	for _, word := range []string{"shell", "bash", "redirect", "printf", "heredoc", "here-doc", "cat >", "echo ", "tee "} {
		if strings.Contains(lower, word) {
			t.Errorf("the page offers the shell (%q) as the way to write the file:\n%s", word, instead)
		}
	}
}
