//go:build !windows

package cli

// leaveWorktree does nothing here: a directory that is a process's current one
// can be removed, so the process stays where it was.
func leaveWorktree(string, string) {}
