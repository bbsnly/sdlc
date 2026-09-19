//go:build !windows

package store

import (
	"os"
	"testing"
)

func TestProcessAliveReportsThisProcessAsRunning(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Error("processAlive said the running test process is not alive")
	}
}

// A process that has exited: signal 0 fails, and processAlive says so.
func TestProcessAliveIsFalseForAProcessThatHasExited(t *testing.T) {
	if processAlive(deadPID(t)) {
		t.Error("processAlive said a process that has exited is still running")
	}
}
