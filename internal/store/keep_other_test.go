//go:build !windows

package store

import "testing"

// keepPID does nothing here: these systems give pids out in turn, so one that
// was just reaped comes round again only after the rest have.
func keepPID(*testing.T, int) {}
