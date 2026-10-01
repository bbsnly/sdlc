// Package shellx runs the commands a project puts in its configuration. They
// are written for a POSIX shell, and Claude Code on Windows runs under Git
// Bash, which has one.
package shellx

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ErrNoShell is why a configured command could not be run at all, which is a
// different thing from it failing.
var ErrNoShell = errors.New("neither sh nor bash is on PATH")

// Command is command, to be run by the shell from dir. StopAll stops it, as
// the end of ctx does.
func Command(ctx context.Context, dir, command string) (*exec.Cmd, error) {
	for _, name := range []string{"sh", "bash"} {
		if path, err := exec.LookPath(name); err == nil {
			ctx, cancel := context.WithCancel(ctx)
			cmd := exec.CommandContext(ctx, path, "-c", command)
			cmd.Dir = dir
			track(ctx, cmd, cancel)
			return cmd, nil
		}
	}
	return nil, ErrNoShell
}

// waitDelay bounds how long Wait waits, once a command is stopped, for
// whatever still holds its output open. StopAll waits no longer than this for
// the commands Run started to be stopped.
const waitDelay = 5 * time.Second

// stops holds, for every command Command made whose context has not ended
// and that Run has not waited for, a way to end that context.
var stops struct {
	sync.Mutex
	of map[*exec.Cmd]*stop
}

// stop is one command's entry in stops. over is made by Run, and closed once
// the command has been killed or waited for, whichever comes first.
type stop struct {
	cancel context.CancelFunc
	over   chan struct{}
	once   sync.Once
}

func (s *stop) done() {
	if s.over != nil {
		s.once.Do(func() { close(s.over) })
	}
}

// track has StopAll end ctx, cmd's. Ending it, not signalling the process, is
// how a command is stopped: exec calls the command's Cancel only while the
// command runs, so a stop can never reach a process that has finished, or
// another that has since taken its number.
func track(ctx context.Context, cmd *exec.Cmd, cancel context.CancelFunc) {
	stops.Lock()
	if stops.of == nil {
		stops.of = map[*exec.Cmd]*stop{}
	}
	stops.of[cmd] = &stop{cancel: cancel}
	stops.Unlock()
	context.AfterFunc(ctx, func() { forget(cmd) })
}

// forget takes cmd out of stops.
func forget(cmd *exec.Cmd) {
	stops.Lock()
	delete(stops.of, cmd)
	stops.Unlock()
}

// Run runs cmd, which Command made, to its end, as cmd.Run does. Where InGroup
// has given it a group, it is put in that group as soon as it has started,
// on the platforms where it is not already; and StopAll, which stops it, does
// not return until it has been killed. Once it has been waited for, StopAll
// forgets it, and a command that finished leaves nothing behind in stops.
func Run(cmd *exec.Cmd) error {
	stops.Lock()
	s := stops.of[cmd]
	if s != nil {
		s.over = make(chan struct{})
	}
	stops.Unlock()
	if s != nil {
		if kill := cmd.Cancel; kill != nil {
			cmd.Cancel = func() error {
				defer s.done()
				return kill()
			}
		}
		defer s.done()
		defer forget(cmd)
	}
	defer leaveGroup(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := joinGroup(cmd); err != nil {
		// Unwatched, it would be a process a stop could not take down.
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}

// StopAll stops every command Command made that is still running, and with
// InGroup, everything each of them started. The signal handler calls it
// before it lets the landing's lock go, so that another stop cannot take the
// lock while the first one's commands still write to the worktree: it waits,
// for as long as waitDelay at most, until each command Run started has been
// killed or has ended. Ending a context only asks for the kill, which exec
// sends from a goroutine of its own, and the handler exits straight after.
func StopAll() {
	stops.Lock()
	all := make([]*stop, 0, len(stops.of))
	var over []chan struct{}
	for _, s := range stops.of {
		all = append(all, s)
		if s.over != nil {
			over = append(over, s.over)
		}
	}
	stops.Unlock()
	for _, s := range all {
		s.cancel()
	}
	if len(over) == 0 {
		return
	}
	deadline := time.NewTimer(waitDelay)
	defer deadline.Stop()
	for _, c := range over {
		select {
		case <-c:
		case <-deadline.C:
			return
		}
	}
}

// TailWriter keeps the last bytes written to it, however much is written.
type TailWriter struct {
	mu    sync.Mutex
	kept  []byte
	limit int
}

// NewTail is a writer that keeps the last limit bytes written to it.
func NewTail(limit int) *TailWriter {
	return &TailWriter{limit: max(limit, 0)}
}

// Write keeps the end of p, and takes all of it. What it keeps grows to twice
// the limit before it is cut back, so that many small writes cost no more than
// a few large ones.
func (w *TailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) >= w.limit {
		w.kept = append(w.kept[:0], p[len(p)-w.limit:]...)
		return len(p), nil
	}
	w.kept = append(w.kept, p...)
	if len(w.kept) > 2*w.limit {
		w.kept = append(w.kept[:0], w.kept[len(w.kept)-w.limit:]...)
	}
	return len(p), nil
}

// String is Tail over the last limit bytes written.
func (w *TailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	kept := w.kept
	if len(kept) > w.limit {
		kept = kept[len(kept)-w.limit:]
	}
	return Tail(string(kept), w.limit)
}

// Tail is the last limit bytes of what a command printed, which is where a
// failing command says what went wrong.
func Tail(output string, limit int) string {
	said := strings.TrimSpace(output)
	if len(said) > limit {
		said = "..." + said[len(said)-limit:]
	}
	return said
}
