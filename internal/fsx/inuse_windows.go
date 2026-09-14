//go:build windows

package fsx

import (
	"errors"
	"syscall"
)

// errorSharingViolation is ERROR_SHARING_VIOLATION, which package syscall does
// not name.
const errorSharingViolation syscall.Errno = 32

// inUse reports whether err is Windows refusing to replace a file because
// another process has it open.
func inUse(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) &&
		(errno == syscall.ERROR_ACCESS_DENIED || errno == errorSharingViolation)
}
