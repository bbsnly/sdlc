package hook

// US-019, macOS only: a file of the repository has a name by number too.
// /.vol/<device>/<inode> names a file as well as a directory, and a copy of an
// outside test onto it is a copy onto the frozen test, whatever the outside
// source is called. Refused by the released tool, and to stay refused once an
// outside path is let go: the landing is what the write is, and the number is no
// path the session holds.

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// orVolfsOfFile is the volfs name of a file, or a skip where this macOS does
// not serve it.
func orVolfsOfFile(t *testing.T, file string) string {
	t.Helper()
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skip("this platform does not say a file's device and inode")
	}
	name := fmt.Sprintf("/.vol/%d/%d", st.Dev, st.Ino)
	if _, err := os.Stat(name); err != nil {
		t.Skipf("this macOS does not serve a file by device and inode, so the name is not a way in: %v", err)
	}
	return name
}

func TestAC1AndAC2ACopyOntoAFrozenTestByDeviceAndInodeStaysRefusedByTheHook(t *testing.T) {
	w := orMake(t)
	file := orVolfsOfFile(t, filepath.Join(w.root, "internal", "x_test.go"))
	dir := orVolfs(t, w)
	orRun(t, w, []orEvasion{
		{"", "cp {out}/flat/x_test.go " + file, ""},
		{"", "cp -p {out}/flat/x_test.go " + file, ""},
		{"", "mv {out}/flat/x_test.go " + file, ""},
		{"{out}/flat", "cp x_test.go " + file, ""},
		{"", "cp {out}/flat/x_test.go " + file + "/", ""},
		{"", "cp {out}/flat/x_test.go " + dir, ""},
		{"", "cp {out}/flat/x_test.go " + dir + "/x_test.go", ""},
	})
}
