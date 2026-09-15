package shellpolicy

import "testing"

// A copy to a file's name writes that file, not a file of the source's name
// inside it. Read as a directory, `rsync -a CLAUDE.md CLAUDE.md.bak` was a
// write to CLAUDE.md.bak/CLAUDE.md, and refused.
func TestACopyToAFileNameWritesThatFile(t *testing.T) {
	s := ready
	s.Frozen = []string{"internal/calc/add_test.go"}
	for _, command := range []string{
		"rsync -a CLAUDE.md CLAUDE.md.bak",
		"rsync -a CLAUDE.md docs/agent-guide.md",
		"scp host:proj/CLAUDE.md ./CLAUDE.remote.md",
	} {
		allowed(t, command, s)
	}
	for _, command := range []string{
		"rsync -a /tmp/a/CLAUDE.md /tmp/b/notes.md docs.old",
		"rsync -a /tmp/snap/CLAUDE.md docs.v2/",
		"rsync -a /tmp/snap/CLAUDE.md 'docs.v2/'",
		"rsync -a /tmp/snap/CLAUDE.md internal/..",
		"rsync -a /tmp/snap/CLAUDE.md .github",
	} {
		refused(t, command, s, "protected-path-through-the-tool")
	}
}
