// Package checklegs drives individual Taskfile.yml legs -- markdownlint,
// secrets, shellcheck -- against a disposable fixture repository, the same
// way US-037's own ANALYSIS.md reproduced the bug: a real git repository, the
// real pinned tool, nothing of this repository's own tree touched. There is
// no other way to exercise a Taskfile leg's behaviour deterministically:
// each one is a few lines of shell wrapping a third-party tool, not a
// package this repository owns, so there was no existing harness for it
// (US-037's ANALYSIS.md, "Test home") until this file.
//
// Every leg here is reached through `go run module@version`, exactly as
// Taskfile.yml itself reaches it (GITLEAKS, and the `task` CLI these tests
// use to run a single named task against a fixture directory instead of
// this repository). Plain `go run` checks a pinned module's deprecation
// status over the network even once its zip is already in the local module
// cache -- confirmed by pointing GOPROXY at an unreachable host, which fails
// the same way a genuinely offline machine would -- so runTask instead
// points GOPROXY at that same local cache, served as a file proxy
// (fileProxyEnv), and the suite checks a module or lockfile is already
// there (requireGoModule, requireNode) before it tries, rather than
// discovering the lack of network from what came back.
package checklegs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// repoRoot is this repository's own root. test/checklegs sits two levels
// below it, the same distance test/e2e already relies on for its own build
// target ("../../cmd/sdlc").
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("../..")
	if err != nil {
		t.Fatalf("resolving repository root: %v", err)
	}
	return abs
}

// write creates rel under root, making its parent directories as needed --
// the same small helper internal/tools/linkcheck's own tests use.
func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// copyFromRepo copies this repository's own rel (a config file a leg reads,
// such as .markdownlint-cli2.jsonc) into a fixture, live, so a fix that
// changes the real file is what the test sees too -- not a frozen snapshot
// of what it said when this suite was written.
func copyFromRepo(t *testing.T, fixtureRoot, rel string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatalf("reading this repository's own %s: %v", rel, err)
	}
	write(t, fixtureRoot, rel, string(body))
}

// gitInit makes root a git repository and nothing more. No fixture here is
// ever committed: every leg under test reads the working tree, the index or
// .gitignore, none of which needs history, and `git ls-files` already lists
// a staged path before it is ever committed -- which is all "tracked" means
// for AC-1 through AC-3.
func gitInit(t *testing.T, root string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// gitAdd stages rel under root.
func gitAdd(t *testing.T, root string, rel ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"add"}, rel...)...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add %v: %v\n%s", rel, err, out)
	}
}

// pinnedVersion reads name's value out of this repository's own live
// Taskfile.yml vars block (TASK_VERSION, GITLEAKS_VERSION, ...), the same
// value `./task` itself resolves -- never a copy kept in this test file,
// which a version bump would silently stop checking against.
func pinnedVersion(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRoot(t), "Taskfile.yml"))
	if err != nil {
		t.Fatalf("reading Taskfile.yml: %v", err)
	}
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(name) + `:\s*(\S+)`)
	m := re.FindSubmatch(body)
	if m == nil {
		t.Fatalf("Taskfile.yml no longer sets %s", name)
	}
	return string(m[1])
}

// goModCache is this machine's module cache directory, read once via `go
// env GOMODCACHE` -- a local lookup, never a network one.
func goModCache(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// fileProxyEnv is the GOPROXY every `go run` in this suite uses: this
// machine's own module cache, served back to it as a file proxy. Plain
// GOPROXY=off refuses even a module whose zip is already cached, because
// `go run module@version` also asks its source for the module's
// deprecation status, and off means "no source to ask" rather than
// "already known" -- confirmed by pointing GOPROXY at an unreachable host,
// which fails the identical way. A file proxy answers that question from
// the same cache instead. It carries no ",direct" fallback, so a module
// this cache does not already have fails fast rather than reaching out --
// which is why every caller checks requireGoModule first, for a reason a
// person can act on instead of a bare resolution error.
func fileProxyEnv(t *testing.T) string {
	t.Helper()
	path := filepath.ToSlash(goModCache(t))
	if len(path) >= 2 && path[1] == ':' { // a Windows drive letter, e.g. "C:/..."
		path = "/" + path
	}
	return "GOPROXY=file://" + path + "/cache/download"
}

// moduleCached reports whether module@version is already extracted into the
// local module cache fileProxyEnv serves from.
func moduleCached(t *testing.T, module, version string) bool {
	t.Helper()
	dir := filepath.Join(goModCache(t), filepath.FromSlash(module)+"@v"+version)
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

// requireGoModule skips, with an actionable fix, when module@version is not
// already in the local cache fileProxyEnv serves every `go run` in this
// suite from. Nothing here ever falls back to fetching it.
func requireGoModule(t *testing.T, module, version, fix string) {
	t.Helper()
	if !moduleCached(t, module, version) {
		t.Skipf("%s@v%s is not in the local Go module cache; %s", module, version, fix)
	}
}

// runTask runs one named task from this repository's own live Taskfile.yml,
// with fixtureRoot as the directory it executes in and looks for files
// under -- never this repository's own tree. This is "the leg" a person
// means by `./task <name>`, redirected at a disposable fixture instead of
// pinning how any individual leg happens to be written.
//
// Its environment pins GOPROXY to the local file proxy (fileProxyEnv), which
// a nested `go run` inside the task's own shell -- GITLEAKS, for the
// secrets leg -- inherits the same as any other child process, and forces
// npm offline (markdownlint's own npm ci step never asks the registry
// either, proven in the same way: an empty npm cache under this same
// setting fails fast with ENOTCACHED rather than reaching out).
func runTask(t *testing.T, fixtureRoot, name string) (string, error) {
	t.Helper()
	root := repoRoot(t)
	version := pinnedVersion(t, "TASK_VERSION")
	requireGoModule(t, "github.com/go-task/task/v3", version,
		"run any ./task command once with network access to fetch it")

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run",
		"github.com/go-task/task/v3/cmd/task@v"+version,
		"-d", fixtureRoot, "-t", filepath.Join(root, "Taskfile.yml"), name)
	cmd.Env = append(os.Environ(), fileProxyEnv(t), "npm_config_offline=true")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// requireNode skips, with an actionable fix, a markdownlint-leg test where
// node or npm are not installed, or where this repository's own
// .github/tools/markdownlint has never been installed here. The second
// check is what proves npm's local cache already holds every package the
// leg's own (offline-forced) npm ci step will ask for: npm ci always
// deletes and reinstalls node_modules from the same lockfile, so whatever
// built it here before is exactly what it needs again.
func requireNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed, and the markdownlint leg runs on it")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed, and the markdownlint leg's own npm ci step needs it")
	}
	installed := filepath.Join(repoRoot(t), ".github", "tools", "markdownlint",
		"node_modules", "markdownlint-cli2", "markdownlint-cli2-bin.mjs")
	if _, err := os.Stat(installed); err != nil {
		t.Skip("this repository's own .github/tools/markdownlint has never been installed " +
			"here, so npm's local cache does not hold it yet; run `./task markdownlint` " +
			"once with network access")
	}
}

// requireShellcheck skips a shellcheck-leg test where the binary is not on
// PATH, the same way CI's own `./task tools` would refuse to run the real
// leg.
func requireShellcheck(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("shellcheck"); err != nil {
		t.Skip("shellcheck is not installed, and the shellcheck leg needs it")
	}
}

// seedMarkdownlintTool gives a fixture its own copy of what the real
// markdownlint task needs beside the files under test: the lockfile under
// .github/tools/markdownlint (npm ci always deletes and reinstalls
// node_modules from it, so a symlink into this repository's own copy would
// be destructive) and the live .markdownlint-cli2.jsonc the leg's config
// knob, if that is where a fix lands, would change.
func seedMarkdownlintTool(t *testing.T, fixtureRoot string) {
	t.Helper()
	requireNode(t)
	copyFromRepo(t, fixtureRoot, ".github/tools/markdownlint/package.json")
	copyFromRepo(t, fixtureRoot, ".github/tools/markdownlint/package-lock.json")
	copyFromRepo(t, fixtureRoot, ".markdownlint-cli2.jsonc")
}
