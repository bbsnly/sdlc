package shellpolicy

// US-109, Gate 6 unfreeze: rows for the story's own fail-closed exits that no row
// reached. Each is a reading the plan claims, held where the walk leaves the common
// path: an array literal whose `)` hides inside a substitution or an expansion, an
// alias whose value a path rule refuses, a script find hands a shell that the shell
// decodes, and a find whose program word the shell makes whole.

import (
	"strings"
	"testing"
)

// AC-8, T1: a code list written as an array literal (bash's BASH_ALIASES, zsh's
// functions) is read as code to the `)` that closes it. A `)` inside `$(...)` or
// `${...}`, or after a backslash, is not that close: read as one, the literal ended
// early and the approve in the element after it ran at its use unread.
func TestAC8ACodeListLiteralIsReadToItsCloseAcrossASubstitutionOrAnExpansion(t *testing.T) {
	for _, c := range []struct{ command, own string }{
		{"BASH_ALIASES=([x]=$(printf y) [s]='sdlc approve US-001'); s", "approval-is-a-human-decision"},
		{"BASH_ALIASES=([x]=$(echo $(printf q)) [s]='sdlc approve US-001'); s", "approval-is-a-human-decision"},
		{"BASH_ALIASES=([x]=$(printf y; echo z) [s]='sdlc approve US-001'); s", "approval-is-a-human-decision"},
		{"BASH_ALIASES=([x]=${y:-z} [s]='sdlc approve US-001'); s", "approval-is-a-human-decision"},
		{"BASH_ALIASES=([x]=${y:-)} [s]='sdlc approve US-001'); s", "approval-is-a-human-decision"},
		{"BASH_ALIASES=([x]=\\) [s]='sdlc approve US-001'); s", "approval-is-a-human-decision"},
		{"functions=([s]=$(printf y) [t]='sdlc unfreeze --reason x'); t", "unfreeze-is-a-human-decision"},
		// The shells read these otherwise than the lexer: a `;` inside, a `#` after.
		{"BASH_ALIASES=([s]='ls' ; [t]='sdlc approve US-001'); t", "approval-is-a-human-decision"},
		{"BASH_ALIASES=([s]='sdlc approve US-001')#x", "approval-is-a-human-decision"},
	} {
		for _, agent := range refusedAMadeWord {
			refusedUnder(t, c.command, State{CommitReady: true, Agent: agent}, assignRule, c.own)
		}
		if f, ok := HumanDecisions(c.command, false, aStoryWaits); !ok || f.Rule != assignRule && f.Rule != c.own {
			t.Errorf("AC-8 HumanDecisions(%q) = %q, %v; want %s or %s", c.command, f.Rule, ok, assignRule, c.own)
		}
	}
	// A literal whose values run nothing a person owns stays allowed.
	allowed(t, "BASH_ALIASES=([x]=$(printf y) [s]='ls -l'); s", State{CommitReady: true, Agent: "implementer"})
}

// AC-8 and every refusal says what to do: a path rule reached through an alias's use
// is refused where the alias is defined, and the reason names the alias and what it
// runs, since the command the person sees was never written out.
func TestAC8APathRuleReachedThroughAnAliasNamesTheAliasAndWhatItRuns(t *testing.T) {
	for _, c := range []struct{ command, rule, runs string }{
		{"alias w='rm .sdlc/state/tests.lock'\nw", "loop-state-through-the-tool", "rm .sdlc/state/tests.lock"},
		{"shopt -s expand_aliases\nalias w='rm -rf .sdlc'\nw", "loop-state-through-the-tool", "rm -rf .sdlc"},
		{"alias w=\"cp x CLAUDE.md\"\nw", "protected-path-through-the-tool", "cp x CLAUDE.md"},
	} {
		f, ok := Inspect(c.command, State{CommitReady: true, Agent: "implementer"})
		if !ok || f.Rule != c.rule {
			t.Errorf("AC-8 %q = %q, %v; want %s", c.command, f.Rule, ok, c.rule)
			continue
		}
		if want := "defining the alias `w` is refused: it runs `" + c.runs + "`"; !strings.Contains(f.Reason, want) {
			t.Errorf("AC-8 %q: the reason does not say %q: %s", c.command, want, f.Reason)
		}
		if f.Route == "" {
			t.Errorf("AC-8 %q: the refusal says nothing to do", c.command)
		}
	}
}

// AC-8, T1: a script find hands a shell is read again as the shell decodes it, so
// a program word spelt `$'sdlc'` is read as made and refused like any other.
func TestAC8AScriptFindHandsAShellIsReadAgainAsTheShellDecodesIt(t *testing.T) {
	command := "find . -exec sh -c \"\\$'sdlc' approve US-001\" \\;"
	for _, agent := range refusedAMadeWord {
		refusedUnder(t, command, State{CommitReady: true, Agent: agent}, assignRule, "approval-is-a-human-decision")
	}
	refusedEverywhereUnder(t, command, assignRule, "approval-is-a-human-decision")
}

// AC-8, T1, PLAN.md round 12: bash's `${!x:=…}` and zsh's flagged `${(P)x::=…}` can
// change which name an expansion writes, so an assigning expansion with flags or `!` is
// refused wherever it stands, and the reason and route say so.
func TestAC8AnAssigningExpansionWithFlagsOrBangIsRefusedWhereverItStands(t *testing.T) {
	for _, command := range []string{
		": ${!n:=x}; sdlc status",
		": ${(P)n::=x}; sdlc status",
		": ${=n:=x}; sdlc status",
		": ${(L)n:=x}; sdlc status",
	} {
		f, ok := Inspect(command, State{CommitReady: true, Agent: "implementer"})
		if !ok || f.Rule != assignRule || !strings.Contains(f.Reason, "flags or `!`") || !strings.Contains(f.Route, "assign in a command of its own") {
			t.Errorf("AC-8 %q = %q, %v (%s; %s); want %s naming the flags and routing to a plain assignment", command, f.Rule, ok, f.Reason, f.Route, assignRule)
		}
		if h, ok := HumanDecisions(command, false, aStoryWaits); !ok || h.Rule != assignRule {
			t.Errorf("AC-8 HumanDecisions(%q) = %q, %v; want %s", command, h.Rule, ok, assignRule)
		}
	}
	// An expansion that assigns with no flag before the name is read as any write is.
	allowed(t, ": ${n:=x}; sdlc status", State{CommitReady: true, Agent: "implementer"})
}

// AC-8, T1, PLAN.md round 12: a nameref anywhere on a line makes every value the line
// assigns a command, since bash points a nameref by a later assignment as well as by its
// own value.
func TestAC8ANamerefOnTheLineMakesEveryValueItAssignsACommand(t *testing.T) {
	for _, command := range []string{
		"declare -n r=BASH_ALIASES; r[s]='sdlc approve US-001'; s",
		"typeset -n r=functions; r[s]='sdlc approve US-001'; s",
		"declare -n r; r=x; y='sdlc approve US-001'",
		"declare -n r=x; r='sdlc approve US-001'",
	} {
		f, ok := Inspect(command, State{CommitReady: true, Agent: "implementer"})
		if !ok || f.Rule != assignRule || !strings.Contains(f.Reason, "nameref") || !strings.Contains(f.Reason, "sdlc approve US-001") {
			t.Errorf("AC-8 %q = %q, %v (%s); want %s naming the nameref and the value", command, f.Rule, ok, f.Reason, assignRule)
		}
		if f.Route == "" {
			t.Errorf("AC-8 %q: the refusal says nothing to do", command)
		}
		// Where no story waits, an approval is the binary's to refuse, as for the
		// command written out (the alias rows in team_test.go hold the same).
		if h, ok := HumanDecisions(command, false, aStoryWaits); !ok || h.Rule != assignRule {
			t.Errorf("AC-8 HumanDecisions(%q) = %q, %v; want %s", command, h.Rule, ok, assignRule)
		}
	}
}

// AC-8, T1: zsh's `set -A` assigns a list whole, and to `functions` or `aliases` it
// changes what a name runs, so a later use whose words read as a person's command or a
// review is refused, under the class's own reason. A use with no words is a known gap
// docs/enforcement.md declines, and has no row.
func TestAC8SetAOnACodeListChangesWhatALaterNameRuns(t *testing.T) {
	for _, c := range []struct{ command, rule string }{
		{"set -A aliases s sdlc; s approve US-001", assignRule},
		{"set +A aliases s sdlc; s approve US-001", assignRule},
		{"set -o err -A functions s sdlc; s review add code_review code-reviewer approve", reviewerRule},
	} {
		f, ok := Inspect(c.command, State{CommitReady: true, Agent: "implementer"})
		if !ok || f.Rule != c.rule || !strings.Contains(f.Reason, "changes what a name runs") {
			t.Errorf("AC-8 %q = %q, %v (%s); want %s, saying the line changes what a name runs", c.command, f.Rule, ok, f.Reason, c.rule)
		}
	}
	allowed(t, "set -A arr a b; sdlc status", State{CommitReady: true, Agent: "implementer"})
}

// AC-8, T1, the plan's made program word (Gate 4 round 4, the person's ruling): where
// the program word is made and the words after it read `review add` or a person's
// command, the command is refused. find -exec takes a program word too. The partly
// made and the variable spellings are refused today; a word the shell makes whole,
// `"$(printf sdlc)"`, is not: the cut leaves the marks unpaired, and the reading on
// the words that replaces them keeps the program word as written, so neither reading
// sees that sdlc was made. In bash and zsh find runs `sdlc approve US-001`.
func TestAC8AProgramWordTheShellMakesWholeUnderFindIsRefused(t *testing.T) {
	for _, c := range []struct{ command, own string }{
		{`find . -exec "$(printf sdlc)" approve US-001 \;`, "approval-is-a-human-decision"},
		{`find . -exec "$(printf sdlc)" review add code_review code-reviewer approve \;`, reviewerRule},
		{`find . -exec "$(printf sd)lc" approve US-001 \;`, "approval-is-a-human-decision"},
		{`find . -exec "$X" approve US-001 \;`, "approval-is-a-human-decision"},
	} {
		for _, agent := range []string{"", "implementer", "sdet", "security"} {
			refusedUnder(t, c.command, State{CommitReady: true, Agent: agent}, assignRule, reviewerRule, c.own)
		}
		if !strings.Contains(c.command, "review add") {
			refusedEverywhereUnder(t, c.command, assignRule, c.own)
		}
	}
	// A made program word under find followed by anything else is not narrowed.
	if f, ok := HumanDecisions(`find . -exec "$(printf sdlc)" status \;`, false, aStoryWaits); ok {
		t.Errorf("AC-8 HumanDecisions refuses a made program word running status under %s", f.Rule)
	}
}
