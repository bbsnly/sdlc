package gitx

// US-088: `sdlc stop` puts a finished story's commits onto a trunk that moved,
// itself. These hold the git half of it: the one sealed rebase the tool runs,
// what it leaves when it stops, how it is abandoned, and the measure that
// says the result is the change the reviewers saw.
//
// The functions these tests hold gitx to, by name and signature:
//
//	Replay(ctx, checkout, onto, upstream string) error
//	AbandonReplay(ctx, checkout string) error
//	Unmerged(ctx, checkout string) ([]string, error)
//	LinesFingerprint(ctx, root, base, tip string, leaveOut ...string) (string, error)
//
// Replay makes the commits of the branch checked out in checkout that follow
// upstream again on top of onto, both full commit hashes, and moves that
// branch to the result. Where a commit does not apply it returns an error and
// leaves the rebase part-way, so that Unmerged can name the paths in
// conflict; AbandonReplay then puts the checkout back as it was. Nothing the
// repository configures -- a merge driver, merge.default, an editor, commit
// signing, a hook, the rebase's own settings -- runs in it or shapes what it
// makes. LinesFingerprint is Fingerprint without context lines: the change's
// own added and removed lines, file by file.
//
// They are also where the facts about git the landing rests on are run rather
// than read: which conflicts git lists, that an abandoned rebase leaves no
// trace, and what a missing committer leaves.
//
// Helpers carry an rp prefix.

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// rpRepo is lgRepo with two more files, one whose name has a space and one
// whose name is not ASCII, and a committer in the repository's configuration:
// a rebase makes commits, and the tests' environment names nobody.
func rpRepo(t *testing.T) (root, fork string) {
	t.Helper()
	root, _ = lgRepo(t)
	write(t, root, "price list.txt", "tea 1\n")
	write(t, root, "prix détaillé.txt", "thé 1\n")
	run(t, root, "config", "user.email", "t@example.com")
	run(t, root, "config", "user.name", "Test")
	return root, lgCommit(t, root, "trunk with two more files")
}

// rpBilling is billing.go with the story's two lines low in it, and trunk's
// header line high in it where trunk is set.
func rpBilling(story, trunk bool) string {
	lines := lgLines()
	if story {
		lines = lgInsert(lines, 26, "// story line A", "// story line B")
	}
	if trunk {
		lines = lgInsert(lines, 2, "// a header trunk added")
	}
	return lgFile(lines)
}

// rpStory is a story branch in its own worktree with two commits: billing.go
// changed and invoice.go added, then a.txt changed.
func rpStory(t *testing.T, root string) (wt, tip string) {
	t.Helper()
	wt = lgStory(t, root)
	write(t, wt, "billing.go", rpBilling(true, false))
	write(t, wt, "invoice.go", "package billing\n")
	lgCommit(t, wt, "US-001: invoices")
	write(t, wt, "a.txt", "a, changed by the story\n")
	return wt, lgCommit(t, wt, "US-001: a")
}

// rpTrunkMoves is trunk moved on by a commit that changes billing.go far from
// the story's lines, so that a replay has to merge the file, and adds a file.
func rpTrunkMoves(t *testing.T, root string) string {
	t.Helper()
	write(t, root, "billing.go", rpBilling(false, true))
	write(t, root, "other.go", "package billing\n")
	return lgCommit(t, root, "another story lands")
}

// rpSnap is everything about a checkout a replay that did not happen must
// leave alone: the branch, the commit, every ref, the index, what git status
// says, the stashes, whether anything is part-way, and each tracked file.
func rpSnap(t *testing.T, wt string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("branch " + wtGitOut(t, wt, "rev-parse", "--abbrev-ref", "HEAD") + "\n")
	b.WriteString("head " + lgRev(t, wt, "HEAD") + "\n")
	b.WriteString("refs\n" + lgRefs(t, wt) + "\n")
	b.WriteString("index\n" + wtGitOut(t, wt, "ls-files", "-s") + "\n")
	b.WriteString("status\n" + lgStatus(t, wt) + "\n")
	b.WriteString("stashes\n" + wtGitOut(t, wt, "stash", "list", "--format=%H") + "\n")
	b.WriteString("part-way " + rpPartWay(t, wt) + "\n")
	for name := range strings.SplitSeq(wtGitOut(t, wt, "-c", "core.quotePath=false", "ls-files"), "\n") {
		b.WriteString("file " + name + "\n" + lgRead(t, wt, name) + "\n")
	}
	return b.String()
}

func rpPartWay(t *testing.T, wt string) string {
	t.Helper()
	what, err := InProgress(t.Context(), wt)
	if err != nil {
		t.Fatalf("InProgress: %v", err)
	}
	return what
}

func rpUnmerged(t *testing.T, wt string) []string {
	t.Helper()
	names, err := Unmerged(t.Context(), wt)
	if err != nil {
		t.Fatalf("Unmerged: %v", err)
	}
	slices.Sort(names)
	return names
}

func rpLines(t *testing.T, root, base, tip string, leaveOut ...string) string {
	t.Helper()
	fp, err := LinesFingerprint(t.Context(), root, base, tip, leaveOut...)
	if err != nil {
		t.Fatalf("LinesFingerprint(%s, %s): %v", base, tip, err)
	}
	if fp == "" {
		t.Fatalf("LinesFingerprint(%s, %s) is empty", base, tip)
	}
	return fp
}

// rpScript is an executable shell script in dir that leaves a mark when it
// runs, does what body says, and is named by a path git's configuration takes.
func rpScript(t *testing.T, dir, marks, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	text := "#!/bin/sh\necho ran > '" + filepath.ToSlash(filepath.Join(marks, name)) + "'\n" + body + "\n"
	if err := os.WriteFile(path, []byte(text), 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(path)
}

// rpNoneRan fails the test for every mark a script left.
func rpNoneRan(t *testing.T, marks string) {
	t.Helper()
	ran, err := os.ReadDir(marks)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ran {
		t.Errorf("the repository's %s ran in the replay", e.Name())
	}
}

// ---------------------------------------------------------------- Replay

// The story's commits are made again on trunk as it is: the same commits in
// the same order by the same author, the branch moved to them and still
// checked out, both sides' lines in the file both changed, nothing part-way
// and nothing uncommitted. The change is the one that was there before.
func TestReplayPutsTheStorysCommitsOntoTheTrunkThatMoved(t *testing.T) {
	t.Parallel()
	root, fork := rpRepo(t)
	wt, before := rpStory(t, root)
	trunk := rpTrunkMoves(t, root)
	reviewed := lgFingerprint(t, root, fork, before)

	if err := Replay(t.Context(), wt, trunk, fork); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	after := lgRev(t, wt, "HEAD")
	if after == before {
		t.Fatal("the story's tip is where it was")
	}
	if got := wtGitOut(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); got != "story/US-001" {
		t.Errorf("the worktree has %q checked out, want story/US-001", got)
	}
	if got := lgRev(t, root, "refs/heads/story/US-001"); got != after {
		t.Errorf("story/US-001 is at %s and the worktree at %s", got, after)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != trunk {
		t.Errorf("trunk moved from %s to %s", trunk, got)
	}
	if got := wtGitOut(t, wt, "log", "--format=%an <%ae> %s", trunk+"..HEAD"); got != "Test <t@example.com> US-001: a\nTest <t@example.com> US-001: invoices" {
		t.Errorf("the commits on trunk are\n%s\nwant the story's two, in their order, by their author", got)
	}
	if got := lgRev(t, wt, "HEAD~2"); got != trunk {
		t.Errorf("under the story's two commits is %s, want trunk at %s", got, trunk)
	}
	if got := lgRead(t, wt, "billing.go"); got != rpBilling(true, true) {
		t.Errorf("billing.go is\n%s\nwant the story's lines and trunk's", got)
	}
	if got := lgRead(t, wt, "other.go"); got == "" {
		t.Error("the file trunk added is not in the worktree")
	}
	if got := lgStatus(t, wt); got != "" {
		t.Errorf("the worktree holds uncommitted changes after the replay:\n%s", got)
	}
	if got := rpPartWay(t, wt); got != "" {
		t.Errorf("%s is part-way after a replay that finished", got)
	}
	if got := lgFingerprint(t, root, trunk, after); got != reviewed {
		t.Error("the change on trunk is not the change the story made")
	}
}

// Both commits are read by the caller, and a value that is not a whole commit
// hash -- a name that could move, a short hash, an option, nothing -- is
// refused before git sees it, with the checkout as it was.
func TestReplayRefusesAnythingButWholeCommitHashes(t *testing.T) {
	t.Parallel()
	root, fork := rpRepo(t)
	wt, _ := rpStory(t, root)
	trunk := rpTrunkMoves(t, root)
	before := rpSnap(t, wt)

	for _, tc := range []struct{ onto, upstream string }{
		{"main", fork},
		{trunk, "main"},
		{trunk[:12], fork},
		{trunk, fork[:12]},
		{"", fork},
		{trunk, ""},
		{"--root", fork},
		{trunk, "--exec=touch " + filepath.Join(lgTempDir(t), "x")},
		{"refs/heads/main", fork},
	} {
		if err := Replay(t.Context(), wt, tc.onto, tc.upstream); err == nil {
			t.Errorf("Replay(%q, %q) went ahead", tc.onto, tc.upstream)
		}
	}
	if got := rpSnap(t, wt); got != before {
		t.Errorf("a refused replay changed the checkout:\nbefore\n%s\nafter\n%s", before, got)
	}
}

// rpConflicts are the ways a story's commits fail to apply, each with what the
// story did, what trunk did since, and the paths git must name.
var rpConflicts = []struct {
	name         string
	story, trunk map[string]string // path to content; "" removes the file
	second       map[string]string // a second story commit, where the case needs one
	want         []string
}{
	{
		name:  "both changed the same lines",
		story: map[string]string{"billing.go": rpBilling(true, false)},
		trunk: map[string]string{"billing.go": lgFile(lgInsert(lgLines(), 26, "// trunk's own line"))},
		want:  []string{"billing.go"},
	},
	{
		name:  "the story changed a file trunk removed",
		story: map[string]string{"notes.txt": "notes, changed by the story\n"},
		trunk: map[string]string{"notes.txt": ""},
		want:  []string{"notes.txt"},
	},
	{
		name:  "the story removed a file trunk changed",
		story: map[string]string{"notes.txt": ""},
		trunk: map[string]string{"notes.txt": "notes, changed on trunk\n"},
		want:  []string{"notes.txt"},
	},
	{
		name:  "both added the same file",
		story: map[string]string{"invoice.go": "package billing\n\n// the story's\n"},
		trunk: map[string]string{"invoice.go": "package billing\n\n// trunk's\n"},
		want:  []string{"invoice.go"},
	},
	{
		name:  "a name with a space and a name that is not ASCII",
		story: map[string]string{"price list.txt": "tea 2\n", "prix détaillé.txt": "thé 2\n"},
		trunk: map[string]string{"price list.txt": "tea 3\n", "prix détaillé.txt": "thé 3\n"},
		want:  []string{"price list.txt", "prix détaillé.txt"},
	},
	{
		name:   "the story's second commit, after its first applied",
		story:  map[string]string{"invoice.go": "package billing\n"},
		second: map[string]string{"a.txt": "a, changed by the story\n"},
		trunk:  map[string]string{"a.txt": "a, changed on trunk\n"},
		want:   []string{"a.txt"},
	},
}

// rpApply writes files into dir, removing those given no content, and commits.
func rpApply(t *testing.T, dir, message string, files map[string]string) string {
	t.Helper()
	for path, body := range files {
		if body == "" {
			if err := os.Remove(filepath.Join(dir, filepath.FromSlash(path))); err != nil {
				t.Fatal(err)
			}
			continue
		}
		write(t, dir, path, body)
	}
	return lgCommit(t, dir, message)
}

// A commit that does not apply stops the replay with an error and the rebase
// part-way; Unmerged names exactly the paths in conflict, as they are spelt,
// whatever kind of conflict it is; and AbandonReplay leaves the branch, the
// commit, the index, the tracked files, an untracked file and a stash exactly
// as they were before the replay began.
func TestAReplayThatConflictsNamesThePathsAndIsAbandonedWithoutATrace(t *testing.T) {
	t.Parallel()
	for _, tc := range rpConflicts {
		t.Run(tc.name, func(t *testing.T) {
			root, fork := rpRepo(t)
			wt := lgStory(t, root)
			// A stash made before the story's work, and an untracked file: what a
			// session may have beside its commits.
			write(t, wt, "a.txt", "set aside\n")
			run(t, wt, "stash", "push", "--quiet", "-m", "set aside")
			rpApply(t, wt, "US-001: first", tc.story)
			if tc.second != nil {
				rpApply(t, wt, "US-001: second", tc.second)
			}
			write(t, wt, "scratch.txt", "not tracked\n")
			trunk := rpApply(t, root, "another story lands", tc.trunk)
			before := rpSnap(t, wt)
			tip := lgRev(t, wt, "HEAD")

			if err := Replay(t.Context(), wt, trunk, fork); err == nil {
				t.Fatal("Replay reported no error for commits that do not apply")
			}
			if got := rpPartWay(t, wt); got == "" {
				t.Error("nothing is part-way after a replay that stopped on a conflict")
			}
			if got := rpUnmerged(t, wt); !slices.Equal(got, tc.want) {
				t.Errorf("Unmerged names %q, want %q", got, tc.want)
			}
			if got := lgRev(t, root, "refs/heads/main"); got != trunk {
				t.Errorf("trunk moved from %s to %s", trunk, got)
			}

			if err := AbandonReplay(t.Context(), wt); err != nil {
				t.Fatalf("AbandonReplay: %v", err)
			}
			if got := rpSnap(t, wt); got != before {
				t.Errorf("the abandoned replay left the checkout changed:\nbefore\n%s\nafter\n%s", before, got)
			}
			if got := lgRev(t, root, "refs/heads/story/US-001"); got != tip {
				t.Errorf("story/US-001 is at %s after the replay was abandoned, want %s", got, tip)
			}
			if got := rpUnmerged(t, wt); len(got) != 0 {
				t.Errorf("Unmerged names %q with nothing part-way", got)
			}
		})
	}
}

// Unmerged with nothing in conflict names nothing, and is no error.
func TestUnmergedNamesNothingWhereNothingIsInConflict(t *testing.T) {
	t.Parallel()
	root, _ := rpRepo(t)
	wt, _ := rpStory(t, root)
	write(t, wt, "a.txt", "changed and not committed\n")
	if got := rpUnmerged(t, wt); len(got) != 0 {
		t.Errorf("Unmerged names %q in a checkout with no conflict", got)
	}
}

// With no committer the replay cannot make a commit. It says so with an
// error, and what it leaves is either nothing or a rebase AbandonReplay
// clears: the checkout ends exactly as it began, and trunk where it was.
func TestAReplayWithNoCommitterFailsAndLeavesTheCheckoutAsItWas(t *testing.T) {
	t.Parallel()
	root, fork := rpRepo(t)
	wt, _ := rpStory(t, root)
	trunk := rpTrunkMoves(t, root)
	run(t, root, "config", "--unset", "user.email")
	run(t, root, "config", "--unset", "user.name")
	// Without this git makes a committer up from the machine's own names where
	// it can, and the test would depend on the machine.
	run(t, root, "config", "user.useConfigOnly", "true")
	before := rpSnap(t, wt)

	if err := Replay(t.Context(), wt, trunk, fork); err == nil {
		t.Fatal("Replay reported no error with no committer to make the commits as")
	}
	if rpPartWay(t, wt) != "" {
		if err := AbandonReplay(t.Context(), wt); err != nil {
			t.Fatalf("AbandonReplay: %v", err)
		}
	}
	if got := rpSnap(t, wt); got != before {
		t.Errorf("the replay that could not commit left the checkout changed:\nbefore\n%s\nafter\n%s", before, got)
	}
	if got := lgRev(t, root, "refs/heads/main"); got != trunk {
		t.Errorf("trunk moved from %s to %s", trunk, got)
	}
}

// A checkout holding an uncommitted change is not replayed over, and nothing
// is set aside for it, whatever the repository's rebase.autoStash says: a
// stash the tool made is a change of the session's that the tool then owns.
func TestReplayNeverStashesAnUncommittedChange(t *testing.T) {
	t.Parallel()
	root, fork := rpRepo(t)
	wt, tip := rpStory(t, root)
	trunk := rpTrunkMoves(t, root)
	run(t, root, "config", "rebase.autoStash", "true")
	write(t, wt, "billing.go", rpBilling(true, false)+"// not committed\n")
	before := rpSnap(t, wt)

	if err := Replay(t.Context(), wt, trunk, fork); err == nil {
		t.Fatal("Replay went ahead over an uncommitted change")
	}
	if rpPartWay(t, wt) != "" {
		if err := AbandonReplay(t.Context(), wt); err != nil {
			t.Fatalf("AbandonReplay: %v", err)
		}
	}
	if got := wtGitOut(t, wt, "stash", "list"); got != "" {
		t.Errorf("the replay left a stash:\n%s", got)
	}
	if got := rpSnap(t, wt); got != before {
		t.Errorf("the refused replay changed the checkout:\nbefore\n%s\nafter\n%s", before, got)
	}
	if got := lgRev(t, wt, "HEAD"); got != tip {
		t.Errorf("the story's tip moved from %s to %s", tip, got)
	}
}

// The replay is git's merge backend whatever the repository asks for: the
// rebase it leaves part-way on a conflict is the one `--merge` leaves, and
// not the patch-applying one, which names no unmerged path for some conflicts
// and takes a fork point from the reflog.
func TestReplayUsesTheMergeBackendWhateverTheRepositoryConfigures(t *testing.T) {
	t.Parallel()
	root, fork := rpRepo(t)
	wt := lgStory(t, root)
	rpApply(t, wt, "US-001", rpConflicts[0].story)
	trunk := rpApply(t, root, "another story lands", rpConflicts[0].trunk)
	run(t, root, "config", "rebase.backend", "apply")

	if err := Replay(t.Context(), wt, trunk, fork); err == nil {
		t.Fatal("Replay reported no error for commits that do not apply")
	}
	gitDir := wtGitOut(t, wt, "rev-parse", "--absolute-git-dir")
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-merge")); err != nil {
		t.Errorf("the replay left no rebase-merge directory, which `--merge` does: %v", err)
	}
	if _, err := os.Stat(filepath.Join(gitDir, "rebase-apply")); err == nil {
		t.Error("the replay left a rebase-apply directory: it ran the patch-applying backend")
	}
	if err := AbandonReplay(t.Context(), wt); err != nil {
		t.Fatalf("AbandonReplay: %v", err)
	}
}

// THREATS and the security review's finding 4: the repository's configuration
// is the session's to write, and nothing in it runs in the replay or decides
// what the replay makes. A merge driver that would put the story's lines
// somewhere else is not run: the file is merged as text, or the replay stops
// and names it. An editor, a signing program and the hooks do not run, and
// the rebase's own settings do not move another branch or squash a commit.
func TestTheRepositorysConfigurationDoesNotRunInOrShapeTheReplay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the programs here are shell scripts marked executable, which Windows does not run as such")
	}
	steered := "printf 'package billing\\n\\n// story line A\\n// story line B\\n' > \"$2\"\nexit 0"
	for _, tc := range []struct {
		name string
		// mayStop is for a path whose attribute names a configured driver: the
		// replay may refuse to merge it, and must then name it or have begun
		// nothing.
		mayStop bool
		set     func(t *testing.T, root, bin, marks string)
	}{
		{name: "merge.default names a driver", set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			run(t, root, "config", "merge.steer.driver", "'"+rpScript(t, bin, marks, "driver", steered)+"' %O %A %B")
			run(t, root, "config", "merge.default", "steer")
		}},
		{name: "an attribute names a configured driver", mayStop: true, set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			run(t, root, "config", "merge.steer.driver", "'"+rpScript(t, bin, marks, "driver", steered)+"' %O %A %B")
			write(t, root, ".git/info/attributes", "billing.go merge=steer\n")
		}},
		{name: "a driver whose name holds an equals sign", mayStop: true, set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			run(t, root, "config", "merge.st=eer.driver", "'"+rpScript(t, bin, marks, "driver", steered)+"' %O %A %B")
			write(t, root, ".git/info/attributes", "billing.go merge=st=eer\n")
		}},
		{name: "an editor", set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			editor := "'" + rpScript(t, bin, marks, "editor", "exit 1") + "'"
			run(t, root, "config", "core.editor", editor)
			run(t, root, "config", "sequence.editor", editor)
			t.Setenv("GIT_EDITOR", editor)
			t.Setenv("GIT_SEQUENCE_EDITOR", editor)
		}},
		{name: "commit.gpgSign and a signing program", set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			run(t, root, "config", "commit.gpgSign", "true")
			run(t, root, "config", "gpg.program", rpScript(t, bin, marks, "gpg", "exit 1"))
		}},
		{name: "the hooks", set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			for _, name := range []string{"pre-rebase", "post-rewrite", "post-checkout", "post-commit", "prepare-commit-msg", "commit-msg", "pre-commit", "reference-transaction"} {
				rpScript(t, bin, marks, name, "exit 0")
			}
			run(t, root, "config", "core.hooksPath", filepath.ToSlash(bin))
		}},
		{name: "the rebase's own settings", set: func(t *testing.T, root, _, _ string) {
			t.Helper()
			for _, kv := range [][2]string{
				{"rebase.updateRefs", "true"}, {"rebase.autoSquash", "true"}, {"rebase.rebaseMerges", "true"},
				{"rebase.forkPoint", "true"}, {"rerere.enabled", "true"},
				{"merge.conflictStyle", "diff3"}, {"merge.renames", "false"},
			} {
				run(t, root, "config", kv[0], kv[1])
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, fork := rpRepo(t)
			wt, tip := rpStory(t, root)
			// A branch at the story's first commit, which rebase.updateRefs
			// would move with it.
			first := lgRev(t, wt, "HEAD~1")
			run(t, root, "branch", "bystander", first)
			trunk := rpTrunkMoves(t, root)
			bin, marks := lgTempDir(t), lgTempDir(t)
			tc.set(t, root, bin, marks)
			before := rpSnap(t, wt)

			err := Replay(t.Context(), wt, trunk, fork)
			rpNoneRan(t, marks)
			if err != nil {
				if !tc.mayStop {
					t.Fatalf("Replay: %v", err)
				}
				rpAbandonIfPartWay(t, wt)
				rpNoneRan(t, marks)
				if got := rpSnap(t, wt); got != before {
					t.Errorf("the replay that stopped left the checkout changed:\nbefore\n%s\nafter\n%s", before, got)
				}
				return
			}
			if got := lgRead(t, wt, "billing.go"); got != rpBilling(true, true) {
				t.Errorf("billing.go is\n%s\nwant the story's lines and trunk's, each where its side put them", got)
			}
			if got := wtGitOut(t, wt, "log", "--format=%s", trunk+"..HEAD"); got != "US-001: a\nUS-001: invoices" {
				t.Errorf("the commits on trunk are\n%s\nwant the story's two", got)
			}
			if got := wtGitOut(t, wt, "log", "--format=%G?", trunk+"..HEAD"); got != "N\nN" {
				t.Errorf("the replayed commits' signatures read %q, want none", got)
			}
			if got := lgRev(t, root, "refs/heads/bystander"); got != first {
				t.Errorf("another branch moved from %s to %s", first, got)
			}
			if got := lgRev(t, wt, "HEAD"); got == tip {
				t.Error("the story's tip is where it was")
			}
		})
	}
}

// ---------------------------------------------------------------- LinesFingerprint

// rpCommitOn is a commit on top of base, made in the main checkout with HEAD
// detached, holding files; main is checked out again afterwards.
func rpCommitOn(t *testing.T, root, base, message string, files map[string]string) string {
	t.Helper()
	run(t, root, "switch", "--quiet", "--detach", base)
	commit := rpApply(t, root, message, files)
	run(t, root, "switch", "--quiet", "main")
	return commit
}

// rpNear is billing.go with the story's two lines low in it and, where trunk
// is set, a line of trunk's two lines above them: inside the three lines of
// context a patch carries, and not beside them, so that git still applies one
// on the other.
func rpNear(story, trunk bool) string {
	lines := lgLines()
	if story {
		lines = lgInsert(lines, 26, "// story line A", "// story line B")
	}
	if trunk {
		lines = lgInsert(lines, 24, "// trunk's line, two above")
	}
	return lgFile(lines)
}

// The change's own lines are the same after a replay onto a trunk that
// changed a line two above them, where the measure with context is not; and
// from the other side, what the replay put under the story is what trunk
// made. These two equalities are how the landing knows its own replay.
func TestLinesFingerprintIsTheSameChangeAcrossAReplayBesideTrunksLines(t *testing.T) {
	t.Parallel()
	root, fork := rpRepo(t)
	story := rpCommitOn(t, root, fork, "the story", map[string]string{"billing.go": rpNear(true, false), "invoice.go": "package billing\n"})
	trunk := rpCommitOn(t, root, fork, "trunk", map[string]string{"billing.go": rpNear(false, true), "other.go": "package billing\n"})
	replayed := rpCommitOn(t, root, trunk, "the story, on trunk", map[string]string{"billing.go": rpNear(true, true), "invoice.go": "package billing\n"})

	if lgFingerprint(t, root, fork, story) == lgFingerprint(t, root, trunk, replayed) {
		t.Fatal("precondition: the measure with context does not see trunk's line here, so this case shows nothing")
	}
	if rpLines(t, root, fork, story) != rpLines(t, root, trunk, replayed) {
		t.Error("the story's own lines read as another change once trunk changed a line two above them")
	}
	if rpLines(t, root, fork, trunk) != rpLines(t, root, story, replayed) {
		t.Error("trunk's own lines read as another change under the story")
	}
	if rpLines(t, root, fork, story) == rpLines(t, root, fork, trunk) {
		t.Error("two different changes have one fingerprint")
	}
}

// Red team V1 and security 3 and 4: a result that holds the same lines as the
// reviewed change, somewhere else, is not the reviewed change. Held from both
// sides -- the story's change on its old base against its change on trunk, and
// trunk's change under the story against trunk's change on top of it -- every
// one of these fails at least one of the two.
func TestLinesFingerprintFromBothSidesSeesAChangeThatIsNotTheOneReplayed(t *testing.T) {
	t.Parallel()
	root, _ := rpRepo(t)
	// A base whose billing.go holds one line twice, far apart.
	twice := lgInsert(lgInsert(lgLines(), 20, "// the same line"), 8, "// the same line")
	fork := rpCommitOn(t, root, lgRev(t, root, "main"), "a base with one line twice", map[string]string{"billing.go": lgFile(twice)})
	without := func(lines []string, nth int) []string {
		out, seen := []string{}, 0
		for _, l := range lines {
			if l == "// the same line" {
				if seen++; seen == nth {
					continue
				}
			}
			out = append(out, l)
		}
		return out
	}
	header := func(lines []string) []string { return lgInsert(lines, 2, "// a header trunk added") }
	added := func(lines []string, at int) []string {
		return lgInsert(lines, at, "// story line A", "// story line B")
	}
	// The story removes the second of the two equal lines and adds two lines
	// low in the file; trunk adds a header.
	low := len(twice) - 4
	story := rpCommitOn(t, root, fork, "the story", map[string]string{"billing.go": lgFile(added(without(twice, 2), low-1))})
	trunk := rpCommitOn(t, root, fork, "trunk", map[string]string{"billing.go": lgFile(header(twice)), "other.go": "package billing\n"})
	honest := lgFile(header(added(without(twice, 2), low-1)))

	both := func(result string) bool {
		return rpLines(t, root, fork, story) == rpLines(t, root, trunk, result) &&
			rpLines(t, root, fork, trunk) == rpLines(t, root, story, result)
	}
	if replayed := rpCommitOn(t, root, trunk, "as replayed", map[string]string{"billing.go": honest}); !both(replayed) {
		t.Fatal("the story's change as git replays it is not held to be the same change")
	}
	for name, files := range map[string]map[string]string{
		"the added lines somewhere else in the file": {"billing.go": lgFile(header(added(without(twice, 2), 12)))},
		"the added lines in another file": {
			"billing.go": lgFile(header(without(twice, 2))),
			"other.go":   "package billing\n// story line A\n// story line B\n",
		},
		"the other of two equal lines removed": {"billing.go": lgFile(header(added(without(twice, 1), low-1)))},
		"one line more":                        {"billing.go": lgFile(header(lgInsert(added(without(twice, 2), low-1), 5, "// one more")))},
		"a line of trunk's gone":               {"billing.go": lgFile(added(without(twice, 2), low-1))},
		"a line removed and not added":         {"billing.go": lgFile(header(without(twice, 2)))},
	} {
		t.Run(name, func(t *testing.T) {
			if result := rpCommitOn(t, root, trunk, name, files); both(result) {
				t.Error("held to be the change that was replayed")
			}
		})
	}
}

// A change that differs in any way has another fingerprint: a line more, a
// space, a tab, a mode, a binary file's bytes.
func TestLinesFingerprintDiffersForAChangeThatDiffersInAnyWay(t *testing.T) {
	t.Parallel()
	c := lgChangesOn(t)
	reviewed := rpLines(t, c.root, c.t0, c.story)
	for name, tip := range map[string]string{
		"one more line": c.extraLine, "a trailing space": c.trailingSpace, "a tab for a space": c.tabbed,
		"a file made executable": c.executable, "a binary change": c.binary,
	} {
		if runtime.GOOS == "windows" && name == "a file made executable" {
			continue // the file system keeps no executable bit for add -A to take
		}
		if rpLines(t, c.root, c.t0, tip) == reviewed {
			t.Errorf("%s has the fingerprint of the reviewed change", name)
		}
	}
	if rpLines(t, c.root, c.t0, c.binary) == rpLines(t, c.root, c.t0, c.otherBinary) {
		t.Error("two different changes to a binary file have one fingerprint")
	}
	if again := rpLines(t, c.root, c.t0, c.story); again != reviewed {
		t.Error("the same change read twice has two fingerprints")
	}
}

// What the repository configures for a diff does not move the measure, and
// in particular not the context it asks for: the measure carries none. It is
// of the commits and not of the working tree, leaves out what it is told to,
// and refuses a revision that is none or an option.
func TestLinesFingerprintIsOfTheCommitsWhateverTheRepositoryConfigures(t *testing.T) {
	c := lgChangesOn(t)
	reviewed, other := rpLines(t, c.root, c.t0, c.story), rpLines(t, c.root, c.t0, c.extraLine)
	withoutInvoice := rpLines(t, c.root, c.t0, c.story, "invoice.go")
	if withoutInvoice == reviewed {
		t.Error("leaving invoice.go out left the fingerprint as it was")
	}
	if rpLines(t, c.root, c.t0, c.story, "nothing/here") != reviewed {
		t.Error("leaving out a path the change does not touch moved the fingerprint")
	}

	for _, kv := range [][2]string{
		{"diff.noprefix", "true"}, {"diff.mnemonicPrefix", "true"}, {"diff.srcPrefix", "left/"},
		{"diff.algorithm", "histogram"}, {"diff.renames", "false"}, {"diff.context", "10"},
		{"diff.interHunkContext", "10"}, {"color.ui", "always"}, {"core.quotePath", "false"},
		{"diff.external", "true"}, {"diff.blank.textconv", "true"}, {"diff.blank.binary", "true"},
	} {
		run(t, c.root, "config", kv[0], kv[1])
	}
	write(t, c.root, ".git/info/attributes", "* diff=blank\n*.go -diff\n")
	t.Setenv("GIT_EXTERNAL_DIFF", "true")
	write(t, c.root, "billing.go", "package billing\n\n// not committed\n")

	if got := rpLines(t, c.root, c.t0, c.story); got != reviewed {
		t.Errorf("the repository's configuration or its working tree moved the fingerprint from %s to %s", reviewed, got)
	}
	if got := rpLines(t, c.root, c.t0, c.extraLine); got != other {
		t.Errorf("the repository's configuration moved another change's fingerprint from %s to %s", other, got)
	}
	for _, tc := range []struct{ base, tip string }{
		{"", c.story}, {c.t0, ""}, {"--all", c.story}, {c.t0, "--output=" + filepath.Join(lgTempDir(t), "x")}, {c.t0, "no-such-revision"},
	} {
		if fp, err := LinesFingerprint(t.Context(), c.root, tc.base, tc.tip); err == nil {
			t.Errorf("LinesFingerprint(%q, %q) gave %q and no error", tc.base, tc.tip, fp)
		}
	}
}

// rpAbandonIfPartWay abandons a replay that stopped part-way, after checking
// it stopped on the one file that should conflict.
func rpAbandonIfPartWay(t *testing.T, wt string) {
	t.Helper()
	if rpPartWay(t, wt) == "" {
		return
	}
	if got := rpUnmerged(t, wt); !slices.Equal(got, []string{"billing.go"}) {
		t.Errorf("the replay stopped and Unmerged names %q, want billing.go", got)
	}
	if err := AbandonReplay(t.Context(), wt); err != nil {
		t.Fatalf("AbandonReplay: %v", err)
	}
}
