package store

// US-129 AC-4 and AC-5 for Reads itself: a file is read once while the file
// on disk is the one read, and read again once it is not, or once what was
// kept of it, of the directory it is in, or of everything, is forgotten.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAC4AKeptReadIsUsedOnlyWhileTheFileIsTheOneReadOrUntilItIsForgotten(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "record.json")
	r := NewReads()
	if _, err := r.File(path); !os.IsNotExist(err) {
		t.Fatalf("AC-4 a file that is not there read as %v", err)
	}
	if _, err := r.File(path); !os.IsNotExist(err) {
		t.Errorf("AC-4 a file still not there read as %v", err)
	}
	put := func(body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, path); err != nil {
			t.Fatal(err)
		}
	}
	read := func(want string) {
		t.Helper()
		got, err := r.File(path)
		if err != nil || string(got) != want {
			t.Errorf("AC-4 the read is %q (%v), want %q", got, err, want)
		}
	}
	put("one")
	read("one")
	put("two")
	read("two")
	r.Forget(path)
	read("two")
	r.Forget(filepath.Join(dir, "sub"))
	read("two")
	r.Forget()
	read("two")
	var none *Reads
	if got, err := none.File(path); err != nil || string(got) != "two" {
		t.Errorf("AC-4 no Reads read %q (%v)", got, err)
	}
	none.Forget()
	none.keep(path, nil)
	if none.size(path) != 0 {
		t.Error("AC-4 no Reads keeps a size")
	}
	r.keep(path, []byte("two"))
	if r.size(path) != 3 {
		t.Errorf("AC-4 the kept write is %d bytes, want 3", r.size(path))
	}
	r.keep(filepath.Join(dir, "absent"), []byte("x"))
	if r.size(filepath.Join(dir, "absent")) != 0 {
		t.Error("AC-4 a write that is not on disk was kept")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := r.File(path); !os.IsNotExist(err) {
		t.Errorf("AC-4 a file removed since it was read reads as %v", err)
	}
}
