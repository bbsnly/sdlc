package pathrules

// US-108 AC-18: on macOS /System/Volumes/Data<path> is <path>. It is a
// firmlink, not a symlink, so resolving does not fold it, and a file tool's
// write to the project spelled through it was read as outside the project:
// write-outside-repository where the plain path was orchestrator-delegates,
// and an implementer's ordinary write refused (ANALYSIS.md 7.8). foldedRel is
// where the project's own path is matched against a spelling filepath.Rel
// cannot, so that is where the firmlink is read.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const dataVolume = "/System/Volumes/Data"

// viaDataVolume is p spelled through the firmlink, and skips where that is not
// the same directory on this machine.
func viaDataVolume(t *testing.T, p string) string {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("/System/Volumes/Data is a macOS firmlink; no other platform has it")
	}
	spelled := dataVolume + p
	there, err := os.Stat(spelled)
	if err != nil {
		t.Skipf("%s is not reachable through the firmlink on this machine: %v", p, err)
	}
	if here, err := os.Stat(p); err != nil || !os.SameFile(here, there) {
		t.Skipf("%s is not the same directory as %s on this machine", spelled, p)
	}
	return spelled
}

func TestAC18FoldedRelReadsAPathThroughTheDataVolumeFirmlinkAsTheProjects(t *testing.T) {
	root := project(t)
	through := viaDataVolume(t, root)
	for _, tc := range []struct{ name, p, want string }{
		{"the root", through, ""},
		{"a file", through + "/internal/invoice.go", "internal/invoice.go"},
		{"loop state", through + "/.sdlc/state/active", ".sdlc/state/active"},
		{"in capitals", strings.ToUpper(through) + "/.sdlc/config.json", ".sdlc/config.json"},
		{"the firmlink in lower case", strings.ToLower(dataVolume) + root + "/CLAUDE.md", "claude.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, inside := foldedRel(root, tc.p); !inside || got != tc.want {
				t.Errorf("AC-18: foldedRel(root, %s) = %q, %v; want %q inside", tc.p, got, inside, tc.want)
			}
		})
	}
}

func TestAC18RelReadsAPathThroughTheDataVolumeFirmlinkAsTheProjectPath(t *testing.T) {
	root := project(t)
	through := viaDataVolume(t, root)
	for _, tc := range []struct{ name, p, want string }{
		{"a file", filepath.Join(through, "internal", "invoice.go"), "internal/invoice.go"},
		{"loop state", filepath.Join(through, ".sdlc", "state", "active"), ".sdlc/state/active"},
		{"the root", through, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, outside := Rel(root, tc.p); outside || got != tc.want {
				t.Errorf("AC-18: Rel(root, %s) = %q, outside %v; want %q inside", tc.p, got, outside, tc.want)
			}
		})
	}
	// The project named through the firmlink is the project too, as Claude
	// Code may report the working directory that way.
	if got, outside := Rel(through, filepath.Join(root, "CLAUDE.md")); outside || got != "CLAUDE.md" {
		t.Errorf("AC-18: Rel(the project through the firmlink, its CLAUDE.md) = %q, outside %v; want CLAUDE.md inside", got, outside)
	}
	if got, inside := Within(root, filepath.Join(through, ".git")); !inside || got != ".git" {
		t.Errorf("AC-18: Within(root, .git through the firmlink) = %q, %v; want .git inside", got, inside)
	}
}

// What is not the project stays outside it whichever way it is spelled: a
// directory beside it, the volume's own root, and a prefix that is not the
// firmlink, only a name starting like it.
func TestAC18APathThroughTheFirmlinkThatIsNotTheProjectStaysOutside(t *testing.T) {
	parent := project(t)
	root := filepath.Join(parent, "proj")
	beside := filepath.Join(parent, "proj-beside")
	for _, dir := range []string{root, beside} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	through := viaDataVolume(t, beside)
	for _, p := range []string{
		filepath.Join(through, "x.go"),
		dataVolume,
		dataVolume + "x" + root + "/x.go",
		filepath.Join(viaDataVolume(t, parent), "x.go"),
	} {
		if got, outside := Rel(root, p); !outside {
			t.Errorf("AC-18: Rel(root, %s) = %q inside the project", p, got)
		}
		if got, inside := foldedRel(root, p); inside {
			t.Errorf("AC-18: foldedRel(root, %s) = %q inside the project", p, got)
		}
	}
}
