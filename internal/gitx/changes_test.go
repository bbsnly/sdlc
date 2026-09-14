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

func TestChangedLinesCountsTheWorkAndNotTheMove(t *testing.T) {
	// Finding renames is git's default, and a user's configuration can turn it
	// off. The count must not change with it.
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "diff.renames")
	t.Setenv("GIT_CONFIG_VALUE_0", "false")
	root := repo(t)
	write(t, root, "ten.txt", "1\n2\n3\n4\n5\n6\n7\n8\n9\n10\n")
	write(t, root, ".sdlc/state.json", "{}\n")
	write(t, root, "stories.json", "[]\n")
	run(t, root, "add", "-A")
	run(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "base")

	run(t, root, "mv", "ten.txt", "moved.txt")              // moved, not written: nothing
	write(t, root, "new.txt", "one\ntwo\n")                 // untracked, and counted: two
	write(t, root, "image.bin", "\x00\x01\x02\x03")         // binary: nothing
	write(t, root, ".sdlc/state.json", "{\"a\": 1}\n")      // the loop's own: nothing
	write(t, root, "stories.json", "[{\"id\": \"A-1\"}]\n") // excluded by the caller

	tree, err := TreeHash(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ChangedLines(t.Context(), root, tree, "stories.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2 {
		t.Errorf("ChangedLines = %d, want 2", got)
	}
}
