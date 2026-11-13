//go:build !windows

package main

import (
	"os"
	"runtime"
	"syscall"
)

const (
	cpuNote = ", with its children's"
	rssNote = ""
)

// peakRSS is the process's peak resident set in kilobytes, from what wait4
// reported. Darwin reports it in bytes and every other Unix in kilobytes.
func peakRSS(ps *os.ProcessState) (int64, bool) {
	ru, ok := ps.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0, false
	}
	kb := int64(ru.Maxrss) //nolint:unconvert // Maxrss is int32 on 32-bit Linux
	if runtime.GOOS == "darwin" {
		kb /= 1024
	}
	return kb, true
}
