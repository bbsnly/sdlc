// US-108 AC-9 and AC-11: the plugin launcher fetches its own binary.
//
// A person who installs the plugin from Claude Desktop's plugin browser never
// runs an installer, so the launcher (plugin/bin/sdlc-hook, and sdlc-hook.cmd
// on Windows) fetches the release matching the plugin's version on the first
// SessionStart. It checks the archive against the checksums pinned in the
// plugin, never against a checksums file fetched beside it, and puts the binary
// in the plugin's bin directory, which Claude Code puts on the model's PATH.
//
// What these tests bind, and the implementation has to provide:
//
//   - the pins: checksums.txt at the root of the plugin, in the form a release's
//     checksums.txt has ("<sha256>  <archive>"), naming one release;
//   - the version: "version" in .claude-plugin/plugin.json;
//   - where it fetches from: SDLC_DOWNLOAD_BASE, as the installers read it,
//     then /v<version>/<archive>. The override is only for a mirror; the pins
//     still apply to whatever it serves.
//
// Each test runs the real launcher from a copy of plugin/ under a temporary
// HOME, as Claude Code installs it, against a local httptest mirror.

package install

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// versionedOnce is a build of sdlc that says it is the plugin's version, as a
// release build does.
var versionedOnce struct {
	sync.Once
	path string
	err  error
}

func versionedBuild(t *testing.T) string {
	t.Helper()
	built(t) // makes buildOnce.dir, which TestMain removes
	versionedOnce.Do(func() {
		dir := filepath.Join(buildOnce.dir, "versioned")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			versionedOnce.err = err
			return
		}
		versionedOnce.path = filepath.Join(dir, binaryName())
		build := exec.CommandContext(context.Background(), "go", "build",
			"-ldflags", "-X github.com/bbsnly/sdlc/internal/version.version="+version,
			"-o", versionedOnce.path, "../../cmd/sdlc")
		if out, err := build.CombinedOutput(); err != nil {
			versionedOnce.err = fmt.Errorf("building sdlc %s: %w\n%s", version, err, out)
		}
	})
	if versionedOnce.err != nil {
		t.Fatal(versionedOnce.err)
	}
	return versionedOnce.path
}

// hostileBuild is a program that, run at all, leaves marker behind and answers
// as a hook that allows everything would.
func hostileBuild(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	src := "package main\n\nimport \"os\"\n\nvar marker string\n\nfunc main() {\n" +
		"\t_ = os.WriteFile(marker, []byte(\"ran\"), 0o644)\n" +
		"\t_, _ = os.Stdout.WriteString(\"{\\\"continue\\\":true}\\n\")\n}\n"
	writeFile(t, dir, "main.go", src)
	writeFile(t, dir, "go.mod", "module hostile\n\ngo 1.22\n")
	out := filepath.Join(t.TempDir(), binaryName())
	build := exec.CommandContext(t.Context(), "go", "build", "-ldflags", "-X main.marker="+marker, "-o", out, ".")
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	if raw, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the stand-in: %v\n%s", err, raw)
	}
	return out
}

// releaseArchive writes this platform's archive for the plugin's version into
// dir, laid out as goreleaser lays one out, and returns its checksum.
func releaseArchive(t *testing.T, dir, binary string) string {
	t.Helper()
	body, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	entries := []archiveEntry{
		{binaryName(), 0o755, body},
		{"LICENSE", 0o644, []byte("MIT\n")},
		{"README.md", 0o644, []byte("# sdlc\n")},
		{"CHANGELOG.md", 0o644, []byte("# Changelog\n")},
	}
	var buf bytes.Buffer
	if strings.HasSuffix(archiveName(), ".zip") {
		zipEntries(t, &buf, entries)
	} else {
		tarEntries(t, &buf, entries)
	}
	if err := os.WriteFile(filepath.Join(dir, archiveName()), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}

// archiveEntry is one file of a release archive.
type archiveEntry struct {
	name string
	mode int64
	body []byte
}

func zipEntries(t *testing.T, buf *bytes.Buffer, entries []archiveEntry) {
	t.Helper()
	zw := zip.NewWriter(buf)
	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func tarEntries(t *testing.T, buf *bytes.Buffer, entries []archiveEntry) {
	t.Helper()
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: e.name, Mode: e.mode, Size: int64(len(e.body))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

// pinsFor is a checksums file for release ver that lists every archive a
// release publishes, with sum for this platform's and a well-formed stand-in
// for the others.
func pinsFor(ver, sum string) string {
	var b strings.Builder
	here := strings.Replace(archiveName(), version, ver, 1)
	names := []string{}
	for _, p := range []string{"darwin_arm64.tar.gz", "linux_amd64.tar.gz", "linux_arm64.tar.gz", "windows_amd64.zip", "windows_arm64.zip"} {
		names = append(names, "sdlc_"+ver+"_"+p)
	}
	// A machine a release does not ship for still runs these tests.
	if !slices.Contains(names, here) {
		names = append(names, here)
	}
	for _, name := range names {
		line := sum
		if name != here {
			other := sha256.Sum256([]byte(name))
			line = hex.EncodeToString(other[:])
		}
		fmt.Fprintf(&b, "%s  %s\n", line, name)
	}
	return b.String()
}

// installedPlugin copies plugin/ to where Claude Code caches an installed
// plugin under home, at the plugin's version, with pins as its checksums
// (none when pins is empty), and returns its root.
func installedPlugin(t *testing.T, home, pins string) string {
	t.Helper()
	root := filepath.Join(home, ".claude", "plugins", "cache", "sdlc", "sdlc", version)
	if err := os.CopyFS(root, os.DirFS(filepath.Join(repoRoot(t), "plugin"))); err != nil {
		t.Fatal(err)
	}
	// A binary built into the checkout is not one the plugin browser installs.
	for _, name := range []string{"sdlc", "sdlc.exe"} {
		if err := os.Remove(filepath.Join(root, "bin", name)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(root, ".claude-plugin", "plugin.json")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw = regexp.MustCompile(`"version":\s*"[^"]*"`).ReplaceAll(raw, []byte(`"version": "`+version+`"`))
	if err := os.WriteFile(manifest, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	at := filepath.Join(root, "checksums.txt")
	if pins == "" {
		if err := os.Remove(at); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	} else if err := os.WriteFile(at, []byte(pins), 0o644); err != nil {
		t.Fatal(err)
	}
	// Writable, as a copy Claude Code made is: the fetched binary goes in it.
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if d.IsDir() || filepath.Base(filepath.Dir(path)) == "bin" {
			mode = 0o755
		}
		return os.Chmod(path, mode)
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

// mirror serves a release directory where GitHub would, and counts what it is
// asked for.
type mirror struct {
	base string
	mu   sync.Mutex
	asks map[string]int
}

func serveMirror(t *testing.T, dir string) *mirror {
	t.Helper()
	m := &mirror{asks: map[string]int{}}
	const downloads = "/releases/download/"
	files := http.FileServer(http.Dir(dir))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		m.asks[r.URL.Path]++
		m.mu.Unlock()
		rest, ok := strings.CutPrefix(r.URL.Path, downloads+"v"+version+"/")
		if !ok || dir == "" {
			http.NotFound(w, r)
			return
		}
		r.URL.Path = "/" + rest
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	m.base = server.URL + "/releases/download"
	return m
}

// unreachable is a download base nothing answers at.
func unreachable(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	base := server.URL + "/releases/download"
	server.Close()
	return base
}

// archiveAsks is how many times any archive was asked for.
func (m *mirror) archiveAsks() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for path, count := range m.asks {
		if strings.HasSuffix(path, ".tar.gz") || strings.HasSuffix(path, ".zip") {
			n += count
		}
	}
	return n
}

// hookLauncher is one way Claude Code runs the hook command on this platform,
// and the shell the model's Bash tool would find sdlc with.
type hookLauncher struct {
	name  string
	argv  func(root, event string) []string
	shell func(command string) []string
}

func hookLaunchers(t *testing.T) []hookLauncher {
	t.Helper()
	var out []hookLauncher
	if runtime.GOOS == "windows" {
		out = append(out, hookLauncher{"cmd",
			func(root, event string) []string {
				return []string{"cmd", "/c", filepath.Join(root, "bin", "sdlc-hook.cmd"), event}
			},
			func(command string) []string { return []string{"cmd", "/c", command} }})
	}
	if sh, err := exec.LookPath("sh"); err == nil {
		out = append(out, hookLauncher{"sh",
			func(root, event string) []string { return []string{sh, filepath.Join(root, "bin", "sdlc-hook"), event} },
			func(command string) []string { return []string{sh, "-c", command} }})
	}
	if len(out) == 0 {
		t.Skip("nothing here can run the launcher")
	}
	return out
}

// systemPath is this machine's PATH without any directory that holds an sdlc,
// so the only sdlc to be found is one the launcher fetched.
func systemPath(t *testing.T) string {
	t.Helper()
	var keep []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		found := false
		for _, name := range []string{"sdlc", "sdlc.exe", "sdlc.cmd"} {
			if info, err := os.Stat(filepath.Join(dir, name)); err == nil && !info.IsDir() {
				found = true
			}
		}
		if !found && dir != "" {
			keep = append(keep, dir)
		}
	}
	return strings.Join(keep, string(os.PathListSeparator))
}

// session is what Claude Code hands a hook in a local session: a HOME of its
// own, the plugin's root and data directories, the project directory, and a
// mirror to fetch from. Nothing comes from the environment the tests run in.
type session struct {
	home, root, data, project, base string
}

func (s session) env(t *testing.T, path string) []string {
	t.Helper()
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "SDLC_") || strings.HasPrefix(upper, "CLAUDE_") {
			continue
		}
		switch upper {
		case "PATH", "HOME", "USERPROFILE", "LOCALAPPDATA", "PWD":
			continue
		}
		env = append(env, kv)
	}
	return append(env, "PATH="+path, "HOME="+s.home, "USERPROFILE="+s.home, "LOCALAPPDATA="+s.home,
		"SDLC_DOWNLOAD_BASE="+s.base)
}

// hook runs the launcher for one event as Claude Code does, and returns what it
// wrote to standard output. A hook that does not exit 0 is one Claude Code
// reads no answer from.
func (s session) hook(t *testing.T, l hookLauncher, event string, payload map[string]any) string {
	t.Helper()
	argv := l.argv(s.root, event)
	cmd := exec.CommandContext(t.Context(), argv[0], argv[1:]...)
	cmd.Env = append(s.env(t, systemPath(t)),
		"CLAUDE_PLUGIN_ROOT="+s.root, "CLAUDE_PLUGIN_DATA="+s.data, "CLAUDE_PROJECT_DIR="+s.project,
		"CLAUDE_CODE_SESSION_ID="+fmt.Sprint(payload["session_id"]))
	cmd.Dir = s.project
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the launcher failed on %s: %v\nstdout: %s\nstderr: %s", event, err, out, stderr.String())
	}
	return string(out)
}

// bash runs a command as the model's Bash tool does: the plugin's bin directory
// on PATH after the rest, and none of the plugin's variables, which Claude Code
// keeps from the Bash tool.
func (s session) bash(t *testing.T, l hookLauncher, command string) (string, error) {
	t.Helper()
	argv := l.shell(command)
	cmd := exec.CommandContext(t.Context(), argv[0], argv[1:]...)
	cmd.Env = s.env(t, systemPath(t)+string(os.PathListSeparator)+filepath.Join(s.root, "bin"))
	cmd.Dir = s.project
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// hookReply is every field a launcher's answer may carry.
type hookReply struct {
	Continue      *bool  `json:"continue"`
	StopReason    string `json:"stopReason"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	SystemMessage string `json:"systemMessage"`
}

func (r hookReply) shown() string {
	return strings.Join([]string{r.Reason, r.StopReason, r.SystemMessage}, "\n")
}

// oneObject decodes stdout, which has to be one JSON object and nothing more:
// anything else on a hook's stdout is a parse error to Claude Code, or text
// added to the model's context.
func oneObject(t *testing.T, raw string) hookReply {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(raw))
	var r hookReply
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("the launcher's stdout is not one JSON object: %v\n%q", err, raw)
	}
	if rest := strings.TrimSpace(raw[dec.InputOffset():]); rest != "" {
		t.Fatalf("the launcher printed more than one JSON object: %q", raw)
	}
	if r.Continue != nil && !*r.Continue {
		t.Errorf("the launcher ended the session: %q", raw)
	}
	return r
}

func mkdirs(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	mkdirs(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tempDir is t.TempDir with the links in its name resolved, as a session's
// working directory is reported.
func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return dir
}

// freshRepository is a repository a person has opened Claude Desktop in and
// not yet run /sdlc:init in.
func freshRepository(t *testing.T) string {
	t.Helper()
	dir := tempDir(t)
	mkdirs(t, dir, ".git")
	return dir
}

// takingPart is a project that takes part in the loop, with no story under way.
func takingPart(t *testing.T) string {
	t.Helper()
	dir := freshRepository(t)
	writeFile(t, dir, ".sdlc/config.json", `{"version":1}`)
	writeFile(t, dir, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"ready"}]}`+"\n")
	return dir
}

// underWay is a project with story A-1 under way in session-a.
func underWay(t *testing.T) string {
	t.Helper()
	dir := takingPart(t)
	writeFile(t, dir, ".sdlc/state/active", "A-1\n")
	writeFile(t, dir, ".sdlc/state/session", "session-a\n")
	writeFile(t, dir, "user_stories.json", `{"stories":[{"id":"A-1","title":"Invoices","status":"in_progress"}]}`+"\n")
	return dir
}

func sessionStart(project, id string) map[string]any {
	return map[string]any{
		"hook_event_name": "SessionStart", "session_id": id, "cwd": project,
		"transcript_path": filepath.Join(project, "transcript.jsonl"), "source": "startup",
	}
}

func typedCommand(project, id, name, args string) map[string]any {
	return map[string]any{
		"hook_event_name": "UserPromptExpansion", "session_id": id, "cwd": project,
		"transcript_path": filepath.Join(project, "transcript.jsonl"), "permission_mode": "default",
		"expansion_type": "slash_command", "command_name": name, "command_args": args,
		"command_source": "plugin", "prompt": strings.TrimSpace("/" + name + " " + args),
	}
}

func protectedWrite(project, id string) map[string]any {
	return map[string]any{
		"hook_event_name": "PreToolUse", "session_id": id, "cwd": project, "tool_name": "Write",
		"tool_input": map[string]string{"file_path": filepath.Join(project, ".sdlc", "state", "active")},
	}
}

// AC-9: a person who installed the plugin from the plugin browser opens a
// session in a repository that does not take part yet. The launcher fetches
// the plugin's own version, once; the model's Bash then finds that sdlc on its
// PATH, and the hook runs it.
func TestAC9OnTheFirstSessionStartTheLauncherFetchesThePluginsReleaseAndTheHookAndBashFindIt(t *testing.T) {
	for _, l := range hookLaunchers(t) {
		t.Run("AC-9 "+l.name, func(t *testing.T) {
			releaseDir := t.TempDir()
			sum := releaseArchive(t, releaseDir, versionedBuild(t))
			m := serveMirror(t, releaseDir)
			home := tempDir(t)
			s := session{home: home, root: installedPlugin(t, home, pinsFor(version, sum)),
				data: mkdirs(t, home, ".claude", "plugins", "data", "sdlc"), project: freshRepository(t), base: m.base}

			started := s.hook(t, l, "SessionStart", sessionStart(s.project, "session-a"))
			if m.archiveAsks() == 0 {
				t.Fatalf("the first SessionStart asked the mirror for nothing: %v", m.asks)
			}
			oneObject(t, started)

			out, err := s.bash(t, l, "sdlc version")
			if err != nil {
				t.Fatalf("the model's Bash found no sdlc after the first SessionStart: %v\n%s", err, out)
			}
			if !strings.Contains(out, version) {
				t.Errorf("the model's Bash found an sdlc that is not the plugin's version %s: %q", version, out)
			}

			project := underWay(t)
			s.project = project
			reply := s.hook(t, l, "PreToolUse", protectedWrite(project, "session-a"))
			if !strings.Contains(reply, `"permissionDecision":"deny"`) {
				t.Errorf("the hook did not run the fetched sdlc: a write to the loop's state was answered %q", reply)
			}

			asked := m.archiveAsks()
			oneObject(t, s.hook(t, l, "SessionStart", sessionStart(project, "session-b")))
			if again := m.archiveAsks(); again != asked {
				t.Errorf("a later SessionStart fetched the release again (%d asks, then %d)", asked, again)
			}
		})
	}
}

// AC-9: an archive that does not match the checksum pinned in the plugin is
// never run, nor left where a later call would run it -- even from a mirror
// that serves a checksums file vouching for it. And the session is told.
func TestAC9TheLauncherNeverRunsABinaryThatFailsItsPinnedChecksum(t *testing.T) {
	for _, l := range hookLaunchers(t) {
		t.Run("AC-9 "+l.name, func(t *testing.T) {
			genuine := releaseArchive(t, t.TempDir(), versionedBuild(t))
			// No space in it: it goes to the linker as one word.
			marker := filepath.Join(tempDir(t), "hostile-binary-ran")
			hostile := hostileBuild(t, marker)
			releaseDir := t.TempDir()
			served := releaseArchive(t, releaseDir, hostile)
			writeFile(t, releaseDir, "checksums.txt", pinsFor(version, served))
			m := serveMirror(t, releaseDir)
			home := tempDir(t)
			s := session{home: home, root: installedPlugin(t, home, pinsFor(version, genuine)),
				data: mkdirs(t, home, ".claude", "plugins", "data", "sdlc"), project: takingPart(t), base: m.base}

			r := oneObject(t, s.hook(t, l, "SessionStart", sessionStart(s.project, "session-a")))
			if !strings.Contains(strings.ToLower(r.shown()), "checksum") {
				t.Errorf("the session was not told the download failed its checksum: %+v", r)
			}

			project := underWay(t)
			s.project = project
			oneObject(t, s.hook(t, l, "PreToolUse", protectedWrite(project, "session-a")))
			_, _ = s.bash(t, l, "sdlc version")
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("a binary that failed its pinned checksum was run")
			}

			hostileBody, err := os.ReadFile(hostile)
			if err != nil {
				t.Fatal(err)
			}
			for _, dir := range []string{s.root, s.data} {
				_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
					if err != nil || d.IsDir() {
						return err
					}
					if body, err := os.ReadFile(path); err == nil && bytes.Equal(body, hostileBody) {
						t.Errorf("the binary that failed its checksum was left at %s", path)
					}
					return nil
				})
			}
		})
	}
}

// AC-11: whatever stops the launcher from finding or fetching sdlc, a session
// in a project that takes part is told what happened and how to put it right
// without a terminal, which a person in Claude Desktop does not have. Setting
// SDLC_BIN in Desktop's local environment editor is the one route that always
// works from inside the app.
func TestAC11WhenNoSdlcCanBeFetchedASessionStartIsToldWhatHappenedAndHowToPutItRightInTheApp(t *testing.T) {
	for _, l := range hookLaunchers(t) {
		for _, c := range []struct {
			name, says string
			setUp      func(t *testing.T, releaseDir string) (pins string, base func(*mirror) string)
			fetches    bool
		}{
			{"AC-11 the release cannot be reached", "fetch", func(t *testing.T, dir string) (string, func(*mirror) string) {
				t.Helper()
				return pinsFor(version, releaseArchive(t, dir, versionedBuild(t))),
					func(*mirror) string { return unreachable(t) }
			}, true},
			{"AC-11 the release holds no archive for this platform", "fetch", func(t *testing.T, dir string) (string, func(*mirror) string) {
				t.Helper()
				sum := releaseArchive(t, dir, versionedBuild(t))
				if err := os.Remove(filepath.Join(dir, archiveName())); err != nil {
					t.Fatal(err)
				}
				return pinsFor(version, sum), func(m *mirror) string { return m.base }
			}, true},
			// The tagged commit carries the previous release's pins (the
			// story's release-order decision): the launcher fails closed and
			// never fetches what it holds no pin for.
			{"AC-11 the pins are for another release", version, func(t *testing.T, dir string) (string, func(*mirror) string) {
				t.Helper()
				sum := releaseArchive(t, dir, versionedBuild(t))
				return pinsFor("9.9.8", sum), func(m *mirror) string { return m.base }
			}, false},
			{"AC-11 the plugin carries no pins", "checksum", func(t *testing.T, dir string) (string, func(*mirror) string) {
				t.Helper()
				releaseArchive(t, dir, versionedBuild(t))
				return "", func(m *mirror) string { return m.base }
			}, false},
		} {
			t.Run(l.name+"/"+c.name, func(t *testing.T) {
				releaseDir := t.TempDir()
				pins, base := c.setUp(t, releaseDir)
				m := serveMirror(t, releaseDir)
				home := tempDir(t)
				s := session{home: home, root: installedPlugin(t, home, pins),
					data: mkdirs(t, home, ".claude", "plugins", "data", "sdlc"), project: takingPart(t), base: base(m)}

				r := oneObject(t, s.hook(t, l, "SessionStart", sessionStart(s.project, "session-new")))
				toldOfNoSdlc(t, r, c.says)
				if !c.fetches && m.archiveAsks() != 0 {
					t.Errorf("the launcher fetched an archive it holds no pin for: %v", m.asks)
				}
				if out, err := s.bash(t, l, "sdlc version"); err == nil {
					t.Errorf("an sdlc was installed all the same: %q", out)
				}
			})
		}
	}
}

// AC-11: a person who types one of the plugin's commands while no sdlc can be
// found is told, in a project that takes part or one that does not yet; a
// command that is not the plugin's still hears nothing from it.
func TestAC11ASlashCommandTypedWhenNoSdlcCanBeFoundTellsThePersonHowToPutItRightInTheApp(t *testing.T) {
	for _, l := range hookLaunchers(t) {
		for _, c := range []struct {
			name    string
			project func(t *testing.T) string
			command string
			told    bool
		}{
			{"AC-11 /sdlc:init in a repository that does not take part yet", freshRepository, "sdlc:init", true},
			{"AC-11 /sdlc:approve in a project with no story under way", takingPart, "sdlc:approve", true},
			{"AC-11 /sdlc:unfreeze in a project with a story under way, from another session", underWay, "sdlc:unfreeze", true},
			{"AC-11 another plugin's command", freshRepository, "other:deploy", false},
		} {
			t.Run(l.name+"/"+c.name, func(t *testing.T) {
				home := tempDir(t)
				s := session{home: home, root: installedPlugin(t, home, pinsFor(version, strings.Repeat("0", 64))),
					data: mkdirs(t, home, ".claude", "plugins", "data", "sdlc"), project: c.project(t), base: unreachable(t)}
				out := s.hook(t, l, "UserPromptExpansion", typedCommand(s.project, "session-b", c.command, "A-1"))
				if !c.told {
					if strings.TrimSpace(out) != "" && strings.TrimSpace(oneObject(t, out).shown()) != "" {
						t.Errorf("a command that is not the plugin's heard from it: %q", out)
					}
					return
				}
				toldOfNoSdlc(t, oneObject(t, out), "")
			})
		}
	}
}

// toldOfNoSdlc holds a reply to what AC-11 asks of it: it says that nothing is
// enforced and why, and how to put it right from inside the app.
func toldOfNoSdlc(t *testing.T, r hookReply, why string) {
	t.Helper()
	shown := r.shown()
	for _, want := range []string{"nothing is being enforced", "SDLC_BIN", "local environment editor"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the session was not told %q:\n%s", want, shown)
		}
	}
	if why != "" && !strings.Contains(strings.ToLower(shown), strings.ToLower(why)) {
		t.Errorf("the session was not told what happened (%q):\n%s", why, shown)
	}
}
