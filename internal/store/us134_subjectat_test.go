package store

// US-134 AC-6: the landing takes the result the implementation gate recorded
// only where the commit that lands is the tree the gate ran on. The gate
// measures the working tree with ReviewSubject; the landing measures the
// commit with ReviewSubjectAt. For a checkout holding the commit's files the
// two have to be one hash, story bookkeeping left out of both, and work on
// disk that nobody committed is in the first and not the second. The gate
// also compares the backlog before and after its commands by BacklogSubject,
// which is the backlog as ReviewSubject measures it.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
)

// atFresh is a store over root with nothing read yet, so that what one
// measurement read is not what the next one is served.
func atFresh(root string, cfg config.Config) *Store {
	return New(&config.Project{Root: root, Config: cfg}).WithClock(func() time.Time { return fixedTime })
}

func atHead(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "HEAD")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func atSubjects(t *testing.T, root string, cfg config.Config, commit string) (onDisk, atCommit string) {
	t.Helper()
	var err error
	if onDisk, err = atFresh(root, cfg).ReviewSubject(t.Context()); err != nil {
		t.Fatalf("ReviewSubject: %v", err)
	}
	if atCommit, err = atFresh(root, cfg).ReviewSubjectAt(t.Context(), commit); err != nil {
		t.Fatalf("ReviewSubjectAt: %v", err)
	}
	return onDisk, atCommit
}

func atWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAC6TheSubjectOfACommitIsTheSubjectOfTheCleanCheckoutThatHoldsIt(t *testing.T) {
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress","updated":"2026-01-02T03:04:05Z"}]}`+"\n")
	atWrite(t, s.root, "src/main.go", "package main\n")
	atWrite(t, s.root, ".sdlc/state/active", "A-1\n")
	commitAll(t, s.root, "work")
	head := atHead(t, s.root)

	onDisk, atCommit := atSubjects(t, s.root, s.cfg, head)
	if onDisk != atCommit {
		t.Fatalf("ReviewSubject of the clean checkout = %s, ReviewSubjectAt of HEAD = %s: the landing would never take the gate's result", onDisk, atCommit)
	}

	// A commit that moved only the story's bookkeeping measures as before.
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"done","updated":"2026-03-04T05:06:07Z"}]}`+"\n")
	commitAll(t, s.root, "bookkeeping")
	if _, got := atSubjects(t, s.root, s.cfg, atHead(t, s.root)); got != atCommit {
		t.Errorf("a commit that moved only status and updated measures %s, want %s", got, atCommit)
	}

	// One that changed the story itself does not.
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One, retitled","status":"done"}]}`+"\n")
	commitAll(t, s.root, "the story")
	disk, got := atSubjects(t, s.root, s.cfg, atHead(t, s.root))
	if got == atCommit {
		t.Errorf("a commit that retitled the story measures as before it, %s", got)
	}
	if got != disk {
		t.Errorf("after the retitle, ReviewSubjectAt of HEAD = %s, ReviewSubject = %s", got, disk)
	}
}

func TestAC6WorkOnDiskThatNobodyCommittedIsNotInTheSubjectOfTheCommit(t *testing.T) {
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress"}]}`+"\n")
	atWrite(t, s.root, "src/main.go", "package main\n")
	commitAll(t, s.root, "work")
	head := atHead(t, s.root)
	_, before := atSubjects(t, s.root, s.cfg, head)

	atWrite(t, s.root, "src/main.go", "package main\n\nfunc main() {}\n")
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One, changed on disk","status":"in_progress"}]}`+"\n")
	onDisk, atCommit := atSubjects(t, s.root, s.cfg, head)
	if atCommit != before {
		t.Errorf("changes on disk moved the commit's subject from %s to %s", before, atCommit)
	}
	if onDisk == atCommit {
		t.Errorf("the working tree with uncommitted work measures as the commit, %s", onDisk)
	}
}

func TestAC6ACommitWithNoBacklogOrOneThatDoesNotParseIsMeasuredAsItsTree(t *testing.T) {
	cases := []struct {
		name    string
		backlog string
	}{
		{name: "AC-6 no backlog in the commit"},
		{name: "AC-6 a backlog that does not parse", backlog: "{\"stories\": [ not json\n"},
		{name: "AC-6 a backlog with no stories array", backlog: "{\"status\": \"ready\"}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStore(t)
			initGit(t, s.root)
			atWrite(t, s.root, "README.md", "# fixture\n")
			if c.backlog != "" {
				writeBacklog(t, s, c.backlog)
			}
			commitAll(t, s.root, "work")
			onDisk, atCommit := atSubjects(t, s.root, s.cfg, atHead(t, s.root))
			if onDisk != atCommit {
				t.Errorf("ReviewSubject = %s, ReviewSubjectAt of HEAD = %s, want one hash", onDisk, atCommit)
			}
		})
	}
}

func TestAC6ABacklogOutsideTheRepositoryIsLeftOutOfTheSubjectOfACommit(t *testing.T) {
	s := newStore(t)
	initGit(t, s.root)
	atWrite(t, s.root, "README.md", "# fixture\n")
	commitAll(t, s.root, "work")
	cfg := s.cfg
	cfg.Backlog.Path = "../elsewhere/user_stories.json"
	onDisk, atCommit := atSubjects(t, s.root, cfg, atHead(t, s.root))
	if onDisk != atCommit {
		t.Errorf("with the backlog outside the repository, ReviewSubject = %s, ReviewSubjectAt = %s", onDisk, atCommit)
	}
	if _, _, ok := atFresh(s.root, cfg).BacklogSubject(); ok {
		t.Errorf("BacklogSubject measured a backlog outside the repository")
	}
}

func TestAC6TheSubjectOfACommitNamedOtherwiseThanByItsFullHashIsAnError(t *testing.T) {
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"ready"}]}`+"\n")
	commitAll(t, s.root, "work")
	for _, name := range []string{"HEAD", atHead(t, s.root)[:12]} {
		if got, err := atFresh(s.root, s.cfg).ReviewSubjectAt(t.Context(), name); err == nil {
			t.Errorf("ReviewSubjectAt(%q) = %s, want an error: a commit is named by its full hash", name, got)
		}
	}
}

func TestAC3TheBacklogIsGivenWithoutItsBookkeepingAsTheSubjectMeasuresIt(t *testing.T) {
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"in_progress","updated":"2026-01-02T03:04:05Z"}]}`+"\n")
	rel, normal, ok := atFresh(s.root, s.cfg).BacklogSubject()
	if !ok {
		t.Fatal("BacklogSubject of a backlog in the repository is not ok")
	}
	if want := filepath.ToSlash(s.cfg.Backlog.Path); rel != want {
		t.Errorf("the backlog's path is %q, want %q, slash-separated from the root", rel, want)
	}
	if bytes.Contains(normal, []byte("in_progress")) || bytes.Contains(normal, []byte("2026-01-02")) {
		t.Errorf("the backlog is given with its bookkeeping: %s", normal)
	}
	if !bytes.Contains(normal, []byte(`"title":"One"`)) {
		t.Errorf("the backlog lost the story itself: %s", normal)
	}

	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One","status":"done","updated":"2026-05-06T07:08:09Z"}]}`+"\n")
	_, moved, _ := atFresh(s.root, s.cfg).BacklogSubject()
	if !bytes.Equal(moved, normal) {
		t.Errorf("a move of status and updated changed the backlog given: %s, then %s", normal, moved)
	}

	raw := "{\"stories\": [ not json\n"
	writeBacklog(t, s, raw)
	if _, got, ok := atFresh(s.root, s.cfg).BacklogSubject(); !ok || string(got) != raw {
		t.Errorf("a backlog that does not parse is given as %q (ok %v), want it as it is", got, ok)
	}

	if err := os.Remove(s.cfg.BacklogPath(s.root)); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := atFresh(s.root, s.cfg).BacklogSubject(); ok {
		t.Errorf("a backlog that is not there is given as measured")
	}
}
