package checklegs

import (
	"strings"
	"testing"
)

// mdFixture builds a fixture holding one clean tracked file and, if
// withGitignored is true, one gitignored Markdown file under
// .sdlc/stories/US-999 -- the exact path AC-1 names, and the one this
// repository's own gates write to (.sdlc/stories/<ID>/*.md, .gitignore:21).
func mdFixture(t *testing.T, withGitignored bool) string {
	t.Helper()
	root := t.TempDir()
	gitInit(t, root)
	write(t, root, ".gitignore", "/.sdlc/stories/\n")
	write(t, root, "README.md", "# Clean\n\nThis document is fine.\n")
	gitAdd(t, root, "README.md", ".gitignore")
	seedMarkdownlintTool(t, root)
	if withGitignored {
		// A line past .markdownlint-cli2.jsonc's own "line_length": 100,
		// under a heading with a blank line after it so the only rule this
		// trips is MD013 -- the exact rule ANALYSIS.md's own reproduction
		// measured (.sdlc/stories/US-999/NOTE.md:3:101, MD013/line-length).
		long := strings.TrimSpace(strings.Repeat("lorem ", 20))
		write(t, root, ".sdlc/stories/US-999/NOTE.md", "# Title\n\n"+long+"\n")
	}
	return root
}

// TestAC1AndAC2MarkdownlintIgnoresGitignoredMarkdownUnderSdlcStories fails
// today: markdownlint-cli2 globs **/*.md with dot:true unconditionally
// (markdownlint-cli2.mjs:460) and only consults .gitignore where
// markdownlintOptions.gitignore === true, which .markdownlint-cli2.jsonc
// does not set, so the gitignored file under .sdlc/stories/ is linted and
// MD013 fires: exit 1, where a clean clone -- nothing under .sdlc/stories/
// to begin with -- exits 0. This is exactly the shape ANALYSIS.md
// reproduced (58 gitignored files, zero of them `git check-ignore` would
// call tracked, still failing the leg).
//
// AC-1 names this leg and this path directly. It also stands as this
// suite's evidence for markdownlint's share of AC-2's leg-generic "no leg
// ... shall fail on a file Git does not track": ANALYSIS.md names
// markdownlint and secrets as the only two of sixteen legs this story finds
// broken that way (TestAC2SecretsLegIgnoresAGitignoredSecretButStillCatchesAnUntrackedOne
// covers the other), and every other leg's immunity is argued, not
// reproduced, because none of them are currently broken.
func TestAC1AndAC2MarkdownlintIgnoresGitignoredMarkdownUnderSdlcStories(t *testing.T) {
	clean := mdFixture(t, false)
	cleanOut, cleanErr := runTask(t, clean, "markdownlint")
	if cleanErr != nil {
		t.Fatalf("a clean clone (nothing gitignored present) must pass; this is the "+
			"fixture's own sanity check, not AC-1 itself: %v\n%s", cleanErr, cleanOut)
	}

	full := mdFixture(t, true)
	fullOut, fullErr := runTask(t, full, "markdownlint")

	// AC-1: "shall exit 0 where a clean clone of the same tree exits 0."
	if fullErr != nil {
		t.Errorf("markdownlint failed on a tree holding a gitignored Markdown file under "+
			".sdlc/stories/, where the clean-clone fixture above passed; AC-1 requires the "+
			"same exit code:\n%s", fullOut)
	}
	// AC-1: "shall report only findings in files Git tracks." NOTE.md is
	// the gitignored file's name; nothing tracked in either fixture shares
	// it, so its presence in the output can only mean it was linted.
	if strings.Contains(fullOut, "NOTE.md") {
		t.Errorf("markdownlint's output named the gitignored file, which git ls-files "+
			"does not list in this fixture:\n%s", fullOut)
	}
}
