package shellpolicy

// Rows for released readings no row reached: macOS's arch as a wrapper, and a
// substitution inside a `${...}` outside double quotes.

import "testing"

// security, round 3, N2: macOS's arch runs a program as env does, its options read
// whole, so what it runs is read as written and refused by its own rule.
func TestArchRunsTheProgramAfterItsOptionsAndThatProgramIsRead(t *testing.T) {
	for _, c := range []struct{ command, rule string }{
		{"arch -arm64 sdlc approve US-001", "approval-is-a-human-decision"},
		{"arch -c -d FOO -e A=B -x86_64 sdlc review add code_review code-reviewer approve", reviewerRule},
		{"arch -dFOO -- sdlc unfreeze --reason x", "unfreeze-is-a-human-decision"},
		{"arch -arch x86_64 rm .sdlc/state/tests.lock", "loop-state-through-the-tool"},
	} {
		refusedUnder(t, c.command, State{CommitReady: true, Agent: "implementer"}, c.rule)
	}
	allowed(t, "arch -arm64 ls", State{CommitReady: true, Agent: "implementer"})
}

// US-112: a substitution inside a `${...}` runs as one outside does, in a default
// value, quoted or not, nested, in backticks, and past a quoted or escaped brace.
func TestASubstitutionInsideAParameterExpansionIsRead(t *testing.T) {
	for _, command := range []string{
		"echo ${x:-\"$(sdlc approve US-001)\"}",
		"echo ${x:-${y:-$(sdlc approve US-001)}}",
		"echo ${x:-`sdlc approve US-001`}",
		"echo ${x:-\"`sdlc approve US-001`\"}",
		"echo ${x:-\"\\}\"$(sdlc approve US-001)}",
		"echo ${x:-'}'$(sdlc approve US-001)}",
		"echo ${x:-\\}$(sdlc approve US-001)}",
		"echo ${x:-$(sdlc approve US-001)",
	} {
		refusedUnder(t, command, State{CommitReady: true, Agent: "implementer"}, "approval-is-a-human-decision")
		if f, ok := HumanDecisions(command, false, aStoryWaits); !ok || f.Rule != "approval-is-a-human-decision" {
			t.Errorf("HumanDecisions(%q) = %q, %v; want approval-is-a-human-decision", command, f.Rule, ok)
		}
	}
	allowed(t, "echo ${x:-plain}", State{CommitReady: true, Agent: "implementer"})
}
