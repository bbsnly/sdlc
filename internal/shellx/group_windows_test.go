//go:build windows

package shellx

// Gate 3 reopened for US-029.3's rework: group_windows.go's job-object
// mechanism has no test on any platform that builds it, since this machine
// (and CI's coverage run) is not Windows -- cover-g7.out's evidence for it is
// GOOS=windows go vet alone. This file is the Windows-only half; it mirrors
// group_test.go's fixtures, through a job instead of a POSIX process group,
// since Command still runs everything through sh or bash (Git Bash on
// Windows), which InGroup then puts in a job of its own.
//
// "It will not run here": there is no Windows host in this loop's own CI
// either (see rework-g7.txt's "Not hand-run" note). It is read, not run,
// until one exists; GOOS=windows go vet and go build hold it to compiling.
//
// Helpers carry an rw prefix, for rework.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

// rwPatience bounds how long a test waits for a pipe to close before it says
// the process that holds it was not stopped. It is a deadline, not a pause:
// nothing waits for it when the job is gone.
const rwPatience = 20 * time.Second

// rwJobStarted starts, in a job of its own, a shell that starts a child in the
// background, says the child's pid, and waits for it, and returns the pipe
// the child also holds.
func rwJobStarted(ctx context.Context, t *testing.T) io.Reader {
	t.Helper()
	cmd, err := Command(ctx, t.TempDir(), "sleep 300 & echo $!; wait")
	if errors.Is(err, ErrNoShell) {
		t.Skip("neither sh nor bash is on PATH")
	}
	if err != nil {
		t.Fatal(err)
	}
	InGroup(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	r := bufio.NewReader(out)
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatalf("the shell did not say it had started its child: %v", err)
	}
	return r
}

// rwGone holds the pipe to closing, which it does only once the shell and its
// child have both gone.
func rwGone(t *testing.T, r io.Reader, how string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, r)
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(rwPatience):
		t.Fatalf("after %s, a process the command started still holds its output open", how)
	}
}

// A command's context ending kills the job -- the shell and the child it
// started in the background both -- not the shell alone: a job object holds
// everything assigned to it, transitively, which sending a signal to one
// process on Windows cannot reach the way a POSIX process group does.
func TestACommandInAJobIsKilledWithItsChildrenWhenItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := rwJobStarted(ctx, t)
	cancel()
	rwGone(t, r, "the context was cancelled")
}

// StopAll reaches the same job from the record Command left for it, with the
// same effect: the shell and its background child are both gone.
func TestStopAllKillsACommandsJobWithEverythingInIt(t *testing.T) {
	r := rwJobStarted(t.Context(), t)
	StopAll()
	rwGone(t, r, "StopAll")
}

// leaveGroup closes the job's handle once Run has waited for the command, and
// the job was made with JOBOBJECT_LIMIT_KILL_ON_JOB_CLOSE: anything the
// command left running when it finished on its own, not killed, ends there
// too, by the time Run returns -- not left behind, still writing to the
// worktree, the way a plain process group would leave it once its one tracked
// process has gone.
func TestWhatACommandLeftRunningEndsWhenRunReturns(t *testing.T) {
	cmd, err := Command(t.Context(), t.TempDir(), "sleep 300 & echo $!; exit 0")
	if errors.Is(err, ErrNoShell) {
		t.Skip("neither sh nor bash is on PATH")
	}
	if err != nil {
		t.Fatal(err)
	}
	InGroup(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(out)
	started := make(chan struct{})
	go func() {
		_, _ = r.ReadString('\n')
		close(started)
	}()

	if err := Run(cmd); err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case <-started:
	case <-time.After(rwPatience):
		t.Fatal("the shell did not say it had started its child before Run returned")
	}
	// Run has returned, which is leaveGroup's cue to close the job's handle;
	// the child, still in that job when the shell exited without it, is gone
	// by the time the pipe closes.
	rwGone(t, r, "Run returning")
}
