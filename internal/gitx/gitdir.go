package gitx

import (
	"os"
	"path/filepath"
	"strings"
)

// GitDirOf is the git directory of the checkout at path and the common
// directory of its repository, both canonical as `git rev-parse
// --absolute-git-dir` and `--git-common-dir` print them: a `.git` directory is
// its own; a `.git` file names a linked worktree's directory on its `gitdir:`
// line, relative to the checkout or absolute; and a `commondir` file inside a
// git directory names the repository's. A checkout whose `.git` cannot be read
// that way is an error, and the caller asks git.
//
// config.GitdirOf calls this: config may import a leaf, and a leaf never
// imports config (the contract's dependency direction).
func GitDirOf(path string) (gitDir, commonDir string, err error) {
	dotGit := filepath.Join(path, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return "", "", err
	}
	if info.IsDir() {
		gitDir = dotGit
	} else {
		raw, err := os.ReadFile(dotGit)
		if err != nil {
			return "", "", err
		}
		line, _, _ := strings.Cut(string(raw), "\n")
		p, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
		p = filepath.FromSlash(strings.TrimSpace(p))
		if !ok || p == "" {
			return "", "", &os.PathError{Op: "read", Path: dotGit, Err: os.ErrInvalid}
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(path, p)
		}
		gitDir = p
	}
	gitDir = realPath(gitDir)
	commonDir = gitDir
	if raw, err := os.ReadFile(filepath.Join(gitDir, "commondir")); err == nil { //nolint:gosec // G703: the directory the checkout's own .git names, read as git reads it
		p := filepath.FromSlash(strings.TrimSpace(firstLine(string(raw))))
		if p != "" {
			if !filepath.IsAbs(p) {
				p = filepath.Join(gitDir, p)
			}
			commonDir = realPath(p)
		}
	}
	if _, err := os.Stat(filepath.Join(gitDir, "HEAD")); err != nil { //nolint:gosec // G703: only asks whether what .git names has a HEAD; nothing is read or written
		// Whatever .git named, it is not a git directory git would read.
		return "", "", err
	}
	return gitDir, commonDir, nil
}

// realPath is p with its links resolved, as git prints a directory, or p
// cleaned where they cannot be.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// absPath is filepath.Abs.
func absPath(p string) (string, error) { return filepath.Abs(p) }
