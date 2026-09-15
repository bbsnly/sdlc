package shellpolicy

import "testing"

// A file copied into a directory lands there under its own name. Read as
// writing only the directory, `rsync -a /tmp/snap/internal/calc/add_test.go
// internal/calc/` put a frozen test back from a scratch copy, where cp with the
// same words was refused.
func TestAFileCopiedIntoADirectoryKeepsItsName(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for command, rule := range map[string]string{
		"rsync -a /tmp/snap/internal/calc/add_test.go internal/calc/": "frozen-test-through-the-tool",
		"rsync -a ../snap/CLAUDE.md .":                                "protected-path-through-the-tool",
		"rsync -a /tmp/snap/.sdlc .":                                  "loop-state-through-the-tool",
		"scp host:proj/CLAUDE.md .":                                   "protected-path-through-the-tool",
		"scp -P 22 host:CLAUDE.md ./":                                 "protected-path-through-the-tool",
		"scp host:proj/CLAUDE.md C:/proj/":                            "protected-path-through-the-tool",
		"rsync -a /tmp/snap/CLAUDE.md ./backup-12:30/":                "protected-path-through-the-tool",
		"rsync -a /tmp/snap/CLAUDE.md docs":                           "protected-path-through-the-tool",
		`rsync -a ..\snap\add_test.go internal/calc/`:                 "frozen-test-through-the-tool",
		"rsync -a /tmp/snap/add_test.go 'internal/calc/'":             "frozen-test-through-the-tool",
	} {
		refused(t, command, s, rule)
	}
	for _, command := range []string{
		"rsync -a /tmp/snap/new.go internal/calc/",
		"scp CLAUDE.md host:proj/",
		"rsync -a CLAUDE.md backup-host:/srv/",
	} {
		allowed(t, command, s)
	}
	// Copied outside the project, a file is nothing of the project's.
	placed := s
	placed.Resolve = func(word string) string {
		if isAbsolute(word) {
			return ""
		}
		return word
	}
	for _, command := range []string{
		"rsync -av CLAUDE.md .sdlc/config.json internal/calc/add_test.go /tmp/backup/",
		"scp host:proj/CLAUDE.md /tmp/review/",
	} {
		allowed(t, command, placed)
	}
}
