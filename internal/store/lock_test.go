package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// A freeze in a shape a newer sdlc wrote is refused, not read as a freeze of
// nothing: every field this build looked for would have been empty.
func TestAFreezeInAnUnknownFormatIsRefused(t *testing.T) {
	s := newStore(t)
	path := filepath.Join(s.root, ".sdlc", "state", "tests.lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(`{"schema":"sdlc/tests-lock/2","story":"A-1","hashes":{"a_test.go":"00"}}`)
	if lock, err := s.Lock(); err == nil {
		t.Errorf("a freeze in an unknown format was read, as %+v", lock)
	} else if got := codeOf(t, err); got != sdlcerr.StateUnreadable || !IsUnknownFormat(err) {
		t.Errorf("code = %s, want %s for a format this build does not read: %v", got, sdlcerr.StateUnreadable, err)
	}

	// Not every freeze that will not read is one a newer sdlc wrote.
	write("{not json")
	if _, err := s.Lock(); err == nil || IsUnknownFormat(err) {
		t.Errorf("a freeze that is not JSON was taken for one in a newer format: %v", err)
	}

	write(`{"schema":"sdlc/tests-lock/1","story":"A-1","files":{"a_test.go":"00"}}`)
	if lock, err := s.Lock(); err != nil || !lock.Holds("a_test.go") {
		t.Errorf("a freeze in this build's format did not read: %+v, %v", lock, err)
	}
}
