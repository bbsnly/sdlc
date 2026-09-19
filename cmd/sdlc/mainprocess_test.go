package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runBinary runs the real main() as a subprocess -- via mainHelperEnv, set up
// in TestMain -- and reports what it wrote and how it exited. run and crash
// are exercised directly and exhaustively above; this is the one seam that
// proves main actually wires os.Args, the real streams and os.Exit to them,
// rather than, say, dropping run's exit code on the floor.
func runBinary(t *testing.T, args []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
	cmd.Env = append(os.Environ(), mainHelperEnv+"=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		exitCode = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running sdlc: %v", err)
	}
	return out.String(), errb.String(), exitCode
}

func TestMainPrintsToTheRealStdoutAndExitsZero(t *testing.T) {
	out, errb, code := runBinary(t, []string{"version"})
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errb)
	}
	if errb != "" {
		t.Errorf("stderr = %q", errb)
	}

	// run() itself is exercised directly and exhaustively in main_test.go;
	// what this seam has to prove is that main() hands it the real streams
	// and os.Args without dropping or mangling anything on the way, so the
	// subprocess's stdout must be exactly what run() prints for the same
	// args, not merely non-empty.
	var want, wantErr bytes.Buffer
	if wantCode := run([]string{"version"}, strings.NewReader(""), &want, &wantErr, noEnv); wantCode != 0 {
		t.Fatalf("run() itself failed to print a version: exit %d, stderr=%q", wantCode, wantErr.String())
	}
	if out != want.String() {
		t.Errorf("main() printed %q, run() prints %q for the same args", out, want.String())
	}
}

// Without main's os.Exit(run(...)), a failing command would still exit 0 and
// a script chaining sdlc calls would carry on past the failure.
func TestMainForwardsRunsExitCodeOnFailure(t *testing.T) {
	out, errb, code := runBinary(t, []string{"not-a-real-command"})
	if code == 0 {
		t.Fatalf("an unknown command exited 0, stderr=%q", errb)
	}

	var wantOut, wantErr bytes.Buffer
	wantCode := run([]string{"not-a-real-command"}, strings.NewReader(""), &wantOut, &wantErr, noEnv)
	if code != wantCode {
		t.Errorf("main() exited %d, run() exits %d for the same args", code, wantCode)
	}
	if out != wantOut.String() {
		t.Errorf("main() printed %q to stdout, run() prints %q for the same args", out, wantOut.String())
	}
	if !strings.Contains(errb, "not-a-real-command") {
		t.Errorf("stderr does not name the unknown command: %q", errb)
	}
}
