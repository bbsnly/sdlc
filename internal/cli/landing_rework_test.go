package cli

// US-029.3, Gate 7's rework: B1, and the reopen texts every refusal a
// landing can hand a person points into. codemapCheck (landing.go :679)
// takes the root CODEMAP.md only as a plain file by exactly that name, so a
// directory, a link, a submodule entry or a name differing only in case
// there is refused with SDLC-E0055 on every path a landing can be on: the
// unmoved one (nothing has moved trunk since the branch was cut), the
// rebased one (trunk moved and the story rebased onto it) and the
// already-landed one (trunk already holds the story's tip). Every refusal
// that hands a story to a person names the answer-first route -- "sdlc
// approve <ID>" or its --reject, before the gate command that reopens the
// gate -- and .sdlc/worktrees/<ID>, where a person runs both.
//
// Everything here drives a real repository and shares landing_test.go's and
// landing_states_test.go's ld and lx helpers. Helpers of this file's own
// carry an lr prefix.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// lrOddKind is one way to put something other than a plain file at the root
// CODEMAP.md: a function that stages it as the last commit on wt, already
// finished and bookkept, and the path the refusal is expected to name.
type lrOddKind struct {
	name  string
	stage func(t *testing.T, wt string) string
	named string
}

var lrOddKinds = []lrOddKind{
	{"a directory", lrDirectory, "CODEMAP.md/evil.go"},
	{"a link", lrSymlink, "CODEMAP.md"},
	{"a submodule entry", lrSubmodule, "CODEMAP.md"},
	{"a name differing only in case", lrCaseVariant, "codemap.md"},
}

// lrDirectory replaces wt's root CODEMAP.md, a plain file, with a directory
// holding one file, and commits it.
func lrDirectory(t *testing.T, wt string) string {
	t.Helper()
	if err := os.Remove(filepath.Join(wt, "CODEMAP.md")); err != nil {
		t.Fatal(err)
	}
	return swCommitFile(t, wt, "CODEMAP.md/evil.go", "a directory where the bookkeeper's file was\n",
		"a directory at the root CODEMAP.md")
}

// lrSymlink replaces it with a symlink to README.md, skipping where the host
// cannot make one or git here will not track it as a link (core.symlinks
// off): the case under test is CODEMAP.md staged as a link, mode 120000.
func lrSymlink(t *testing.T, wt string) string {
	t.Helper()
	path := filepath.Join(wt, "CODEMAP.md")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("README.md", path); err != nil {
		t.Skipf("could not symlink CODEMAP.md: %v", err)
	}
	swGit(t, wt, "add", "--", "CODEMAP.md")
	if mode := swGit(t, wt, "ls-files", "-s", "--", "CODEMAP.md"); !strings.HasPrefix(mode, "120000") {
		t.Skipf("git did not stage CODEMAP.md as a link here: %s", mode)
	}
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m",
		"a link at the root CODEMAP.md")
	return swRev(t, wt, "HEAD")
}

// lrSubmodule replaces it with a gitlink, the tree entry a submodule leaves
// behind, without checking one out: only the mode at the path is under test,
// so any commit-shaped hash stands in for the submodule's own.
func lrSubmodule(t *testing.T, wt string) string {
	t.Helper()
	if err := os.Remove(filepath.Join(wt, "CODEMAP.md")); err != nil {
		t.Fatal(err)
	}
	sha := swRev(t, wt, "HEAD")
	swGit(t, wt, "update-index", "--add", "--cacheinfo", "160000,"+sha+",CODEMAP.md")
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m",
		"a submodule entry at the root CODEMAP.md")
	return swRev(t, wt, "HEAD")
}

// lrCaseVariant renames it to codemap.md, a name differing only in case: the
// variant a case-insensitive disk can hold alongside CODEMAP.md, which is
// the one a hand run of this story's fix produced (US-029.3's rework notes).
func lrCaseVariant(t *testing.T, wt string) string {
	t.Helper()
	swGit(t, wt, "mv", "CODEMAP.md", "codemap.md")
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m",
		"codemap.md, differing only in case")
	return swRev(t, wt, "HEAD")
}

// lrExecutableCodemap replaces the root CODEMAP.md with one just like it but
// executable (mode 100755), skipping where the host or git here does not
// carry the bit back (Windows).
func lrExecutableCodemap(t *testing.T, wt string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not give git an executable bit to track")
	}
	path := filepath.Join(wt, "CODEMAP.md")
	writeFile(t, wt, "CODEMAP.md", "# CODEMAP\n\nan executable bookkeeper's file.\n")
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	swGit(t, wt, "add", "--", "CODEMAP.md")
	if mode := swGit(t, wt, "ls-files", "-s", "--", "CODEMAP.md"); !strings.HasPrefix(mode, "100755") {
		t.Skipf("git did not stage CODEMAP.md as executable here: %s", mode)
	}
	swGit(t, wt, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m",
		"an executable CODEMAP.md")
	return swRev(t, wt, "HEAD")
}

// ---------------------------------------------------------------- B1

// B1, the unmoved path: nothing has moved trunk since the branch was cut.
// An odd root CODEMAP.md is refused with SDLC-E0055 before any command
// runs, the refusal names it, and trunk stays exactly where it was.
func TestB1AnOddRootCodemapIsRefusedOnTheUnmovedPath(t *testing.T) {
	t.Parallel()
	for _, kind := range lrOddKinds {
		t.Run(kind.name, func(t *testing.T) {
			main, wt := ldWalked(t, ldWalk{finished: true})
			ldBookkeeping(t, wt)
			kind.stage(t, wt)
			before := ldTrunk(t, main)

			out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
			if !wtNames(out, kind.named) {
				t.Errorf("the refusal does not name %s:\n%s", kind.named, out)
			}
			if strings.Contains(out, "running commands.") {
				t.Errorf("a command ran before codemapCheck refused the odd root CODEMAP.md:\n%s", out)
			}
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s on an odd root CODEMAP.md", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// B1, the rebased path: trunk moved since the branch was cut and the story
// rebased onto it, carrying the odd root CODEMAP.md with it. It is still
// refused before any command runs, and trunk stays at the tip the rebase
// itself put it on.
func TestB1AnOddRootCodemapIsRefusedOnTheRebasedPath(t *testing.T) {
	t.Parallel()
	for _, kind := range lrOddKinds {
		t.Run(kind.name, func(t *testing.T) {
			main, wt := ldWalked(t, ldWalk{finished: true})
			ldBookkeeping(t, wt)
			kind.stage(t, wt)
			ldMoveTrunk(t, main)
			ldRebase(t, wt)
			before := ldTrunk(t, main)

			out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
			if !wtNames(out, kind.named) {
				t.Errorf("the refusal does not name %s:\n%s", kind.named, out)
			}
			if strings.Contains(out, "running commands.") {
				t.Errorf("a command ran before codemapCheck refused the odd root CODEMAP.md:\n%s", out)
			}
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s on an odd root CODEMAP.md", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// B1, the already-landed path: trunk already holds the story's tip, odd
// root CODEMAP.md included. It is still refused, and nothing is recorded
// as landed.
func TestB1AnOddRootCodemapIsRefusedOnTheAlreadyLandedPath(t *testing.T) {
	t.Parallel()
	for _, kind := range lrOddKinds {
		t.Run(kind.name, func(t *testing.T) {
			main, wt := ldWalked(t, ldWalk{finished: true, spike: true})
			ldBookkeeping(t, wt)
			tip := kind.stage(t, wt)
			swGit(t, main, "update-ref", "refs/heads/main", tip)
			before := ldTrunk(t, main)

			out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
			if !wtNames(out, kind.named) {
				t.Errorf("the refusal does not name %s:\n%s", kind.named, out)
			}
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s on an odd root CODEMAP.md already on trunk", got)
			}
			lxNotDone(t, main)
		})
	}
}

// B1's control: a plain file at exactly CODEMAP.md still lands, whether it
// is an ordinary file or an executable one.
func TestB1APlainOrExecutableRootCodemapStillLands(t *testing.T) {
	t.Parallel()
	t.Run("plain", func(t *testing.T) {
		main, wt := ldWalked(t, ldWalk{finished: true})
		tip := ldBookkeeping(t, wt)
		ldLanded(t, main, wt, tip)
	})
	t.Run("executable", func(t *testing.T) {
		main, wt := ldWalked(t, ldWalk{finished: true})
		tip := lrExecutableCodemap(t, wt)
		ldLanded(t, main, wt, tip)
	})
}

// ---------------------------------------------------------------- the reopen texts

// lrAnswerFirst holds text to naming the answer-first route every reopen
// fix gives a person: approve, or approve --reject, in the story's
// worktree, before the gate command that actually reopens the gate.
func lrAnswerFirst(t *testing.T, text, id, gate string) {
	t.Helper()
	suSays(t, text, `"sdlc approve `+id+`"`, `"sdlc approve `+id+` --reject ..."`, worktreeIn(id),
		`"sdlc gate `+gate+` fail --story `+id+` --note ..."`, "/sdlc:next")
}

// SDLC-E0036: a finished story with a question open waits for a person, and
// the fix names where they answer it and the worktree it is answered in.
func TestE0036sFixNamesTheAnswerFirstRouteAndTheWorktree(t *testing.T) {
	t.Parallel()
	_, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	wtMust(t, wt, ldSession, "", "escalate", "landing", "--message", "land it now?")

	out := swRefused(t, "SDLC-E0036", wt, "", "stop")
	_, fix := ldRefusal(t, out, "SDLC-E0036")
	suSays(t, fix, `"sdlc approve US-001"`, `"sdlc approve US-001 --reject ..."`, worktreeIn("US-001"),
		`"sdlc stop" there lands it`)
}

// SDLC-E0029, the person's case: a change changed after the commit gate,
// and trunk rewritten so it no longer holds where the reviewed change
// began, cannot be told from what the branch holds now -- reopenFix's
// answer-first route, not the "review it again" one requireFreshReviews
// gives a change trunk still holds the base of.
func TestE0029sReopenFixNamesTheAnswerFirstRouteAndTheWorktree(t *testing.T) {
	t.Parallel()
	main, wt, cut := ldWalkedFrom(t, ldWalk{finished: true, trunkMoved: true, spike: true})
	ldBookkeeping(t, wt)
	lxChangeAfterTheReviews(t, wt)
	back := swRev(t, main, cut+"~1")
	rewritten := swGit(t, main, "-c", "user.email=t@example.com", "-c", "user.name=Test",
		"commit-tree", cut+"^{tree}", "-p", back, "-m", "trunk rewritten")
	swGit(t, main, "update-ref", "refs/heads/main", rewritten, cut)
	ldRebase(t, wt)
	before := ldTrunk(t, main)

	out := swRefused(t, ldStale, wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldStale)
	lrAnswerFirst(t, fix, "US-001", "code_review")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s though the story's reviewed base is gone from it", got)
	}
	ldStillHeld(t, main, wt)
}

// SDLC-E0055, the story's branch gone: the fix a person runs to make the
// branch again names the answer-first route and the worktree just as the
// review-stale and freeze fixes do (landing.go :460, reopenRoute with
// GateImplementation).
func TestE0055sMissingBranchFixNamesTheAnswerFirstRouteAndTheWorktree(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{finished: true})
	ldBookkeeping(t, wt)
	swGit(t, wt, "switch", "--quiet", "--detach")
	swGit(t, wt, "branch", "--quiet", "-D", "story/US-001")
	before := ldTrunk(t, main)

	out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldCheckFailed)
	suSays(t, fix, "git branch story/US-001", "git switch story/US-001")
	lrAnswerFirst(t, fix, "US-001", "implementation")
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s though the story's branch was gone", got)
	}
}

// SDLC-E0025 and SDLC-E0043 at the landing: a frozen test the story's branch
// changed, or a test file it added, after the freeze -- trunk's own are
// trunk's, US-088 -- offer the reopen besides restoring or dropping the test:
// unfreezeRoute's answer-first route and worktree, then "sdlc unfreeze
// --reason ..." once the iteration it reopens is running.
func TestE0025AndE0043sFreezeFixesNameTheAnswerFirstRouteAndTheWorktree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, code, rel string
	}{
		{"E0025 a frozen test the story changed", ldFrozenMoved, "internal/tax_test.go"},
		{"E0043 a test file the story added", ldUnfrozenTest, "internal/refund_test.go"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			main, wt := ldWalked(t, ldWalk{finished: true, taxTest: true})
			ldBookkeeping(t, wt)
			swCommitFile(t, wt, tc.rel, "package internal\n\n// changed by the story\n", "the story's own change to a test")
			ldReviewAgain(t, wt)
			before := ldTrunk(t, main)

			out := swRefused(t, tc.code, wt, ldSession, "stop")
			_, fix := ldRefusal(t, out, tc.code)
			lrAnswerFirst(t, fix, "US-001", "code_review")
			suSays(t, fix, `"sdlc unfreeze --reason ..."`)
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s past a broken freeze", got)
			}
			ldStillHeld(t, main, wt)
		})
	}
}

// SDLC-E0037: a story in a tier that pauses, whose change is no longer the
// one a person approved, is not approved for the work as it now is. The fix
// hands it to a person again and names the worktree the approval is read in,
// though it offers no --reject: the work is not reopened, only approved or
// left waiting. A change that is still the one approved keeps its approval
// across the landing (US-088 AC-8).
func TestE0037sFixNamesApproveInTheWorktree(t *testing.T) {
	t.Parallel()
	main, wt := ldWalked(t, ldWalk{tier: "high"})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, "")
	ldBookkeeping(t, wt)
	lxChangeAfterTheReviews(t, wt)
	ldReviewAgain(t, wt)
	before := ldTrunk(t, main)

	out := swRefused(t, ldNotApproved, wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	suSays(t, fix, `"sdlc escalate pre_commit_approval --message ..."`, `"sdlc approve US-001"`, worktreeIn("US-001"))
	if got := ldTrunk(t, main); got != before {
		t.Errorf("trunk moved to %s though the changed work was not approved", got)
	}
	ldStillHeld(t, main, wt)
}

// ---------------------------------------------------------------- the 64 KB window

// lrPadFile is a file of n bytes with no digit in any of them, so that
// reading it after a coverage figure cannot itself look like a number.
func lrPadFile(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pad.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", n)), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// numberLimit keeps only the last 64 KB of what commands.coverage prints on
// stdout to find its last number in: a figure followed by less of no-digit
// text than that is still read, and the story lands; by at least that much,
// it is not, so the check fails closed and the story does not.
func TestLandingReadsACoverageFigureWithinTheLast64KBOfStdout(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		padAfter int
		lands    bool
	}{
		{"padded under the window", 4 << 10, true},
		{"padded to just under the window", 64<<10 - 1024, true},
		{"padded past the window", 64<<10 + 1024, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pad := lrPadFile(t, tc.padAfter)
			command := "printf '95'; cat " + suSh(pad)
			main := lxRepository(t, map[string]string{"coverage": command}, nil)
			wt := ldFinished(t, main)
			ldBookkeeping(t, wt)
			// needsCommands only runs the configured commands where the tree is
			// rebased or changed since the commit gate; a rebase onto a moved
			// trunk is what makes commands.coverage run at all here.
			ldMoveTrunk(t, main)
			rebased := ldRebase(t, wt)
			if tc.lands {
				ldLanded(t, main, wt, rebased)
				return
			}
			before := ldTrunk(t, main)
			out := swRefused(t, ldCheckFailed, wt, ldSession, "stop")
			said, _ := ldRefusal(t, out, ldCheckFailed)
			suSays(t, said, "commands.coverage", "printed no number")
			if got := ldTrunk(t, main); got != before {
				t.Errorf("trunk moved to %s though commands.coverage's figure fell outside the window", got)
			}
		})
	}
}
