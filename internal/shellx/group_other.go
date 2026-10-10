//go:build !windows

package shellx

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
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
	cmd.Cancel = func() error { return killGroup(cmd.Process.Pid) }
	cmd.WaitDelay = waitDelay
}

// groupPatience bounds how long a stop goes on killing a group that still
// answers, and groupPause is how long it leaves between two kills. A group is
// gone within a pause or two; the bound is for one whose dead are never
// collected, which on Linux goes on answering.
const (
	groupPatience = time.Second
	groupPause    = time.Millisecond
)

// killGroup kills the group pgid leads, and kills it again until nothing in it
// is left to kill. One kill is not enough on macOS: a process whose fork was
// under way when the signal went to the group is not signalled, and its child
// lives on in the group with nobody to stop it. An interrupted `sdlc parallel`
// left a session's tool running that way, about once in twenty runs of its
// test and most runs on a loaded machine. The child is in the group by the
// time its parent has died, so the next kill finds it.
//
// It stops at the first kill the system refuses, and does not signal the group
// again after that: ESRCH is a group with nothing in it, on either system, and
// EPERM is what macOS answers for one that holds only the dead, where Linux
// answers as for the living until they are collected. An emptied group's
// number can be given to another, so it is never kept.
func killGroup(pgid int) error {
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	killed := true
	for until := time.Now().Add(groupPatience); killed && time.Now().Before(until); {
		time.Sleep(groupPause)
		killed = syscall.Kill(-pgid, syscall.SIGKILL) == nil
	}
	return nil
}

// joinGroup has nothing to do here: the process was made in its group.
func joinGroup(*exec.Cmd) error { return nil }

// leaveGroup has nothing to let go of here.
func leaveGroup(*exec.Cmd) {}
