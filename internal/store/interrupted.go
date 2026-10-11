package store

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// interruptedWrite is the name of the file a write goes to before it is renamed
// over the one it replaces: the file's own name behind a dot, then .tmp and the
// digits os.CreateTemp puts in place of its star.
var interruptedWrite = regexp.MustCompile(`^\..+\.tmp[0-9]+$`)

// ClearInterruptedWrites removes the files that writes killed part-way left
// behind. A write that finishes renames its file away, and one that fails
// removes it, but a process killed in between does neither. What it leaves is
// a file nobody reads, and one the commit gate counts as uncommitted work.
//
// The caller holds the project's lock. Every write this looks for is made
// under it, so none of them can still be in progress.
func (s *Store) ClearInterruptedWrites() {
	dirs := []string{filepath.Join(s.root, filepath.FromSlash(stateDir))}
	shared, err := s.shared()
	if err == nil {
		// The records are the main checkout's. A worktree's own copy of them,
		// checked out by git, is nobody's to sweep.
		dirs = append(dirs, filepath.Join(shared, filepath.FromSlash(storiesDir)))
	}
	if claims, ok := s.claimsPath(); ok {
		// In the git directory, which is no checkout's: the leaves check below
		// is about links out of a checkout, and this is not in one.
		removeInterruptedIn(claims)
	}
	for _, root := range dirs {
		// Where a link out of the repository leads, the files are not the
		// loop's, whatever they are called.
		if s.leaves(root) {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && interruptedWrite.MatchString(d.Name()) {
				removeLeftover(path)
			}
			return nil
		})
	}

	if err == nil {
		s.clearInterruptedBacklog(shared)
	}
}

// removeInterruptedIn removes the leftovers of interrupted writes directly in dir.
func removeInterruptedIn(dir string) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if interruptedWrite.MatchString(e.Name()) {
			removeLeftover(filepath.Join(dir, e.Name()))
		}
	}
}

// clearInterruptedBacklog looks for a write of the backlog itself. The backlog
// is the user's file, in a directory that is theirs, so nothing else there is
// looked for.
func (s *Store) clearInterruptedBacklog(shared string) {
	backlog := s.cfg.BacklogPath(shared)
	if resolved, err := filepath.EvalSymlinks(backlog); err == nil {
		backlog = resolved
	}
	entries, _ := os.ReadDir(filepath.Dir(backlog))
	for _, e := range entries {
		if InterruptedWriteOf(e.Name(), filepath.Base(backlog)) {
			removeLeftover(filepath.Join(filepath.Dir(backlog), e.Name()))
		}
	}
}

// InterruptedWriteOf reports whether name is what a write of the file named
// file leaves behind when it is killed.
func InterruptedWriteOf(name, file string) bool {
	return strings.HasPrefix(name, "."+file+".tmp")
}

func removeLeftover(path string) {
	if err := os.Remove(path); err != nil {
		slog.Debug("could not remove what an interrupted write left", "path", path, "err", err)
	}
}
