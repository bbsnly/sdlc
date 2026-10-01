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
	"slices"
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

	env := []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index")}
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
		if o.At != "" {
			// A path holding files beneath it is a directory where the
			// override stands in for one file. git refuses the entry then, and
			// a caller that read that as a tree it could not measure let the
			// shape through unchecked.
			if holdsBeneath(string(staged), o.Path) {
				return "", ErrOverrideNotAFile
			}
			if err := takeAt(ctx, root, env, o); err != nil {
				return "", err
			}
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

// ErrOverrideNotAFile is an override taken from a commit whose path the tree
// holds files beneath: a directory where the override stands in for one file.
var ErrOverrideNotAFile = errors.New("the override's path holds files beneath it")

// holdsBeneath reports whether the output of ls-files --stage names any entry
// beneath path rather than path itself.
func holdsBeneath(staged, path string) bool {
	for line := range strings.Lines(staged) {
		if _, name, ok := strings.Cut(strings.TrimRight(line, "\n"), "\t"); ok && name != path {
			return true
		}
	}
	return false
}

// takeAt puts o.Path into the index env names as the commit o.At has it: its
// entry, mode and object together, and out of the index where the commit has
// no file there. The object is the commit's, so no content is read or written.
func takeAt(ctx context.Context, root string, env []string, o Override) error {
	if err := fullHashes(o.At); err != nil {
		return err
	}
	out, err := git(ctx, root, env, "ls-tree", "-z", o.At, "--", o.Path)
	if err != nil {
		return err
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		meta, name, ok := strings.Cut(entry, "\t")
		fields := strings.Fields(meta)
		if !ok || name != o.Path || len(fields) != 3 || fields[1] != "blob" {
			continue
		}
		_, err := git(ctx, root, env, "update-index", "--cacheinfo", fields[0]+","+fields[2]+","+o.Path)
		return err
	}
	_, err = git(ctx, root, env, "update-index", "--force-remove", "--", o.Path)
	return err
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

// Commit is one commit, with its first parent: "" for a root commit.
type Commit struct {
	Hash   string
	Parent string
}

// GitDir is the repository's git directory, absolute and in the platform's own
// separators. It is where the loop keeps what must never be seen by `git
// status`: anything in the working tree is, and the commit gate refuses a tree
// with anything uncommitted in it. A linked worktree has a git directory of its
// own.
func GitDir(ctx context.Context, root string) (string, error) {
	out, err := git(ctx, root, nil, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(firstLine(string(out))), nil
}

// CommitsBetween lists the commits after base up to and including end, newest
// first, each with its first parent -- what `git rev-list base..end` lists. A
// revision git does not have is a failure rather than an empty range: read as
// empty, it would say the story committed nothing. A revision that starts with
// a dash is refused before git sees it, as Resolve refuses one.
func CommitsBetween(ctx context.Context, root, base, end string) ([]Commit, error) {
	for _, rev := range []string{base, end} {
		if strings.HasPrefix(rev, "-") || rev == "" {
			return nil, notACommit{sdlcerr.New(sdlcerr.RepositoryUnreadable,
				strconv.Quote(rev)+" is not a commit",
				"no revision is empty or starts with a dash, and git would read one that does as an option")}
		}
	}
	out, err := git(ctx, root, nil, "rev-list", "--parents", base+".."+end, "--")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
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
//
// The full ref, with refs/heads/ taken off: git shortens a name that a tag
// shares to heads/<name>, so with a tag called main, HEAD on main read as a
// branch called heads/main.
func Branch(ctx context.Context, root string) (string, error) {
	out, err := git(ctx, root, nil, "symbolic-ref", "--quiet", "HEAD")
	if err == nil {
		if name, ok := strings.CutPrefix(firstLine(string(out)), "refs/heads/"); ok {
			return name, nil
		}
		return "", nil
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

// Behind fetches branch from remote and counts the commits on it that this
// repository's own branch of that name does not have. It never prompts: a
// remote that wants a password it has not been given is a remote that could
// not be asked.
//
// It counts from the branch and not from HEAD, because a story is cut from the
// branch wherever HEAD is: counted from HEAD, a checkout on a branch that had
// the remote's commits hid a trunk two behind. A branch this repository does
// not have has no tip to count from, and is a failure rather than none behind.
func Behind(ctx context.Context, root, remote, branch string) (int, error) {
	for _, name := range []string{remote, branch} {
		if name == "" || strings.HasPrefix(name, "-") {
			return 0, sdlcerr.New(sdlcerr.RepositoryUnreadable,
				strconv.Quote(name)+" cannot be asked about",
				"no remote or branch is empty or starts with a dash, and git would read one that does as an option")
		}
	}
	local := "refs/heads/" + branch
	if _, err := git(ctx, root, nil, "rev-parse", "--verify", "--quiet", local+"^{commit}"); err != nil {
		return 0, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			strconv.Quote(branch)+" is not a branch with a commit in this repository",
			"trunk is counted from the branch of that name here, and there is none to count from").WithCause(err)
	}
	if _, err := git(ctx, root, []string{"GIT_TERMINAL_PROMPT=0"}, "fetch", "--quiet", remote, branch); err != nil {
		return 0, err
	}
	out, err := git(ctx, root, nil, "rev-list", "--count", local+"..FETCH_HEAD")
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

// Override is content to hash in place of a file's own; see TreeHash. Path is
// repository-relative and slash-separated. Where At names a commit, the file is
// hashed as that commit has it, and Content is not read.
type Override struct {
	Path    string
	Content []byte
	At      string
}

// git runs git in root, the root of a checkout, with the environment extra adds
// to Environ's.
func git(ctx context.Context, root string, extra []string, args ...string) ([]byte, error) {
	return gitInput(ctx, root, extra, nil, args...)
}

// gitInput is git with something on its standard input.
//
// It is told the work tree as well as run in it. --work-tree outranks
// core.worktree from any of git's configuration channels, a file or the
// environment, either of which could otherwise send what gitx measures, or the
// worktree start makes, to another directory.
func gitInput(ctx context.Context, root string, extra []string, stdin []byte, args ...string) ([]byte, error) {
	full := args
	if root != "" {
		if abs, err := filepath.Abs(root); err == nil {
			full = append([]string{"--work-tree=" + abs}, args...)
		}
	}
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = root
	cmd.Env = Environ(extra...)
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

// locationVariables are what git reads, before the directory it runs in, to
// decide which repository, index and objects it works on.
var locationVariables = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX",
}

// Environ is this process's environment without git's location variables,
// followed by extra, for a git command, or a command of the project's, run in
// a checkout that is named. Inherited, each of them sent what the loop
// measured -- the commit a story's base is taken from, the tree its reviews
// are stamped with -- to whatever repository it named: the test harness set
// two of them, and a git hook sets GIT_INDEX_FILE for everything it runs.
// Names are compared as Windows compares them, without regard to case.
func Environ(extra ...string) []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+len(extra))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(locationVariables, func(v string) bool { return strings.EqualFold(v, name) }) {
			out = append(out, kv)
		}
	}
	return append(out, extra...)
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
