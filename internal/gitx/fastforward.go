package gitx

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// absent is the mode git gives a path a tree does not have.
const absent = "000000"

// entry is a path's state in a tree, an index or a working tree: its mode and
// its object, or absent.
type entry struct{ mode, hash string }

func (e entry) there() bool { return e.mode != absent && e.mode != "" }

// change is one path the fast-forward writes, as from and to have it.
type change struct {
	path     string
	from, to entry
}

// heldPath is a path a checkout held other than as from has it, and is allowed
// to: what the checkout held there, to put back where the landing is refused.
type heldPath struct {
	path        string
	from, index entry
	// body is the file's content, where there was one, and perm its
	// permissions.
	body []byte
	perm fs.FileMode
}

// checkoutPlan is what the fast-forward does in one checkout that holds trunk.
type checkoutPlan struct {
	holder
	// takeIn is the paths whose working tree already holds to's content and
	// whose index does not yet: taken into the index before the move.
	takeIn []change
	// held is every path the checkout held other than as from has it.
	held []heldPath
}

// FastForward moves refs/heads/<trunk> from the commit from, which the caller
// read, to the commit to, and every checkout that has trunk checked out moves
// with it; where none has, only the ref moves. It makes no commit, runs no
// hook, and never reaches a remote.
//
// Every checkout that holds trunk is checked before anything is written, with
// a fixed number of git calls for each, and one in a state the move would
// write over is a refusal that names it. Then each checkout's index and files
// move from from to to, tree first and ref second as git's own fast-forward
// does, and the ref moves by compare-and-swap. Where git refuses in a later
// checkout, or the swap is lost, every checkout already written is put back.
func FastForward(ctx context.Context, root, trunk, from, to string) error {
	if err := branchName(ctx, root, trunk); err != nil {
		return err
	}
	if err := fullHashes(from, to); err != nil {
		return err
	}
	tip, err := BranchTip(ctx, root, trunk)
	if err != nil {
		return err
	}
	if tip != from {
		return notFastForward(trunk+" is at "+tip[:12]+", not at "+from[:12]+", where it was read",
			"another commit reached "+trunk+" after the landing read it")
	}
	if ok, err := IsAncestor(ctx, root, from, to); err != nil {
		return err
	} else if !ok {
		return notFastForward(to[:12]+" does not follow on from "+trunk+" at "+from[:12],
			"a fast-forward only ever moves a branch to a commit that has its tip in its history")
	}
	if merges, err := HasMerges(ctx, root, from, to); err != nil {
		return err
	} else if merges {
		return notFastForward("a merge commit lies between "+from[:12]+" and "+to[:12],
			"a landing puts the story's commits on "+trunk+" in a line, and carries no merge")
	}

	changes, err := writtenPaths(ctx, root, from, to)
	if err != nil {
		return err
	}
	holding, err := holders(ctx, root, trunk)
	if err != nil {
		return err
	}
	plans := make([]*checkoutPlan, 0, len(holding))
	for _, h := range holding {
		p, err := checkCheckout(ctx, h, trunk, from, changes)
		if err != nil {
			return err
		}
		plans = append(plans, p)
	}

	var written []*checkoutPlan
	for _, p := range plans {
		if err := p.write(ctx, from, to); err != nil {
			left := putBack(ctx, written, from, to)
			return sdlcerr.New(sdlcerr.UncommittedOnTrunk,
				"git would not move the checkout "+strconv.Quote(p.path)+", which has "+trunk+" checked out",
				failure(err, "git refused")+left).WithCause(ErrWouldOverwrite)
		}
		written = append(written, p)
	}
	if _, err := sealed(ctx, root, "update-ref", "-m", "sdlc: landed by fast-forward",
		"refs/heads/"+trunk, to, from); err != nil {
		left := putBack(ctx, written, from, to)
		return notFastForward(trunk+" could not be moved from "+from[:12]+" to "+to[:12],
			failure(err, "git refused")+left)
	}
	return nil
}

func notFastForward(what, why string) error {
	return sdlcerr.New(sdlcerr.BranchBehindTrunk, what, why).WithCause(ErrNotFastForward)
}

// writtenPaths is every path that differs between from and to.
func writtenPaths(ctx context.Context, root, from, to string) ([]change, error) {
	out, err := sealed(ctx, root, "diff-tree", "-r", "-z", "--raw", "--no-renames", "--full-index",
		"--ignore-submodules=none", from, to, "--")
	if err != nil {
		return nil, err
	}
	var changes []change
	for _, d := range parseRaw(out) {
		changes = append(changes, change{path: d.path, from: d.src, to: d.dst})
	}
	return changes, nil
}

// rawDiff is one entry of git's raw diff output.
type rawDiff struct {
	path     string
	src, dst entry
}

// parseRaw reads git's raw diff output, written with -z and no renames: a
// ":srcmode dstmode srchash dsthash status" field, then the path.
func parseRaw(out []byte) []rawDiff {
	fields := strings.Split(string(out), "\x00")
	var diffs []rawDiff
	for i := 0; i+1 < len(fields); i += 2 {
		meta := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(meta) < 5 {
			break
		}
		diffs = append(diffs, rawDiff{
			path: fields[i+1],
			src:  entry{meta[0], meta[2]},
			dst:  entry{meta[1], meta[3]},
		})
	}
	return diffs
}

// zeroHash reports whether git wrote a hash of zeros: content it did not read.
func zeroHash(h string) bool { return strings.Trim(h, "0") == "" }

// checkCheckout checks one checkout that holds trunk, and writes nothing. What
// it finds in the way is a refusal; a path that already holds to's content is
// not in the way, and is marked for the write phase to take in.
func checkCheckout(ctx context.Context, h holder, trunk, from string, changes []change) (*checkoutPlan, error) {
	quoted := strconv.Quote(h.path)
	if h.detached {
		return nil, partWayRefusal(quoted, "a rebase or a bisect of "+trunk)
	}
	if op, err := partWay(h.gitDir); err != nil {
		return nil, err
	} else if op != "" {
		return nil, partWayRefusal(quoted+", which has "+trunk+" checked out,", op)
	}
	lock := filepath.Join(h.gitDir, "index.lock")
	if _, err := os.Lstat(lock); err == nil {
		return nil, sdlcerr.New(sdlcerr.UncommittedOnTrunk,
			"the checkout "+quoted+", which has "+trunk+" checked out, has its index locked",
			"git keeps "+strconv.Quote(lock)+" while a git command runs there, and the landing does not "+
				"write a checkout's index while one may be").
			WithFix(`if no git command is running in that checkout, remove the lock file named above; then run "sdlc stop" in the story's worktree again; an assistant hands the story to a person with "sdlc escalate" and stops`)
	}

	cached, err := sealed(ctx, h.path, "diff-index", "-z", "--raw", "--no-renames", "--full-index", "--cached",
		"--ignore-submodules=none", from, "--")
	if err != nil {
		return nil, err
	}
	onDisk, err := sealed(ctx, h.path, "diff-index", "-z", "--raw", "--no-renames", "--full-index",
		"--ignore-submodules=none", from, "--")
	if err != nil {
		return nil, err
	}
	index, tree := map[string]entry{}, map[string]entry{}
	for _, d := range parseRaw(cached) {
		index[d.path] = d.dst
	}
	for _, d := range parseRaw(onDisk) {
		tree[d.path] = d.dst
	}

	adding := map[string]bool{}
	for _, c := range changes {
		if !c.to.there() || c.from.there() {
			continue
		}
		for d := path.Dir(c.path); d != "."; d = path.Dir(d) {
			adding[d] = true
		}
	}

	type state struct {
		c           change
		index, tree entry
	}
	states := make([]state, 0, len(changes))
	var inTheWay, toHash []string
	hashed := map[string]int{}
	for _, c := range changes {
		st := state{c: c, index: c.from, tree: c.from}
		if e, ok := index[c.path]; ok {
			st.index = e
		}
		if e, ok := tree[c.path]; ok {
			st.tree = e
		}
		switch {
		case st.tree.there() && st.tree.mode == "160000":
			st.tree = st.index
		case st.tree.there() && zeroHash(st.tree.hash):
			if st.tree.mode == "120000" || strings.Contains(c.path, "\n") {
				inTheWay = append(inTheWay, c.path)
				continue
			}
			hashed[c.path] = len(toHash)
			toHash = append(toHash, c.path)
		case !st.tree.there():
			// Nothing git tracks is there; something it does not may be.
			found, err := untracked(h.path, c.path)
			if err != nil {
				return nil, err
			}
			switch found {
			case "":
			case "file":
				if strings.Contains(c.path, "\n") {
					inTheWay = append(inTheWay, c.path)
					continue
				}
				st.tree = entry{mode: c.to.mode, hash: ""}
				hashed[c.path] = len(toHash)
				toHash = append(toHash, c.path)
			default:
				inTheWay = append(inTheWay, c.path)
				continue
			}
		}
		states = append(states, st)
	}
	// A parent of a path the landing adds that is a file or a link, and not a
	// path the landing itself takes away, would have the new file written
	// through it or refused half-way.
	var parents []string
	for d := range adding {
		parents = append(parents, d)
	}
	slices.Sort(parents)
	for _, d := range parents {
		if slices.ContainsFunc(changes, func(c change) bool { return c.path == d && !c.to.there() }) {
			continue
		}
		info, err := os.Lstat(filepath.Join(h.path, filepath.FromSlash(d)))
		if err == nil && !info.IsDir() {
			inTheWay = append(inTheWay, d)
		}
	}

	var hashes []string
	if len(toHash) > 0 {
		out, err := sealedCall(ctx, h.path, nil, []byte(strings.Join(toHash, "\n")+"\n"), nil,
			"hash-object", "--stdin-paths")
		if err != nil {
			return nil, err
		}
		hashes = strings.Fields(string(out))
		if len(hashes) != len(toHash) {
			return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
				"the files in "+quoted+" could not be read",
				"git hashed "+strconv.Itoa(len(hashes))+" of "+strconv.Itoa(len(toHash))+" of them")
		}
	}

	plan := &checkoutPlan{holder: h}
	for _, st := range states {
		if i, ok := hashed[st.c.path]; ok {
			st.tree.hash = hashes[i]
		}
		c := st.c
		switch {
		case st.index == c.from && st.tree == c.from:
		case st.tree == c.to && (st.index == c.from || st.index == c.to):
			if st.index == c.from {
				plan.takeIn = append(plan.takeIn, c)
			}
			k := heldPath{path: c.path, from: c.from, index: st.index}
			full := filepath.Join(h.path, filepath.FromSlash(c.path))
			if info, err := os.Lstat(full); err == nil && info.Mode().IsRegular() {
				if k.body, err = os.ReadFile(full); err != nil {
					return nil, sdlcerr.New(sdlcerr.RepositoryUnreadable,
						strconv.Quote(c.path)+" in "+quoted+" could not be read", err.Error()).WithCause(err)
				}
				k.perm = info.Mode().Perm()
			}
			plan.held = append(plan.held, k)
		default:
			inTheWay = append(inTheWay, c.path)
		}
	}
	if len(inTheWay) > 0 {
		slices.Sort(inTheWay)
		names := make([]string, len(inTheWay))
		for i, p := range inTheWay {
			names[i] = strconv.Quote(p)
		}
		return nil, sdlcerr.New(sdlcerr.UncommittedOnTrunk,
			"the checkout "+quoted+", which has "+trunk+" checked out, has changes where the landing writes",
			"moving "+trunk+" there would write over "+strings.Join(names, ", ")+", which "+
				"nobody committed").WithCause(ErrWouldOverwrite)
	}
	return plan, nil
}

// untracked is what the checkout at dir has at the slash path p that its index
// does not: "" for nothing, "file" for a regular file by exactly that name,
// and otherwise what is there -- a directory, a link, or a name that differs
// from p only in case, which a file system that folds case finds for it.
func untracked(dir, p string) (string, error) {
	full := filepath.Join(dir, filepath.FromSlash(p))
	info, err := os.Lstat(full)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrInvalid), isNotDir(err):
		return "", nil
	case err != nil:
		return "", sdlcerr.New(sdlcerr.RepositoryUnreadable,
			strconv.Quote(p)+" in "+strconv.Quote(dir)+" could not be read", err.Error()).WithCause(err)
	}
	if !listedAs(filepath.Dir(full), filepath.Base(full)) {
		return "another name", nil
	}
	if !info.Mode().IsRegular() {
		return "not a file", nil
	}
	return "file", nil
}

// listedAs reports whether dir lists an entry by exactly name. A directory
// that cannot be listed lists nothing, so what Lstat found there is taken for
// another name, which is in the way.
func listedAs(dir, name string) bool {
	names, err := os.ReadDir(dir)
	return err == nil && slices.ContainsFunc(names, func(e fs.DirEntry) bool { return e.Name() == name })
}

// isNotDir reports whether err is a path with a file where a directory
// belongs, which holds nothing at the path itself.
func isNotDir(err error) bool {
	return errors.Is(err, syscall.ENOTDIR)
}

// partWayRefusal is FastForward's refusal of a checkout with an operation
// part-way in it.
func partWayRefusal(checkout, op string) error {
	return sdlcerr.New(sdlcerr.UncommittedOnTrunk,
		"the checkout "+checkout+" is part-way through "+op,
		"the landing moves the files of every checkout that has trunk checked out, and would write "+
			"into the middle of it").
		WithFix(`finish it or abandon it in that checkout, then run "sdlc stop" in the story's worktree again; an assistant hands the story to a person with "sdlc escalate" and stops`)
}

// write moves the checkout's index and files from from to to: what already
// holds to's content is taken in first, the index's view of the files is
// brought up to date, and a two-tree merge does the rest. Where git refuses,
// what was taken in is put back.
func (p *checkoutPlan) write(ctx context.Context, from, to string) error {
	if len(p.takeIn) > 0 {
		var info bytes.Buffer
		for _, c := range p.takeIn {
			info.WriteString(indexInfo(c.to, c.path))
		}
		if _, err := sealedCall(ctx, p.path, nil, info.Bytes(), nil, "update-index", "-z", "--index-info"); err != nil {
			return err
		}
	}
	_, err := sealed(ctx, p.path, "update-index", "-q", "--ignore-submodules", "--refresh")
	if err == nil {
		_, err = sealed(ctx, p.path, "read-tree", "-m", "-u", from, to)
	}
	if err != nil && len(p.takeIn) > 0 {
		var info bytes.Buffer
		for _, c := range p.takeIn {
			info.WriteString(indexInfo(c.from, c.path))
		}
		_, _ = sealedCall(ctx, p.path, nil, info.Bytes(), nil, "update-index", "-z", "--index-info")
	}
	return err
}

// indexInfo is the line update-index --index-info reads to set path to e, or
// to take it out where e is absent.
func indexInfo(e entry, path string) string {
	if !e.there() {
		return "0 " + strings.Repeat("0", 40) + "\t" + path + "\x00"
	}
	return e.mode + " " + e.hash + "\t" + path + "\x00"
}

// putBack moves every checkout in written back from to to from, with what
// each held put back as it was, and says which could not be.
func putBack(ctx context.Context, written []*checkoutPlan, from, to string) string {
	var left []string
	for i := len(written) - 1; i >= 0; i-- {
		p := written[i]
		if _, err := sealed(ctx, p.path, "read-tree", "-m", "-u", to, from); err != nil {
			left = append(left, strconv.Quote(p.path))
			continue
		}
		var info bytes.Buffer
		for _, k := range p.held {
			full := filepath.Join(p.path, filepath.FromSlash(k.path))
			var err error
			if k.body != nil {
				if err = os.MkdirAll(filepath.Dir(full), 0o755); err == nil {
					err = os.WriteFile(full, k.body, k.perm)
				}
			} else if err = os.Remove(full); errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
			if err != nil {
				left = append(left, strconv.Quote(p.path))
			}
			if k.index != k.from {
				info.WriteString(indexInfo(k.index, k.path))
			}
		}
		if info.Len() > 0 {
			if _, err := sealedCall(ctx, p.path, nil, info.Bytes(), nil, "update-index", "-z", "--index-info"); err != nil {
				left = append(left, strconv.Quote(p.path))
			}
		}
	}
	if len(left) == 0 {
		return "; every checkout it had moved was put back"
	}
	slices.Sort(left)
	return "; " + strings.Join(slices.Compact(left), ", ") + " could not be put back, and holds the story's files " +
		"at " + to[:12] + " while trunk is still at " + from[:12] + ": the next sdlc stop finishes the move there"
}

// RemoveLandedWorktree takes away the worktree at dir, on branch, once trunk
// has its commits, and deletes branch while it is at landed. A worktree
// holding files nobody committed is kept, and their names are returned: kept
// is what git lists as uncommitted or untracked, and files it ignores are
// removed with the worktree. The main checkout, a directory git does not
// list, and a worktree that is locked, on no branch or on another branch are
// refused, and nothing is ever removed by force.
func RemoveLandedWorktree(ctx context.Context, root, dir, branch, landed string) (kept []string, err error) {
	if err := notAnOption("a branch", branch); err != nil {
		return nil, err
	}
	if err := fullHashes(landed); err != nil {
		return nil, err
	}
	out, err := sealed(ctx, root, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	listed := parseWorktrees(out)
	at := slices.IndexFunc(listed, func(w listedWorktree) bool { return samePath(w.path, dir) })
	switch {
	case at < 0:
		return nil, notRemoved(strconv.Quote(dir)+" is not a worktree git lists",
			"only a worktree of this repository is taken away")
	case at == 0:
		return nil, notRemoved(strconv.Quote(dir)+" is the main checkout", "the main checkout is never taken away")
	case listed[at].locked:
		return nil, notRemoved(strconv.Quote(dir)+" is locked",
			"somebody locked it with git worktree lock, and a locked worktree is left to them")
	case listed[at].branch == "":
		return nil, notRemoved(strconv.Quote(dir)+" is on no branch",
			"a commit on no branch is lost when its worktree is removed")
	case listed[at].branch != "refs/heads/"+branch:
		return nil, notRemoved(strconv.Quote(dir)+" does not have "+strconv.Quote(branch)+" checked out",
			"a worktree is taken away only with the branch it was made on")
	}
	tip, err := BranchTip(ctx, root, branch)
	if err != nil {
		return nil, err
	}
	if tip != landed {
		return nil, notRemoved(strconv.Quote(branch)+" is at "+tip[:12]+", past the "+landed[:12]+" that landed",
			"its commits since are not on trunk, and removing the branch would lose them")
	}
	if kept, err = Uncommitted(ctx, dir, true); err != nil {
		return nil, err
	}
	if len(kept) > 0 {
		return kept, nil
	}
	if _, err := sealed(ctx, root, "worktree", "remove", "--", dir); err != nil {
		return nil, notRemoved("the worktree at "+strconv.Quote(dir)+" could not be taken away",
			failure(err, "git could not remove it")+"; it and the branch "+strconv.Quote(branch)+
				" are still there, and a file something has open in it keeps it there on Windows")
	}
	if _, err := sealed(ctx, root, "update-ref", "-d", "refs/heads/"+branch, landed); err != nil {
		return nil, notRemoved("the branch "+strconv.Quote(branch)+" could not be deleted",
			failure(err, "git could not delete it")+"; the worktree was taken away, and the branch is still there")
	}
	return nil, nil
}
