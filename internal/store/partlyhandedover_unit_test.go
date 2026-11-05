package store

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// A hand-over that could not be taken back is told apart from any other refusal,
// through wrapping, and reads as the refusal it carries.
func TestPartlyHandedOverTellsAHandOverThatCouldNotBeTakenBackFromAnyOther(t *testing.T) {
	inner := sdlcerr.New(sdlcerr.ConfigInvalid, "what", "why")
	partly := partlyHandedOverError{err: inner}
	if !PartlyHandedOver(partly) || !PartlyHandedOver(fmt.Errorf("stop: %w", partly)) {
		t.Error("PartlyHandedOver does not find the hand-over, alone or wrapped")
	}
	if PartlyHandedOver(inner) || PartlyHandedOver(errors.New("other")) || PartlyHandedOver(nil) {
		t.Error("PartlyHandedOver answers for an error that is not one")
	}
	if partly.Error() != inner.Error() {
		t.Errorf("the hand-over reads %q, want the refusal it carries, %q", partly.Error(), inner.Error())
	}
	var carried *sdlcerr.Error
	if !errors.As(partly, &carried) || carried != inner {
		t.Error("the hand-over does not unwrap to the refusal it carries")
	}
}

// The list of the agent host's session markers is a copy: a caller that writes to
// its own changes no other caller's.
func TestClaudeCodeMarkersIsACopyNoCallerCanWriteTo(t *testing.T) {
	first := ClaudeCodeMarkers()
	if !slices.Contains(first[:], SessionEnv) || !slices.Contains(first[:], AgentEnv) {
		t.Errorf("the markers %q do not hold the session and agent variables", first)
	}
	first[0] = "changed"
	if ClaudeCodeMarkers()[0] == "changed" {
		t.Error("a caller's change to the markers reached the next caller")
	}
}

// A store given its own environment reads the session through it, not through the
// process's: tests run many commands at once in one process.
func TestWithEnvHasTheStoreReadItsOwnEnvironment(t *testing.T) {
	s := newStore(t)
	t.Setenv(SessionEnv, "from-the-process")
	if got := s.WithEnv(func(name string) string {
		if name == SessionEnv {
			return "from-the-command"
		}
		return ""
	}); got != s {
		t.Fatal("WithEnv does not return the store it was asked of")
	}
	if got := s.getenv(SessionEnv); got != "from-the-command" {
		t.Errorf("the store reads %s as %q, want the command's own", SessionEnv, got)
	}
}
