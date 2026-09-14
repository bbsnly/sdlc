package pathrules

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func project(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root
}

func TestRelPutsPathsIntoRepositoryRelativeForm(t *testing.T) {
	root := project(t)
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"already relative", "internal/store/store.go", "internal/store/store.go"},
		{"dot prefixed", "./CLAUDE.md", "CLAUDE.md"},
		{"absolute inside", filepath.Join(root, "cmd", "sdlc", "main.go"), "cmd/sdlc/main.go"},
		{"the root itself", root, ""},
		{"redundant segments", "a/./b/../b/c.go", "a/b/c.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, outside := Rel(root, tc.in)
			if outside {
				t.Fatalf("Rel(%q) said outside", tc.in)
			}
			if got != tc.want {
				t.Errorf("Rel(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// macOS and Windows fold case, and filepath.Rel does not: the project's loop
// state, named with the project's path in capitals, was outside the project.
func TestRelFoldsCaseWhereTheFilesystemDoes(t *testing.T) {
	root := project(t)
	upper := filepath.Join(strings.ToUpper(root), ".sdlc", "state", "active")
	got, outside := Rel(root, upper)
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		if !outside {
			t.Errorf("on a filesystem that keeps case, %s was inside the project as %q", upper, got)
		}
		return
	}
	if outside || got != ".sdlc/state/active" {
		t.Errorf("Rel(%s) = %q, outside %v; want .sdlc/state/active inside", upper, got, outside)
	}
	if _, outside := Rel(root, strings.ToUpper(root)); outside {
		t.Error("the project's own path in capitals was outside it")
	}
	if _, outside := Rel(root, strings.ToUpper(root)+"-other"); !outside {
		t.Error("a sibling whose name starts with the project's was inside it")
	}
}

func TestRelReportsPathsThatLeaveTheRepository(t *testing.T) {
	root := project(t)
	for _, in := range []string{
		"../outside.txt",
		"a/../../outside.txt",
		filepath.Join(filepath.Dir(root), "sibling", "file.txt"),
	} {
		if _, outside := Rel(root, in); !outside {
			t.Errorf("Rel(%q) did not report leaving the repository", in)
		}
	}
}

// A symlink inside the repository pointing at a protected directory is the
// interesting case: the path written never names the directory being protected.
func TestRelFollowsASymlinkIntoAProtectedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevation on Windows")
	}
	root := project(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, ".git"), filepath.Join(root, "innocent")); err != nil {
		t.Fatal(err)
	}

	got, outside := Rel(root, "innocent/config")
	if outside {
		t.Fatal("a link inside the repository was read as leaving it")
	}
	if got != ".git/config" {
		t.Errorf("Rel = %q, want it resolved to .git/config", got)
	}
}

// The file being written usually does not exist yet, so resolution has to work
// on the part of the path that does.
func TestRelResolvesEvenWhenTheFileIsNotThereYet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevation on Windows")
	}
	root := project(t)
	target := filepath.Join(root, "real")
	if err := os.MkdirAll(filepath.Join(target, "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	got, outside := Rel(root, "link/deep/not-created-yet.go")
	if outside {
		t.Fatal("outside = true")
	}
	if got != "real/deep/not-created-yet.go" {
		t.Errorf("Rel = %q", got)
	}
}

func TestRelReportsASymlinkOutOfTheRepository(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need elevation on Windows")
	}
	root := project(t)
	elsewhere := project(t)
	if err := os.Symlink(elsewhere, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}

	if _, outside := Rel(root, "escape/secrets.env"); !outside {
		t.Error("a link out of the repository was not reported")
	}
}

func TestAbsJoinsOnlyWhatIsRelative(t *testing.T) {
	base := project(t)
	if got, ok := Abs(base, "a/b.go"); !ok || got != filepath.Join(base, "a", "b.go") {
		t.Errorf("Abs(relative) = %q, %v", got, ok)
	}
	inside := filepath.Join(base, "c.go")
	if got, ok := Abs(base, inside); !ok || got != inside {
		t.Errorf("Abs(absolute) = %q, %v", got, ok)
	}
}

// On Windows a path can be rooted without a drive, and filepath.IsAbs calls it
// relative. Joined onto the project, `\repo\CLAUDE.md` became `repo/CLAUDE.md`,
// which no rule protects, while the tool wrote the project's own CLAUDE.md.
func TestAPathRootedWithoutADriveIsOnTheProjectsDrive(t *testing.T) {
	root := project(t)
	volume := filepath.VolumeName(root)
	if volume == "" {
		t.Skip("only Windows has a path that is rooted and not absolute")
	}
	rooted := root[len(volume):]
	for in, want := range map[string]string{
		rooted + `\CLAUDE.md`:                            "CLAUDE.md",
		filepath.ToSlash(rooted) + "/.sdlc/state/active": ".sdlc/state/active",
	} {
		if got, outside := Rel(root, in); outside || got != want {
			t.Errorf("Rel(%q) = %q, outside %v; want %q", in, got, outside, want)
		}
	}
	// Relative to a working directory on that drive, which only the writer knows.
	if got, outside := Rel(root, volume+"CLAUDE.md"); !outside {
		t.Errorf("Rel(%q) = %q, inside; a drive-relative path cannot be placed", volume+"CLAUDE.md", got)
	}
}

// The file being written usually does not exist yet, and a path tens of
// thousands of levels deep fits in one tool call. Looking it up a level at a
// time from the far end took longer than the hook is given.
func TestADeepPathThatIsNotThereResolvesQuickly(t *testing.T) {
	root := project(t)
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(root, "a", "b", strings.Repeat("x"+string(filepath.Separator), 40000)+"f.go")
	start := time.Now()
	rel, outside := Rel(root, deep)
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("a path 40,000 levels deep took %s", took)
	}
	if outside || !strings.HasPrefix(rel, "a/b/x/x/") || !strings.HasSuffix(rel, "/f.go") {
		t.Errorf("Rel = %.40q..., outside %v", rel, outside)
	}
}

func TestRelOnAnEmptyPath(t *testing.T) {
	got, outside := Rel(project(t), "")
	if got != "" || outside {
		t.Errorf("Rel(\"\") = %q, %v", got, outside)
	}
}

func TestUnderIsDirectoryAware(t *testing.T) {
	for _, tc := range []struct {
		rel, dir string
		want     bool
	}{
		{".sdlc/state/active", ".sdlc", true},
		{".sdlc/state/active", ".sdlc/", true},
		{".sdlc", ".sdlc", true},
		{".sdlcfoo/x", ".sdlc", false},
		{".sdlc-other", ".sdlc", false},
		{"internal/x.go", "internal/store", false},
		{"anything", "", true},
		// Letters that fold to ASCII at a different byte length. Each of
		// these is the protected path on APFS, and each was let through.
		{".ſdlc/state/active", ".sdlc", true},
		{".sdlc/ſtate/active", ".sdlc/state", true},
		{".sdlc/stories/A-1/reviewſ/x.md", ".sdlc/stories/A-1/reviews", true},
		{"internal/Keys/x", "internal/keys", true},
		{".ſdlcfoo/x", ".sdlc", false},
		// Letters that fold to more than one letter. strings.EqualFold does
		// not fold these at all, and APFS does.
		{".sdlc/ﬆate/active", ".sdlc/state", true},
		{".sdlc/conﬁg.json", ".sdlc/config.json", true},
		// Windows drops a trailing dot or space from a name.
		{".sdlc./state/active", ".sdlc/state", true},
		{".sdlc /config.json", ".sdlc/config.json", true},
		// NTFS names a file's main stream `::$DATA`, in any case.
		{".sdlc/config.json::$DATA", ".sdlc/config.json", true},
		{".sdlc/stories/A-1/gate-record.json::$data", ".sdlc/stories/A-1/gate-record.json", true},
	} {
		if got := Under(tc.rel, tc.dir); got != tc.want {
			t.Errorf("Under(%q, %q) = %v, want %v", tc.rel, tc.dir, got, tc.want)
		}
	}
}

func TestUnderAny(t *testing.T) {
	if !UnderAny("CODEMAP.md", ".sdlc", "CODEMAP.md") {
		t.Error("UnderAny missed an exact match")
	}
	if UnderAny("src/main.go", ".sdlc", "CODEMAP.md") {
		t.Error("UnderAny matched something it should not")
	}
}
