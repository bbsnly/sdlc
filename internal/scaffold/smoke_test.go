package scaffold

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// `sdlc start` runs commands.smoke before each new story, and the first story
// of a new Go project starts from a go.mod with no packages in it. The smoke
// check `sdlc init` used to write, `go build ./... && go vet ./...`, fails on
// exactly that, so it would have refused every new Go project's first story.
func TestTheGoSmokeCheckPassesOnAModuleWithNothingInItYet(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to run the command with")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go to run the command with")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stack, ok := detectGo(root)
	if !ok {
		t.Fatal("a go.mod is not a Go project")
	}
	smoke := func() error {
		cmd := exec.CommandContext(t.Context(), shell, "-c", stack.Commands["smoke"])
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("%s", out)
		}
		return err
	}

	if err := smoke(); err != nil {
		t.Errorf("smoke %q failed on an empty module: %v", stack.Commands["smoke"], err)
	}

	// And it is still a check: a package that does not compile fails it.
	if err := os.WriteFile(filepath.Join(root, "broken.go"), []byte("package demo\n\nfunc Broken() { x }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if smoke() == nil {
		t.Errorf("smoke %q passed on a package that does not compile", stack.Commands["smoke"])
	}
}
