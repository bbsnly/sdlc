package cli

// US-050 AC-8: about a hundred of this package's tests begin from an empty
// repository that `git init` and `sdlc init` turn into the same files every
// time, some of them then committing those files unchanged. Running both for
// real, once per test, is most of what made the package slow (ANALYSIS.md,
// AC-8). The first test that needs one of these starting points still makes
// it for real, in a scratch directory under its own t.TempDir(); every later
// one gets an exact copy of the files that made, which costs a handful of
// writes rather than a git process and sdlc init's own work each time.
//
// Nothing here changes what a test starts from: the files, refs and
// configuration a copy gets are the ones `git init`/`sdlc init` would have
// written there, byte for byte, because that is what was captured. A plain
// `git init` here never writes an absolute path into what it makes (that is
// `git worktree add`'s doing, not plain init's), so a copy under another
// temporary directory is as good as the original.

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// seedFile is one file or directory of a captured template tree, relative to
// its root.
type seedFile struct {
	rel  string
	dir  bool
	mode fs.FileMode
	data []byte
}

// templateCache holds one template, built at most once per package run.
type templateCache struct {
	once  sync.Once
	files []seedFile
}

// get returns the template's files, running build to make them for real the
// first time anything asks for this cache and never again. Tests that ask while
// it is being built wait for it.
//
// build runs in a scratch directory of its own, under the test that happens
// to trigger it, with nothing that test set: it runs its commands in the
// directory it is given, and leaves the test where it was.
func (c *templateCache) get(t *testing.T, build func(t *testing.T, root string)) []seedFile {
	t.Helper()
	c.once.Do(func() {
		root := wtTempDir(t)
		build(t, root)
		c.files = captureTree(t, root)
	})
	if c.files == nil {
		t.Fatal("the template repository was not made: the test that made it failed first")
	}
	return c.files
}

// captureTree reads every file and directory under root so that it can be
// written out again elsewhere.
func captureTree(t *testing.T, root string) []seedFile {
	t.Helper()
	var files []seedFile
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			files = append(files, seedFile{rel: rel, dir: true, mode: 0o755})
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, seedFile{rel: rel, mode: info.Mode().Perm(), data: data})
		return nil
	})
	if err != nil {
		t.Fatalf("reading the template tree at %s: %v", root, err)
	}
	return files
}

// materialise writes a captured template into dst, which must already exist.
func materialise(t *testing.T, dst string, files []seedFile) {
	t.Helper()
	for _, f := range files {
		path := filepath.Join(dst, filepath.FromSlash(f.rel))
		if f.dir {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, f.data, f.mode); err != nil {
			t.Fatal(err)
		}
	}
}

// gitInitTemplate is a bare `git init` followed by `sdlc init`, with nothing
// committed: the starting point wtRepositoryWith builds on before its own
// stories, configuration and first commit.
var gitInitTemplate templateCache

// seedGitInit materialises gitInitTemplate into root, an empty directory.
func seedGitInit(t *testing.T, root string) {
	t.Helper()
	materialise(t, root, gitInitTemplate.get(t, func(t *testing.T, scratch string) {
		wtGit(t, scratch, "init", "--quiet", "--initial-branch=main")
		runIn(t, scratch, "init")
	}))
}

// initialisedTemplate is gitProject(t) followed by initialised(t, root) as it
// used to run for real every time: `git init`, `sdlc init`, story worktrees
// turned off, and everything committed.
var initialisedTemplate templateCache

// gitProjectInitialised is gitProject(t) followed by initialised(t, root): a
// real repository, with sdlc's own init already run, story worktrees off and
// everything committed, which is where most of this package's tests begin. The
// test's commands run in it.
func gitProjectInitialised(t *testing.T) string {
	t.Helper()
	root := wtTempDir(t)
	materialise(t, root, initialisedTemplate.get(t, func(t *testing.T, scratch string) {
		wtGit(t, scratch, "init", "--quiet", "--initial-branch=main")
		initialised(t, scratch)
	}))
	chdir(t, root)
	return root
}
