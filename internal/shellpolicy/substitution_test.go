package shellpolicy

import "testing"

// A command substitution is part of the word it is written in: `$(pwd)/.sdlc`
// is the project's .sdlc. Split out as a command of its own, the path after it
// was a command called `/.sdlc/state/active`, and nothing removed it.
func TestASubstitutionIsPartOfItsWord(t *testing.T) {
	for _, command := range []string{
		"rm $(pwd)/.sdlc/state/active",
		"rm `pwd`/.sdlc/state/active",
		`rm "$(pwd)"/.sdlc/state/active`,
		"rm $(git rev-parse --show-toplevel)/.sdlc/config.json",
		"echo x > $(pwd)/.sdlc/state/active",
		"cp x $(dirname $(pwd))/project/.sdlc/config.json",
		// What runs inside it is still read.
		"echo $(rm .sdlc/state/active)/x",
		"echo `rm .sdlc/state/active`/x",
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
	refused(t, "echo x > $(pwd)/CLAUDE.md", ready, "protected-path-through-the-tool")
	ps := ready
	ps.PowerShell = true
	refused(t, "Remove-Item $(Get-Location)/.sdlc/state/active", ps, "loop-state-through-the-tool")

	for _, command := range []string{
		"echo $(date)/x > build/log",
		"ls $(pwd)/src",
		"go build -o $(pwd)/bin/app ./cmd/app",
		"echo $(cat .sdlc/state/active)",
		// Never closed, and read as far as it goes.
		"echo $(pwd /x",
		"echo `pwd /x",
	} {
		allowed(t, command, ready)
	}

	// Where the lookup puts the root of the disk outside the project, the
	// substitution has to leave a word that is not the root of the disk.
	placed := ready
	placed.Resolve = func(word string) string {
		if isAbsolute(word) {
			return ""
		}
		return word
	}
	for _, command := range []string{
		"rm $(pwd)/.sdlc/state/active",
		`rm "$(pwd)"/.sdlc/state/active`,
		`rm '$(pwd)'/.sdlc/state/active`,
		"echo $(rm $(pwd)/.sdlc/state/active)/x",
		"echo x>$(pwd)/.sdlc/state/active",
		"dd if=/dev/null of=$(pwd)/.sdlc/state/active",
	} {
		refused(t, command, placed, "loop-state-through-the-tool")
	}
	placedPS := placed
	placedPS.PowerShell = true
	refused(t, "Set-Content -Path:$(Get-Location)/.sdlc/state/active x", placedPS, "loop-state-through-the-tool")
}
