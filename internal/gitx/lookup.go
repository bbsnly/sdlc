package gitx

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The lookups here go to the invocation's session (session.go) where there is
// one, and to a process of their own, as before, where there is none or the
// session cannot answer. Each asks the session only a shared name and only
// takes the answer whose shape the question has; anything else is the
// process's to answer, with the same refusals it has always given.

// sessionOf is the session for the repository root is a checkout of, or nil.
func sessionOf(ctx context.Context, root string) *session {
	m := memoOf(ctx)
	if m == nil {
		return nil
	}
	return m.sessionFor(ctx, root)
}

// plainSessionOf is sessionOf for a lookup that runs as plain git today:
// one that reads replace refs and grafts, which the sealed session does not.
// It is nil where the repository has any, so that the answer stays the one
// today's process gives (US-129 design review, security F-17 and red team
// F-28): refs/replace/* loose or packed, info/grafts, GIT_REPLACE_REF_BASE,
// GIT_NO_REPLACE_OBJECTS or GIT_GRAFT_FILE in the environment; and a reftable
// ref store, whose refs/replace cannot be read from the files.
func plainSessionOf(ctx context.Context, root string) *session {
	m := memoOf(ctx)
	if m == nil {
		return nil
	}
	_, common, err := GitDirOf(root)
	if err != nil {
		return nil
	}
	m.mu.Lock()
	allowed, seen := m.plain[common]
	if !seen {
		allowed = plainLookupsReadAlike(common)
		if m.plain == nil {
			m.plain = map[string]bool{}
		}
		m.plain[common] = allowed
	}
	m.mu.Unlock()
	if !allowed {
		return nil
	}
	return m.sessionFor(ctx, root)
}

// plainLookupsReadAlike reports whether a plain lookup and a sealed one read
// the same in the repository whose common directory is common.
func plainLookupsReadAlike(common string) bool {
	for _, v := range []string{"GIT_REPLACE_REF_BASE", "GIT_NO_REPLACE_OBJECTS", "GIT_GRAFT_FILE"} {
		if _, set := os.LookupEnv(v); set {
			return false
		}
	}
	if _, err := os.Stat(filepath.Join(common, "info", "grafts")); err == nil {
		return false
	}
	if _, err := os.Stat(filepath.Join(common, "reftable")); err == nil {
		return false
	}
	if entries, err := os.ReadDir(filepath.Join(common, "refs", "replace")); err == nil && len(entries) > 0 {
		return false
	}
	f, err := os.Open(filepath.Join(common, "packed-refs"))
	if err != nil {
		return true
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if strings.Contains(sc.Text(), " refs/replace/") {
			return false
		}
	}
	return sc.Err() == nil
}

// headFromFile is Head read from the checkout's own HEAD file and the
// session: the commit, NoCommits for a branch with none yet, and false where
// the process has to be asked. A detached HEAD holds the hash itself; a
// branch is a ref: line, and only one under refs/heads/ is a name every
// checkout reads alike.
func headFromFile(ctx context.Context, root string) (string, bool) {
	s := plainSessionOf(ctx, root)
	if s == nil {
		return "", false
	}
	gitDir, _, err := GitDirOf(root)
	if err != nil {
		return "", false
	}
	head, ok := headFile(gitDir)
	if !ok {
		return "", false
	}
	if ref, isRef := strings.CutPrefix(head, "ref: "); isRef {
		return branchHeadFromSession(ctx, root, s, ref)
	}
	if !fullHash(head) {
		return "", false
	}
	if r, ok := s.info(head); ok && r.found(typeCommit) {
		return head, true
	}
	return "", false
}

// branchHeadFromSession is headFromFile for a HEAD that names ref.
func branchHeadFromSession(ctx context.Context, root string, s *session, ref string) (string, bool) {
	if !strings.HasPrefix(ref, headsPrefix) {
		return "", false
	}
	r, ok := s.info(ref)
	switch {
	case !ok || r.ambiguous:
		return "", false
	case r.missing:
		// A branch with no commits yet, as rev-parse failing and
		// symbolic-ref answering reads today -- only where the ref is
		// not there. A ref whose commit git cannot find is the process's
		// to answer: rev-parse prints the hash all the same, and the
		// commit gate reads that of a reviewed commit put beyond reach.
		if refAbsent(root, ref) {
			if _, err := Branch(ctx, root); err == nil {
				return NoCommits, true
			}
		}
		return "", false
	case r.found(typeCommit):
		if there, _ := refInFiles(root, ref); !there {
			return "", false
		}
		return r.oid, true
	}
	return "", false
}

const typeCommit = "commit"

// tipFromSession is BranchTip from the session: the hash refs/heads/<branch>
// holds, missing for no such branch, and false where the process has to be
// asked.
func tipFromSession(ctx context.Context, root, branch string) (tip string, missing, ok bool) {
	r, ok := sessionOf(ctx, root).info(headsPrefix + branch)
	if !ok || r.ambiguous {
		return "", false, false
	}
	there, absent := refInFiles(root, headsPrefix+branch)
	if r.missing {
		// Missing is the branch not being there or its commit not being
		// found. Only the first is answered here; for the second the
		// process is asked and its answer is BranchTip's: show-ref --verify
		// dies with "bad ref" there (exit 128, probed for the code review's
		// F-4), where this once said it printed the hash, ran it and
		// dropped what it said.
		return "", absent, absent
	}
	if !there {
		return "", false, false
	}
	return r.oid, false, true
}

// refInFiles is what the files of the repository root belongs to show of ref:
// there, as a regular file of that name holding a full hash and nothing else,
// or a packed-refs line for exactly it; absent, loose and packed; or neither,
// where the files cannot say what git would: a reftable store, a directory or
// a link at the name, or a file that holds anything but a hash. That last is
// a symbolic ref, which git follows, or a broken one, which git refuses:
// with refs/heads/alias a symbolic ref to trunk's branch and HEAD on it, git
// says the checkout is on trunk, and read as "on alias" start refused it as
// off trunk (US-129 verification, round 6).
//
// The session resolves a name by git's guessing rules, so for a branch that
// is not there it answered the tag refs/tags/refs/heads/<branch>, where the
// base's show-ref --verify said there was no such branch: a tag any process
// can make chose the tip the landing reads (US-129 code review, security
// F-1). Its answer for a ref is used only where the ref is there, which is
// what the rules try first. And it reports a ref that is not there and one
// whose object git cannot find alike, which absent tells apart.
func refInFiles(root, ref string) (there, absent bool) {
	_, common, err := GitDirOf(root)
	if err != nil {
		return false, false
	}
	if _, err := os.Stat(filepath.Join(common, "reftable")); err == nil {
		return false, false
	}
	loose := filepath.Join(common, filepath.FromSlash(ref))
	switch info, err := os.Lstat(loose); {
	case err == nil:
		if !info.Mode().IsRegular() {
			return false, false
		}
		raw, err := os.ReadFile(loose)
		return err == nil && fullHash(strings.TrimSuffix(string(raw), "\n")), false
	case !errors.Is(err, os.ErrNotExist):
		return false, false
	}
	f, err := os.Open(filepath.Join(common, "packed-refs"))
	if errors.Is(err, os.ErrNotExist) {
		return false, true
	}
	if err != nil {
		return false, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if strings.HasSuffix(sc.Text(), " "+ref) {
			return true, false
		}
	}
	return false, sc.Err() == nil
}

// refAbsent reports whether the files show no ref named ref. False where it
// cannot be told from them.
func refAbsent(root, ref string) bool {
	_, absent := refInFiles(root, ref)
	return absent
}

// commitFromSession is whether the session has the commit: false, false where
// the process has to be asked.
func commitFromSession(ctx context.Context, root, commit string) (has, ok bool) {
	r, ok := sessionOf(ctx, root).info(commit)
	switch {
	case !ok || r.ambiguous:
		return false, false
	case r.missing:
		return false, true
	case r.found(typeCommit):
		return true, true
	}
	return false, false
}

// fileFromSession is FileAt from the session, and false where the process has
// to be asked: a commit it does not have, a path the commit does not have as a
// file, or a name it cannot be asked.
func fileFromSession(ctx context.Context, root, commit, path string) ([]byte, bool) {
	s := sessionOf(ctx, root)
	if r, ok := s.info(commit + "^{commit}"); !ok || !r.found(typeCommit) {
		return nil, false
	}
	body, r, ok := s.contents(commit + ":" + path)
	if !ok || !r.found("blob") {
		return nil, false
	}
	return body, true
}

// commitNamedBySession is Resolve from the session, for the spellings every
// checkout reads alike -- a full hash, or a full ref under refs/heads/ or
// refs/tags/ that is there (refInFiles) -- and false for every other spelling
// and whenever the name is not a commit, which the process refuses in its own
// words.
func commitNamedBySession(ctx context.Context, root, rev string) (string, bool) {
	if !fullHash(rev) {
		if !strings.HasPrefix(rev, headsPrefix) && !strings.HasPrefix(rev, "refs/tags/") {
			return "", false
		}
		if there, _ := refInFiles(root, rev); !there {
			return "", false
		}
	}
	r, ok := plainSessionOf(ctx, root).info(rev + "^{commit}")
	if !ok || !r.found(typeCommit) {
		return "", false
	}
	return r.oid, true
}

// holdsFromSession is Holds from the session, for a commit's full hash: has
// is whether commit has anything at path, and ok false where the process has
// to be asked.
func holdsFromSession(ctx context.Context, root, commit, path string) (has, ok bool) {
	r, ok := plainSessionOf(ctx, root).info(commit + ":" + path)
	if !ok || r.ambiguous {
		return false, false
	}
	return !r.missing, true
}

// refHeldBySession is whether the session finds refs/heads/<branch> at all,
// and false, false where the process has to be asked.
func refHeldBySession(ctx context.Context, root, branch string) (held, ok bool) {
	r, ok := plainSessionOf(ctx, root).info(headsPrefix + branch)
	if !ok || r.ambiguous {
		return false, false
	}
	there, absent := refInFiles(root, headsPrefix+branch)
	if r.missing {
		return false, absent
	}
	return there, there
}

// isCommitInSession is whether the sealed session says hash is a commit:
// what `merge-base X X` and `merge-base --is-ancestor X X` establish before
// answering for a commit and itself (US-129 design review, red team F-22).
func isCommitInSession(ctx context.Context, root, hash string) bool {
	r, ok := sessionOf(ctx, root).info(hash)
	return ok && r.found(typeCommit)
}
