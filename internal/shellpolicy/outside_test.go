package shellpolicy

import "testing"

// A directory the lookup puts outside the project holds none of its frozen
// tests, whatever it ends with: cleaning up a scratch copy, `rm -rf
// /tmp/snap/internal`, was taking internal/calc/add_test.go away.
func TestADirectoryOutsideTheProjectHoldsNoFrozenTest(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go", "tests/test_calc.py"}
	s.Resolve = func(word string) string {
		if rest, ok := cutProject(word); ok {
			return rest
		}
		if isAbsolute(word) {
			return ""
		}
		return word
	}
	for _, command := range []string{
		"rm -rf /tmp/snap/internal",
		"rm -rf /tmp/snap/internal/calc",
		"mv /tmp/gen/internal ./generated",
		"rm -rf /tmp/venv/lib/python3.12/site-packages/tests",
	} {
		allowed(t, command, s)
	}
	// The project's own directories, written from the root of the disk, are
	// still its own.
	for _, command := range []string{
		"rm -rf /work/project/internal",
		"rm -rf /work/project/internal/calc",
		"mv /work/project/tests /tmp",
	} {
		refused(t, command, s, "frozen-test-through-the-tool")
	}
}

// cutProject is the project-relative rest of a path under /work/project.
func cutProject(word string) (string, bool) {
	const project = "/work/project/"
	if len(word) > len(project) && word[:len(project)] == project {
		return word[len(project):], true
	}
	return "", false
}
