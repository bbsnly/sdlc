//go:build !windows

package shellx

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// InGroup has cmd, not yet started, run in a process group of its own, and
// stopped with everything in that group: a test runner's children otherwise
// keep writing to the worktree after the command that started them was
// stopped.
func InGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = waitDelay
}

// joinGroup has nothing to do here: the process was made in its group.
func joinGroup(*exec.Cmd) error { return nil }

// leaveGroup has nothing to let go of here.
func leaveGroup(*exec.Cmd) {}
