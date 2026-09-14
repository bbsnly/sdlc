package store

import "os"

// processAlive reports whether pid is a running process. On Windows finding a
// process opens it, which fails once it has exited and nothing holds it open.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
