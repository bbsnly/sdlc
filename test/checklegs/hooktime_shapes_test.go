package checklegs

// US-035: `./task hook-time` times two more shapes, each held to the perl
// shape's budget: the four tools `touch`, `cp`, `sed` and `tee` with one long
// single-dash word (AC-10), and many words each through a chain of 255 links
// (AC-6), which the leg skips, saying why, where it cannot make a link
// (decided D5). The page a person reads when the leg fails names them as it
// names the three US-038 added.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAC6AC10ContributingNamesTheShapesThisStoryAddsAndTheSkip(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "CONTRIBUTING.md"))
	if err != nil {
		t.Fatal(err)
	}
	page := strings.ReplaceAll(string(raw), "\r\n", "\n")
	_, section, found := strings.Cut(page, "\n## Running the checks\n")
	if !found {
		t.Fatal("CONTRIBUTING.md has no section \"Running the checks\"")
	}
	section, _, _ = strings.Cut(section, "\n## ")
	at := strings.Index(section, "./task hook-time")
	if at < 0 {
		t.Fatal("\"Running the checks\" does not say how to run the leg: no `./task hook-time`")
	}
	said := strings.Join(strings.Fields(section[at:]), " ")
	for _, want := range []struct{ what, word string }{
		{"the single-dash shape, by the name the leg prints", "single-dash"},
		{"the four tools of the single-dash shape", "touch"},
		{"the four tools of the single-dash shape", "tee"},
		{"the links shape, by the name the leg prints", "links"},
		{"the 255 links each word goes through", "255"},
		{"that the links shape is skipped where no link can be made", "skip"},
		{"the cdpath shape, by the name the leg prints", "cdpath"},
	} {
		t.Run("AC-10 "+want.word, func(t *testing.T) {
			if !strings.Contains(said, want.word) {
				t.Errorf("after `./task hook-time`, the section does not say %s (looked for %q)", want.what, want.word)
			}
		})
	}
}
