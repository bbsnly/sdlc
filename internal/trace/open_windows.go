package trace

import (
	"io/fs"
	"os"
	"syscall"
)

// openFlags are none on Windows. Go's open there has no O_NOFOLLOW, and no
// code reads O_NONBLOCK; there is no named pipe in the file system to wait on.
// Before the first record, the exclusive create is CREATE_NEW, which Go opens
// with FILE_FLAG_OPEN_REPARSE_POINT, so a link planted then is not followed.
// After it, the descriptor check closes the window between the Lstat and the
// open: os.SameFile loads the Lstat result's file id from the path, opened
// without following a link, when it compares, so it compares the open handle
// with what the name is now. A symbolic link swapped in gives the link's own
// id, which differs from the target the open followed; a hard link swapped
// in gives the same id, and its link count of two refuses it.
const openFlags = 0

// oneName reports whether the open file has this name and no other.
func oneName(f *os.File, _ fs.FileInfo) bool {
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info); err != nil {
		return false
	}
	return info.NumberOfLinks == 1
}
