package gitx

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// These are the binary's only git commands that change a repository. Each of
// them acts on what this call made and on nothing else: a branch or a worktree
// that was already there may hold somebody's work, and is never reused, reset
// or removed (THREATS T6).

// The causes AddWorktree is refused for, which errors.Is finds in what it
// returns, so that the caller can say what to do about each.
var (
	// ErrInTheWay is a branch, a worktree or a directory with something in it
	// already where the new one goes.
	ErrInTheWay = errors.New("something is already where the worktree goes")
	// ErrNotADirectory is a link, or a file, where a directory belongs.
	ErrNotADirectory = errors.New("a link or a file is where a directory belongs")
	// ErrBranchName is a branch name git will not take.
	ErrBranchName = errors.New("git will not take the name as a branch")
	// ErrNoTrunk is a trunk this repository has no branch with a commit for.
	ErrNoTrunk = errors.New("trunk is not a branch with a commit here")
)

// AddWorktree makes a worktree at dir, which must be absolute, on a new branch
// cut from the commit refs/heads/<trunk> is on, in the repository whose
// checkout is root. The branch is created before the worktree, in one step that
// fails where it is already there, so that of two starts racing for one story
// the second makes nothing. What this call made is taken away again if the
// worktree cannot be made, and anything that could not be is named.
//
// git runs the repository's post-checkout hook as it checks the worktree out,
// and nothing here undoes what that hook did.
func AddWorktree(ctx context.Context, root, dir, branch, trunk string) error {
	for _, name := range []string{branch, trunk} {
		if name == "" || strings.HasPrefix(name, "-") {
			return notMade(strconv.Quote(name)+" cannot name a branch",
				"no branch is empty or starts with a dash, and git would read one that does as an option", ErrBranchName)
		}
	}
	if !filepath.IsAbs(dir) {
		return notMade(sdlcerr.QuotePath(dir)+" is not an absolute path",
			"the worktree is made where it is named, and a relative path names somewhere different from each directory", nil)
	}
	if err := refuseLinks(root, dir); err != nil {
		return err
	}
	if _, err := git(ctx, root, nil, "check-ref-format", "--branch", branch); err != nil {
		return notMade(strconv.Quote(branch)+" is not a name git takes for a branch",
			failure(err, "a story's branch is named for it, and git refuses this name"), ErrBranchName)
	}
	out, err := git(ctx, root, nil, "rev-parse", "--verify", "--quiet", "refs/heads/"+trunk+"^{commit}")
	if err != nil {
		return notMade("trunk, "+strconv.Quote(trunk)+", is not a branch with a commit in this repository",
			"a story's branch is cut from the commit trunk's branch is on, and there is none to cut it from", ErrNoTrunk)
	}
	hash := firstLine(string(out))

	listed, err := listWorktrees(ctx, root)
	if err != nil {
		return err
	}
	for _, w := range listed {
		if samePath(w.path, dir) {
			return notMade("a worktree at "+sdlcerr.QuotePath(dir)+" already exists",
				"git lists it as a checkout of this repository, and a checkout that is already there may hold "+
					"somebody's work, so start never reuses or removes it", ErrInTheWay)
		}
	}
	if _, err := git(ctx, root, nil, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		return notMade("the branch "+strconv.Quote(branch)+" already exists",
			"a story is begun on a new branch cut from trunk, and a branch that is already there may hold "+
				"somebody's work, so start never reuses, resets or removes it", ErrInTheWay)
	}
	existed := false
	if entries, err := os.ReadDir(dir); err == nil {
		existed = true
		if len(entries) > 0 {
			return notMade(sdlcerr.QuotePath(dir)+" already exists and has something in it",
				"start makes the worktree in a directory of its own, and never writes over or removes "+
					"what it did not make", ErrInTheWay)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return notMade(sdlcerr.QuotePath(dir)+" could not be read", err.Error(), ErrInTheWay)
	}

	// Created only where it is not there: a racing start that got here first
	// has made it, and this one loses with nothing made.
	message := "sdlc start: " + branch + " cut from " + trunk
	if _, err := git(ctx, root, nil, "update-ref", "-m", message, "refs/heads/"+branch, hash, ""); err != nil {
		return notMade("the branch "+strconv.Quote(branch)+" already exists",
			failure(err, "another start made it a moment ago"), ErrInTheWay)
	}
	// The short name, and no -b: git checks the branch out rather than a tag
	// that shares its name, and sets up no tracking for a branch cut from a hash.
	if _, err := git(ctx, root, nil, "worktree", "add", "--quiet", "--", dir, branch); err != nil {
		left := undoAdd(ctx, root, dir, branch, hash, existed)
		why := failure(err, "git could not check the branch out")
		if len(left) > 0 {
			why += "; and what start made could not all be taken away again: " + strings.Join(left, "; ")
		}
		return notMade("the worktree at "+sdlcerr.QuotePath(dir)+" could not be made", why, nil)
	}
	return nil
}

// undoAdd takes away what AddWorktree made before git failed to make the
// worktree, and names whatever is left.
func undoAdd(ctx context.Context, root, dir, branch, hash string, existed bool) []string {
	var left []string
	if listed, err := listWorktrees(ctx, root); err == nil {
		for _, w := range listed {
			if samePath(w.path, dir) {
				if _, err := git(ctx, root, nil, "worktree", "remove", "--force", "--", dir); err != nil {
					left = append(left, "the worktree "+sdlcerr.QuotePath(dir)+" is still registered with git")
				}
			}
		}
	}
	switch _, err := os.Lstat(dir); {
	case err == nil && !existed:
		if err := os.RemoveAll(dir); err != nil {
			left = append(left, sdlcerr.QuotePath(dir)+" is still there")
		}
	case errors.Is(err, fs.ErrNotExist) && existed:
		// It was an empty directory before, and it is one again.
		_ = os.Mkdir(dir, 0o755)
	}
	// Deleted only while it still points where this call put it.
	if _, err := git(ctx, root, nil, "update-ref", "-d", "refs/heads/"+branch, hash); err != nil {
		left = append(left, "the branch "+strconv.Quote(branch)+" is still there")
	}
	return left
}

// RemoveWorktree takes away the worktree at dir, whatever it holds, and then the
// branch it has checked out, which must be branch. It is start's rollback, of a
// worktree start has just made. The main checkout, a directory git does not
// list, and a worktree on another branch are refused, so that a wrong path
// cannot become a deletion.
func RemoveWorktree(ctx context.Context, root, dir, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") {
		return notRemoved(strconv.Quote(branch)+" cannot name a branch",
			"no branch is empty or starts with a dash, and git would read one that does as an option")
	}
	listed, err := listWorktrees(ctx, root)
	if err != nil {
		return err
	}
	var found *listedWorktree
	for i := range listed {
		if samePath(listed[i].path, dir) {
			found = &listed[i]
			break
		}
	}
	switch {
	case found == nil:
		return notRemoved(sdlcerr.QuotePath(dir)+" is not a worktree git lists",
			"only a worktree of this repository is taken away")
	case found == &listed[0]:
		return notRemoved(sdlcerr.QuotePath(dir)+" is the main checkout",
			"the main checkout is never taken away")
	case found.branch != "refs/heads/"+branch:
		return notRemoved(sdlcerr.QuotePath(dir)+" does not have "+strconv.Quote(branch)+" checked out",
			"a worktree is taken away only with the branch it was made on")
	}
	// One --force, not two: the worktrees start makes are never locked.
	if _, err := git(ctx, root, nil, "worktree", "remove", "--force", "--", dir); err != nil {
		return notRemoved("the worktree at "+sdlcerr.QuotePath(dir)+" could not be taken away",
			failure(err, "git could not remove it")+"; it and the branch "+strconv.Quote(branch)+
				" are still there, and a file something has open in it keeps it there on Windows")
	}
	if _, err := git(ctx, root, nil, "update-ref", "-d", "refs/heads/"+branch, found.head); err != nil {
		return notRemoved("the branch "+strconv.Quote(branch)+" could not be deleted",
			failure(err, "git could not delete it")+"; the worktree was taken away, and the branch is still there")
	}
	return nil
}

// Ahead is how many commits branch has that trunk does not: the work a story's
// branch holds, which nobody should remove without bringing it to trunk first.
func Ahead(ctx context.Context, root, branch, trunk string) (int, error) {
	if err := branchNames(branch, trunk); err != nil {
		return 0, err
	}
	return count(ctx, root, "refs/heads/"+trunk+"..refs/heads/"+branch)
}

// HeadAhead is how many commits the checkout at dir has on its HEAD that trunk
// does not, whichever branch it has checked out, or none. A worktree on no
// branch can hold commits no branch has, and removing it loses them: doctor
// had called such a worktree empty, counting only the branch start made.
func HeadAhead(ctx context.Context, dir, trunk string) (int, error) {
	if err := branchNames(trunk); err != nil {
		return 0, err
	}
	return count(ctx, dir, "refs/heads/"+trunk+"..HEAD")
}

// branchNames refuses a name git would read as an option, or no name at all.
func branchNames(names ...string) error {
	for _, name := range names {
		if name == "" || strings.HasPrefix(name, "-") {
			return sdlcerr.New(sdlcerr.RepositoryUnreadable,
				strconv.Quote(name)+" cannot name a branch",
				"no branch is empty or starts with a dash, and git would read one that does as an option")
		}
	}
	return nil
}

// count is how many commits the range names, in the repository root is in.
func count(ctx context.Context, root, commits string) (int, error) {
	out, err := git(ctx, root, nil, "rev-list", "--count", commits, "--")
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

// Holds reports whether the commit rev names has a file at path, a slash path
// from the root of the repository: what a new worktree cut from it has.
func Holds(ctx context.Context, root, rev, path string) (bool, error) {
	if _, err := Resolve(ctx, root, rev); err != nil {
		return false, err
	}
	_, err := git(ctx, root, nil, "cat-file", "-e", rev+"^{commit}:"+path)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, nil
	case errors.As(err, &exit):
		return false, nil
	}
	return false, err
}

// MinimumVersion is the oldest git the worktree commands work with: `git
// worktree remove`, the newest command gitx runs, came in 2.17.
var MinimumVersion = [2]int{2, 17}

// gitVersion is the major and minor number in what `git version` prints: "git
// version 2.39.5 (Apple Git-154)", or "git version 2.45.1.windows.1".
var gitVersion = regexp.MustCompile(`(\d+)\.(\d+)`)

// Version is the major and minor version of the git on PATH.
func Version(ctx context.Context) (major, minor int, err error) {
	out, err := git(ctx, "", nil, "version")
	if err != nil {
		return 0, 0, err
	}
	m := gitVersion.FindStringSubmatch(string(out))
	if m == nil {
		return 0, 0, sdlcerr.New(sdlcerr.RepositoryUnreadable,
			"git did not say which version it is",
			"git said: "+firstLine(string(out)))
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return major, minor, nil
}

// A Worktree is one checkout git lists for a repository.
type Worktree struct {
	// Path is where it is, as git has it.
	Path string
	// Branch is the branch it has checked out, without refs/heads/, and ""
	// where it has none.
	Branch string
	// Gone is a checkout whose directory git can no longer find.
	Gone bool
	// Locked is a checkout somebody locked with git worktree lock, which is
	// never taken away without them.
	Locked bool
}

// Worktrees is every checkout git lists for the repository, the main checkout
// first.
func Worktrees(ctx context.Context, root string) ([]Worktree, error) {
	listed, err := listWorktrees(ctx, root)
	if err != nil {
		return nil, err
	}
	out := make([]Worktree, len(listed))
	for i, w := range listed {
		branch, _ := strings.CutPrefix(w.branch, "refs/heads/")
		out[i] = Worktree{Path: w.path, Branch: branch, Gone: w.prunable, Locked: w.locked}
	}
	return out, nil
}

// listedWorktree is one entry of `git worktree list --porcelain`.
type listedWorktree struct {
	path, head, branch     string
	prunable, locked, bare bool
}

func listWorktrees(ctx context.Context, root string) ([]listedWorktree, error) {
	out, err := git(ctx, root, nil, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

// parseWorktrees reads what `git worktree list --porcelain` printed.
func parseWorktrees(out []byte) []listedWorktree {
	var listed []listedWorktree
	for _, line := range strings.Split(string(out), "\n") {
		key, value, _ := strings.Cut(strings.TrimRight(line, "\r"), " ")
		switch {
		case key == "worktree":
			listed = append(listed, listedWorktree{path: filepath.FromSlash(value)})
		case len(listed) == 0:
		case key == "HEAD":
			listed[len(listed)-1].head = value
		case key == "branch":
			listed[len(listed)-1].branch = value
		case key == "prunable":
			listed[len(listed)-1].prunable = true
		case key == "locked":
			listed[len(listed)-1].locked = true
		case key == "bare":
			listed[len(listed)-1].bare = true
		}
	}
	return listed
}

// refuseLinks refuses a link, or a file, at dir or at a directory between root
// and dir. git would make the worktree through a link wherever it led, and
// nothing start writes goes through one.
func refuseLinks(root, dir string) error {
	steps := []string{dir, filepath.Dir(dir)}
	if within(root, dir) {
		steps = nil
		for p := dir; !samePath(p, root) && filepath.Dir(p) != p; p = filepath.Dir(p) {
			steps = append(steps, p)
		}
	}
	for i := len(steps) - 1; i >= 0; i-- {
		info, err := os.Lstat(steps[i])
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return notMade(sdlcerr.QuotePath(steps[i])+" could not be read", err.Error(), ErrNotADirectory)
		case info.Mode()&fs.ModeSymlink != 0:
			return notMade(sdlcerr.QuotePath(steps[i])+" is a link",
				"the worktree is made in a directory of the main checkout itself, and never through a link to "+
					"somewhere else", ErrNotADirectory)
		case !info.IsDir():
			return notMade(sdlcerr.QuotePath(steps[i])+" is a file, where a directory belongs",
				"the worktree is made in a directory of the main checkout, and start never writes over "+
					"what it did not make", ErrNotADirectory)
		}
	}
	return nil
}

// within reports whether p is root or inside it.
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// samePath reports whether git's path a names the directory b: the same
// directory where both are there, and otherwise the same path once the links
// above it are read.
func samePath(a, b string) bool {
	x, errA := os.Stat(a)
	y, errB := os.Stat(b)
	if errA == nil && errB == nil {
		return os.SameFile(x, y)
	}
	return settled(a) == settled(b)
}

// settled is p with the links in the directory above it read, as git writes
// a path it was given.
func settled(p string) string {
	p = filepath.Clean(p)
	if parent, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(parent, filepath.Base(p))
	}
	return p
}

// notMade is AddWorktree's refusal, with the cause errors.Is finds, where there
// is one.
func notMade(what, why string, cause error) error {
	e := sdlcerr.New(sdlcerr.WorktreeNotMade, "the story's worktree could not be made: "+what, why)
	if cause != nil {
		return e.WithCause(cause)
	}
	return e
}

// notRemoved is RemoveWorktree's refusal.
func notRemoved(what, why string) error {
	return sdlcerr.New(sdlcerr.WorktreeNotMade, "the story's worktree could not be taken away: "+what, why)
}

// failure is what git said when err is its refusal, or otherwise.
func failure(err error, otherwise string) string {
	var e *sdlcerr.Error
	if errors.As(err, &e) && e.Why != "" {
		return e.Why
	}
	return otherwise
}
