package shellpolicy

import "testing"

// Throwing away what the work tree holds that is not committed throws away the
// loop's record, which never is, and the frozen tests, which are not until the
// story is.
func TestDiscardingTheWorkTreeDiscardsTheStory(t *testing.T) {
	for _, command := range []string{
		"rm -rf .",
		"rm -rf ./",
		"rm -rf internal/..",
		"git clean -f",
		"git clean -fd",
		"git clean -dfx",
		"git clean --force -d",
		"git clean -fd -e build",
		"git clean -fd --exclude build",
		"git -C . clean -fdx",
		"git stash",
		"git stash -u",
		"git stash push --include-untracked",
		"git stash pop",
		"git reset --hard",
		"git reset --hard HEAD~1",
		"git reset --merge",
		"git reset --keep",
		"cd build && git reset --hard",
		"git checkout -f main",
		"git checkout --force main",
		"git switch -f main",
		"git switch --discard-changes main",
		"git read-tree -u --reset HEAD",
		"git read-tree -mu HEAD",
	} {
		refused(t, command, ready, "loop-state-through-the-tool")
	}
	ps := ready
	ps.PowerShell = true
	refused(t, `Remove-Item -Recurse -Force .\`, ps, "loop-state-through-the-tool")

	// git clean told where to clean, or run somewhere, cleans only there.
	frozen := ready
	frozen.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		"git clean -fd internal",
		"git clean -fd -- internal",
		"cd internal && git clean -fd",
		"git -C internal clean -fd",
	} {
		refused(t, command, frozen, "frozen-test-through-the-tool")
	}
	for _, command := range []string{
		"git clean -n",
		"git clean -nd",
		"git clean -fd build",
		"cd build && git clean -fd",
		"git -C build clean -fdx",
		"git stash list",
		"git stash show -p",
		"git stash drop",
		"git stash clear",
		"git stash create",
		"git stash store abc123",
		"git reset",
		"git reset --soft HEAD~1",
		"git reset HEAD internal/calc/add_test.go",
		"git switch main",
		"git checkout main",
		"git checkout -b feature",
		"git read-tree HEAD",
		"echo reset --hard",
		"cd build && rm -rf .",
		"cd $OUT && rm -rf .",
	} {
		allowed(t, command, frozen)
	}

	// Another repository's work tree is its own.
	placed := ready
	placed.Resolve = func(word string) string {
		if isAbsolute(word) {
			return ""
		}
		return word
	}
	for _, command := range []string{
		"git -C /tmp/other reset --hard",
		"cd /tmp/other && git stash -u",
	} {
		allowed(t, command, placed)
	}
	refused(t, "git reset --hard", placed, "loop-state-through-the-tool")
}
