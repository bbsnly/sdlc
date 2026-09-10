// Package fsx holds the file operations the loop needs to be careful about.
//
// It returns ordinary errors. The packages whose output a user reads wrap these
// into an sdlcerr with a code and a fix; the operation itself has no opinion
// about how the failure should be explained.
package fsx

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path so that a reader sees either the previous
// contents or the new ones, never a mixture.
//
// The temporary file goes in the target's own directory, not the system
// temporary directory: a rename across filesystems is a copy, and a copy is not
// atomic. Missing parent directories are created.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	tmp := f.Name()
	// Removing the temporary file is unconditional: after a successful rename
	// it is no longer there, and Remove on a missing file is not an error worth
	// reporting.
	defer func() { _ = os.Remove(tmp) }()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("flush %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, perm); err != nil {
		return fmt.Errorf("set permissions on %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("move %s into place at %s: %w", tmp, path, err)
	}
	return nil
}

// Exists reports whether path is there. It says nothing about whether it can be
// read: callers that care read it and handle the error.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
