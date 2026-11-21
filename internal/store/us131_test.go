package store

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// US-131 AC-3: `sdlc decide` writes a story's DECISIONS.md whole from its
// record, and puts the file back as it was when the record then cannot be
// saved, so that the file never holds a decision the record does not.

func TestAC3RecordStoredSaysWhetherAStoryHasAGateRecordOnDisk(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if s.RecordStored("A-1") {
		t.Error("AC-3 RecordStored is true before any record is saved")
	}
	// Record starts a fresh one where there is none, and writes nothing.
	if _, err := s.Record("A-1"); err != nil {
		t.Fatal(err)
	}
	if s.RecordStored("A-1") {
		t.Error("AC-3 RecordStored is true for the fresh record Record made and did not save")
	}
	if err := s.SaveRecord(model.NewRecord("A-1", fixedTime)); err != nil {
		t.Fatal(err)
	}
	if !s.RecordStored("A-1") {
		t.Error("AC-3 RecordStored is false after SaveRecord")
	}
	if s.RecordStored("A-2") {
		t.Error("AC-3 RecordStored answers for another story")
	}
	if s.RecordStored("../../etc") {
		t.Error("AC-3 RecordStored accepted a traversing id")
	}
}

func TestAC3WriteDecisionsWritesTheFileWhereDecisionsPathSays(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path, undo, err := s.WriteDecisions("A-1", []byte("# Decisions\n\none\n"))
	if err != nil {
		t.Fatal(err)
	}
	if want := ".sdlc/stories/A-1/DECISIONS.md"; path != want || DecisionsPath("A-1") != want {
		t.Errorf("AC-3 WriteDecisions said %q and DecisionsPath %q, want %q", path, DecisionsPath("A-1"), want)
	}
	if undo == nil {
		t.Fatal("AC-3 WriteDecisions gave no undo")
	}
	file := filepath.Join(s.root, filepath.FromSlash(path))
	if body, err := os.ReadFile(file); err != nil || string(body) != "# Decisions\n\none\n" {
		t.Errorf("AC-3 on disk: %q, %v", body, err)
	}

	// A second write replaces the file whole.
	if _, _, err := s.WriteDecisions("A-1", []byte("# Decisions\n\none\ntwo\n")); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(file); err != nil || string(body) != "# Decisions\n\none\ntwo\n" {
		t.Errorf("AC-3 on disk after the second write: %q, %v", body, err)
	}
}

// The undo puts back what the file held before the write, and where there was
// no file, removes the one the write made.
func TestAC3WriteDecisionsUndoPutsTheFileBackAsItWas(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	path, undo, err := s.WriteDecisions("A-1", []byte("first\n"))
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(s.root, filepath.FromSlash(path))
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("AC-3 undoing the first write left the file: %v", err)
	}
	// Undoing again, with the file gone, is not a failure.
	if err := undo(); err != nil {
		t.Errorf("AC-3 undoing a write whose file is gone failed: %v", err)
	}

	if _, _, err := s.WriteDecisions("A-1", []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	_, undo, err = s.WriteDecisions("A-1", []byte("first\nsecond\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if body, err := os.ReadFile(file); err != nil || string(body) != "first\n" {
		t.Errorf("AC-3 undoing the second write left %q, %v, want what the first wrote", body, err)
	}
}

func TestAC3WriteDecisionsRefusesAnUnsafeIdAndAFileItCannotRead(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if path, undo, err := s.WriteDecisions("../../etc", []byte("x\n")); err == nil || path != "" || undo != nil {
		t.Fatalf("AC-3 a decisions file was written outside the stories directory: %q", path)
	} else if got := codeOf(t, err); got != sdlcerr.UnsafeStoryID {
		t.Errorf("AC-3 code = %s, want %s", got, sdlcerr.UnsafeStoryID)
	}

	// A directory where the file needs to be cannot be read back for the undo,
	// so nothing is written over it.
	dir, err := s.StoryDir("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, model.DecisionsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if path, undo, err := s.WriteDecisions("A-1", []byte("x\n")); err == nil || path != "" || undo != nil {
		t.Fatalf("AC-3 WriteDecisions over a directory said %q", path)
	} else if got := codeOf(t, err); got != sdlcerr.StateUnreadable {
		t.Errorf("AC-3 code = %s, want %s", got, sdlcerr.StateUnreadable)
	}
}

// A story directory the store cannot write in fails the write, and gives no
// undo for a file that was never written.
func TestAC3WriteDecisionsFailsWhereTheFileCannotBeWritten(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("this platform does not restrict a directory owner's own writes by permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	s := newStore(t)
	dir, err := s.StoryDir("A-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if path, undo, err := s.WriteDecisions("A-1", []byte("x\n")); err == nil || path != "" || undo != nil {
		t.Fatalf("AC-3 WriteDecisions in a directory it cannot write said %q", path)
	} else if got := codeOf(t, err); got != sdlcerr.StateUnwritable {
		t.Errorf("AC-3 code = %s, want %s", got, sdlcerr.StateUnwritable)
	}
	if _, err := os.Stat(filepath.Join(dir, model.DecisionsFile)); !os.IsNotExist(err) {
		t.Errorf("AC-3 a failed write left a decisions file: %v", err)
	}
}

func TestAC3RecordPathIsWhereTheGateRecordIsSaved(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	if want := ".sdlc/stories/A-1/gate-record.json"; RecordPath("A-1") != want {
		t.Errorf("AC-3 RecordPath = %q, want %q", RecordPath("A-1"), want)
	}
	if err := s.SaveRecord(model.NewRecord("A-1", fixedTime)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.root, filepath.FromSlash(RecordPath("A-1")))); err != nil {
		t.Errorf("AC-3 no record where RecordPath says: %v", err)
	}
}
