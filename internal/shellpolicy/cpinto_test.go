package shellpolicy

import "testing"

// cp and mv into a directory write each file under its own name, as rsync and
// scp do. Read as writing the directory, `cp /tmp/snap/CLAUDE.md .` put back a
// protected file, and `cp /tmp/snap/add_test.go internal/calc/` a frozen test.
func TestCopyingIntoADirectoryWritesTheNameThere(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for command, rule := range map[string]string{
		"cp /tmp/snap/CLAUDE.md .":                                  "protected-path-through-the-tool",
		"cp -r /tmp/snap/.claude .":                                 "protected-path-through-the-tool",
		"mv /tmp/snap/CLAUDE.md ./":                                 "protected-path-through-the-tool",
		"cp -t . /tmp/snap/CLAUDE.md":                               "protected-path-through-the-tool",
		"cp -t. /tmp/snap/CLAUDE.md":                                "protected-path-through-the-tool",
		"cp --target-directory=. /tmp/snap/CLAUDE.md":               "protected-path-through-the-tool",
		"cp -t docs.v2 /tmp/snap/CLAUDE.md":                         "protected-path-through-the-tool",
		"cp -r /tmp/snap/.sdlc .":                                   "loop-state-through-the-tool",
		"cp -R /tmp/snap/.sdlc ./":                                  "loop-state-through-the-tool",
		"cp /tmp/snap/add_test.go internal/calc/":                   "frozen-test-through-the-tool",
		"cp -t internal/calc /tmp/snap/add_test.go":                 "frozen-test-through-the-tool",
		"cp -tinternal/calc /tmp/snap/add_test.go":                  "frozen-test-through-the-tool",
		"cp --target-directory=internal/calc /tmp/snap/add_test.go": "frozen-test-through-the-tool",
	} {
		refused(t, command, s, rule)
	}
	ps := s
	ps.PowerShell = true
	refused(t, "Copy-Item /tmp/snap/CLAUDE.md -Destination .", ps, "protected-path-through-the-tool")
	refused(t, "Copy-Item /tmp/snap/CLAUDE.md -Destination:.", ps, "protected-path-through-the-tool")
	refused(t, "Copy-Item /tmp/snap/add_test.go -Destination internal/calc", ps, "frozen-test-through-the-tool")
	refused(t, "Copy-Item /tmp/snap/add_test.go -Destination:internal/calc", ps, "frozen-test-through-the-tool")
	// A directory given as an option is one whatever its name, and a name with
	// an extension otherwise is the file itself.
	dotted := s
	dotted.Frozen = []string{"internal/calc.v2/add_test.go"}
	refused(t, "cp -t internal/calc.v2 /tmp/snap/add_test.go", dotted, "frozen-test-through-the-tool")
	allowed(t, "cp /tmp/snap/add_test.go internal/calc.v2", dotted)
	for _, command := range []string{
		"cp /tmp/snap/notes.md docs/",
		"mv build/out.txt dist/report.txt",
		"cp /tmp/snap/add_test.go internal/calc/add_test.go.orig",
	} {
		allowed(t, command, s)
	}
}
