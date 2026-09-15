package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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
	bin := t.TempDir()
	standIn(t, filepath.Join(bin, "sdlc.exe"), "installed:")

	system := filepath.Join(os.Getenv("SystemRoot"), "System32")
	// The runner's install job may have put a real sdlc.exe where the install
	// script does, and the launcher looks there too.
	nowhere := t.TempDir()

	if out := runCmdLauncher(t, launcher, project, bin+";"+system, nowhere); !strings.Contains(out, "installed: [hook PreToolUse]") || strings.Contains(out, "planted") {
		t.Errorf("with sdlc.exe on PATH and an sdlc.cmd in the project, the launcher said:\n%s", out)
	}
	if out := runCmdLauncher(t, launcher, project, system, nowhere); !strings.Contains(out, "not found") || strings.Contains(out, "planted") {
		t.Errorf("with no sdlc on PATH and an sdlc.cmd in the project, the launcher said:\n%s", out)
	}
}

// A desktop app such as Claude Desktop keeps the PATH it started with, so a
// binary installed to the default directory was not found by the hook until the
// app was restarted, and never when npx installed it, since that leaves PATH
// alone. The launcher looks where the installers put it, after PATH.
func TestTheWindowsLauncherFindsTheBinaryWhereTheInstallerPutIt(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe is what is under test; the Windows runner runs this")
	}
	launcher, err := filepath.Abs(filepath.Join(pluginDir, "bin", "sdlc-hook.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	writeConfig(t, project)
	local := t.TempDir()
	standIn(t, filepath.Join(local, "Programs", "sdlc", "bin", "sdlc.exe"), "default:")
	onPath := t.TempDir()
	standIn(t, filepath.Join(onPath, "sdlc.exe"), "path:")
	system := filepath.Join(os.Getenv("SystemRoot"), "System32")

	if out := runCmdLauncher(t, launcher, project, system, local); !strings.Contains(out, "default: [hook PreToolUse]") {
		t.Errorf("with sdlc.exe only where the install script puts it, the launcher said:\n%s", out)
	}
	if out := runCmdLauncher(t, launcher, project, onPath+";"+system, local); !strings.Contains(out, "path: [hook PreToolUse]") {
		t.Errorf("with sdlc.exe on PATH as well, the launcher said:\n%s", out)
	}
}

// The command in hooks.json has no extension, so on Windows Git Bash's sh may
// run plugin/bin/sdlc-hook instead of the .cmd, and it is handed LOCALAPPDATA as
// Windows wrote it, with backslashes.
func TestTheGitBashLauncherFindsTheBinaryWhereTheInstallerPutIt(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Git Bash on Windows is what is under test; the Windows runner runs this")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		// Nothing else runs this launcher on Windows, so on CI a runner that
		// lost Git's sh must fail rather than skip without a sound.
		if os.Getenv("CI") != "" {
			t.Fatal("no sh on PATH on CI, so the Git Bash launcher went untested")
		}
		t.Skip("no sh on PATH, so the POSIX launcher is not run here")
	}
	script, err := filepath.Abs(filepath.Join(pluginDir, "bin", "sdlc-hook"))
	if err != nil {
		t.Fatal(err)
	}
	local := t.TempDir()
	if !strings.Contains(local, `\`) {
		t.Fatalf("LOCALAPPDATA %q is not written the way Windows writes it", local)
	}
	standIn(t, filepath.Join(local, "Programs", "sdlc", "bin", "sdlc.exe"), "default:")
	onPath := t.TempDir()
	standIn(t, filepath.Join(onPath, "sdlc.exe"), "path:")
	home := t.TempDir()

	run := func(path string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), sh, script, "PreToolUse")
		cmd.Dir = t.TempDir()
		cmd.Stdin = strings.NewReader("{}")
		for _, kv := range os.Environ() {
			name, _, _ := strings.Cut(kv, "=")
			switch strings.ToUpper(name) {
			case "PATH", "SDLC_BIN", "CLAUDE_PLUGIN_ROOT", "LOCALAPPDATA", "HOME":
				continue
			}
			cmd.Env = append(cmd.Env, kv)
		}
		cmd.Env = append(cmd.Env, "PATH="+path, "LOCALAPPDATA="+local, "HOME="+home)
		combined, _ := cmd.CombinedOutput()
		return string(combined)
	}
	shDir := filepath.Dir(sh)

	if out := run(shDir); !strings.Contains(out, "default: [hook PreToolUse]") {
		t.Errorf("with sdlc.exe only under a backslashed LOCALAPPDATA, the sh launcher said:\n%s", out)
	}
	if out := run(onPath + ";" + shDir); !strings.Contains(out, "path: [hook PreToolUse]") {
		t.Errorf("with sdlc.exe on PATH as well, the sh launcher said:\n%s", out)
	}
}

// standIn builds a program at path that prints label and the arguments it was
// handed.
func standIn(t *testing.T, path, label string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "main.go")
	code := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() { fmt.Println(" +
		strconv.Quote(label) + ", os.Args[1:]) }\n"
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(t.Context(), "go", "build", "-o", path, src)
	if combined, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the stand-in binary: %v\n%s", err, combined)
	}
}

// runCmdLauncher runs the cmd.exe launcher from project with the PATH and
// LOCALAPPDATA given, and nothing else of the environment that says where sdlc
// is.
func runCmdLauncher(t *testing.T, launcher, project, path, localAppData string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "cmd", "/c", launcher, "PreToolUse")
	cmd.Dir = project
	cmd.Stdin = strings.NewReader("{}")
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "PATH", "SDLC_BIN", "CLAUDE_PLUGIN_ROOT", "LOCALAPPDATA":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "PATH="+path, "LOCALAPPDATA="+localAppData)
	combined, _ := cmd.CombinedOutput()
	return string(combined)
}
