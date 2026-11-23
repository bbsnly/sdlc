package fsx

import (
	"io/fs"
	"os"
	"syscall"
)

// OpenFlags are none on Windows. Go's open there has no O_NOFOLLOW, and no
// code reads O_NONBLOCK; there is no named pipe in the file system to wait on.
// The descriptor check closes the window between the Lstat and the open, in
// OpenRegular and in trace's opens of the gate record: os.SameFile loads the
// Lstat result's file id from the path, opened without following a link, when
// it compares, so it compares the open handle with what the name is now. A
// symbolic link swapped in gives the link's own id, which differs from the
// target the open followed; a hard link swapped in gives the same id, and
// OneName's link count of two refuses it where the caller asks, as trace and
// the story's .gitignore do. trace's first record is an exclusive create,
// CREATE_NEW, which Go opens with FILE_FLAG_OPEN_REPARSE_POINT, so a link
// planted before it is not followed.
const OpenFlags = 0

// OneName reports whether the open file has this name and no other.
func OneName(f *os.File, _ fs.FileInfo) bool {
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info); err != nil {
		return false
	}
	return info.NumberOfLinks == 1
}
