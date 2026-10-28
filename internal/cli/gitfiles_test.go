package cli

// The test helpers' reads of git state, taken from the files git keeps it in.
//
// swRev, swRefs, swWorktrees and the rest used to start a git process for each
// answer, about 16,000 of internal/cli's 66,000 per run, and a process start
// is most of what a test costs on a hosted runner. A ref is a file, HEAD is a
// file and a linked worktree is a directory with two files in it, so these read
// them. What these helpers return decides what tests assert, and a stale or
// wrong answer would turn a failure into a pass, so each function answers only
// the shapes it can read exactly (a full ref name, HEAD, a branch) and says it
// cannot for anything else, a reftable repository or a file it cannot make
// sense of. The caller then asks git, which is what it did before. The tests in
// gitfiles_agree_test.go hold every answer to git's.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// gfRepo is a repository found from a directory. In a linked worktree the two
// directories differ: HEAD and the part-way markers are the worktree's own,
// under gitDir, while refs, packed-refs, the reflogs and the list of worktrees
// are shared, under common.
type gfRepo struct {
	gitDir, common string
	bare           bool
}

// gfOpen finds the repository git would find from dir, or says it cannot be
// read from files: no repository above dir, a reftable one, a ref storage
// extension this does not know, or anything else unlike what git init makes.
func gfOpen(dir string) (gfRepo, bool) {
	d, err := filepath.Abs(dir)
	if err != nil {
		return gfRepo{}, false
	}
	for {
		// A directory inside a .git directory is not a place git discovers
		// the repository from the way it does from a working tree.
		if filepath.Base(d) == ".git" {
			return gfRepo{}, false
		}
		dot := filepath.Join(d, ".git")
		fi, err := os.Lstat(dot)
		switch {
		case err == nil && fi.IsDir():
			return gfLayout(dot, false)
		case err == nil && fi.Mode().IsRegular():
			raw, err := os.ReadFile(dot)
			if err != nil {
				return gfRepo{}, false
			}
			target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
			if !ok {
				return gfRepo{}, false
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(d, target)
			}
			return gfLayout(filepath.Clean(target), false)
		case err == nil:
			return gfRepo{}, false
		case !errors.Is(err, fs.ErrNotExist):
			return gfRepo{}, false
		}
		if gfIsGitDir(d) {
			return gfLayout(d, true)
		}
		parent := filepath.Dir(d)
		if parent == d {
			return gfRepo{}, false
		}
		d = parent
	}
}

// gfIsGitDir reports whether dir is itself a git directory, as a bare
// repository is.
func gfIsGitDir(dir string) bool {
	head, err := os.Stat(filepath.Join(dir, "HEAD"))
	if err != nil || !head.Mode().IsRegular() {
		return false
	}
	for _, sub := range []string{"objects", "refs"} {
		fi, err := os.Stat(filepath.Join(dir, sub))
		if err != nil || !fi.IsDir() {
			return false
		}
	}
	return true
}

// gfLayout completes a repository from its own git directory: the common one
// a linked worktree names, and the checks that the refs are files.
func gfLayout(gitDir string, bare bool) (gfRepo, bool) {
	// A linked worktree's own directory holds HEAD and little else; the
	// objects and refs are in the common one, checked below.
	if fi, err := os.Stat(filepath.Join(gitDir, "HEAD")); err != nil || !fi.Mode().IsRegular() {
		return gfRepo{}, false
	}
	r := gfRepo{gitDir: gitDir, common: gitDir, bare: bare}
	if raw, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil {
		common := strings.TrimSpace(string(raw))
		if !filepath.IsAbs(common) {
			common = filepath.Join(gitDir, common)
		}
		r.common = filepath.Clean(common)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return gfRepo{}, false
	}
	if !gfIsGitDir(r.common) {
		return gfRepo{}, false
	}
	if _, err := os.Lstat(filepath.Join(r.common, "reftable")); err == nil || !errors.Is(err, fs.ErrNotExist) {
		return gfRepo{}, false
	}
	config, err := os.ReadFile(filepath.Join(r.common, "config"))
	if err != nil {
		return gfRepo{}, false
	}
	lower := strings.ToLower(string(config))
	if strings.Contains(lower, "refstorage") {
		return gfRepo{}, false
	}
	for line := range strings.Lines(lower) {
		if key, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok &&
			strings.TrimSpace(key) == "bare" && strings.TrimSpace(value) != "false" {
			r.bare = true
		}
	}
	return r, true
}

// gfSafeName reports whether name is one this reads as a path: letters,
// digits and a few marks, no empty or dotted segment and no .lock, so that
// no name git would refuse, or would treat specially, is looked up as a file.
func gfSafeName(name string) bool {
	if name == "" || len(name) > 200 || strings.HasSuffix(name, ".") {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '/' || c == '-' || c == '_' || c == '+':
		default:
			return false
		}
	}
	for seg := range strings.SplitSeq(name, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") || strings.HasSuffix(seg, ".lock") {
			return false
		}
	}
	return true
}

// gfFullRef reports whether name is a full ref name kept under the shared
// refs directory. The refs a worktree keeps for itself are left to git.
func gfFullRef(name string) bool {
	if !strings.HasPrefix(name, "refs/") || !gfSafeName(name) {
		return false
	}
	for _, own := range []string{"refs/bisect/", "refs/worktree/", "refs/rewritten/"} {
		if strings.HasPrefix(name, own) {
			return false
		}
	}
	return true
}

// gfHex reports whether s is an object id as git writes one.
func gfHex(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// packed is the packed-refs file as name to object id, and says whether it
// could be read. A repository that has never packed has none, which is a
// readable empty answer.
func (r gfRepo) packed() (map[string]string, bool) {
	raw, err := os.ReadFile(filepath.Join(r.common, "packed-refs"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, true
	}
	if err != nil {
		return nil, false
	}
	refs := map[string]string{}
	for line := range strings.Lines(string(raw)) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == '^' {
			continue
		}
		id, name, ok := strings.Cut(line, " ")
		if !ok || !gfHex(id) || !gfFullRef(name) {
			return nil, false
		}
		refs[name] = id
	}
	return refs, true
}

// resolve is the object id the full ref name, or HEAD, names: a loose file
// first, which wins over the packed one, then packed-refs, with a symbolic ref
// followed. found is false for a ref that is not there (an unborn branch);
// ok is false for anything this cannot read.
func (r gfRepo) resolve(name string, depth int) (id string, found, ok bool) {
	if depth > 5 {
		return "", false, false
	}
	path := filepath.Join(r.gitDir, "HEAD")
	if name != "HEAD" {
		if !gfFullRef(name) {
			return "", false, false
		}
		path = filepath.Join(r.common, filepath.FromSlash(name))
	}
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		s := strings.TrimSpace(string(raw))
		if target, sym := strings.CutPrefix(s, "ref: "); sym {
			return r.resolve(strings.TrimSpace(target), depth+1)
		}
		if !gfHex(s) {
			return "", false, false
		}
		return s, true, true
	case errors.Is(err, fs.ErrNotExist) && name != "HEAD":
		packed, ok := r.packed()
		if !ok {
			return "", false, false
		}
		id, found = packed[name]
		return id, found, true
	}
	return "", false, false
}

// exists reports whether a ref, a file of the repository or a packed ref is
// there under name, for the unambiguous short names symbolic-ref --short makes.
func (r gfRepo) exists(name string) (found, ok bool) {
	if !gfSafeName(name) {
		return false, false
	}
	for _, dir := range []string{r.common, r.gitDir} {
		if _, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name))); err == nil {
			return true, true
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, false
		}
	}
	if gfFullRef(name) {
		packed, ok := r.packed()
		_, found := packed[name]
		return found, ok
	}
	return false, true
}

// gfRev is the commit rev names, for HEAD and for a full branch name, and
// false for any other expression: HEAD~1, a hash, a tag, a short name, an
// unborn branch. It does not open the object, so a ref to a missing or
// non-commit object reads as it is written; git never writes one to a branch.
func gfRev(dir, rev string) (string, bool) {
	if rev != "HEAD" && (!strings.HasPrefix(rev, "refs/heads/") || !gfFullRef(rev)) {
		return "", false
	}
	r, ok := gfOpen(dir)
	if !ok {
		return "", false
	}
	id, found, ok := r.resolve(rev, 0)
	return id, found && ok
}

// gfHasBranch reports whether refs/heads/<branch> exists, and whether that
// could be read.
func gfHasBranch(dir, branch string) (has, ok bool) {
	name := "refs/heads/" + branch
	if !gfFullRef(name) {
		return false, false
	}
	r, ok := gfOpen(dir)
	if !ok {
		return false, false
	}
	// A branch that is a directory of other branches reads as an error here
	// and is left to git.
	_, found, ok := r.resolve(name, 0)
	return found, ok
}

// gfRefs is every branch with its commit, as for-each-ref prints them with
// "%(refname) %(objectname)" over refs/heads: sorted by name, a loose ref over
// a packed one. A symbolic ref among them, or a name it does not read, is left
// to git.
func gfRefs(dir string) (string, bool) {
	r, ok := gfOpen(dir)
	if !ok {
		return "", false
	}
	refs, ok := r.packed()
	if !ok {
		return "", false
	}
	heads := map[string]string{}
	for name, id := range refs {
		if strings.HasPrefix(name, "refs/heads/") {
			heads[name] = id
		}
	}
	var walk func(rel string) bool
	walk = func(rel string) bool {
		entries, err := os.ReadDir(filepath.Join(r.common, filepath.FromSlash(rel)))
		if err != nil {
			return false
		}
		for _, e := range entries {
			name := rel + "/" + e.Name()
			switch {
			case e.IsDir():
				if !gfSafeName(name) || !walk(name) {
					return false
				}
			case e.Type().IsRegular():
				if strings.HasSuffix(name, ".lock") {
					continue
				}
				raw, err := os.ReadFile(filepath.Join(r.common, filepath.FromSlash(name)))
				id := strings.TrimSpace(string(raw))
				if err != nil || !gfFullRef(name) || !gfHex(id) {
					return false
				}
				heads[name] = id
			default:
				return false
			}
		}
		return true
	}
	if !walk("refs/heads") {
		return "", false
	}
	names := make([]string, 0, len(heads))
	for name := range heads {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := make([]string, len(names))
	for i, name := range names {
		lines[i] = name + " " + heads[name]
	}
	return strings.Join(lines, "\n"), true
}

// gfWorktrees is every working tree git lists for the repository: the main
// one, as git spells it with symbolic links resolved, then the linked ones by
// the name of their directory under worktrees. Order apart, it is the list of
// `git worktree list --porcelain`. A bare repository, a linked worktree whose
// record is unreadable, or a main checkout not in a .git directory are left
// to git.
func gfWorktrees(dir string) ([]string, bool) {
	r, ok := gfOpen(dir)
	if !ok || r.bare || filepath.Base(r.common) != ".git" {
		return nil, false
	}
	resolved, err := filepath.EvalSymlinks(r.common)
	if err != nil {
		return nil, false
	}
	out := []string{filepath.Dir(resolved)}
	entries, err := os.ReadDir(filepath.Join(r.common, "worktrees"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, false
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join(r.common, "worktrees", e.Name(), "gitdir"))
		if err != nil {
			return nil, false
		}
		dot := filepath.Clean(strings.TrimSpace(string(raw)))
		if filepath.Base(dot) != ".git" {
			return nil, false
		}
		out = append(out, filepath.Dir(dot))
	}
	return out, true
}

// gfBranchOn is the branch the checkout at dir is on, as symbolic-ref --short
// HEAD prints it. A detached HEAD, which git answers with an error, and a
// branch whose short name another ref also has, which git qualifies, are left
// to git.
func gfBranchOn(dir string) (string, bool) {
	r, ok := gfOpen(dir)
	if !ok {
		return "", false
	}
	raw, err := os.ReadFile(filepath.Join(r.gitDir, "HEAD"))
	if err != nil {
		return "", false
	}
	target, sym := strings.CutPrefix(strings.TrimSpace(string(raw)), "ref: ")
	branch, isBranch := strings.CutPrefix(target, "refs/heads/")
	if !sym || !isBranch || !gfFullRef(target) {
		return "", false
	}
	// The short forms git tries before refs/heads/<name>.
	for _, earlier := range []string{branch, "refs/" + branch, "refs/tags/" + branch} {
		if found, ok := r.exists(earlier); found || !ok {
			return "", false
		}
	}
	return branch, true
}

// gfStashes is the stash reflog as it is on disk, which changes with every
// push, pop and drop and is empty when there is no stash: what `git stash
// list` is made from.
func gfStashes(dir string) (string, bool) {
	r, ok := gfOpen(dir)
	if !ok {
		return "", false
	}
	raw, err := os.ReadFile(filepath.Join(r.common, "logs", "refs", "stash"))
	if errors.Is(err, fs.ErrNotExist) {
		return "", true
	}
	return string(raw), err == nil
}

// gfOwnPath is where the checkout at dir keeps one of the files that belong to
// that checkout, such as MERGE_HEAD or rebase-merge, as rev-parse --git-path
// names it.
func gfOwnPath(dir, name string) (string, bool) {
	r, ok := gfOpen(dir)
	if !ok {
		return "", false
	}
	return filepath.Join(r.gitDir, name), true
}
