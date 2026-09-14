//go:build windows

package pathrules

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A junction needs no privilege to make, and filepath.EvalSymlinks does not
// follow one. A junction to .sdlc put the freeze at `j/state/tests.lock`, and
// one to .git put the hooks at `g/hooks/pre-commit`: paths no rule names.
func TestRelFollowsAJunctionIntoAProtectedDirectory(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{".sdlc/state", ".git/hooks"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, ".sdlc", "state", "tests.lock"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"j": ".sdlc", "g": ".git"} {
		out, err := exec.CommandContext(t.Context(), "cmd", "/c", "mklink", "/J",
			filepath.Join(root, link), filepath.Join(root, target)).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J %s %s: %v\n%s", link, target, err, out)
		}
	}

	for through, want := range map[string]string{
		`j\state\tests.lock`: ".sdlc/state/tests.lock", // there
		`j\state\active`:     ".sdlc/state/active",     // not yet
		`g\hooks\pre-commit`: ".git/hooks/pre-commit",
		`j\stories\A-1\x.md`: ".sdlc/stories/A-1/x.md", // several parts not yet there
	} {
		rel, outside := Rel(root, filepath.Join(root, through))
		if outside || rel != want {
			t.Errorf("Rel(%s) = %q (outside %v), want %q", through, rel, outside, want)
		}
	}
}
