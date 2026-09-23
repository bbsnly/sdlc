package store

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/gitx"
)

// Two commits nothing in this package has to have made: what is written down is
// a name, and whether the repository holds a commit by that name is the gate's
// question, asked of git, not this one.
const (
	headOne = "1111111111111111111111111111111111111111"
	headTwo = "2222222222222222222222222222222222222222"
)

// observing is a store on a real repository, because what the loop watched is
// kept in the repository's git directory.
func observing(t *testing.T) *Store {
	t.Helper()
	s := newStore(t)
	initGit(t, s.root)
	return s
}

func observed(t *testing.T, s *Store, id string) []string {
	t.Helper()
	got, err := s.ObservedCommits(t.Context(), id)
	if err != nil {
		t.Fatalf("ObservedCommits(%q): %v", id, err)
	}
	slices.Sort(got)
	return got
}

// observationOnDisk finds the file one observation was written to, and the
// directory holding it, without this test having to know where the format puts
// them: an observation is named by the head it watched.
func observationOnDisk(t *testing.T, s *Store, head string) (dir string, body []byte) {
	t.Helper()
	gitDir := filepath.Join(s.root, ".git")
	walked := filepath.WalkDir(gitDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != head {
			return err
		}
		dir = filepath.Dir(path)
		read, err := os.ReadFile(path)
		body = read
		return err
	})
	if walked != nil {
		t.Fatal(walked)
	}
	if dir == "" {
		t.Fatalf("no observation named %s was written anywhere under %s", head, gitDir)
	}
	return dir, body
}

// otherSchema is the same observation claiming a format this version has never
// heard of.
func otherSchema(t *testing.T, good []byte) string {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal(good, &fields); err != nil {
		t.Fatalf("an observation is not a JSON object: %v (%q)", err, good)
	}
	named := false
	for k, v := range fields {
		if s, ok := v.(string); ok && (k == "schema" || strings.HasPrefix(s, "sdlc/")) {
			fields[k] = "sdlc/not-a-format/9999"
			named = true
		}
	}
	if !named {
		t.Error("an observation carries no schema version, so a file written by a later " +
			"version cannot be told from one this version wrote")
	}
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// What one process watched is read back by another as the evidence for what a
// story committed, so the round trip is the whole of this format's job.
func TestObservedCommitsHoldsEveryHeadTheLoopWatchedAndNothingElse(t *testing.T) {
	s := observing(t)
	if got := observed(t, s, "A-1"); len(got) != 0 {
		t.Errorf("a story nobody watched has observations %v", got)
	}
	for _, head := range []string{headOne, headTwo} {
		if err := s.ObserveCommit(t.Context(), "A-1", head); err != nil {
			t.Fatal(err)
		}
	}
	// The same head is watched twice in an ordinary run: Inspect reads a
	// PowerShell command twice over, and a rehearsal comes before the commit.
	// An observation is a fact about a head, not a count of anything.
	if err := s.ObserveCommit(t.Context(), "A-1", headOne); err != nil {
		t.Fatal(err)
	}
	if got, want := observed(t, s, "A-1"), []string{headOne, headTwo}; !slices.Equal(got, want) {
		t.Errorf("ObservedCommits = %v, want %v", got, want)
	}

	if err := s.ClearObservedCommits(t.Context(), "A-1"); err != nil {
		t.Fatal(err)
	}
	if got := observed(t, s, "A-1"); len(got) != 0 {
		t.Errorf("ObservedCommits after clearing = %v, want none", got)
	}
	// An iteration ends by clearing, and it ends whether anything was watched
	// or not, so clearing what is already clear is not a failure.
	if err := s.ClearObservedCommits(t.Context(), "A-1"); err != nil {
		t.Errorf("ClearObservedCommits with nothing watched: %v", err)
	}
}

// A file left behind by the story before this one is the failure the freeze
// already had to be taught about, which is why tests.lock names its story. A
// range built from another story's commits attributes that story's work to this
// one, and nothing in the record would say so.
func TestObservationsBelongToOneStoryAndAreClearedWithIt(t *testing.T) {
	s := observing(t)
	for id, head := range map[string]string{"A-1": headOne, "B-2": headTwo} {
		if err := s.ObserveCommit(t.Context(), id, head); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := observed(t, s, "A-1"), []string{headOne}; !slices.Equal(got, want) {
		t.Errorf("A-1 watched %v, want %v", got, want)
	}
	if err := s.ClearObservedCommits(t.Context(), "A-1"); err != nil {
		t.Fatal(err)
	}
	if got, want := observed(t, s, "B-2"), []string{headTwo}; !slices.Equal(got, want) {
		t.Errorf("clearing A-1 left B-2 with %v, want %v", got, want)
	}
}

// The gate believes this file, so anything the reader cannot recognise has to
// read as no observations at all -- never as evidence. Read as evidence,
// whatever somebody left in the directory becomes a range the record then
// states as fact; read as nothing, the gate falls back to what it recorded
// before any of this existed.
//
// Each body below names a head the story never committed on, so a reader that
// believes one of them says so by listing it.
func TestAnObservationThatCannotBeRecognisedIsReadAsNoneRatherThanAsEvidence(t *testing.T) {
	s := observing(t)
	if err := s.ObserveCommit(t.Context(), "A-1", headOne); err != nil {
		t.Fatal(err)
	}
	dir, good := observationOnDisk(t, s, headOne)
	elsewhere := func(head string) string { return strings.ReplaceAll(string(good), headOne, head) }

	for _, tc := range []struct{ why, name, body string }{
		{
			"what WriteFileAtomic leaves behind when the machine stops mid-write",
			".tmp-half-written", elsewhere(strings.Repeat("7", 40)),
		},
		{
			"a name that is not a commit's",
			"not-a-commit", elsewhere(strings.Repeat("8", 40)),
		},
		{"a file that will not parse", strings.Repeat("3", 40), "{not json at all"},
		{"a file with nothing in it", strings.Repeat("4", 40), ""},
		{
			"an observation of another story, left in this one's directory",
			strings.Repeat("5", 40),
			strings.ReplaceAll(elsewhere(strings.Repeat("9", 40)), "A-1", "B-2"),
		},
		{
			"a format this version has never heard of",
			strings.Repeat("6", 40), otherSchema(t, good),
		},
	} {
		t.Run(tc.why, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.Remove(path) }()

			if got, want := observed(t, s, "A-1"), []string{headOne}; !slices.Equal(got, want) {
				t.Errorf("ObservedCommits = %v, want only the observation this loop wrote, %v", got, want)
			}
		})
	}
}

// An observation written anywhere in the working tree is seen by `git status`,
// and the commit gate refuses a tree with anything uncommitted in it: the tool
// would be trapping a finished story on its own note to itself. Where this goes
// is not a matter of taste, and this is the measurement that decides it.
func TestWritingDownWhatWasWatchedLeavesTheWorkingTreeClean(t *testing.T) {
	s := observing(t)
	if err := s.ObserveCommit(t.Context(), "A-1", headOne); err != nil {
		t.Fatal(err)
	}
	// A tree nothing was written into is clean for the wrong reason.
	if got := observed(t, s, "A-1"); len(got) != 1 {
		t.Fatalf("nothing was written down at all: %v", got)
	}
	clean, err := gitx.Clean(t.Context(), s.root)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		changes, _ := gitx.Changes(t.Context(), s.root)
		t.Errorf("writing down a watched commit left the tree uncommitted: %v", changes)
	}
}

// A story id becomes part of a path, and it arrives here from a state file the
// hook read, on the fast path of every allowed commit.
func TestAnObservationCannotEscapeTheStoryItBelongsTo(t *testing.T) {
	s := observing(t)
	for _, id := range []string{"../../../etc", "..", "a/b", "", "x/../../y"} {
		if err := s.ObserveCommit(t.Context(), id, headOne); err == nil {
			t.Errorf("ObserveCommit for the story %q was allowed", id)
		}
		if got, err := s.ObservedCommits(t.Context(), id); err == nil {
			t.Errorf("ObservedCommits for the story %q = %v, nil; want a refusal", id, got)
		}
		if err := s.ClearObservedCommits(t.Context(), id); err == nil {
			t.Errorf("ClearObservedCommits for the story %q was allowed", id)
		}
	}
}

// gitx.Head answers a repository with no commits yet with git's all-zero name,
// which is forty hexadecimal characters and passes any check that an
// observation is named by a commit. No commit ever has it as a first parent --
// a root commit has none at all -- so a range built on it is built on nothing.
func TestTheAllZeroHeadIsNeverAnObservation(t *testing.T) {
	s := observing(t)
	// Refusing to write it and ignoring it on the way back both answer this;
	// what must not happen is a range being built from it. The real head
	// beside it is the control: an empty answer has to be this rule, not a
	// store that wrote nothing down either way.
	_ = s.ObserveCommit(t.Context(), "A-1", gitx.NoCommits)
	if err := s.ObserveCommit(t.Context(), "A-1", headOne); err != nil {
		t.Fatal(err)
	}
	if got, want := observed(t, s, "A-1"), []string{headOne}; !slices.Equal(got, want) {
		t.Errorf("ObservedCommits = %v, want %v: the all-zero head counts as no commit", got, want)
	}
}

// The recorder runs on the hook's commit path, where every failure ends in the
// command being allowed anyway. It has to fail rather than panic, and a failure
// to read has to answer "nothing was watched" rather than something the gate
// would weigh as evidence.
func TestObservingOutsideARepositoryFailsAndReadsAsNothingWatched(t *testing.T) {
	s := newStore(t) // no git repository at all
	if err := s.ObserveCommit(t.Context(), "A-1", headOne); err == nil {
		t.Error("an observation was written outside a repository")
	}
	if got, _ := s.ObservedCommits(t.Context(), "A-1"); len(got) != 0 {
		t.Errorf("ObservedCommits outside a repository = %v, want none", got)
	}
}
