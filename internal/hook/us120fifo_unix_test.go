//go:build unix

package hook

import (
	"syscall"
	"testing"
)

// u120Fifo makes a named pipe at path, or skips where the file system will not
// hold one.
func u120Fifo(t *testing.T, path string) {
	t.Helper()
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("this file system would not make a named pipe, which the row is about: %v", err)
	}
}
