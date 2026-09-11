// The `go install` route, which no other test covers.
//
// It is the only documented install that does not download a release: it
// builds from source on the user's own machine, so nothing about it is
// exercised by pointing an installer at a local mirror. What it can still get
// wrong is everything the other routes get right by accident -- the module
// building at all from a clean cache, the binary landing where GOBIN says,
// and `sdlc version` reporting something true about a build nobody published.
package install

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goInstallVersion is the version the proxy below serves. The module path has
// no /vN suffix, so Go accepts only v0 and v1 for it; 0.9.9 is high enough
// never to collide with a real tag.
const goInstallVersion = "v0.9.9"

// goEnv is the environment every `go install` here runs under. It is built
// from scratch rather than inherited: GOFLAGS, GOPRIVATE or a GOPROXY set on
// the machine would each change what is being tested, and GOPRIVATE in
// particular sends the fetch to github.com instead of the mirror.
func goEnv(t *testing.T, gobin, goproxy string) []string {
	t.Helper()
	keep := []string{"PATH", "HOME", "USERPROFILE", "SystemRoot", "TMP", "TEMP", "TMPDIR", "LOCALAPPDATA", "APPDATA", "GOCACHE", "GOMODCACHE", "GOPATH", "GOROOT"}
	// -modcacherw keeps the extracted module writable, so the eviction in
	// evictFromModuleCache can actually delete it.
	env := []string{"GOBIN=" + gobin, "GOFLAGS=-modcacherw", "GOPRIVATE=", "GONOPROXY=", "GONOSUMDB=", "GOWORK=off"}
	if goproxy != "" {
		env = append(env, "GOPROXY="+goproxy, "GOSUMDB=off", "GONOSUMCHECK=1")
	}
	for _, name := range keep {
		if v, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+v)
		}
	}
	return env
}

// goInstalled runs the installed binary and returns the line `sdlc version`
// prints, failing the test if anything about the install did not work.
func goInstalled(t *testing.T, gobin string) string {
	t.Helper()
	target := filepath.Join(gobin, binaryName())
	if _, err := os.Stat(target); err != nil {
		entries, _ := os.ReadDir(gobin)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("go install put nothing at %s (GOBIN holds %v): %v", target, names, err)
	}
	out, err := exec.CommandContext(t.Context(), target, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("the installed binary does not run: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestGoInstallFromSourcePutsAWorkingBinaryOnDisk(t *testing.T) {
	gobin := t.TempDir()
	cmd := exec.CommandContext(t.Context(), "go", "install", "./cmd/sdlc")
	cmd.Dir = ".." + string(filepath.Separator) + ".."
	cmd.Env = goEnv(t, gobin, "")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go install ./cmd/sdlc failed: %v\n%s", err, out)
	}

	// A build nobody published still has to say something true about itself.
	// The empty string and a made-up release tag are both worse than
	// "0.0.0-dev", and the version package exists to make sure neither
	// happens.
	got := goInstalled(t, gobin)
	if got == "" {
		t.Fatal("the installed binary reported no version at all")
	}
	if !strings.HasPrefix(got, "v") && !strings.HasPrefix(got, "0.0.0-dev") {
		t.Errorf("version = %q, want a module version or 0.0.0-dev", got)
	}
}

// This is the route the documentation actually tells people to use, and the
// promise it makes -- that `sdlc version` reports the module version -- is
// only true because the version package falls back to build info. Serving the
// module from a file proxy is the only way to install a real version of it
// without tagging one.
func TestGoInstallAtAVersionReportsThatVersion(t *testing.T) {
	gobin := t.TempDir()
	proxy := fileProxy(t)
	// The module cache is keyed on module@version and never revalidated, so
	// without this the second run of this test on a machine installs the
	// copy the first run left behind rather than the one just built. That is
	// not a slow test made fast; it is a test that stops testing. Found by
	// breaking the version package and watching this still pass.
	evictFromModuleCache(t)

	pkg := "github.com/bbsnly/sdlc/cmd/sdlc@" + goInstallVersion
	cmd := exec.CommandContext(t.Context(), "go", "install", pkg)
	// Outside any module: `go install pkg@version` refuses to run in one.
	cmd.Dir = t.TempDir()
	// The mirror carries this module only. Its dependencies come from
	// wherever they normally would, which in practice is the module cache
	// the rest of the suite has already filled.
	cmd.Env = goEnv(t, gobin, "file://"+filepath.ToSlash(proxy)+",https://proxy.golang.org,direct")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go install %s failed: %v\n%s", pkg, err, out)
	}

	if got := goInstalled(t, gobin); got != goInstallVersion {
		t.Errorf("version = %q, want %q -- a module version installed at a version should report that version", got, goInstallVersion)
	}
}

// fileProxy writes a module proxy holding this repository at
// goInstallVersion, and returns its root. The layout is the one `go help
// goproxy` describes: <module>/@v/<version>.{info,mod,zip}.
func fileProxy(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "github.com", "bbsnly", "sdlc", "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join("..", "..")
	gomod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, body []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(goInstallVersion+".mod", gomod)
	write(goInstallVersion+".info", fmt.Appendf(nil, "{%q:%q,%q:%q}\n", "Version", goInstallVersion, "Time", "2026-01-01T00:00:00Z"))
	write("list", []byte(goInstallVersion+"\n"))

	writeModuleZip(t, filepath.Join(dir, goInstallVersion+".zip"), repo)
	return root
}

// writeModuleZip builds the zip the proxy serves. Only what `./cmd/sdlc`
// needs goes in: a module zip may not contain a nested go.mod, and shipping
// the whole tree would put every fixture and vendored script in it for no
// reason.
func writeModuleZip(t *testing.T, path, repo string) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	w := zip.NewWriter(out)
	prefix := "github.com/bbsnly/sdlc@" + goInstallVersion + "/"

	add := func(rel string) {
		src, err := os.Open(filepath.Join(repo, rel))
		if err != nil {
			t.Fatal(err)
		}
		defer src.Close()
		// Entries are named with forward slashes whatever the host does, and
		// directory entries are left out: the zip reader rejects them.
		dst, err := w.Create(prefix + filepath.ToSlash(rel))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(dst, src); err != nil {
			t.Fatal(err)
		}
	}

	add("go.mod")
	if _, err := os.Stat(filepath.Join(repo, "go.sum")); err == nil {
		add("go.sum")
	}
	for _, tree := range []string{"cmd", "internal"} {
		walk(t, repo, tree, add)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// walk calls add for every file under tree that belongs in the module zip:
// Go source, and the files packages embed.
func walk(t *testing.T, repo, tree string, add func(string)) {
	t.Helper()
	root := filepath.Join(repo, tree)
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(repo, p)
		if err != nil {
			return err
		}
		switch {
		case strings.HasSuffix(p, ".go"):
			add(rel)
		case strings.Contains(filepath.ToSlash(rel), "/templates/"):
			add(rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// evictFromModuleCache removes this repository at goInstallVersion from the
// module cache. Only that one entry: the dependencies stay cached, which is
// what keeps the test off the network.
func evictFromModuleCache(t *testing.T) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("reading GOMODCACHE: %v", err)
	}
	cache := strings.TrimSpace(string(out))
	if cache == "" {
		return
	}
	for _, p := range []string{
		filepath.Join(cache, "github.com", "bbsnly", "sdlc@"+goInstallVersion),
		filepath.Join(cache, "cache", "download", "github.com", "bbsnly", "sdlc", "@v"),
	} {
		// Go writes the cache read-only, and on Windows a read-only file
		// cannot be unlinked at all, so the permissions come off first.
		_ = filepath.WalkDir(p, func(name string, _ os.DirEntry, err error) error {
			if err == nil {
				_ = os.Chmod(name, 0o755)
			}
			// A path that cannot be walked is one that is already not there.
			return nil //nolint:nilerr // nothing to recover from; the RemoveAll below reports what matters
		})
		if err := os.RemoveAll(p); err != nil {
			t.Fatalf("evicting %s from the module cache: %v", p, err)
		}
	}
}
