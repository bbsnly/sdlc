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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// goInstallBase is the version the proxy below serves, before the content
// hash is appended. The module path has no /vN suffix, so Go accepts only v0
// and v1 for it; 0.9.9 is high enough never to collide with a real tag.
const goInstallBase = "v0.9.9"

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
	// Everything Go caches about a module is keyed on module@version and
	// never revalidated, because a real version is immutable. This one is
	// not: it is built from the working tree, which changes. So the version
	// carries a hash of what went into it, and a changed tree is a different
	// version rather than a stale answer.
	//
	// Two separate caches had to be beaten. The extracted module and the
	// download cache live under GOMODCACHE and evictFromModuleCache clears
	// them; the module *index* lives under GOCACHE and nothing clears it,
	// which is how this test compiled a version of the tree from ten minutes
	// earlier and reported a failure that was not real.
	evictFromModuleCache(t)
	proxy, version := fileProxy(t)

	pkg := "github.com/bbsnly/sdlc/cmd/sdlc@" + version
	cmd := exec.CommandContext(t.Context(), "go", "install", pkg)
	// Outside any module: `go install pkg@version` refuses to run in one.
	cmd.Dir = t.TempDir()
	// The mirror carries this module only. Its dependencies come from
	// wherever they normally would, which in practice is the module cache
	// the rest of the suite has already filled.
	cmd.Env = goEnv(t, gobin, fileURL(proxy)+",https://proxy.golang.org,direct")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go install %s failed: %v\n%s", pkg, err, out)
	}

	if got := goInstalled(t, gobin); got != version {
		t.Errorf("version = %q, want %q -- a module version installed at a version should report that version", got, version)
	}
}

// fileProxy writes a module proxy holding this repository, and returns its
// root and the version it serves. The layout is the one `go help goproxy`
// describes: <module>/@v/<version>.{info,mod,zip}.
//
// The version ends in a hash of everything that goes into the module, so that
// a tree which has changed since the last run is a different version and none
// of Go's caches can answer for it.
func fileProxy(t *testing.T) (proxy, version string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "github.com", "bbsnly", "sdlc", "@v")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join("..", "..")
	files := moduleFiles(t, repo)
	// A prerelease identifier: alphanumerics and hyphens, which is what the
	// hex digest is. It sorts below v0.9.9 and above nothing anyone has.
	version = goInstallBase + "-0.a" + contentHash(t, repo, files)

	gomod, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, body []byte) {
		if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(version+".mod", gomod)
	write(version+".info", fmt.Appendf(nil, "{%q:%q,%q:%q}\n", "Version", version, "Time", "2026-01-01T00:00:00Z"))
	write("list", []byte(version+"\n"))

	writeModuleZip(t, filepath.Join(dir, version+".zip"), repo, files, version)
	return root, version
}

// moduleFiles is everything that goes into the module zip, in a stable order.
// Only what `./cmd/sdlc` needs: a module zip may not contain a nested go.mod,
// and shipping the whole tree would put every fixture and vendored script in
// it for no reason.
func moduleFiles(t *testing.T, repo string) []string {
	t.Helper()
	files := []string{"go.mod"}
	if _, err := os.Stat(filepath.Join(repo, "go.sum")); err == nil {
		files = append(files, "go.sum")
	}
	for _, tree := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(repo, tree), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(repo, p)
			if err != nil {
				return err
			}
			if strings.HasSuffix(p, ".go") || strings.Contains(filepath.ToSlash(rel), "/templates/") {
				files = append(files, rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(files)
	return files
}

// contentHash is what makes the version unique to this tree: every path and
// every byte that will be in the zip.
func contentHash(t *testing.T, repo string, files []string) string {
	t.Helper()
	sum := sha256.New()
	for _, rel := range files {
		body, err := os.ReadFile(filepath.Join(repo, rel))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(sum, "%s %d\n", filepath.ToSlash(rel), len(body))
		sum.Write(body)
	}
	return hex.EncodeToString(sum.Sum(nil))[:12]
}

// writeModuleZip builds the zip the proxy serves.
func writeModuleZip(t *testing.T, path, repo string, files []string, version string) {
	t.Helper()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	w := zip.NewWriter(out)
	prefix := "github.com/bbsnly/sdlc@" + version + "/"

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

	for _, rel := range files {
		add(rel)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// evictFromModuleCache removes every copy of this repository at a test version
// from the module cache, so that a run does not leave one behind for every
// change made to the tree. Only this module: the dependencies stay cached,
// which is what keeps the test off the network.
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
	stale, err := filepath.Glob(filepath.Join(cache, "github.com", "bbsnly", "sdlc@"+goInstallBase+"*"))
	if err != nil {
		t.Fatal(err)
	}
	stale = append(stale, filepath.Join(cache, "cache", "download", "github.com", "bbsnly", "sdlc", "@v"))
	for _, p := range stale {
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

// fileURL turns a directory into the file:// URL a GOPROXY list accepts.
//
// Three slashes, not two: on Windows the path starts with a drive letter, and
// `file://C:/...` reads C: as the host -- "invalid file:// proxy URL with
// non-path elements", which is how this first failed on windows-11-arm. The
// leading slash is already there on POSIX, so it is added only if missing.
func fileURL(dir string) string {
	return "file:///" + strings.TrimPrefix(filepath.ToSlash(dir), "/")
}
