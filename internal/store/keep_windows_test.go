package store

import (
	"syscall"
	"testing"
)

// keepPID holds a handle to the process with pid until the test ends, which
// keeps Windows from giving its pid to another process.
func keepPID(t *testing.T, pid int) {
	t.Helper()
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(h) })
}
