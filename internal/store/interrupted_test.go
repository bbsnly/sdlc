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

func TestAnInterruptedWriteOfLoopStateIsCleared(t *testing.T) {
	s := newStore(t)
	left := filepath.Join(s.root, ".sdlc", "state", ".active.tmp3")
	if err := os.MkdirAll(filepath.Dir(left), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(left, []byte("A-"), 0o644); err != nil {
		t.Fatal(err)
	}

	s.ClearInterruptedWrites()
	if _, err := os.Stat(left); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("what a killed write of loop state left is still there: %v", err)
	}
}

// With .sdlc a link out of the repository, as a clone can bring one, the files
// where it leads are somebody else's, and none of them is removed.
func TestInterruptedWritesAreNotClearedThroughALinkOutOfTheRepository(t *testing.T) {
	s := newStore(t)
	outside := t.TempDir()
	kept := []string{
		filepath.Join(outside, "state", ".active.tmp1"),
		filepath.Join(outside, "stories", "A-1", ".record.json.tmp7"),
	}
	for _, path := range kept {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("theirs"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(s.root, ".sdlc")); err != nil {
		t.Skipf("this system cannot make a symlink: %v", err)
	}

	s.ClearInterruptedWrites()
	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s, outside the repository, was removed: %v", path, err)
		}
	}
}
