package store

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// worktreesDir is where `sdlc start` makes each new story's worktree, in the
// main checkout: inside the repository, so that a Claude Code session opened
// there may edit it without asking, and under .sdlc, which `go ./...` and the
// like pass over.
const worktreesDir = config.Dir + "/worktrees"

// StoryBranch is the branch a story's worktree is made on.
func StoryBranch(id string) string { return "story/" + id }

// StoryWorktree is where the story's worktree is made: .sdlc/worktrees/<ID> in
// the main checkout, whichever checkout asks.
func (s *Store) StoryWorktree(id string) (string, error) {
	if err := CheckID(id); err != nil {
		return "", err
	}
	co, err := s.checkout()
	if err != nil {
		return "", err
	}
	return filepath.Join(co.Main, filepath.FromSlash(worktreesDir), id), nil
}

// LockStory takes the lock one start holds on a story from making the story's
// worktree to recording the story as begun there. That is outside the
// project's lock, which the smoke check in between would hold for longer than
// every other command waits for it; and without this, of two starts that
// picked the same story at once, the second found the first one's worktree in
// its way before the first had claimed the story, and refused. The second now
// waits here, and then finds the story held.
//
// It is the project's lock, keyed on the story's worktree rather than the
// project: a directory in the user's cache while it is held, and nothing on
// disk once it is let go.
func (s *Store) LockStory(id string) (*Guard, error) {
	dir, err := s.StoryWorktree(id)
	if err != nil {
		return nil, err
	}
	return Lock(dir, "start "+id)
}

// StoryWorktreeOf is the story whose worktree this checkout is, where it is
// one: a linked worktree at .sdlc/worktrees/<ID> of its main checkout. A
// worktree a person made elsewhere, or named for no story, is none.
func (s *Store) StoryWorktreeOf() (string, bool) {
	co, err := s.checkout()
	if err != nil || !co.Linked() {
		return "", false
	}
	id := filepath.Base(co.Root)
	if CheckID(id) != nil || !config.SamePath(filepath.Dir(co.Root), filepath.Join(co.Main, filepath.FromSlash(worktreesDir))) {
		return "", false
	}
	return id, true
}

// IsStoryWorktree reports whether dir is the worktree `sdlc start` makes for
// the story id. A worktree made by hand, wherever it is, is not.
func (s *Store) IsStoryWorktree(dir, id string) bool {
	wt, err := s.StoryWorktree(id)
	return err == nil && config.SamePath(wt, dir)
}

// At opens another checkout of the same repository, with the configuration
// that checkout reads and this store's clock, so that everything one command
// writes carries one time.
func (s *Store) At(root string) (*Store, error) {
	cfg, err := config.Load(root)
	if err != nil {
		return nil, err
	}
	out := New(&config.Project{Root: root, Config: cfg})
	out.now = s.now
	return out, nil
}

// worktreesIgnore is the file that keeps the story worktrees out of the main
// checkout's `git status`.
const worktreesIgnore = worktreesDir + "/.gitignore"

// EnsureWorktreesIgnored keeps the story worktrees in main out of the main
// checkout's `git status` and `git add -A`, which would otherwise take each
// one in as an embedded repository. It writes .sdlc/worktrees/.gitignore
// holding `*`, which hides the directory and itself, and changes neither the
// project's own .gitignore nor .git/info/exclude, which are a person's.
//
// The file is created only where there is none, and only in a directory that
// is really there rather than a link to one elsewhere. One already there is
// never rewritten: where it does not hide everything in the directory, this
// says so, and a worktree is not left in view without a word.
func EnsureWorktreesIgnored(main string) error {
	dir := filepath.Join(main, filepath.FromSlash(worktreesDir))
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return notIgnored(main, "it is not a directory in the main checkout")
	}
	path := filepath.Join(main, filepath.FromSlash(worktreesIgnore))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	switch {
	case err == nil:
		_, werr := f.WriteString("*\n")
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return notIgnored(main, "it could not be written: "+werr.Error())
		}
		return nil
	case errors.Is(err, fs.ErrExist):
		return CheckWorktreesIgnored(main)
	}
	return notIgnored(main, "it could not be written: "+err.Error())
}

// CheckWorktreesIgnored says whether a .sdlc/worktrees/.gitignore already in
// main fails to hide everything in the directory. None there yet is no
// problem: EnsureWorktreesIgnored writes one.
func CheckWorktreesIgnored(main string) error {
	// Not through a link, or into a file: making the worktree refuses both,
	// and says so.
	if !realDirectory(filepath.Join(main, filepath.FromSlash(worktreesDir))) {
		return nil
	}
	path := filepath.Join(main, filepath.FromSlash(worktreesIgnore))
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return notIgnored(main, "it could not be read: "+err.Error())
	case !info.Mode().IsRegular():
		return notIgnored(main, "it is not a file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return notIgnored(main, "it could not be read: "+err.Error())
	}
	if !hidesAll(raw) {
		return notIgnored(main, `it has no line "*" that hides everything in the directory, or a line starting "!" `+
			"shows something again")
	}
	return nil
}

// realDirectory reports whether path is a directory itself, not a link to one.
func realDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir()
}

// hidesAll reports whether a .gitignore hides everything in its directory: a
// pattern that matches every name, and none starting with ! to show one again.
func hidesAll(raw []byte) bool {
	all := false
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "!"):
			return false
		case line == "*" || line == "/*" || line == "**":
			all = true
		}
	}
	return all
}

func notIgnored(main, why string) error {
	return sdlcerr.New(sdlcerr.WorktreeNotMade,
		"the story's worktree could not be kept out of the main checkout's git status: "+
			quote(filepath.Join(main, filepath.FromSlash(worktreesIgnore)))+" does not hide it",
		why+", and without it git status and git add -A in the main checkout take in every story worktree").
		WithFix(`in the main checkout, put a line "*" in ` + worktreesIgnore + ` and nothing that starts with "!", ` +
			`or remove the file, then start again; an assistant stops and tells the person`)
}
