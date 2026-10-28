package hook

// Gate 3 reopened for US-029.3's rework (B2): route R1's two failure branches
// in reviewsFresh (internal/hook/hook.go :629-656) were untested. After the
// retro, the hook measures the review subject with the root CODEMAP.md
// overridden as HEAD had it; where that override cannot stand in, the commit
// is refused, not let through as if nothing had changed.
//
// Reuses lhFinished and the R1 fixtures from landing_test.go (lh-prefixed) and
// the swCheckouts layout they build on.
//
// Helpers carry an rw prefix, for rework.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rwGitOut is git in dir, with its stdout, for a value a test needs to read
// back rather than merely run.
func rwGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v", strings.Join(args, " "), dir, err)
	}
	return string(out)
}

// rwCommitObject is the path of commit's own loose object file in root's
// repository, wherever root's checkout keeps the shared object store.
func rwCommitObject(t *testing.T, root, commit string) string {
	t.Helper()
	common := strings.TrimSpace(rwGitOut(t, root, "rev-parse", "--git-common-dir"))
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	return filepath.Join(common, "objects", commit[:2], commit[2:])
}

// B2: after the retro, a commit is refused where the root CODEMAP.md has
// become a directory since the reviews were stamped -- TreeHash's
// ErrOverrideNotAFile, read by reviewsFresh's first branch.
func TestACommitIsRefusedWhenTheRootCodemapHoldsFilesBeneathItAfterTheRetro(t *testing.T) {
	t.Parallel()
	l := lhFinished(t)
	if err := os.Remove(filepath.Join(l.wt, "CODEMAP.md")); err != nil {
		t.Fatal(err)
	}
	write(t, l.wt, "CODEMAP.md/evil.go", "package evil\n")
	wtGit(t, l.wt, "add", "-A")

	got := swCall(t, swHeld, swBash("R1 with CODEMAP.md turned into a directory",
		swOpenings(l)[0], "git commit -m 'docs: map A-1'"), nil)
	if !denied(got) {
		t.Fatal("allowed a commit where the root CODEMAP.md holds files beneath it")
	}
	if !strings.Contains(swReason(got), "holds files beneath it") {
		t.Errorf("refused, but not for that: %s", swReason(got))
	}
}

// B2: after the retro, a commit is refused where the override cannot be
// measured for a reason other than a directory in its place -- reviewsFresh's
// second branch, taken only when the plain tree (no override) still measures.
// The reviewed commit's own object is put beyond git's reach here to produce
// that failure without disturbing the working tree the plain measurement
// reads.
func TestACommitIsRefusedWhenCodemapsHistoricalOverrideCannotBeMeasuredButThePlainTreeDoes(t *testing.T) {
	t.Parallel()
	l := lhFinished(t)
	reviewed := strings.TrimSpace(rwGitOut(t, l.wt, "rev-parse", "HEAD"))
	obj := rwCommitObject(t, l.wt, reviewed)
	if _, err := os.Stat(obj); err != nil {
		t.Fatalf("precondition: the reviewed commit's object is not where expected: %v", err)
	}
	if err := os.Remove(obj); err != nil {
		t.Fatal(err)
	}

	write(t, l.wt, "CODEMAP.md", "# CODEMAP\n\ninvoice.go keeps invoices.\n")
	wtGit(t, l.wt, "add", "CODEMAP.md")

	got := swCall(t, swHeld, swBash("R1 with the reviewed commit's object gone",
		swOpenings(l)[0], "git commit -m 'docs: map A-1'"), nil)
	if !denied(got) {
		t.Fatal("allowed a commit whose override could not be measured")
	}
	if !strings.Contains(swReason(got), "could not be measured as HEAD has it") {
		t.Errorf("refused, but not for that: %s", swReason(got))
	}
}
