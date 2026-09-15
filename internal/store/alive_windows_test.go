package store

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// A process that has exited stays openable while anything holds a handle to
// it, and finding it open was taken for finding it running: the lock such a
// process left was waited out instead of broken, and on a busy runner the test
// that breaks one saw its dead holder alive.
func TestAProcessThatHasExitedIsNotRunningWhileAHandleToItIsOpen(t *testing.T) {
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	// cmd holds the process open until Wait, so this cannot miss it.
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}

	if processAlive(pid) {
		t.Errorf("pid %d has exited, and was reported running because a handle to it is still open", pid)
	}
	if !processAlive(os.Getpid()) {
		t.Error("this process was reported not running")
	}
}
