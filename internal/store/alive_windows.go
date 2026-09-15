package store

import (
	"errors"
	"math"
	"syscall"
)

// processQueryLimitedInformation and stillActive are
// PROCESS_QUERY_LIMITED_INFORMATION and STILL_ACTIVE, which package syscall
// does not name.
const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

// processAlive reports whether pid is a running process.
//
// Opening it is not enough to say so. A process that has exited stays openable
// for as long as anything holds a handle to it -- an antivirus scan, a parent
// that has not closed its own -- and was taken for running, so the lock it left
// was waited out rather than broken. Its exit code says whether it is still
// going. A process this user may not open is somebody's, and is running.
func processAlive(pid int) bool {
	if pid <= 0 || pid > math.MaxUint32 {
		return false
	}
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.ERROR_ACCESS_DENIED)
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}
