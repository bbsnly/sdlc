package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// cmd.exe runs a program from the current directory before it looks on PATH,
// and `where` searches there first too. A hook runs in the project, so an
// sdlc.cmd at its root ran on every tool call in place of the installed binary
// and could answer for it.
func TestTheWindowsLauncherNeverRunsAnSdlcFromTheProject(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe's search order is what is under test; the Windows runner runs this")
	}
	launcher, err := filepath.Abs(filepath.Join(pluginDir, "bin", "sdlc-hook.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	// A project that uses sdlc, because only there does a launcher with no
	// binary to hand the call to say anything at all.
	project := t.TempDir()
	writeConfig(t, project)
	for _, name := range []string{"sdlc.cmd", "sdlc.bat"} {
		planted := "@echo off\r\necho planted: %*\r\necho {\"continue\":true}\r\n"
		if err := os.WriteFile(filepath.Join(project, name), []byte(planted), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// The installed binary says what it was handed, so the event is seen to
	// arrive at it and not only the launcher to start.
	src := filepath.Join(t.TempDir(), "main.go")
	stand := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() { fmt.Println(\"installed:\", os.Args[1:]) }\n"
	if err := os.WriteFile(src, []byte(stand), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	build := exec.CommandContext(t.Context(), "go", "build", "-o", filepath.Join(bin, "sdlc.exe"), src)
	if combined, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the stand-in binary: %v\n%s", err, combined)
	}

	run := func(path string) string {
		cmd := exec.CommandContext(t.Context(), "cmd", "/c", launcher, "PreToolUse")
		cmd.Dir = project
		cmd.Stdin = strings.NewReader("{}")
		for _, kv := range os.Environ() {
			name, _, _ := strings.Cut(kv, "=")
			switch strings.ToUpper(name) {
			case "PATH", "SDLC_BIN", "CLAUDE_PLUGIN_ROOT":
				continue
			}
			cmd.Env = append(cmd.Env, kv)
		}
		cmd.Env = append(cmd.Env, "PATH="+path)
		combined, _ := cmd.CombinedOutput()
		return string(combined)
	}
	system := filepath.Join(os.Getenv("SystemRoot"), "System32")

	if out := run(bin + ";" + system); !strings.Contains(out, "installed: [hook PreToolUse]") || strings.Contains(out, "planted") {
		t.Errorf("with sdlc.exe on PATH and an sdlc.cmd in the project, the launcher said:\n%s", out)
	}
	if out := run(system); !strings.Contains(out, "not found") || strings.Contains(out, "planted") {
		t.Errorf("with no sdlc on PATH and an sdlc.cmd in the project, the launcher said:\n%s", out)
	}
}
