// Every platform a release ships, checked from whichever one this is.
//
// The installers were only ever run on the three architectures CI has runners
// for, so half of what a release publishes -- linux/arm64, darwin/amd64,
// windows/arm64 -- was built, checksummed, attested and shipped without anyone
// ever installing it. What can go wrong there is not arch-specific code, of
// which there is none; it is the installer naming the wrong archive for a
// machine it has never seen, which is a pure function of `uname` and testable
// anywhere.
//
// So `uname` is shimmed, the archive is cross-built for the target, and the
// real install.sh runs against it start to finish. Everything except the last
// line -- running a foreign binary -- is exercised for real.
package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// shipped is what .goreleaser.yml publishes: darwin, linux and windows on
// amd64 and arm64. Windows is absent here because install.sh refuses it by
// design -- that route is install.ps1, which has a runner for each of its
// architectures.
var shipped = []struct {
	goos, goarch string
	// What `uname -s` and `uname -m` say on such a machine. Both spellings
	// of each architecture are real: Linux says aarch64 and x86_64, macOS
	// says arm64, and some BSD-derived userlands say amd64.
	unameS  string
	unameMs []string
}{
	{"linux", "amd64", "Linux", []string{"x86_64", "amd64"}},
	{"linux", "arm64", "Linux", []string{"aarch64", "arm64"}},
	{"darwin", "amd64", "Darwin", []string{"x86_64"}},
	{"darwin", "arm64", "Darwin", []string{"arm64"}},
}

func TestTheShellInstallerInstallsEveryPlatformWeShip(t *testing.T) {
	skipUnlessPOSIX(t)
	for _, target := range shipped {
		for _, machine := range target.unameMs {
			name := fmt.Sprintf("%s_%s_via_%s", target.goos, target.goarch, machine)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				binary, mirror := crossRelease(t, target.goos, target.goarch)
				dir := t.TempDir()
				shim := unameShim(t, target.unameS, machine)

				out, err := runWithEnv(t, serve(t, mirror), dir,
					[]string{
						"SDLC_VERSION=" + version,
						"PATH=" + shim + string(os.PathListSeparator) + os.Getenv("PATH"),
					},
					"sh", "install.sh")
				if err != nil {
					t.Fatalf("install.sh failed for %s/%s: %v\n%s", target.goos, target.goarch, err, out)
				}

				// Byte-identical to the cross-built binary is the whole
				// assertion: the installer chose that archive, found its line
				// in checksums.txt, agreed with the hash, and unpacked it.
				// Any of those going wrong for an unfamiliar `uname` shows up
				// here. Running it is the one thing that cannot be checked
				// from a machine of a different architecture.
				sameFile(t, filepath.Join(dir, "sdlc"), binary)
			})
		}
	}
}

// A machine we publish nothing for must be told so, rather than given a
// 404 from a URL it built out of a name it did not recognise.
func TestTheShellInstallerSaysSoOnAPlatformWeDoNotShip(t *testing.T) {
	skipUnlessPOSIX(t)
	for _, unrecognised := range []struct{ what, s, m string }{
		{"operating system", "FreeBSD", "x86_64"},
		{"architecture", "Linux", "riscv64"},
	} {
		t.Run(unrecognised.what, func(t *testing.T) {
			t.Parallel()
			_, mirror := crossRelease(t, runtime.GOOS, runtime.GOARCH)
			dir := t.TempDir()
			shim := unameShim(t, unrecognised.s, unrecognised.m)

			out, err := runWithEnv(t, serve(t, mirror), dir,
				[]string{
					"SDLC_VERSION=" + version,
					"PATH=" + shim + string(os.PathListSeparator) + os.Getenv("PATH"),
				},
				"sh", "install.sh")
			if err == nil {
				t.Fatalf("install.sh claimed to install on an unsupported platform:\n%s", out)
			}
			if !strings.Contains(out, "no prebuilt binary") {
				t.Errorf("the refusal does not say why:\n%s", out)
			}
			// Being told to build from source is the only useful thing this
			// message can offer, so it is part of the contract.
			if !strings.Contains(out, "go install") {
				t.Errorf("the refusal does not say what to do instead:\n%s", out)
			}
			nothingInstalled(t, dir)
		})
	}
}

// crossRelease builds sdlc for another platform and publishes it the way
// goreleaser would: one archive, named for the target, and a checksums.txt
// that lists it. It returns the binary that went in and the mirror directory.
func crossRelease(t *testing.T, goos, goarch string) (binary, mirror string) {
	t.Helper()
	mirror = t.TempDir()
	binary = crossBuilt(t, goos, goarch)

	var archive string
	if goos == "windows" {
		archive = filepath.Join(mirror, fmt.Sprintf("sdlc_%s_windows_%s.zip", version, goarch))
		writeZip(t, archive, binary)
	} else {
		archive = filepath.Join(mirror, fmt.Sprintf("sdlc_%s_%s_%s.tar.gz", version, goos, goarch))
		writeTarGz(t, archive, binary)
	}

	// Two names in the file, only one of them right, because a checksums.txt
	// with a single line cannot show that the installer looked up the one it
	// needed rather than taking whatever was there.
	checksums := fmt.Sprintf("%s  %s\n%s  %s\n",
		strings.Repeat("0", 64), "sdlc_"+version+"_plan9_mips.tar.gz",
		sha256File(t, archive), filepath.Base(archive))
	if err := os.WriteFile(filepath.Join(mirror, "checksums.txt"), []byte(checksums), 0o644); err != nil {
		t.Fatal(err)
	}
	return binary, mirror
}

func crossBuilt(t *testing.T, goos, goarch string) string {
	t.Helper()
	name := "sdlc"
	if goos == "windows" {
		name = "sdlc.exe"
	}
	out := filepath.Join(t.TempDir(), name)
	build := exec.CommandContext(t.Context(), "go", "build", "-o", out, "./cmd/sdlc")
	build.Dir = repoRoot(t)
	build.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	if combined, err := build.CombinedOutput(); err != nil {
		t.Fatalf("cross-building %s/%s: %v\n%s", goos, goarch, err, combined)
	}
	return out
}

// unameShim writes a `uname` earlier on PATH than the real one, answering for
// a machine this test is not running on. It handles the two forms install.sh
// uses and passes anything else through, so a shim that drifts from the script
// fails loudly rather than quietly reporting the host.
func unameShim(t *testing.T, system, machine string) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
  -s) echo %q ;;
  -m) echo %q ;;
  *) echo "uname shim: unexpected argument $*" >&2; exit 1 ;;
esac
`, system, machine)
	path := filepath.Join(dir, "uname")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func sameFile(t *testing.T, got, want string) {
	t.Helper()
	a, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("nothing was installed: %v", err)
	}
	b, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Errorf("%s is not the binary from the archive for this platform (%d bytes vs %d)", got, len(a), len(b))
	}
}
