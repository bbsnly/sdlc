//go:build !windows

package hook

import (
	"os"
	"syscall"
)

// oneName reports whether a file has one name and no more. Where that cannot
// be told, it is not known to.
func oneName(_ string, info os.FileInfo) bool {
	// Compared, not converted: the count is 16 bits wide on macOS and 32 or 64
	// on Linux, so no one conversion is needed on every platform, and the
	// linter refuses one that is not.
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}

// namedAsLooked reports whether the disk looks for a path under the names it
// is written with. Windows is where it does not.
func namedAsLooked(string) bool {
	return true
}
