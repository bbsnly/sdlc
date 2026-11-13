//go:build !windows

package store

import (
	"os"
	"path/filepath"
	"testing"
)

// us129Spellings is each pair of paths that name one directory here: a
// directory and a symbolic link to it, and on macOS the temporary directory
// under /var and under /private/var, which it resolves to.
func us129Spellings(t *testing.T) ([]us129Spelling, string) {
	t.Helper()
	dir := t.TempDir()
	linked := filepath.Join(dir, "linked")
	if err := os.Mkdir(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(linked, link); err != nil {
		return nil, "no symbolic link could be made here, so a directory has one spelling: " + err.Error()
	}
	out := []us129Spelling{{"a directory and a symbolic link to it", linked, link}}
	plain := filepath.Join(dir, "plain")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(plain)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != plain {
		out = append(out, us129Spelling{"the temporary directory and the path it resolves to", plain, resolved})
	}
	return out, ""
}
