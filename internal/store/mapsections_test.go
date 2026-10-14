package store

// US-089: the sections a story's bookkeeper stored for the root CODEMAP.md,
// kept until `sdlc stop` has put them on trunk, a person's `sdlc remap` has,
// or a person has dropped them.
//
// They are kept in the repository's common git directory, at
// <git-dir>/sdlc/codemap/<ID>.md, and nowhere under .sdlc/ or the story's
// worktree. The worktree goes when the story lands, and its own git directory
// with it; a file in the working tree stops the landing (SDLC-E0032); and the
// story's branch never holds it. The git directory is never committed, never
// pushed, and is protected from the session by the file rules and the shell
// rules. One place serves the whole of a change's life: what the bookkeeper
// stored is what the stop reads, what a failed map step leaves is the same
// file left where it is, and what a stop killed between the landing and the
// map commit leaves is the same file again (THREATS T4, T6).
//
// The functions these tests hold the store to:
//
//	(*Store).WriteMapSections(story string, content []byte) error
//	(*Store).MapSections(story string) (content []byte, stored bool, err error)
//	(*Store).DropMapSections(story string) error
//	(*Store).StoriesWithMapSections() ([]string, error)
//
// Each is the same from every checkout of the repository. The story id passes
// CheckID before it is part of a path, as commits.go does for what it keeps in
// the git directory. The store keeps bytes: what a section is, and whether
// these are any, is internal/codemap's to say and the caller's to ask again
// every time it reads them.
//
// Helpers carry an ms prefix, for map sections.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

const msSections = "<!-- sdlc:add -->\n## refund.go\n\nKeeps refunds.\n"

// msFile is where the sections of US-001 are kept, in the main checkout's
// git directory.
func msFile(main string) string {
	return filepath.Join(main, ".git", "sdlc", "codemap", "US-001.md")
}

func msStored(t *testing.T, s *Store, id string) (string, bool) {
	t.Helper()
	got, stored, err := s.MapSections(id)
	if err != nil {
		t.Fatalf("MapSections(%q): %v", id, err)
	}
	return string(got), stored
}

func msWritten(t *testing.T, s *Store, id, content string) {
	t.Helper()
	if err := s.WriteMapSections(id, []byte(content)); err != nil {
		t.Fatalf("WriteMapSections(%q): %v", id, err)
	}
}

func TestMapSectionsAreKeptInTheCommonGitDirectoryAndReadBackAsWritten(t *testing.T) {
	main, _ := swNested(t)
	s := clStore(main)

	if got, stored := msStored(t, s, "US-001"); stored || got != "" {
		t.Fatalf("before anything was stored MapSections gave %q, %v", got, stored)
	}
	msWritten(t, s, "US-001", msSections)
	raw, err := os.ReadFile(msFile(main))
	if err != nil {
		t.Fatalf("the sections are not at <git-dir>/sdlc/codemap/US-001.md: %v", err)
	}
	if string(raw) != msSections {
		t.Errorf("the file holds %q, want the sections byte for byte", raw)
	}
	if got, stored := msStored(t, s, "US-001"); !stored || got != msSections {
		t.Errorf("MapSections gave %q, %v, want what was written", got, stored)
	}
}

// The bookkeeper stores from the story's worktree, and the stop that reads
// them has taken that worktree away by the time a person repairs anything: so
// the place is the one every checkout shares, never the worktree's own git
// directory or its working tree.
func TestMapSectionsStoredInAStoryWorktreeAreTheMainCheckoutsToRead(t *testing.T) {
	main, wt := swNested(t)
	status := func(dir string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", "status", "--porcelain", "--untracked-files=all")
		cmd.Dir = dir
		raw, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	before := map[string]string{main: status(main), wt: status(wt)}
	if before[wt] != "" {
		t.Fatalf("precondition: the worktree holds something uncommitted: %q", before[wt])
	}
	msWritten(t, clStore(wt), "US-001", msSections)

	if !clExists(msFile(main)) {
		t.Fatal("sections stored from the story's worktree are not in the common git directory")
	}
	if got, stored := msStored(t, clStore(main), "US-001"); !stored || got != msSections {
		t.Errorf("the main checkout reads %q, %v, want what the worktree stored", got, stored)
	}
	if clExists(filepath.Join(main, ".git", "worktrees", "US-001", "sdlc", "codemap")) {
		t.Error("the sections are in the worktree's own git directory, which goes with the worktree")
	}
	// Nothing for `git status` to see in either checkout: an uncommitted file
	// in the worktree stops the landing.
	for _, dir := range []string{main, wt} {
		if out := status(dir); out != before[dir] {
			t.Errorf("git status in %s reports %q after the sections were stored, and %q before", dir, out, before[dir])
		}
	}

	// And they outlive the worktree.
	runGit(t, main, "worktree", "remove", "--force", wt)
	if got, stored := msStored(t, clStore(main), "US-001"); !stored || got != msSections {
		t.Errorf("after the worktree was removed the main checkout reads %q, %v", got, stored)
	}
}

// Storing again replaces what was stored: the bookkeeper puts a refused set
// right, and one story has one change, not two.
func TestWritingMapSectionsAgainReplacesThem(t *testing.T) {
	main, wt := swNested(t)
	s := clStore(wt)
	msWritten(t, s, "US-001", msSections)
	second := "<!-- sdlc:add -->\n## refund.go\n\nKeeps refunds, and credit notes.\n"
	msWritten(t, s, "US-001", second)
	if got, _ := msStored(t, s, "US-001"); got != second {
		t.Errorf("after a second write MapSections gave %q, want the second", got)
	}
	entries, err := os.ReadDir(filepath.Dir(msFile(main)))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("the directory holds %d entries after two writes, want the one file and nothing left beside it", len(entries))
	}
}

func TestOneStorysMapSectionsAreNotAnothers(t *testing.T) {
	main, _ := swNested(t)
	s := clStore(main)
	msWritten(t, s, "US-001", msSections)
	msWritten(t, s, "US-002", "<!-- sdlc:add -->\n## shipping.go\n\nKeeps shipping.\n")

	if got, _ := msStored(t, s, "US-001"); got != msSections {
		t.Errorf("US-001's sections are now %q", got)
	}
	if got, stored := msStored(t, s, "US-003"); stored {
		t.Errorf("US-003, which stored nothing, has %q", got)
	}
	if err := s.DropMapSections("US-001"); err != nil {
		t.Fatalf("DropMapSections: %v", err)
	}
	if _, stored := msStored(t, s, "US-001"); stored {
		t.Error("US-001's sections are still there after they were dropped")
	}
	if got, stored := msStored(t, s, "US-002"); !stored || got == "" {
		t.Error("dropping US-001's sections took US-002's")
	}
}

// THREATS T4: after a commit or a person's --drop nothing is left under the
// git directory for the story. Dropping what is not there is not a failure:
// the caller drops after a commit whether or not a kill came between.
func TestDroppingMapSectionsLeavesNothingAndDroppingNothingIsNotAFailure(t *testing.T) {
	main, _ := swNested(t)
	s := clStore(main)
	if err := s.DropMapSections("US-001"); err != nil {
		t.Fatalf("dropping where nothing was ever stored: %v", err)
	}
	msWritten(t, s, "US-001", msSections)
	for i := range 2 {
		if err := s.DropMapSections("US-001"); err != nil {
			t.Fatalf("drop %d: %v", i+1, err)
		}
	}
	if clExists(msFile(main)) {
		t.Error("the file is still there after the drop")
	}
	if got, stored := msStored(t, s, "US-001"); stored || got != "" {
		t.Errorf("after the drop MapSections gave %q, %v", got, stored)
	}
}

// THREATS T4: a path built from a story id needs CheckID first. None of these
// reaches the disk, in any of the three calls.
func TestAStoryIDThatIsNotSafeNeverBecomesAPathForMapSections(t *testing.T) {
	main, _ := swNested(t)
	s := clStore(main)
	clWrite(t, filepath.Join(main, ".git", "outside.md"), "git's own\n")
	before := msTree(t, filepath.Join(main, ".git"))
	for _, id := range []string{"", "..", "../outside", "../../outside", "a/b", `a\b`, "US-001/../../outside", ".hidden", "-rf", "US 001", "US-001\n", "US-001\x00"} {
		if err := s.WriteMapSections(id, []byte(msSections)); codeOfOrNone(err) != sdlcerr.UnsafeStoryID {
			t.Errorf("WriteMapSections(%q) = %v, want it refused as an unsafe story id", id, err)
		}
		if got, stored, err := s.MapSections(id); codeOfOrNone(err) != sdlcerr.UnsafeStoryID || stored || got != nil {
			t.Errorf("MapSections(%q) = %q, %v, %v, want it refused as an unsafe story id", id, got, stored, err)
		}
		if err := s.DropMapSections(id); codeOfOrNone(err) != sdlcerr.UnsafeStoryID {
			t.Errorf("DropMapSections(%q) = %v, want it refused as an unsafe story id", id, err)
		}
	}
	if after := msTree(t, filepath.Join(main, ".git")); !slices.Equal(before, after) {
		t.Errorf("an unsafe id changed what the git directory holds:\nbefore %q\nafter  %q", before, after)
	}
}

// codeOfOrNone is the code of an sdlc error, and the zero code for anything
// else, nil included.
func codeOfOrNone(err error) sdlcerr.Code {
	var e *sdlcerr.Error
	if err == nil || !errors.As(err, &e) {
		return sdlcerr.Code{}
	}
	return e.Code
}

// msTree is every path under dir, for a before and after.
func msTree(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		out = append(out, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(out)
	return out
}

// AC-6 from the store's side: where the sections cannot be written the caller
// is told, with a code a person can look up, and nothing half-written is left
// for the stop to read as a set.
func TestMapSectionsThatCannotBeWrittenAreAnErrorAPersonCanRead(t *testing.T) {
	t.Run("the directory's place is taken by a file", func(t *testing.T) {
		main, _ := swNested(t)
		clWrite(t, filepath.Join(main, ".git", "sdlc", "codemap"), "in the way\n")
		s := clStore(main)
		err := s.WriteMapSections("US-001", []byte(msSections))
		clError(t, err, sdlcerr.StateUnwritable)
		if got, stored, _ := s.MapSections("US-001"); stored {
			t.Errorf("after the failed write MapSections has %q", got)
		}
	})
	t.Run("the file's place is taken by a directory", func(t *testing.T) {
		main, _ := swNested(t)
		clWrite(t, filepath.Join(msFile(main), "inside.txt"), "in the way\n")
		clError(t, clStore(main).WriteMapSections("US-001", []byte(msSections)), sdlcerr.StateUnwritable)
	})
	t.Run("the directory cannot be written", func(t *testing.T) {
		skipIfPermissionsDoNotBind(t)
		main, _ := swNested(t)
		s := clStore(main)
		msWritten(t, s, "US-002", msSections)
		dir := filepath.Dir(msFile(main))
		if err := os.Chmod(dir, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		clError(t, s.WriteMapSections("US-001", []byte(msSections)), sdlcerr.StateUnwritable)
		// THREATS T4: a drop that did not happen says so, rather than leave a
		// person believing the change is gone.
		clError(t, s.DropMapSections("US-002"), sdlcerr.StateUnwritable)
		if _, stored := msStored(t, s, "US-002"); !stored {
			t.Error("the drop that failed took the sections all the same")
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Errorf("the failed write left %d entries in the directory, want US-002's file alone", len(entries))
		}
	})
}

// A file that is there and cannot be read is not "nothing stored": read as
// nothing, a story's map change would vanish with nobody told (THREATS T6).
func TestMapSectionsThatCannotBeReadAreAnErrorAndNotNothing(t *testing.T) {
	t.Run("a directory where the file goes", func(t *testing.T) {
		main, _ := swNested(t)
		clWrite(t, filepath.Join(msFile(main), "inside.txt"), "in the way\n")
		got, stored, err := clStore(main).MapSections("US-001")
		clError(t, err, sdlcerr.StateUnreadable)
		if stored || got != nil {
			t.Errorf("MapSections gave %q, %v beside its error", got, stored)
		}
	})
	t.Run("a file nobody may read", func(t *testing.T) {
		skipIfPermissionsDoNotBind(t)
		main, _ := swNested(t)
		s := clStore(main)
		msWritten(t, s, "US-001", msSections)
		if err := os.Chmod(msFile(main), 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(msFile(main), 0o644) })
		_, _, err := s.MapSections("US-001")
		clError(t, err, sdlcerr.StateUnreadable)
	})
}

// The sections are bytes to the store, kept as given: an empty set is still a
// set somebody stored, and it is the caller that refuses it.
func TestTheStoreKeepsWhateverBytesItIsGiven(t *testing.T) {
	main, _ := swNested(t)
	s := clStore(main)
	for name, content := range map[string]string{
		"no newline at the end": "<!-- sdlc:add -->\n## a.go\n\nKeeps a.",
		"CRLF and a NUL":        "<!-- sdlc:add -->\r\n## a.go\r\n\r\nKe\x00eps a.\r\n",
		"nothing":               "",
	} {
		msWritten(t, s, "US-001", content)
		if got, stored := msStored(t, s, "US-001"); !stored || got != content {
			t.Errorf("%s: MapSections gave %q, %v, want the bytes as written", name, got, stored)
		}
	}
}

// THREATS T6: the next command has to be able to say which stories have a map
// change that never reached trunk, without knowing their ids beforehand.
func TestStoriesWithMapSectionsListsEachStoryThatHasThemAndNothingElse(t *testing.T) {
	main, wt := swNested(t)
	s := clStore(main)

	got, err := s.StoriesWithMapSections()
	if err != nil || len(got) != 0 {
		t.Fatalf("before anything was stored: %q, %v", got, err)
	}
	for _, id := range []string{"US-003", "US-001", "US-002"} {
		msWritten(t, s, id, msSections)
	}
	if err := s.DropMapSections("US-002"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(msFile(main))
	// What is not one story's sections reads as nothing: a leftover temporary
	// file, a directory, a name that is no story id, another extension.
	clWrite(t, filepath.Join(dir, ".US-004.md.tmp123"), msSections)
	clWrite(t, filepath.Join(dir, "US-005.md", "inside.txt"), "a directory\n")
	clWrite(t, filepath.Join(dir, "US-006.txt"), msSections)
	clWrite(t, filepath.Join(dir, "US-007"), msSections)
	clWrite(t, filepath.Join(dir, "..md"), msSections)
	clWrite(t, filepath.Join(dir, "-rf.md"), msSections)

	for _, from := range []string{main, wt} {
		got, err := clStore(from).StoriesWithMapSections()
		if err != nil {
			t.Fatalf("StoriesWithMapSections in %s: %v", from, err)
		}
		if want := []string{"US-001", "US-003"}; !slices.Equal(got, want) {
			t.Errorf("in %s it lists %q, want %q, in order", from, got, want)
		}
	}
}

func TestStoriesWithMapSectionsSaysSoWhenTheDirectoryCannotBeListed(t *testing.T) {
	main, _ := swNested(t)
	clWrite(t, filepath.Join(main, ".git", "sdlc", "codemap"), "in the way\n")
	got, err := clStore(main).StoriesWithMapSections()
	clError(t, err, sdlcerr.StateUnreadable)
	if len(got) != 0 {
		t.Errorf("it lists %q beside its error", got)
	}
}

// Where the checkout's repository cannot be found there is no common git
// directory to keep anything in: each call says so rather than write beside
// the project.
func TestMapSectionsOutsideARepositoryAreAnError(t *testing.T) {
	root := clTempDir(t)
	s := clStore(root)
	if err := s.WriteMapSections("US-001", []byte(msSections)); err == nil {
		t.Error("WriteMapSections went ahead in a directory that is no repository")
	}
	if got, stored, err := s.MapSections("US-001"); err == nil || stored || got != nil {
		t.Errorf("MapSections in a directory that is no repository gave %q, %v, %v", got, stored, err)
	}
	if err := s.DropMapSections("US-001"); err == nil {
		t.Error("DropMapSections went ahead in a directory that is no repository")
	}
	if got, err := s.StoriesWithMapSections(); err == nil || len(got) != 0 {
		t.Errorf("StoriesWithMapSections in a directory that is no repository gave %q, %v", got, err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("the calls left %d entries in the directory", len(entries))
	}
}
