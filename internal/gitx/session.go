package gitx

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// A session is one `git cat-file --batch-command` process that answers every
// object and revision lookup an invocation makes of one repository: HEAD's
// commit, a branch's tip, whether a commit exists, a file as a commit has it.
// One process in place of one per question. It keeps nothing: git resolves
// every request against the refs and objects as they are when it arrives,
// which was measured across another process's update-ref, pack-refs, a repack
// and a detach (US-129's plan, 2.2), so an invocation's own write is seen by
// its next lookup with no drop rule.
//
// It is asked only names that read the same from every checkout of the
// repository: full hashes, refs/heads/* and refs/tags/* by full name,
// <hash>^{commit} and <hash>:<path>. HEAD, refs/bisect/*, refs/worktree/* and
// the pseudorefs belong to one worktree, and a session started for one
// checkout answered another's HEAD (design review, security F-9): Head reads
// the checkout's own HEAD file instead (headFile here, headFromFile in
// lookup.go, Head and branchFromFile in gitx.go). It runs with --git-dir on
// the common directory and its working directory there, so no checkout's
// removal waits on it (security F-15).
//
// A request is one line, so a name holding a line break, a carriage return or
// NUL is never written; the caller runs today's process for it. Every reply
// is read against the request's shape, and anything else ends the session:
// after a split request every later reply would belong to an earlier
// question (security F-10). Where no session can start -- git before 2.36 has
// no --batch-command -- or one ends, every lookup runs as its own process, as
// before; the session's own failure never becomes a refusal.
type session struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     *bufio.Reader
	private string
	dead    bool
	counted func()
}

// reply is what the session said of one name.
type reply struct {
	oid, typ  string
	size      int64
	missing   bool
	ambiguous bool
}

// found reports whether the reply names an object of type typ.
func (r reply) found(typ string) bool { return !r.missing && !r.ambiguous && r.typ == typ }

// sessionFor is the session for the repository the checkout root belongs to,
// started if need be, and nil where none can run in this invocation. It
// starts under the invocation's context, not the asker's: every later lookup
// is answered by it, and under the first asker's it ended with whatever that
// one call was bounded by (US-129 code review, F-7).
func (m *Memo) sessionFor(_ context.Context, root string) *session {
	_, common, err := GitDirOf(root)
	if err != nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	if s, seen := m.sessions[common]; seen {
		return s
	}
	if m.sessions == nil {
		m.sessions = map[string]*session{}
	}
	s := startSession(m.ctx, common) //nolint:contextcheck // the invocation's context on purpose: the session answers every later lookup, not only this asker's
	if s != nil {
		m.starts++
		s.counted = func() {
			m.mu.Lock()
			m.requests++
			m.mu.Unlock()
		}
	}
	m.sessions[common] = s
	return s
}

// startSession starts the process for the repository whose common directory
// is common, or returns nil.
func startSession(ctx context.Context, common string) *session {
	private, err := os.MkdirTemp("", "sdlc-git-")
	if err != nil {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "--no-replace-objects",
		"-c", "core.fsmonitor=", "-c", "core.commitGraph=false",
		"--git-dir="+common, "cat-file", "--batch-command")
	cmd.Dir = common
	cmd.Env = sealedEnviron(private)
	cmd.Stderr = io.Discard
	in, err := cmd.StdinPipe()
	if err != nil {
		_ = os.RemoveAll(private)
		return nil
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(private)
		return nil
	}
	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(private)
		return nil
	}
	return &session{cmd: cmd, in: in, out: bufio.NewReader(out), private: private}
}

// usable reports whether name may be written as one request line.
func usable(name string) bool {
	return name != "" && !strings.ContainsAny(name, "\n\r\x00")
}

// info asks what name is, and reports false where the session could not
// answer: the caller then runs its own process.
func (s *session) info(name string) (reply, bool) {
	if s == nil || !usable(name) {
		return reply{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dead {
		return reply{}, false
	}
	r, ok := s.ask("info", name)
	return r, ok
}

// maxContents is the largest object the session reads into memory. What the
// loop reads through it is the configuration, test files and the contract,
// kilobytes each; the buffer is allocated on git's word for the size, and
// anything larger is the process's to read, as it always was.
const maxContents = 64 << 20

// contents is the bytes of the object name names, and what the session said
// of it; false where the session could not answer.
func (s *session) contents(name string) ([]byte, reply, bool) {
	if s == nil || !usable(name) {
		return nil, reply{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dead {
		return nil, reply{}, false
	}
	r, ok := s.ask("contents", name)
	if !ok || r.missing || r.ambiguous {
		return nil, r, ok
	}
	if r.size < 0 || r.size > maxContents {
		s.end()
		return nil, reply{}, false
	}
	body := make([]byte, r.size+1)
	if _, err := io.ReadFull(s.out, body); err != nil || body[r.size] != '\n' {
		s.end()
		return nil, reply{}, false
	}
	return body[:r.size], r, true
}

// ask writes one request and reads its header line. The lock is held.
func (s *session) ask(verb, name string) (reply, bool) {
	if s.counted != nil {
		s.counted()
	}
	if _, err := io.WriteString(s.in, verb+" "+name+"\n"); err != nil {
		s.end()
		return reply{}, false
	}
	line, err := s.out.ReadString('\n')
	if err != nil {
		s.end()
		return reply{}, false
	}
	line = strings.TrimSuffix(line, "\n")
	switch line {
	case name + " missing":
		return reply{missing: true}, true
	case name + " ambiguous":
		return reply{ambiguous: true}, true
	}
	fields := strings.Fields(line)
	if len(fields) != 3 || !fullHash(fields[0]) || strings.Count(line, " ") != 2 {
		s.end()
		return reply{}, false
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		s.end()
		return reply{}, false
	}
	return reply{oid: fields[0], typ: fields[1], size: size}, true
}

// end marks the session dead and ends the process: killed, not asked to
// finish, since it only reads and the hook's reply must not wait on git
// ending (security F-12). The lock is held.
func (s *session) end() {
	if s.dead {
		return
	}
	s.dead = true
	_ = s.in.Close()
	_ = s.cmd.Process.Kill()
	_ = s.cmd.Wait()
	_ = os.RemoveAll(s.private)
}

// close ends the session from Close, without waiting for a request in
// progress on another goroutine.
func (s *session) close() {
	if s.mu.TryLock() {
		s.end()
		s.mu.Unlock()
		return
	}
	_ = s.in.Close()
	_ = s.cmd.Process.Kill()
	// The request that holds the lock removes this too when its read fails,
	// if it runs again before the process exits; the hook's does not.
	_ = os.RemoveAll(s.private)
}

// headFile is what the checkout's own HEAD file holds, where it holds one of
// the two shapes git writes and nothing else: `ref: <name>` with a name under
// refs/ that keeps git's ref-name rules, or a bare hash, each with one line
// ending or none. Git also trims white space after "ref:" and at the end of
// the line, and a file read more leniently here read `ref: refs/heads/main `
// as a branch named "main " where git read main (US-129 verification, F-2;
// red team V23), and `ref: refs/heads/../heads/main` as a branch where
// symbolic-ref refused the name (verification 2, F-13): every other shape is
// false, and git's to read, with its own answer or error. So is a HEAD that
// is not a regular file: git reads a link to refs/heads/<x> as being on <x>,
// and read through the link here it was the hash the branch holds, a detached
// HEAD, so start refused a checkout on trunk as off it (code review,
// security F-2).
func headFile(gitDir string) (string, bool) {
	path := filepath.Join(gitDir, "HEAD")
	if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	head := strings.TrimSuffix(string(raw), "\n")
	if ref, isRef := strings.CutPrefix(head, "ref: "); isRef {
		return head, refName(ref)
	}
	return head, fullHash(head)
}

// refName reports whether name is a full ref name under refs/ that
// check-ref-format accepts: no empty component, none starting with a dot or
// ending in .lock, no "..", no "@{", no control byte, space or one of
// ~^:?*[\, and no slash or dot at the end. A name that fails here is not
// refused: it goes to git, whose reading of it is the one that counts.
func refName(name string) bool {
	rest, underRefs := strings.CutPrefix(name, "refs/")
	if !underRefs || rest == "" || strings.Contains(name, "..") || strings.Contains(name, "@{") ||
		strings.HasSuffix(name, "/") || strings.HasSuffix(name, ".") {
		return false
	}
	for i := range len(name) {
		if c := name[i]; c < 0x21 || c == 0x7f || strings.IndexByte("~^:?*[\\", c) >= 0 {
			return false
		}
	}
	for component := range strings.SplitSeq(rest, "/") {
		if component == "" || component[0] == '.' || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}
