// Package gitx runs the few git commands the loop cannot do without.
//
// It is deliberately thin. Everything else in this tool works on the file
// system directly, because a subprocess is slow, is a dependency, and fails in
// ways that are hard to explain to a user. Git earns its place here for one
// thing only: it already knows which files are part of the project and which
// are build output, vendored code or anything else the ignore rules exclude.
// Walking the tree ourselves would mean reimplementing that, badly.
package gitx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// Files lists the repository's working tree: everything git tracks, plus the
// files that are not tracked yet and not ignored. New tests are untracked until
// they are committed, so both halves matter.
//
// Paths come back repository-relative and slash-separated, sorted, with no
// duplicates -- the same shape the rest of the loop uses.
func Files(ctx context.Context, root string) ([]string, error) {
	out, err := git(ctx, root, nil, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	files := make([]string, 0, 64)
	for _, p := range strings.Split(string(out), "\x00") {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		files = append(files, p)
	}
	sort.Strings(files)
	return files, nil
}

// TreeHash is the hash of the working tree as git would store it: every file
// git tracks or would track, by content, in one identifier.
//
// It is computed in a temporary index so that the repository's own index is
// untouched. The loop records this against a review, because an approval of
// code that has since changed is not an approval, and without recording what
// was in front of the reviewer there is no way to tell the two apart.
//
// The loop's own directory is left out. Recording a review writes a review file
// and a gate record, both under .sdlc/, so a hash that included them would make
// every review stale the instant it was filed. What a reviewer of code is
// looking at is the code.
//
// An override hashes a file as other content than it has on disk, which is how
// a caller leaves out what in a file is not the work. It stands in only for a
// file the tree already has: one git ignores, or one under .sdlc/, stays out.
func TreeHash(ctx context.Context, root string, overrides ...Override) (string, error) {
	// The directory is created, the index file inside it is not: git writes the
	// index itself and refuses to read an empty file as one.
	dir, err := os.MkdirTemp("", "sdlc-index-")
	if err != nil {
		return "", sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the working tree could not be measured",
			"a temporary directory could not be created").WithCause(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	env := append(os.Environ(), "GIT_INDEX_FILE="+filepath.Join(dir, "index"))
	if _, err := git(ctx, root, env, "add", "-A", "--", ".", ":(exclude).sdlc"); err != nil {
		return "", err
	}
	for _, o := range overrides {
		staged, err := git(ctx, root, env, "ls-files", "--stage", "--", ":(literal)"+o.Path)
		if err != nil {
			return "", err
		}
		mode, _, inTree := strings.Cut(firstLine(string(staged)), " ")
		if !inTree {
			continue
		}
		blob, err := gitInput(ctx, root, env, o.Content, "hash-object", "-w", "--stdin", "--path="+o.Path)
		if err != nil {
			return "", err
		}
		if _, err := git(ctx, root, env, "update-index", "--cacheinfo",
			mode+","+firstLine(string(blob))+","+o.Path); err != nil {
			return "", err
		}
	}
	out, err := git(ctx, root, env, "write-tree")
	if err != nil {
		return "", err
	}
	return firstLine(string(out)), nil
}

// Clean reports whether the working tree has nothing uncommitted, which is what
// the commit gate means by "committed".
func Clean(ctx context.Context, root string) (bool, error) {
	out, err := git(ctx, root, nil, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "", nil
}

// NoCommits stands for HEAD in a repository with no commits yet, as git's
// all-zero object name stands for a ref that does not exist.
const NoCommits = "0000000000000000000000000000000000000000"

// Head is the commit HEAD is on, or NoCommits in a repository with no commits
// yet. Read as "", which a record from before HEAD was kept also holds, the
// first story of a new project was held to no HEAD at all.
func Head(ctx context.Context, root string) (string, error) {
	out, err := git(ctx, root, nil, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		// A repository with no commits is still on a branch; a directory that
		// is not a repository is on none.
		if _, branchErr := Branch(ctx, root); branchErr == nil {
			return NoCommits, nil
		}
		return "", err
	}
	return firstLine(string(out)), nil
}

// Resolve is the commit a revision names, in full: a hash, a tag, HEAD~3. What
// git cannot read as a commit is refused with what git said. A revision that
// starts with a dash is refused before git sees it, because git would read it
// as an option, and every hash Resolve returns is safe to pass on.
//
// NamesNoCommit tells a refusal from git failing to run at all.
func Resolve(ctx context.Context, root, rev string) (string, error) {
	if strings.HasPrefix(rev, "-") {
		return "", notACommit{sdlcerr.New(sdlcerr.RepositoryUnreadable,
			strconv.Quote(rev)+" is not a commit",
			"no revision starts with a dash, and git would read one that does as an option")}
	}
	out, err := git(ctx, root, nil, "rev-parse", "--verify", rev+"^{commit}")
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return "", notACommit{err}
	case err != nil:
		return "", err
	}
	return firstLine(string(out)), nil
}

// notACommit is Resolve's refusal: git ran, or would have, and the revision
// names no commit.
type notACommit struct{ error }

func (e notACommit) Unwrap() error { return e.error }

// NamesNoCommit reports whether err is Resolve saying a revision names no
// commit, rather than git not running: missing from PATH, or stopped. Only the
// first is reason to tell somebody the commit is not there.
func NamesNoCommit(err error) bool {
	var refused notACommit
	return errors.As(err, &refused)
}

// IsAncestor reports whether ancestor is in descendant's history, which a
// commit is in its own. Git says no with exit status 1; anything else it says,
// such as a commit it does not have, is a failure rather than a no. Both are
// full hashes git has printed, so neither can be read as an option.
func IsAncestor(ctx context.Context, root, ancestor, descendant string) (bool, error) {
	_, err := git(ctx, root, nil, "merge-base", "--is-ancestor", ancestor, descendant)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	}
	return false, err
}

// Commit is one commit, with its first parent: "" for a root commit.
type Commit struct {
	Hash   string
	Parent string
}

// CommitsNaming lists the commits in HEAD's history whose message names word,
// newest first. Git finds the candidates by fixed string, and the whole word is
// checked here: US-1 is not named by "US-10" or "US-1.2", and is by "US-1.".
func CommitsNaming(ctx context.Context, root, word string) ([]Commit, error) {
	if word == "" {
		return nil, nil
	}
	// -z ends each commit with a NUL, which no commit message can hold; the
	// first line is the commit and its parents, and the rest is the message.
	out, err := git(ctx, root, nil, "log", "-z", "--fixed-strings", "--grep="+word,
		"--format=%H %P%n%B", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for entry := range strings.SplitSeq(string(out), "\x00") {
		header, message, ok := strings.Cut(entry, "\n")
		fields := strings.Fields(header)
		if !ok || len(fields) == 0 || !namesWord(message, word) {
			continue
		}
		c := Commit{Hash: fields[0]}
		if len(fields) > 1 {
			c.Parent = fields[1]
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// namesWord reports whether word appears in text with nothing carrying it on at
// either edge.
func namesWord(text, word string) bool {
	for from := 0; from <= len(text)-len(word); {
		i := strings.Index(text[from:], word)
		if i < 0 {
			return false
		}
		start := from + i
		if !carriesOn(text, start-1, -1) && !carriesOn(text, start+len(word), 1) {
			return true
		}
		from = start + 1
	}
	return false
}

// carriesOn reports whether the byte at i continues a word past its edge, going
// in direction step: a letter, a digit, a dash or an underscore does, and so
// does a dot with one of those beyond it. A dot on its own ends a sentence.
func carriesOn(text string, i, step int) bool {
	if i < 0 || i >= len(text) {
		return false
	}
	if wordByte(text[i]) {
		return true
	}
	next := i + step
	return text[i] == '.' && next >= 0 && next < len(text) && wordByte(text[next])
}

func wordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '-' || b == '_'
}

// Changes lists every path with uncommitted changes, tracked or not, the way git
// reports them: repository-relative and slash-separated, with a directory in
// which nothing is tracked reported once, as the directory.
func Changes(ctx context.Context, root string) ([]string, error) {
	out, err := git(ctx, root, nil, "status", "--porcelain", "-z", "--untracked-files=normal")
	if err != nil {
		return nil, err
	}
	var paths []string
	entries := strings.Split(string(out), "\x00")
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 {
			continue
		}
		paths = append(paths, entry[3:])
		// A rename or a copy is followed by the path it came from.
		if (entry[0] == 'R' || entry[0] == 'C') && i+1 < len(entries) {
			i++
			paths = append(paths, entries[i])
		}
	}
	return paths, nil
}

// Branch is the branch HEAD is on, or "" when HEAD is detached. A repository
// with no commits yet is on the branch its first commit will be made on.
func Branch(ctx context.Context, root string) (string, error) {
	out, err := git(ctx, root, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil {
		return firstLine(string(out)), nil
	}
	if head, headErr := git(ctx, root, nil, "rev-parse", "--abbrev-ref", "HEAD"); headErr == nil &&
		firstLine(string(head)) == "HEAD" {
		return "", nil
	}
	return "", err
}

// Trunk is what this repository most likely calls its trunk, for a
// configuration that does not say yet: the branch origin's HEAD names, then
// main or master if there is one, then the branch HEAD is on -- which, in a
// repository with no commits yet, is the one its first commit will be made on.
// It is "" when none of those answers, and the caller has a default for that.
func Trunk(ctx context.Context, root string) string {
	if out, err := git(ctx, root, nil, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if name, ok := strings.CutPrefix(firstLine(string(out)), "origin/"); ok && name != "" {
			return name
		}
	}
	for _, name := range []string{"main", "master"} {
		if _, err := git(ctx, root, nil, "show-ref", "--verify", "--quiet", "refs/heads/"+name); err == nil {
			return name
		}
	}
	branch, _ := Branch(ctx, root)
	return branch
}

// Behind fetches branch from remote and counts the commits on it that HEAD does
// not have. It never prompts: a remote that wants a password it has not been
// given is a remote that could not be asked.
func Behind(ctx context.Context, root, remote, branch string) (int, error) {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if _, err := git(ctx, root, env, "fetch", "--quiet", remote, branch); err != nil {
		return 0, err
	}
	out, err := git(ctx, root, nil, "rev-list", "--count", "HEAD..FETCH_HEAD")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(firstLine(string(out)))
	if err != nil {
		return 0, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"git rev-list did not report a count",
			"git said: "+firstLine(string(out))).WithCause(err)
	}
	return n, nil
}

// ChangedLines counts the lines added and removed between HEAD and tree, the
// way `git diff --numstat` counts them, with renames found so that moving a
// file is not counted as writing it again. The loop's own directory is left out,
// as TreeHash leaves it out, and so is every path in exclude. A binary file has
// no lines, and counts as none.
func ChangedLines(ctx context.Context, root, tree string, exclude ...string) (int, error) {
	args := []string{"diff", "--numstat", "--find-renames", "HEAD", tree, "--", ".", ":(exclude).sdlc"}
	for _, p := range exclude {
		args = append(args, ":(exclude,literal)"+p)
	}
	out, err := git(ctx, root, nil, args...)
	if err != nil {
		return 0, err
	}
	total := 0
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		added, rest, _ := strings.Cut(line, "\t")
		removed, _, _ := strings.Cut(rest, "\t")
		for _, n := range []string{added, removed} {
			if count, err := strconv.Atoi(n); err == nil {
				total += count
			}
		}
	}
	return total, nil
}

// Override is content to hash in place of a file's own; see TreeHash. Path is
// repository-relative and slash-separated.
type Override struct {
	Path    string
	Content []byte
}

func git(ctx context.Context, root string, env []string, args ...string) ([]byte, error) {
	return gitInput(ctx, root, env, nil, args...)
}

// gitInput is git with something on its standard input.
func gitInput(ctx context.Context, root string, env []string, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = root
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = strings.NewReader(string(stdin))
	}

	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"git "+args[0]+" did not finish",
			describeGitFailure(stderr.String())).WithCause(err)
	}
	return out, nil
}

func describeGitFailure(stderr string) string {
	if line := firstLine(stderr); line != "" {
		return "git said: " + line
	}
	return "git ls-files did not finish; it may not be installed, or this may " +
		"not be a repository git can read"
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}
