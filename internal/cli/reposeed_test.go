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
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
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
		t.Helper()
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
	gitProjectInitialisedAt(t, root)
	return root
}

// gitProjectInitialisedAt is gitProjectInitialised in root, an empty
// directory, for a walked template's builder.
func gitProjectInitialisedAt(t *testing.T, root string) {
	t.Helper()
	materialise(t, root, initialisedTemplate.get(t, func(t *testing.T, scratch string) {
		t.Helper()
		wtGit(t, scratch, "init", "--quiet", "--initial-branch=main")
		initialised(t, scratch)
	}))
	chdir(t, root)
}

// US-055 AC-3: taking a story through the gates for real, gate by gate, was
// about seven in ten of the git processes this package started, and on
// Windows, where each one costs tens of milliseconds, the main reason the
// package did not finish inside go test's ten minutes (ANALYSIS.md, AC-3). A
// walked template is a story taken through the gates by the real commands
// once per package run, then copied into every test that starts from the same
// point, as the templates above are for `git init` and `sdlc init`. Because
// the real commands make it, a gate that grows a requirement still fails every
// test that starts past it (reach's promise). A test whose subject is the walk
// itself, TestAStoryGoesAllTheWayThrough among them, still walks for real.
//
// A walked story is not free of paths the way a plain `git init` is. The
// claim on it names the checkout that holds it, and a story worktree is tied
// to its repository by absolute paths both ways: the worktree's .git file and
// .git/worktrees/<name>/gitdir. A copy has every spelling of the template's
// checkout path rewritten to its own, and fails if a file still names the
// template's checkout. A test that read a record naming another test's
// checkout would be testing something else, not just running faster. Each
// checkout's index is then refreshed: a copied file has a new inode and new
// times, and git would read every tracked file as changed until it looked.

// walkedTemplate is one walked story, built at most once per package run: the
// files of the checkout it was built in, that checkout's path as t.TempDir
// spelled it and resolved, and what its builder noted on the way.
type walkedTemplate struct {
	once      sync.Once
	raw, root string
	files     []seedFile
	notes     map[string]string
}

// walkedTemplates holds each walked template by the name its helper gives it.
// The name has to say everything the builder does, because the first builder
// to run for a name is the only one that ever runs.
var walkedTemplates sync.Map

// walkedCopy is a copy of the walked template named key in a new directory of
// t's, and returns that directory. build makes the template, the first time
// anything asks for key, by taking a story through the real commands in the
// empty directory it is given. The test's commands run wherever they ran
// before: the helper that asked says where they go next.
func walkedCopy(t *testing.T, key string, build func(t *testing.T, root string)) string {
	t.Helper()
	root, _ := walkedCopyNoted(t, key, func(t *testing.T, root string) map[string]string {
		t.Helper()
		build(t, root)
		return nil
	})
	return root
}

// walkedCopyNoted is walkedCopy for a builder that notes what it read on the
// way, such as trunk's tip before the story started, and returns the notes
// with the copy. A test that read a value before the walk reads the same
// value here, not one read after the walk that only happens to agree.
func walkedCopyNoted(t *testing.T, key string, build func(t *testing.T, root string) map[string]string) (string, map[string]string) {
	t.Helper()
	v, _ := walkedTemplates.LoadOrStore(key, &walkedTemplate{})
	tpl, ok := v.(*walkedTemplate)
	if !ok {
		t.Fatalf("the template for %q is a %T, not a *walkedTemplate", key, v)
	}
	tpl.once.Do(func() {
		raw, root := tempDirSpellings(t)
		var notes map[string]string
		withNothingSet(t, func() { notes = build(t, root) })
		tpl.raw, tpl.root, tpl.notes = raw, root, notes
		tpl.files = captureTree(t, root)
	})
	if tpl.files == nil {
		t.Fatalf("the walked template %q was not made: the test that made it failed first", key)
	}

	raw, root := tempDirSpellings(t)
	rewrite := rootRewriter(tpl.raw, tpl.root, raw, root)
	files := make([]seedFile, len(tpl.files))
	for i, f := range tpl.files {
		if !f.dir && !inObjectStore(f.rel) {
			f.data = rewrite(f.data)
			if namesCheckout(f.data, tpl.raw, tpl.root) {
				t.Fatalf("the copy of the walked template %q still names the checkout it was built in, in %s:\n%s",
					key, f.rel, f.data)
			}
		}
		files[i] = f
	}
	materialise(t, root, files)
	for _, checkout := range checkoutsIn(t, root) {
		wtGit(t, checkout, "update-index", "-q", "--refresh")
	}
	return root, maps.Clone(tpl.notes)
}

// withNothingSet runs build with t's commands running where, and with what, a
// test that set nothing has them, and puts back whatever t had set afterwards.
// Otherwise a session, a directory or a HOME the test that happens to trigger
// a template had set would end up in every copy of it.
func withNothingSet(t *testing.T, build func()) {
	t.Helper()
	saved := invocationOf(t)
	if saved.env == nil {
		saved.env = map[string]string{}
	}
	ownInvocation(t, func(inv *invocation) { inv.dir, inv.env = packageDir(), map[string]string{} })
	defer ownInvocation(t, func(inv *invocation) { inv.dir, inv.env = saved.dir, saved.env })
	build()
}

// tempDirSpellings is a new directory of t's, as t.TempDir spelled it and as
// resolved. On macOS the two differ by /private; on Windows the first can hold
// an 8.3 short name for a directory on the way where the second has the long
// one.
func tempDirSpellings(t *testing.T) (raw, resolved string) {
	t.Helper()
	raw = t.TempDir()
	resolved = raw
	if r, err := filepath.EvalSymlinks(raw); err == nil {
		resolved = r
	}
	return raw, resolved
}

// pathSpellings is every way a file may hold path: as the platform spells it,
// slash-separated as git writes it on Windows, and with each backslash doubled
// as JSON escapes it. On other platforms the three are one.
func pathSpellings(path string) []string {
	native := filepath.Clean(path)
	out := []string{strings.ReplaceAll(native, `\`, `\\`)}
	for _, s := range []string{native, filepath.ToSlash(native)} {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// rootRewriter rewrites every spelling of a template's checkout, fromRaw or
// fromRoot, to the same spelling of a copy's, toRaw or toRoot. Windows paths
// are matched without regard to case, as Windows reads them.
func rootRewriter(fromRaw, fromRoot, toRaw, toRoot string) func([]byte) []byte {
	type pair struct {
		from *regexp.Regexp
		to   []byte
	}
	var pairs []pair
	add := func(from, to string) {
		froms, tos := pathSpellings(from), pathSpellings(to)
		for i := range froms {
			pattern := regexp.QuoteMeta(froms[i])
			if runtime.GOOS == "windows" {
				pattern = "(?i)" + pattern
			}
			pairs = append(pairs, pair{regexp.MustCompile(pattern), []byte(tos[i])})
		}
	}
	add(fromRoot, toRoot)
	if fromRaw != fromRoot {
		add(fromRaw, toRaw)
	}
	return func(data []byte) []byte {
		for _, p := range pairs {
			data = p.from.ReplaceAllLiteral(data, p.to)
		}
		return data
	}
}

// namesCheckout reports whether data still holds the last two parts of a
// template's checkout path, raw or resolved, in any spelling: a new directory
// of t.TempDir's under a directory named for the test with a random number, so
// what a copy has rewritten will not hold them unless something was missed.
func namesCheckout(data []byte, raw, root string) bool {
	for _, path := range []string{raw, root} {
		tail := filepath.Join(filepath.Base(filepath.Dir(path)), filepath.Base(path))
		for _, s := range pathSpellings(tail) {
			if runtime.GOOS == "windows" {
				if bytes.Contains(bytes.ToLower(data), bytes.ToLower([]byte(s))) {
					return true
				}
			} else if bytes.Contains(data, []byte(s)) {
				return true
			}
		}
	}
	return false
}

// inObjectStore reports whether rel, slash-separated, is under a git object
// store, whose files are compressed and never hold a path as written, or is a
// git index. An index can name its checkout in its untracked cache, which git
// drops by itself when the checkout's path is not the one it names; and a byte
// rewritten inside an index breaks its checksum. The refresh after the copy
// writes each index again.
func inObjectStore(rel string) bool {
	if rel == ".git/index" || (strings.HasPrefix(rel, ".git/worktrees/") && strings.HasSuffix(rel, "/index")) {
		return true
	}
	return strings.HasPrefix(rel, ".git/objects/") || strings.Contains(rel, "/.git/objects/")
}

// checkoutsIn is every checkout of the repository at root that is under root:
// root itself, and each linked worktree git has a record of. A worktree
// outside root was not captured with the template, and its copy would be a
// worktree git cannot find, so it fails the test instead.
func checkoutsIn(t *testing.T, root string) []string {
	t.Helper()
	out := []string{root}
	records, err := filepath.Glob(filepath.Join(root, ".git", "worktrees", "*", "gitdir"))
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		raw, err := os.ReadFile(record)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Dir(filepath.FromSlash(strings.TrimSpace(string(raw))))
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			t.Fatalf("%s names the worktree %s, outside the copy at %s", record, dir, root)
		}
		out = append(out, dir)
	}
	return out
}
