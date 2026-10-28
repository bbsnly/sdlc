package hook

// US-019, macOS only: the repository has more names than the one it was opened
// by. /.vol/<device>/<inode> is any directory of a volume by number, and
// /System/Volumes/Data is the same volume as the path without it. Neither is
// the text of any path the session holds, and each of them is the repository.
// Read for a frozen test, a path through either is refused today and has to
// stay refused once an outside path is let go. (Loop state and protected paths
// through either, and a plain `rm -f` of a frozen test by device and inode, are
// allowed today and are not this story's to change.)

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// orVolfs is the volfs name of the project's internal directory, or a skip
// where this macOS does not serve the name.
func orVolfs(t *testing.T, w orWorld) string {
	t.Helper()
	info, err := os.Stat(filepath.Join(w.root, "internal"))
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("this platform does not say a directory's device and inode")
	}
	name := fmt.Sprintf("/.vol/%d/%d", st.Dev, st.Ino)
	if _, err := os.Stat(name + "/x_test.go"); err != nil {
		t.Skipf("this macOS does not serve a file by device and inode, so the name is not a way in: %v", err)
	}
	return name
}

func TestAC1AndAC2AFrozenTestByDeviceAndInodeStaysRefusedByTheHook(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	vol := orVolfs(t, w)
	rows := []orEvasion{
		{"", "echo x > " + vol + "/x_test.go", ""},
		{"", "echo x > " + vol + "/brand_new_test.go", ""},
		{"", "touch " + vol + "/brand_new_test.go", ""},
		{"", "cp {out}/flat/x_test.go " + vol + "/", ""},
		{"", "find " + vol + " -name '*_test*' -delete", ""},
		{"", "cd " + vol + " && echo x > x_test.go", ""},
		{"{out}", "echo x > " + vol + "/x_test.go", ""},
	}
	orRun(t, w, rows)
}

func TestAC1AndAC2AFrozenTestThroughTheDataVolumeStaysRefusedByTheHook(t *testing.T) {
	t.Parallel()
	w := orMake(t)
	data := "/System/Volumes/Data" + slash(w.root)
	if _, err := os.Stat(data + "/internal/x_test.go"); err != nil {
		t.Skipf("this macOS has no data volume to name the project by: %v", err)
	}
	rows := []orEvasion{
		{"", "rm -f " + data + "/internal/x_test.go", ""},
		{"", "echo x > " + data + "/internal/x_test.go", ""},
		{"", "echo x > " + data + "/internal/brand_new_test.go", ""},
		{"", "find " + data + " -name '*_test*' -delete", ""},
		{"{out}", "rm -f " + data + "/internal/x_test.go", ""},
	}
	orRun(t, w, rows)
}
