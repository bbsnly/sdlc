package trace

// US-112, design review round 2 (security's note on B2, and N1). Two windows
// the round-2 tests did not reach:
//
//   - Between Append's Lstat of the trace's name and its open. On Windows
//     there is no O_NOFOLLOW, and before the first record the Lstat finds
//     nothing for SameFile to compare with, so a link planted then is
//     followed and its target, a regular file with one link, is appended to.
//     An open that creates only where nothing is there (O_EXCL, Windows'
//     CREATE_NEW) fails on any name, a link included. A named pipe swapped in
//     there would hold the open until a reader came, which is never, and the
//     hook with it, after a deny is already on stdout.
//   - A named pipe at the name before anything is recorded: Append refuses it,
//     but Read opened it and waited, so `sdlc trace` hung, and so did each
//     SubagentStop's read of the hand-back.
//
// afterLstat is the seam into the first window: Append calls it, where it is
// set, with the trace's path after each Lstat of that path and before it
// opens the path. Nothing but these tests sets it. These tests are not
// parallel, as they set it; Go runs every sequential test before the parallel
// ones resume, so no other Append sees it.
//
// What only the code can show, for the verifier: that Windows' open uses
// CREATE_NEW where the Lstat found nothing (the first subtest holds it only
// where the runner may make a symlink, and passes on POSIX through
// O_NOFOLLOW as well); and that Read's own open is O_NONBLOCK, for a pipe
// swapped in after its Lstat, which no seam reaches.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

// bound is how long a call that must not wait on a pipe is given before the
// test says it waited. It is a ceiling, not a sleep: a call that returns
// ends the wait at once.
const bound = 10 * time.Second

// noPipes skips on Windows, which has no named pipe in the file system.
func noPipes(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no named pipe in the file system, and no mkfifo")
	}
}

// mkfifo makes a named pipe at path. It takes a context and not t, so that the
// seam can call it from the goroutine within runs Append in.
func mkfifo(ctx context.Context, path string) error {
	return exec.CommandContext(ctx, "mkfifo", path).Run()
}

// pipeAt makes a named pipe at path, or skips where it cannot.
func pipeAt(t *testing.T, path string) {
	t.Helper()
	noPipes(t)
	if err := mkfifo(t.Context(), path); err != nil {
		t.Skipf("mkfifo would not make a named pipe here: %v", err)
	}
}

// within runs call, and fails the test if it has not returned by the bound.
// A call left waiting on the pipe at path is let go by opening the pipe's
// other end, so that the test ends.
func within(t *testing.T, what, path string, call func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- call() }()
	select {
	case err := <-done:
		return err
	case <-time.After(bound):
		for _, flag := range []int{os.O_RDONLY, os.O_WRONLY} {
			if f, err := os.OpenFile(path, flag|syscall.O_NONBLOCK, 0); err == nil {
				_ = f.Close()
			}
		}
		t.Fatalf("%s was still waiting on a named pipe after %v", what, bound)
		return nil
	}
}

// setAfterLstat sets the seam for the rest of the test.
func setAfterLstat(t *testing.T, at func(path string)) {
	t.Helper()
	before := afterLstat
	afterLstat = at
	t.Cleanup(func() { afterLstat = before })
}

// once is at, called the first time only: Append may Lstat again after an
// exclusive create found the name taken.
func once(at func(path string)) func(string) {
	var o sync.Once
	return func(path string) { o.Do(func() { at(path) }) }
}

const firstLine = `{"v":1,"at":"2026-10-01T10:00:00.000Z","kind":"compaction","trigger":"auto"}` + "\n"

// AC-1, THREATS T2 and security's B2 note: a name that appears between the
// Lstat and the open is not followed, before the first record or after it.
// A regular file that appears there before the first record is another
// hook's first record, and is appended to, not lost.
func TestAC1AC23ANameThatAppearsBetweenTheLstatAndTheOpenIsNotFollowed(t *testing.T) {
	t.Run("AC-1 a link planted before the first record", func(t *testing.T) {
		dir := storyDir(t)
		victim := filepath.Join(t.TempDir(), "victim_test.go")
		if err := os.WriteFile(victim, []byte("left alone\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		planted := false
		setAfterLstat(t, once(func(path string) {
			planted = os.Symlink(victim, path) == nil
		}))
		err := Append(dir, Record{"kind": "compaction", "trigger": "auto"})
		if !planted {
			t.Skip("this platform will not make a symlink here, or Append never reached the seam")
		}
		if err == nil {
			t.Error("AC-1 a record was written through a link planted at the trace's name before its first record")
		}
		if raw, _ := os.ReadFile(victim); string(raw) != "left alone\n" {
			t.Errorf("AC-1 the file the link names was changed: %q", raw)
		}
	})
	t.Run("AC-1 a link swapped in after the first record", func(t *testing.T) {
		dir := storyDir(t)
		if err := Append(dir, Record{"kind": "compaction", "trigger": "auto"}); err != nil {
			t.Fatalf("the first record was refused: %v", err)
		}
		victim := filepath.Join(t.TempDir(), "victim_test.go")
		if err := os.WriteFile(victim, []byte("left alone\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		planted := false
		setAfterLstat(t, once(func(path string) {
			planted = os.Remove(path) == nil && os.Symlink(victim, path) == nil
		}))
		err := Append(dir, Record{"kind": "compaction", "trigger": "manual"})
		if !planted {
			t.Skip("this platform will not make a symlink here, or Append never reached the seam")
		}
		if err == nil {
			t.Error("AC-1 a record was written through a link swapped in for the trace between its Lstat and its open")
		}
		if raw, _ := os.ReadFile(victim); string(raw) != "left alone\n" {
			t.Errorf("AC-1 the file the link names was changed: %q", raw)
		}
	})
	t.Run("AC-23 another appender's first record", func(t *testing.T) {
		dir := storyDir(t)
		reached := false
		setAfterLstat(t, once(func(path string) {
			reached = true
			if err := os.WriteFile(path, []byte(firstLine), 0o644); err != nil {
				t.Error(err)
			}
		}))
		if err := Append(dir, Record{"kind": "compaction", "trigger": "manual"}); err != nil {
			t.Errorf("AC-23 a record was refused because another appender created the trace first: %v", err)
		}
		if !reached {
			t.Fatal("Append never called afterLstat, so the window is not reached")
		}
		got := lines(t, dir)
		if len(got) != 2 || got[0]+"\n" != firstLine || decoded(t, got[1])["trigger"] != "manual" {
			t.Errorf("AC-23 the trace holds %q, want the other appender's record and then this one", got)
		}
	})
	t.Run("AC-1 a named pipe made before the first record", func(t *testing.T) {
		noPipes(t)
		dir := storyDir(t)
		path := filepath.Join(dir, traceName)
		made, ctx := false, t.Context()
		setAfterLstat(t, once(func(p string) { made = mkfifo(ctx, p) == nil }))
		err := within(t, "Append", path, func() error {
			return Append(dir, Record{"kind": "compaction", "trigger": "auto"})
		})
		if !made {
			t.Skip("mkfifo would not make a named pipe here, or Append never reached the seam")
		}
		if err == nil {
			t.Error("AC-1 Append wrote to a named pipe made at the trace's name before its first record")
		}
	})
	t.Run("AC-1 a named pipe swapped in after the first record", func(t *testing.T) {
		noPipes(t)
		dir := storyDir(t)
		if err := Append(dir, Record{"kind": "compaction", "trigger": "auto"}); err != nil {
			t.Fatalf("the first record was refused: %v", err)
		}
		path := filepath.Join(dir, traceName)
		made, ctx := false, t.Context()
		setAfterLstat(t, once(func(p string) { made = os.Remove(p) == nil && mkfifo(ctx, p) == nil }))
		err := within(t, "Append", path, func() error {
			return Append(dir, Record{"kind": "compaction", "trigger": "manual"})
		})
		if !made {
			t.Skip("mkfifo would not make a named pipe here, or Append never reached the seam")
		}
		if err == nil {
			t.Error("AC-1 Append wrote to a named pipe swapped in for the trace between its Lstat and its open")
		}
	})
}

// AC-1, security's N1: a named pipe at the trace's name takes no record, and
// neither the writer nor the reader waits on it. The reader says the trace is
// there and cannot be read; it is never "not there", which `sdlc trace` would
// show as a story that predates tracing.
func TestAC1AC21ANamedPipeAtTheTracesNameIsNeitherWrittenNorWaitedOn(t *testing.T) {
	t.Run("AC-1 the writer", func(t *testing.T) {
		dir := storyDir(t)
		path := filepath.Join(dir, traceName)
		pipeAt(t, path)
		err := within(t, "Append", path, func() error {
			return Append(dir, Record{"kind": "compaction", "trigger": "auto"})
		})
		if err == nil {
			t.Error("AC-1 a record was written to a named pipe at the trace's name")
		}
		if fi, err := os.Lstat(path); err != nil || fi.Mode()&fs.ModeNamedPipe == 0 {
			t.Errorf("AC-1 the named pipe was replaced or removed: %v", err)
		}
	})
	t.Run("AC-21 the reader", func(t *testing.T) {
		dir := storyDir(t)
		path := filepath.Join(dir, traceName)
		pipeAt(t, path)
		err := within(t, "Read", path, func() error {
			_, err := Read(dir)
			return err
		})
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			t.Errorf("AC-21 reading a named pipe at the trace's name gave %v, want an error that is not \"not there\"", err)
		}
	})
}
