package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/logging"
)

func noEnv(string) string { return "" }

func TestVersionPrintsToStdout(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errb, noEnv); code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errb.String())
	}
	if strings.TrimSpace(out.String()) == "" {
		t.Error("version printed nothing")
	}
}

func TestHookAlwaysEmitsValidJSON(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"hook", "PreToolUse"}, strings.NewReader(`{"tool_name":"Bash"}`), &out, &errb, noEnv)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var d map[string]any
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("hook stdout is not JSON: %v (%q)", err, out.String())
	}
}

// The green-when for this row: a panic never leaks a stack to stdout.
func TestPanicNeverLeaksAStackToStdout(t *testing.T) {
	boom := func(string) string { panic("boom: a bug in the tool") }

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"cli", []string{"version"}},
		{"hook", []string{"hook", "PreToolUse"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			code := run(tc.args, strings.NewReader(""), &out, &errb, boom)

			if code != 1 {
				t.Errorf("a crash should exit 1, got %d", code)
			}
			if strings.Contains(out.String(), "goroutine ") || strings.Contains(out.String(), ".go:") {
				t.Errorf("stack leaked to stdout: %q", out.String())
			}
			if !strings.Contains(errb.String(), "This is a bug") {
				t.Errorf("stderr should tell the user what to do, got %q", errb.String())
			}
		})
	}
}

// A crash in a hook must not wedge the session: the tool failing is not a
// reason the user cannot keep working.
func TestHookCrashStillEmitsParseableJSONAndContinues(t *testing.T) {
	boom := func(string) string { panic("boom") }
	var out, errb bytes.Buffer
	_ = run([]string{"hook", "PreToolUse"}, strings.NewReader(""), &out, &errb, boom)

	var d struct {
		Continue   bool   `json:"continue"`
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("crashed hook stdout is not JSON: %v (%q)", err, out.String())
	}
	if !d.Continue {
		t.Error("a crash in the tool must not block the user's action")
	}
	if !strings.Contains(d.StopReason, "crashed") {
		t.Errorf("the reason should say what happened, got %q", d.StopReason)
	}
}

func TestCrashIncludesStackOnStderrOnlyWhenAskedAndNeverOnStdout(t *testing.T) {
	stack := []byte("goroutine 1 [running]:\nmain.boom()\n\tmain.go:1 +0x1\n")

	for _, withStack := range []bool{true, false} {
		var out, errb bytes.Buffer
		crash("boom", stack, false, &out, &errb, withStack)

		if got := strings.Contains(errb.String(), "goroutine 1"); got != withStack {
			t.Errorf("withStack=%v: stack on stderr = %v, want %v", withStack, got, withStack)
		}
		if strings.Contains(out.String(), "goroutine 1") {
			t.Errorf("withStack=%v: stack reached stdout", withStack)
		}
		if !withStack && !strings.Contains(errb.String(), logging.EnvDebug) {
			t.Error("without a stack, stderr should name the flag that produces one")
		}
	}
}

// A panic raised while reading the environment used to re-panic inside the
// recover, because the crash handler went back to the environment to decide
// whether to print a stack. The handler must be total.
func TestCrashHandlerSurvivesAPanicFromTheEnvironmentItself(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"version"}, strings.NewReader(""),
		&out, &errb, func(string) string { panic("the environment itself panicked") })

	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if !strings.Contains(errb.String(), "This is a bug") {
		t.Errorf("stderr should still guide the user, got %q", errb.String())
	}
}
