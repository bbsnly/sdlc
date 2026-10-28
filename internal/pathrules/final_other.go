//go:build !windows

package pathrules

import (
	"errors"
	"runtime"
	"syscall"
)

// finalPath is followed by filepath.EvalSymlinks everywhere but Windows, where
// a junction is a link it does not follow.
func finalPath(string) (string, bool) { return "", false }

// normalise is p, which has no symlink on it, spelled as resolve would spell
// it: as it is, here.
func normalise(p string) string { return p }

// lookupLimit reports whether err is a lookup that passed the system's own
// limit on the links one lookup follows: ELOOP, from 33 links on macOS and 41
// on Linux.
func lookupLimit(err error) bool { return errors.Is(err, syscall.ELOOP) }

// limitReadsAsNotThere is false: ELOOP is what both systems give a lookup past
// their limit, whether the name at its end is there or not.
const limitReadsAsNotThere = false

// lookupLinks is the most links one lookup follows: 32 on macOS and 40 on
// Linux. Only a test that gives lstat Windows' answer reaches it here.
func lookupLinks() int {
	if runtime.GOOS == "darwin" {
		return 32
	}
	return 40
}

// pathTooLong reports whether err is a lookup of a path longer than the system
// looks up in one go: ENAMETOOLONG, past 1,024 bytes on macOS and 4,096 on
// Linux. The system gives it for a name past nameMax too, which lstat tells
// apart with nameOverLimit.
func pathTooLong(err error) bool { return errors.Is(err, syscall.ENAMETOOLONG) }
