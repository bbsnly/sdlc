package gitx

// US-029.3, Gate 3 reopened: unit tests for the error paths of FastForward and
// its small reads that the round-3 suite left uncovered, each reachable
// without making git itself fail -- a bad argument, a ref that does not
// resolve, a checkout git lists whose directory is gone, a read-only index
// directory, and the pure parsing helpers (parseRaw, hashPatch) fed input by
// hand. cli/landing.go's reopenFix and shellpolicy/refs.go's firstWord are a
// second test author's; nothing here touches those packages.
//
// Helpers carry an lp prefix, so that they cannot be mistaken for landing_test.go's
// lg helpers or landing_unit_test.go's lu ones.

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// lpSkipIfPermissionsDoNotBind is for a test that denies a write by taking
// away a directory's write permission: not on Windows, which does not carry
// Unix permission bits, and not as root, which ignores them.
func lpSkipIfPermissionsDoNotBind(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("this platform does not restrict a directory owner's own writes by permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
}

// ---------------------------------------------------------------- FastForward: bad arguments and a ref that does not resolve

// FastForward's own first check is the trunk name, before anything it names
// is read: a name git's check-ref-format itself refuses is not "-main" or ""
// (already held), but one that is syntactically a dash-free word and still no
// branch git will make, such as one holding "..".
func TestFastForwardRefusesATrunkNameGitDoesNotTakeForABranch(t *testing.T) {
	t.Parallel()
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	refs := lgRefs(t, root)

	if err := FastForward(t.Context(), root, "bad..branch", from, to); err == nil {
		t.Fatal("FastForward went ahead with a trunk name git does not take for a branch")
	}
	if got := lgRefs(t, root); got != refs {
		t.Errorf("a refused call changed the refs:\nbefore\n%s\nafter\n%s", refs, got)
	}
}

// A commit named by a hash that is well-formed but names nothing here is a
// refusal from the ancestry check itself, not a silent "not an ancestor": git
// says so with more than exit status 1, and FastForward passes that failure
// on rather than reading it as "no".
func TestFastForwardRefusesAToThatIsNotARealCommit(t *testing.T) {
	t.Parallel()
	root, from := lgRepo(t)
	fake := strings.Repeat("f", 40)

	if err := FastForward(t.Context(), root, "main", from, fake); err == nil {
		t.Fatal("FastForward went ahead to a commit that does not exist here")
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
}

// A trunk name that is a perfectly good branch name, and simply is not one
// here, is fs.ErrNotExist, and nothing moves.
func TestFastForwardRefusesATrunkBranchThatDoesNotExist(t *testing.T) {
	t.Parallel()
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)

	err := FastForward(t.Context(), root, "release", from, to)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("FastForward with no such branch = %v, want fs.ErrNotExist", err)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
}

// ---------------------------------------------------------------- holders: a checkout git lists whose directory is gone

// CheckedOut is CODEMAP.md's own promise: "a checkout whose directory is gone
// holds nothing." A worktree git's administrative files still list, once its
// own directory is removed by hand rather than by "git worktree remove", is
// silently left out, and is no reason to refuse.
func TestCheckedOutLeavesOutAWorktreeWhoseDirectoryIsGone(t *testing.T) {
	t.Parallel()
	root, _ := lgRepo(t)
	gone := luForced(t, root, "main")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	paths, err := CheckedOut(t.Context(), root, "main")
	if err != nil {
		t.Fatalf("CheckedOut: %v", err)
	}
	if slices.ContainsFunc(paths, func(p string) bool { return samePath(p, gone) }) {
		t.Errorf("CheckedOut = %v, still names %s, whose directory is gone", paths, gone)
	}
	if !slices.ContainsFunc(paths, func(p string) bool { return samePath(p, root) }) {
		t.Errorf("CheckedOut = %v, want the main checkout still in it", paths)
	}
}

// ---------------------------------------------------------------- partWay: a git directory that cannot be read

// partWay's own read of the checkout's git directory is a plain Lstat of each
// operation's marker file; what it cannot read at all -- not "the file is not
// there", but permission refused -- is the checkout's own failure to report,
// never silence.
func TestPartWayRefusesAGitDirectoryItCannotRead(t *testing.T) {
	t.Parallel()
	lpSkipIfPermissionsDoNotBind(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	if _, err := partWay(dir); err == nil {
		t.Fatal("partWay read a directory with no permission to read it")
	}
}

// ---------------------------------------------------------------- FastForward and putBack: a later checkout's index cannot be written

// Round 3's own put-back test (O2) locks the later checkout with an
// index.lock file, which checkCheckout's own probe catches before either
// checkout is written, so putBack is never reached by it. Here the later
// checkout's directory has no write permission at all: checkCheckout's reads
// are unaffected, but its write cannot create the lock file update-index and
// read-tree both need, so the failure is only found where FastForward's own
// write phase is, after the earlier checkout has already been moved. putBack
// must undo that earlier move, and trunk must be left exactly where it was.
func TestFastForwardPutsBackAnEarlierCheckoutWhenALaterOnesIndexCannotBeWritten(t *testing.T) {
	t.Parallel()
	lpSkipIfPermissionsDoNotBind(t)
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	later := luForced(t, root, "main")
	lockDir := filepath.Dir(luGitPath(t, later, "index.lock"))
	if err := os.Chmod(lockDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockDir, 0o755) })
	billing := lgRead(t, root, "billing.go")

	if err := FastForward(t.Context(), root, "main", from, to); err == nil {
		t.Fatal("FastForward went ahead with a later checkout whose index cannot be written")
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
	if got := lgRead(t, root, "billing.go"); got != billing {
		t.Errorf("the earlier checkout was not put back: billing.go is %q", got)
	}
	if got := lgRead(t, root, "invoice.go"); got != "" {
		t.Errorf("the earlier checkout was not put back: it still holds the story's invoice.go")
	}
	if got := lgStatus(t, root); got != "" {
		t.Errorf("the earlier checkout is not clean once put back:\n%s", got)
	}
}

// ---------------------------------------------------------------- checkCheckout: a path it cannot read to hash

// A path already at the story's own content -- so it is not in the way, and
// is one checkCheckout means to hold as it is -- still has its content
// hashed to compare against what the story adds: staging it changes its
// ctime, which is enough for git's own index cache to distrust the stat it
// holds and ask checkCheckout to hash the file itself, and a file it cannot
// then read is a refusal, not a held path silently dropped.
func TestFastForwardRefusesAPathItCannotReadToHash(t *testing.T) {
	t.Parallel()
	lpSkipIfPermissionsDoNotBind(t)
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	write(t, root, "invoice.go", "package billing\n")
	run(t, root, "add", "--", "invoice.go")
	full := filepath.Join(root, "invoice.go")
	if err := os.Chmod(full, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(full, 0o644) })

	if err := FastForward(t.Context(), root, "main", from, to); err == nil {
		t.Fatal("FastForward went ahead with a path it could not read to hash")
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
}

// ---------------------------------------------------------------- FileAt: a bad argument that is not empty or a dash

// FileAt's path is handed to git a line at a time on cat-file --batch's
// standard input; a line break in the path itself is refused before git ever
// sees it, rather than read as two lines.
func TestFileAtRefusesAPathWithALineBreakInIt(t *testing.T) {
	t.Parallel()
	root, from := lgRepo(t)
	if _, err := FileAt(t.Context(), root, from, "bad\npath"); err == nil {
		t.Fatal("FileAt read a path with a line break in it")
	}
}

// ---------------------------------------------------------------- parseRaw: a malformed record

// parseRaw reads diff-tree's raw, -z output two fields at a time; a record
// with fewer than the five fields a real one always has -- git refusing to
// print one is not plausible, but the parser does not trust that -- stops the
// read rather than reading the next field as a path.
func TestParseRawStopsAtARecordWithTooFewFields(t *testing.T) {
	t.Parallel()
	out := []byte(":100644 100644 " + strings.Repeat("a", 40) + "\x00short\x00")
	if got := parseRaw(out); got != nil {
		t.Errorf("parseRaw(%q) = %v, want nil on a record with too few fields", out, got)
	}
}

// ---------------------------------------------------------------- hashPatch: the pure reader

// hashPatch reads to EOF, keeping whatever is on the reader's last line even
// without a trailing newline: the fingerprint of a patch git printed without
// one at the end of the file is not silently dropped.
func TestHashPatchKeepsTheLastLineWithNoTrailingNewline(t *testing.T) {
	t.Parallel()
	in := bufio.NewReader(strings.NewReader(
		"diff --git a/x b/x\n" +
			"index " + strings.Repeat("a", 40) + ".." + strings.Repeat("b", 40) + " 100644\n" +
			"--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new, no trailing newline"))
	var out bytes.Buffer
	if err := hashPatch(in, &out); err != nil {
		t.Fatalf("hashPatch: %v", err)
	}
	want := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@\n-old\n+new, no trailing newline"
	if got := out.String(); got != want {
		t.Errorf("hashPatch wrote %q, want %q", got, want)
	}
}

// A line longer than the reader's own buffer is carried whole across as many
// reads as it takes, not cut at the buffer's edge.
func TestHashPatchCarriesALineAcrossTheReadersBuffer(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 40)
	in := bufio.NewReaderSize(strings.NewReader("+"+long+"\n"), 16)
	var out bytes.Buffer
	if err := hashPatch(in, &out); err != nil {
		t.Fatalf("hashPatch: %v", err)
	}
	if got, want := out.String(), "+"+long+"\n"; got != want {
		t.Errorf("hashPatch wrote %q, want %q", got, want)
	}
}

// lpFailingReader gives body, then err: a read failure that is neither the
// buffer filling up nor EOF, part-way through a line with nothing else to
// tell it apart from an ordinary one.
type lpFailingReader struct {
	body []byte
	err  error
}

func (r *lpFailingReader) Read(p []byte) (int, error) {
	if len(r.body) == 0 {
		return 0, r.err
	}
	n := copy(p, r.body)
	r.body = r.body[n:]
	return n, nil
}

// A read error that is not EOF stops hashPatch, and is the error a caller
// sees: the change could not be read, not a truncated one read as whole.
func TestHashPatchStopsAtAReadErrorThatIsNotEOF(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	in := bufio.NewReader(&lpFailingReader{body: []byte("no newline yet"), err: boom})
	var out bytes.Buffer
	if err := hashPatch(in, &out); !errors.Is(err, boom) {
		t.Fatalf("hashPatch = %v, want %v", err, boom)
	}
}

// ---------------------------------------------------------------- write: taking a path in cannot write the index

// A checkout that is the only one holding trunk, and holds nothing but a path
// already at the story's own content, still has an index to write before the
// two-tree merge runs; where that first write cannot be made, FastForward
// refuses before read-tree is ever called, and trunk does not move.
func TestFastForwardRefusesWhenTakingAPathInCannotWriteTheIndex(t *testing.T) {
	t.Parallel()
	lpSkipIfPermissionsDoNotBind(t)
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	write(t, root, "invoice.go", "package billing\n")
	lockDir := filepath.Dir(luGitPath(t, root, "index.lock"))
	if err := os.Chmod(lockDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockDir, 0o755) })

	if err := FastForward(t.Context(), root, "main", from, to); err == nil {
		t.Fatal("FastForward went ahead when taking a path in could not write the index")
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
}

// ---------------------------------------------------------------- putBack: held content, on disk and in the index

// Put back does not only move the tree back to from: a path an earlier
// checkout already held at the story's own content -- untracked, or staged
// over a change the story also makes -- is restored exactly as it stood
// before the attempt, on disk and in the index alike, not left at whatever
// the reversed two-tree merge happened to leave there.
func TestFastForwardPutsBackHeldContentOnDiskAndInTheIndex(t *testing.T) {
	t.Parallel()
	lpSkipIfPermissionsDoNotBind(t)
	root, from := lgRepo(t)
	wt, to := lgStoryWork(t, root)
	// An untracked path the story adds, already at its content (O1's "added
	// path"), and a change the story makes, already at its content but staged
	// (O1's "staged"): both held, put back on disk and in the index.
	write(t, root, "invoice.go", lgRead(t, wt, "invoice.go"))
	write(t, root, "billing.go", lgRead(t, wt, "billing.go"))
	run(t, root, "add", "--", "billing.go")
	status := lgStatus(t, root)

	later := luForced(t, root, "main")
	lockDir := filepath.Dir(luGitPath(t, later, "index.lock"))
	if err := os.Chmod(lockDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockDir, 0o755) })

	if err := FastForward(t.Context(), root, "main", from, to); err == nil {
		t.Fatal("FastForward went ahead with a later checkout whose index cannot be written")
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
	if got := lgRead(t, root, "invoice.go"); got != lgRead(t, wt, "invoice.go") {
		t.Errorf("the untracked invoice.go held before the attempt is now %q", got)
	}
	if got := lgRead(t, root, "billing.go"); got != lgRead(t, wt, "billing.go") {
		t.Errorf("the staged billing.go held before the attempt is now %q", got)
	}
	if got := lgStatus(t, root); got != status {
		t.Errorf("put back left the checkout as:\n%s\nwant exactly as it stood before the attempt:\n%s", got, status)
	}
}

// ---------------------------------------------------------------- Fingerprint: a diff driver's own algorithm

// Before git 2.42, a diff driver's own algorithm outranked the one on the
// command line; Fingerprint reads every configured one and overrides it, so
// that a repository's own diff.<driver>.algorithm does not change what a
// fingerprint is. A driver configured at all, matching no path here, still
// takes the read past the empty case every other call in this file leaves it
// at.
func TestFingerprintOverridesADiffDriversOwnConfiguredAlgorithm(t *testing.T) {
	t.Parallel()
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	run(t, root, "config", "diff.custom.algorithm", "patience")

	fp, err := Fingerprint(t.Context(), root, from, to)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if fp == "" {
		t.Fatal("Fingerprint is empty")
	}
}

// ---------------------------------------------------------------- FastForward: a merge commit between from and to

// A merge commit between from and to is refused whether or not it moves
// trunk itself: here it reaches trunk only through the commit FastForward is
// asked to move to, on a branch of its own, and from stays untouched.
func TestFastForwardRefusesAMergeCommitBetweenFromAndTo(t *testing.T) {
	t.Parallel()
	root, from := lgRepo(t)
	run(t, root, "switch", "--quiet", "-c", "topic", "main")
	write(t, root, "topic.txt", "topic work\n")
	lgCommit(t, root, "topic work")
	run(t, root, "switch", "--quiet", "-c", "combined", "main")
	write(t, root, "trunk.txt", "trunk work\n")
	lgCommit(t, root, "trunk work")
	run(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "merge", "--quiet", "--no-ff", "topic")
	to := lgRev(t, root, "HEAD")
	run(t, root, "switch", "--quiet", "main")

	err := FastForward(t.Context(), root, "main", from, to)
	if !errors.Is(err, ErrNotFastForward) {
		t.Fatalf("FastForward over a merge commit = %v, want ErrNotFastForward", err)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
}

// ---------------------------------------------------------------- sealed: no temporary directory can be made

// Every sealed call starts by making itself a private temporary directory;
// where none can be made -- here, TMPDIR itself names one that does not
// exist -- git is never run at all, and the caller sees why.
func TestSealedRefusesWhenNoTemporaryDirectoryCanBeMade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("MkdirTemp on Windows does not read TMPDIR")
	}
	root, _ := lgRepo(t)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "does-not-exist"))

	if _, err := BranchTip(t.Context(), root, "main"); err == nil {
		t.Fatal("BranchTip went ahead with no temporary directory to run git in")
	}
}

// ---------------------------------------------------------------- statusPaths: a rename or a copy

// statusPaths reads git status's -z porcelain output two entries at a time
// for a rename or a copy, so the name it had is skipped rather than read as a
// path of its own.
func TestStatusPathsSkipsTheOldNameOfARenameOrACopy(t *testing.T) {
	t.Parallel()
	out := []byte("R  new.txt\x00old.txt\x00M  other.txt\x00")
	got := statusPaths(out)
	want := []string{"new.txt", "other.txt"}
	if !slices.Equal(got, want) {
		t.Errorf("statusPaths(%q) = %v, want %v", out, got, want)
	}
}

// ---------------------------------------------------------------- checkCheckout: a tracked path replaced by a symbolic link

// A tracked path the fast-forward would write, replaced on disk by an
// uncommitted symbolic link, is in the way: its content is a link's target,
// not the blob content a plain rehash reads, so it is refused rather than
// written through.
func TestFastForwardRefusesATrackedPathReplacedByASymbolicLink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("making a symbolic link needs a privilege Windows does not give by default")
	}
	root, from := lgRepo(t)
	_, to := lgStoryWork(t, root)
	billing := filepath.Join(root, "billing.go")
	if err := os.Remove(billing); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", billing); err != nil {
		t.Fatal(err)
	}
	status := lgStatus(t, root)

	err := FastForward(t.Context(), root, "main", from, to)
	if !errors.Is(err, ErrWouldOverwrite) {
		t.Fatalf("FastForward over a tracked path replaced by a symbolic link = %v, want ErrWouldOverwrite", err)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != from {
		t.Errorf("trunk moved to %s", got)
	}
	if got := lgStatus(t, root); got != status {
		t.Errorf("a refused fast-forward changed the main checkout:\nbefore\n%s\nafter\n%s", status, got)
	}
}
