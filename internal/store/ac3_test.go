package store

// AC-3 (US-050): a message that names a file or directory path in a refusal
// writes each separator once, slash-separated, with nothing escaped that the
// path does not contain -- on every platform. store.quote wraps every path
// like that with strconv.Quote, which doubles every backslash of a native
// Windows path (ANALYSIS.md, "AC-3: every message whose path spelling
// changes"); the fix keeps the bytes POSIX already writes and makes Windows
// match them by converting to a slash-separated path before quoting it.
//
// This drives three representative refusals that name a path -- a claim held
// by another checkout, the worktrees .gitignore, and the landing lock -- and
// checks each one's wording rather than quote itself, since quote is not
// exported and every caller is expected to go through it the same way.
//
// Helpers carry a pq prefix, for path quoting.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// pqSlashSeparated holds msg to naming path slash-separated: once converted
// with filepath.ToSlash, and with no separator doubled by quoting it. On
// POSIX this already holds, because ToSlash is the identity there and a
// native path has no backslash to double; the doubled form is a bug that
// only shows on a path that uses "\" as its separator.
func pqSlashSeparated(t *testing.T, msg, path string) {
	t.Helper()
	want := filepath.ToSlash(path)
	if !strings.Contains(msg, want) {
		t.Errorf("message %q does not name %q (slash-separated)", msg, want)
	}
	if strings.Contains(msg, `\\`) {
		t.Errorf("message %q doubles a separator: %q", msg, msg)
	}
}

// AC-3: the worktrees .gitignore refusal names the .gitignore slash-separated.
func TestAC3TheWorktreesGitignoreRefusalNamesItsPathSlashSeparated(t *testing.T) {
	main := clTempDir(t)
	err := EnsureWorktreesIgnored(main)
	if err == nil {
		t.Fatal("want a refusal: .sdlc/worktrees does not exist yet")
	}
	e := clError(t, err, sdlcerr.WorktreeNotMade)
	pqSlashSeparated(t, e.What, suIgnore(main))
}

// AC-3: a story held by another checkout names that checkout's path
// slash-separated, in both what happened and why.
func TestAC3TheHeldElsewhereRefusalNamesTheCheckoutSlashSeparated(t *testing.T) {
	checkout := clTempDir(t)
	err := HeldElsewhereError("US-001", Holder{Checkout: checkout, Live: true}, "a reason")
	e := clError(t, err, sdlcerr.StoryHeldElsewhere)
	pqSlashSeparated(t, e.What+" "+e.Why, checkout)
}

// AC-3: a second landing of the same story, refused while the first holds the
// lock, names the lock's own path slash-separated.
func TestAC3TheLandingLockRefusalNamesItsPathSlashSeparated(t *testing.T) {
	cacheDir(t)
	s := newStore(t)
	held, err := s.LockLanding("US-001")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(held.Release)

	_, err = s.LockLanding("US-001")
	e := clError(t, err, sdlcerr.StateUnwritable)

	project, perr := guardPath(s.LockRoot())
	if perr != nil {
		t.Fatal(perr)
	}
	pqSlashSeparated(t, e.Why, project+"-landing-US-001")
}

// AC-3's boundary, and THREATS.md's "Paths in messages: AC-3" -- a checkout
// path is read from a claim file in the git directory, so a path holding a
// quote or a newline must stay escaped once every separator is written only
// once. Making a path slash-separated by dropping strconv.Quote outright (as
// opposed to converting the separator first and quoting what is left) would
// pass every check above and still let a crafted path forge text in a
// refusal the model reads; this is what catches that.
func TestAC3APathsOwnQuoteAndNewlineStayEscaped(t *testing.T) {
	checkout := `/elsewhere/say "please" and` + "\n" + `look again`
	err := HeldElsewhereError("US-001", Holder{Checkout: checkout, Live: true}, "a reason")
	e := clError(t, err, sdlcerr.StoryHeldElsewhere)
	if strings.ContainsRune(e.Why, '\n') {
		t.Errorf("why = %q, has an unescaped newline from the checkout path", e.Why)
	}
	if !strings.Contains(e.Why, `\"please\"`) {
		t.Errorf("why = %q, want the checkout's own quotes escaped as \\\"please\\\"", e.Why)
	}
	if !strings.Contains(e.Why, `\n`) {
		t.Errorf("why = %q, want the checkout's own newline escaped as \\n", e.Why)
	}
}
