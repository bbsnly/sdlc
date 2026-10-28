package store

import (
	"os"
	"path/filepath"
	"testing"
)

// inTheWay is what tells a file where the sections' directory should be from
// no directory at all on Windows, where listing a file fails as though it
// were not there. macOS and Linux never ask it that, so it is held here on
// every platform.
func TestInTheWayIsTrueOnlyWhereSomethingIsThereThroughALink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "codemap")
	if err := os.WriteFile(file, []byte("in the way\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows := map[string]bool{
		file:                           true,
		dir:                            true,
		filepath.Join(dir, "missing"):  false,
		filepath.Join(file, "beneath"): false,
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "nothing"), dangling); err != nil {
		t.Logf("no link to nothing: %v", err)
	} else {
		rows[dangling] = false
	}
	for path, want := range rows {
		if got := inTheWay(path); got != want {
			t.Errorf("inTheWay(%s) = %v, want %v", path, got, want)
		}
	}
}
