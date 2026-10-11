package gitx

// US-029.2: `sdlc start` makes each new story a git worktree of its own, on a
// new branch cut from the tip of trunk, and takes it away again when the start
// cannot finish (AC-1, AC-4). These are the binary's first git commands that
// change a repository, so they are held to what THREATS.md asks of them: never
// reuse, reset or remove what they did not make (T6), never hand git a value
// it would read as an option (T8), refuse a name git will not take as a ref
// (T9), and act on the repository they are given whatever GIT_DIR and its
// neighbours in the environment say (T3).
//
// The functions these tests hold gitx to:
//
//	AddWorktree(ctx, root, dir, branch, trunk string) error
//	RemoveWorktree(ctx, root, dir, branch string) error
//
// root is a checkout of the repository, dir the new worktree's absolute path,
// branch the new branch's name (story/<ID>), and trunk the name of the branch
// the story is cut from, as git.trunk_branch gives it. AddWorktree makes dir
// and the directories above it, and creates branch at the commit
// refs/heads/<trunk> is on. RemoveWorktree takes away the worktree at dir,
// whatever it holds, and then deletes branch: it is start's rollback.
//
// Behind is also here: with E0038 lifted (AC-3), the main checkout may be on
// any branch, and trunk being behind its remote has to be counted from trunk.

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

// wtExec is git in dir, with the environment the test is running in.
func wtExec(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	return cmd
}

// wtGitOut is what git prints in dir, trimmed.
func wtGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := wtExec(t, dir, args...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v in %s: %v", args, dir, err)
	}
	return strings.TrimSpace(string(out))
}

// wtGitFails reports whether git fails in dir, for a ref or a path that should
// not be there.
func wtGitFails(t *testing.T, dir string, args ...string) bool {
	t.Helper()
	return wtExec(t, dir, args...).Run() != nil
}

// wtTrunk is a repository whose trunk, main, has two commits and whose HEAD
// is on another branch, feature, one commit past the first: what AC-3 allows
// the main checkout to be on when a story starts. A tag also called main
// names the first commit, so a branch cut from "main" by name, rather than
// from refs/heads/main, starts in the wrong place.
func wtTrunk(t *testing.T) (root, first, tip string) {
	t.Helper()
	root = repo(t)
	run(t, root, "symbolic-ref", "HEAD", "refs/heads/main")
	write(t, root, "invoice.go", "package invoice\n")
	first = commitFile(t, root, "notes.txt", "first")
	tip = commitFile(t, root, "notes.txt", "second")
	run(t, root, "tag", "main", first)
	run(t, root, "checkout", "--quiet", "-b", "feature", first)
	commitFile(t, root, "feature.txt", "feature work")
	return root, first, tip
}

// wtUntouched is what a start must leave as it was in the checkout it ran from
// (AC-2): the branch, the commit, the index and a tracked file.
type wtUntouched struct{ branch, head, index, file string }

func wtSnapshot(t *testing.T, root string) wtUntouched {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "invoice.go"))
	if err != nil {
		t.Fatal(err)
	}
	return wtUntouched{
		branch: wtGitOut(t, root, "symbolic-ref", "--short", "HEAD"),
		head:   wtGitOut(t, root, "rev-parse", "HEAD"),
		index:  wtGitOut(t, root, "ls-files", "--stage"),
		file:   string(raw),
	}
}

// wtList is every worktree `git worktree list` knows, the main checkout
// first.
func wtList(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for line := range strings.SplitSeq(wtGitOut(t, root, "worktree", "list", "--porcelain"), "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			out = append(out, p)
		}
	}
	return out
}

// wtStoryDir is where decision 3 puts a story's worktree.
func wtStoryDir(root, id string) string {
	return filepath.Join(root, ".sdlc", "worktrees", id)
}

// wtWantNothingMade holds a refused AddWorktree to AC-4: no worktree, no branch
// and no directory left behind by it.
func wtWantNothingMade(t *testing.T, root, dir, branch string) {
	t.Helper()
	if got := wtList(t, root); len(got) != 1 {
		t.Errorf("git knows these worktrees after a refused add: %v, want only the main checkout", got)
	}
	if !wtGitFails(t, root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch) {
		t.Errorf("the branch %s was left behind", branch)
	}
	if _, err := os.Lstat(dir); err == nil {
		t.Errorf("%s was left behind", dir)
	}
}

// wtWantRefusal holds an error to the form every git failure here takes, a code a
// person can look up, and to saying why in git's own words.
func wtWantRefusal(t *testing.T, err error, what, why string) {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("%s = %v, want an error with a code a person can look up", what, err)
	}
	if why != "" && !strings.Contains(e.What+" "+e.Why, why) {
		t.Errorf("%s says %q / %q, want git's reason, %q, in it", what, e.What, e.Why, why)
	}
}

// ---------------------------------------------------------------- AC-1

// AC-1 and decision 3: the worktree is made at .sdlc/worktrees/<ID> on a new
// branch story/<ID>, cut from the tip of trunk -- not from wherever HEAD is,
// and not from a tag that happens to share trunk's name -- and the checkout it
// was made from keeps its branch, its commit, its index and its files (AC-2).
func TestAC1AddWorktreeCutsANewBranchFromTheTipOfTrunkWhereverHeadIs(t *testing.T) {
	root, _, tip := wtTrunk(t)
	before := wtSnapshot(t, root)
	dir := wtStoryDir(root, "US-1")

	if err := AddWorktree(t.Context(), root, dir, "story/US-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	if got := wtGitOut(t, dir, "symbolic-ref", "--short", "HEAD"); got != "story/US-1" {
		t.Errorf("the worktree is on %q, want the new branch story/US-1", got)
	}
	if got := wtGitOut(t, dir, "rev-parse", "HEAD"); got != tip {
		t.Errorf("the worktree's HEAD is %s, want the tip of trunk %s", got, tip)
	}
	if got := wtGitOut(t, root, "rev-parse", "refs/heads/story/US-1"); got != tip {
		t.Errorf("story/US-1 is at %s in the repository, want the tip of trunk %s", got, tip)
	}
	if !sameDir(t, wtGitOut(t, dir, "rev-parse", "--path-format=absolute", "--git-common-dir"), filepath.Join(root, ".git")) {
		t.Error("the worktree is not a worktree of the repository it was made from")
	}
	if got := wtList(t, root); len(got) != 2 || !slices.ContainsFunc(got, func(p string) bool { return sameDir(t, p, dir) }) {
		t.Errorf("git knows these worktrees: %v, want the main checkout and %s", got, dir)
	}
	if raw, err := os.ReadFile(filepath.Join(dir, "invoice.go")); err != nil || string(raw) != "package invoice\n" {
		t.Errorf("trunk's files are not checked out in the worktree: %q, %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "feature.txt")); err == nil {
		t.Error("the worktree has the branch HEAD was on, not trunk")
	}
	if after := wtSnapshot(t, root); after != before {
		t.Errorf("the checkout the worktree was made from changed:\nbefore %+v\nafter  %+v", before, after)
	}
}

// ---------------------------------------------------------------- AC-4, T6

// T6: a story branch that is already there -- a person's, or one a crash left
// -- is refused, never reused and never reset to trunk, and nothing is made.
func TestAC4AndT6AddWorktreeRefusesABranchThatIsAlreadyThereAndLeavesItWhereItWas(t *testing.T) {
	root, first, _ := wtTrunk(t)
	run(t, root, "branch", "story/US-1", first)
	dir := wtStoryDir(root, "US-1")

	err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
	wtWantRefusal(t, err, "AddWorktree on a branch that is already there", "already exists")

	if got := wtGitOut(t, root, "rev-parse", "refs/heads/story/US-1"); got != first {
		t.Errorf("the branch that was already there moved to %s, want it left at %s", got, first)
	}
	if got := wtList(t, root); len(got) != 1 {
		t.Errorf("git knows these worktrees after the refusal: %v", got)
	}
	if _, err := os.Lstat(dir); err == nil {
		t.Errorf("%s was made for a branch that was refused", dir)
	}
}

// T6: a directory with something in it at the story's path is refused, what is
// in it stays, and no branch is left behind for it.
func TestAC4AndT6AddWorktreeRefusesADirectoryWithSomethingInItAndLeavesWhatIsThere(t *testing.T) {
	root, _, _ := wtTrunk(t)
	dir := wtStoryDir(root, "US-1")
	write(t, dir, "keep.txt", "a person's work\n")

	err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
	wtWantRefusal(t, err, "AddWorktree into a directory with something in it", "already exists")

	if raw, err := os.ReadFile(filepath.Join(dir, "keep.txt")); err != nil || string(raw) != "a person's work\n" {
		t.Errorf("what was in the directory is %q, %v; want it left as it was", raw, err)
	}
	if !wtGitFails(t, root, "rev-parse", "--verify", "--quiet", "refs/heads/story/US-1") {
		t.Error("the branch was left behind by a refused add")
	}
	if got := wtList(t, root); len(got) != 1 {
		t.Errorf("git knows these worktrees after the refusal: %v", got)
	}
}

// T6: two starts of one story race to make its worktree. The second is
// refused, and in failing it takes nothing away from the first.
func TestAC4AndT6ASecondAddOfTheSameStoryFailsAndLeavesTheFirstStanding(t *testing.T) {
	root, _, tip := wtTrunk(t)
	dir := wtStoryDir(root, "US-1")
	if err := AddWorktree(t.Context(), root, dir, "story/US-1", "main"); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	write(t, dir, "work.go", "package invoice\n")

	err := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
	wtWantRefusal(t, err, "the second AddWorktree of one story", "")

	if raw, err := os.ReadFile(filepath.Join(dir, "work.go")); err != nil || string(raw) != "package invoice\n" {
		t.Errorf("the first worktree's work is %q, %v; want it wtUntouched", raw, err)
	}
	if got := wtGitOut(t, dir, "rev-parse", "HEAD"); got != tip {
		t.Errorf("the first worktree's HEAD is %s, want %s", got, tip)
	}
	if got := wtGitOut(t, root, "rev-parse", "refs/heads/story/US-1"); got != tip {
		t.Errorf("story/US-1 is at %s, want it left at %s", got, tip)
	}
	if got := wtList(t, root); len(got) != 2 {
		t.Errorf("git knows these worktrees: %v, want the main checkout and the first start's", got)
	}
}

// T9: an id the store accepts can still be a name git will not take as a ref.
// The refusal is AC-4's, and nothing is left behind.
func TestAC4AndT9AddWorktreeRefusesABranchNameGitWillNotTakeAndLeavesNothing(t *testing.T) {
	for _, id := range []string{"A..B", "X.lock"} {
		t.Run("T9 "+id, func(t *testing.T) {
			root, _, _ := wtTrunk(t)
			before := wtSnapshot(t, root)
			dir, branch := wtStoryDir(root, id), "story/"+id

			err := AddWorktree(t.Context(), root, dir, branch, "main")
			wtWantRefusal(t, err, "AddWorktree on "+branch, "")
			wtWantNothingMade(t, root, dir, branch)
			if after := wtSnapshot(t, root); after != before {
				t.Errorf("the checkout changed: before %+v, after %+v", before, after)
			}
		})
	}
}

// AC-4: a trunk the repository does not have is a worktree that cannot be
// made, and nothing is left behind.
func TestAC4AddWorktreeFromATrunkTheRepositoryDoesNotHaveLeavesNothing(t *testing.T) {
	root, _, _ := wtTrunk(t)
	dir := wtStoryDir(root, "US-1")
	err := AddWorktree(t.Context(), root, dir, "story/US-1", "trunk-nobody-made")
	wtWantRefusal(t, err, "AddWorktree from a trunk that is not there", "")
	wtWantNothingMade(t, root, dir, "story/US-1")
}

// T8: trunk comes from the configuration and the branch from a story id, and
// neither is handed to git where it could be read as an option: a trunk called
// --no-checkout made a worktree from HEAD with nothing checked out.
func TestT8AddWorktreeNeverHandsGitAValueItWouldReadAsAnOption(t *testing.T) {
	for _, tc := range []struct{ name, branch, trunk string }{
		{"T8 a trunk that is an option", "story/US-1", "--no-checkout"},
		{"T8 a trunk that is a short option", "story/US-1", "-f"},
		{"T8 a branch that is an option", "--detach", "main"},
		{"T8 a branch that is a short option", "-B", "main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, _ := wtTrunk(t)
			dir := wtStoryDir(root, "US-1")
			err := AddWorktree(t.Context(), root, dir, tc.branch, tc.trunk)
			wtWantRefusal(t, err, "AddWorktree("+tc.branch+", "+tc.trunk+")", "")
			if got := wtList(t, root); len(got) != 1 {
				t.Errorf("git knows these worktrees: %v, want only the main checkout", got)
			}
			if _, err := os.Lstat(dir); err == nil {
				t.Errorf("%s was made", dir)
			}
		})
	}
}

// AC-4: start's rollback takes away the worktree it made and the branch it
// made, even with what the smoke check left in it, and nothing else: the
// checkout it was made from and another story's worktree stay as they were.
func TestAC4RemoveWorktreeTakesAwayTheWorktreeAndItsBranchAndNothingElse(t *testing.T) {
	root, _, tip := wtTrunk(t)
	before := wtSnapshot(t, root)
	dir, kept := wtStoryDir(root, "US-1"), wtStoryDir(root, "US-2")
	for id, d := range map[string]string{"US-1": dir, "US-2": kept} {
		if err := AddWorktree(t.Context(), root, d, "story/"+id, "main"); err != nil {
			t.Fatalf("AddWorktree %s: %v", id, err)
		}
	}
	write(t, dir, "build/output.bin", "what smoke made\n")
	write(t, dir, "invoice.go", "package invoice // changed\n")

	if err := RemoveWorktree(t.Context(), root, dir, "story/US-1"); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	if _, err := os.Lstat(dir); err == nil {
		t.Errorf("%s is still there", dir)
	}
	if !wtGitFails(t, root, "rev-parse", "--verify", "--quiet", "refs/heads/story/US-1") {
		t.Error("story/US-1 is still there")
	}
	if got := wtList(t, root); len(got) != 2 || !slices.ContainsFunc(got, func(p string) bool { return sameDir(t, p, kept) }) {
		t.Errorf("git knows these worktrees: %v, want the main checkout and %s", got, kept)
	}
	if got := wtGitOut(t, root, "rev-parse", "refs/heads/story/US-2"); got != tip {
		t.Errorf("the other story's branch is at %s, want %s", got, tip)
	}
	if got := wtGitOut(t, kept, "rev-parse", "HEAD"); got != tip {
		t.Errorf("the other story's worktree is at %s, want %s", got, tip)
	}
	if after := wtSnapshot(t, root); after != before {
		t.Errorf("the main checkout changed: before %+v, after %+v", before, after)
	}
}

// T6: the rollback is handed paths, and a wrong one must not become a
// deletion. Asked to remove the main checkout, it refuses, and trunk -- which
// git would let it delete, with HEAD on another branch -- stays.
func TestAC4AndT6RemoveWorktreeRefusesTheMainCheckoutAndKeepsItsBranch(t *testing.T) {
	root, _, tip := wtTrunk(t)
	before := wtSnapshot(t, root)

	err := RemoveWorktree(t.Context(), root, root, "main")
	wtWantRefusal(t, err, "RemoveWorktree of the main checkout", "")

	if got := wtGitOut(t, root, "rev-parse", "refs/heads/main"); got != tip {
		t.Errorf("trunk is at %s after a refused removal, want %s", got, tip)
	}
	if after := wtSnapshot(t, root); after != before {
		t.Errorf("the main checkout changed: before %+v, after %+v", before, after)
	}
}

// ---------------------------------------------------------------- AC-3

// AC-3: trunk behind its remote still refuses a new story, and with E0038
// lifted the main checkout can be on any branch. Counted from HEAD, a
// checkout on a branch that has origin's commits hid a trunk two behind, and
// one on an old branch refused a trunk that was level.
func TestAC3BehindCountsFromTrunkNotFromHead(t *testing.T) {
	ctx := t.Context()
	origin := repo(t)
	run(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	first := commit(t, origin, "first")
	commit(t, origin, "second")
	tip := commit(t, origin, "third")

	local := t.TempDir()
	clone(t, origin, local)

	// Trunk two behind, and HEAD on a branch that has everything origin has.
	// Git will not move the branch HEAD is on, so HEAD moves first.
	run(t, local, "checkout", "--quiet", "-b", "caught-up", tip)
	run(t, local, "branch", "--force", "main", first)
	if got, err := Behind(ctx, local, origin, "main"); err != nil || got != 2 {
		t.Errorf("Behind with trunk two behind and HEAD level = %d, %v; want 2", got, err)
	}

	// Trunk level, and HEAD on a branch two behind.
	run(t, local, "checkout", "--quiet", "-b", "old", first)
	run(t, local, "branch", "--force", "main", tip)
	if got, err := Behind(ctx, local, origin, "main"); err != nil || got != 0 {
		t.Errorf("Behind with trunk level and HEAD two behind = %d, %v; want 0", got, err)
	}
}

// A trunk this checkout does not have locally has no tip to cut a story from,
// and is a failure rather than "behind by none".
func TestAC3BehindFailsWhenTrunkIsNotABranchHere(t *testing.T) {
	origin := repo(t)
	run(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	commit(t, origin, "first")
	run(t, origin, "branch", "release")

	local := t.TempDir()
	clone(t, origin, local)
	_, err := Behind(t.Context(), local, origin, "release")
	wantUnreadable(t, err, "Behind for a trunk with no local branch")
}

// ---------------------------------------------------------------- T3

// T3: git reads GIT_DIR, GIT_WORK_TREE, GIT_COMMON_DIR, GIT_INDEX_FILE and
// GIT_OBJECT_DIRECTORY before the directory it runs in, and gitx is handed the
// directory it means. Inherited, each sent a measurement -- the commit a
// story's Base is taken from, the tree its reviews are stamped with, whether
// it is committed -- to whatever repository the variable named: the cli's own
// test harness sets two of them for every project.
//
// Each answer is taken with the variables unset, and must be the same with
// each one set to another repository.
func TestT3GitsVariablesDoNotSteerWhichRepositoryGitxReads(t *testing.T) {
	ctx := t.Context()
	root, first, tip := wtTrunk(t)
	// The tag is AC-1's trap and would only make Branch say heads/main here.
	run(t, root, "tag", "--delete", "main")
	run(t, root, "checkout", "--quiet", "main")
	commitFile(t, root, "notes.txt", "US-1: invoices")
	write(t, root, "invoice.go", "package invoice // not committed\n")

	other := repo(t)
	run(t, other, "symbolic-ref", "HEAD", "refs/heads/master")
	commitFile(t, other, "elsewhere.txt", "another repository")
	write(t, other, "stray.txt", "not committed either\n")

	type answers struct {
		head, branch, trunk, gitDir, tree, resolved string
		clean, ancestor                             bool
		changes, files                              []string
		between, naming                             []Commit
	}
	measure := func() (a answers, errs []error) {
		keep := func(err error) {
			if err != nil {
				errs = append(errs, err)
			}
		}
		var err error
		a.head, err = Head(ctx, root)
		keep(err)
		a.branch, err = Branch(ctx, root)
		keep(err)
		a.trunk = Trunk(ctx, root)
		a.gitDir, err = GitDir(ctx, root)
		keep(err)
		a.tree, err = TreeHash(ctx, root)
		keep(err)
		a.resolved, err = Resolve(ctx, root, "HEAD~1")
		keep(err)
		a.clean, err = Clean(ctx, root)
		keep(err)
		a.ancestor, err = IsAncestor(ctx, root, first, tip)
		keep(err)
		a.changes, err = Changes(ctx, root)
		keep(err)
		a.files, err = Files(ctx, root)
		keep(err)
		a.between, err = CommitsBetween(ctx, root, first, tip)
		keep(err)
		a.naming, err = CommitsNaming(ctx, root, "US-1")
		keep(err)
		return a, errs
	}
	want, errs := measure()
	if len(errs) > 0 {
		t.Fatalf("measuring with nothing set: %v", errs)
	}

	for name, value := range map[string]string{
		"GIT_DIR":              filepath.Join(other, ".git"),
		"GIT_WORK_TREE":        other,
		"GIT_COMMON_DIR":       filepath.Join(other, ".git"),
		"GIT_INDEX_FILE":       filepath.Join(other, ".git", "index"),
		"GIT_OBJECT_DIRECTORY": filepath.Join(other, ".git", "objects"),
	} {
		t.Run("T3 "+name, func(t *testing.T) {
			t.Setenv(name, value)
			got, errs := measure()
			if len(errs) > 0 {
				t.Errorf("with %s set, gitx failed on the repository it was given: %v", name, errs)
			}
			for _, c := range []struct {
				what      string
				got, want any
			}{
				{"Head", got.head, want.head},
				{"Branch", got.branch, want.branch},
				{"Trunk", got.trunk, want.trunk},
				{"GitDir", got.gitDir, want.gitDir},
				{"TreeHash", got.tree, want.tree},
				{"Resolve", got.resolved, want.resolved},
				{"Clean", got.clean, want.clean},
				{"IsAncestor", got.ancestor, want.ancestor},
				{"Changes", strings.Join(got.changes, ","), strings.Join(want.changes, ",")},
				{"Files", strings.Join(got.files, ","), strings.Join(want.files, ",")},
				{"CommitsBetween", len(got.between), len(want.between)},
				{"CommitsNaming", len(got.naming), len(want.naming)},
			} {
				if c.got != c.want {
					t.Errorf("with %s set, %s = %v; want %v, the repository it was given", name, c.what, c.got, c.want)
				}
			}
		})
	}
}

// T3: a worktree made, or taken away, with GIT_DIR or GIT_WORK_TREE set is the
// repository's it was asked of, not the variable's.
func TestT3AWorktreeIsMadeAndRemovedInTheRepositoryItWasAskedOf(t *testing.T) {
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR"} {
		t.Run("T3 "+name, func(t *testing.T) {
			root, _, tip := wtTrunk(t)
			other := repo(t)
			run(t, other, "symbolic-ref", "HEAD", "refs/heads/main")
			commitFile(t, other, "elsewhere.txt", "another repository")
			value := filepath.Join(other, ".git")
			if name == "GIT_WORK_TREE" {
				value = other
			}
			dir := wtStoryDir(root, "US-1")

			t.Setenv(name, value)
			made := AddWorktree(t.Context(), root, dir, "story/US-1", "main")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
			if made != nil {
				t.Fatalf("with %s set, AddWorktree: %v", name, made)
			}
			if got := wtGitOut(t, root, "rev-parse", "refs/heads/story/US-1"); got != tip {
				t.Errorf("with %s set, story/US-1 is at %s in the repository asked, want %s", name, got, tip)
			}
			if !wtGitFails(t, other, "rev-parse", "--verify", "--quiet", "refs/heads/story/US-1") {
				t.Errorf("with %s set, the branch was made in the repository the variable named", name)
			}
			if got := wtList(t, other); len(got) != 1 {
				t.Errorf("with %s set, the repository the variable named has worktrees %v", name, got)
			}

			t.Setenv(name, value)
			removed := RemoveWorktree(t.Context(), root, dir, "story/US-1")
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
			if removed != nil {
				t.Fatalf("with %s set, RemoveWorktree: %v", name, removed)
			}
			if !wtGitFails(t, root, "rev-parse", "--verify", "--quiet", "refs/heads/story/US-1") {
				t.Errorf("with %s set, story/US-1 was not removed from the repository asked", name)
			}
			if got := wtList(t, root); len(got) != 1 {
				t.Errorf("with %s set, the repository asked still has worktrees %v", name, got)
			}
		})
	}
}

// T3: Behind fetches and counts in the repository it is asked of. With GIT_DIR
// at another repository, the fetch and the count went there.
func TestT3BehindFetchesAndCountsInTheRepositoryItIsAskedOf(t *testing.T) {
	origin := repo(t)
	run(t, origin, "symbolic-ref", "HEAD", "refs/heads/main")
	first := commit(t, origin, "first")
	commit(t, origin, "second")
	commit(t, origin, "third")

	local := t.TempDir()
	clone(t, origin, local)
	run(t, local, "reset", "--hard", "--quiet", first)
	level := t.TempDir()
	clone(t, origin, level)

	for name, value := range map[string]string{
		"GIT_DIR":       filepath.Join(level, ".git"),
		"GIT_WORK_TREE": level,
	} {
		t.Run("T3 "+name, func(t *testing.T) {
			t.Setenv(name, value)
			if got, err := Behind(t.Context(), local, origin, "main"); err != nil || got != 2 {
				t.Errorf("with %s at a clone that is level, Behind = %d, %v; want 2 for the clone asked", name, got, err)
			}
		})
	}
}
