//go:build windows

package shellx

// US-050, AC-7: the job object is held to account through Run, the way the
// landing runs a command, and a test fails if cancelling the command's
// context, StopAll, or Run returning leaves running a process the command
// started (CWE-404: a process left running after its owner stopped it, still
// writing to the worktree after the landing's checks).
//
// The fixture these replaced started the shell with cmd.Start, which never
// puts it in its job -- only Run calls joinGroup -- so a cancel and StopAll
// fell back to killing the shell alone, and both tests timed out on Windows
// CI. The test of Run returning read a pipe that cmd.Wait closes, so it passed
// whatever the job did. Here the command writes to a pipe the test owns: exec
// hands an *os.File to the shell as it is and closes nothing of it, so the
// read end reaches end of file only once every process holding the write end
// -- the shell, and the child it started in the background -- has gone.
//
// Helpers carry a jb prefix, for job.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// jbPatience bounds every wait here. It is a deadline, not a pause: nothing
// waits for it once what is waited for has happened.
const jbPatience = 20 * time.Second

// jbCommand is a command Run is running, in a goroutine, in a job of its own.
type jbCommand struct {
	// out is the read end of the pipe the command and its children write to.
	out *bufio.Reader
	// finished is closed once Run has returned, with err its result.
	finished chan struct{}
	err      error
}

// jbRun runs script through Run in a goroutine, in a job of its own, its
// output on a pipe the test owns, and returns once the shell has said its
// first line. The test's copy of the write end is closed by the goroutine
// that ran Run, once Run has returned, so that end of file on the read end
// means Run has returned and no process the command started holds it. Closing
// it from the test's own goroutine raced with Start reading it.
func jbRun(ctx context.Context, t *testing.T, script string) *jbCommand {
	t.Helper()
	cmd, err := Command(ctx, t.TempDir(), script)
	if errors.Is(err, ErrNoShell) {
		t.Skip("neither sh nor bash is on PATH")
	}
	if err != nil {
		t.Fatal(err)
	}
	InGroup(cmd)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	cmd.Stdout = w

	c := &jbCommand{out: bufio.NewReader(r), finished: make(chan struct{})}
	go func() {
		c.err = Run(cmd)
		_ = w.Close()
		close(c.finished)
	}()
	// The shell is gone before the directory it ran in is removed, whatever
	// the test found.
	t.Cleanup(func() {
		select {
		case <-c.finished:
		case <-time.After(jbPatience):
		}
	})

	said := make(chan error, 1)
	go func() {
		_, err := c.out.ReadString('\n')
		said <- err
	}()
	select {
	case err := <-said:
		if err != nil {
			t.Fatalf("the shell did not say it had started its child (Run: %v): %v", jbReturned(t, c, "the pipe closed"), err)
		}
	case <-time.After(jbPatience):
		t.Fatal("the shell did not say it had started its child")
	}
	return c
}

// jbRunning holds Run to not having returned yet. The shell waits for its
// child, so a Run that has returned means the child never ran, and every
// check after this would pass with nothing to stop.
func jbRunning(t *testing.T, c *jbCommand) {
	t.Helper()
	select {
	case <-c.finished:
		t.Fatalf("Run returned %v before anything stopped it: the child the shell waits for did not run", c.err)
	default:
	}
}

// jbGone fails the test unless the pipe reaches end of file before the
// deadline: every process that held its write end has gone. A read that
// fails is not end of file; nothing but the test closes the read end.
func jbGone(t *testing.T, c *jbCommand, after string) {
	t.Helper()
	eof := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, c.out)
		eof <- err
	}()
	select {
	case err := <-eof:
		if err != nil {
			t.Fatalf("after %s, reading what the command wrote failed: %v", after, err)
		}
	case <-time.After(jbPatience):
		t.Fatalf("after %s, a process the command started still holds its output open", after)
	}
}

// jbReturned waits, as long as the deadline, for Run to return, and gives
// what it returned.
func jbReturned(t *testing.T, c *jbCommand, after string) error {
	t.Helper()
	select {
	case <-c.finished:
		return c.err
	case <-time.After(jbPatience):
		t.Fatalf("after %s, Run had not returned", after)
		return nil
	}
}

// A command's context ending kills its job -- the shell and the child it
// started in the background both -- not the shell alone: a job holds every
// process started in it, which a signal to one process on Windows cannot
// reach the way a POSIX process group does.
func TestAC7CancellingTheContextOfACommandRunInAJobEndsEveryProcessItStarted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c := jbRun(ctx, t, "sleep 300 & echo started; wait")
	jbRunning(t, c)
	cancel()
	jbGone(t, c, "its context was cancelled")
	if err := jbReturned(t, c, "its context was cancelled"); err == nil {
		t.Error("Run returned no error for a command whose context was cancelled while it ran")
	}
}

// StopAll, which the signal handler calls before the landing's lock is let
// go, reaches the same job from the record Command left for it, and does not
// return until the command has been killed: the shell and its background
// child are both gone.
func TestAC7StopAllEndsEveryProcessACommandRunInAJobStarted(t *testing.T) {
	c := jbRun(t.Context(), t, "sleep 300 & echo started; wait")
	jbRunning(t, c)
	StopAll()
	jbGone(t, c, "StopAll")
	if err := jbReturned(t, c, "StopAll"); err == nil {
		t.Error("Run returned no error for a command StopAll killed")
	}
}

// Run closes the job once it has waited for the command, and the job was made
// with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE: what the command left running when
// it finished on its own, not killed, ends there too -- not left behind,
// still writing to the worktree, the way a process group is once its one
// tracked process has gone.
func TestAC7RunReturningEndsWhatACommandInAJobLeftRunning(t *testing.T) {
	c := jbRun(t.Context(), t, "sleep 300 & echo started; exit 0")
	if err := jbReturned(t, c, "the shell exited"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	jbGone(t, c, "Run returned")
}
