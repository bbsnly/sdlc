package gitx

// Gate 3 reopened for US-029.3's rework (B1, B2): ModeAt and TreeHash's
// ErrOverrideNotAFile were at 0% coverage under the code review's own
// evidence (cover-g7.out). B1's codemapCheck (internal/cli) refuses a
// directory, link, submodule entry or case-only name at the root CODEMAP.md
// by reading ModeAt's answer; B2's route R1 (internal/hook) measures the
// review subject with CODEMAP.md overridden as HEAD had it, which TreeHash
// refuses with ErrOverrideNotAFile where the tree holds files beneath that
// path instead of the plain file the override stands in for.
//
// Helpers carry an rw prefix, for rework.

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// rwSubmodule stages a gitlink entry at path in root's index, without an
// actual submodule checked out there: it is only ModeAt's answer for one that
// matters, not its content, and a real submodule would need a second
// repository this test has no reason to carry.
func rwSubmodule(t *testing.T, root, path string) {
	t.Helper()
	run(t, root, "update-index", "--add", "--cacheinfo",
		"160000,1111111111111111111111111111111111111111,"+path)
}

// TestModeAtNamesTheKindOfEntryAtEachPath is B1's row: codemapCheck tells a
// plain file and an executable file, which land, from a directory, a link and
// a submodule entry, which are refused, by reading this alone.
func TestModeAtNamesTheKindOfEntryAtEachPath(t *testing.T) {
	root := repo(t)
	run(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, root, "plain.go", "package x\n")
	write(t, root, "exec.go", "package x\n")
	write(t, root, "dir/inside.go", "package x\n")
	if err := os.Chmod(filepath.Join(root, "exec.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, root, "add", "plain.go", "exec.go", "dir/inside.go")
	linked := true
	if err := os.Symlink("plain.go", filepath.Join(root, "link.go")); err != nil {
		linked = false
		t.Logf("this platform will not make a link here: %v", err)
	}
	if linked {
		run(t, root, "add", "link.go")
	}
	// Staged and committed without lgCommit's `add -A`: a gitlink has nothing
	// on disk for -A to find, so -A would read it as deleted and drop it.
	rwSubmodule(t, root, "sub")
	run(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "everything")
	commit := wtGitOut(t, root, "rev-parse", "HEAD")

	// AC-6: NTFS has no executable bit, so a file made executable here is
	// still read back "100644" on Windows (CI log,
	// internal/gitx/rework_test.go:78); that one row skips there, with the
	// reason, and every other row still runs.
	noExecBit := runtime.GOOS == "windows"

	for _, tc := range []struct {
		name, path, want, skipWhy string
		skip                      bool
	}{
		{"a plain file", "plain.go", "100644", "", false},
		{"AC-6 an executable file", "exec.go", "100755", "NTFS has no executable bit to read back", noExecBit},
		{"a directory", "dir", "040000", "", false},
		{"a link", "link.go", "120000", "this platform will not make a link here", !linked},
		{"a submodule entry", "sub", "160000", "", false},
		{"a path the commit does not have", "missing.go", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip {
				t.Skip(tc.skipWhy)
			}
			got, err := ModeAt(t.Context(), root, commit, tc.path)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("ModeAt(%s) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestModeAtIsEmptyInAnEmptyCommit is the boundary the table above does not
// reach: a commit that holds nothing at all still answers every path with "",
// not an error.
func TestModeAtIsEmptyInAnEmptyCommit(t *testing.T) {
	root := repo(t)
	run(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	empty := lgCommit(t, root, "nothing yet")
	got, err := ModeAt(t.Context(), root, empty, "CODEMAP.md")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("ModeAt in an empty commit = %q, want empty", got)
	}
}

// TestTreeHashReturnsErrOverrideNotAFileWhenTheOverridesPathHoldsFilesBeneathIt
// is B2's row: the working tree that is about to be measured has turned
// CODEMAP.md from a plain file into a directory, so the override taken from an
// earlier commit -- where it was still a plain file -- cannot stand in for it.
func TestTreeHashReturnsErrOverrideNotAFileWhenTheOverridesPathHoldsFilesBeneathIt(t *testing.T) {
	root, _ := lgRepo(t)
	write(t, root, "CODEMAP.md", "# map\n")
	at := lgCommit(t, root, "map, as a plain file")

	if err := os.Remove(filepath.Join(root, "CODEMAP.md")); err != nil {
		t.Fatal(err)
	}
	write(t, root, "CODEMAP.md/evil.go", "package evil\n")
	write(t, root, "CODEMAP.md/other.go", "package evil\n")

	_, err := TreeHash(t.Context(), root, Override{Path: "CODEMAP.md", At: at})
	if !errors.Is(err, ErrOverrideNotAFile) {
		t.Fatalf("TreeHash with files beneath the override's path = %v, want ErrOverrideNotAFile", err)
	}

	// Control: the same tree, measured without asking for CODEMAP.md as the
	// commit had it, is not an error -- a directory of files is an ordinary
	// tree on its own. The failure is the override's, not the tree's.
	if _, err := TreeHash(t.Context(), root); err != nil {
		t.Errorf("TreeHash without the override failed on an ordinary tree: %v", err)
	}
}

// TestTreeHashWithAContentOverrideIsNotToldApartFromADirectoryByErrOverrideNotAFile
// is the boundary next to it: holdsBeneath is only asked where the override
// names a commit to take the path from (o.At). A content override (o.At
// empty) still cannot stand in for a directory -- git refuses a blob where an
// entry beneath it is already staged -- but that failure is an ordinary one,
// not ErrOverrideNotAFile, because nothing here asked the question.
func TestTreeHashWithAContentOverrideIsNotToldApartFromADirectoryByErrOverrideNotAFile(t *testing.T) {
	root, _ := lgRepo(t)
	write(t, root, "CODEMAP.md/evil.go", "package evil\n")

	_, err := TreeHash(t.Context(), root, Override{Path: "CODEMAP.md", Content: []byte("# stood in for\n")})
	if err == nil {
		t.Fatal("TreeHash with a content override over a directory succeeded, want an error")
	}
	if errors.Is(err, ErrOverrideNotAFile) {
		t.Errorf("TreeHash with a content override over a directory = ErrOverrideNotAFile, "+
			"want an ordinary git failure: %v", err)
	}
}

// TestHoldsBeneathTellsEntriesUnderThePathFromThePathItself is holdsBeneath's
// own table: ls-files --stage's shape, a line per entry, tab before the name.
func TestHoldsBeneathTellsEntriesUnderThePathFromThePathItself(t *testing.T) {
	const mode = "100644 aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa 0"
	for _, tc := range []struct {
		name, staged string
		want         bool
	}{
		{"nothing staged there at all", "", false},
		{"the path itself, alone", mode + "\tCODEMAP.md\n", false},
		{"the path itself, with no trailing newline", mode + "\tCODEMAP.md", false},
		{"one file beneath it", mode + "\tCODEMAP.md/a.txt\n", true},
		{"the path itself plus a file beneath it", mode + "\tCODEMAP.md\n" + mode + "\tCODEMAP.md/a.txt\n", true},
		{"two files beneath it, the path itself absent", mode + "\tCODEMAP.md/a.txt\n" + mode + "\tCODEMAP.md/b.txt\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := holdsBeneath(tc.staged, "CODEMAP.md"); got != tc.want {
				t.Errorf("holdsBeneath(%q, CODEMAP.md) = %v, want %v", tc.staged, got, tc.want)
			}
		})
	}
}
