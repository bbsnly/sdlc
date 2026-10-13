package hook

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// oneName reports whether a file has one name and no more. Where that cannot
// be told, it is not known to.
func oneName(path string, _ os.FileInfo) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	// Opened for nothing but its attributes, and as the link where it is one.
	h, err := syscall.CreateFile(p, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h) //nolint:errcheck // nothing to do about a handle that will not close
	var d syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &d); err != nil {
		return false
	}
	return d.NumberOfLinks == 1
}

// namedAsLooked reports whether the disk looks for a path under the names it
// is written with. Windows takes the dots and spaces off the end of a name
// before it looks, and a program a POSIX shell runs here can write the name
// as it stands: asked about `out.`, the disk would answer about `out`, which
// can be a plain file beside a link the command writes through.
func namedAsLooked(path string) bool {
	for name := range strings.SplitSeq(filepath.ToSlash(path), "/") {
		if strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
			return false
		}
	}
	return true
}
