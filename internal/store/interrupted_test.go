package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// A backlog linked into the project is written where the link points, so a
// write of it that was killed leaves its file there, not beside the link.
func TestAnInterruptedWriteOfALinkedBacklogIsClearedWhereItWasMade(t *testing.T) {
	s := newStore(t)
	target := filepath.Join(t.TempDir(), "stories.json")
	if err := os.WriteFile(target, []byte(`{"stories":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, s.cfg.BacklogPath(s.root)); err != nil {
		t.Fatal(err)
	}
	left := filepath.Join(filepath.Dir(target), ".stories.json.tmp42")
	if err := os.WriteFile(left, []byte("half a backlog"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.ClearInterruptedWrites()
	if _, err := os.Stat(left); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("what a killed write of the linked backlog left is still there: %v", err)
	}
}
