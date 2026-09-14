//go:build windows

package pathrules

import (
	"strings"
	"syscall"
	"unsafe"
)

var getFinalPathNameByHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

// finalPath is the name Windows itself gives an existing p, once every link on
// the way to it is followed. filepath.EvalSymlinks has not followed a
// junction since Go 1.23, and making one needs no privilege: a junction to
// .sdlc put the freeze at `j\state\tests.lock`, which no rule names.
func finalPath(p string) (string, bool) {
	name, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	// No access is asked for: the handle is only named, never read or written.
	// Backup semantics is what lets a directory be opened at all.
	h, err := syscall.CreateFile(name, 0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", false
	}
	defer syscall.CloseHandle(h)

	buf := make([]uint16, syscall.MAX_PATH)
	for {
		// The last argument, 0, asks for the name with a drive letter.
		n, _, _ := getFinalPathNameByHandle.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
		switch {
		case n == 0:
			return "", false
		case int(n) >= len(buf):
			// Too small, and n is the size it needs.
			buf = make([]uint16, n)
			continue
		}
		out := syscall.UTF16ToString(buf[:n])
		if rest, ok := strings.CutPrefix(out, `\\?\UNC\`); ok {
			return `\\` + rest, true
		}
		return strings.TrimPrefix(out, `\\?\`), true
	}
}
