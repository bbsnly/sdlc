package gitx

import (
	"errors"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// commit makes one commit with a message and returns its hash.
func commit(t *testing.T, root, message string) string {
	t.Helper()
	write(t, root, "log.txt", message)
	run(t, root, "add", "-A")
	run(t, root, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "--quiet", "-m", message)
	sha, err := Resolve(t.Context(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return sha
}

func TestResolveNamesACommitInFullOrRefuses(t *testing.T) {
	root := repo(t)
	first := commit(t, root, "first")
	second := commit(t, root, "second")

	if len(second) != 40 {
		t.Errorf("HEAD resolved to %q, want a full hash", second)
	}
	if got, err := Resolve(t.Context(), root, "HEAD~1"); err != nil || got != first {
		t.Errorf("HEAD~1 = %q, %v; want %s", got, err, first)
	}
	if got, err := Resolve(t.Context(), root, first[:12]); err != nil || got != first {
		t.Errorf("a short hash = %q, %v; want %s", got, err, first)
	}
	// A name that starts with a dash is refused even when a branch has it, since
	// git would read it as an option.
	run(t, root, "update-ref", "refs/heads/-x", "HEAD")
	for _, rev := range []string{"nope", "", "--all", "-x", "HEAD^{tree}"} {
		if got, err := Resolve(t.Context(), root, rev); !NamesNoCommit(err) {
			t.Errorf("%q resolved to %q, %v; want a refusal saying it names no commit", rev, got, err)
		}
	}

	// Git that does not run has said nothing about the commit.
	t.Setenv("PATH", t.TempDir())
	if _, err := Resolve(t.Context(), root, "HEAD"); err == nil || NamesNoCommit(err) {
		t.Errorf("with git not on PATH, Resolve = %v; want a failure that does not say the commit is missing", err)
	}
}

// notACommitError wraps what git said, so a caller further up that wants the
// code and the fix -- to render a message, say -- can still reach it by
// unwrapping past NamesNoCommit's own check.
func TestAFailedResolveStillCarriesTheUnderlyingCode(t *testing.T) {
	t.Parallel()
	root := repo(t)
	_, err := Resolve(t.Context(), root, "nope")
	if !NamesNoCommit(err) {
		t.Fatalf("Resolve(%q) = %v; want a refusal saying it names no commit", "nope", err)
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("the refusal does not unwrap to a coded error: %v", err)
	}
	if e.Code != sdlcerr.RepositoryUnreadable {
		t.Errorf("code = %s", e.Code)
	}
}

func TestIsAncestorTellsNoFromAFailure(t *testing.T) {
	t.Parallel()
	root := repo(t)
	first := commit(t, root, "first")
	second := commit(t, root, "second")

	for _, c := range []struct {
		ancestor, descendant string
		want                 bool
	}{
		{first, second, true},
		{second, first, false},
		{first, first, true},
	} {
		if got, err := IsAncestor(t.Context(), root, c.ancestor, c.descendant); err != nil || got != c.want {
			t.Errorf("IsAncestor(%.7s, %.7s) = %v, %v; want %v", c.ancestor, c.descendant, got, err, c.want)
		}
	}
	if _, err := IsAncestor(t.Context(), root, strings.Repeat("f", 40), second); err == nil {
		t.Error("a commit the repository does not have was answered as a no, not a failure")
	}
}

// With no word to look for, there is nothing to find: CommitsNaming does not
// run git at all, let alone read every commit's message against an empty
// string.
func TestCommitsNamingWithNoWordFindsNothing(t *testing.T) {
	t.Parallel()
	root := repo(t)
	commit(t, root, "first")
	got, err := CommitsNaming(t.Context(), root, "")
	if err != nil || got != nil {
		t.Errorf("CommitsNaming(root, \"\") = %v, %v; want nil, nil", got, err)
	}
}

// A repository with no commits yet has no HEAD for git log to start from, and
// that failure is passed on rather than read as no commits naming the word.
func TestCommitsNamingFailsWithNoHead(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	run(t, root, "init", "--quiet")
	_, err := CommitsNaming(t.Context(), root, "US-1")
	wantUnreadable(t, err, "CommitsNaming with no HEAD to start from")
}

// A story id is found as a whole word: the story US-1 is not the story US-10,
// and a sentence can end in its name.
func TestCommitsNamingWantsTheWholeWord(t *testing.T) {
	t.Parallel()
	root := repo(t)
	root0 := commit(t, root, "US-1: the first part")
	commit(t, root, "US-10 and XUS-1 and US-1.2 are other stories")
	second := commit(t, root, "chore: tidy\n\nfinishes the work for US-1.")
	commit(t, root, "US_1, us-1 and US-1-b are not it either")
	// The word's only occurrence carries on right at the end of the message,
	// with no room left to look again: namesWord must give up by running out
	// of message to search, not by finding "US-1" absent from the rest of it.
	commit(t, root, "fixes XUS-1")

	got, err := CommitsNaming(t.Context(), root, "US-1")
	if err != nil {
		t.Fatal(err)
	}
	want := []Commit{{Hash: second}, {Hash: root0}}
	if len(got) != len(want) {
		t.Fatalf("found %+v; want %s then %s", got, second, root0)
	}
	for i := range want {
		if got[i].Hash != want[i].Hash {
			t.Errorf("commit %d is %s; want %s", i, got[i].Hash, want[i].Hash)
		}
	}
	if got[1].Parent != "" {
		t.Errorf("the root commit has parent %q; want none", got[1].Parent)
	}
	if got[0].Parent == "" {
		t.Error("a commit with a parent was reported without one")
	}
}
