//go:build !windows

package fsx

// The named pipe is a file kind of systems other than Windows, where
// OpenFlags is written; Windows has none to plant at a path.

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestAC2ANamedPipeAtTheNameIsRefusedWithoutWaitingForAWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitignore")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("this file system makes no named pipe: %v", err)
	}
	// An open that waited on the pipe would hang here, with no writer ever
	// coming, and the test would time out rather than pass.
	f, _, err := OpenRegular(path)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, ErrNotRegular) {
		t.Errorf("OpenRegular of a named pipe is %v, want ErrNotRegular", err)
	}
}

func TestAC2TheOpenNeverFollowsALinkAndNeverWaitsOnAPipe(t *testing.T) {
	if OpenFlags&syscall.O_NOFOLLOW == 0 {
		t.Errorf("OpenFlags lacks O_NOFOLLOW: a link swapped in after the Lstat would be followed")
	}
	if OpenFlags&syscall.O_NONBLOCK == 0 {
		t.Errorf("OpenFlags lacks O_NONBLOCK: a pipe swapped in after the Lstat would hold the open")
	}
	// A link planted between the Lstat and the open is what O_NOFOLLOW is
	// for; opened with the flags directly, the link is refused by the system.
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if f, err := os.OpenFile(link, os.O_RDONLY|OpenFlags, 0); err == nil {
		_ = f.Close()
		t.Errorf("a link opened with OpenFlags was followed")
	}
}
