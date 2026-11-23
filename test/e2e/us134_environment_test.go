// US-134 AC-1 and AC-13, put to the built hook in a sandbox: a call that runs
// the configured commands, `sdlc gate implementation pass` or `sdlc freeze`,
// is refused where the line changes the environment it starts with, since the
// result it records is reused in place of the verifier's run and the
// landing's (design review, security F-1). The call alone, and another sdlc
// call with a setting in front of it, go through. internal/shellpolicy holds
// the spellings.

package e2e

import "testing"

func TestAC1AC13TheHookRefusesACallThatRunsTheConfiguredCommandsWithItsEnvironmentChangedInTheSandbox(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.works(t, sb.repo, "", "", "start")
	tree := sb.tree()
	for _, command := range []string{
		"GOFLAGS='-run=^$' sdlc gate implementation pass --note green",
		`PATH="$PWD/fake:$PATH" sdlc freeze`,
		"export ZZ_US134=1; sdlc freeze",
		"sh -c 'sdlc gate implementation pass --note green'",
	} {
		sb.refuses(t, sandboxSession, tree, map[string]any{"command": command}, "commands-run-as-configured")
	}
	for _, command := range []string{
		"sdlc gate implementation pass --note green",
		"sdlc freeze",
		"cd . && sdlc freeze",
		"GOFLAGS=-v sdlc status",
		"GOFLAGS=-v sdlc gate implementation fail --note red",
	} {
		sb.allows(t, tree, "", bashTool, map[string]any{"command": command})
	}
}
