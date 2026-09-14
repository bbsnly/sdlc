// Package shellx runs the commands a project puts in its configuration. They
// are written for a POSIX shell, and Claude Code on Windows runs under Git
// Bash, which has one.
package shellx

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// ErrNoShell is why a configured command could not be run at all, which is a
// different thing from it failing.
var ErrNoShell = errors.New("neither sh nor bash is on PATH")

// Command is command, to be run by the shell from dir.
func Command(ctx context.Context, dir, command string) (*exec.Cmd, error) {
	for _, name := range []string{"sh", "bash"} {
		if path, err := exec.LookPath(name); err == nil {
			cmd := exec.CommandContext(ctx, path, "-c", command)
			cmd.Dir = dir
			return cmd, nil
		}
	}
	return nil, ErrNoShell
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
