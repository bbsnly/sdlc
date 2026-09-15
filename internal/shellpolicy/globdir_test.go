package shellpolicy

import "testing"

// A glob that names a directory names the frozen tests in it. `rm -rf *` at
// the project's root took every frozen test away, and named none of them.
func TestAGlobbedDirectoryHoldsItsFrozenTests(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go", "internal/load/testdata/config.json"}
	for _, command := range []string{
		"rm -rf *",
		"rm -rf ./*",
		"rm -rf internal/*",
		"rm -rf intern*",
		"rm -rf internal/c?lc",
		"rm -rf internal/[ck]alc",
		"rm -rf internal/[!x]alc",
		"rm -rf Internal/*",
		"rm -rf */load",
		"cd internal && rm -rf *",
		"mv internal/* /tmp",
		"find * -delete",
	} {
		refused(t, command, s, "frozen-test-through-the-tool")
	}
	for _, command := range []string{
		"rm -rf build/*",
		"rm -rf internal/other*",
		"rm -rf */testdata",
		"rm -rf internal/calc/*.tmp",
		"cd build && rm -rf *",
		"cd $OUT && rm -rf *",
		"cp -r internal/* /tmp/backup",
	} {
		allowed(t, command, s)
	}

	ps := s
	ps.PowerShell = true
	refused(t, `Remove-Item -Recurse internal\*`, ps, "frozen-test-through-the-tool")

	// The shell's `*` leaves a name starting with a dot alone.
	hidden := ready
	hidden.Frozen = []string{".config/check_test.go"}
	allowed(t, "rm -rf *", hidden)
	refused(t, "rm -rf .conf*", hidden, "frozen-test-through-the-tool")
}
