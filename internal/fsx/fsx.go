// Package fsx holds the file operations the loop needs to be careful about.
//
// It returns ordinary errors. The packages whose output a user reads wrap these
// into an sdlcerr with a code and a fix; the operation itself has no opinion
// about how the failure should be explained.
package fsx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// WriteFileAtomic writes data to path so that a reader sees either the previous
// contents or the new ones, never a mixture.
//
// The temporary file goes in the target's own directory, not the system
// temporary directory: a rename across filesystems is a copy, and a copy is not
// atomic. Missing parent directories are created.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	_, err := WriteFileAtomicInfo(path, data, perm)
	return err
}

// WriteFileAtomicInfo is WriteFileAtomic, and says which file it put at path:
// the temporary file's FileInfo, whose identity, size and modification time
// the rename carries to path. A caller that keeps what it wrote tells by it
// whether path still holds that file.
//
// It is taken from the open handle, once the data is flushed, and not from
// the path afterwards: a stat of the path describes whatever is there by
// then, which may be another writer's file, and on Windows a FileInfo made
// from a path loads its identity later still, at its first comparison. It is
// nil where the handle could not be asked, which fails nothing: the write
// itself is whole.
func WriteFileAtomicInfo(path string, data []byte, perm fs.FileMode) (fs.FileInfo, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: an ordinary directory other tools and the user read, as MkdirAll's callers expect
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}

	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return nil, fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	tmp := f.Name()
	// Removing the temporary file is unconditional: after a successful rename
	// it is no longer there, and Remove on a missing file is not an error worth
	// reporting.
	defer func() { _ = os.Remove(tmp) }()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("flush %s: %w", tmp, err)
	}
	info, _ := f.Stat()
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return nil, fmt.Errorf("set permissions on %s: %w", tmp, err)
	}
	if err := Replace(tmp, path); err != nil {
		return nil, fmt.Errorf("move %s into place at %s: %w", tmp, path, err)
	}
	return info, nil
}

// How long a replacement waits for a reader to let go of the file it replaces.
// Only Windows makes it wait: a file another process has open cannot be renamed
// over there, and the hook reads the loop's state on every tool call. A command
// writing that state met a reader sooner or later, and failed part-way through
// the files it was changing.
const (
	replaceWait = 2 * time.Second
	replacePoll = 10 * time.Millisecond
)

// Replace renames tmp over path, waiting out a reader that has path open. A
// rename replaces whatever stands at path, a link or a pipe among them, and
// never writes through it.
func Replace(tmp, path string) error {
	deadline := time.Now().Add(replaceWait)
	for {
		err := os.Rename(tmp, path)
		if err == nil || !inUse(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(replacePoll)
	}
}

// ErrNotRegular is a path that is not a regular file, or that changed between
// its Lstat and its open.
var ErrNotRegular = errors.New("not a regular file")

// OpenRegular opens path to read only where it is a regular file, and returns
// it with what the open descriptor is. It never follows a link and never waits
// on a named pipe (OpenFlags), and it keeps the descriptor only where it is the
// file the Lstat saw: a link to /dev/zero or to a pipe, or one swapped in
// between the two, is refused rather than read.
func OpenRegular(path string) (*os.File, fs.FileInfo, error) {
	seen, err := os.Lstat(path)
	if err != nil {
		return nil, nil, err
	}
	if !seen.Mode().IsRegular() {
		return nil, nil, ErrNotRegular
	}
	f, err := os.OpenFile(path, os.O_RDONLY|OpenFlags, 0)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() || !os.SameFile(seen, fi) {
		_ = f.Close()
		return nil, nil, ErrNotRegular
	}
	return f, fi, nil
}

// Exists reports whether path is there. It says nothing about whether it can be
// read: callers that care read it and handle the error.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
