package shellx

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Command hands the string to a real POSIX shell, run from the directory it
// was given: the loop's configured commands are written for sh, and Claude
// Code on Windows runs them under Git Bash, which has one.
//
// US-050, AC-6: this compared the shell's `pwd` with the directory, and Git
// Bash spells a drive-letter path in its own /c/... form, not the Windows
// form, so it failed on Windows with the shell where it should be. What is
// held here instead holds on every platform: a file the test put in the
// directory is read by a relative name, and one the command makes by a
// relative name appears in the directory. The command makes it only once the
// read has worked, so a shell started anywhere else writes nothing there.
func TestAC6CommandRunsThroughAPOSIXShellFromTheDirectoryItIsGiven(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "given"), []byte("the given directory\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd, err := Command(t.Context(), dir, `cat ./given && printf '%s\n' "$((2 + 3))" > made-here`)
	if errors.Is(err, ErrNoShell) {
		t.Skip("neither sh nor bash is on PATH")
	}
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	if got := strings.TrimSpace(out.String()); got != "the given directory" {
		t.Errorf("the command read ./given as %q, want the file in the directory Command was given", got)
	}
	made, err := os.ReadFile(filepath.Join(dir, "made-here"))
	if err != nil {
		t.Fatalf("the file the command made by a relative name is not in the directory Command was given: %v", err)
	}
	if got := strings.TrimSpace(string(made)); got != "5" {
		t.Errorf("$((2 + 3)) wrote %q, want 5 from a POSIX shell's arithmetic", got)
	}
}

// The exit status a command finishes with is the shell's own, not something
// Command has an opinion about.
func TestCommandsExitStatusIsTheShells(t *testing.T) {
	cmd, err := Command(t.Context(), t.TempDir(), "exit 3")
	if errors.Is(err, ErrNoShell) {
		t.Skip("neither sh nor bash is on PATH")
	}
	if err != nil {
		t.Fatal(err)
	}
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("exit 3 finished with %v, want an exit error", err)
	}
	if exit.ExitCode() != 3 {
		t.Errorf("exit code = %d, want 3", exit.ExitCode())
	}
}

// Standard output and standard error are the shell's own streams, kept apart:
// a command's failure is read from stderr, not guessed at from a mix of both.
func TestCommandKeepsStdoutAndStderrApart(t *testing.T) {
	cmd, err := Command(t.Context(), t.TempDir(), "echo out; echo err >&2")
	if errors.Is(err, ErrNoShell) {
		t.Skip("neither sh nor bash is on PATH")
	}
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(stdout.String()); got != "out" {
		t.Errorf("stdout = %q, want %q", got, "out")
	}
	if got := strings.TrimSpace(stderr.String()); got != "err" {
		t.Errorf("stderr = %q, want %q", got, "err")
	}
}

// A command a project configures reads the same environment the process
// running it has, since it is not told to start from nothing.
func TestCommandInheritsTheProcessEnvironment(t *testing.T) {
	t.Setenv("SDLCX_SHELLX_TEST", "carried")
	cmd, err := Command(t.Context(), t.TempDir(), "echo $SDLCX_SHELLX_TEST")
	if errors.Is(err, ErrNoShell) {
		t.Skip("neither sh nor bash is on PATH")
	}
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != "carried" {
		t.Errorf("the shell saw %q for the variable, want %q", got, "carried")
	}
}

// Neither sh nor bash on PATH is the one way a project's command cannot run
// at all, told apart from the command itself failing.
func TestCommandFailsWithoutAShellOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Command(t.Context(), t.TempDir(), "true"); !errors.Is(err, ErrNoShell) {
		t.Errorf("Command with no shell on PATH = %v, want %v", err, ErrNoShell)
	}
}

func TestTailKeepsOnlyTheEnd(t *testing.T) {
	for _, c := range []struct {
		name, output string
		limit        int
		want         string
	}{
		{"shorter than the limit is kept whole", "  a failure\n", 20, "a failure"},
		{"longer than the limit is cut from the front", strings.Repeat("x", 10) + "tail", 4, "...tail"},
		{"empty output stays empty", "   \n", 10, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := Tail(c.output, c.limit); got != c.want {
				t.Errorf("Tail(%q, %d) = %q, want %q", c.output, c.limit, got, c.want)
			}
		})
	}
}
