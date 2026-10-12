// US-003, sdlc doctor and the shell operators inside a quoted string: the
// configured commands.fmt_file of AC-2, put to the built binary in a real
// repository. internal/cli holds the same behaviour without a build.

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// AC-2: the awk and go fmt command, exactly as the story quotes it, is no
// `commands on PATH` problem and doctor exits 0. A directory holding the two
// programs and the built binary is the whole PATH, so the answer does not
// depend on what the developer has installed, and `go` being looked up is not
// asserted (the VAR=value blind spot is out of scope for this story).
func TestAC2SdlcDoctorAcceptsTheAwkAndGoFmtCommandWithOperatorsInQuotes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the command is POSIX shell and the stand-in programs are symlinks; doctor's lookup is held by internal/cli on every platform")
	}
	binary := build(t)
	root := project(t, binary)

	bin := t.TempDir()
	for _, program := range []string{"awk", "go", "gofmt", "git", "sed", "head", "grep", "tail", "sh"} {
		found, err := exec.LookPath(program)
		if err != nil {
			continue
		}
		if err := os.Symlink(found, filepath.Join(bin, program)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(bin, "awk")); err != nil {
		t.Fatalf("this machine has no awk, which AC-2 needs installed: %v", err)
	}
	if err := os.Symlink(binary, filepath.Join(bin, "sdlc")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	const command = `case "$FILE" in *.go) t=$(awk '/^toolchain /{print $2; exit}' go.mod); GOTOOLCHAIN="${t:-local}" go fmt "$FILE" >/dev/null ;; esac`
	for _, key := range []string{"build", "test", "lint", "fmt", "fmt_check", "coverage", "smoke", "npm_test", "mutation"} {
		setConfig(t, root, "commands", key, nil)
	}
	setConfig(t, root, "commands", "build", "go build ./...")
	setConfig(t, root, "commands", "test", "go test ./...")
	setConfig(t, root, "commands", "fmt_file", command)

	out, code := printed(t, binary, root, "doctor", "--json")
	var payload struct {
		Checks []struct {
			Name   string `json:"name"`
			State  string `json:"state"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("sdlc doctor --json is not JSON: %v\n%s", err, out)
	}
	for _, c := range payload.Checks {
		if c.Name == "commands on PATH" {
			t.Errorf("doctor reports a commands on PATH problem: %s", c.Detail)
		}
	}
	if code != 0 {
		t.Errorf("sdlc doctor exited %d, want 0:\n%s", code, strings.TrimSpace(out))
	}
}
