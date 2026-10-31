package trace

// US-112, after design review round 1. Security found a hard link at the
// trace's name passes an Lstat check (B2), and a story directory that is a
// link out of the checkout passes it too (THREATS T3); the red team found that
// a list of forbidden keys would pass the key test as well as a list of
// allowed ones (G7).

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// AC-12, G7: the key list allows; it does not forbid. A key nobody thought to
// forbid, a name off by its case or a space, and the empty key are refused
// as surely as "command" is.
func TestAC12TheWriterRefusesAnyKeyNotOnItsListNotOnlyTheOnesNamed(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"zz", "x", "kind2", "Class", "class ", " class", "classes", "programs", "duration", "v2", "", "__proto__", "notes"} {
		t.Run("AC-12 "+key, func(t *testing.T) {
			t.Parallel()
			dir := storyDir(t)
			if err := Append(dir, Record{"kind": "shell", "class": "other", key: 1}); err == nil {
				t.Errorf("a record holding the key %q was written", key)
			}
			if raw, err := os.ReadFile(filepath.Join(dir, traceName)); err == nil {
				t.Errorf("a refused record left a trace: %q", raw)
			}
		})
	}
}

// AC-1 and AC-17, B2: a hard link at the trace's name is a regular file to
// Lstat, and the append would go to the file it names: a frozen test, a
// source file or a dotfile on the same volume. The writer refuses a trace
// whose file has a name besides this one, whichever name came first, and
// writes nothing to either.
func TestAC1AC17AHardLinkAtTheTracesNameIsNotWrittenThrough(t *testing.T) {
	t.Parallel()
	t.Run("AC-1 a link planted at the name", func(t *testing.T) {
		t.Parallel()
		dir := storyDir(t)
		victim := filepath.Join(filepath.Dir(filepath.Dir(dir)), "victim_test.go")
		if err := os.WriteFile(victim, []byte("left alone\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(victim, filepath.Join(dir, traceName)); err != nil {
			t.Skipf("this file system will not make a hard link here: %v", err)
		}
		if err := Append(dir, Record{"kind": "compaction", "trigger": "auto"}); err == nil {
			t.Error("a record was written through a hard link at the trace's name")
		}
		if raw, _ := os.ReadFile(victim); string(raw) != "left alone\n" {
			t.Errorf("the file the link names was changed: %q", raw)
		}
	})
	t.Run("AC-1 a second name made for the trace", func(t *testing.T) {
		t.Parallel()
		dir := storyDir(t)
		if err := Append(dir, Record{"kind": "compaction", "trigger": "auto"}); err != nil {
			t.Fatalf("the first record was refused: %v", err)
		}
		before, err := os.ReadFile(filepath.Join(dir, traceName))
		if err != nil {
			t.Fatal(err)
		}
		second := filepath.Join(t.TempDir(), "second.jsonl")
		if err := os.Link(filepath.Join(dir, traceName), second); err != nil {
			t.Skipf("this file system will not make a hard link here: %v", err)
		}
		if err := Append(dir, Record{"kind": "compaction", "trigger": "manual"}); err == nil {
			t.Error("a record was written to a trace that has another name")
		}
		if after, _ := os.ReadFile(second); string(after) != string(before) {
			t.Errorf("the trace's other name was written: %q", after)
		}
	})
}

// AC-1, THREATS T3: the trace writer is the one writer outside the store,
// which refuses a path that leaves the checkout (store.go's leaves). A story
// directory, or the stories directory above it, that is a link to somewhere
// else takes nothing; a checkout reached through a link, as macOS's /var is,
// is the checkout all the same.
func TestAC1AStoryDirectoryThatIsALinkOutOfTheCheckoutIsNotWritten(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		linked func(root, outside string) error
	}{
		{"the story's directory", func(root, outside string) error {
			if err := os.MkdirAll(filepath.Join(root, ".sdlc", "stories"), 0o755); err != nil {
				return err
			}
			return os.Symlink(outside, filepath.Join(root, ".sdlc", "stories", "A-1"))
		}},
		{"the stories directory", func(root, outside string) error {
			if err := os.MkdirAll(filepath.Join(root, ".sdlc"), 0o755); err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Join(outside, "A-1"), 0o755); err != nil {
				return err
			}
			return os.Symlink(outside, filepath.Join(root, ".sdlc", "stories"))
		}},
	} {
		t.Run("AC-1 "+c.name, func(t *testing.T) {
			t.Parallel()
			root, outside := t.TempDir(), t.TempDir()
			if err := c.linked(root, outside); err != nil {
				t.Skipf("this platform will not make a link here: %v", err)
			}
			if err := Append(filepath.Join(root, ".sdlc", "stories", "A-1"), Record{"kind": "compaction", "trigger": "auto"}); err == nil {
				t.Errorf("a record was written through %s, a link out of the checkout", c.name)
			}
			err := filepath.WalkDir(outside, func(path string, d fs.DirEntry, err error) error {
				if err == nil && d.Name() == traceName {
					t.Errorf("a trace was written outside the checkout: %s", path)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("AC-1 a checkout reached through a link", func(t *testing.T) {
		t.Parallel()
		checkout := t.TempDir()
		if err := os.MkdirAll(filepath.Join(checkout, ".sdlc", "stories", "A-1"), 0o755); err != nil {
			t.Fatal(err)
		}
		via := filepath.Join(t.TempDir(), "checkout")
		if err := os.Symlink(checkout, via); err != nil {
			t.Skipf("this platform will not make a link here: %v", err)
		}
		if err := Append(filepath.Join(via, ".sdlc", "stories", "A-1"), Record{"kind": "compaction", "trigger": "auto"}); err != nil {
			t.Errorf("a record was refused in a checkout reached through a link: %v", err)
		}
		if _, err := os.Stat(filepath.Join(checkout, ".sdlc", "stories", "A-1", traceName)); errors.Is(err, fs.ErrNotExist) {
			t.Error("the trace was not written in the checkout")
		}
	})
}
