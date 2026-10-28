package cli

// The helpers that read git state from files (gitfiles_test.go) answer what
// tests assert on, so each answer is held to git's own, in every shape of
// repository the loop makes: refs loose and packed, a loose ref over a packed
// one, a symbolic and a detached HEAD, and a linked worktree, where HEAD is
// the worktree's and the refs are the repository's.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
)

// gfInit makes a repository at dir with two commits on main.
func gfInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	swGit(t, dir, "init", "--quiet", "--initial-branch=main")
	swCommitFile(t, dir, "a.txt", "one\n", "one")
	swCommitFile(t, dir, "a.txt", "two\n", "two")
}

// gfGitWorktrees is the working trees `git worktree list` gives for dir.
func gfGitWorktrees(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	for line := range strings.Lines(swGit(t, dir, "worktree", "list", "--porcelain")) {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
			out = append(out, filepath.Clean(p))
		}
	}
	return out
}

// gfAgree holds each helper's answer for the checkout at dir to git's. The
// branches are every one the repository has; it also asks about names it has
// not. Each answer is also required to come from the files, so that a helper
// that always fell back to git could not pass for being right.
func gfAgree(t *testing.T, stage, dir string, branches []string) {
	t.Helper()
	fail := func(what, got, want string) {
		t.Errorf("%s, in %s: %s read from files is %q, git says %q", stage, dir, what, got, want)
	}

	revs := []string{"HEAD"}
	for _, b := range branches {
		revs = append(revs, "refs/heads/"+b)
	}
	for _, rev := range revs {
		want := swGit(t, dir, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
		got, ok := gfRev(dir, rev)
		if !ok {
			t.Errorf("%s, in %s: %s was left to git, so the file reader was not tried", stage, dir, rev)
		}
		if ok && got != want {
			fail(rev, got, want)
		}
		if got := swRev(t, dir, rev); got != want {
			fail("swRev("+rev+")", got, want)
		}
	}
	// What cannot be read from files is git's, and is not claimed.
	for _, rev := range []string{"HEAD~1", "HEAD^{tree}", "main", "refs/tags/v1", "HEAD^", "refs/heads/../x", "@", "refs/heads/main@{1}"} {
		if id, ok := gfRev(dir, rev); ok {
			t.Errorf("%s, in %s: %s was read from files as %s", stage, dir, rev, id)
		}
	}

	for _, b := range append(slices.Clone(branches), "nothere", "story", "story/US-001/deeper", "Main", "bad..name", "x.lock") {
		_, wantOK := swGitTry(t, dir, "show-ref", "--verify", "--quiet", "refs/heads/"+b)
		if got := swHasBranch(t, dir, b); got != wantOK {
			t.Errorf("%s, in %s: swHasBranch(%q) is %v, git says %v", stage, dir, b, got, wantOK)
		}
		if has, ok := gfHasBranch(dir, b); slices.Contains(branches, b) && (!ok || !has) {
			t.Errorf("%s, in %s: branch %q was not read from files (has %v, ok %v)", stage, dir, b, has, ok)
		}
	}

	wantRefs := swGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
	if got, ok := gfRefs(dir); ok && got != wantRefs {
		fail("the branches", got, wantRefs)
	} else if !ok {
		t.Errorf("%s, in %s: the branches were left to git", stage, dir)
	}
	if got := swRefs(t, dir); got != wantRefs {
		fail("swRefs", got, wantRefs)
	}

	want := gfGitWorktrees(t, dir)
	got := swWorktrees(t, dir)
	if len(got) != len(want) || (len(want) > 0 && !config.SamePath(got[0], want[0])) {
		t.Errorf("%s, in %s: the working trees read from files are %q, git lists %q", stage, dir, got, want)
	} else {
		// Git sorts the linked ones in its own way; what has to agree is the set.
		for _, w := range want[1:] {
			if !slices.ContainsFunc(got, func(p string) bool { return config.SamePath(p, w) }) {
				t.Errorf("%s, in %s: git lists %s as a working tree and the files do not: %q", stage, dir, w, got)
			}
		}
		if _, ok := gfWorktrees(dir); !ok {
			t.Errorf("%s, in %s: the working trees were left to git", stage, dir)
		}
	}
	for _, w := range want {
		if !swListed(t, dir, w) {
			t.Errorf("%s, in %s: swListed does not find %s, which git lists", stage, dir, w)
		}
	}
	if swListed(t, dir, filepath.Join(dir, "elsewhere")) {
		t.Errorf("%s, in %s: swListed finds a directory git does not list", stage, dir)
	}

	wantOn, _ := swGitTry(t, dir, "symbolic-ref", "--short", "HEAD")
	if got := swBranchOn(t, dir); got != wantOn {
		fail("the branch checked out", got, wantOn)
	}
	if on, ok := gfBranchOn(dir); ok && on != wantOn {
		fail("the branch checked out (files)", on, wantOn)
	}

	stashes := swGit(t, dir, "stash", "list")
	if log, ok := gfStashes(dir); !ok || (log == "") != (stashes == "") {
		t.Errorf("%s, in %s: the stash reflog is %q (read %v) and git lists %q", stage, dir, log, ok, stashes)
	}

	for _, name := range []string{"MERGE_HEAD", "rebase-merge", "CHERRY_PICK_HEAD"} {
		wantPath := swGit(t, dir, "rev-parse", "--git-path", name)
		if !filepath.IsAbs(wantPath) {
			wantPath = filepath.Join(dir, wantPath)
		}
		if p, ok := gfOwnPath(dir, name); !ok || !config.SamePath(filepath.Dir(p), filepath.Dir(wantPath)) || filepath.Base(p) != filepath.Base(wantPath) {
			t.Errorf("%s, in %s: %s is at %q (read %v), git says %q", stage, dir, name, p, ok, wantPath)
		}
	}
}

func TestTheFileReadingHelpersAgreeWithGit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	main := filepath.Join(root, "main")
	linked := filepath.Join(root, "linked")
	detached := filepath.Join(root, "detached")
	gfInit(t, main)
	if err := os.MkdirAll(filepath.Join(main, "sub", "deeper"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Every ref loose, a story branch with a slash in its name.
	swGit(t, main, "branch", "story/US-001")
	swGit(t, main, "branch", "plain")
	branches := []string{"main", "story/US-001", "plain"}
	// A directory below the checkout's root is asked about in the first and the
	// last stage only: each stage costs a few dozen git processes.
	deeper := filepath.Join(main, "sub", "deeper")
	dirs := func(below bool) []string {
		if below {
			return []string{main, deeper, linked, detached}
		}
		return []string{main, linked, detached}
	}
	swGit(t, main, "worktree", "add", "--quiet", "-b", "story/US-002", linked)
	swGit(t, main, "worktree", "add", "--quiet", "--detach", detached, "HEAD~1")
	branches = append(branches, "story/US-002")
	for _, dir := range dirs(true) {
		gfAgree(t, "with every ref loose", dir, branches)
	}

	// Packed: the refs move out of their files into packed-refs.
	swGit(t, main, "pack-refs", "--all")
	for _, dir := range dirs(false) {
		gfAgree(t, "with every ref packed", dir, branches)
	}

	// A new loose ref beside the packed ones, and a packed branch moved on,
	// which makes the loose file the truth and packed-refs stale.
	swGit(t, main, "branch", "loose")
	branches = append(branches, "loose")
	moved := swCommitFile(t, linked, "b.txt", "linked\n", "on the linked worktree")
	swGit(t, main, "branch", "-f", "plain", moved)
	if got := swRev(t, main, "refs/heads/plain"); got != moved {
		t.Errorf("a packed branch moved on reads as %s, want the loose ref's %s", got, moved)
	}
	for _, dir := range dirs(false) {
		gfAgree(t, "with loose refs over packed ones", dir, branches)
	}

	// The checkouts' HEADs are their own: main moves to another branch, the
	// linked one commits again, and the detached one moves to a branch tip.
	swGit(t, main, "switch", "--quiet", "story/US-001")
	again := swCommitFile(t, linked, "c.txt", "again\n", "again")
	swGit(t, detached, "checkout", "--quiet", "--detach", "loose")
	for _, tc := range []struct {
		dir, head string
	}{{main, swRev(t, main, "refs/heads/story/US-001")}, {linked, again}, {detached, swRev(t, main, "refs/heads/loose")}} {
		if got := swRev(t, tc.dir, "HEAD"); got != tc.head {
			t.Errorf("HEAD in %s is %s, want %s: HEAD is each worktree's own", tc.dir, got, tc.head)
		}
	}
	for _, dir := range dirs(false) {
		gfAgree(t, "after the checkouts moved", dir, branches)
	}

	// A branch deleted from loose and packed refs alike, and a stash.
	swGit(t, main, "branch", "-D", "plain")
	branches = []string{"main", "story/US-001", "story/US-002", "loose"}
	if err := os.WriteFile(filepath.Join(main, "a.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test", "stash", "push", "--quiet")
	for _, dir := range dirs(true) {
		if swHasBranch(t, dir, "plain") {
			t.Errorf("a deleted branch is still there from %s", dir)
		}
		gfAgree(t, "after branches were deleted and a change stashed", dir, branches)
	}
	if log, _ := gfStashes(main); log == "" {
		t.Error("a stash was pushed and the stash reflog is empty")
	}
	if p, ok := gfOwnPath(linked, "MERGE_HEAD"); !ok || !strings.Contains(filepath.ToSlash(p), ".git/worktrees/") {
		t.Errorf("MERGE_HEAD of a linked worktree is at %q, want it in the worktree's own directory under .git/worktrees", p)
	}
}

func TestTheFileReadingHelpersLeaveWhatTheyCannotReadToGit(t *testing.T) {
	t.Parallel()

	t.Run("a repository with no commit yet", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "unborn")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		swGit(t, dir, "init", "--quiet", "--initial-branch=main")
		if id, ok := gfRev(dir, "HEAD"); ok {
			t.Errorf("HEAD of a repository with no commit reads as %s", id)
		}
		if got := swBranchOn(t, dir); got != "main" {
			t.Errorf("the unborn branch reads as %q, want main", got)
		}
		if got := swRefs(t, dir); got != "" {
			t.Errorf("a repository with no branch lists %q", got)
		}
		if swHasBranch(t, dir, "main") {
			t.Error("the unborn branch is there")
		}
	})

	t.Run("a bare repository", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		src := filepath.Join(root, "src")
		gfInit(t, src)
		bare := filepath.Join(root, "bare.git")
		swGit(t, root, "clone", "--quiet", "--bare", src, bare)
		if _, ok := gfWorktrees(bare); ok {
			t.Error("the working trees of a bare repository were read from files")
		}
		want := swGit(t, bare, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
		if got := swRefs(t, bare); got != want {
			t.Errorf("a bare repository's branches read as %q, git says %q", got, want)
		}
		if got, want := swRev(t, bare, "HEAD"), swRev(t, src, "HEAD"); got != want {
			t.Errorf("a bare repository's HEAD reads as %s, want %s", got, want)
		}
	})

	t.Run("a branch that shares its short name with a tag", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "ambiguous")
		gfInit(t, dir)
		swGit(t, dir, "branch", "dup")
		swGit(t, dir, "tag", "dup")
		swGit(t, dir, "switch", "--quiet", "dup")
		want, _ := swGitTry(t, dir, "symbolic-ref", "--short", "HEAD")
		if got := swBranchOn(t, dir); got != want {
			t.Errorf("a branch named like a tag reads as %q, git says %q", got, want)
		}
		if _, ok := gfBranchOn(dir); ok {
			t.Error("an ambiguous short name was read from files")
		}
	})

	t.Run("a symbolic ref among the branches", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "alias")
		gfInit(t, dir)
		swGit(t, dir, "symbolic-ref", "refs/heads/alias", "refs/heads/main")
		want := swGit(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads")
		if got := swRefs(t, dir); got != want {
			t.Errorf("branches with a symbolic ref read as %q, git says %q", got, want)
		}
		if _, ok := gfRefs(dir); ok {
			t.Error("a symbolic ref among the branches was read from files")
		}
	})

	t.Run("a reftable repository", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "reftable")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if out, ok := swGitTry(t, dir, "init", "--quiet", "--initial-branch=main", "--ref-format=reftable"); !ok {
			t.Skipf("this git cannot make a reftable repository: %s", out)
		}
		swCommitFile(t, dir, "a.txt", "one\n", "one")
		if _, ok := gfOpen(dir); ok {
			t.Error("a reftable repository was read from files")
		}
		if got, want := swRev(t, dir, "HEAD"), swGit(t, dir, "rev-parse", "HEAD"); got != want {
			t.Errorf("HEAD of a reftable repository reads as %s, git says %s", got, want)
		}
		if got := swBranchOn(t, dir); got != "main" {
			t.Errorf("the branch of a reftable repository reads as %q", got)
		}
	})

	t.Run("a ref storage extension", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "extension")
		gfInit(t, dir)
		cfg := filepath.Join(dir, ".git", "config")
		raw, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfg, append(raw, "[extensions]\n\trefStorage = files\n"...), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := gfOpen(dir); ok {
			t.Error("a repository that names its ref storage was read from files")
		}
	})

	t.Run("a directory that is no repository", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if _, ok := gfOpen(dir); ok {
			t.Skipf("the temporary directory is inside a repository")
		}
		if _, ok := gfRev(dir, "HEAD"); ok {
			t.Error("HEAD was read where there is no repository")
		}
	})

	t.Run("a directory that is gone", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "main")
		gfInit(t, dir)
		gone := filepath.Join(dir, ".sdlc", "worktrees", "US-001")
		if _, ok := gfRev(gone, "HEAD"); ok {
			t.Error("HEAD was read from files for a directory that does not exist")
		}
		if _, ok := gfBranchOn(gone); ok {
			t.Error("the branch was read from files for a directory that does not exist")
		}
		if _, ok := gfRefs(gone); ok {
			t.Error("the branches were listed from files for a directory that does not exist")
		}
	})

	t.Run("a file that is not what git writes", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "damaged")
		gfInit(t, dir)
		if err := os.WriteFile(filepath.Join(dir, ".git", "refs", "heads", "broken"), []byte("not an object id\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if id, ok := gfRev(dir, "refs/heads/broken"); ok {
			t.Errorf("a ref file with no object id in it reads as %s", id)
		}
		if _, ok := gfRefs(dir); ok {
			t.Error("a ref file with no object id in it was listed from files")
		}
		if err := os.WriteFile(filepath.Join(dir, ".git", "packed-refs"), []byte("# pack-refs\nnot-hex refs/heads/x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := gfRefs(dir); ok {
			t.Error("a packed-refs line with no object id in it was listed from files")
		}
	})
}
