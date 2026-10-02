package config

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// A Checkout is one working tree of a repository, and where the state every
// working tree of that repository shares is kept.
//
// Every linked worktree of a repository shares the main checkout's backlog and
// story records. Each worktree reading its own copy was two sessions working
// the same story, each from a backlog that said nobody else was.
type Checkout struct {
	// Root is this checkout.
	Root string
	// Main is the repository's main checkout, where the backlog and the story
	// records are kept. It is empty where FindCheckout refused.
	Main string
	// Common is the git directory every checkout of the repository shares, as
	// far as it could be read; empty where there is none.
	Common string
	// GitDir is this checkout's own git directory: Common in the main
	// checkout, <Common>/worktrees/<name> in a linked worktree.
	GitDir string

	pointer    bool
	linked     bool
	candidates []string
}

// FindCheckout finds the main checkout of the repository root is a checkout
// of, from git's own files: the .git entry, the commondir it names, and the
// common configuration. Not from git: the hook asks on every tool call, and
// GIT_DIR and GIT_COMMON_DIR, which git would honour, are exactly what a
// session would set to point a worktree at a main checkout of its own making.
//
// The checkout is filled as far as it could be read, even when the main
// checkout cannot be established: what the hook protects is worked out from
// that, and a refusal has to leave the protection standing.
func FindCheckout(root string) (Checkout, error) {
	root = canonical(root)
	c := Checkout{Root: root}
	dotgit := filepath.Join(root, ".git")
	info, found := stat(dotgit)
	switch {
	case !found:
		// No repository at all is the tests' own layout, and a directory the
		// store has always treated as one checkout.
		c.Main = root
		return c, nil
	case info.IsDir():
		c.Main, c.Common, c.GitDir = root, canonical(dotgit), canonical(dotgit)
		c.candidates = []string{c.Common}
		return c, nil
	}
	c.pointer = true
	gitdir, ok := readGitdir(dotgit, root)
	if !ok {
		return c, noMainCheckout("its .git file does not say where its git directory is")
	}
	if !isDir(gitdir) {
		return c, noMainCheckout("its .git file leads to " + sdlcerr.QuotePath(gitdir) + ", which is not a directory")
	}
	c.GitDir = gitdir
	common, found := readCommondir(gitdir)
	if !found {
		// The main checkout of a repository made with --separate-git-dir, or a
		// submodule: this checkout is its repository's only one.
		c.Main, c.Common = root, gitdir
		c.candidates = []string{gitdir}
		return c, nil
	}
	c.linked = true
	if !filepath.IsAbs(common) {
		common = filepath.Join(gitdir, common)
	}
	common = canonical(common)
	if isDir(common) {
		c.Common = common
		c.candidates = append(c.candidates, common)
	}
	// The directory git keeps this worktree's own files in is inside the
	// common one, whatever commondir says: a rewritten commondir must not take
	// the protection of the real one with it.
	if parent := filepath.Dir(gitdir); filepath.Base(parent) == "worktrees" {
		if owner := filepath.Dir(parent); isDir(owner) && !slices.ContainsFunc(c.candidates, func(p string) bool { return SamePath(p, owner) }) {
			c.candidates = append(c.candidates, owner)
		}
	}

	switch {
	case c.Common == "":
		return c, noMainCheckout("it is a linked worktree whose commondir leads to " + sdlcerr.QuotePath(common) +
			", which is not a directory")
	case bare(common):
		return c, noMainCheckout("it is a linked worktree of a bare repository: core.bare is true in " +
			sdlcerr.QuotePath(filepath.Join(common, "config")) + ", so no checkout's .git is the repository's git directory")
	}
	main := filepath.Dir(common)
	here, errHere := os.Stat(filepath.Join(main, ".git"))
	shared, errShared := os.Stat(common)
	if filepath.Base(common) != ".git" || !SamePath(filepath.Dir(gitdir), filepath.Join(common, "worktrees")) ||
		errHere != nil || errShared != nil || !os.SameFile(here, shared) {
		return c, noMainCheckout("it is a linked worktree whose git directory " + sdlcerr.QuotePath(common) +
			" is not the .git directory of a main checkout, as in a repository made with --separate-git-dir " +
			"or a submodule's worktree")
	}
	c.Main = main
	return c, nil
}

func noMainCheckout(why string) error {
	return sdlcerr.New(sdlcerr.NoMainCheckout,
		"this checkout's repository has no main checkout to keep the backlog and the story records in",
		"this checkout is not the repository's main checkout, and the main checkout cannot be found from "+
			"git's own files: "+why)
}

// Linked reports whether this checkout is a linked worktree of its repository.
func (c Checkout) Linked() bool { return c.linked }

// Pointer reports whether this checkout's .git is a file naming its git
// directory elsewhere: a linked worktree, a submodule, or the main checkout of a
// repository made with --separate-git-dir.
func (c Checkout) Pointer() bool { return c.pointer }

// Candidates are the directories that may be the repository's common git
// directory: the one commondir names, and the one git keeps this worktree's
// own files in. Unlike FindCheckout they are not checked against core.bare or
// the shape of a main checkout, because they say what the hook protects, and a
// session held in a worktree could otherwise set core.bare, write the main
// checkout's records while nothing protected them, and unset it again.
//
// readable is false only for a .git file from which not one candidate could be
// read.
func (c Checkout) Candidates() (dirs []string, readable bool) {
	return c.candidates, len(c.candidates) > 0 || !c.pointer
}

// HasWorktrees reports whether the repository has a linked worktree, as git
// counts them: an entry under the common directory's worktrees, whether or not
// the checkout it names is still there.
func (c Checkout) HasWorktrees() bool {
	if c.Common == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(c.Common, "worktrees"))
	return err == nil && len(entries) > 0
}

// Worktrees is every linked worktree git has an entry for, in the order of the
// entries' names, prunable ones included. Each entry's gitdir file names the
// worktree's .git; one written relative, as worktree.useRelativePaths writes it,
// is relative to the entry's own directory.
func (c Checkout) Worktrees() []string { return c.WorktreesUpTo(0) }

// WorktreesUpTo is Worktrees, reading no more than limit entries that name a
// worktree; 0 is every one. The hook asks on every tool call, and reading every
// entry before keeping the first limit cost a read per entry git had kept.
func (c Checkout) WorktreesUpTo(limit int) []string {
	if c.Common == "" {
		return nil
	}
	dir := filepath.Join(c.Common, "worktrees")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if limit > 0 && len(out) == limit {
			break
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name(), "gitdir"))
		if err != nil {
			continue
		}
		p := filepath.FromSlash(strings.TrimSpace(string(raw)))
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, e.Name(), p)
		}
		out = append(out, canonical(filepath.Dir(p)))
	}
	return out
}

// SamePath reports whether a and b are the same directory or file: the same
// file on disk where both exist, and otherwise the same path, compared as the
// filesystem compares names.
func SamePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if x, err := os.Stat(a); err == nil {
		if y, err := os.Stat(b); err == nil {
			return os.SameFile(x, y)
		}
	}
	return foldPath(a) == foldPath(b)
}

// PathKey is p as a key that two spellings of the same directory share, for a
// set of paths: comparing each with SamePath stats both sides of every pair,
// which grew with the square of the worktrees.
func PathKey(p string) string {
	if p == "" {
		return ""
	}
	return foldPath(canonical(p))
}

func foldPath(p string) string {
	p = filepath.ToSlash(filepath.Clean(p))
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return pathrules.Fold(p)
	}
	return p
}

// canonical is p absolute, cleaned and, where it exists, with its links
// resolved, in the platform's own separators.
func canonical(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

// readGitdir reads a .git file, which names the checkout's git directory,
// relative to the checkout where it is not absolute.
func readGitdir(dotgit, root string) (string, bool) {
	raw, err := os.ReadFile(dotgit)
	if err != nil {
		return "", false
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	p, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	p = filepath.FromSlash(strings.TrimSpace(p))
	if !ok || p == "" {
		return "", false
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	return canonical(p), true
}

// GitdirOf is the git directory a .git file names, and whether it names one.
func GitdirOf(dotgit string) (string, bool) {
	return readGitdir(dotgit, filepath.Dir(dotgit))
}

// stat is what is at p, and whether anything could be read there.
func stat(p string) (os.FileInfo, bool) {
	info, err := os.Stat(p)
	return info, err == nil
}

// readCommondir is where a worktree's commondir file says the common git
// directory is, as it is written, and whether the git directory has one.
func readCommondir(gitdir string) (string, bool) {
	raw, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	if err != nil {
		return "", false
	}
	return filepath.FromSlash(strings.TrimSpace(string(raw))), true
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// bare reports whether the repository's configuration sets core.bare. It reads
// the one setting the way git does for the common spellings, and nothing else:
// no includes, which git itself leaves out of a worktree's view of the setting
// only when they are conditional, and which would need all of git's reader.
func bare(common string) bool {
	raw, err := os.ReadFile(filepath.Join(common, "config"))
	if err != nil {
		return false
	}
	// Git keeps the last value it reads, across every [core] section. Stopping
	// at the first one read "bare = false" followed by "bare = true" as not
	// bare, where git itself calls the repository bare.
	isBare, section := false, ""
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			name, rest, _ := strings.Cut(strings.TrimPrefix(line, "["), "]")
			section = strings.ToLower(strings.TrimSpace(name))
			// Git takes a setting on the header's own line: "[core] bare = true".
			line = strings.TrimSpace(rest)
		}
		if line == "" || line[0] == '#' || line[0] == ';' || section != "core" {
			continue
		}
		key, value, hasValue := strings.Cut(line, "=")
		if !strings.EqualFold(strings.TrimSpace(key), "bare") {
			continue
		}
		isBare = !hasValue || gitBool(value)
	}
	return isBare
}

// gitBool is a boolean value as git reads it, comment and quotes taken off. A
// value git refuses to read as a boolean is false here: git stops on it, and
// what the main checkout is then is not this reader's to guess.
func gitBool(value string) bool {
	value, _, _ = strings.Cut(value, "#")
	value, _, _ = strings.Cut(value, ";")
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), `"`))
	switch value {
	case "true", "yes", "on":
		return true
	}
	n, err := strconv.Atoi(value)
	return err == nil && n != 0
}
