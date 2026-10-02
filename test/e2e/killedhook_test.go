package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestAC2AndAC4ATimingLineIsNeverWrittenForAKilledHook kills a real hook
// process while it is blocked reading its payload -- the only way a hook
// actually stops, since this is what Claude Code's own timeout does too --
// and checks that the debug log it was told to keep has no "stage" entry,
// the line SDLC_TRACE=1 asks for.
//
// AC-4's evidence: logging.Stage (internal/logging/logging.go) is only ever
// run from a deferred call in cmd/sdlc/main.go, and nothing that ends the
// process early -- a kill, or exitOnInterrupt's os.Exit -- runs a deferred
// function. So no stage line is written for a killed call regardless of
// where in it the kill lands, which is why this test does not need to catch
// the process mid-read to prove it: killing it at all, before it would
// otherwise finish, which it never does here, is already that case.
//
// It fails today because docs/troubleshooting.md's "Warnings the hook
// prints" section puts "and SDLC_TRACE=1 says how long each call took"
// straight after describing the killed call, with nothing to say the
// promise is about a different, finished one.
func TestAC2AndAC4ATimingLineIsNeverWrittenForAKilledHook(t *testing.T) {
	binary := build(t)
	debugFile := filepath.Join(t.TempDir(), "hook.log")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	cmd := exec.CommandContext(t.Context(), binary, "hook", "PreToolUse")
	cmd.Stdin = r
	cmd.Env = append(os.Environ(), "SDLC_DEBUG_FILE="+debugFile, "SDLC_TRACE=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the hook: %v", err)
	}

	// hook.Run's first act, unconditionally, is reading the whole payload
	// (internal/hook/hook.go), and w is never written to or closed, so the
	// process can only be blocked there, never finished, until this kills it.
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("killing the hook: %v", err)
	}
	_ = cmd.Wait() // "signal: killed" on every platform this builds for; the wait is for the OS to be done with it, not for a particular error

	log, readErr := os.ReadFile(debugFile)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	if strings.Contains(string(log), "msg=stage") {
		t.Fatalf("a killed hook left a stage/timing line, so this test is not measuring what it thinks:\n%s", log)
	}

	warnings := section(readFile(t, filepath.Join("..", "..", "docs", "troubleshooting.md")),
		"## Warnings the hook prints")
	flat := strings.Join(strings.Fields(warnings), " ")
	var traceSentence string
	for _, s := range sentences.Split(flat, -1) {
		if strings.Contains(s, "SDLC_TRACE=1") {
			traceSentence = s
			break
		}
	}
	if traceSentence == "" {
		t.Fatal(`docs/troubleshooting.md's "Warnings the hook prints" section no longer mentions SDLC_TRACE=1`)
	}
	if !strings.Contains(traceSentence, "finish") {
		t.Errorf("docs/troubleshooting.md does not say SDLC_TRACE=1 covers the calls that finish; "+
			"it reads %q, which a killed call that writes no stage line at all contradicts", traceSentence)
	}
}
