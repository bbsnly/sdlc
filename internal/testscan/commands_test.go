package testscan

// US-069 AC-1, the half of it that is not a test run: which commands there are
// to be run, which command a list of arguments runs, and what is said when a
// command was not run. test/e2e records what it ran with Resolve and hands the
// record to Unrun; that guard is test/e2e/zcommands_test.go.
//
// What these bind, and the implementation has to provide, in package testscan
// (import path github.com/bbsnly/sdlc/internal/testscan):
//
//	func Commands() []string
//	    Every command a person can run, as a person types it after "sdlc":
//	    "approve", "cost add", "staffing assign". It is the tree cli.New builds,
//	    groups included (story, artifact, review and staffing run their own
//	    help), without hidden commands, "help" or "completion", which is what
//	    internal/cli/docs_test.go walks; and "hook", which main.go answers
//	    before the tree is reached. Sorted, each once.
//
//	func Resolve(args []string) (string, bool)
//	    The command that arguments after "sdlc" run: the deepest command in the
//	    tree they name, "hook" for arguments that begin with it, and false for
//	    arguments that name none. Flags and operands after the command's words
//	    do not change the answer. A single argument that holds a whole command
//	    line, as a payload handed to the hook does, names nothing.
//
//	func Unrun(ran map[string]bool, gaps map[string]string) []string
//	    The lines that make the guard fail, for commands that ran and the known
//	    gaps (command -> reason). In this order: one line for each command in
//	    Commands() that is in neither (in Commands() order), then one line for
//	    each gap that does not hold (in order of the gap's command):
//
//	      "sdlc <command>: no e2e case ran it to the result it expected"
//	      "sdlc <command>: listed as a known gap, but a case ran it, so the gap can go"
//	      "sdlc <command>: listed as a known gap without a reason"
//	      "known gap <command>: no such command"
//
//	    Nothing is returned when every command ran or is a gap that holds.

import (
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/bbsnly/sdlc/internal/cli"
)

// treeCommands walks the tree the way internal/cli/docs_test.go does, so that a
// command added tomorrow is in the answer without anybody editing this test.
func treeCommands() []string {
	var out []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			out = append(out, strings.TrimPrefix(sub.CommandPath(), "sdlc "))
			walk(sub)
		}
	}
	walk(cli.New(strings.NewReader(""), io.Discard, io.Discard))
	out = append(out, "hook")
	slices.Sort(out)
	return out
}

func TestAC1TheCommandsToRunAreTheTreeAPersonSeesAndTheHook(t *testing.T) {
	got, want := Commands(), treeCommands()
	if !slices.Equal(got, want) {
		t.Errorf("Commands() =\n%q\nwant the command tree, groups included, and hook:\n%q", got, want)
	}
	for _, must := range []string{"hook", "cost", "cost add", "staffing assign", "story"} {
		if !slices.Contains(got, must) {
			t.Errorf("Commands() has no %q", must)
		}
	}
	for _, never := range []string{"", "sdlc", "help", "completion"} {
		if slices.Contains(got, never) {
			t.Errorf("Commands() has %q", never)
		}
	}
}

func TestAC1ArgumentsResolveToTheCommandTheyRunAndAMentionResolvesToNothing(t *testing.T) {
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"approve", "US-001", "--reject", "the migration has no way back"}, "approve"},
		{[]string{"gate", "dor", "pass", "--note", "ready"}, "gate"},
		{[]string{"staffing", "assign", "security", "--model", "opus"}, "staffing assign"},
		{[]string{"cost"}, "cost"},
		{[]string{"cost", "add", "--usd", "2.40"}, "cost add"},
		{[]string{"status", "--json"}, "status"},
		{[]string{"story"}, "story"},
		{[]string{"hook", "PreToolUse"}, "hook"},
	} {
		if got, ok := Resolve(c.args); got != c.want || !ok {
			t.Errorf("Resolve(%q) = %q, %v; want %q, true", c.args, got, ok, c.want)
		}
	}
	for _, args := range [][]string{
		nil,
		{"sdlc approve US-001"},
		{"approve US-001"},
		{"nosuchcommand"},
		{"--json"},
		{"help"},
		{"completion"},
	} {
		if got, ok := Resolve(args); ok {
			t.Errorf("Resolve(%q) = %q, true; want it to name no command", args, got)
		}
	}
}

func TestAC1AnUnrunCommandIsNamedAndAGapThatNoLongerHoldsIsToo(t *testing.T) {
	everything := func(except ...string) map[string]bool {
		ran := map[string]bool{}
		for _, c := range Commands() {
			ran[c] = !slices.Contains(except, c)
		}
		return ran
	}
	const missing = ": no e2e case ran it to the result it expected"

	for _, c := range []struct {
		name string
		ran  map[string]bool
		gaps map[string]string
		want []string
	}{
		{"every command ran", everything(), nil, nil},
		{"commands that did not run are each named, in the order of the tree",
			everything("cost add", "approve"), nil,
			[]string{"sdlc approve" + missing, "sdlc cost add" + missing}},
		{"a command in a group that ran is not covered by the group",
			everything("staffing assign"), nil,
			[]string{"sdlc staffing assign" + missing}},
		{"a known gap with its reason is not named",
			everything("story"), map[string]string{"story": "prints its help only"}, nil},
		{"a gap for a command that ran has outlived its cause",
			everything(), map[string]string{"story": "prints its help only"},
			[]string{"sdlc story: listed as a known gap, but a case ran it, so the gap can go"}},
		{"a gap needs a reason, and has to name a command",
			everything("review", "story"),
			map[string]string{"review": "", "nosuchcommand": "it was removed", "story": "prints its help only"},
			[]string{"known gap nosuchcommand: no such command", "sdlc review: listed as a known gap without a reason"}},
		{"a command that did not run and is no gap comes before the gaps that do not hold",
			everything("freeze", "story"),
			map[string]string{"story": "prints its help only", "nosuchcommand": "x"},
			[]string{"sdlc freeze" + missing, "known gap nosuchcommand: no such command"}},
	} {
		if got := Unrun(c.ran, c.gaps); !slices.Equal(got, c.want) {
			t.Errorf("%s:\ngot  %q\nwant %q", c.name, got, c.want)
		}
	}
}
