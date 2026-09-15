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
	for _, dir := range []string{stateDir, storiesDir} {
		root := filepath.Join(s.root, filepath.FromSlash(dir))
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

	// The backlog is the user's file, in a directory that is theirs, so only a
	// write of the backlog itself is looked for there.
	backlog := s.cfg.BacklogPath(s.root)
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
