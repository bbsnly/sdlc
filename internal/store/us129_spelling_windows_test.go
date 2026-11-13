//go:build windows

package store

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// us129Spellings is a directory with a long name and its 8.3 short name, where
// the volume makes short names.
func us129Spellings(t *testing.T) ([]us129Spelling, string) {
	t.Helper()
	long := filepath.Join(t.TempDir(), "a directory with a long name")
	if err := os.Mkdir(long, 0o755); err != nil {
		t.Fatal(err)
	}
	from, err := syscall.UTF16PtrFromString(long)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, syscall.MAX_PATH)
	n, err := syscall.GetShortPathName(from, &buf[0], uint32(len(buf)))
	if err == nil && int(n) > len(buf) {
		buf = make([]uint16, n)
		n, err = syscall.GetShortPathName(from, &buf[0], uint32(len(buf)))
	}
	if err != nil {
		return nil, "the volume gave no short name for " + long + ": " + err.Error()
	}
	short := syscall.UTF16ToString(buf[:n])
	if strings.EqualFold(filepath.Base(short), filepath.Base(long)) {
		return nil, "8.3 short names are off on the volume of " + long + ", so a directory has one spelling"
	}
	return []us129Spelling{{"a directory and its 8.3 short name", long, short}}, ""
}
