package gitx

// US-089, the edges of CommitFile the first tests left unreached: a parent
// with nothing in it, and a parent whose file at the path differs only by
// case.

import (
	"strings"
	"testing"
)

// AC-4: a parent whose tree is empty still gets the file, and the commit is
// that one path.
func TestAC4CommitFileAddsTheFileToAParentThatHoldsNothing(t *testing.T) {
	t.Parallel()
	root, _ := rpRepo(t)
	mktree := wtExec(t, root, "mktree")
	mktree.Stdin = strings.NewReader("")
	out, err := mktree.Output()
	if err != nil {
		t.Fatalf("git mktree: %v", err)
	}
	empty := wtGitOut(t, root, "commit-tree", strings.TrimSpace(string(out)), "-m", "nothing at all")
	if got := wtGitOut(t, root, "ls-tree", "-r", "--name-only", empty); got != "" {
		t.Fatalf("precondition: the parent holds %q, want nothing", got)
	}

	commit, err := CommitFile(t.Context(), root, empty, cfMap, []byte(cfBody), "docs: map US-001")
	if err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	cfMade(t, root, empty, commit, cfBody)
	if got := wtGitOut(t, root, "ls-tree", "-r", "--name-only", commit); got != cfMap {
		t.Errorf("the commit holds %q, want CODEMAP.md alone", got)
	}
}

// AC-5: where the parent has codemap.md and no CODEMAP.md, the two are one
// file on a disk that folds case, and a commit with both would leave trunk
// that no such checkout can hold. There is no commit, and the error names the
// file the parent has.
func TestAC5CommitFileRefusesWhereTheParentHasThePathInAnotherCase(t *testing.T) {
	t.Parallel()
	root, _ := rpRepo(t)
	write(t, root, "codemap.md", "# the map, by another name\n")
	tip := lgCommit(t, root, "the map in lower case")
	if got := wtGitOut(t, root, "ls-tree", "--name-only", tip, "--", cfMap); got != "" {
		t.Fatalf("precondition: the parent has %q at CODEMAP.md, want nothing", got)
	}
	before := cfState(t, root)

	commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody), "docs: map US-001")
	cfRefused(t, commit, err, root, before)
	lgSays(t, err, "codemap.md", cfMap)
}
