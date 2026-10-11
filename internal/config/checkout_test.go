package config

// US-029.1: where a checkout's repository keeps what every checkout shares.
// The expectations are the plan's step 2 and THREATS.md's T3 and T12: the main
// checkout is found from git's own files and never from git's variables, a
// repository with no main checkout gets SDLC-E0052, and what the hook protects
// (the candidates) survives every way of making the main checkout unfindable.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

func coGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func coTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

func coWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// coCommit makes the one commit a worktree needs to be cut from.
func coCommit(t *testing.T, dir string) {
	t.Helper()
	coWrite(t, filepath.Join(dir, "README.md"), "a project\n")
	coGit(t, dir, "add", "-A")
	coGit(t, dir, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "the project")
}

// coRepository is a main checkout with one commit in it.
func coRepository(t *testing.T) string {
	t.Helper()
	main := coTempDir(t)
	coGit(t, main, "init", "--quiet", "--initial-branch=main")
	coCommit(t, main)
	return main
}

// coWorktree adds a linked worktree of repo, which may be a main checkout or a
// bare repository, on a branch of its own.
func coWorktree(t *testing.T, repo, name string) string {
	t.Helper()
	dir := filepath.Join(coTempDir(t), name)
	coGit(t, repo, "worktree", "add", "--quiet", "-b", name, dir, "main")
	return dir
}

func coFind(t *testing.T, root string) Checkout {
	t.Helper()
	co, err := FindCheckout(root)
	if err != nil {
		t.Fatalf("FindCheckout(%s): %v", root, err)
	}
	return co
}

// coRefused is FindCheckout's SDLC-E0052, with the why it gave.
func coRefused(t *testing.T, root string) (Checkout, string) {
	t.Helper()
	co, err := FindCheckout(root)
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != sdlcerr.NoMainCheckout {
		t.Fatalf("FindCheckout(%s) = %+v, %v; want SDLC-E0052", root, co, err)
	}
	if co.Main != "" {
		t.Errorf("FindCheckout refused and still named %s as the main checkout", co.Main)
	}
	return co, e.Why
}

func coIsCandidate(co Checkout, dir string) bool {
	dirs, _ := co.Candidates()
	return slices.ContainsFunc(dirs, func(d string) bool { return SamePath(d, dir) })
}

func TestFindCheckoutInAMainCheckoutIsThatCheckoutAndItsGitDirectory(t *testing.T) {
	main := coRepository(t)
	co := coFind(t, main)
	dotgit := filepath.Join(main, ".git")
	if co.Root != main || co.Main != main || co.Common != dotgit || co.GitDir != dotgit {
		t.Errorf("FindCheckout(main) = %+v, want root and main %s, common and git dir %s", co, main, dotgit)
	}
	if co.Linked() || co.Pointer() {
		t.Errorf("a main checkout reads as linked %v, pointer %v", co.Linked(), co.Pointer())
	}
	if dirs, readable := co.Candidates(); !readable || len(dirs) != 1 || dirs[0] != dotgit {
		t.Errorf("candidates %v readable %v, want only %s", dirs, readable, dotgit)
	}
	// AC-10: a repository with no linked worktree is a single checkout.
	if co.HasWorktrees() || len(co.Worktrees()) != 0 {
		t.Errorf("a repository with no linked worktree has worktrees %v", co.Worktrees())
	}
}

// The tests' own layout, and any project not in git: the store has always kept
// everything in the directory itself.
func TestFindCheckoutOutsideARepositoryIsTheDirectoryItself(t *testing.T) {
	dir := coTempDir(t)
	co := coFind(t, dir)
	if co.Main != dir || co.Common != "" || co.GitDir != "" || co.Linked() || co.Pointer() {
		t.Errorf("FindCheckout(no repository) = %+v, want %s as its own main checkout and nothing else", co, dir)
	}
	if dirs, readable := co.Candidates(); len(dirs) != 0 || !readable {
		t.Errorf("candidates %v readable %v: a directory with no .git is not the unplaced case", dirs, readable)
	}
	if co.HasWorktrees() || co.Worktrees() != nil {
		t.Error("a directory outside git has worktrees")
	}
}

func TestAC1FindCheckoutInALinkedWorktreeFindsTheMainCheckout(t *testing.T) {
	main := coRepository(t)
	wt := coWorktree(t, main, "wt")
	dotgit := filepath.Join(main, ".git")

	co := coFind(t, wt)
	if co.Root != wt || co.Main != main || co.Common != dotgit ||
		co.GitDir != filepath.Join(dotgit, "worktrees", "wt") {
		t.Errorf("FindCheckout(worktree) = %+v, want main %s, common %s, git dir %s", co, main, dotgit,
			filepath.Join(dotgit, "worktrees", "wt"))
	}
	if !co.Linked() || !co.Pointer() {
		t.Errorf("a linked worktree reads as linked %v, pointer %v", co.Linked(), co.Pointer())
	}
	if !coIsCandidate(co, dotgit) {
		t.Errorf("the main checkout's git directory is not a candidate: %v", co.candidates)
	}
	for _, from := range []string{main, wt} {
		c := coFind(t, from)
		if got := c.Worktrees(); len(got) != 1 || !SamePath(got[0], wt) {
			t.Errorf("Worktrees() from %s = %v, want [%s]", from, got, wt)
		}
		if !c.HasWorktrees() {
			t.Errorf("HasWorktrees() from %s is false with a linked worktree", from)
		}
	}
}

// T3: git's variables name a directory anybody can make. FindCheckout reads
// git's own files and nothing else.
func TestT3GitsVariablesDoNotMoveTheMainCheckout(t *testing.T) {
	main := coRepository(t)
	wt := coWorktree(t, main, "wt")
	fake := coTempDir(t)
	coGit(t, fake, "init", "--quiet", "--initial-branch=main")

	for _, name := range []string{"GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE"} {
		t.Run("T3 "+name, func(t *testing.T) {
			value := filepath.Join(fake, ".git")
			if name == "GIT_WORK_TREE" {
				value = fake
			}
			t.Setenv(name, value)
			co := coFind(t, wt)
			if co.Main != main || co.Common != filepath.Join(main, ".git") {
				t.Errorf("with %s=%s the main checkout is %s and the common directory %s", name, value, co.Main, co.Common)
			}
			if coIsCandidate(co, filepath.Join(fake, ".git")) {
				t.Errorf("with %s set, the fake's git directory is a candidate", name)
			}
		})
	}
}

// AC-9, D6: a bare repository has no main checkout, and its worktrees keep no
// backlog. What the hook protects is still found.
func TestAC9AWorktreeOfABareRepositoryHasNoMainCheckout(t *testing.T) {
	main := coRepository(t)
	bare := filepath.Join(coTempDir(t), "repo.git")
	coGit(t, main, "clone", "--quiet", "--bare", main, bare)
	wt := coWorktree(t, bare, "wt")

	co, why := coRefused(t, wt)
	if !strings.Contains(why, "bare") {
		t.Errorf("the why does not say the repository is bare: %q", why)
	}
	if !coIsCandidate(co, bare) {
		t.Errorf("the bare repository is not a candidate, so nothing in it would be protected: %v", co.candidates)
	}
}

// AC-9, D6: a repository made with --separate-git-dir has a main checkout the
// worktree cannot find from git's files. The main checkout itself works as a
// single checkout does.
func TestAC9AWorktreeOfASeparateGitDirectoryHasNoMainCheckoutButItsMainCheckoutWorks(t *testing.T) {
	main, gitDir := coTempDir(t), filepath.Join(coTempDir(t), "gitdir")
	coGit(t, main, "init", "--quiet", "--initial-branch=main", "--separate-git-dir="+gitDir)
	coCommit(t, main)

	co := coFind(t, main)
	if co.Main != main || co.Common != gitDir || co.GitDir != gitDir || !co.Pointer() || co.Linked() {
		t.Errorf("FindCheckout(separate main) = %+v, want main %s and git directory %s, a pointer, not linked", co, main, gitDir)
	}

	wt := coWorktree(t, main, "wt")
	co, why := coRefused(t, wt)
	if !strings.Contains(why, "separate") {
		t.Errorf("the why does not name a separate git directory: %q", why)
	}
	if !coIsCandidate(co, gitDir) {
		t.Errorf("the separate git directory is not a candidate: %v", co.candidates)
	}
}

// T12 and B1: core.bare makes the main checkout unfindable to the CLI, and must
// not take the protection with it.
func TestT12CoreBareRefusesTheWorktreeAndLeavesTheMainCheckoutsGitDirectoryACandidate(t *testing.T) {
	main := coRepository(t)
	wt := coWorktree(t, main, "wt")
	coGit(t, main, "config", "core.bare", "true")

	co, why := coRefused(t, wt)
	if !strings.Contains(why, "bare") {
		t.Errorf("the why does not say the repository reads as bare: %q", why)
	}
	if dirs, readable := co.Candidates(); !readable || !coIsCandidate(co, filepath.Join(main, ".git")) {
		t.Errorf("candidates %v readable %v: core.bare switched the protection off", dirs, readable)
	}
}

// T12: a rewritten commondir must not take the protection of the real common
// directory with it, and must not make another directory the main checkout.
func TestT12ARewrittenCommondirIsNoMainCheckoutAndTheRealCommonDirectoryStaysACandidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		to   func(t *testing.T) string
	}{
		{"T12 commondir leading nowhere", func(t *testing.T) string {
			t.Helper()
			return filepath.Join(coTempDir(t), "nowhere")
		}},
		{"T12 commondir leading to another repository's .git", func(t *testing.T) string {
			t.Helper()
			fake := coTempDir(t)
			coGit(t, fake, "init", "--quiet", "--initial-branch=main")
			return filepath.Join(fake, ".git")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main := coRepository(t)
			wt := coWorktree(t, main, "wt")
			coWrite(t, filepath.Join(main, ".git", "worktrees", "wt", "commondir"), tc.to(t)+"\n")

			co, _ := coRefused(t, wt)
			if dirs, readable := co.Candidates(); !readable || !coIsCandidate(co, filepath.Join(main, ".git")) {
				t.Errorf("candidates %v readable %v: the rewrite took the real common directory's protection", dirs, readable)
			}
		})
	}
}

// A .git file that leads nowhere is no main checkout: reading the directory as
// its own main checkout would read the copy of the backlog git put in a
// worktree whose main checkout moved (D5), and it is the unplaced case, where
// no candidate can be read.
func TestADotGitFileThatLeadsNowhereIsNoMainCheckoutAndTheUnplacedCase(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"a .git file that names no git directory", "this is not a pointer\n"},
		{"a .git file with an empty gitdir", "gitdir: \n"},
		{"a .git file leading to a directory that is not there", "gitdir: " + filepath.Join(coTempDir(t), "gone") + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := coTempDir(t)
			coWrite(t, filepath.Join(dir, ".git"), tc.body)
			co, _ := coRefused(t, dir)
			if !co.Pointer() {
				t.Error("a .git file does not read as a pointer")
			}
			if dirs, readable := co.Candidates(); readable || len(dirs) != 0 {
				t.Errorf("candidates %v readable %v, want the unplaced case", dirs, readable)
			}
		})
	}
}

// worktree.useRelativePaths writes both pointers relative: the .git file to
// the worktree, and the entry's gitdir to the entry's own directory.
func TestRelativePointersAreReadAgainstTheirOwnDirectories(t *testing.T) {
	main := coRepository(t)
	wt := coWorktree(t, main, "wt")
	entry := filepath.Join(main, ".git", "worktrees", "wt")

	toEntry, err := filepath.Rel(wt, entry)
	if err != nil {
		t.Fatal(err)
	}
	toWorktree, err := filepath.Rel(entry, filepath.Join(wt, ".git"))
	if err != nil {
		t.Fatal(err)
	}
	coWrite(t, filepath.Join(wt, ".git"), "gitdir: "+filepath.ToSlash(toEntry)+"\n")
	coWrite(t, filepath.Join(entry, "gitdir"), filepath.ToSlash(toWorktree)+"\n")

	co := coFind(t, wt)
	if co.Main != main || !SamePath(co.GitDir, entry) {
		t.Errorf("with relative pointers FindCheckout = %+v, want main %s and git dir %s", co, main, entry)
	}
	if got := co.Worktrees(); len(got) != 1 || !SamePath(got[0], wt) {
		t.Errorf("Worktrees() = %v, want [%s]", got, wt)
	}
}

// A worktree deleted without `git worktree remove` is still an entry, and a
// story it held is still held until a person releases it (AC-8).
func TestWorktreesListsAnEntryWhoseWorktreeIsGone(t *testing.T) {
	main := coRepository(t)
	wt := coWorktree(t, main, "wt")
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}
	co := coFind(t, main)
	if got := co.Worktrees(); len(got) != 1 || filepath.Clean(got[0]) != wt {
		t.Errorf("Worktrees() = %v, want the gone worktree %s", got, wt)
	}
	if !co.HasWorktrees() {
		t.Error("HasWorktrees() is false with a prunable entry")
	}
}

// Whether a repository is bare is git's answer, read from the common
// configuration as git reads it: the last value set wins, the key and the
// section are read without regard to case, a key with no value is true, and
// core.bare in any other section is not core.bare.
func TestAC9CoreBareIsReadAsGitReadsIt(t *testing.T) {
	main := coRepository(t)
	wt := coWorktree(t, main, "wt")
	config := filepath.Join(main, ".git", "config")
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	// Without the core.bare = false git init wrote, so that each case is the
	// only setting there is unless it says otherwise.
	var original strings.Builder
	for line := range strings.Lines(string(raw)) {
		if key, _, _ := strings.Cut(strings.TrimSpace(line), "="); strings.TrimSpace(key) != "bare" {
			original.WriteString(line)
		}
	}
	for _, tc := range []struct {
		name, body string
		bare       bool
	}{
		{"bare = true", "[core]\n\tbare = true\n", true},
		{"a key with no value", "[core]\n\tbare\n", true},
		{"yes", "[core]\n\tbare = yes\n", true},
		{"any case", "[CORE]\n\tBare = TRUE\n", true},
		{"a comment after the value", "[core]\n\tbare = true ; set by hand\n", true},
		{"bare = false", "[core]\n\tbare = false\n", false},
		{"off", "[core]\n\tbare = off\n", false},
		{"in another section", "[remote \"origin\"]\n\tbare = true\n", false},
		{"false, then true in a second core section", "[core]\n\tbare = false\n[core]\n\tbare = true\n", true},
		{"true, then false in the same section", "[core]\n\tbare = true\n\tbare = false\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coWrite(t, config, original.String()+tc.body)
			t.Cleanup(func() { coWrite(t, config, original.String()) })
			if says := coGit(t, main, "rev-parse", "--is-bare-repository") == "true"; says != tc.bare {
				t.Fatalf("fixture: git reads %q as bare=%v, not %v", tc.body, says, tc.bare)
			}
			co, err := FindCheckout(wt)
			var e *sdlcerr.Error
			refused := errors.As(err, &e) && e.Code == sdlcerr.NoMainCheckout
			switch {
			case tc.bare && !refused:
				t.Errorf("git reads the repository as bare, and FindCheckout found main %s (%v)", co.Main, err)
			case !tc.bare && (err != nil || co.Main != main):
				t.Errorf("git reads the repository as not bare, and FindCheckout = %+v, %v", co, err)
			}
		})
	}
}

func TestSamePathComparesAsTheFilesystemDoes(t *testing.T) {
	dir := coTempDir(t)
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if !SamePath(a, a+string(filepath.Separator)+".") {
		t.Error("a directory spelled with a trailing . is not itself")
	}
	if SamePath(a, b) {
		t.Error("two directories are the same")
	}
	if SamePath("", a) || SamePath(a, "") || SamePath("", "") {
		t.Error("an empty path is the same as something")
	}
	missing := filepath.Join(dir, "Gone", "X")
	folds := runtime.GOOS == "darwin" || runtime.GOOS == "windows"
	if got := SamePath(missing, strings.ToLower(missing)); got != folds {
		t.Errorf("two spellings of a path that is not there differ only in case: same %v, want %v on %s", got, folds, runtime.GOOS)
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(a, link); err != nil {
		t.Skipf("this platform will not make a link here: %v", err)
	}
	if !SamePath(link, a) {
		t.Error("a directory and a link to it are not the same")
	}
}
