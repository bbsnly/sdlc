package shellpolicy

// US-029.3 (PLAN.md step 12, OQ-3): what the classifier makes of a rebase,
// and where the new rule stands among the others.
//
// After the commit gate, a held session may rebase its own checkout onto
// trunk's branch, and only that: trunk named by its full branch ref, or by a
// short name IsTrunk resolves to that ref, optionally quiet, optionally with
// a -C into its own checkout. Anything else that rebases is refused under
// branch-through-the-tool; before the commit gate a rebase is refused; and
// where the state cannot say what trunk is, the rebase by short name is
// refused.
//
// State's three new fields -- Committed bool, Trunk func() string and
// IsTrunk func(string) bool -- are set by name through reflection, so that
// the rest of this package's tests run before they exist, and a missing one
// fails here and says which.
//
// Helpers carry an rb prefix.

import (
	"reflect"
	"strings"
	"testing"
)

const rbRule = "branch-through-the-tool"

// rbSet sets a State field the story adds.
func rbSet(t *testing.T, s *State, name string, value any) {
	t.Helper()
	f := reflect.ValueOf(s).Elem().FieldByName(name)
	if !f.IsValid() {
		t.Fatalf("State has no %s, which the classifier needs to tell a rebase onto trunk from another", name)
	}
	if value == nil {
		return
	}
	v := reflect.ValueOf(value)
	if !v.Type().AssignableTo(f.Type()) {
		t.Fatalf("State.%s is a %s, and a %s is what it holds", name, f.Type(), v.Type())
	}
	f.Set(v)
}

// rbTrunkIs is IsTrunk as the hook answers it where refs/heads/<trunk> is the
// only ref with trunk's name.
func rbTrunkIs(trunk string) func(string) bool {
	return func(name string) bool { return name == trunk || name == "refs/heads/"+trunk }
}

// rbState is A-1's session with the gates given passed, trunk named trunk,
// and isTrunk as the hook would answer it; a nil isTrunk leaves the field
// unset.
func rbState(t *testing.T, committed, finished bool, trunk string, isTrunk func(string) bool) State {
	t.Helper()
	s := heldInA1(State{CommitReady: true, StoryFinished: finished})
	rbSet(t, &s, "Committed", committed)
	rbSet(t, &s, "Trunk", func() string { return trunk })
	if isTrunk != nil {
		rbSet(t, &s, "IsTrunk", isTrunk)
	} else {
		rbSet(t, &s, "IsTrunk", nil)
	}
	return s
}

// The rebase the session may run, and the way out of one that stopped.
var rbAllowed = []string{
	"git rebase main",
	"git rebase refs/heads/main",
	"git rebase --quiet main",
	"git rebase -q refs/heads/main",
	"git -C /work/main/.sdlc/worktrees/A-1 rebase main",
	"cd /work/main/.sdlc/worktrees/A-1 && git rebase main",
	"git rebase --abort",
}

// Every other rebase, by what makes it another.
var rbRefused = []struct{ class, command string }{
	{"no upstream named", "git rebase"},
	{"onto something else", "git rebase --onto main HEAD~2"},
	{"onto something else", "git rebase other"},
	{"onto something else", "git rebase origin/main"},
	{"onto something else", "git rebase HEAD~1"},
	{"onto something else", "git rebase refs/tags/main"},
	{"onto something else", "git rebase refs/heads/story/B-2"},
	{"onto something else", "git rebase --root"},
	{"another branch rebased", "git rebase main story/B-2"},
	{"another branch rebased", "git rebase main story/A-1"},
	{"an option", "git rebase -i main"},
	{"an option", "git rebase --interactive main"},
	{"an option", "git rebase --exec true main"},
	{"an option", "git rebase -x true main"},
	{"an option", "git rebase --autostash main"},
	{"an option", "git rebase --no-verify main"},
	{"an option", "git rebase --strategy=ours main"},
	{"an option", "git rebase -Xtheirs main"},
	{"an option", "git rebase --update-refs main"},
	{"a setting or alias", "git -c core.hooksPath=/tmp/h rebase main"},
	{"a setting or alias", "git -c rebase.updateRefs=true rebase main"},
	{"a setting or alias", "git -c alias.rb=rebase rb main"},
	{"a setting or alias", "git --git-dir=/work/main/.git rebase main"},
	{"a setting or alias", "GIT_DIR=/work/main/.git git rebase main"},
	{"another checkout", "git -C /work/main rebase main"},
	{"another checkout", "cd /work/main && git rebase main"},
	{"another checkout", "git -C /work/main/.sdlc/worktrees/B-2 rebase main"},
}

// OQ-3: after the commit gate -- the retro to do, or done -- the plain
// rebase onto trunk goes through, and nothing else that rebases does.
func TestAC9AndQ3AfterTheCommitGateOnlyTheRebaseOntoTrunkGoesThrough(t *testing.T) {
	for _, finished := range []bool{false, true} {
		name := map[bool]string{false: "Q3 committed", true: "Q3 finished"}[finished]
		t.Run(name, func(t *testing.T) {
			s := rbState(t, true, finished, "main", rbTrunkIs("main"))
			for _, command := range rbAllowed {
				t.Run(command, func(t *testing.T) { allowed(t, command, s) })
			}
			for _, row := range rbRefused {
				t.Run(row.class+": "+row.command, func(t *testing.T) { wtRefusedAs(t, row.command, s, rbRule) })
			}
		})
	}
}

// OQ-3: trunk is what the state says it is, not "main".
func TestAC9AndQ3TheRebaseIsOntoTheTrunkTheStateNames(t *testing.T) {
	s := rbState(t, true, true, "develop", rbTrunkIs("develop"))
	allowed(t, "git rebase develop", s)
	allowed(t, "git rebase refs/heads/develop", s)
	wtRefusedAs(t, "git rebase main", s, rbRule)
	wtRefusedAs(t, "git rebase refs/heads/main", s, rbRule)
}

// Red team gaming row 7: where the short name is not trunk's branch ref --
// a tag or another ref has the name -- or where the state cannot say, the
// rebase by short name is refused, and by the full ref it is not.
func TestAC9AndQ3AShortNameIsTrunkOnlyWhereItResolvesToTrunksBranch(t *testing.T) {
	tagged := rbState(t, true, true, "main", func(name string) bool { return name == "refs/heads/main" })
	wtRefusedAs(t, "git rebase main", tagged, rbRule)
	allowed(t, "git rebase refs/heads/main", tagged)

	unknown := rbState(t, true, true, "main", nil)
	wtRefusedAs(t, "git rebase main", unknown, rbRule)
}

// OQ-3: before the commit gate there is nothing of the story's on its branch
// to rebase, and the rebase is refused; before the gates, by the commit gate.
func TestAC9AndQ3BeforeTheCommitGateTheRebaseIsRefused(t *testing.T) {
	wtRefusedAny(t, "git rebase main", rbState(t, false, false, "main", rbTrunkIs("main")))

	before := heldInA1(notReady)
	rbSet(t, &before, "Trunk", func() string { return "main" })
	rbSet(t, &before, "IsTrunk", rbTrunkIs("main"))
	refused(t, "git rebase main", before, "commit-gate")
}

// PLAN.md step 12: the new rule answers last. A command another rule refuses
// keeps that rule's name, whichever comes first in it.
func TestAC9TheBranchRuleAnswersAfterEveryOtherRule(t *testing.T) {
	s := rbState(t, true, true, "main", rbTrunkIs("main"))
	for _, command := range []string{
		"git branch -f main HEAD; sdlc approve A-1",
		"sdlc approve A-1; git branch -f main HEAD",
		"git update-ref refs/heads/main HEAD && sdlc unfreeze",
	} {
		f, ok := Inspect(command, s)
		if !ok {
			t.Errorf("allowed: %s", command)
			continue
		}
		if f.Rule == rbRule {
			t.Errorf("%s was refused by %s, which answers only where no other rule does", command, f.Rule)
		}
	}
	wtRefusedAs(t, "git branch -f main HEAD", s, rbRule)
}

// Perf 1: once a branch move is found, reading more of the command is
// bounded; a command made long to wear the reading out is still refused, and
// refused for the branch move, wherever in it the move is.
func TestAC9ALongCommandAroundABranchMoveIsStillRefusedForIt(t *testing.T) {
	s := rbState(t, true, true, "main", rbTrunkIs("main"))
	filler := strings.Repeat("git -c a.b=c -c d.e=f status --short; ", 5000)
	for _, command := range []string{
		"git branch -f main HEAD; " + filler,
		filler + "git branch -f main HEAD",
		"sh -c 'git branch -f main HEAD'; " + filler,
	} {
		f, ok := Inspect(command, s)
		if !ok {
			t.Errorf("a %d-byte command moving main was allowed", len(command))
			continue
		}
		if f.Rule != rbRule {
			t.Errorf("a %d-byte command moving main was refused by %s, want %s", len(command), f.Rule, rbRule)
		}
	}
}
