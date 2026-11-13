package gitx

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

// A Memo is what one invocation -- one command, one hook call -- has already
// asked git and may ask again without a second process: the answers of
// read-only calls whose input is refs, the configuration or the worktree list,
// and the session (session.go) that answers object and revision lookups. It
// lives in the context the invocation runs under, and nothing of it outlives
// the invocation: tests run many invocations in one process and change the
// repository between them, and a package-level memo of anything mutable failed
// them (US-129's analysis, section 5). What is kept of immutable facts, keyed by
// full hashes, stays in answers (memo.go).
//
// Every git call is one of three classes (classOf). A kept answer is dropped by
// any call of the third class through the same context, by Forget, and by
// Close. An answer is stored only if the memo's generation is what it was when
// the call began: a read that started before another goroutine's write and
// finished after the drop would otherwise store the answer from before the
// write (US-129's design review, red team F-4).
type Memo struct {
	// ctx is the context the invocation began under, which its sessions run
	// under whichever call starts them (session.go).
	ctx  context.Context //nolint:containedctx // a session outlives the lookup that starts it and must end with the invocation, not with that lookup's own context
	mu   sync.Mutex
	gen  uint64
	kept map[memoKey][]byte
	// sessions, by the canonical common git directory of the repository.
	// A nil entry is a repository where no session could be started, so that
	// it is not tried again in this invocation.
	sessions map[string]*session
	// plain is, by common git directory, whether the lookups that run as
	// plain git today may go to the sealed session: false where the
	// repository has replace refs or grafts, or its refs are a reftable.
	plain  map[string]bool
	closed bool
	// For the round records of US-129, through slog.Debug at Close.
	hits, starts, requests int
}

// memoKey is one question: the call's class of environment (sealed or plain),
// the checkout it ran in, what the environment added, and the arguments as the
// caller passed them. A sealed call's private hooks directory is not part of
// the question, since no hook runs for a read.
type memoKey struct {
	sealed    bool
	root, env string
	args      string
}

type memoContextKey struct{}

// WithMemo is ctx carrying a new Memo, or ctx itself where it already has one.
func WithMemo(ctx context.Context) context.Context {
	if memoOf(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, memoContextKey{}, &Memo{ctx: ctx, kept: map[memoKey][]byte{}})
}

func memoOf(ctx context.Context) *Memo {
	m, _ := ctx.Value(memoContextKey{}).(*Memo)
	return m
}

// Forget drops every answer the invocation has kept, and leaves its session
// open: the session keeps nothing and reads refs and objects afresh on every
// request. It is called after a project's configured command has run, and
// whenever the loop's lock is taken, so that what follows a lock is a fresh
// read (THREATS, point 3).
func Forget(ctx context.Context) {
	if m := memoOf(ctx); m != nil {
		m.drop()
	}
}

// Close ends the invocation's reuse: the kept answers go, the session is
// ended, and no session starts afterwards. Deferred where the invocation
// begins, after its answer is written: the hook's reply must not wait on git
// ending (US-129's design review, security F-12).
func Close(ctx context.Context) {
	m := memoOf(ctx)
	if m == nil {
		return
	}
	m.mu.Lock()
	m.closed = true
	m.gen++
	clear(m.kept)
	sessions := m.sessions
	m.sessions = nil
	hits, starts, requests := m.hits, m.starts, m.requests
	m.mu.Unlock()
	for _, s := range sessions {
		if s != nil {
			s.close()
		}
	}
	slog.Debug("gitx: the invocation's reuse", "memo_hits", hits, "session_starts", starts, "session_requests", requests)
}

func (m *Memo) drop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gen++
	clear(m.kept)
}

func (m *Memo) generation() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gen
}

func (m *Memo) lookup(key memoKey) ([]byte, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out, ok := m.kept[key]
	if ok {
		m.hits++
		return slices.Clone(out), true
	}
	return nil, false
}

// keep stores out for key, unless the memo moved on while the call ran.
func (m *Memo) keep(key memoKey, out []byte, gen uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen == gen && !m.closed {
		m.kept[key] = slices.Clone(out)
	}
}

// callClass is what a git call does to the memo.
type callClass int

const (
	// classDrop is a call that may change what any kept answer stands for:
	// everything not in the other two classes, unknown subcommands included.
	classDrop callClass = iota
	// classKeep is a read whose input is refs, the configuration or the
	// worktree list: its answer is kept, and a second call is served from it.
	classKeep
	// classPass is run every time and leaves the memo alone: a read of the
	// index or the work tree, which is never kept because the process writes
	// under .sdlc between two such reads; and a call that writes only a
	// private index or loose objects, which changes nothing a kept answer
	// stands for (the frozen AC-3 row on gate tests_frozen and gate commit
	// holds this: the tree hash's calls sit between two trunk-tip reads).
	classPass
)

// classOf is the class of the call args, with stdin on its standard input and
// stdout, where not nil, taking its output; privateIndex says the call's
// environment points git at an index of its own.
func classOf(args []string, stdin []byte, stdout io.Writer, privateIndex bool) callClass {
	sub, rest := subcommand(args)
	has := func(flags ...string) bool {
		return slices.ContainsFunc(rest, func(a string) bool {
			return slices.ContainsFunc(flags, func(f string) bool { return a == f || strings.HasPrefix(a, f+"=") })
		})
	}
	var positional []string
	for _, a := range rest {
		if !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
		}
	}
	switch {
	case passesThrough(sub, has, privateIndex):
		return classPass
	case !keepsAnswer(sub, has, positional):
		return classDrop
	case stdin != nil || stdout != nil:
		// A question on standard input, or an answer sent to a writer, is not
		// in the arguments: two cat-file --batch calls spell the same argv for
		// two files, and a kept answer would hand the second the first's bytes
		// (US-129's design review, security F-1).
		return classPass
	}
	return classKeep
}

// passesThrough reports whether sub reads the working tree, or writes only to
// an index of the call's own: neither kept nor a reason to drop what is.
func passesThrough(sub string, has func(flags ...string) bool, privateIndex bool) bool {
	switch sub {
	case "status", "ls-files", "diff-index", "diff-files", "check-ignore", "check-attr":
		return true
	case "hash-object":
		return !has("-w") || privateIndex
	case "add", "update-index", "write-tree":
		return privateIndex
	}
	return false
}

// keepsAnswer reports whether sub, with its options and positional arguments,
// only reads the repository's objects, refs and configuration, so that its
// answer holds until something writes.
func keepsAnswer(sub string, has func(flags ...string) bool, positional []string) bool {
	switch sub {
	case "rev-parse", "show-ref", "rev-list", "log", "merge-base", "ls-tree", "for-each-ref", "var", "version",
		"cat-file", "describe", "name-rev":
		return true
	case "diff-tree":
		return !has("--stdin")
	case "config":
		return has("--get", "--get-all", "--get-regexp", "--list", "-l") ||
			(len(positional) > 0 && (positional[0] == "get" || positional[0] == "list"))
	case "worktree":
		return len(positional) > 0 && positional[0] == "list"
	case "symbolic-ref":
		return !has("-d", "--delete") && len(positional) <= 1
	}
	return false
}

// subcommand is git's subcommand in args and what follows it, past the
// options git takes before it.
func subcommand(args []string) (string, []string) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-C" || a == "-c":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return a, args[i+1:]
		}
	}
	return "", nil
}

// hasPrivateIndex reports whether extra points git at an index of its own.
func hasPrivateIndex(extra []string) bool {
	return slices.ContainsFunc(extra, func(kv string) bool { return strings.HasPrefix(kv, "GIT_INDEX_FILE=") })
}

// memoised runs call for args in root, serving a kept answer where there is
// one and keeping the answer where the class allows; sealed and env make the
// key with them.
func memoised(ctx context.Context, sealed bool, root string, env []string, stdin []byte, stdout io.Writer,
	args []string, call func() ([]byte, error)) ([]byte, error) {
	m := memoOf(ctx)
	if m == nil {
		return call()
	}
	class := classOf(args, stdin, stdout, hasPrivateIndex(env))
	if class == classDrop {
		// Dropped before the call and again when it returns: a read on
		// another goroutine that began while the write ran took its
		// generation after the first drop, and would keep an answer from
		// the middle of the write (US-129 code review, F-8).
		m.drop()
		defer m.drop()
	}
	if class != classKeep {
		return call()
	}
	key := memoKey{sealed: sealed, root: absRoot(root), env: strings.Join(env, "\x00"), args: strings.Join(args, "\x00")}
	if out, ok := m.lookup(key); ok {
		return out, nil
	}
	gen := m.generation()
	out, err := call()
	if err == nil {
		m.keep(key, out, gen)
	}
	return out, err
}

// absRoot is root as an absolute path, or root itself where none can be made.
func absRoot(root string) string {
	if abs, err := absPath(root); err == nil {
		return abs
	}
	return root
}
