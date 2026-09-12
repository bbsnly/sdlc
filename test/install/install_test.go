// Package install tests the three installers against a release that is real
// in every way except where it is hosted.
//
// The installer is the first thing every user runs. Testing it only by cutting
// a release means the first person to find a broken one is a stranger, so each
// installer is pointed at a local mirror -- SDLC_DOWNLOAD_BASE -- serving an
// archive built and checksummed exactly the way goreleaser builds and
// checksums one.
package install

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

const version = "9.9.9"

// binaryName is what the archive holds and what lands in the install
// directory, which differ only in the extension Windows insists on.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "sdlc.exe"
	}
	return "sdlc"
}

func archiveName() string {
	goos := runtime.GOOS
	arch := runtime.GOARCH
	if goos == "windows" {
		return fmt.Sprintf("sdlc_%s_windows_%s.zip", version, arch)
	}
	return fmt.Sprintf("sdlc_%s_%s_%s.tar.gz", version, goos, arch)
}

// built compiles the binary once for the whole package. Four installers
// downloading the same thing do not need four builds of it.
var buildOnce struct {
	sync.Once
	dir  string
	path string
	err  error
}

// The build outlives every individual test, so it is cleaned up here rather
// than through t.Cleanup, which would delete it while the next test is still
// using it.
func TestMain(m *testing.M) {
	code := m.Run()
	if buildOnce.dir != "" {
		_ = os.RemoveAll(buildOnce.dir)
	}
	os.Exit(code)
}

func built(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "sdlc-build-")
		if err != nil {
			buildOnce.err = err
			return
		}
		buildOnce.dir = dir
		buildOnce.path = filepath.Join(dir, binaryName())
		build := exec.CommandContext(context.Background(), "go", "build", "-o", buildOnce.path, "../../cmd/sdlc")
		out, err := build.CombinedOutput()
		if err != nil {
			buildOnce.err = fmt.Errorf("building sdlc: %w\n%s", err, out)
		}
	})
	if buildOnce.err != nil {
		t.Fatal(buildOnce.err)
	}
	return buildOnce.path
}

// release builds the archive and the checksums file a real release would
// publish for this machine, and returns the directory holding them.
func release(t *testing.T, corrupt bool) string {
	t.Helper()
	dir := t.TempDir()

	binary := built(t)

	archive := filepath.Join(dir, archiveName())
	if strings.HasSuffix(archive, ".zip") {
		writeZip(t, archive, binary)
	} else {
		writeTarGz(t, archive, binary)
	}

	// The checksum is taken before the corruption, so the file on the mirror
	// is exactly what a tampered download looks like: listed, and wrong.
	sum := sha256File(t, archive)
	if corrupt {
		appendByte(t, archive)
	}
	checksums := fmt.Sprintf("%s  %s\n", sum, archiveName())
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(checksums), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// serve publishes a release directory at the path layout GitHub uses, so the
// only thing the installers do differently is the host they ask.
//
// That includes /releases/latest, which answers with the redirect an
// installer reads when no version was given. Without it the resolve-the-latest
// path -- the one every reader of the documentation takes -- could only be
// exercised by a real release.
func serve(t *testing.T, dir string) string {
	t.Helper()
	const downloads = "/releases/download/"
	mux := http.NewServeMux()
	mux.Handle(downloads+"v"+version+"/",
		http.StripPrefix(downloads+"v"+version+"/", http.FileServer(http.Dir(dir))))
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v"+version, http.StatusFound)
	})
	mux.HandleFunc("/releases/tag/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL + "/releases/download"
}

func writeTarGz(t *testing.T, path, binary string) {
	t.Helper()
	body, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	header := &tar.Header{Name: binaryName(), Mode: 0o755, Size: int64(len(body))}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	for _, closer := range []io.Closer{tw, gz} {
		if err := closer.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func writeZip(t *testing.T, path, binary string) {
	t.Helper()
	body, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	zw := zip.NewWriter(file)
	entry, err := zw.Create(binaryName())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func appendByte(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("x"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

// run executes an installer and returns everything it said, whether or not it
// worked. An installer's output is most of what it is, so both are returned.
func run(t *testing.T, base, dir string, name string, args ...string) (string, error) {
	t.Helper()
	return runWithEnv(t, base, dir, []string{"SDLC_VERSION=" + version}, name, args...)
}

// runResolvingLatest is the same, with no version given: the installer has to
// ask the mirror which one is newest, the way it does for a real user.
func runResolvingLatest(t *testing.T, base, dir string, name string, args ...string) (string, error) {
	t.Helper()
	return runWithEnv(t, base, dir, nil, name, args...)
}

func runWithEnv(t *testing.T, base, dir string, extra []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	cmd.Env = append(os.Environ(), extra...)
	cmd.Env = append(cmd.Env,
		"SDLC_DOWNLOAD_BASE="+base,
		"SDLC_INSTALL_DIR="+dir,
		// install.ps1 writes a PATH entry into the user environment, which
		// outlives the test and points at a temporary directory that does
		// not. This is the same switch a Dockerfile would use.
		"SDLC_NO_PATH=1",
		// The installers print a PATH hint when the install directory is not
		// on PATH, which it never is here. Keeping PATH as it is means that
		// branch runs in the test too.
	)
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// installed asserts the binary landed and works, which is the only definition
// of "installed" worth having.
func installed(t *testing.T, dir string) {
	t.Helper()
	target := filepath.Join(dir, binaryName())
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("nothing was installed: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("%s is not executable (mode %v)", target, info.Mode())
	}
	out, err := exec.CommandContext(t.Context(), target, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("the installed binary does not run: %v\n%s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "v") {
		t.Errorf("the installed binary did not report a version: %q", out)
	}
}

func nothingInstalled(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("%s was left behind in the install directory", entry.Name())
	}
}

func TestTheShellInstallerPutsAWorkingBinaryOnDisk(t *testing.T) {
	skipUnlessPOSIX(t)
	dir := t.TempDir()
	out, err := run(t, serve(t, release(t, false)), dir, "sh", "install.sh")
	if err != nil {
		t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	installed(t, dir)
	if !strings.Contains(out, "/plugin marketplace add bbsnly/sdlc") {
		t.Errorf("the installer did not say what to do next:\n%s", out)
	}
}

func TestTheShellInstallerRefusesADownloadThatDoesNotMatchItsChecksum(t *testing.T) {
	skipUnlessPOSIX(t)
	dir := t.TempDir()
	out, err := run(t, serve(t, release(t, true)), dir, "sh", "install.sh")
	if err == nil {
		t.Fatalf("install.sh installed a tampered archive:\n%s", out)
	}
	if !strings.Contains(out, "does not match its checksum") {
		t.Errorf("the refusal did not say why:\n%s", out)
	}
	nothingInstalled(t, dir)
}

func TestThePowerShellInstallerPutsAWorkingBinaryOnDisk(t *testing.T) {
	skipUnlessWindows(t)
	dir := t.TempDir()
	out, err := run(t, serve(t, release(t, false)), dir,
		"powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", "install.ps1")
	if err != nil {
		t.Fatalf("install.ps1 failed: %v\n%s", err, out)
	}
	installed(t, dir)
}

func TestThePowerShellInstallerRefusesADownloadThatDoesNotMatchItsChecksum(t *testing.T) {
	skipUnlessWindows(t)
	dir := t.TempDir()
	out, err := run(t, serve(t, release(t, true)), dir,
		"powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", "install.ps1")
	if err == nil {
		t.Fatalf("install.ps1 installed a tampered archive:\n%s", out)
	}
	if !strings.Contains(out, "does not match its checksum") {
		t.Errorf("the refusal did not say why:\n%s", out)
	}
	nothingInstalled(t, dir)
}

func TestTheNpmInstallerPutsAWorkingBinaryOnDisk(t *testing.T) {
	skipUnlessNode(t)
	dir := t.TempDir()
	out, err := run(t, serve(t, release(t, false)), dir, "node", "npm/bin/sdlc-install.js", "install")
	if err != nil {
		t.Fatalf("the npm installer failed: %v\n%s", err, out)
	}
	installed(t, dir)
}

func TestTheNpmInstallerRefusesADownloadThatDoesNotMatchItsChecksum(t *testing.T) {
	skipUnlessNode(t)
	dir := t.TempDir()
	out, err := run(t, serve(t, release(t, true)), dir, "node", "npm/bin/sdlc-install.js", "install")
	if err == nil {
		t.Fatalf("the npm installer installed a tampered archive:\n%s", out)
	}
	if !strings.Contains(out, "does not match its checksum") {
		t.Errorf("the refusal did not say why:\n%s", out)
	}
	nothingInstalled(t, dir)
}

// Scope, declared rather than hidden: install.sh is for macOS and Linux and
// refuses to run anywhere else by design, so on Windows there is nothing here
// to test. The npm installer, which does support Windows, runs on all three.
func skipUnlessPOSIX(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is for macOS and Linux; Windows is install.ps1 and the npm installer")
	}
}

// install.ps1 is the Windows installer, and PowerShell is where it runs. It is
// not exercised on macOS or Linux even where pwsh happens to be installed:
// what it does -- the user PATH entry, Expand-Archive -- is Windows.
func skipUnlessWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("install.ps1 is the Windows installer; this leg runs on the Windows CI runner")
	}
}

func skipUnlessNode(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH; this leg runs on every CI platform, which all have it")
	}
}

// Every reader of the documentation takes this path: no version given, so the
// installer has to ask which release is newest and read it out of a redirect.
// It was the one branch of all three installers that no test ran, and the
// PowerShell one reads the redirect differently on 5.1 and on 7.
func TestEachInstallerResolvesTheLatestVersionForItself(t *testing.T) {
	base := serve(t, release(t, false))
	for _, route := range installRoutes() {
		t.Run(route.name, func(t *testing.T) {
			route.skip(t)
			dir := t.TempDir()
			out, err := runResolvingLatest(t, base, dir, route.command[0], route.command[1:]...)
			if err != nil {
				t.Fatalf("%s could not resolve the latest version: %v\n%s", route.name, err, out)
			}
			installed(t, dir)
			if !strings.Contains(out, version) {
				t.Errorf("the installer never named the version it chose:\n%s", out)
			}
		})
	}
}

// A release that dropped a platform is the case this branch exists for, and
// the refusal has to leave nothing behind -- the same promise as a bad
// checksum, on a path that reaches a different line of code.
func TestEachInstallerRefusesAReleaseThatDoesNotListThisPlatform(t *testing.T) {
	base := serve(t, releaseMissingThisPlatform(t))
	for _, route := range installRoutes() {
		t.Run(route.name, func(t *testing.T) {
			route.skip(t)
			dir := t.TempDir()
			out, err := run(t, base, dir, route.command[0], route.command[1:]...)
			if err == nil {
				t.Fatalf("%s installed from a release that does not list it:\n%s", route.name, out)
			}
			if !strings.Contains(out, "does not list") {
				t.Errorf("the refusal did not say why:\n%s", out)
			}
			nothingInstalled(t, dir)
		})
	}
}

type installRoute struct {
	name    string
	command []string
	skip    func(*testing.T)
}

// installRoutes is the three ways in, so that a claim made about one of them
// can be made about all three in the same breath.
func installRoutes() []installRoute {
	return []installRoute{
		{"shell", []string{"sh", "install.sh"}, skipUnlessPOSIX},
		{"powershell", []string{"pwsh", "-NoProfile", "-File", "install.ps1"}, skipUnlessWindows},
		// Windows PowerShell 5.1, which is what is already on a Windows
		// machine and therefore what most people run this with. It is not
		// pwsh with an older version number: install.ps1 reads the redirect
		// that resolves "latest" out of a thrown exception here and out of a
		// returned response on 7, and only one of those two branches was ever
		// executed by a test.
		{"windows powershell", []string{
			"powershell", "-NoProfile", "-NonInteractive",
			"-ExecutionPolicy", "Bypass", "-File", "install.ps1",
		}, skipUnlessWindows},
		{"npm", []string{"node", "npm/bin/sdlc-install.js"}, skipUnlessNode},
	}
}

// releaseMissingThisPlatform is a release whose checksums.txt is real but does
// not mention the archive this machine needs.
func releaseMissingThisPlatform(t *testing.T) string {
	t.Helper()
	dir := release(t, false)
	checksums := filepath.Join(dir, "checksums.txt")
	raw, err := os.ReadFile(checksums)
	if err != nil {
		t.Fatal(err)
	}
	other := strings.ReplaceAll(string(raw), archiveName(), "sdlc_"+version+"_plan9_mips.tar.gz")
	if err := os.WriteFile(checksums, []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
