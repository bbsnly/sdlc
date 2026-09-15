package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// me is the Claude Code session the calls in these tests come from.
const me = "session-me"

// writeStory puts story A-1 under way in dir, a project that uses sdlc, with
// session recorded as the one working it, or no session when it is empty.
func writeStory(t *testing.T, dir, session string) {
	t.Helper()
	writeConfig(t, dir)
	putFile(t, dir, ".sdlc/state/active", "A-1\n")
	if session != "" {
		putFile(t, dir, ".sdlc/state/session", session+"\n")
	}
}

func putFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// callFrom is an edit made in the session named, or in one the call does not
// name when it is empty.
func callFrom(session string) string {
	return hookEvent("PreToolUse", session, "", "Edit", map[string]any{"file_path": "internal/x.go"})
}

// hookEvent is a hook payload as Claude Code writes one: a single line, with no
// newline at the end.
func hookEvent(event, session, cwd, tool string, input map[string]any) string {
	e := map[string]any{"hook_event_name": event}
	if session != "" {
		e["session_id"] = session
	}
	if cwd != "" {
		e["cwd"] = cwd
	}
	if tool != "" {
		e["tool_name"], e["tool_input"] = tool, input
	}
	raw, err := json.Marshal(e)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func buildSdlc(t *testing.T) string {
	t.Helper()
	name := "sdlc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(t.Context(), "go", "build", "-o", out, repoRoot+"/cmd/sdlc")
	if combined, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building sdlc: %v\n%s", err, combined)
	}
	return out
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "git", "init", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

// loopRepository is a repository that uses sdlc, with a formatter that leaves a
// file behind when it runs, and story A-1 in the backlog having passed Gate 1.
// Nothing is under way in it until a test says so.
func loopRepository(t *testing.T, dir string) {
	t.Helper()
	gitInit(t, dir)
	cfg, err := json.Marshal(map[string]any{"version": 1, "commands": map[string]string{"fmt_file": "echo formatted > fmt-ran"}})
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, dir, ".sdlc/config.json", string(cfg))
	putFile(t, dir, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress"}]}`+"\n")
	at := time.Now()
	record := model.NewRecord("A-1", at)
	record.SetGate(model.GateDoR, model.GatePass, "", at)
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, dir, ".sdlc/stories/A-1/"+model.RecordFile, string(raw))
}

// hookRun is one hook command run the way Claude Code runs it.
type hookRun struct {
	launcher launcher
	// binary is where SDLC_BIN points, or empty for no binary anywhere the
	// launcher looks.
	binary     string
	projectDir string // CLAUDE_PROJECT_DIR, left unset when empty
	cwd        string
	pwd        string // PWD, left unset when empty
	// home stands in for HOME, LOCALAPPDATA and the cache directory, so that
	// nothing is found there and anything written there is seen.
	home string
}

// run hands the hook one event and returns what it wrote to each stream.
func (h hookRun) run(t *testing.T, event, payload string) (stdout, stderr string) {
	t.Helper()
	// A launcher that hangs on its input holds the session, so it fails here
	// rather than at the end of the test run.
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	argv := append(slices.Clone(h.launcher.argv[:len(h.launcher.argv)-1]), event)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "PATH", "PWD", "SDLC_BIN", "CLAUDE_PLUGIN_ROOT", "CLAUDE_PROJECT_DIR", "HOME",
			"LOCALAPPDATA", "XDG_CACHE_HOME":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	path := h.launcher.path
	if h.binary != "" {
		// The binary runs git for the story's own commit.
		if git, err := exec.LookPath("git"); err == nil {
			path += string(os.PathListSeparator) + filepath.Dir(git)
		}
		cmd.Env = append(cmd.Env, "SDLC_BIN="+h.binary)
	}
	cmd.Env = append(cmd.Env, "PATH="+path, "CLAUDE_PLUGIN_ROOT="+filepath.Join(h.home, "plugin"),
		"HOME="+h.home, "LOCALAPPDATA="+h.home, "XDG_CACHE_HOME="+filepath.Join(h.home, "cache"))
	if h.projectDir != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_PROJECT_DIR="+h.projectDir)
	}
	if h.pwd != "" {
		cmd.Env = append(cmd.Env, "PWD="+h.pwd)
	}
	cmd.Dir = h.cwd
	cmd.Stdin = strings.NewReader(payload)
	var out, errs strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errs
	if err := cmd.Run(); err != nil {
		t.Fatalf("the hook exited with %v, so Claude Code reads nothing it said\nstdout: %s\nstderr: %s", err, out.String(), errs.String())
	}
	return out.String(), errs.String()
}

// said is what a hook said to the session, or empty when it said nothing but
// carry on.
func said(stdout, stderr string) string {
	out := strings.TrimSpace(stdout)
	if out == `{"continue":true}` {
		out = ""
	}
	if errs := strings.TrimSpace(stderr); errs != "" {
		out += " [stderr] " + errs
	}
	return strings.TrimSpace(out)
}

// snapshot is every file and directory under the roots, with what writing to
// one would change.
func snapshot(t *testing.T, roots ...string) map[string]string {
	t.Helper()
	seen := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			seen[path] = fmt.Sprintf("%v %d %s", info.Mode(), info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return seen
}

// changed names what differs between two snapshots.
func changed(before, after map[string]string) []string {
	var out []string
	for path, was := range before {
		if now, ok := after[path]; !ok {
			out = append(out, "removed "+path)
		} else if now != was {
			out = append(out, "changed "+path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			out = append(out, "created "+path)
		}
	}
	slices.Sort(out)
	return out
}

// The plugin is installed for every session there is. A session that did not
// start the story in front of it -- in a directory in no repository, in a
// repository that does not use sdlc, in one that does with nothing under way,
// or in one with a story another session or a terminal started -- hears nothing
// from it, is refused nothing, is never sent back at the end of its turn, and
// has nothing written for it, with the binary installed and without it. The
// session that started the story is the control: the same calls there are
// refused, formatted and sent back, and without the binary it is told that
// nothing is being enforced.
func TestOnlyTheSessionThatStartedAStoryHearsFromThePlugin(t *testing.T) {
	binary := buildSdlc(t)
	states := []struct {
		name string
		make func(t *testing.T, dir string)
		held bool
	}{
		{"a directory in no repository", func(t *testing.T, dir string) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a repository that does not use sdlc", gitInit, false},
		{"a repository that uses sdlc with no story under way", loopRepository, false},
		{"a story another session started", func(t *testing.T, dir string) {
			loopRepository(t, dir)
			writeStoryState(t, dir, "session-other")
		}, false},
		{"a story started from a terminal", func(t *testing.T, dir string) {
			loopRepository(t, dir)
			writeStoryState(t, dir, "")
		}, false},
		{"a story this session started", func(t *testing.T, dir string) {
			loopRepository(t, dir)
			writeStoryState(t, dir, me)
		}, true},
	}
	calls := []struct {
		name, event, tool string
		input             map[string]any
	}{
		{"an edit", "PreToolUse", "Edit", map[string]any{"file_path": "internal/x.go"}},
		{"a commit", "PreToolUse", "Bash", map[string]any{"command": "git commit -m x"}},
		{"a write to the configuration", "PreToolUse", "Write", map[string]any{"file_path": ".sdlc/config.json"}},
		{"a file written", "PostToolUse", "Write", map[string]any{"file_path": "internal/x.go"}},
		{"the end of a turn", "Stop", "", nil},
	}
	for _, l := range launchers(t) {
		for _, withBinary := range []bool{false, true} {
			for _, s := range states {
				name := fmt.Sprintf("%s/binary %v/%s", l.name, withBinary, s.name)
				t.Run(name, func(t *testing.T) {
					dir := filepath.Join(t.TempDir(), "project")
					s.make(t, dir)
					h := hookRun{launcher: l, projectDir: dir, cwd: dir, home: t.TempDir()}
					if withBinary {
						h.binary = binary
					}
					before := snapshot(t, dir, h.home)
					for _, c := range calls {
						stdout, stderr := h.run(t, c.event, hookEvent(c.event, me, dir, c.tool, c.input))
						heard := said(stdout, stderr)
						switch {
						case !s.held:
							if heard != "" {
								t.Errorf("%s was answered: %s", c.name, heard)
							}
						case !withBinary:
							if !strings.Contains(stdout, "the sdlc binary was not found") {
								t.Errorf("%s: the session working the story was not told nothing is enforced: %s", c.name, heard)
							}
						default:
							assertHeld(t, l, dir, c.event, c.name, stdout)
						}
					}
					if !s.held {
						if diff := changed(before, snapshot(t, dir, h.home)); len(diff) > 0 {
							t.Errorf("the hook wrote to disk:\n  %s", strings.Join(diff, "\n  "))
						}
					}
				})
			}
		}
	}
}

// writeStoryState puts story A-1 under way in a repository loopRepository made.
func writeStoryState(t *testing.T, dir, session string) {
	t.Helper()
	putFile(t, dir, ".sdlc/state/active", "A-1\n")
	if session != "" {
		putFile(t, dir, ".sdlc/state/session", session+"\n")
	}
}

// assertHeld checks that the binary held the session working the story to it.
func assertHeld(t *testing.T, l launcher, dir, event, name, stdout string) {
	t.Helper()
	var r struct {
		Decision           string `json:"decision"`
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &r); err != nil {
		t.Fatalf("%s: stdout is not JSON: %v (%q)", name, err, stdout)
	}
	switch event {
	case "PreToolUse":
		if r.HookSpecificOutput.PermissionDecision != "deny" {
			t.Errorf("%s went through in the session working the story: %s", name, stdout)
		}
	case "PostToolUse":
		// The formatter runs through sh, which only the POSIX launcher's PATH
		// is sure to have.
		if l.name == "sh" {
			if _, err := os.Stat(filepath.Join(dir, "fmt-ran")); err != nil {
				t.Errorf("%s: the formatter did not run in the session working the story: %v", name, err)
			}
		}
	case "Stop":
		if r.Decision != "block" {
			t.Errorf("the session working the story ended its turn mid-story: %s", stdout)
		}
	}
}

// msysPath is a Windows path as Git Bash writes it: C:\Users\x is /c/Users/x.
func msysPath(p string) string {
	volume := filepath.VolumeName(p)
	if len(volume) != 2 || volume[1] != ':' {
		return filepath.ToSlash(p)
	}
	return "/" + strings.ToLower(volume[:1]) + filepath.ToSlash(p[len(volume):])
}

// Somebody on Windows with the plugin installed, and sdlc never set up in the
// repository in front of them, was told on every edit that the binary was not
// found. Both launchers are run here, cmd.exe and Git Bash's sh, with the
// directories written every way Windows hands them over, with the binary where
// `npx @bbsnly/sdlc install` puts it and with no binary at all.
//
// A directory with no .git in it belongs to the nearest one above it that uses
// sdlc, as it does for the binary, so a story this session started up there is
// still heard from below: that one is the control. A project above with no
// story under way says nothing.
func TestNothingIsHeardFromAWindowsDirectoryHoweverItIsWritten(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows paths and cmd.exe are what is under test; the Windows runner runs this")
	}
	if _, err := exec.LookPath("sh"); err != nil && os.Getenv("CI") != "" {
		t.Fatal("no sh on PATH on CI, so the Git Bash launcher went untested")
	}
	binary := buildSdlc(t)
	root := filepath.Join(t.TempDir(), "with space")
	loose := filepath.Join(root, "loose")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "repo")
	gitInit(t, repo)
	idle := filepath.Join(root, "idle")
	writeConfig(t, idle)
	mine := filepath.Join(root, "mine")
	writeStory(t, mine, me)
	neutral := filepath.Join(root, "neutral")
	if err := os.MkdirAll(neutral, 0o755); err != nil {
		t.Fatal(err)
	}

	dirs := []struct {
		name, dir, top string
		held           bool
	}{
		{"a directory in no repository", loose, loose, false},
		{"a repository that does not use sdlc", repo, repo, false},
		{"a directory with no .git below a project with no story", filepath.Join(idle, "child"), idle, false},
		{"a directory with no .git below this session's story", filepath.Join(mine, "child"), mine, true},
	}
	forms := []struct {
		name string
		// projectDir writes the directory; cwd is where the hook runs.
		projectDir, cwd func(dir string) string
		// sure is whether a story this session started must be heard: a
		// trailing space is for Windows to make sense of, and is only held to
		// saying nothing where there is nothing to say.
		sure bool
	}{
		{"with backslashes", func(d string) string { return d }, func(string) string { return neutral }, true},
		{"with forward slashes", filepath.ToSlash, func(string) string { return neutral }, true},
		{"with a trailing backslash", func(d string) string { return d + `\` }, func(string) string { return neutral }, true},
		{"with a trailing space", func(d string) string { return d + " " }, func(string) string { return neutral }, false},
		{"unset, run from the directory", func(string) string { return "" }, func(d string) string { return d }, true},
		{"as the root of the drive, run from the directory", func(d string) string { return filepath.VolumeName(d) + `\` }, func(d string) string { return d }, true},
	}
	for _, l := range launchers(t) {
		for _, withBinary := range []bool{false, true} {
			for _, d := range dirs {
				if err := os.MkdirAll(d.dir, 0o755); err != nil {
					t.Fatal(err)
				}
				for _, f := range forms {
					t.Run(fmt.Sprintf("%s/binary %v/%s/%s", l.name, withBinary, d.name, f.name), func(t *testing.T) {
						h := hookRun{launcher: l, projectDir: f.projectDir(d.dir), cwd: f.cwd(d.dir), home: t.TempDir()}
						if l.name == "sh" {
							h.pwd = msysPath(h.cwd)
						}
						if withBinary {
							installed := filepath.Join(h.home, "Programs", "sdlc", "bin", "sdlc.exe")
							copyFile(t, binary, installed)
						}
						before := snapshot(t, d.top, h.home)
						edit := hookEvent("PreToolUse", me, h.cwd, "Edit", map[string]any{"file_path": filepath.Join(d.dir, "x.go")})
						stdout, stderr := h.run(t, "PreToolUse", edit)
						heard := said(stdout, stderr)
						switch {
						case !d.held:
							if heard != "" {
								t.Errorf("an edit was answered: %s", heard)
							}
						case !f.sure:
							t.Logf("an edit from this session's story said: %q", heard)
						case heard == "":
							t.Error("the session working the story heard nothing")
						}
						stdout, stderr = h.run(t, "Stop", hookEvent("Stop", me, h.cwd, "", nil))
						if heard := said(stdout, stderr); !d.held && heard != "" {
							t.Errorf("the end of a turn was answered: %s", heard)
						}
						if !d.held {
							if diff := changed(before, snapshot(t, d.top, h.home)); len(diff) > 0 {
								t.Errorf("the hook wrote to disk:\n  %s", strings.Join(diff, "\n  "))
							}
						}
					})
				}
			}
		}
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, raw, 0o755); err != nil {
		t.Fatal(err)
	}
}

// Claude Code puts a whole edit in the payload, on one line, and writes the
// session id first. The session working a story still hears that the binary is
// missing on an edit far longer than a line cmd.exe's findstr will read, and
// another session still hears nothing.
func TestTheLauncherHearsTheSessionOnALargeEdit(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	gitInit(t, project)
	writeStory(t, project, me)
	content, err := json.Marshal(strings.Repeat("x", 20000))
	if err != nil {
		t.Fatal(err)
	}
	edit := func(session string, idFirst bool) string {
		call := `"hook_event_name":"PreToolUse","tool_name":"Write","tool_input":{"file_path":"x.go","content":` +
			string(content) + `}`
		id := `"session_id":"` + session + `"`
		if idFirst {
			return "{" + id + "," + call + "}"
		}
		return "{" + call + "," + id + "}"
	}
	const warning = "the sdlc binary was not found"
	for _, l := range launchers(t) {
		t.Run(l.name, func(t *testing.T) {
			h := hookRun{launcher: l, projectDir: project, cwd: project, home: t.TempDir()}
			if out, errs := h.run(t, "PreToolUse", edit(me, true)); !strings.Contains(out, warning) {
				t.Errorf("the session working the story heard nothing on a large edit: %q %q", out, errs)
			}
			for _, idFirst := range []bool{true, false} {
				if heard := said(h.run(t, "PreToolUse", edit("session-other", idFirst))); heard != "" {
					t.Errorf("another session heard about a story it did not start: %s", heard)
				}
			}
			out, _ := h.run(t, "PreToolUse", edit(me, false))
			switch heard := strings.Contains(out, warning); {
			case l.name == "sh" && !heard:
				t.Error("the session working the story heard nothing with its id after a large edit")
			case l.name == "cmd":
				// The cmd launcher says why it may not: see sdlc-hook.cmd.
				t.Logf("cmd, with the session id after 20000 bytes on one line: heard = %v", heard)
			}
		})
	}
}

// The launchers read the state files as the binary does: the white space round
// a value is not part of it, a blank line is nothing, and a file with two
// values names neither.
func TestTheLauncherReadsTheStateFilesAsTheBinaryDoes(t *testing.T) {
	cases := []struct {
		name, active, session string
		heard                 bool
	}{
		{"as sdlc start writes them", "A-1\n", me + "\n", true},
		{"with spaces after the session", "A-1\n", me + "   \n", true},
		{"with Windows line endings", "A-1\r\n", me + "\r\n", true},
		{"with a blank line first", "A-1\n", "\n" + me + "\n", true},
		{"with two sessions", "A-1\n", me + "\nsession-other\n", false},
		{"with the session twice", "A-1\n", me + "\n" + me + "\n", false},
		{"with two stories", "A-1\nA-2\n", me + "\n", false},
		{"with a story of white space", "  \n", me + "\n", false},
		{"with a session of white space", "A-1\n", " \n", false},
	}
	for _, l := range launchers(t) {
		for _, c := range cases {
			t.Run(l.name+"/"+c.name, func(t *testing.T) {
				project := filepath.Join(t.TempDir(), "project")
				gitInit(t, project)
				writeConfig(t, project)
				putFile(t, project, ".sdlc/state/active", c.active)
				putFile(t, project, ".sdlc/state/session", c.session)
				h := hookRun{launcher: l, projectDir: project, cwd: project, home: t.TempDir()}
				stdout, stderr := h.run(t, "PreToolUse", callFrom(me))
				if heard := said(stdout, stderr) != ""; heard != c.heard {
					t.Errorf("heard = %v, want %v: %q %q", heard, c.heard, stdout, stderr)
				}
			})
		}
	}
}
