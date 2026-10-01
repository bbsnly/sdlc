package shellx

// Gate 3 reopened for US-029.3's rework: Run was at 0% coverage under the
// code review's own evidence (cover-g7.out) -- every existing test drives a
// command through cmd.Start/cmd.Wait/cmd.Output directly, never through Run
// itself, so neither of Run's two jobs (joining a group once started, and
// forgetting the command once it has been waited for) was ever exercised.
// StopAll's wait for a kill to finish was consequently untested too, since
// that wait is Run's deferred s.done() firing, or the wrapped cmd.Cancel's.
//
// Portable: no process group assumptions beyond what group_test.go already
// makes (grStarted skips itself on Windows, whose background jobs a POSIX
// shell starts differently). The Windows-only job-object path is
// group_windows_test.go's.
//
// Helpers carry an rw prefix, for rework; grStarted, grGone and grPatience
// are group_test.go's, reused as they stand.

import (
	"bufio"
	"context"
	"errors"
	"io"
	"runtime"
	"testing"
	"time"
)

// rwStarted is grStarted's shape, but run through Run in a goroutine instead
// of cmd.Start/cmd.Wait called directly: Run's own bookkeeping is what this
// file holds to account, not bypassed the way group_test.go's fixture does.
// It returns the pipe the child also holds, and the channel Run's result
// arrives on once Run has returned.
func rwStarted(ctx context.Context, t *testing.T) (out io.Reader, done <-chan error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the child here is started by a POSIX shell, whose background jobs Windows runs differently")
	}
	cmd, err := Command(ctx, t.TempDir(), "sleep 300 & echo $!; wait")
	if errors.Is(err, ErrNoShell) {
		t.Skip("neither sh nor bash is on PATH")
	}
	if err != nil {
		t.Fatal(err)
	}
	InGroup(cmd)
	// StdoutPipe must be wired before the process starts, which Run does
	// inside the goroutine below.
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- Run(cmd) }()
	r := bufio.NewReader(pipe)
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatalf("the shell did not say it had started its child: %v", err)
	}
	return r, finished
}

// StopAll's own doc: "it waits ... until each command Run started has been
// killed or has ended". A long-running child a shell started in the
// background, tracked because Run (not a bare cmd.Start) ran the shell, is
// gone by the time StopAll returns -- not merely asked to go, in the
// background, after.
func TestStopAllDoesNotReturnUntilAChildRunStartedIsKilled(t *testing.T) {
	out, done := rwStarted(t.Context(), t)
	StopAll()
	grGone(t, out, "StopAll")
	select {
	case err := <-done:
		if err == nil {
			t.Error("Run returned no error for a command StopAll killed")
		}
	case <-time.After(grPatience):
		t.Fatal("Run had not returned once StopAll, which killed it, had")
	}
}

// Run forgets the command once it has waited for it: StopAll, called again
// afterwards, finds nothing of it left to kill or wait on. Checked against
// the record Run and StopAll share, because the alternative -- timing how
// long a later StopAll takes -- cannot tell "forgotten" from "fast".
func TestRunForgetsTheCommandOnceItHasWaitedForIt(t *testing.T) {
	cmd, err := Command(t.Context(), t.TempDir(), "true")
	if errors.Is(err, ErrNoShell) {
		t.Skip("neither sh nor bash is on PATH")
	}
	if err != nil {
		t.Fatal(err)
	}
	stops.Lock()
	_, tracked := stops.of[cmd]
	stops.Unlock()
	if !tracked {
		t.Fatal("precondition: Command did not record cmd for StopAll to find")
	}
	if err := Run(cmd); err != nil {
		t.Fatalf("Run: %v", err)
	}
	stops.Lock()
	_, still := stops.of[cmd]
	stops.Unlock()
	if still {
		t.Error("Run left the command recorded after waiting for it; StopAll would still try it again")
	}
}
