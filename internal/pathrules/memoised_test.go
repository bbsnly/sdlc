package pathrules

// US-029.2: the hook asks a memoised Resolver about every word of a command in
// every checkout it holds, over one decision. It answers as the filesystem
// did when each path was first asked about, and as an unmemoised Resolver
// does over a disk that does not change.

import (
	"os"
	"path/filepath"
	"testing"
)

// AC-6: memoised or not, Rel and Place give the same answers, through a link
// as much as through a directory.
func TestAC6AMemoisedResolverAnswersAsAnUnmemoisedOneDoes(t *testing.T) {
	root := project(t)
	other := project(t)
	if err := os.MkdirAll(filepath.Join(root, "sub", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, filepath.Join(root, "lnk")); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	plain, memo := NewResolver(root), NewResolver(root).Memoised()
	for _, p := range []string{
		filepath.Join("sub", "deep", "x.go"),
		filepath.Join("sub", "new", "x.go"),
		filepath.Join("lnk", "x.go"),
		filepath.Join(root, "sub"),
		root,
		filepath.Join(other, "y"),
	} {
		// Twice, so that the second answer comes from what the first asked.
		for range 2 {
			if got, want := answer(memo.Rel(p)), answer(plain.Rel(p)); got != want {
				t.Errorf("memoised Rel(%s) = %s, want %s", p, got, want)
			}
			if got, want := answer(memo.Place(p)), answer(plain.Place(p)); got != want {
				t.Errorf("memoised Place(%s) = %s, want %s", p, got, want)
			}
		}
	}
	if rel, outside := memo.Rel(filepath.Join("lnk", "x.go")); !outside || rel != filepath.ToSlash(filepath.Join(other, "x.go")) {
		t.Errorf("Rel through a link out of the project = %s, %v; want %s outside", rel, outside, filepath.Join(other, "x.go"))
	}
}

// AC-6: a memoised Resolver is for one decision, over which the disk is taken
// not to change: a path made after it was asked about is answered as it was
// when asked, and a fresh Resolver sees the new one.
func TestAC6AMemoisedResolverKeepsTheAnswerTheDiskGaveFirst(t *testing.T) {
	root := project(t)
	other := project(t)
	memo := NewResolver(root).Memoised()
	word := filepath.Join("lnk", "gate-record.json")
	before, ok := memo.Place(word)
	if !ok || before != filepath.Join(root, "lnk", "gate-record.json") {
		t.Fatalf("Place(%s) before the link = %s, %v", word, before, ok)
	}
	if err := os.Symlink(other, filepath.Join(root, "lnk")); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	if after, _ := memo.Place(word); after != before {
		t.Errorf("the memoised Place changed its answer with the disk: %s, then %s", before, after)
	}
	if rel, outside := memo.Rel(word); outside || rel != "lnk/gate-record.json" {
		t.Errorf("the memoised Rel = %s, %v; want the answer from before the link", rel, outside)
	}
	fresh, _ := NewResolver(root).Place(word)
	if fresh != filepath.Join(other, "gate-record.json") {
		t.Errorf("a fresh Resolver's Place = %s, want %s through the new link", fresh, filepath.Join(other, "gate-record.json"))
	}
}

// answer is a Rel or Place answer as one comparable string.
func answer(s string, b bool) string {
	if b {
		return s + " (true)"
	}
	return s + " (false)"
}
