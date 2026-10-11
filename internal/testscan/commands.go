package testscan

import (
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// hookCommand is the one command main answers before the command tree is reached.
const hookCommand = "hook"

// Commands is every command a person can run, as typed after "sdlc": the tree
// cli.New builds, groups included, without hidden commands, help or completion
// (what internal/cli/docs_test.go walks), and hook, which main answers before
// the tree is reached.
func Commands() []string {
	out := []string{hookCommand}
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if !sub.Hidden && sub.Name() != "help" && sub.Name() != "completion" {
				out = append(out, strings.TrimPrefix(sub.CommandPath(), "sdlc "))
				walk(sub)
			}
		}
	}
	walk(tree())
	slices.Sort(out)
	return out
}

// Resolve is the command that arguments after "sdlc" run: the deepest command
// in the tree they name, "hook" for arguments that begin with it, and false for
// none. A single argument holding a whole command line names nothing, which is
// how a payload handed to the hook is kept from counting as a run.
func Resolve(args []string) (string, bool) {
	return resolve(tree(), args)
}

func resolve(root *cobra.Command, args []string) (string, bool) {
	if len(args) > 0 && args[0] == hookCommand {
		return hookCommand, true
	}
	found, _, err := root.Find(args)
	if err != nil || found == root {
		return "", false
	}
	return strings.TrimPrefix(found.CommandPath(), "sdlc "), true
}

// Unrun is what makes the guard fail: the commands in neither ran nor gaps, in
// the order of Commands, then the known gaps that do not hold, in the order of
// the command each names.
func Unrun(ran map[string]bool, gaps map[string]string) []string {
	all := Commands()
	var out []string
	for _, c := range all {
		if _, gap := gaps[c]; !ran[c] && !gap {
			out = append(out, "sdlc "+c+": no e2e case ran it to the result it expected")
		}
	}
	for _, c := range slices.Sorted(maps.Keys(gaps)) {
		switch {
		case !slices.Contains(all, c):
			out = append(out, "known gap "+c+": no such command")
		case strings.TrimSpace(gaps[c]) == "":
			out = append(out, "sdlc "+c+": listed as a known gap without a reason")
		case ran[c]:
			out = append(out, "sdlc "+c+": listed as a known gap, but a case ran it, so the gap can go")
		}
	}
	return out
}
