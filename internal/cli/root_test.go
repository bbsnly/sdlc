package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// quietExit's report is the command's own -- doctor's checks, printed to
// stdout -- so an empty "sdlc: " line on stderr would be noise nobody asked
// for.
func TestQuietExitCarriesNoMessageOfItsOwn(t *testing.T) {
	t.Parallel()
	if got := (quietExit{code: 1}).Error(); got != "" {
		t.Errorf("Error() = %q, want empty", got)
	}
}

// The lock lives in the user's cache directory, not the repository, and a
// project cannot be locked while that directory cannot be created. Two
// clones of the same repository share nothing here, so this is the user's
// machine being unwritable, not the project's.
func TestAWriteIsRefusedWhenTheLockDirectoryCannotBeCreated(t *testing.T) {
	gitProjectInitialised(t)

	home := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		blocked := filepath.Join(home, "blocked")
		if err := os.WriteFile(blocked, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("LOCALAPPDATA", blocked)
	case "darwin":
		if err := os.MkdirAll(filepath.Join(home, "Library"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "Library", "Caches"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HOME", home)
	default:
		blocked := filepath.Join(home, "blocked")
		if err := os.WriteFile(blocked, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Setenv("XDG_CACHE_HOME", blocked)
	}

	r := run(t, "start")
	if r.code == 0 {
		t.Fatal("start went ahead with the lock directory blocked")
	}
	if !strings.Contains(r.stderr, "lock directory could not be created") {
		t.Errorf("stderr = %s", r.stderr)
	}
}
