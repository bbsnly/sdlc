package logging

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnvReadsTheThreeVariables(t *testing.T) {
	got := FromEnv(env(map[string]string{
		EnvDebug: "1", EnvTrace: "true", EnvDebugFile: "/tmp/x.log",
	}))
	if !got.Debug || !got.Trace || got.File != "/tmp/x.log" {
		t.Errorf("got %+v", got)
	}
}

func TestAnyNonEmptyValueMeansOn(t *testing.T) {
	// Someone who writes SDLC_DEBUG=yes means yes, and a tool that silently
	// reads that as false is worse than one that is generous.
	for _, v := range []string{"1", "true", "yes", "on", "please"} {
		if !FromEnv(env(map[string]string{EnvDebug: v})).Debug {
			t.Errorf("%q should enable debug", v)
		}
	}
	for _, v := range []string{"", "0", "false"} {
		if FromEnv(env(map[string]string{EnvDebug: v})).Debug {
			t.Errorf("%q should not enable debug", v)
		}
	}
}

func TestSetupWritesToStderrByDefault(t *testing.T) {
	var buf bytes.Buffer
	closer := Setup(Options{Debug: true}, &buf)
	t.Cleanup(func() { _ = closer() })

	slog.Debug("hello", "k", "v")
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("nothing reached stderr: %q", buf.String())
	}
}

func TestSetupWritesToTheDebugFileWhenAsked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "sdlc.log")
	var stderr bytes.Buffer
	closer := Setup(Options{Debug: true, File: path}, &stderr)

	slog.Debug("to the file")
	if err := closer(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "to the file") {
		t.Errorf("log file is %q", body)
	}
	if strings.Contains(stderr.String(), "to the file") {
		t.Error("with a file configured, stderr should stay quiet")
	}
}

func TestAnUnwritableDebugFileDoesNotFailTheCommand(t *testing.T) {
	// Failing to write a debug log is not a reason to fail the work the user
	// asked for.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("i am a file, not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	closer := Setup(Options{Debug: true, File: filepath.Join(blocker, "sdlc.log")}, &stderr)
	t.Cleanup(func() { _ = closer() })

	slog.Debug("still logged")
	if !strings.Contains(stderr.String(), "still logged") {
		t.Errorf("logging should fall back to stderr, got %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), EnvDebugFile) {
		t.Error("the fallback should say which variable it could not honour")
	}
}

func TestStageIsFreeWhenTracingIsOff(t *testing.T) {
	var buf bytes.Buffer
	closer := Setup(Options{Debug: true}, &buf)
	t.Cleanup(func() { _ = closer() })

	Stage(Options{Trace: false}, "quiet")()
	if strings.Contains(buf.String(), "quiet") {
		t.Error("Stage should log nothing when SDLC_TRACE is off")
	}
	Stage(Options{Trace: true}, "loud")()
	if !strings.Contains(buf.String(), "loud") {
		t.Error("Stage should log when SDLC_TRACE is on")
	}
}
