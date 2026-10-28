package gitx

// US-089: after a story has landed, `sdlc stop` puts the story's change to the
// root CODEMAP.md on trunk in a commit of its own. This is the git half of
// that: the one call in the package that makes a commit which is not a replay.
//
// The function these tests hold gitx to:
//
//	CommitFile(ctx, root, parent, path string, content []byte, message string) (commit string, err error)
//
// root is any checkout of the repository. CommitFile makes a commit whose one
// parent is the commit parent, a full hash the caller read, and whose tree is
// parent's with the file at path, slash-separated and repository-relative,
// holding content as a plain file. It returns the new commit's full hash. It
// moves no ref and reads and writes no index and no working tree: the caller
// moves trunk with FastForward, which is still the only function that does.
//
// What reaches trunk through it is text a model wrote, with no reviewer, so
// the call is held to the result and not to the request (THREATS T1): the
// commit differs from its parent at that one path and nowhere else, as a
// plain file, or there is no commit. It is sealed like every other call the
// landing makes (THREATS T7): no hook runs, nothing is signed, no filter sees
// the content, and with no committer it fails rather than make one up.
//
// Helpers carry a cf prefix, for commit file.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const cfMap = "CODEMAP.md"

const cfBody = "# CODEMAP\n\n## billing.go\n\nKeeps the accounts.\n"

var cfFullHash = regexp.MustCompile(`^[0-9a-f]{40}$`)

// cfRepo is rpRepo, which has a committer, with a CODEMAP.md on trunk.
func cfRepo(t *testing.T) (root, tip string) {
	t.Helper()
	root, _ = rpRepo(t)
	write(t, root, cfMap, cfBody)
	return root, lgCommit(t, root, "trunk with a map")
}

// cfShow is the file at rel as commit holds it, byte for byte.
func cfShow(t *testing.T, root, commit, rel string) string {
	t.Helper()
	out, err := wtExec(t, root, "cat-file", "blob", commit+":"+rel).Output()
	if err != nil {
		t.Fatalf("%s has no %s: %v", commit, rel, err)
	}
	return string(out)
}

// cfChanged is every path at which commit differs from its first parent, with
// its mode there, as git's objects have them.
func cfChanged(t *testing.T, root, commit string) string {
	t.Helper()
	return wtGitOut(t, root, "diff-tree", "-r", "--no-renames", "--no-commit-id", "--format=", "--raw", "--abbrev=40", commit+"^", commit)
}

// cfMade holds a commit CommitFile returned to the criteria: one parent, the
// one given (AC-3); one path changed, the one given, as a plain file (AC-4);
// and the content as given.
func cfMade(t *testing.T, root, parent, commit, content string) {
	t.Helper()
	const rel = cfMap
	if !cfFullHash.MatchString(commit) {
		t.Fatalf("CommitFile returned %q, want a commit's full hash", commit)
	}
	if got := wtGitOut(t, root, "rev-list", "--parents", "-n", "1", commit); got != commit+" "+parent {
		t.Errorf("the commit and its parents are %q, want the one parent %s", got, parent)
	}
	changed := cfChanged(t, root, commit)
	lines := strings.Split(changed, "\n")
	if len(lines) != 1 || !strings.HasSuffix(lines[0], "\t"+rel) {
		t.Errorf("the commit changes\n%s\nwant %s and no other path", changed, rel)
	}
	if fields := strings.Fields(lines[0]); len(fields) < 2 || fields[1] != "100644" {
		t.Errorf("the commit has %s as %q, want a plain file, 100644", rel, changed)
	}
	if got := cfShow(t, root, commit, rel); got != content {
		t.Errorf("the commit holds %s as\n%q\nwant\n%q", rel, got, content)
	}
}

// cfState is everything CommitFile has to leave alone: every ref, what HEAD
// is, what git status says, and the index.
func cfState(t *testing.T, root string) string {
	t.Helper()
	return strings.Join([]string{
		lgRefs(t, root),
		wtGitOut(t, root, "rev-parse", "HEAD"),
		lgStatus(t, root),
		wtGitOut(t, root, "ls-files", "--stage"),
	}, "\n--\n")
}

// cfRefused holds a call to having made nothing a caller could move trunk to.
func cfRefused(t *testing.T, commit string, err error, root, before string) {
	t.Helper()
	if err == nil {
		t.Fatalf("CommitFile went ahead and returned %q", commit)
	}
	if commit != "" {
		t.Errorf("CommitFile was refused (%v) and still returned the commit %q", err, commit)
	}
	if got := cfState(t, root); got != before {
		t.Errorf("the refused call changed the repository:\nbefore\n%s\nafter\n%s", before, got)
	}
}

// ---------------------------------------------------------------- the commit

// AC-3 and AC-4: the commit has the one parent it was given and differs from
// it at CODEMAP.md alone, as a plain file holding exactly the content.
func TestAC3AC4CommitFileMakesACommitOnItsParentThatChangesTheOnePath(t *testing.T) {
	t.Parallel()
	root, tip := cfRepo(t)
	content := cfBody + "\n## refund.go\n\nKeeps refunds.\n"

	commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(content), "docs: map US-001")
	if err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	cfMade(t, root, tip, commit, content)
	if got := wtGitOut(t, root, "log", "-1", "--format=%s", commit); got != "docs: map US-001" {
		t.Errorf("the commit's subject is %q, want the message given", got)
	}
	if got := wtGitOut(t, root, "log", "-1", "--format=%an <%ae> / %cn <%ce>", commit); got != "Test <t@example.com> / Test <t@example.com>" {
		t.Errorf("the commit is by %q, want the repository's own committer as author and committer", got)
	}
	for _, f := range []string{"billing.go", "a.txt", "notes.txt", "price list.txt", "prix détaillé.txt"} {
		if got, want := cfShow(t, root, commit, f), cfShow(t, root, tip, f); got != want {
			t.Errorf("the commit holds %s as %q, and its parent as %q", f, got, want)
		}
	}
}

// The fourth answer: where trunk has no CODEMAP.md the commit adds it, and
// that is still one path.
func TestAC4CommitFileAddsTheFileWhereItsParentHasNone(t *testing.T) {
	t.Parallel()
	root, tip := rpRepo(t)
	commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody), "docs: map US-001")
	if err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	cfMade(t, root, tip, commit, cfBody)
}

// A message of more than one line is the commit's message, whole: the record
// of who wrote the text and that nobody read it goes there (THREATS T7).
func TestCommitFileKeepsAMessageOfSeveralLines(t *testing.T) {
	t.Parallel()
	root, tip := cfRepo(t)
	message := "docs: map US-001\n\nWritten by the story's bookkeeper and applied by sdlc stop.\n-- not an option\n"
	commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody+"\nMore.\n"), message)
	if err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	if got := wtGitOut(t, root, "log", "-1", "--format=%B", commit); got != strings.TrimSpace(message) {
		t.Errorf("the commit's message is\n%q\nwant\n%q", got, strings.TrimSpace(message))
	}
}

// THREATS T1: the commit is made from the parent the caller read and from
// nothing a checkout holds. No ref moves, HEAD stays, and the index and the
// working tree are as they were, with whatever a person left in them.
func TestAC4CommitFileMovesNoRefAndTouchesNoIndexOrWorkingTree(t *testing.T) {
	t.Parallel()
	root, tip := cfRepo(t)
	// A person's work in the checkout that has trunk checked out: an edit to
	// the map itself, a staged file, an untracked one. THREATS T8.
	write(t, root, cfMap, cfBody+"\nA person's line, not committed.\n")
	write(t, root, "staged.go", "package billing\n")
	run(t, root, "add", "--", "staged.go")
	write(t, root, "loose.txt", "loose\n")
	before := cfState(t, root)

	commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody+"\n## refund.go\n\nKeeps refunds.\n"), "docs: map US-001")
	if err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	cfMade(t, root, tip, commit, cfBody+"\n## refund.go\n\nKeeps refunds.\n")
	if got := cfState(t, root); got != before {
		t.Errorf("CommitFile changed the refs, HEAD, the index or what git status says:\nbefore\n%s\nafter\n%s", before, got)
	}
	if got := lgRead(t, root, cfMap); got != cfBody+"\nA person's line, not committed.\n" {
		t.Errorf("the person's uncommitted CODEMAP.md is now %q", got)
	}
	if got := wtGitOut(t, root, "diff-tree", "-r", "--name-only", "--no-commit-id", commit+"^", commit); got != cfMap {
		t.Errorf("the commit carries %q, want CODEMAP.md alone: nothing staged or loose rides along", got)
	}
}

// The landing runs in the story's worktree, whose index and HEAD are the
// story's. The commit is the same from there.
func TestCommitFileFromALinkedWorktreeBuildsOnTheParentAndNotOnThatCheckout(t *testing.T) {
	t.Parallel()
	root, tip := cfRepo(t)
	wt := lgStory(t, root)
	write(t, wt, "story.go", "package billing\n")
	lgCommit(t, wt, "the story's own commit")
	write(t, wt, "half.go", "package billing\n")
	run(t, wt, "add", "--", "half.go")
	before := cfState(t, wt)

	commit, err := CommitFile(t.Context(), wt, tip, cfMap, []byte(cfBody+"\nMore.\n"), "docs: map US-001")
	if err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	cfMade(t, root, tip, commit, cfBody+"\nMore.\n")
	if got := cfState(t, wt); got != before {
		t.Errorf("CommitFile changed the worktree it ran in:\nbefore\n%s\nafter\n%s", before, got)
	}
}

// THREATS T2, bytes that are not text: the commit is made from the bytes as
// given. No clean filter, no end-of-line conversion and no attribute of the
// repository sees them, so the caller's own checks are the only ones.
func TestCommitFileCommitsTheBytesAsGivenPastEveryFilter(t *testing.T) {
	t.Parallel()
	root, _ := cfRepo(t)
	bin, marks := lgTempDir(t), lgTempDir(t)
	run(t, root, "config", "filter.steer.clean", "'"+rpScript(t, bin, marks, "clean", "cat")+"'")
	run(t, root, "config", "filter.steer.smudge", "'"+rpScript(t, bin, marks, "smudge", "cat")+"'")
	run(t, root, "config", "core.autocrlf", "true")
	write(t, root, ".gitattributes", "* text=auto eol=lf\nCODEMAP.md filter=steer text eol=lf\n")
	write(t, root, ".git/info/attributes", "CODEMAP.md filter=steer\n")
	tip := lgCommit(t, root, "attributes that would rewrite the map")
	// Committing the attributes ran the filters; only what CommitFile runs counts.
	for _, name := range []string{"clean", "smudge"} {
		if err := os.Remove(filepath.Join(marks, name)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	content := "# CODEMAP\r\n\r\nA line with a NUL \x00 in it.\r\n"

	commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(content), "docs: map US-001")
	if err != nil {
		t.Fatalf("CommitFile: %v", err)
	}
	rpNoneRan(t, marks)
	cfMade(t, root, tip, commit, content)
}

// ---------------------------------------------------------------- sealed

// THREATS T7: no hook the repository configures sees the commit, nothing is
// signed, and no editor is asked. Each of these would run a program the
// session can write, with the privileges of whoever ran the stop.
func TestCommitFileRunsNoHookSignsNothingAndAsksNoEditor(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(t *testing.T, root, bin, marks string)
	}{
		{name: "the hooks", set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			for _, name := range []string{"pre-commit", "prepare-commit-msg", "commit-msg", "post-commit", "post-rewrite", "reference-transaction", "post-index-change"} {
				rpScript(t, bin, marks, name, "echo written > '"+filepath.ToSlash(filepath.Join(root, "by-a-hook.txt"))+"'; exit 0")
			}
			run(t, root, "config", "core.hooksPath", filepath.ToSlash(bin))
		}},
		{name: "a hook that refuses", set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			rpScript(t, bin, marks, "pre-commit", "exit 1")
			rpScript(t, bin, marks, "commit-msg", "exit 1")
			run(t, root, "config", "core.hooksPath", filepath.ToSlash(bin))
		}},
		{name: "commit.gpgSign and a signing program", set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			run(t, root, "config", "commit.gpgSign", "true")
			run(t, root, "config", "gpg.program", rpScript(t, bin, marks, "gpg", "exit 1"))
		}},
		{name: "an editor", set: func(t *testing.T, root, bin, marks string) {
			t.Helper()
			editor := "'" + rpScript(t, bin, marks, "editor", "exit 1") + "'"
			run(t, root, "config", "core.editor", editor)
			t.Setenv("GIT_EDITOR", editor)
		}},
		{name: "a commit template and a clean-up mode", set: func(t *testing.T, root, bin, _ string) {
			t.Helper()
			write(t, bin, "template.txt", "# fill this in\n")
			run(t, root, "config", "commit.template", filepath.ToSlash(filepath.Join(bin, "template.txt")))
			run(t, root, "config", "commit.cleanup", "scissors")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, tip := cfRepo(t)
			bin, marks := lgTempDir(t), lgTempDir(t)
			tc.set(t, root, bin, marks)

			commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody+"\nMore.\n"), "docs: map US-001")
			rpNoneRan(t, marks)
			if err != nil {
				t.Fatalf("CommitFile: %v", err)
			}
			cfMade(t, root, tip, commit, cfBody+"\nMore.\n")
			if got := wtGitOut(t, root, "log", "-1", "--format=%G?|%s", commit); got != "N|docs: map US-001" {
				t.Errorf("the commit's signature and subject are %q, want none and the message as given", got)
			}
			if lgRead(t, root, "by-a-hook.txt") != "" {
				t.Error("a hook of the repository wrote a file")
			}
		})
	}
}

// THREATS T7: with no committer git cannot make the commit. That is an error a
// person can read, a failed map step for the caller, and never a commit under
// a name made up from the machine's.
func TestAC6CommitFileWithNoCommitterFailsAndMakesNothing(t *testing.T) {
	t.Parallel()
	root, tip := cfRepo(t)
	run(t, root, "config", "--unset", "user.email")
	run(t, root, "config", "--unset", "user.name")
	run(t, root, "config", "user.useConfigOnly", "true")
	before := cfState(t, root)

	commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody+"\nMore.\n"), "docs: map US-001")
	cfRefused(t, commit, err, root, before)
	lgSays(t, err)
}

// ---------------------------------------------------------------- refused

// AC-5 and THREATS T1: the check is made on the commit. Where trunk's
// CODEMAP.md is a directory, putting a file there takes every path beneath it
// away, so there is no commit and the error names those paths.
func TestAC5CommitFileRefusesWhereThePathIsADirectoryAndNamesWhatIsBeneathIt(t *testing.T) {
	t.Parallel()
	root, _ := rpRepo(t)
	write(t, root, "CODEMAP.md/notes.md", "kept by hand\n")
	write(t, root, "CODEMAP.md/deeper/more.md", "and more\n")
	tip := lgCommit(t, root, "the map is a directory")
	before := cfState(t, root)

	commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody), "docs: map US-001")
	cfRefused(t, commit, err, root, before)
	lgSays(t, err, "CODEMAP.md/notes.md", "CODEMAP.md/deeper/more.md")
}

// AC-6: a link or a submodule at the path changes no other path, and is still
// not a plain file to put text in. Git's own objects say which it is.
func TestAC6CommitFileRefusesWhereThePathIsALinkOrASubmodule(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, mode string }{
		{"a symbolic link", "120000"},
		{"a submodule", "160000"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, first := rpRepo(t)
			object := first // a gitlink names a commit
			if tc.mode == "120000" {
				write(t, root, "target.txt", "notes.txt")
				object = wtGitOut(t, root, "hash-object", "-w", "--", "target.txt")
				if err := os.Remove(filepath.Join(root, "target.txt")); err != nil {
					t.Fatal(err)
				}
			}
			run(t, root, "update-index", "--add", "--cacheinfo", tc.mode+","+object+","+cfMap)
			run(t, root, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "the map is "+tc.name)
			tip := lgRev(t, root, "HEAD")
			if got := wtGitOut(t, root, "ls-tree", tip, "--", cfMap); !strings.HasPrefix(got, tc.mode+" ") {
				t.Fatalf("precondition: trunk has CODEMAP.md as %q, want mode %s", got, tc.mode)
			}
			before := cfState(t, root)

			commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody), "docs: map US-001")
			cfRefused(t, commit, err, root, before)
			lgSays(t, err, cfMap)
		})
	}
}

// A commit that changes nothing is not a change to the map: the caller is told
// rather than handed a commit trunk would move to for no reason.
func TestCommitFileRefusesContentTheParentAlreadyHolds(t *testing.T) {
	t.Parallel()
	root, tip := cfRepo(t)
	before := cfState(t, root)
	commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody), "docs: map US-001")
	cfRefused(t, commit, err, root, before)
	lgSays(t, err, cfMap)
}

// The parent is a commit the caller read, by its full hash, and nothing git
// would resolve for it: a branch can move between the read and the commit, and
// a word that starts with a dash is an option.
func TestCommitFileRefusesAParentThatIsNotACommitsFullHash(t *testing.T) {
	t.Parallel()
	root, tip := cfRepo(t)
	tree := wtGitOut(t, root, "rev-parse", tip+"^{tree}")
	before := cfState(t, root)
	for name, parent := range map[string]string{
		"nothing":               "",
		"a branch":              "main",
		"HEAD":                  "HEAD",
		"a short hash":          tip[:12],
		"an option":             "--orphan",
		"a hash git has not":    strings.Repeat("1", 40),
		"git's name for none":   strings.Repeat("0", 40),
		"a tree":                tree,
		"a hash in upper case":  strings.ToUpper(tip),
		"a hash and a suffix":   tip + "^",
		"a hash with a newline": tip + "\n" + tip,
	} {
		t.Run(name, func(t *testing.T) {
			commit, err := CommitFile(t.Context(), root, parent, cfMap, []byte(cfBody+"\nMore.\n"), "docs: map US-001")
			cfRefused(t, commit, err, root, before)
			lgSays(t, err)
		})
	}
}

// THREATS T1: the path is the caller's constant, and is still checked. One
// that leaves the repository, names git's own directory, or reads as an
// option is refused before git sees it.
func TestCommitFileRefusesAPathThatIsNotAPlainOneInsideTheRepository(t *testing.T) {
	t.Parallel()
	root, tip := cfRepo(t)
	before := cfState(t, root)
	for name, path := range map[string]string{
		"nothing":               "",
		"the repository itself": ".",
		"a parent directory":    "../CODEMAP.md",
		"a parent, further in":  "docs/../../CODEMAP.md",
		"an absolute path":      filepath.ToSlash(filepath.Join(root, cfMap)),
		"git's own directory":   ".git/config",
		"an option":             "--stdin",
		"a trailing slash":      "CODEMAP.md/",
		"a newline":             "CODEMAP.md\nother.go",
		"a NUL":                 "CODEMAP.md\x00other.go",
		"a backslash":           `docs\CODEMAP.md`,
	} {
		t.Run(name, func(t *testing.T) {
			commit, err := CommitFile(t.Context(), root, tip, path, []byte(cfBody+"\nMore.\n"), "docs: map US-001")
			cfRefused(t, commit, err, root, before)
			lgSays(t, err)
		})
	}
}

func TestCommitFileRefusesAnEmptyMessage(t *testing.T) {
	t.Parallel()
	root, tip := cfRepo(t)
	before := cfState(t, root)
	for _, message := range []string{"", "\n\n", "   "} {
		commit, err := CommitFile(t.Context(), root, tip, cfMap, []byte(cfBody+"\nMore.\n"), message)
		cfRefused(t, commit, err, root, before)
	}
}

// A directory that is no repository is an error, not a commit somewhere else.
func TestCommitFileOutsideARepositoryFails(t *testing.T) {
	t.Parallel()
	_, tip := cfRepo(t)
	commit, err := CommitFile(t.Context(), lgTempDir(t), tip, cfMap, []byte(cfBody), "docs: map US-001")
	if err == nil || commit != "" {
		t.Errorf("CommitFile outside a repository returned %q, %v", commit, err)
	}
}
