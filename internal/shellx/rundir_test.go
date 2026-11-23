package shellx

// US-134 AC-10 and AC-17, at the package that makes the run directory: one
// run's commands are handed one new, empty SDLC_RUN_DIR outside the project,
// and a narrow run's list of test files beside it; what the run registered is
// gone once it is closed or stopped. internal/cli's rows drive this through
// the gate; these hold it where it is written, so that a RunDir that only
// looked right through one caller does not pass.

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// rdTempDir points the system's temporary directory at a directory of the
// test's own, so that what NewRunDir makes is inside t.TempDir().
func rdTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, dir)
	}
	return dir
}

// rdNew is NewRunDir, closed when the test ends.
func rdNew(t *testing.T) *RunDir {
	t.Helper()
	d, err := NewRunDir()
	if err != nil {
		t.Fatalf("NewRunDir: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func rdRegistered(path string) bool {
	removals.Lock()
	defer removals.Unlock()
	return slices.Contains(removals.paths, path)
}

func TestAC10ARunDirectoryIsMadeNewAndPrivateUnderTheSystemsTemporaryDirectoryWithAnEmptyRun(t *testing.T) {
	tmp := rdTempDir(t)
	d := rdNew(t)
	if filepath.Dir(d.Path()) != filepath.Clean(tmp) {
		t.Errorf("the run's directory is %q, want it directly under the temporary directory %q", d.Path(), tmp)
	}
	if !strings.HasPrefix(filepath.Base(d.Path()), "sdlc-run-") {
		t.Errorf("the run's directory is named %q, want it to start sdlc-run-", filepath.Base(d.Path()))
	}
	if d.Run() != filepath.Join(d.Path(), "run") {
		t.Errorf("Run() is %q, want run/ inside %q", d.Run(), d.Path())
	}
	fi, err := os.Stat(d.Run())
	if err != nil || !fi.IsDir() {
		t.Fatalf("the run directory %q is not a directory: %v", d.Run(), err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Errorf("the run directory has mode %v, want 0700 so that no other account reads the run", fi.Mode().Perm())
	}
	entries, err := os.ReadDir(d.Run())
	if err != nil || len(entries) != 0 {
		t.Errorf("the run directory holds %v (%v), want it empty when the first command begins", entries, err)
	}
	if !rdRegistered(d.Path()) {
		t.Errorf("the run's directory %q is not registered, so an interrupt would leave it behind", d.Path())
	}
}

func TestAC10TwoRunsAreHandedTwoDifferentDirectories(t *testing.T) {
	rdTempDir(t)
	first, second := rdNew(t), rdNew(t)
	if first.Path() == second.Path() || first.Run() == second.Run() {
		t.Errorf("two runs share %q, want each run its own directory so that no run reads another's profile", first.Run())
	}
}

func TestAC10ARunDirectoryThatCannotBeMadeIsAnErrorAndRegistersNothing(t *testing.T) {
	tmp := rdTempDir(t)
	missing := filepath.Join(tmp, "not-there")
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, missing)
	}
	d, err := NewRunDir()
	if err == nil {
		_ = d.Close()
		t.Fatalf("NewRunDir under a missing temporary directory returned %q, want an error", d.Path())
	}
	removals.Lock()
	defer removals.Unlock()
	for _, p := range removals.paths {
		if strings.HasPrefix(p, missing) {
			t.Errorf("a run that was never made left %q registered", p)
		}
	}
}

func TestAC10TheRunsEnvironmentReplacesAnInheritedRunDirectoryWithTheRunsOwnSlashSeparated(t *testing.T) {
	rdTempDir(t)
	d := rdNew(t)
	base := []string{"PATH=/usr/bin", RunDirVar + "=/somewhere/else", TestFilesVar + "=/an/old/list", "HOME=/home/dev"}
	env := d.Env(base)
	want := RunDirVar + "=" + filepath.ToSlash(d.Run())
	var runs, lists int
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case RunDirVar:
			runs++
			if kv != want {
				t.Errorf("the run is handed %q, want %q", kv, want)
			}
		case TestFilesVar:
			lists++
		}
	}
	if runs != 1 {
		t.Errorf("the environment sets %s %d times, want once: %q", RunDirVar, runs, env)
	}
	if lists != 0 {
		t.Errorf("a run with no list of tests still hands on %s: %q", TestFilesVar, env)
	}
	for _, keep := range []string{"PATH=/usr/bin", "HOME=/home/dev"} {
		if !slices.Contains(env, keep) {
			t.Errorf("the environment lost %q: %q", keep, env)
		}
	}
	if !slices.Contains(base, RunDirVar+"=/somewhere/else") || len(base) != 4 {
		t.Errorf("Env changed the caller's slice: %q", base)
	}
}

func TestAC10OnWindowsAnInheritedRunDirectoryInAnotherCaseIsTakenOutToo(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("names that differ only in case are one variable only on Windows")
	}
	rdTempDir(t)
	d := rdNew(t)
	env := d.Env([]string{strings.ToLower(RunDirVar) + "=C:/elsewhere", "Sdlc_Test_Files=C:/old"})
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if (strings.EqualFold(name, RunDirVar) && name != RunDirVar) || strings.EqualFold(name, TestFilesVar) {
			t.Errorf("an inherited %q reaches the commands beside the run's own: %q", kv, env)
		}
	}
}

func TestAC10ElsewhereAVariableThatDiffersOnlyInCaseIsAnotherVariableAndIsKept(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("on Windows names that differ only in case are one variable")
	}
	rdTempDir(t)
	d := rdNew(t)
	other := strings.ToLower(RunDirVar) + "=/kept"
	if env := d.Env([]string{other}); !slices.Contains(env, other) {
		t.Errorf("Env took out %q, which is another variable here: %q", other, env)
	}
}

func TestAC17ANarrowRunsListHoldsOneNamePerLineAndIsHandedOnBesideTheRun(t *testing.T) {
	rdTempDir(t)
	d := rdNew(t)
	names := []string{"internal/a/a_test.go", "internal/b/b_test.go"}
	if err := d.ListTests(names); err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	list := filepath.Join(d.Path(), "test-files")
	got, err := os.ReadFile(list)
	if err != nil {
		t.Fatalf("the list is not at %q: %v", list, err)
	}
	if string(got) != "internal/a/a_test.go\ninternal/b/b_test.go\n" {
		t.Errorf("the list holds %q, want one name per line, each ended", got)
	}
	if fi, err := os.Stat(list); err == nil && runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Errorf("the list has mode %v, want 0600", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(d.Run()); len(entries) != 0 {
		t.Errorf("the list was written into the run directory, which must be empty: %v", entries)
	}
	env := d.Env([]string{TestFilesVar + "=/an/old/list"})
	want := TestFilesVar + "=" + filepath.ToSlash(list)
	if !slices.Contains(env, want) || slices.Contains(env, TestFilesVar+"=/an/old/list") {
		t.Errorf("the environment is %q, want %q in place of the inherited list", env, want)
	}
}

func TestAC17AnEmptyListIsAnEmptyFileAndStillHandedOn(t *testing.T) {
	rdTempDir(t)
	d := rdNew(t)
	if err := d.ListTests(nil); err != nil {
		t.Fatalf("ListTests: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(d.Path(), "test-files"))
	if err != nil || len(got) != 0 {
		t.Errorf("an empty list is %q (%v), want an empty file", got, err)
	}
	if env := d.Env(nil); !slices.Contains(env, TestFilesVar+"="+filepath.ToSlash(filepath.Join(d.Path(), "test-files"))) {
		t.Errorf("an empty list is not handed on: %q", env)
	}
}

func TestAC17AListThatCannotBeWrittenIsAnErrorAndIsNotHandedOn(t *testing.T) {
	rdTempDir(t)
	d := rdNew(t)
	// A directory where the list goes is what a write cannot replace.
	if err := os.Mkdir(filepath.Join(d.Path(), "test-files"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := d.ListTests([]string{"a_test.go"}); err == nil {
		t.Fatal("ListTests over a directory returned no error")
	}
	for _, kv := range d.Env(nil) {
		if strings.HasPrefix(kv, TestFilesVar+"=") {
			t.Errorf("a list that was not written is handed on: %q", kv)
		}
	}
}

func TestAC10ClosingARunRemovesItsDirectoryWithEverythingInItAndForgetsIt(t *testing.T) {
	rdTempDir(t)
	d, err := NewRunDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.ListTests([]string{"a_test.go"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d.Run(), "cover.out"), []byte("mode: set\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Lstat(d.Path()); !os.IsNotExist(err) {
		t.Errorf("the run's directory is still there after Close: %v", err)
	}
	if rdRegistered(d.Path()) {
		t.Errorf("a closed run is still registered")
	}
}

func TestAC10WhatARunRegisteredIsRemovedWhenItIsStoppedAndWhatWasKeptIsNot(t *testing.T) {
	tmp := rdTempDir(t)
	gone := filepath.Join(tmp, "gone")
	kept := filepath.Join(tmp, "kept.log")
	if err := os.MkdirAll(filepath.Join(gone, "inside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kept, []byte("what the commands printed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	Register(gone)
	Register(gone)
	Register(kept)
	removals.Lock()
	n := 0
	for _, p := range removals.paths {
		if p == gone {
			n++
		}
	}
	removals.Unlock()
	if n != 1 {
		t.Errorf("a path registered twice is held %d times, want once", n)
	}
	Keep(kept)
	if rdRegistered(kept) {
		t.Errorf("a kept path is still registered")
	}
	StopAll()
	if _, err := os.Lstat(gone); !os.IsNotExist(err) {
		t.Errorf("a registered directory survived StopAll: %v", err)
	}
	if got, err := os.ReadFile(kept); err != nil || string(got) != "what the commands printed\n" {
		t.Errorf("a kept file was removed or changed by StopAll: %q, %v", got, err)
	}
	if rdRegistered(gone) {
		t.Errorf("StopAll left %q registered", gone)
	}
}

func TestAC10RemovingAPathDeletesItAndForgetsIt(t *testing.T) {
	tmp := rdTempDir(t)
	p := filepath.Join(tmp, "out.tmp")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	Register(p)
	if err := Remove(p); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Errorf("Remove left %q: %v", p, err)
	}
	if rdRegistered(p) {
		t.Errorf("Remove left %q registered", p)
	}
	if err := Remove(p); err != nil {
		t.Errorf("removing a path a second time is %v, want nothing to do", err)
	}
}
