//go:build windows

package cli

import (
	"log/slog"
	"os"
	"strings"

	"github.com/bbsnly/sdlc/internal/config"
)

// leaveWorktree moves the process into main when its current directory is in
// worktree, which is about to be removed: Windows does not remove a directory
// that is a process's current one. Only then, because a command run through
// ExecuteIn shares the process with others, and moving it would move them all.
func leaveWorktree(main, worktree string) {
	wd, err := os.Getwd()
	if err != nil {
		return
	}
	here, wt := config.PathKey(wd), config.PathKey(worktree)
	if here != wt && !strings.HasPrefix(here, wt+"/") {
		return
	}
	if err := os.Chdir(main); err != nil {
		slog.Debug("stop could not move into the main checkout", "err", err)
	}
}
