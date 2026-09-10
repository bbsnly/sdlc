package fsx

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteFileAtomicCreatesMissingParents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "c.json")
	if err := WriteFileAtomic(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "{}\n" {
		t.Errorf("contents = %q", got)
	}
}

func TestWriteFileAtomicReplacesCompletely(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := WriteFileAtomic(path, []byte("a much longer first value"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(path, []byte("short"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "short" {
		t.Errorf("contents = %q, want only the new value", got)
	}
}

// The temporary file goes in the target's directory so the rename is a rename
// and not a copy. It must not survive the call.
func TestWriteFileAtomicLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFileAtomic(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "f" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("directory holds %v, want just the target", names)
	}
}

func TestWriteFileAtomicSetsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not carry Unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "secret")
	if err := WriteFileAtomic(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}
}

func TestWriteFileAtomicReportsWhichStepFailed(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("a file, not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := WriteFileAtomic(filepath.Join(blocked, "child"), []byte("x"), 0o644)
	if err == nil {
		t.Fatal("writing under a file succeeded")
	}
	if !strings.Contains(err.Error(), "create ") {
		t.Errorf("error does not name the step that failed: %v", err)
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	if Exists(filepath.Join(dir, "nope")) {
		t.Error("Exists reported a missing file as present")
	}
	if err := os.WriteFile(filepath.Join(dir, "yes"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !Exists(filepath.Join(dir, "yes")) {
		t.Error("Exists reported a present file as missing")
	}
}
