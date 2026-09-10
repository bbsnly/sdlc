package pathrules

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
