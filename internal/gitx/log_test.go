package gitx

import (
	"strings"
	"testing"
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
		if got, err := Resolve(t.Context(), root, rev); err == nil {
			t.Errorf("%q resolved to %q; want a refusal", rev, got)
		}
	}
}

func TestIsAncestorTellsNoFromAFailure(t *testing.T) {
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

// A story id is found as a whole word: the story US-1 is not the story US-10,
// and a sentence can end in its name.
func TestCommitsNamingWantsTheWholeWord(t *testing.T) {
	root := repo(t)
	root0 := commit(t, root, "US-1: the first part")
	commit(t, root, "US-10 and XUS-1 and US-1.2 are other stories")
	second := commit(t, root, "chore: tidy\n\nfinishes the work for US-1.")
	commit(t, root, "US_1, us-1 and US-1-b are not it either")

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
