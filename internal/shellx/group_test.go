package shellx

// US-029.3 (PLAN.md step 4): the landing runs a project's commands on a tree
// about to reach trunk, and a command it gives up on -- the stop killed, its
// context done -- takes everything it started with it. Otherwise a test
// runner's child keeps writing to the worktree after the landing has moved
// on, or holds the landing's lock open. What a command printed is kept to a
// bounded tail, however much it printed.
//
// The functions these hold shellx to, as PLAN.md names them:
//
//	InGroup(cmd)   -- the command runs in a process group (a job, on Windows) of its own
//	StopAll()      -- kills every group still running
//	NewTail(limit) -- a writer keeping the last limit bytes, whose String() is Tail over them
//
// Helpers carry a gr prefix.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"
	"time"
)

// grPatience bounds how long a test waits for a pipe to close before it says
// the process that holds it was not stopped. It is a deadline, not a pause:
// nothing waits for it when the group is gone.
const grPatience = 20 * time.Second

// grStarted starts, in a group of its own, a shell that starts a child in the
// background, says the child's pid, and waits for it; and returns the pipe
// the child also holds.
func grStarted(ctx context.Context, t *testing.T) io.Reader {
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

// grGone holds the pipe to closing, which it does only once the shell and
// its child have both gone.
func grGone(t *testing.T, r io.Reader, how string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, r)
		done <- err
	}()
	select {
	case <-done:
	case <-time.After(grPatience):
		t.Fatalf("after %s, a process the command started still holds its output open", how)
	}
}

// PLAN.md step 4: StopAll, which the signal handler calls before it lets the
// landing's lock go, kills the whole group, the child the shell started
// included.
func TestStopAllKillsEveryProcessACommandStarted(t *testing.T) {
	first := grStarted(t.Context(), t)
	second := grStarted(t.Context(), t)
	StopAll()
	grGone(t, first, "StopAll")
	grGone(t, second, "StopAll")
}

// A command whose context is done is killed with everything it started, not
// the shell alone.
func TestACommandInAGroupIsKilledWithItsChildrenWhenItsContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	r := grStarted(ctx, t)
	cancel()
	grGone(t, r, "the context was cancelled")
}

// A command that finished is no longer StopAll's: a later command runs to
// its end.
func TestAFinishedCommandLeavesNothingForStopAllToKillLater(t *testing.T) {
	run := func() string {
		t.Helper()
		cmd, err := Command(t.Context(), t.TempDir(), "echo ran")
		if errors.Is(err, ErrNoShell) {
			t.Skip("neither sh nor bash is on PATH")
		}
		if err != nil {
			t.Fatal(err)
		}
		InGroup(cmd)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("a command in a group of its own: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	if got := run(); got != "ran" {
		t.Fatalf("the command printed %q", got)
	}
	StopAll()
	if got := run(); got != "ran" {
		t.Errorf("a command started after StopAll printed %q, want it run to its end", got)
	}
}

// PLAN.md step 4 and architect 3: the landing keeps what a command printed
// to a bounded tail, and says it the way Tail does, so the package has one
// idea of a tail.
func TestNewTailKeepsTheLastBytesAndSaysThemAsTailDoes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int
		writes []string
	}{
		{"nothing written", 16, nil},
		{"less than the limit", 64, []string{"ok\n", "  2 passed\n"}},
		{"exactly the limit", 8, []string{"12345678"}},
		{"many small writes past it", 50, strings.SplitAfter(strings.Repeat("line of output\n", 40), "\n")},
		{"one write larger than the limit", 32, []string{strings.Repeat("x", 100) + "\nFAIL: the last line\n"}},
		{"a write that crosses the limit", 20, []string{strings.Repeat("a", 15), strings.Repeat("b", 15), "\ncoverage: 50.0%\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := NewTail(tc.limit)
			all := ""
			var allSb149 strings.Builder
			for _, s := range tc.writes {
				n, err := fmt.Fprint(w, s)
				if n != len(s) || err != nil {
					t.Fatalf("writing %d bytes gave %d, %v; a writer takes all of it", len(s), n, err)
				}
				allSb149.WriteString(s)
			}
			all += allSb149.String()
			kept := all
			if len(kept) > tc.limit {
				kept = kept[len(kept)-tc.limit:]
			}
			if got, want := w.String(), Tail(kept, tc.limit); got != want {
				t.Errorf("String() = %q, want Tail over the last %d bytes, %q", got, tc.limit, want)
			}
		})
	}
}
