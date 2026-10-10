package gitx

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// Landing a finished story: what the landing reads, the fast-forward that
// moves trunk, and taking the landed story's worktree away. Every git call
// here is sealed, reads included, because each answer decides what reaches
// trunk, and the repository it asks is one a session could write.

// The causes FastForward is refused for, which errors.Is finds in what it
// returns.
var (
	// ErrNotFastForward is trunk not at the commit the caller read, or a
	// commit to move it to that does not follow on from that one in a line.
	ErrNotFastForward = errors.New("trunk cannot be fast-forwarded to the commit")
	// ErrWouldOverwrite is a checkout that has trunk checked out holding
	// something, uncommitted or untracked, where the fast-forward writes.
	ErrWouldOverwrite = errors.New("a checkout of trunk holds changes where the fast-forward writes")
)

// unsealed are the variables of this process's environment a sealed call
// runs without, on top of Environ's. Each names a program, configuration, a
// replacement or a graft, or changes how a pathspec or a patch is read, and
// whoever runs sdlc can set any of them on the command line that runs it. The
// files a person's own git reads, GIT_CONFIG_GLOBAL among them, are kept: the
// repository's configuration is as open to a session as they are, and the
// settings that matter here are handed to git outright.
var unsealed = []string{
	"GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	"GIT_EXTERNAL_DIFF", "GIT_DIFF_OPTS",
	"GIT_REPLACE_REF_BASE", "GIT_NO_REPLACE_OBJECTS", "GIT_GRAFT_FILE",
	"GIT_ICASE_PATHSPECS", "GIT_LITERAL_PATHSPECS", "GIT_GLOB_PATHSPECS", "GIT_NOGLOB_PATHSPECS",
	"GIT_ATTR_SOURCE",
}

// sealedEnviron is the environment of a sealed call whose private directory
// is private.
func sealedEnviron(private string) []string {
	env := Environ()
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		name = strings.ToUpper(name)
		if slices.Contains(unsealed, name) ||
			strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GIT_GRAFT_FILE="+filepath.Join(private, "grafts"), "GIT_NO_REPLACE_OBJECTS=1")
}

// sealed runs git in dir as every call of the landing's runs: in a private
// directory of its own, removed when it returns, where the hooks and the graft
// file git is pointed at are not, so that no hook runs even for a read that
// writes the index; with no replacement objects; and with the file system
// monitor, the commit-graph and automatic maintenance off, each of which is a
// program or a file that would answer in the objects' place. A shallow file is
// read as git reads it: it can only hide ancestry, and hidden ancestry refuses.
func sealed(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return sealedCall(ctx, dir, nil, nil, nil, args...)
}

// sealedCall is sealed with more settings handed to git as -c, something on
// its standard input, and its standard output sent to stdout where that is
// not nil.
func sealedCall(ctx context.Context, dir string, settings []string, stdin []byte, stdout io.Writer, args ...string) ([]byte, error) {
	private, err := os.MkdirTemp("", "sdlc-git-")
	if err != nil {
		return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"git "+args[0]+" could not be run",
			"a private temporary directory for it could not be made").WithCause(err)
	}
	defer func() { _ = os.RemoveAll(private) }()

	full := []string{"--no-replace-objects",
		"-c", "core.hooksPath=" + filepath.ToSlash(filepath.Join(private, "hooks")),
		"-c", "core.fsmonitor=",
		"-c", "core.commitGraph=false",
		"-c", "gc.auto=0",
		"-c", "maintenance.auto=false",
	}
	for _, kv := range settings {
		full = append(full, "-c", kv)
	}
	if dir != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			full = append(full, "--work-tree="+abs)
		}
	}
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	cmd.Dir = dir
	cmd.Env = sealedEnviron(private)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	if stdout != nil {
		cmd.Stdout = stdout
	}
	stderr := &firstBytes{limit: 4096}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"git "+args[0]+" did not finish",
			describeGitFailure(args[0], string(stderr.kept))).WithCause(err)
	}
	return out.Bytes(), nil
}

// firstBytes keeps the first limit bytes written to it, which is as much of
// git's standard error as a refusal quotes.
type firstBytes struct {
	kept  []byte
	limit int
}

func (w *firstBytes) Write(p []byte) (int, error) {
	if room := w.limit - len(w.kept); room > 0 {
		w.kept = append(w.kept, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// fullHash reports whether s is a commit's full hash as git prints it: forty
// lowercase hex digits, or sixty-four in a SHA-256 repository.
func fullHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// fullHashes refuses a value that is not a full hash, before git sees it: git
// would read one that starts with a dash as an option, and a name or a short
// hash as whatever it resolves to by then.
func fullHashes(values ...string) error {
	for _, v := range values {
		if !fullHash(v) {
			return sdlcerr.New(sdlcerr.RepositoryUnreadable,
				strconv.Quote(v)+" is not a commit's full hash",
				"a commit is named here only by the full hash git printed for it, so that nothing handed "+
					"to git can be read as an option or as another commit")
		}
	}
	return nil
}

// notAnOption refuses a name or a path that is empty or starts with a dash.
func notAnOption(what, v string) error {
	if v == "" || strings.HasPrefix(v, "-") {
		return sdlcerr.New(sdlcerr.RepositoryUnreadable,
			strconv.Quote(v)+" cannot name "+what,
			"none is empty or starts with a dash, and git would read one that does as an option")
	}
	return nil
}

// branchName refuses a branch name that is empty, starts with a dash, or is
// one git does not take for a branch.
func branchName(ctx context.Context, root, branch string) error {
	if err := notAnOption("a branch", branch); err != nil {
		return err
	}
	if _, err := sealed(ctx, root, "check-ref-format", "refs/heads/"+branch); err != nil {
		return sdlcerr.New(sdlcerr.RepositoryUnreadable,
			strconv.Quote(branch)+" is not a name git takes for a branch",
			failure(err, "git check-ref-format refused it")).WithCause(err)
	}
	return nil
}

// BranchTip is the commit refs/heads/<branch> is on, and nothing else: not a
// tag that shares its name, and not a ref git would guess at from an
// abbreviation. A branch that is not there is fs.ErrNotExist.
func BranchTip(ctx context.Context, root, branch string) (string, error) {
	if err := branchName(ctx, root, branch); err != nil {
		return "", err
	}
	out, err := sealed(ctx, root, "show-ref", "--verify", "--hash", "refs/heads/"+branch)
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return "", sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"there is no branch "+strconv.Quote(branch)+" here",
			"git has no refs/heads/"+branch+" in this repository").WithCause(fs.ErrNotExist)
	case err != nil:
		return "", err
	}
	tip := firstLine(string(out))
	if !fullHash(tip) {
		return "", sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"git did not say which commit "+strconv.Quote(branch)+" is on",
			"git said: "+strconv.Quote(tip))
	}
	return tip, nil
}

// IsAncestor reports whether ancestor is in descendant's history, which a
// commit is in its own, as the objects have it. Git says no with exit status
// 1; anything else it says, such as a commit it does not have, is a failure
// rather than a no.
func IsAncestor(ctx context.Context, root, ancestor, descendant string) (bool, error) {
	if err := fullHashes(ancestor, descendant); err != nil {
		return false, err
	}
	_, err := sealed(ctx, root, "merge-base", "--is-ancestor", ancestor, descendant)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return false, nil
	}
	return false, err
}

// MergeBase is the nearest commit both a and b have in their history, and ""
// for two histories that share none.
func MergeBase(ctx context.Context, root, a, b string) (string, error) {
	if err := fullHashes(a, b); err != nil {
		return "", err
	}
	out, err := sealed(ctx, root, "merge-base", a, b)
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return "", nil
	case err != nil:
		return "", err
	}
	return firstLine(string(out)), nil
}

// HasMerges reports whether a merge commit lies in base..tip: in tip's
// history and not in base's.
func HasMerges(ctx context.Context, root, base, tip string) (bool, error) {
	if err := fullHashes(base, tip); err != nil {
		return false, err
	}
	out, err := sealed(ctx, root, "rev-list", "--min-parents=2", "--max-count=1", base+".."+tip, "--")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// ChangedUnder is the paths that differ between base and tip at dir or under
// it, a slash path from the root, compared without regard to case: a trunk
// landed from Linux is checked out on macOS and Windows as well, where
// .SDLC/notes.json is .sdlc/notes.json. The names are read whole, whatever
// they hold.
func ChangedUnder(ctx context.Context, root, base, tip, dir string) ([]string, error) {
	if err := notAnOption("a path", dir); err != nil {
		return nil, err
	}
	all, err := ChangedBetween(ctx, root, base, tip)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.Trim(dir, "/"), "/")
	var changed []string
	for _, name := range all {
		if atOrUnder(name, parts) {
			changed = append(changed, name)
		}
	}
	return changed, nil
}

// ChangedBetween is every path that differs between base and tip, with both
// names of a file that moved. One call answers for any number of paths: asked
// once for each path a rule lists, the cost grew with the configuration.
func ChangedBetween(ctx context.Context, root, base, tip string) ([]string, error) {
	if err := fullHashes(base, tip); err != nil {
		return nil, err
	}
	out, err := sealed(ctx, root, "diff-tree", "-r", "-z", "--no-renames", "--name-only",
		"--ignore-submodules=none", base, tip, "--")
	if err != nil {
		return nil, err
	}
	var changed []string
	for _, name := range strings.Split(string(out), "\x00") {
		if name != "" {
			changed = append(changed, name)
		}
	}
	return changed, nil
}

// atOrUnder reports whether the slash path name is the path made of dir's
// parts, or under it, part by part without regard to case.
func atOrUnder(name string, dir []string) bool {
	parts := strings.Split(name, "/")
	if len(parts) < len(dir) {
		return false
	}
	for i, d := range dir {
		if !strings.EqualFold(parts[i], d) {
			return false
		}
	}
	return true
}

// ModeAt is the mode of the entry at path, a slash path from the root, as
// commit has it: "100644" or "100755" for a file, "120000" for a link,
// "160000" for a submodule, "040000" for a directory, and "" where the commit
// has nothing by exactly that name.
func ModeAt(ctx context.Context, root, commit, path string) (string, error) {
	if err := fullHashes(commit); err != nil {
		return "", err
	}
	if err := notAnOption("a path", path); err != nil {
		return "", err
	}
	out, err := sealed(ctx, root, "ls-tree", "-z", "--full-tree", commit, "--", path)
	if err != nil {
		return "", err
	}
	for _, entry := range strings.Split(string(out), "\x00") {
		meta, name, ok := strings.Cut(entry, "\t")
		if fields := strings.Fields(meta); ok && name == path && len(fields) == 3 {
			return fields[0], nil
		}
	}
	return "", nil
}

// FileAt is the file at path, a slash path from the root, as commit has it,
// whatever the working tree holds. A path the commit does not have is
// fs.ErrNotExist.
func FileAt(ctx context.Context, root, commit, path string) ([]byte, error) {
	if err := fullHashes(commit); err != nil {
		return nil, err
	}
	if err := notAnOption("a path", path); err != nil {
		return nil, err
	}
	if strings.ContainsAny(path, "\n\x00") {
		return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			strconv.Quote(path)+" cannot be read from a commit here",
			"a path with a line break in it cannot be handed to git a line at a time")
	}
	// One call: the commit first, so that a commit git does not have is told
	// apart from a path the commit does not have.
	out, err := sealedCall(ctx, root, nil, []byte(commit+"^{commit}\n"+commit+":"+path+"\n"), nil,
		"cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	rest := out
	var body []byte
	for i, want := range []string{"commit", "blob"} {
		header, after, ok := bytes.Cut(rest, []byte("\n"))
		fields := strings.Fields(string(header))
		if !ok || len(fields) != 3 || !fullHash(fields[0]) {
			if i == 1 && bytes.HasSuffix(header, []byte(" missing")) {
				return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
					strconv.Quote(path)+" is not in commit "+commit[:12],
					"the commit has no file at that path").WithCause(fs.ErrNotExist)
			}
			return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
				strconv.Quote(path)+" could not be read from commit "+commit[:12],
				"git said: "+strconv.Quote(string(header)))
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || fields[1] != want || size+1 > len(after) {
			return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
				strconv.Quote(path)+" could not be read from commit "+commit[:12],
				"git said: "+strconv.Quote(string(header)))
		}
		body, rest = after[:size], after[size+1:]
	}
	return body, nil
}

// Uncommitted is every path git status lists in checkout: each tracked file
// whose index entry or working copy differs from HEAD, and, with untracked,
// each file git neither tracks nor ignores. What a command run there changed
// that its commit does not have is among the first.
func Uncommitted(ctx context.Context, checkout string, untracked bool) ([]string, error) {
	which := "--untracked-files=no"
	if untracked {
		which = "--untracked-files=all"
	}
	out, err := sealed(ctx, checkout, "status", "--porcelain", "-z", which, "--ignore-submodules=none")
	if err != nil {
		return nil, err
	}
	return statusPaths(out), nil
}

// statusPaths is the paths in git status's porcelain output, written with -z.
func statusPaths(out []byte) []string {
	var paths []string
	fields := strings.Split(string(out), "\x00")
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		paths = append(paths, f[3:])
		// A rename or a copy is followed by the name it had.
		if f[0] == 'R' || f[0] == 'C' {
			i++
		}
	}
	return paths
}

// operations are the files git keeps in a checkout's git directory while an
// operation is part-way, with what each is.
var operations = []struct{ file, what string }{
	{"rebase-merge", "a rebase"},
	{"rebase-apply", "a rebase or a git am"},
	{"MERGE_HEAD", "a merge"},
	{"CHERRY_PICK_HEAD", "a cherry-pick"},
	{"REVERT_HEAD", "a revert"},
	{"sequencer", "a cherry-pick or a revert"},
	{"BISECT_LOG", "a bisect"},
	{"BISECT_START", "a bisect"},
}

// InProgress is the operation part-way in checkout, "a rebase" or "a merge"
// and so on, and "" where none is.
func InProgress(ctx context.Context, checkout string) (string, error) {
	dir, err := absoluteGitDir(ctx, checkout)
	if err != nil {
		return "", err
	}
	return partWay(dir)
}

func partWay(gitDir string) (string, error) {
	for _, op := range operations {
		_, err := os.Lstat(filepath.Join(gitDir, op.file))
		switch {
		case err == nil:
			return op.what, nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", sdlcerr.New(sdlcerr.RepositoryUnreadable,
				"git's own files in "+sdlcerr.QuotePath(gitDir)+" could not be read", err.Error()).WithCause(err)
		}
	}
	return "", nil
}

// absoluteGitDir is the git directory of the checkout at dir: its own, for a
// linked worktree.
func absoluteGitDir(ctx context.Context, dir string) (string, error) {
	out, err := sealed(ctx, dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	return filepath.FromSlash(firstLine(string(out))), nil
}

// ResolvesTo reports whether git, in checkout, reads name as the ref ref:
// "main" is refs/heads/main only while no tag or other ref of that name makes
// it read as another.
func ResolvesTo(ctx context.Context, checkout, name, ref string) (bool, error) {
	if err := notAnOption("a ref", name); err != nil {
		return false, err
	}
	out, err := sealed(ctx, checkout, "rev-parse", "--symbolic-full-name", "--verify", "--quiet", name)
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		return false, nil
	case err != nil:
		return false, err
	}
	return firstLine(string(out)) == ref, nil
}

// holder is a checkout that holds a branch: one with it checked out, or one on
// no branch while a rebase or a bisect of it is part-way.
type holder struct {
	path, gitDir string
	// detached is the second kind.
	detached bool
}

// CheckedOut is every checkout that holds branch: those with it checked out,
// and those on no branch while a rebase or a bisect of it is part-way. A
// checkout whose directory is gone holds nothing.
func CheckedOut(ctx context.Context, root, branch string) ([]string, error) {
	holding, err := holders(ctx, root, branch)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(holding))
	for i, h := range holding {
		paths[i] = h.path
	}
	return paths, nil
}

func holders(ctx context.Context, root, branch string) ([]holder, error) {
	if err := notAnOption("a branch", branch); err != nil {
		return nil, err
	}
	out, err := sealed(ctx, root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	ref := "refs/heads/" + branch
	var holding []holder
	for _, w := range parseWorktrees(out) {
		if w.bare || w.prunable {
			continue
		}
		if _, err := os.Stat(w.path); err != nil {
			continue
		}
		switch w.branch {
		case ref:
			dir, err := absoluteGitDir(ctx, w.path)
			if err != nil {
				return nil, err
			}
			holding = append(holding, holder{path: w.path, gitDir: dir})
		case "":
			dir, err := absoluteGitDir(ctx, w.path)
			if err != nil {
				return nil, err
			}
			if rebasingOrBisecting(dir, branch) {
				holding = append(holding, holder{path: w.path, gitDir: dir, detached: true})
			}
		}
	}
	return holding, nil
}

// rebasingOrBisecting reports whether the git directory gitDir is part-way
// through a rebase or a bisect that started from branch.
func rebasingOrBisecting(gitDir, branch string) bool {
	for _, f := range []string{"rebase-merge/head-name", "rebase-apply/head-name", "BISECT_START"} {
		raw, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		if name := strings.TrimSpace(string(raw)); name == "refs/heads/"+branch || name == branch {
			return true
		}
	}
	return false
}

// Fingerprint is the change from base's tree to tip's, as one hash: the same
// change made on another base has the same fingerprint, and a change that
// differs in anything a person would see in the code has another. It is read
// from git's objects alone, with the paths in leaveOut, slash paths from the
// root, left out, and everything under a directory among them.
//
// Everything that shapes the patch is fixed on the command line, so that no
// setting of the repository's can move it or make two changes read as one.
// Each hunk header is cut down to its marker, and each index line left out,
// because both say where the change sits, and a rebase moves it.
func Fingerprint(ctx context.Context, root, base, tip string, leaveOut ...string) (string, error) {
	if err := fullHashes(base, tip); err != nil {
		return "", err
	}
	// Before git 2.42, a diff driver's own algorithm, set in the repository's
	// configuration and chosen by its attributes, outranked the one on the
	// command line.
	settings := []string{"core.quotePath=true", "diff.suppressBlankEmpty=false", "diff.relative=false"}
	drivers, err := sealed(ctx, root, "config", "-z", "--get-regexp", `^diff\..+\.algorithm$`)
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit) && exit.ExitCode() == 1:
	case err != nil:
		return "", err
	}
	for _, entry := range strings.Split(string(drivers), "\x00") {
		if key, _, _ := strings.Cut(entry, "\n"); key != "" {
			settings = append(settings, key+"=myers")
		}
	}
	args := []string{"diff-tree", "-r", "-p", "--text", "--binary", "--full-index",
		"--no-color", "--no-ext-diff", "--no-textconv", "--no-renames",
		"-U3", "--inter-hunk-context=0", "--diff-algorithm=myers", "--indent-heuristic",
		"--src-prefix=a/", "--dst-prefix=b/", "--submodule=short", "--ignore-submodules=none",
		base + "^{commit}", tip + "^{commit}", "--"}
	for _, p := range leaveOut {
		args = append(args, ":(top,exclude,literal)"+p)
	}

	pr, pw := io.Pipe()
	ran := make(chan error, 1)
	go func() {
		_, err := sealedCall(ctx, root, settings, nil, pw, args...)
		_ = pw.CloseWithError(err)
		ran <- err
	}()
	sum := sha256.New()
	readErr := hashPatch(bufio.NewReaderSize(pr, 64<<10), sum)
	_ = pr.CloseWithError(readErr)
	if err := <-ran; err != nil {
		return "", err
	}
	if readErr != nil {
		return "", sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the change could not be read", readErr.Error()).WithCause(readErr)
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// hashPatch writes the patch r reads to w, with each hunk header cut down to
// "@@" and each index line left out, a line at a time however long the line.
func hashPatch(r *bufio.Reader, w io.Writer) error {
	for {
		head, err := r.Peek(6)
		if len(head) == 0 {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		keep := true
		switch {
		case bytes.HasPrefix(head, []byte("@@")):
			_, _ = w.Write([]byte("@@\n"))
			keep = false
		case bytes.HasPrefix(head, []byte("index ")):
			keep = false
		}
		for {
			chunk, err := r.ReadSlice('\n')
			if keep {
				_, _ = w.Write(chunk)
			}
			if errors.Is(err, bufio.ErrBufferFull) {
				continue
			}
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			break
		}
	}
}
