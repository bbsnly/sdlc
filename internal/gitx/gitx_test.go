package gitx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	run(t, root, "init", "--quiet")
	return root
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The freeze has to see a test file that was written a moment ago and never
// committed, and must not see build output. Both halves are why this asks git
// instead of walking the tree.
func TestTheWorkingTreeIsTrackedFilesPlusWhatIsNotIgnored(t *testing.T) {
	root := repo(t)
	write(t, root, ".gitignore", "dist/\n*.log\n")
	write(t, root, "main.go", "package main\n")
	run(t, root, "add", ".")
	run(t, root, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "--quiet", "-m", "first")

	write(t, root, "internal/x_test.go", "package internal\n")
	write(t, root, "dist/binary", "\x7fELF")
	write(t, root, "debug.log", "noise")

	got, err := Files(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".gitignore", "main.go", "internal/x_test.go"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s is missing from %v", want, got)
		}
	}
	for _, unwanted := range []string{"dist/binary", "debug.log"} {
		if slices.Contains(got, unwanted) {
			t.Errorf("%s is ignored but was listed", unwanted)
		}
	}
	if !slices.IsSorted(got) {
		t.Errorf("the listing is not sorted: %v", got)
	}
}

func TestOutsideARepositoryTheFailureExplainsItself(t *testing.T) {
	_, err := Files(t.Context(), t.TempDir())
	if err == nil {
		t.Fatal("a directory that is not a repository listed files")
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("the error carries no code: %v", err)
	}
	if e.Code != sdlcerr.RepositoryUnreadable {
		t.Errorf("code = %s", e.Code)
	}
	if !strings.Contains(sdlcerr.Render(err), "git") {
		t.Errorf("the failure does not mention git: %s", sdlcerr.Render(err))
	}
}
