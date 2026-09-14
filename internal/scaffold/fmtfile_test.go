package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// commands.fmt_file runs on every file a tool writes, and `gofmt -w` on a
// Markdown file fails. The command `sdlc init` writes has to leave a file its
// formatter does not understand alone, rather than report a failure on every
// document the session touches.
func TestAFormatterForOneLanguageLeavesOtherFilesAlone(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		if shell, err = exec.LookPath("bash"); err != nil {
			t.Skip("no sh or bash to run the command with")
		}
	}
	for name, detect := range map[string]struct {
		marker string
		stack  func(string) (Stack, bool)
	}{
		"Go":     {"go.mod", detectGo},
		"Python": {"pyproject.toml", detectPython},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, detect.marker), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			stack, ok := detect.stack(root)
			if !ok || stack.Commands["fmt_file"] == "" {
				t.Fatalf("%s writes no fmt_file", name)
			}
			notes := filepath.Join(root, "NOTES.md")
			if err := os.WriteFile(notes, []byte("# Notes\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := exec.CommandContext(t.Context(), shell, "-c", stack.Commands["fmt_file"])
			cmd.Dir = root
			// An empty PATH: were the formatter run at all, it would fail
			// to be found.
			cmd.Env = []string{"FILE=" + notes, "PATH="}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("fmt_file %q failed on a Markdown file: %v\n%s", stack.Commands["fmt_file"], err, out)
			}
		})
	}
}
