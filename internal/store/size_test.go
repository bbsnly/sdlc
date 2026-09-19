package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// ChangeSize counts the work, not the loop's own bookkeeping: moving a story
// touches the backlog, and that must not count against the diff size cap a
// story's own code is measured against.
func TestChangeSizeCountsChangedLinesButNotTheBacklogMove(t *testing.T) {
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"ready"}]}`)
	commitAll(t, s.root, "base")

	if err := s.SetStoryStatus("A-1", model.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "code.go"),
		[]byte("package x\n\nfunc f() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := s.ChangeSize(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("ChangeSize = %d, want 3: the backlog's own move must not count", got)
	}
}

func TestChangeSizeFailsWithoutAGitRepository(t *testing.T) {
	s := newStore(t)
	if _, err := s.ChangeSize(t.Context()); codeOf(t, err) != sdlcerr.RepositoryUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.RepositoryUnreadable)
	}
}
