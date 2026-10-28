package gitx

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
	t.Parallel()
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

// An override hashes a file as other content, and stands in only for a file the
// tree already has: it must not bring in one git ignores or the loop leaves out.
func TestAnOverrideStandsInOnlyForAFileTheTreeHas(t *testing.T) {
	t.Parallel()
	root := repo(t)
	write(t, root, ".gitignore", "*.log\n")
	write(t, root, "main.go", "package main\n")
	write(t, root, "debug.log", "noise")
	write(t, root, ".sdlc/state/active", "A-1\n")

	plain, err := TreeHash(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	overridden, err := TreeHash(t.Context(), root, Override{Path: "main.go", Content: []byte("package other\n")})
	if err != nil {
		t.Fatal(err)
	}
	if overridden == plain {
		t.Fatal("the override changed nothing")
	}
	write(t, root, "main.go", "package other\n")
	onDisk, err := TreeHash(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	if overridden != onDisk {
		t.Error("an override does not hash the same as that content on disk")
	}

	write(t, root, "main.go", "package main\n")
	for _, path := range []string{"debug.log", ".sdlc/state/active", "absent.go"} {
		got, err := TreeHash(t.Context(), root, Override{Path: path, Content: []byte("x")})
		if err != nil {
			t.Fatal(err)
		}
		if got != plain {
			t.Errorf("an override for %s, which is not in the tree, changed the hash", path)
		}
	}
}

// The tree hash is the review stamp: every review recorded in a project
// carries one, and a release that hashed the same tree to another value would
// make all of them stale at once. Nothing else pins the bytes, only the
// behaviour, so these constants are what TreeHash gave for this tree when they
// were written, and each is also what git mktree makes of the entries the
// fixture should leave in. A change that moves one stales every review already
// recorded and needs a story that says so; a faster TreeHash must arrive at the
// same values.
const (
	pinnedTree          = "50565eb4ed50d7136c2cb308a4a94cce6c928d6b"
	pinnedTreeOverride  = "78989c57b17d95eb6cae34f85b0e2d73e9ef927a"
	pinnedTreeAtHas     = "66b528706d10d791f325c0530ab3e56993b0daea"
	pinnedTreeAtHasNone = "33045d9d25cb95d0b578119968d049eab22b6d10"
)

// pinnedFixture is a working tree with each kind of entry TreeHash decides on:
// tracked, untracked and ignored files, a nested directory, an empty one, and
// the loop's own directory. It returns the root and the commit holding the
// tracked files, whose main.go is not the one on disk.
//
// Every file is LF only and has no executable bit, and the repository's own
// configuration settles the two settings that differ by platform: Git for
// Windows' system configuration turns core.autocrlf on, and git init writes
// core.fileMode false on Windows and true elsewhere. The repository's own
// setting outranks the global and system files, and testenv clears the
// variables that could outrank it, so the tree git stores is the same
// everywhere.
func pinnedFixture(t *testing.T) (root, commit string) {
	t.Helper()
	root = repo(t)
	run(t, root, "config", "core.autocrlf", "false")
	run(t, root, "config", "core.fileMode", "false")
	write(t, root, ".gitignore", "*.log\ndist/\n")
	write(t, root, "README.md", "# fixture\n")
	write(t, root, "main.go", "package main\n")
	write(t, root, "internal/deep/deep.go", "package deep\n")
	run(t, root, "add", ".")
	run(t, root, "-c", "user.email=a@b", "-c", "user.name=t", "commit", "--quiet", "-m", "first")
	commit, err := Head(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}

	write(t, root, "main.go", "package main\n\nfunc main() {}\n")
	write(t, root, "notes.txt", "untracked, not ignored\n")
	write(t, root, "internal/deep/deep_test.go", "package deep\n")
	write(t, root, "debug.log", "ignored\n")
	write(t, root, "dist/binary", "ignored with its directory\n")
	write(t, root, ".sdlc/state/active", "A-1\n")
	write(t, root, ".sdlc/stories/A-1/REVIEW.md", "approve\n")
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, commit
}

func TestTreeHashNeverChangesForTheSameTree(t *testing.T) {
	t.Parallel()
	root, commit := pinnedFixture(t)
	for _, tc := range []struct {
		name      string
		overrides []Override
		want      string
	}{
		{"with no overrides", nil, pinnedTree},
		{"with the content of a file overridden",
			[]Override{{Path: "main.go", Content: []byte("package main\n\nfunc main() { println() }\n")}},
			pinnedTreeOverride},
		// An override for a file the tree does not hold is passed over, so
		// these leave the hash where the content override alone put it.
		{"with overrides beside it for files the tree does not hold",
			[]Override{
				{Path: "main.go", Content: []byte("package main\n\nfunc main() { println() }\n")},
				{Path: "debug.log", Content: []byte("x")},
				{Path: ".sdlc/state/active", Content: []byte("x")},
				{Path: "absent.go", Content: []byte("x")},
			},
			pinnedTreeOverride},
		{"with a file taken from a commit that has it",
			[]Override{{Path: "main.go", At: commit}}, pinnedTreeAtHas},
		{"with a file taken from a commit that does not have it",
			[]Override{{Path: "notes.txt", At: commit}}, pinnedTreeAtHasNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TreeHash(t.Context(), root, tc.overrides...)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				// The listing is what to compare with another platform's when
				// only one of them moves.
				out, _ := exec.CommandContext(t.Context(), "git", "-C", root, "ls-tree", "-r", got).CombinedOutput()
				t.Errorf("TreeHash = %s, want %s for the same tree; it holds:\n%s", got, tc.want, out)
			}
		})
	}
}

func TestOutsideARepositoryTheFailureExplainsItself(t *testing.T) {
	t.Parallel()
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

// TreeHash's first git call is outside every override, so a directory that is
// not a repository at all fails there, the same way every other command here
// does.
func TestTreeHashOutsideARepositoryFails(t *testing.T) {
	t.Parallel()
	_, err := TreeHash(t.Context(), t.TempDir())
	wantUnreadable(t, err, "TreeHash outside a repository")
}

// The temporary index lives wherever TMPDIR points, and a value that names no
// directory is the same failure a full disk would give: there is nowhere to
// put the index that measures the tree.
func TestTreeHashFailsWhenNoTemporaryDirectoryIsReachable(t *testing.T) {
	root := repo(t)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	// Windows reads a different variable for the same purpose, and TMPDIR would
	// be ignored there rather than reproducing the failure.
	if runtime.GOOS == "windows" {
		t.Setenv("TMP", filepath.Join(t.TempDir(), "missing"))
	}
	_, err := TreeHash(t.Context(), root)
	wantUnreadable(t, err, "TreeHash with no temporary directory reachable")
	if !strings.Contains(sdlcerr.Render(err), "temporary directory") {
		t.Errorf("the failure does not say why: %s", sdlcerr.Render(err))
	}
}

// Clean is what the commit gate means by "committed": nothing left over,
// tracked or not.
func TestCleanReportsWhetherAnythingIsUncommitted(t *testing.T) {
	t.Parallel()
	root := repo(t)
	commit(t, root, "first")

	if clean, err := Clean(t.Context(), root); err != nil || !clean {
		t.Errorf("Clean = %v, %v; want true right after a commit", clean, err)
	}

	write(t, root, "a.txt", "changed\n")
	if clean, err := Clean(t.Context(), root); err != nil || clean {
		t.Errorf("Clean = %v, %v; want false with an uncommitted change", clean, err)
	}

	if _, err := Clean(t.Context(), t.TempDir()); err == nil {
		t.Error("Clean reported on a directory that is not a repository")
	}
}

// Head is HEAD in full, NoCommits before a repository has one, and a failure
// outside a repository -- the three states the freeze and the commit gate
// tell apart.
func TestHeadIsTheCommitHeadIsOnOrNoCommitsYet(t *testing.T) {
	t.Parallel()
	root := repo(t)
	if got, err := Head(t.Context(), root); err != nil || got != NoCommits {
		t.Errorf("Head before any commit = %q, %v; want %s", got, err, NoCommits)
	}

	want := commit(t, root, "first")
	if got, err := Head(t.Context(), root); err != nil || got != want {
		t.Errorf("Head = %q, %v; want %s", got, err, want)
	}

	if _, err := Head(t.Context(), t.TempDir()); err == nil {
		t.Error("Head reported on a directory that is not a repository")
	}
}

// A repository is on the branch its own detached HEAD is not, and Branch says
// so with no error: a detached checkout is not a failure to read the
// repository.
func TestBranchIsEmptyWithADetachedHead(t *testing.T) {
	t.Parallel()
	root := repo(t)
	head := commit(t, root, "first")
	run(t, root, "checkout", "--quiet", head)

	got, err := Branch(t.Context(), root)
	if err != nil {
		t.Fatalf("Branch on a detached HEAD failed: %v", err)
	}
	if got != "" {
		t.Errorf("Branch on a detached HEAD = %q, want none", got)
	}
}

// init writes the trunk's name before anyone has said it, so it takes the name
// from the repository: a project on master got "main", and its first `sdlc
// start` was refused for not being on trunk.
func TestTrunkIsWhatTheRepositoryCallsIt(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	unborn := t.TempDir()
	run(t, unborn, "init", "--quiet")
	run(t, unborn, "symbolic-ref", "HEAD", "refs/heads/trunk")
	if got := Trunk(ctx, unborn); got != "trunk" {
		t.Errorf("a repository with no commits yet, on trunk: Trunk = %q", got)
	}

	root := repo(t)
	run(t, root, "symbolic-ref", "HEAD", "refs/heads/master")
	write(t, root, "a.txt", "a\n")
	run(t, root, "add", "a.txt")
	run(t, root, "-c", "user.name=sdlc", "-c", "user.email=sdlc@example.com", "-c", "commit.gpgsign=false",
		"commit", "--quiet", "-m", "a")
	run(t, root, "checkout", "--quiet", "-b", "feature")
	if got := Trunk(ctx, root); got != "master" {
		t.Errorf("on a feature branch of a repository with master: Trunk = %q", got)
	}

	run(t, root, "update-ref", "refs/remotes/origin/develop", "HEAD")
	run(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")
	if got := Trunk(ctx, root); got != "develop" {
		t.Errorf("with origin's HEAD on develop: Trunk = %q", got)
	}

	if got := Trunk(ctx, t.TempDir()); got != "" {
		t.Errorf("outside a repository: Trunk = %q, want nothing", got)
	}
}
