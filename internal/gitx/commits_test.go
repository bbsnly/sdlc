package gitx

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// commitFile is a commit that touches one named file, so that two branches can
// be built side by side and merged without meeting in the same file.
func commitFile(t *testing.T, root, name, message string) string {
	t.Helper()
	write(t, root, name, message)
	run(t, root, "add", "-A")
	run(t, root, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "--quiet", "-m", message)
	sha, err := Resolve(t.Context(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

// sameDir compares two paths as the filesystem sees them: a temporary directory
// on macOS is reached through a link, and git prints the other side of it.
func sameDir(t *testing.T, a, b string) bool {
	t.Helper()
	for _, p := range []*string{&a, &b} {
		if resolved, err := filepath.EvalSymlinks(*p); err == nil {
			*p = resolved
		}
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// The loop writes down the commits it watched a session make, and the only
// place that can go is the repository's git directory. Everywhere in the
// working tree is seen by `git status --porcelain`, which is Clean, which is
// what the commit gate calls "committed": a note the loop wrote to itself would
// leave the tree dirty and the gate would refuse the story on its own
// bookkeeping.
func TestTheGitDirectoryIsWhereARepositoryKeepsWhatIsNotInTheTree(t *testing.T) {
	root := repo(t)
	commit(t, root, "first")

	dir, err := GitDir(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(dir) {
		t.Errorf("GitDir = %q, want an absolute path", dir)
	}
	// Git prints a path slash-separated whatever the platform; every use of
	// this one joins onto it with the platform's own separator, so it has to
	// arrive ready for filepath rather than needing a conversion each time.
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil {
		t.Errorf("GitDir = %q, which holds no HEAD: %v", dir, err)
	}
	if !sameDir(t, dir, filepath.Join(root, ".git")) {
		t.Errorf("GitDir = %q, want %q", dir, filepath.Join(root, ".git"))
	}

	// A linked worktree keeps a git directory of its own, which is what stops
	// one checkout reading the observations another left behind -- the
	// cross-checkout contamination the freeze already had to be taught about.
	linked := filepath.Join(t.TempDir(), "side")
	run(t, root, "worktree", "add", "--quiet", linked, "-b", "side")
	other, err := GitDir(t.Context(), linked)
	if err != nil {
		t.Fatal(err)
	}
	if sameDir(t, other, dir) {
		t.Errorf("a linked worktree reports the main repository's git directory, %q", other)
	}

	// Outside a repository there is nowhere to put one, and the caller has to
	// be able to tell that from an answer.
	if got, err := GitDir(t.Context(), t.TempDir()); err == nil {
		t.Errorf("GitDir outside a repository = %q, nil; want a failure", got)
	}
}

// The pair on a record means what `git rev-list base..end` means: the base left
// out, the end taken in, newest first. Which of those commits are the story's
// is settled by what each one was made on, so every commit arrives with its
// first parent beside it.
func TestCommitsBetweenListsTheRangeNewestFirstWithEachCommitsFirstParent(t *testing.T) {
	root := repo(t)
	base := commit(t, root, "base")
	first := commit(t, root, "first")
	second := commit(t, root, "second")

	got, err := CommitsBetween(t.Context(), root, base, second)
	if err != nil {
		t.Fatal(err)
	}
	want := []Commit{{Hash: second, Parent: first}, {Hash: first, Parent: base}}
	if !slices.Equal(got, want) {
		t.Errorf("CommitsBetween(%.7s, %.7s) = %+v, want %+v", base, second, got, want)
	}

	// A range whose ends are one commit holds nothing. The gate asks this of
	// every story, including one that committed after the base and nothing
	// since, so an empty answer is ordinary rather than a failure.
	if got, err := CommitsBetween(t.Context(), root, second, second); err != nil || len(got) != 0 {
		t.Errorf("CommitsBetween over one commit = %+v, %v; want nothing and no failure", got, err)
	}

	// A commit this repository does not have is a failure, not an empty range.
	// Read as an empty range it would say the story committed nothing, and the
	// gate would record that as the truth.
	if got, err := CommitsBetween(t.Context(), root, strings.Repeat("f", 40), second); err == nil {
		t.Errorf("CommitsBetween from a commit git does not have = %+v, nil; want a failure", got)
	}
}

// A merge is one of the commands the classifier calls a commit, so a merge the
// session made is one of the story's own. Its first parent is the branch it was
// made on; everything else it brought in is somebody else's work, and a range
// that names the merge names all of that too.
//
// Both halves are what the commit gate turns on: it needs the first parent to
// tell which commits the session made, and it needs to see that the range holds
// more than those before it believes the range.
func TestAMergeCarriesItsFirstParentAndItsRangeCarriesTheWholeSideBranch(t *testing.T) {
	root := repo(t)
	base := commitFile(t, root, "base.txt", "base")
	ours := commitFile(t, root, "ours.txt", "ours")

	run(t, root, "checkout", "--quiet", "-b", "side", base)
	theirs := commitFile(t, root, "theirs.txt", "theirs")
	run(t, root, "checkout", "--quiet", ours)
	run(t, root, "-c", "user.email=a@b", "-c", "user.name=t", "merge", "--no-ff", "--no-edit", "side")
	merge, err := Resolve(t.Context(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}

	got, err := CommitsBetween(t.Context(), root, ours, merge)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Hash != merge {
		t.Fatalf("CommitsBetween(%.7s, %.7s) = %+v, want the merge and the branch it brought in",
			ours, merge, got)
	}
	if got[0].Parent != ours {
		t.Errorf("the merge's first parent is %.7s, want %.7s, the branch it was made on",
			got[0].Parent, ours)
	}
	if got[1].Hash != theirs {
		t.Errorf("the range holds %.7s beside the merge, want %.7s from the side branch",
			got[1].Hash, theirs)
	}
}
