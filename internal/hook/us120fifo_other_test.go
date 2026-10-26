//go:build !unix

package hook

import "testing"

// u120Fifo skips: outside Unix a named pipe is not a path in the file system
// an archive word can name, which the row is about.
func u120Fifo(t *testing.T, _ string) {
	t.Helper()
	t.Skip("a named pipe is not a file-system path on this platform, which the row is about")
}
