package fsx

// US-134 AC-2 and AC-13 (security F-10): the gate's and the freeze's output
// files, and the .gitignore beside them, are read and written only as the
// regular file with one name that sits at the path. A link planted at the
// name, a named pipe, or a second name for a file elsewhere is refused rather
// than read or written through. internal/cli's rows plant each at the name;
// these hold the two checks where they are written.

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAC2ARegularFileIsOpenedToReadWithWhatItsDescriptorIs(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitignore")
	if err := os.WriteFile(path, []byte("implementation-run.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, fi, err := OpenRegular(path)
	if err != nil {
		t.Fatalf("OpenRegular of a regular file: %v", err)
	}
	defer func() { _ = f.Close() }()
	if !fi.Mode().IsRegular() || fi.Size() != int64(len("implementation-run.log\n")) {
		t.Errorf("OpenRegular returned %v, size %d, want the file's own information", fi.Mode(), fi.Size())
	}
	got, err := io.ReadAll(f)
	if err != nil || string(got) != "implementation-run.log\n" {
		t.Errorf("read %q, %v, want the file's content", got, err)
	}
	if _, err := f.WriteString("x"); err == nil {
		t.Errorf("the descriptor OpenRegular returned can be written, want it open to read only")
	}
}

func TestAC2AMissingFileIsNotExistAndNotTakenForALink(t *testing.T) {
	_, _, err := OpenRegular(filepath.Join(t.TempDir(), "absent"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("OpenRegular of a missing file is %v, want fs.ErrNotExist so that a caller can write it new", err)
	}
	if errors.Is(err, ErrNotRegular) {
		t.Errorf("a missing file is reported as not regular")
	}
}

func TestAC2ADirectoryAtTheNameIsNotRegular(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".gitignore")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	f, _, err := OpenRegular(path)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, ErrNotRegular) {
		t.Errorf("OpenRegular of a directory is %v, want ErrNotRegular", err)
	}
}

func TestAC2ALinkAtTheNameIsNeverFollowedEvenToARegularFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("not the project's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".gitignore")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("a symbolic link needs a privilege this Windows account does not hold: %v", err)
		}
		t.Fatal(err)
	}
	f, _, err := OpenRegular(link)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, ErrNotRegular) {
		t.Errorf("OpenRegular of a link to a regular file is %v, want ErrNotRegular", err)
	}
}

func TestAC2AFileWithOneNameHasOneNameAndASecondNameIsSeen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "implementation-run.log")
	if err := os.WriteFile(path, []byte("output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, fi, err := OpenRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	one := OneName(f, fi)
	_ = f.Close()
	if !one {
		t.Errorf("a file with one name is reported as having more")
	}
	if err := os.Link(path, filepath.Join(dir, "second-name")); err != nil {
		t.Skipf("this file system makes no hard link: %v", err)
	}
	f, fi, err = OpenRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if OneName(f, fi) {
		t.Errorf("a file with a second name is reported as having one, so a write would reach the other name too")
	}
}

func TestAC2ReplacePutsTheTemporaryFileAtThePathInPlaceOfWhatWasThere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "implementation-run.log")
	tmp := filepath.Join(dir, ".implementation-run.log.tmp1")
	if err := os.WriteFile(path, []byte("an earlier run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmp, []byte("this run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(tmp, path); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "this run\n" {
		t.Errorf("the path holds %q, %v, want this run's output", got, err)
	}
	if _, err := os.Lstat(tmp); !os.IsNotExist(err) {
		t.Errorf("the temporary file is still there after Replace: %v", err)
	}
}

func TestAC2ReplaceOfAMissingTemporaryFileIsAnErrorAndLeavesThePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "implementation-run.log")
	if err := os.WriteFile(path, []byte("an earlier run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(filepath.Join(dir, "absent.tmp"), path); err == nil {
		t.Errorf("Replace of a missing file returned no error")
	}
	if got, _ := os.ReadFile(path); string(got) != "an earlier run\n" {
		t.Errorf("a failed Replace changed the path to %q", got)
	}
}

func TestAC2ReplaceOverALinkReplacesTheLinkAndLeavesItsTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("not the project's\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "implementation-run.log")
	if err := os.Symlink(target, path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("a symbolic link needs a privilege this Windows account does not hold: %v", err)
		}
		t.Fatal(err)
	}
	tmp := filepath.Join(dir, ".implementation-run.log.tmp1")
	if err := os.WriteFile(tmp, []byte("this run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Replace(tmp, path); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
		t.Errorf("the path is %v, %v after Replace, want a regular file in place of the link", fi, err)
	}
	if got, _ := os.ReadFile(target); string(got) != "not the project's\n" {
		t.Errorf("Replace wrote through the link: its target holds %q", got)
	}
}
