package gitx

// US-133 AC-2 and AC-3: the files a story changes are measured from where its
// branch left trunk, together with what the checkout has not committed. A file
// left out of the set is a change to a listed path the person is not asked
// about, so each way a change can sit in a checkout is held here: committed,
// staged, unstaged, untracked in a directory git has never seen, moved under
// either name, and deleted.

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func csCommit(t *testing.T, root, message string) string {
	t.Helper()
	run(t, root, "add", "-A")
	run(t, root, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "--quiet", "-m", message)
	sha, err := Resolve(t.Context(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

// csStory is a checkout whose story has commits since base and work on top of
// them in every state git status can report. want is every path the story has
// changed; still.txt and kept/same.txt are the controls it has not touched.
func csStory(t *testing.T) (root, base, head string, want []string) {
	t.Helper()
	root = repo(t)
	for _, f := range []string{"a.txt", "gone.txt", "moved/old.txt", "kept/x.txt", "kept/same.txt", "r/from.txt", "d.txt", "still.txt"} {
		write(t, root, f, f+"\n")
	}
	base = csCommit(t, root, "where the story left trunk")

	write(t, root, "a.txt", "changed in a commit\n")
	run(t, root, "rm", "--quiet", "gone.txt")
	run(t, root, "mv", "moved/old.txt", "moved/new.txt")
	head = csCommit(t, root, "the story's own commit")

	write(t, root, "staged.txt", "new and staged\n")
	run(t, root, "add", "staged.txt")
	write(t, root, "kept/x.txt", "changed and not staged\n")
	write(t, root, "brand/new/deep/file.txt", "untracked in directories git has never seen\n")
	write(t, root, "brand/new/deep/ü name.txt", "a name git would quote without -z\n")
	run(t, root, "mv", "r/from.txt", "r/to.txt")
	if err := os.Remove(filepath.Join(root, "d.txt")); err != nil {
		t.Fatal(err)
	}
	// a.txt is changed again, uncommitted: it is still one path.
	write(t, root, "a.txt", "changed again\n")

	want = []string{
		"a.txt", "brand/new/deep/file.txt", "brand/new/deep/ü name.txt", "d.txt", "gone.txt",
		"kept/x.txt", "moved/new.txt", "moved/old.txt", "r/from.txt", "r/to.txt", "staged.txt",
	}
	return root, base, head, want
}

func TestAC2TheChangedSetIsTheStorysCommitsAndEveryUncommittedChange(t *testing.T) {
	t.Parallel()
	root, base, head, want := csStory(t)
	got, err := ChangedSince(t.Context(), root, base, head)
	if err != nil {
		t.Fatalf("ChangedSince: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("AC-2 changed since the base = %q\nwant %q", got, want)
	}
	for _, untouched := range []string{"still.txt", "kept/same.txt"} {
		if slices.Contains(got, untouched) {
			t.Errorf("AC-2 %s, which the story never touched, is in its change", untouched)
		}
	}
}

// AC-2: where the base is the head, or there is none, the change is what the
// checkout has not committed, and nothing a commit holds.
func TestAC2WithNoCommitsSinceTheBaseTheChangeIsTheUncommittedWork(t *testing.T) {
	t.Parallel()
	root, _, head, _ := csStory(t)
	uncommitted := []string{
		"a.txt", "brand/new/deep/file.txt", "brand/new/deep/ü name.txt", "d.txt",
		"kept/x.txt", "r/from.txt", "r/to.txt", "staged.txt",
	}
	for name, base := range map[string]string{"AC2 the base is the head": head, "AC2 no base": ""} {
		t.Run(name, func(t *testing.T) {
			got, err := ChangedSince(t.Context(), root, base, head)
			if err != nil {
				t.Fatalf("ChangedSince: %v", err)
			}
			if !slices.Equal(got, uncommitted) {
				t.Errorf("AC-2 changed = %q\nwant %q", got, uncommitted)
			}
		})
	}
}

// AC-3: a file moved out from under a listed path is a change to that path,
// so a committed move is there under both names even where the tree is clean.
func TestAC3AFileMovedAwayIsChangedUnderItsOldNameAndItsNew(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "internal/hook/hook.go", "package hook\n")
	base := csCommit(t, root, "base")
	run(t, root, "mv", "internal/hook/hook.go", "internal/elsewhere.go")
	head := csCommit(t, root, "moved away")
	got, err := ChangedSince(t.Context(), root, base, head)
	if err != nil {
		t.Fatalf("ChangedSince: %v", err)
	}
	if want := []string{"internal/elsewhere.go", "internal/hook/hook.go"}; !slices.Equal(got, want) {
		t.Errorf("AC-3 a committed move = %q, want both names %q", got, want)
	}
}

// AC-3: git reports a rename in the worktree column too, where the new name is
// intent-to-add (`git add -N`): " R new", then the old name in a field of its
// own. Read as an entry, the old name lost its first three characters, and a
// file moved out from under internal/hook/** matched nothing (US-133 code
// review F-3, security F-1).
func TestAC3AnIntentToAddRenameIsChangedUnderItsOldNameAndItsNew(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "internal/hook/hook.go", "package hook\n")
	head := csCommit(t, root, "base")
	if err := os.Rename(filepath.Join(root, "internal", "hook", "hook.go"), filepath.Join(root, "elsewhere.go")); err != nil {
		t.Fatal(err)
	}
	run(t, root, "add", "-N", "elsewhere.go")
	got, err := ChangedSince(t.Context(), root, head, head)
	if err != nil {
		t.Fatalf("ChangedSince: %v", err)
	}
	if want := []string{"elsewhere.go", "internal/hook/hook.go"}; !slices.Equal(got, want) {
		t.Errorf("AC-3 an intent-to-add rename = %q, want both names, whole, %q", got, want)
	}

	// The same entry as git writes it, so that the row holds on a git that
	// pairs the two another way.
	out := []byte(" R elsewhere.go\x00internal/hook/hook.go\x00 M kept/x.txt\x00")
	if got, want := statusPathsBothNames(out), []string{"elsewhere.go", "internal/hook/hook.go", "kept/x.txt"}; !slices.Equal(got, want) {
		t.Errorf("AC-3 a worktree-column rename = %q, want %q", got, want)
	}
}

// AC-2: a clean checkout on its base has changed nothing.
func TestAC2ACleanCheckoutOnItsBaseHasChangedNothing(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "a.txt", "a\n")
	head := csCommit(t, root, "base")
	got, err := ChangedSince(t.Context(), root, head, head)
	if err != nil || len(got) != 0 {
		t.Errorf("AC-2 a clean checkout changed %q, %v", got, err)
	}
}

// AC-3: what git cannot answer is an error, never an empty change, which the
// pause would read as no listed file touched.
func TestAC3AChangeGitCannotMeasureIsAnErrorAndNotAnEmptySet(t *testing.T) {
	t.Parallel()
	root, base, head, _ := csStory(t)
	if got, err := ChangedSince(t.Context(), root, base[:12], head); err == nil {
		t.Errorf("AC-3 an abbreviated base measured %q", got)
	}
	if got, err := ChangedSince(t.Context(), root, "0123456789abcdef0123456789abcdef01234567", head); err == nil {
		t.Errorf("AC-3 a base that is no commit measured %q", got)
	}
	notRepo := t.TempDir()
	if got, err := ChangedSince(t.Context(), notRepo, "", ""); err == nil {
		t.Errorf("AC-3 a directory that is no repository measured %q", got)
	}
	if got, err := Touched(t.Context(), notRepo); err == nil {
		t.Errorf("AC-3 Touched in a directory that is no repository gave %q", got)
	}
}

// AC-3: git status names a staged rename or copy as two fields, the new name
// and then the old; both are kept, and an entry too short to hold a path is
// none.
func TestAC3TheStatusOfARenameOrACopyGivesBothNames(t *testing.T) {
	t.Parallel()
	out := []byte("R  r/to.txt\x00r/from.txt\x00C  c/copy.txt\x00c/orig.txt\x00 M kept/x.txt\x00?? new/f.txt\x00xx\x00R  last.txt\x00")
	want := []string{"r/to.txt", "r/from.txt", "c/copy.txt", "c/orig.txt", "kept/x.txt", "new/f.txt", "last.txt"}
	if got := statusPathsBothNames(out); !slices.Equal(got, want) {
		t.Errorf("AC-3 paths = %q, want %q", got, want)
	}
	if got := statusPathsBothNames(nil); len(got) != 0 {
		t.Errorf("an empty status gave %q", got)
	}
}
