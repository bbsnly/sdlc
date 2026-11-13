//go:build windows

package main

import "os"

const (
	cpuNote = " (the process's own: Windows does not count its children)"
	rssNote = ": not reported on Windows, where the process's peak working set is not in what wait reports"
)

// peakRSS is not reported on Windows: os.ProcessState carries the process's
// times and nothing of its memory.
func peakRSS(*os.ProcessState) (int64, bool) { return 0, false }
