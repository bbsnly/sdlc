//go:build !windows

package fsx

// inUse is always false here: a file another process has open can be renamed
// over.
func inUse(error) bool { return false }
