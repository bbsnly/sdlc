package shellpolicy

import "testing"

// tar --remove-files and rsync --remove-source-files delete what they copy.
// Read as writing only the archive or the destination, `tar -czf /tmp/bak.tgz
// --remove-files internal/calc/add_test.go` took a frozen test away as a copy to
// elsewhere.
func TestACopyThatRemovesItsSourcesTakesThemAway(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for command, rule := range map[string]string{
		"tar -czf /tmp/bak.tgz --remove-files internal/calc/add_test.go":        "frozen-test-through-the-tool",
		"tar --remove-files -cf /tmp/bak.tar internal/calc":                     "frozen-test-through-the-tool",
		"tar -cf /tmp/bak.tar --remove-files .sdlc/state/active":                "loop-state-through-the-tool",
		"tar -cf /tmp/bak.tar --remove-files CLAUDE.md":                         "protected-path-through-the-tool",
		"tar --remove-files -cf /tmp/bak.tar .":                                 "loop-state-through-the-tool",
		"rsync -av --remove-source-files .sdlc/state/active /tmp/backup/":       "loop-state-through-the-tool",
		"rsync -a --remove-source-files CLAUDE.md /tmp/backup/":                 "protected-path-through-the-tool",
		"rsync -a --remove-source-files internal/calc/add_test.go /tmp/backup/": "frozen-test-through-the-tool",
		"rsync -a --remove-source-files internal/calc /tmp/backup/":             "frozen-test-through-the-tool",
		"rsync -a --remove-source-files ./ /tmp/backup/":                        "loop-state-through-the-tool",
	} {
		refused(t, command, s, rule)
	}
	for _, command := range []string{
		"tar -czf /tmp/bak.tgz --remove-files /tmp/scratch",
		"rsync -a --remove-source-files /tmp/incoming/ /tmp/done/",
		"rsync -a --remove-source-files /tmp/incoming/new.go .",
		"rsync --remove-source-files",
	} {
		allowed(t, command, s)
	}
}
