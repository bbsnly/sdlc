package gitx

import (
	"slices"
	"testing"
)

func TestChangesNamesEveryPathNotCommitted(t *testing.T) {
	root := repo(t)
	write(t, root, "moved.txt", "a\n")
	write(t, root, "kept.txt", "k\n")
	run(t, root, "add", "-A")
	run(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "base")

	run(t, root, "mv", "moved.txt", "a name with spaces.txt")
	write(t, root, "kept.txt", "changed\n")
	write(t, root, "new/one.txt", "1\n")

	got, err := Changes(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	// A rename is both of its paths, and an untracked directory is itself.
	want := []string{"a name with spaces.txt", "kept.txt", "moved.txt", "new/"}
	if !slices.Equal(got, want) {
		t.Errorf("Changes = %q, want %q", got, want)
	}
}
