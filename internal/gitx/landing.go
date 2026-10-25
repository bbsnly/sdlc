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
	"unicode/utf8"

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
	"GIT_EDITOR", "GIT_SEQUENCE_EDITOR",
}

// privateDir stands, in a setting handed to sealedCall, for the call's private
// directory, which exists only while the call runs.
const privateDir = "{sdlc-private}"

// DriverKind is one of the two kinds of driver whose setting the landing
// overrides on git's command line, by the name of its section.
type DriverKind string

const (
	// MergeDrivers are overridden while a story's commits are put onto trunk.
	MergeDrivers DriverKind = "merge"
	// DiffDrivers are overridden wherever a change is measured.
	DiffDrivers DriverKind = "diff"
)

// variable is the setting of a driver of the kind that the landing overrides.
func (k DriverKind) variable() string {
	if k == MergeDrivers {
		return "driver"
	}
	return "algorithm"
}

// Keys is the key of every driver of the kind that the configuration holds, as
// dir reads it and as git spells it, the one with an empty name among them,
// each once and in the order git first lists it.
//
// Git is asked for every key and the choice is made here. It was once asked
// for the keys that match a pattern, which it matches in the locale of the
// process: in a UTF-8 locale a name with a byte that is not UTF-8 was left out,
// so its driver was neither refused nor overridden, and ran. Only names are
// asked for, so no value of the configuration is read; git has had --name-only
// since 2.6.0 (its release notes say so), which is older than the oldest git
// this package works with. A listing that fails is an error, never "none".
func (k DriverKind) Keys(ctx context.Context, dir string) ([]string, error) {
	out, err := sealed(ctx, dir, "config", "-z", "--list", "--name-only")
	if err != nil {
		return nil, err
	}
	prefix, suffix := string(k)+".", "."+k.variable()
	var keys []string
	seen := map[string]bool{}
	for _, key := range nulSeparated(out) {
		if len(key) >= len(prefix)+len(suffix) && strings.HasPrefix(key, prefix) && strings.HasSuffix(key, suffix) && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys, nil
}

// PrivateDirText is the text a driver's name cannot hold, for the words of a
// refusal that names it.
const PrivateDirText = privateDir

// Unoverridable is those of keys that cannot be handed to git on its command
// line as they are, in the order given. git -c ends a key at its first "=",
// and sealedCall writes the private directory wherever its placeholder stands,
// so a setting for a name that holds either lands on another key and the
// driver's own stays. A byte that is not UTF-8 is not counted on to reach git
// unchanged where a command line is converted on its way, as Windows converts
// one, and is held to the rule on every system so that a repository is refused
// alike on all of them.
func Unoverridable(keys []string) []string {
	var bad []string
	for _, key := range keys {
		if strings.Contains(key, "=") || strings.Contains(key, privateDir) || !utf8.ValidString(key) {
			bad = append(bad, key)
		}
	}
	return bad
}

// DriverNamesError is the cause of a refusal made for the drivers of one kind
// whose keys are Unoverridable. It carries what the refusal is about to the
// caller that words it, which nothing can read out of prose.
type DriverNamesError struct {
	Kind DriverKind
	Keys []string
}

// Error names the kind and how many keys, and no key: a key is repository
// text, and this may be logged.
func (e *DriverNamesError) Error() string {
	return strconv.Itoa(len(e.Keys)) + " " + string(e.Kind) + " driver keys cannot be handed to git's command line"
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
// read as git reads it: it can only hide ancestry, and hidden ancestry refuses
// wherever ancestry is what was asked. The one question it answers otherwise is
// HasCommit's: a commit it hides is a commit git does not have, and whoever
// passes such a commit over has to refuse on something else the file hides
// with it.
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
		full = append(full, "-c", strings.ReplaceAll(kv, privateDir, filepath.ToSlash(private)))
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
	for i := range len(s) {
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
	if _, err := sealed(ctx, root, "check-ref-format", headsPrefix+branch); err != nil {
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
	out, err := sealed(ctx, root, "show-ref", argVerify, "--hash", headsPrefix+branch)
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
			msgGitSaid+strconv.Quote(tip))
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

// FirstAbove is the lowest commit on tip's own line, its first parents, that
// base does not have in its history, and "" where there is none: the first
// commit of a branch above where it left another.
func FirstAbove(ctx context.Context, root, base, tip string) (string, error) {
	if err := fullHashes(base, tip); err != nil {
		return "", err
	}
	out, err := sealed(ctx, root, "rev-list", "--first-parent", "--reverse", base+".."+tip, "--")
	return firstLine(string(out)), err
}

// HasCommit reports whether git has the commit among its objects here. One it
// does not have is a no and not a failure: git prunes a commit no branch
// reaches any longer, and a caller that asked IsAncestor instead could not
// tell that from a call that failed, which answers nothing.
func HasCommit(ctx context.Context, root, commit string) (bool, error) {
	if err := fullHashes(commit); err != nil {
		return false, err
	}
	out, err := sealedCall(ctx, root, nil, []byte(commit+"\n"), nil, "cat-file", "--batch-check")
	said := firstLine(string(out))
	if err != nil || said == commit+" missing" {
		return false, err
	}
	if !strings.HasPrefix(said, commit+" commit ") {
		return false, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"git did not say whether it has commit "+commit[:12], msgGitSaid+strconv.Quote(said))
	}
	return true, nil
}

// Written is what the commits of base..tip say of themselves, oldest first:
// for each, its hash, its author's name, its author's email and its whole
// message, every field ended by a NUL, which none of them can hold. A range
// with no commit is the empty string. The caller takes it apart: what git
// wrote is returned as it wrote it, so that nothing here decides what a
// message is.
//
// The author is the commit's own (%an, %ae) and never the one a .mailmap in
// the repository says the person is, and the message is %B, the bytes after
// the headers with nothing taken off either end: an approval holds a story to
// these, and a name nobody committed as, or a final newline git added, would
// make two different commits read the same or the same commit read as two. A
// signature is not checked, because checking one runs a program the
// repository's configuration names. The message is asked for as UTF-8, so that
// a commit made again under another i18n.commitEncoding is compared as text.
func Written(ctx context.Context, root, base, tip string) (string, error) {
	if err := fullHashes(base, tip); err != nil {
		return "", err
	}
	out, err := sealed(ctx, root, "log", "-z", "--reverse", "--topo-order", "--no-show-signature",
		"--encoding=UTF-8", "--format=%H%x00%an%x00%ae%x00%B", base+".."+tip, "--")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ChangedUnder is the paths that differ between base and tip at dir or under
// it, a slash path from the root, compared without regard to case: a trunk
// landed from Linux is checked out on macOS and Windows as well, where
// .SDLC/notes.json is .sdlc/notes.json. The names are read whole, whatever
// they hold.
func ChangedUnder(ctx context.Context, root, base, tip, dir string) ([]string, error) {
	if err := notAnOption(msgAPath, dir); err != nil {
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
	out, err := sealed(ctx, root, "diff-tree", "-r", "-z", argNoRenames, "--name-only",
		argIgnoreSubmodNone, base, tip, "--")
	if err != nil {
		return nil, err
	}
	return nulSeparated(out), nil
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
	if err := notAnOption(msgAPath, path); err != nil {
		return "", err
	}
	out, err := sealed(ctx, root, "ls-tree", "-z", "--full-tree", commit, "--", path)
	if err != nil {
		return "", err
	}
	for entry := range strings.SplitSeq(string(out), "\x00") {
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
	if err := notAnOption(msgAPath, path); err != nil {
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
		var err error
		if body, rest, err = batchObject(rest, want, i == 1, path, commit); err != nil {
			return nil, err
		}
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
	out, err := sealed(ctx, checkout, "status", "--porcelain", "-z", which, argIgnoreSubmodNone)
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
				"git's own files in "+sdlcerr.QuotePath(gitDir)+msgCouldNotBeRead, err.Error()).WithCause(err)
		}
	}
	return "", nil
}

// absoluteGitDir is the git directory of the checkout at dir: its own, for a
// linked worktree.
func absoluteGitDir(ctx context.Context, dir string) (string, error) {
	out, err := sealed(ctx, dir, argRevParse, "--absolute-git-dir")
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
	out, err := sealed(ctx, checkout, argRevParse, "--symbolic-full-name", argVerify, argQuiet, name)
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
	var holding []holder
	for _, w := range parseWorktrees(out) {
		if w.bare || w.prunable {
			continue
		}
		if _, err := os.Stat(w.path); err != nil {
			continue
		}
		h, held, err := holderOf(ctx, w, branch)
		if err != nil {
			return nil, err
		}
		if held {
			holding = append(holding, h)
		}
	}
	return holding, nil
}

// holderOf is the worktree w as a holder of branch, and false where it does
// not hold it.
func holderOf(ctx context.Context, w listedWorktree, branch string) (holder, bool, error) {
	switch w.branch {
	case headsPrefix + branch:
		dir, err := absoluteGitDir(ctx, w.path)
		if err != nil {
			return holder{}, false, err
		}
		return holder{path: w.path, gitDir: dir}, true, nil
	case "":
		dir, err := absoluteGitDir(ctx, w.path)
		if err != nil {
			return holder{}, false, err
		}
		if rebasingOrBisecting(dir, branch) {
			return holder{path: w.path, gitDir: dir, detached: true}, true, nil
		}
	}
	return holder{}, false, nil
}

// rebasingOrBisecting reports whether the git directory gitDir is part-way
// through a rebase or a bisect that started from branch.
func rebasingOrBisecting(gitDir, branch string) bool {
	for _, f := range []string{"rebase-merge/head-name", "rebase-apply/head-name", "BISECT_START"} {
		raw, err := os.ReadFile(filepath.Join(gitDir, filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		if name := strings.TrimSpace(string(raw)); name == headsPrefix+branch || name == branch {
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
	return fingerprint(ctx, root, "-U3", base, tip, leaveOut)
}

// LinesFingerprint is Fingerprint without the lines around a change: its own
// added and removed lines, file by file. Trunk changing a line beside the
// change moves Fingerprint and not this, and this alone cannot say where in a
// file a line sits. The landing asks it from both sides of its own replay,
// the story's change and trunk's, which together can.
func LinesFingerprint(ctx context.Context, root, base, tip string, leaveOut ...string) (string, error) {
	return fingerprint(ctx, root, "-U0", base, tip, leaveOut)
}

// fingerprint is the change from base to tip as one hash, with as many lines
// of context as the -U option unified asks for.
func fingerprint(ctx context.Context, root, unified, base, tip string, leaveOut []string) (string, error) {
	if err := fullHashes(base, tip); err != nil {
		return "", err
	}
	settings, err := patchSettings(ctx, root)
	if err != nil {
		return "", err
	}
	args := []string{"diff-tree", "-r", "-p", "--text", "--binary", argFullIndex,
		"--no-color", "--no-ext-diff", "--no-textconv", argNoRenames,
		unified, "--inter-hunk-context=0", "--diff-algorithm=myers", "--indent-heuristic",
		"--src-prefix=a/", "--dst-prefix=b/", "--submodule=short", argIgnoreSubmodNone,
		base + "^{commit}", tip + "^{commit}", "--"}
	for _, p := range leaveOut {
		args = append(args, ":(top,exclude,literal)"+p)
	}

	sum := sha256.New()
	if err := streamPatch(ctx, root, settings, args, func(r *bufio.Reader) error {
		return hashPatch(r, sum)
	}); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// patchSettings are the settings, handed to git as -c, that fix how a patch
// is written whatever the repository's configuration says: quoting, blank
// context lines, paths relative to the root, and the algorithm of every diff
// driver the configuration defines.
func patchSettings(ctx context.Context, root string) ([]string, error) {
	// Before git 2.42, a diff driver's own algorithm, set in the repository's
	// configuration and chosen by its attributes, outranked the one on the
	// command line.
	settings := []string{"core.quotePath=true", "diff.suppressBlankEmpty=false", "diff.relative=false"}
	drivers, err := DiffDrivers.Keys(ctx, root)
	if err != nil {
		return nil, err
	}
	for _, key := range drivers {
		settings = append(settings, key+"=myers")
	}
	return settings, nil
}

// streamPatch runs the sealed git call with args and hands what it prints to
// read as a stream, never all at once. The reader end is drained or closed on
// every path out, an early error of read's included, so that git's writer
// never blocks on a pipe nobody reads and its goroutine does not outlive the
// call.
//
// The error is git's own failure where there is one. A failure of read's own,
// a patch that cannot be one, wins over the broken pipe it causes in git.
func streamPatch(ctx context.Context, root string, settings, args []string, read func(*bufio.Reader) error) error {
	pr, pw := io.Pipe()
	ran := make(chan error, 1)
	go func() {
		_, err := sealedCall(ctx, root, settings, nil, pw, args...)
		_ = pw.CloseWithError(err)
		ran <- err
	}()
	readErr := read(bufio.NewReaderSize(pr, 64<<10))
	if readErr == nil {
		// What read left unread is read past: a closed pipe fails git's write.
		_, readErr = io.Copy(io.Discard, pr)
	}
	_ = pr.CloseWithError(readErr)
	gitErr := <-ran
	switch {
	case readErr != nil && (gitErr == nil || !errors.Is(readErr, gitErr)):
		if _, told := errors.AsType[*sdlcerr.Error](readErr); told {
			return readErr
		}
		return sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"the change could not be read", readErr.Error()).WithCause(readErr)
	case gitErr != nil:
		return gitErr
	}
	return nil
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
		if done, err := passLine(r, w, keepsLine(head, w)); done {
			return err
		}
	}
}

// keepsLine reports whether the line starting with head is part of what is
// hashed. A hunk header is not, and "@@" stands for it in w.
func keepsLine(head []byte, w io.Writer) bool {
	switch {
	case bytes.HasPrefix(head, []byte("@@")):
		_, _ = w.Write([]byte("@@\n"))
		return false
	case bytes.HasPrefix(head, []byte("index ")):
		return false
	}
	return true
}

// passLine reads one line from r, however long, and writes it to w where keep
// is set. It reports done with the error to return once r has ended or failed.
func passLine(r *bufio.Reader, w io.Writer, keep bool) (done bool, err error) {
	for {
		chunk, err := r.ReadSlice('\n')
		if keep {
			_, _ = w.Write(chunk)
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			return true, nil
		case err != nil:
			return true, err
		}
		return false, nil
	}
}

// batchObject cuts the next object of cat-file --batch's answer off rest: its
// body and what follows. An object of another type or size than wanted is a
// failure, and so is a missing one, which is told apart for the path when last
// is set, because the commit is read first.
func batchObject(rest []byte, want string, last bool, path, commit string) (body, after []byte, err error) {
	header, after, ok := bytes.Cut(rest, []byte("\n"))
	fields := strings.Fields(string(header))
	if !ok || len(fields) != 3 || !fullHash(fields[0]) {
		if last && bytes.HasSuffix(header, []byte(" missing")) {
			return nil, nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
				strconv.Quote(path)+" is not in commit "+commit[:12],
				"the commit has no file at that path").WithCause(fs.ErrNotExist)
		}
		return nil, nil, unreadableObject(path, commit, header)
	}
	size, err := strconv.Atoi(fields[2])
	if err != nil || fields[1] != want || size+1 > len(after) {
		return nil, nil, unreadableObject(path, commit, header)
	}
	return after[:size], after[size+1:], nil
}

func unreadableObject(path, commit string, header []byte) error {
	return sdlcerr.New(sdlcerr.RepositoryUnreadable,
		strconv.Quote(path)+" could not be read from commit "+commit[:12],
		msgGitSaid+strconv.Quote(string(header)))
}
