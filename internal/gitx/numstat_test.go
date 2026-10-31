package gitx

// US-112 AC-10: the size of a story's change, measured with git, file by file,
// so that the commit gate can count the change and the part of it the freeze
// covers. A binary file is a file with no lines.

import (
	"slices"
	"strings"
	"testing"
)

// commitAll commits everything in root and returns the commit.
func commitAll(t *testing.T, root, message string) string {
	t.Helper()
	run(t, root, "add", "-A")
	run(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "--allow-empty", "-m", message)
	head, err := Head(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	return head
}

func statFor(t *testing.T, got []FileStat, path string) FileStat {
	t.Helper()
	i := slices.IndexFunc(got, func(f FileStat) bool { return f.Path == path })
	if i < 0 {
		t.Fatalf("no line for %s in %+v", path, got)
	}
	return got[i]
}

func TestAC10NumstatCountsTheLinesAddedAndRemovedInEachFile(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "kept.go", "a\nb\nc\n")
	write(t, root, "gone.go", "x\ny\n")
	write(t, root, ".sdlc/stories/A-1/gate-record.json", "{}\n")
	write(t, root, "CODEMAP.md", "map\n")
	base := commitAll(t, root, "base")

	write(t, root, "kept.go", "a\nB\nc\nd\n")
	write(t, root, "new_test.go", "1\n2\n3\n")
	write(t, root, "a dir/with spaces.txt", "s\n")
	write(t, root, "logo.png", "\x89PNG\r\n\x1a\n\x00\x00\x00binary\x00")
	write(t, root, ".sdlc/stories/A-1/gate-record.json", "{\"x\":1}\n")
	write(t, root, "CODEMAP.md", "map\nmore\n")
	run(t, root, "rm", "--quiet", "gone.go")
	end := commitAll(t, root, "change")

	got, err := Numstat(t.Context(), root, base, end, ".sdlc", "CODEMAP.md")
	if err != nil {
		t.Fatalf("a change between two commits was not measured: %v", err)
	}
	var paths []string
	for _, f := range got {
		paths = append(paths, f.Path)
	}
	slices.Sort(paths)
	want := []string{"a dir/with spaces.txt", "gone.go", "kept.go", "logo.png", "new_test.go"}
	if !slices.Equal(paths, want) {
		t.Errorf("files measured = %q, want %q: what is left out stays out, and every other change is in", paths, want)
	}
	if k := statFor(t, got, "kept.go"); k.Added != 2 || k.Removed != 1 || k.Binary {
		t.Errorf("kept.go = %+v, want 2 added and 1 removed", k)
	}
	if n := statFor(t, got, "new_test.go"); n.Added != 3 || n.Removed != 0 {
		t.Errorf("new_test.go = %+v, want 3 added", n)
	}
	if g := statFor(t, got, "gone.go"); g.Added != 0 || g.Removed != 2 {
		t.Errorf("gone.go = %+v, want 2 removed", g)
	}
	if b := statFor(t, got, "logo.png"); !b.Binary || b.Added != 0 || b.Removed != 0 {
		t.Errorf("logo.png = %+v, want a binary file with no lines", b)
	}
}

// A path is slash-separated and as git names it, whatever the platform, so it
// can be matched against the freeze's list.
func TestAC10NumstatNamesAPathTheWayTheFreezeDoes(t *testing.T) {
	t.Parallel()
	root := repo(t)
	base := commitAll(t, root, "base")
	write(t, root, "internal/deep/x_test.go", "1\n")
	write(t, root, "ünïcode/naïve.go", "1\n")
	end := commitAll(t, root, "change")
	got, err := Numstat(t.Context(), root, base, end)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"internal/deep/x_test.go", "ünïcode/naïve.go"} {
		if f := statFor(t, got, path); f.Added != 1 || strings.Contains(f.Path, `\`) {
			t.Errorf("%s = %+v", path, f)
		}
	}
}

func TestAC10NumstatOfTheSameCommitIsNothing(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, "a.go", "a\n")
	head := commitAll(t, root, "only")
	got, err := Numstat(t.Context(), root, head, head)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a commit measured against itself changed %+v", got)
	}
}

// AC-10: where git cannot measure, the caller is told, and leaves the record
// out.
func TestAC10NumstatThatGitCannotMeasureFails(t *testing.T) {
	t.Parallel()
	if _, err := Numstat(t.Context(), t.TempDir(), "HEAD~1", "HEAD"); err == nil {
		t.Error("a directory that is not a repository was measured")
	}
	root := repo(t)
	head := commitAll(t, root, "only")
	if _, err := Numstat(t.Context(), root, strings.Repeat("0", 40), head); err == nil {
		t.Error("a base that is not a commit was measured")
	}
}
