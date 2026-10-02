package pathrules

// US-050, AC-1: a word that reaches a path through a link and then `..` is
// placed where the shell that runs it writes, the link followed before the
// `..`, on every platform. On Windows, Win32 normalisation took `lnk\..` away
// by name before NTFS was asked, so Place came back name-first there, and a
// write through a link into another checkout's loop state was read as a write
// beside the link: improper link resolution before file access (CWE-59), by
// way of a relative path (CWE-23).
//
// The cases below pin how the walk itself reads `.` and `..` over the part of
// a word that is on disk, so they hold on every platform: `..` at the top of
// the volume stays there, `.` is nothing, `..` after a directory that is not a
// link is its parent, and a link that leads nowhere still leaves the word a
// reading, since a word the hook cannot place is a word it lets through.
// Every case is asked of a memoised resolver too, which is the one the hook
// uses.

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// acLinkKinds are the links a word can pass through: a symlink, wherever one
// can be made, and a junction, which only Windows has and which any user may
// make there without privilege.
var acLinkKinds = []string{"symlink", "junction"}

// acLink makes at a link of the kind named to the directory target, or skips
// where that kind cannot be made.
func acLink(t *testing.T, kind, target, at string) {
	t.Helper()
	if kind == "junction" {
		if runtime.GOOS != "windows" {
			t.Skip("junctions exist only on Windows")
		}
		out, err := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J", at, target).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J %s %s: %v\n%s", at, target, err, out)
		}
		return
	}
	if err := os.Symlink(target, at); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
}

// acResolvers are a fresh resolver for root and a memoised one.
func acResolvers(root string) map[string]Resolver {
	return map[string]Resolver{"unmemoised": NewResolver(root), "memoised": NewResolver(root).Memoised()}
}

// acPlaces holds Place to each word's answer, from root, both ways.
func acPlaces(t *testing.T, root string, cases []struct{ in, want string }) {
	t.Helper()
	for name, res := range acResolvers(root) {
		for _, tc := range cases {
			got, ok := res.Place(tc.in)
			if !ok {
				t.Errorf("%s: Place(%q) could not place it, want %q", name, tc.in, tc.want)
				continue
			}
			if got != tc.want {
				t.Errorf("%s: Place(%q) = %q, want %q", name, tc.in, got, tc.want)
			}
		}
	}
}

// acLinked is a project with a link, lnk, to the reviews directory of a story
// in another checkout, and that story's directory.
func acLinked(t *testing.T, kind string) (root, records string) {
	t.Helper()
	root, other := project(t), project(t)
	records = filepath.Join(other, ".sdlc", "stories", "A-1")
	if err := os.MkdirAll(filepath.Join(records, "reviews", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	acLink(t, kind, filepath.Join(records, "reviews"), filepath.Join(root, "lnk"))
	return root, records
}

// The shape the CI run on Windows found: `lnk/../gate-record.json` is the
// record beside the reviews lnk leads to, in the other checkout, and not a
// file beside lnk in the project. Spelt with either separator, relative or
// absolute, the record there or not yet.
func TestAC1AWordThroughALinkAndThenDotDotIsPlacedWhereTheLinkLeads(t *testing.T) {
	for _, kind := range acLinkKinds {
		t.Run(kind, func(t *testing.T) {
			root, records := acLinked(t, kind)
			if err := os.WriteFile(filepath.Join(records, "there.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
			record := filepath.Join(records, "gate-record.json")
			acPlaces(t, root, []struct{ in, want string }{
				{"lnk/../gate-record.json", record},
				{uncleaned("lnk", "..", "gate-record.json"), record},
				{uncleaned(root, "lnk", "..", "gate-record.json"), record},
				{"lnk/../there.json", filepath.Join(records, "there.json")},
				{"lnk/../../A-1/gate-record.json", record},
				{"lnk/../reviews/new.md", filepath.Join(records, "reviews", "new.md")},
			})
		})
	}
}

// `..` at the top of the volume is the top of the volume, as it is in every
// shell, and the walk goes on from there through what is on disk: here, down
// to the project again, through lnk, and up out of what it leads to.
func TestAC1DotDotAtTheTopOfTheVolumeStaysThereAndTheWalkGoesOn(t *testing.T) {
	for _, kind := range acLinkKinds {
		t.Run(kind, func(t *testing.T) {
			root, records := acLinked(t, kind)
			volume := filepath.VolumeName(root)
			sep := string(filepath.Separator)
			below := strings.TrimPrefix(root[len(volume):], sep)
			acPlaces(t, root, []struct{ in, want string }{
				{uncleaned(volume, "..", "..", below, "lnk", "..", "gate-record.json"), filepath.Join(records, "gate-record.json")},
				{uncleaned(volume, "..", below, "lnk", "new.md"), filepath.Join(records, "reviews", "new.md")},
				{uncleaned(volume, "..", "..", below, "x.go"), filepath.Join(root, "x.go")},
			})
		})
	}
}

// `.` is no step at all, wherever it is in the word: before the link, between
// the link and `..`, and in the part that is not there yet.
func TestAC1DotSegmentsAreNoStepWhereverTheyAreInTheWord(t *testing.T) {
	for _, kind := range acLinkKinds {
		t.Run(kind, func(t *testing.T) {
			root, records := acLinked(t, kind)
			record := filepath.Join(records, "gate-record.json")
			acPlaces(t, root, []struct{ in, want string }{
				{"./lnk/./../gate-record.json", record},
				{"././lnk/.././././gate-record.json", record},
				{uncleaned(root, ".", "lnk", ".", "..", "gate-record.json"), record},
				{"lnk/./new.md", filepath.Join(records, "reviews", "new.md")},
				{"lnk/missing/./y", filepath.Join(records, "reviews", "missing", "y")},
				{"./x.go", filepath.Join(root, "x.go")},
			})
		})
	}
}

// `..` after a directory that is not a link is that directory's parent, in the
// project and inside what a link leads to alike: `lnk/inner/../..` is two
// steps up from inner where lnk leads, not from lnk.
func TestAC1DotDotAfterADirectoryThatIsNotALinkIsItsParent(t *testing.T) {
	for _, kind := range acLinkKinds {
		t.Run(kind, func(t *testing.T) {
			root, records := acLinked(t, kind)
			if err := os.MkdirAll(filepath.Join(root, "real", "sub"), 0o755); err != nil {
				t.Fatal(err)
			}
			record := filepath.Join(records, "gate-record.json")
			acPlaces(t, root, []struct{ in, want string }{
				{"real/sub/../file.go", filepath.Join(root, "real", "file.go")},
				{"real/sub/../../file.go", filepath.Join(root, "file.go")},
				{"real/sub/../../real/sub/x", filepath.Join(root, "real", "sub", "x")},
				{"real/../lnk/../gate-record.json", record},
				{"lnk/inner/../../gate-record.json", record},
				{"lnk/../reviews/inner/../../gate-record.json", record},
			})
		})
	}
}

// A link that leads nowhere, or round in a circle, cannot be followed, and the
// shell's write through it fails. The word is still placed, `..` taking apart
// the link by name, rather than left with no reading at all: the hook answers
// a word it cannot place, or a walk that panics, by letting it through.
func TestAC1ALinkThatCannotBeFollowedStillLeavesTheWordAReading(t *testing.T) {
	t.Run("symlink to nothing", func(t *testing.T) {
		root := project(t)
		acLink(t, "symlink", filepath.Join(root, "never-made"), filepath.Join(root, "gone"))
		acPlaces(t, root, []struct{ in, want string }{
			{"gone/../x", filepath.Join(root, "x")},
			{"gone/./../x", filepath.Join(root, "x")},
		})
	})
	t.Run("junction to nothing", func(t *testing.T) {
		root := project(t)
		target := filepath.Join(root, "removed")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatal(err)
		}
		acLink(t, "junction", target, filepath.Join(root, "gone"))
		if err := os.Remove(target); err != nil {
			t.Fatal(err)
		}
		acPlaces(t, root, []struct{ in, want string }{
			{"gone/../x", filepath.Join(root, "x")},
		})
	})
	t.Run("symlinks in a cycle", func(t *testing.T) {
		root := project(t)
		acLink(t, "symlink", filepath.Join(root, "c2"), filepath.Join(root, "c1"))
		acLink(t, "symlink", filepath.Join(root, "c1"), filepath.Join(root, "c2"))
		acPlaces(t, root, []struct{ in, want string }{
			{"c1/../x", filepath.Join(root, "x")},
			{"c1/../c2/../x", filepath.Join(root, "x")},
		})
	})
}
