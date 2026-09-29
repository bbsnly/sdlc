package pathrules

// US-029.2: a story is worked in a worktree at .sdlc/worktrees/<ID> inside the
// main checkout, and the hook tells the two apart by where a path lands. These
// hold the two readings of a path it relies on from a worktree nested that way
// (decision 7, item 6, carried from US-029.1), and Within's answer for the
// root itself spelled in another case.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// uncleaned joins parts with the separator and nothing else: filepath.Join
// cleans, and would take the `d/..` these are about out of the word.
func uncleaned(parts ...string) string {
	return strings.Join(parts, string(filepath.Separator))
}

// nestedWorktree is a directory standing for a main checkout, with a story
// worktree at .sdlc/worktrees/A-1 inside it and the story's records beside
// it, and a link in the worktree leading to the records.
func nestedWorktree(t *testing.T) (wt, records string) {
	t.Helper()
	main := project(t)
	wt = filepath.Join(main, ".sdlc", "worktrees", "A-1")
	records = filepath.Join(main, ".sdlc", "stories", "A-1")
	for _, dir := range []string{wt, records} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(records, filepath.Join(wt, "lnk")); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	return wt, records
}

// `mkdir -p d && echo x > d/../lnk/gate-record.json` writes where lnk leads.
// Before d is made, Place cannot know that, and the hook asks the word cleaned
// first as well: that reading, and Place's own once d is there, both land in
// the records, as the filesystem does.
func TestAC6DotDotAfterADirectoryMadeInTheSameCommandIsReadThroughTheLinkFromANestedWorktree(t *testing.T) {
	wt, records := nestedWorktree(t)
	res := NewResolver(wt)
	word := uncleaned("d", "..", "lnk", "gate-record.json")
	want := filepath.Join(records, "gate-record.json")

	lexical, ok := Abs(res.Root(), word)
	if !ok {
		t.Fatalf("Abs(%q) could not place it", word)
	}
	if got := mustPlace(t, res, filepath.Clean(lexical)); got != want {
		t.Errorf("Place of %s cleaned = %q, want %q, where the link leads", word, got, want)
	}

	if err := os.Mkdir(filepath.Join(wt, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := mustPlace(t, res, word); got != want {
		t.Errorf("with d made, Place(%q) = %q, want %q, where the link leads", word, got, want)
	}
	for _, deeper := range []string{
		uncleaned("d", "e", "..", "..", "lnk", "gate-record.json"),
		uncleaned("d", ".", "..", "lnk", "..", "A-1", "gate-record.json"),
	} {
		if err := os.MkdirAll(filepath.Join(wt, "d", "e"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := mustPlace(t, res, deeper); got != want {
			t.Errorf("Place(%q) = %q, want %q", deeper, got, want)
		}
	}
}

// The file tools are judged by Rel, which cleans first and then follows the
// links on what exists: the same word, from the worktree, is outside it.
func TestAC6DotDotThroughALinkOutOfANestedWorktreeIsOutsideIt(t *testing.T) {
	wt, records := nestedWorktree(t)
	for _, word := range []string{
		uncleaned("d", "..", "lnk", "gate-record.json"),
		uncleaned("lnk", "gate-record.json"),
		uncleaned(wt, "d", "..", "lnk", "gate-record.json"),
	} {
		rel, outside := Rel(wt, word)
		if !outside {
			t.Errorf("Rel(worktree, %q) = %q inside, want it outside: it is %s", word, rel, filepath.Join(records, "gate-record.json"))
		}
	}
	if rel, outside := Rel(wt, uncleaned("d", "..", "invoice.go")); outside || rel != "invoice.go" {
		t.Errorf("Rel(worktree, d/../invoice.go) = %q, %v; want invoice.go inside", rel, outside)
	}
}

// The root spelled in another case is the root, on a filesystem that folds
// case, and a directory beside it on one that does not.
func TestAC6WithinTakesTheRootSpelledInAnotherCaseAsTheRootWhereTheFilesystemFolds(t *testing.T) {
	root := project(t)
	rel, inside := Within(root, strings.ToUpper(root))
	folds := runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	switch {
	case folds && (!inside || rel != "."):
		t.Errorf("Within(root, ROOT) = %q, %v; want the root itself, \".\"", rel, inside)
	case !folds && inside && strings.ToUpper(root) != root:
		t.Errorf("Within(root, ROOT) = %q, %v; want outside on a filesystem that keeps case", rel, inside)
	}
}
