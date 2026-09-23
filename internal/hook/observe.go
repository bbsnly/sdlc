package hook

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sync"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/store"
)

// gitBudget is the one deadline for git on a shell command's path: every git
// call the hook makes for the command -- measuring the tree for each commit it
// spells, and writing down the head a commit is made on -- shares it.
//
// It starts at the first git call, not when the hook began. The command's own
// parse runs first and is the attacker's to make expensive (US-004 measured 121
// KB of PowerShell at 29.2 s inside the hook), and the freshness check fails
// open when it cannot measure the tree: a deadline taken at the start handed
// the parse the check's budget, and an expired check is a commit allowed with
// stale reviews and a person's pre-commit approval unchecked.
//
// And it is one, not one per caller. A deadline of its own for each commit a
// line spells put the worst case at that many times treeTimeout -- two is 40 s
// against the 30 s hooks.json gives the hook -- and a hook Claude Code kills
// says nothing, so the commit went through unchecked. What is left: parse work
// between two commits in one line spends the second one's share, so its check
// can fail open. The first check answered the same question of the same tree,
// and its refusal stops the whole command.
//
// The clock is a parameter because a deadline is tested by moving time, not
// by waiting for it.
func gitBudget(parent context.Context, now func() time.Time) (func() context.Context, context.CancelFunc) {
	base, cancelBase := context.WithCancel(parent)
	var once sync.Once
	ctx, cancelDeadline := base, context.CancelFunc(func() {})
	budget := func() context.Context {
		once.Do(func() { ctx, cancelDeadline = context.WithDeadline(base, now().Add(treeTimeout)) })
		return ctx
	}
	return budget, func() {
		cancelDeadline()
		cancelBase()
	}
}

// observe writes down the head a commit the hook has just allowed is about to
// be made on, so that the commit gate can tell the story's commits from
// anybody else's (THREATS N1). It runs only for a command Inspect allowed, and
// only when the classification said the command commits now.
//
// It is on the fast path of every allowed commit, so it holds four properties:
// its git calls share the command's budget rather than taking one of their
// own; it takes no lock, because an observation is one file written only when
// it is absent; it runs after Inspect has allowed, so a refused commit leaves
// nothing behind; and every failure is a debug line and nothing else. A
// recorder that could stop a commit would be the loop refusing finished work
// over its own note to itself.
//
// One allowed command plants one observation, of the head before it runs. A
// command that commits twice -- `git commit -m a && git commit -m b` in one
// call -- leaves its second commit's parent unwatched, and the range the
// commit gate works out stops short of it.
func observe(project, story string, budget func() context.Context) {
	head, err := gitx.Head(budget(), project)
	if err != nil || head == gitx.NoCommits {
		slog.Debug("hook could not read the head a commit is made on", "err", err)
		return
	}
	// Writing down a head needs the repository and nothing in the
	// configuration.
	s := store.New(&config.Project{Root: project})
	if err := s.ObserveCommit(budget(), story, head); err != nil {
		slog.Debug("hook could not write down the head a commit is made on", "head", head, "err", err)
	}
}

// nestedRepository reports whether dir, repository-relative and
// slash-separated, is inside a repository of its own within the project: a
// submodule, or a fixture a test made. git commits there to that repository, and
// writing down this project's HEAD for it planted an observation a person's next
// commit here was recorded on. It is asked only for a commit the hook allows,
// and a directory it cannot read is taken as nested, because the cost of that
// is one observation and the cost of the other answer is a person's commit in
// the story's range.
func nestedRepository(project, dir string) bool {
	for d := path.Clean(dir); d != "." && d != "/" && d != ".."; d = path.Dir(d) {
		_, err := os.Lstat(filepath.Join(project, filepath.FromSlash(d), ".git"))
		if err == nil || !errors.Is(err, fs.ErrNotExist) {
			return true
		}
	}
	return false
}
