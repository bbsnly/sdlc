//go:build !windows

package store

import (
	"errors"
	"os"
	"syscall"
)

// processAlive reports whether pid is a running process. Signal 0 checks
// without delivering anything; a process that belongs to someone else is
// refused the signal, and is still running.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
