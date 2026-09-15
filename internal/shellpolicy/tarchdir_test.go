package shellpolicy

import "testing"

// tar reads the operands after -C from the directory it names. Read from the
// project, `tar -czf /tmp/dist.tgz -C dist --remove-files .` removed the project
// rather than dist.
func TestTarReadsOperandsFromWhereItChangedTo(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		"tar --remove-files -czf /tmp/logs.tgz -C internal calc",
		"tar --remove-files -czf /tmp/logs.tgz --directory internal calc",
		"tar --remove-files -czf /tmp/logs.tgz --directory=internal calc",
		"tar -czf /tmp/x.tgz --remove-files internal/calc -C /tmp y",
		"tar -xf evil.tar -C internal calc/add_test.go",
	} {
		refused(t, command, s, "frozen-test-through-the-tool")
	}
	for _, command := range []string{
		"tar -czf /tmp/dist.tgz -C dist --remove-files .",
		"tar --remove-files -czf /tmp/logs.tgz -C logs .",
	} {
		allowed(t, command, s)
	}
	placed := s
	placed.Resolve = func(word string) string {
		if isAbsolute(word) {
			return ""
		}
		return word
	}
	for _, command := range []string{
		"tar -C /tmp/build -czf /tmp/out.tgz --remove-files .",
	} {
		allowed(t, command, placed)
	}
}
