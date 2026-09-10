package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// repo makes a directory that looks like a Git repository, and returns the path
// with symlinks resolved so that comparisons hold on macOS, where the temporary
// directory is reached through /var.
func repo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_WORK_TREE", "")
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeConfig(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(File)), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func codeOf(t *testing.T, err error) sdlcerr.Code {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("error is not an *sdlcerr.Error: %v", err)
	}
	return e.Code
}

func TestFindRootFromTheRootItself(t *testing.T) {
	root := repo(t)
	got, err := FindRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("FindRoot = %q, want %q", got, root)
	}
}

func TestFindRootFromDeepInside(t *testing.T) {
	root := repo(t)
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(deep)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("FindRoot = %q, want %q", got, root)
	}
}

// A worktree and a submodule both carry .git as a file rather than a directory.
func TestFindRootWhenGitIsAFile(t *testing.T) {
	t.Setenv("GIT_WORK_TREE", "")
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Errorf("FindRoot = %q, want %q", got, root)
	}
}

func TestFindRootOutsideAnyRepository(t *testing.T) {
	t.Setenv("GIT_WORK_TREE", "")
	dir := t.TempDir()
	_, err := FindRoot(dir)
	if err == nil {
		t.Fatal("FindRoot succeeded outside a repository")
	}
	if got := codeOf(t, err); got != sdlcerr.NotAGitRepo {
		t.Errorf("code = %s, want %s", got, sdlcerr.NotAGitRepo)
	}
	if !strings.Contains(err.Error(), "not a Git repository") {
		t.Errorf("error does not say what happened: %v", err)
	}
}

func TestFindRootHonoursGitWorkTree(t *testing.T) {
	elsewhere := t.TempDir()
	t.Setenv("GIT_WORK_TREE", elsewhere)
	got, err := FindRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != elsewhere {
		t.Errorf("FindRoot = %q, want the GIT_WORK_TREE %q", got, elsewhere)
	}
}

func TestLoadWithoutAConfig(t *testing.T) {
	root := repo(t)
	_, err := Load(root)
	if err == nil {
		t.Fatal("Load succeeded with no config file")
	}
	if got := codeOf(t, err); got != sdlcerr.NotInitialised {
		t.Errorf("code = %s, want %s", got, sdlcerr.NotInitialised)
	}
}

// A hand-edited file that names two settings must not silently zero the rest.
func TestLoadKeepsDefaultsForAbsentFields(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"thresholds":{"diff_size_cap":40}}`)

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Thresholds.DiffSizeCap != 40 {
		t.Errorf("DiffSizeCap = %d, want the file's 40", cfg.Thresholds.DiffSizeCap)
	}
	if cfg.Thresholds.CoverageMin != Default().Thresholds.CoverageMin {
		t.Errorf("CoverageMin = %v, want the default %v",
			cfg.Thresholds.CoverageMin, Default().Thresholds.CoverageMin)
	}
	if cfg.Backlog.Path != Default().Backlog.Path {
		t.Errorf("Backlog.Path = %q, want the default %q", cfg.Backlog.Path, Default().Backlog.Path)
	}
	if len(cfg.Paths.Tests.FileGlobs) == 0 {
		t.Error("Paths.Tests.FileGlobs was emptied by a file that did not mention it")
	}
}

func TestLoadOverridesWhatTheFileNames(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"backlog":{"path":"stories/backlog.json"},"git":{"trunk_branch":"trunk"}}`)

	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backlog.Path != "stories/backlog.json" {
		t.Errorf("Backlog.Path = %q", cfg.Backlog.Path)
	}
	if cfg.Git.TrunkBranch != "trunk" {
		t.Errorf("TrunkBranch = %q", cfg.Git.TrunkBranch)
	}
}

func TestLoadPointsAtTheLineThatBrokeTheJSON(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, "{\n  \"version\": 1,\n  \"backlog\": { \"path\": }\n}\n")

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load accepted broken JSON")
	}
	if got := codeOf(t, err); got != sdlcerr.ConfigUnreadable {
		t.Errorf("code = %s, want %s", got, sdlcerr.ConfigUnreadable)
	}
	var e *sdlcerr.Error
	errors.As(err, &e)
	if !strings.Contains(e.Why, "line 3") {
		t.Errorf("why = %q, want it to name line 3", e.Why)
	}
}

func TestBacklogPathFallsBackWhenTheFileEmptiesIt(t *testing.T) {
	cfg := Default()
	cfg.Backlog.Path = ""
	got := cfg.BacklogPath(filepath.FromSlash("/repo"))
	want := filepath.Join(filepath.FromSlash("/repo"), "user_stories.json")
	if got != want {
		t.Errorf("BacklogPath = %q, want %q", got, want)
	}
}

func TestOpenResolvesRootAndConfigTogether(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"git":{"trunk_branch":"main"}}`)
	deep := filepath.Join(root, "cmd", "app")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := Open(deep)
	if err != nil {
		t.Fatal(err)
	}
	if p.Root != root {
		t.Errorf("Root = %q, want %q", p.Root, root)
	}
	if got, want := p.Path(Dir, "state"), filepath.Join(root, Dir, "state"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}
