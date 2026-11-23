//go:build !windows

package fsx

import (
	"io/fs"
	"os"
	"syscall"
)

// OpenFlags keep an open from following a link swapped in after the Lstat,
// and from waiting on a named pipe swapped in there: an open of a pipe with
// no reader fails at once, and one that opens fails the descriptor check.
// Neither changes anything for a regular file.
const OpenFlags = syscall.O_NOFOLLOW | syscall.O_NONBLOCK

// OneName reports whether the open file has this name and no other.
func OneName(_ *os.File, fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}
