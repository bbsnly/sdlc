package pathrules

// US-029.1, step 13(a): Place is where a word lands on disk, with nothing
// cleaned before the links on the way are followed (red-team R2-5). Cleaning
// first read `link/../x` as x beside the link, where the filesystem writes x
// beside what the link leads to.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func mustPlace(t *testing.T, res Resolver, p string) string {
	t.Helper()
	got, ok := res.Place(p)
	if !ok {
		t.Fatalf("Place(%q) could not place it", p)
	}
	return got
}

func TestPlaceJoinsAWordToTheProjectAndTakesApartWhatIsNotThereByName(t *testing.T) {
	root := project(t)
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := NewResolver(root)
	for _, tc := range []struct{ in, want string }{
		{"notes.txt", filepath.Join(root, "notes.txt")},
		{"a/new/file.go", filepath.Join(root, "a", "new", "file.go")},
		{"missing/./b/../c.go", filepath.Join(root, "missing", "c.go")},
		{"a/../b.go", filepath.Join(root, "b.go")},
		{filepath.Join(root, "a", "x.go"), filepath.Join(root, "a", "x.go")},
	} {
		if got := mustPlace(t, res, tc.in); got != tc.want {
			t.Errorf("Place(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestPlaceCannotPlaceAnEmptyWord(t *testing.T) {
	if got, ok := NewResolver(project(t)).Place(""); ok {
		t.Errorf("Place(\"\") = %q, placed", got)
	}
}

// `..` past the top of the disk stays there, as it does in every shell.
func TestPlaceDoesNotClimbAboveTheRoot(t *testing.T) {
	root := project(t)
	volume := filepath.VolumeName(root)
	sep := string(filepath.Separator)
	up := strings.Repeat(".."+sep, strings.Count(root, sep)+3)
	got := mustPlace(t, NewResolver(root), up+"no-such-dir-for-place"+sep+"x")
	if want := volume + sep + "no-such-dir-for-place" + sep + "x"; got != want {
		t.Errorf("Place(%q) = %q, want %q", up+"...", got, want)
	}
}

// The links are the filesystem's to follow: a link as the last part that exists
// is where the rest goes, and `..` after it leaves what it leads to, not the
// link.
func TestPlaceFollowsLinksBeforeItReadsDotDot(t *testing.T) {
	root, other := project(t), project(t)
	target := filepath.Join(other, "records", "reviews")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "lnk")); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	res := NewResolver(root)
	for _, tc := range []struct{ in, want string }{
		{"lnk/new.md", filepath.Join(target, "new.md")},
		{"lnk/../gate-record.json", filepath.Join(other, "records", "gate-record.json")},
		{"lnk/../../x/../y", filepath.Join(other, "y")},
		{"lnk/not-there/../z", filepath.Join(target, "z")},
	} {
		if got := mustPlace(t, res, tc.in); got != tc.want {
			t.Errorf("Place(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A project reached through a link is placed where it really is, so a word in
// it compares with the checkouts found from git's files.
func TestPlaceAndRootAreTheProjectAsItIsOnDisk(t *testing.T) {
	onDisk := project(t)
	link := filepath.Join(project(t), "project")
	if err := os.Symlink(onDisk, link); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	res := NewResolver(link)
	if res.Root() != onDisk {
		t.Errorf("Root() = %q, want %q", res.Root(), onDisk)
	}
	if got := mustPlace(t, res, "src/main.go"); got != filepath.Join(onDisk, "src", "main.go") {
		t.Errorf("Place(src/main.go) = %q, want it under %s", got, onDisk)
	}
}

func TestPlaceOnWindowsNeedsADriveItCanName(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-relative paths exist only on Windows")
	}
	root := project(t)
	res := NewResolver(root)
	if got, ok := res.Place("Z:relative"); ok {
		t.Errorf("Place(Z:relative) = %q, placed on a drive's current directory nobody knows", got)
	}
	if got := mustPlace(t, res, `\top\x`); got != filepath.VolumeName(root)+`\top\x` {
		t.Errorf(`Place(\top\x) = %q, want it on the project's drive`, got)
	}
}

func TestWithinIsAPathRelativeToARootItIsIn(t *testing.T) {
	root := project(t)
	for _, tc := range []struct {
		p, rel string
		inside bool
	}{
		{root, ".", true},
		{filepath.Join(root, ".sdlc", "state", "active"), ".sdlc/state/active", true},
		{filepath.Dir(root), "", false},
		{filepath.Join(filepath.Dir(root), filepath.Base(root)+"-beside", "x"), "", false},
	} {
		rel, inside := Within(root, tc.p)
		if inside != tc.inside || tc.inside && rel != tc.rel {
			t.Errorf("Within(root, %q) = %q, %v; want %q, %v", tc.p, rel, inside, tc.rel, tc.inside)
		}
	}
	upper := filepath.Join(strings.ToUpper(root), ".SDLC", "State")
	rel, inside := Within(root, upper)
	if folds := runtime.GOOS == "darwin" || runtime.GOOS == "windows"; inside != folds ||
		folds && !strings.EqualFold(rel, ".sdlc/state") {
		t.Errorf("Within(root, %q) = %q, %v; a filesystem that folds case is inside, one that keeps it is not", upper, rel, inside)
	}
}
