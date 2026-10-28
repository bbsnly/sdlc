package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/gitx"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// ReviewSubject is what a review and a commit are checked against: the
// working tree, less the loop's own directory, and less the two fields sdlc
// writes as a story moves. Waiting for a person and resuming must not disturb
// it; the story itself must.
func TestReviewSubjectIsStableAcrossAStatusMoveButNotOverTheStoryItself(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, "{\"stories\":[{\"id\":\"A-1\",\"title\":\"One\",\"status\":\"ready\"}]}\n")
	commitAll(t, s.root, "story")

	before, err := s.ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetStoryStatus("A-1", model.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	after, err := s.ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Errorf("ReviewSubject changed when only status and updated moved: %q -> %q", before, after)
	}

	writeBacklog(t, s, `{"stories":[{"id":"A-1","title":"One (retitled)","status":"in_progress",`+
		`"updated":"2026-09-10T08:30:00Z"}]}`)
	changed, err := s.ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if changed == after {
		t.Error("ReviewSubject did not change when the story itself changed")
	}
}

// pinnedSubject is what ReviewSubject gave for the tree below when it was
// written. It is the stamp on every review recorded in a project, so a release
// that moved it for the same tree and backlog would stale all of them at once;
// it is also the plain tree hash of that tree with the backlog's status and
// updated fields taken out. A faster ReviewSubject must arrive at the same
// value.
const pinnedSubject = "2fdf8b115ee0e77d4f1b964dd024e169510fc661"

// The fixture is LF only, has no executable bit, and settles in the
// repository's own configuration the two settings that differ by platform
// (core.autocrlf, which Git for Windows' system configuration turns on, and
// core.fileMode, which git init writes differently on Windows), so the tree is
// the same everywhere.
func TestReviewSubjectNeverChangesForTheSameTreeAndBacklog(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	runGit(t, s.root, "config", "core.autocrlf", "false")
	runGit(t, s.root, "config", "core.fileMode", "false")
	writeBacklog(t, s, `{
  "stories": [
    {
      "id": "A-1",
      "title": "One",
      "status": "ready",
      "acceptance_criteria": [{"id": "AC-1", "text": "WHEN it runs, it works."}]
    },
    {
      "id": "A-2",
      "title": "Two",
      "status": "done",
      "updated": "2026-01-02T03:04:05Z"
    }
  ]
}
`)
	for rel, body := range map[string]string{
		"README.md":          "# fixture\n",
		"src/main.go":        "package main\n",
		".sdlc/state/active": "A-1\n",
	} {
		path := filepath.Join(s.root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commitAll(t, s.root, "fixture")

	before, err := s.ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if before != pinnedSubject {
		t.Errorf("ReviewSubject = %s, want %s for the same tree and backlog", before, pinnedSubject)
	}

	if err := s.SetStoryStatus("A-1", model.StatusInProgress); err != nil {
		t.Fatal(err)
	}
	moved, err := os.ReadFile(s.BacklogPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(moved), `"in_progress"`) || !strings.Contains(string(moved), model.Timestamp(fixedTime)) {
		t.Fatalf("the status move did not reach the backlog:\n%s", moved)
	}
	after, err := s.ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if after != pinnedSubject {
		t.Errorf("after a status move, ReviewSubject = %s, want %s: only status and updated moved", after, pinnedSubject)
	}
}

// A backlog kept outside the repository cannot be hashed with an override in
// its place, so the whole tree is what stands in for it.
func TestReviewSubjectFallsBackToTheWholeTreeWhenTheBacklogIsOutsideTheRepository(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	if err := os.WriteFile(filepath.Join(s.root, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, s.root, "base")
	s.cfg.Backlog.Path = "../outside.json"
	outside := filepath.Join(filepath.Dir(s.root), "outside.json")
	if err := os.WriteFile(outside, []byte(`{"stories":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := s.ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want, err := gitx.TreeHash(t.Context(), s.root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("ReviewSubject = %q, want the whole tree's hash %q", got, want)
	}
}

func TestReviewSubjectFallsBackToTheWholeTreeWhenTheBacklogCannotBeRead(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	if err := os.WriteFile(filepath.Join(s.root, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitAll(t, s.root, "base")
	// No backlog written at all.

	got, err := s.ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want, err := gitx.TreeHash(t.Context(), s.root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("ReviewSubject = %q, want %q", got, want)
	}
}

func TestReviewSubjectFallsBackToTheWholeTreeWhenTheBacklogDoesNotParse(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	initGit(t, s.root)
	writeBacklog(t, s, "not json")
	commitAll(t, s.root, "base")

	got, err := s.ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want, err := gitx.TreeHash(t.Context(), s.root)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("ReviewSubject = %q, want %q", got, want)
	}
}

func TestReviewSubjectFailsWithoutAGitRepository(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	writeBacklog(t, s, `{"stories":[]}`)
	if _, err := s.ReviewSubject(t.Context()); codeOf(t, err) != sdlcerr.RepositoryUnreadable {
		t.Errorf("code = %v, want %s", err, sdlcerr.RepositoryUnreadable)
	}
}

// editStory and withoutStoryFields walk the backlog's bytes themselves rather
// than trust encoding/json's view of it, so that every other byte survives
// untouched. Both refuse rather than guess when the bytes are not what a
// backlog with a "stories" array of objects would be.
func TestEditStoryFailsSafelyOnBytesItDoesNotExpect(t *testing.T) {
	t.Parallel()
	change1 := change{"status", jsonString("done")}
	for name, data := range map[string]string{
		"not an object":                          `["stories"]`,
		"no stories key":                         `{"foo":1}`,
		"stories not an array":                   `{"stories":"nope"}`,
		"stories not an object":                  `{"stories":{"a":1}}`,
		"a story is not an object":               `{"stories":[1,2]}`,
		"a story is an empty object":             `{"stories":[{}]}`,
		"a key at the top level is not a string": `{1:2}`,
		"a top-level value is malformed":         `{"stories":}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := editStory([]byte(data), "A-1", change1); ok {
				t.Errorf("editStory accepted %s", data)
			}
		})
	}
}

// A story without the id being edited, or whose id does not match, is passed
// over rather than edited: editStory reports that nothing was found.
func TestEditStoryReportsNoMatchWhenTheIDIsNotInTheBytes(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"a different id": `{"stories":[{"id":"B-2","status":"ready"}]}`,
		"no id at all":   `{"stories":[{"status":"ready"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := editStory([]byte(data), "A-1", change{"status", jsonString("done")}); ok {
				t.Errorf("editStory found a match in %s", data)
			}
		})
	}
}

func TestWithoutStoryFieldsFailsSafelyOnBytesItDoesNotExpect(t *testing.T) {
	t.Parallel()
	for name, data := range map[string]string{
		"not an object":                          `["stories"]`,
		"no stories key":                         `{"foo":1}`,
		"stories not an array":                   `{"stories":"nope"}`,
		"a story is not an object":               `{"stories":[1,2]}`,
		"a key at the top level is not a string": `{1:2}`,
		"a top-level value is malformed":         `{"stories":}`,
		// Removing the field leaves the story with no members at all, and an
		// object with none is not one a story can be (see objectMembers):
		// there is no id or title left to find on the next pass.
		"the matched key is the story's only field": `{"stories":[{"status":"ready"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, ok := withoutStoryFields([]byte(data), "status", "updated"); ok {
				t.Errorf("withoutStoryFields accepted %s", data)
			}
		})
	}
}

// The span removed for a matched key that is not the story's only field
// differs by whether it comes first: firstStoryField takes the comma before
// it when there is one, the comma after it when the match is first.
func TestWithoutStoryFieldsRemovesAMatchedKeyWhicheverPositionItIsIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, data, want string }{
		{
			name: "the matched key is first among several",
			data: `{"stories":[{"status":"ready","id":"A-1"}]}`,
			want: `{"stories":[{"id":"A-1"}]}`,
		},
		{
			name: "the matched key comes after another field",
			data: `{"stories":[{"id":"A-1","status":"ready"}]}`,
			want: `{"stories":[{"id":"A-1"}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := withoutStoryFields([]byte(tc.data), "status", "updated")
			if !ok {
				t.Fatalf("withoutStoryFields refused %s", tc.data)
			}
			if string(got) != tc.want {
				t.Errorf("withoutStoryFields(%s) = %s, want %s", tc.data, got, tc.want)
			}
		})
	}
}
