package store

// US-009: the `why` of a file that could not be opened says which file it was.
//
// One worker opens the file for three kinds of caller, and its open failure
// used to give all of them the sentence that is right for the freeze only:
//
//	Store.HashFile      -- the freeze's test files (AC-5: the sentence stays)
//	Store.HashArtifact  -- a story's plan, from the design review (AC-3)
//	Store.RecordProgress -- a story's gate record, from the stop guard (AC-4)
//
// A file that is not there fails at the open the same way one that cannot be
// read does, so none of these needs permissions taken away and none is skipped
// on a platform that ignores them.

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// ufRefusal is the sdlc error err carries, which must be a file that could
// not be read.
func ufRefusal(t *testing.T, what string, err error) *sdlcerr.Error {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("%s: %v carries no sdlc error", what, err)
	}
	if e.Code != sdlcerr.StateUnreadable {
		t.Fatalf("%s: code = %s, want %s: both failures keep the code they have", what, e.Code, sdlcerr.StateUnreadable)
	}
	return e
}

// ufNotTheFreeze holds a `why` to not blaming the freeze or a test file, and to
// saying something about the file at all.
func ufNotTheFreeze(t *testing.T, what string, e *sdlcerr.Error) {
	t.Helper()
	for _, banned := range []string{"freeze", "test file"} {
		if strings.Contains(strings.ToLower(e.Why), banned) {
			t.Errorf("%s: why = %q, which mentions %q for a file that is not a test", what, e.Why, banned)
		}
	}
	if !strings.Contains(e.Why, "opened") && !strings.Contains(e.Why, "read") {
		t.Errorf("%s: why = %q, which says nothing of the file not being opened or read", what, e.Why)
	}
}

// ---------------------------------------------------------------- AC-3

func TestAC3APlanThatCannotBeOpenedIsSaidToBeThePlanAndNotTheFreeze(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	plan, ok := model.FindArtifact("plan")
	if !ok {
		t.Fatal("the loop has no plan artefact")
	}
	_, err := s.HashArtifact("A-1", plan)
	if err == nil {
		t.Fatal("a plan that is not there was hashed")
	}
	e := ufRefusal(t, "HashArtifact(plan)", err)
	ufNotTheFreeze(t, "HashArtifact(plan)", e)

	if !strings.Contains(e.What, ".sdlc/stories/A-1/PLAN.md") {
		t.Errorf("what = %q, want it to keep naming the plan's path", e.What)
	}
	// Case matters: the path above says PLAN, and the sentence has to say the
	// plan in words of its own.
	if !regexp.MustCompile(`\bplan\b.*\bcould not be (read|opened)`).MatchString(e.Why) {
		t.Errorf("why = %q, want it to say the plan could not be read", e.Why)
	}
}

// ---------------------------------------------------------------- AC-4

func TestAC4AGateRecordThatCannotBeOpenedIsNotBlamedOnTheFreeze(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	_, err := s.RecordProgress("A-1")
	if err == nil {
		t.Fatal("a record that is not there was hashed")
	}
	e := ufRefusal(t, "RecordProgress", err)
	ufNotTheFreeze(t, "RecordProgress", e)

	if want := ".sdlc/stories/A-1/gate-record.json could not be read"; e.What != want {
		t.Errorf("what = %q, want %q", e.What, want)
	}
}

// ---------------------------------------------------------------- AC-5 (guard: passes today)

func TestAC5TheFreezeStillSaysItRecordsWhatATestFileContainsAndThisOneCouldNotBeOpened(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	_, err := s.HashFile("internal/invoice_test.go")
	if err == nil {
		t.Fatal("a test file that is not there was hashed")
	}
	e := ufRefusal(t, "HashFile", err)
	if want := "internal/invoice_test.go could not be read"; e.What != want {
		t.Errorf("what = %q, want %q", e.What, want)
	}
	for _, want := range []string{
		"the freeze records what a test file contains",
		"this one could not be opened",
	} {
		if !strings.Contains(e.Why, want) {
			t.Errorf("why = %q, want it to say %q", e.Why, want)
		}
	}
}

// What the freeze says about a file that is a test is not given to a document
// by the change that took it from the others: the three callers differ.
func TestAC5TheFreezesSentenceBelongsToTheFreezeAlone(t *testing.T) {
	t.Parallel()
	s := newStore(t)
	plan, _ := model.FindArtifact("plan")
	_, testErr := s.HashFile("a_test.go")
	_, planErr := s.HashArtifact("A-1", plan)
	_, recErr := s.RecordProgress("A-1")
	why := map[string]string{
		"test file": ufRefusal(t, "HashFile", testErr).Why,
		"plan":      ufRefusal(t, "HashArtifact", planErr).Why,
		"record":    ufRefusal(t, "RecordProgress", recErr).Why,
	}
	if why["test file"] == why["plan"] || why["test file"] == why["record"] {
		t.Errorf("the freeze's sentence is shared with a file that is not a test: %v", why)
	}
}
