package testenv

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// US-087: a command that writes takes a lock under the user's cache
// directory, and the tests of every package that starts with Run used to take
// theirs in the developer's own. Run now gives the tests a home and cache
// directory inside the directory it makes, keeps the Go caches where they
// were, and fails the run when a test leaves anything in that cache directory.
//
// These tests call run, which is Run with the tests it starts handed in as a
// function, because Run's own m.Run cannot be replaced and the profile of
// coverage is written inside it: what Run does after the tests have run is
// out of the profile of a test that is only started by Run. Run is
// `return run(m.Run)`, and a nil m, as the test above passes, is still fine
// for as long as it fails before the tests are started.

// us087HomeVars are the variables the system reads to find the home and the
// cache directory of the user. Run points all of them into the directory it
// makes on every system, whichever one this is, so that Windows, macOS and
// Linux are the same story.
var us087HomeVars = []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LocalAppData"}

// us087GoVars are the Go settings that a changed home would lose: the build
// cache, the module cache, the workspace they sit in, and the file the user's
// own `go env -w` settings are in.
var us087GoVars = []string{"GOCACHE", "GOMODCACHE", "GOPATH", "GOENV"}

// us087Restored is every variable Run may change, so that a test which calls
// run leaves the rest of the binary's tests the environment they started with.
var us087Restored = slices.Concat(
	[]string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_NOSYSTEM", "GIT_CEILING_DIRECTORIES", "npm_config_cache"},
	us087HomeVars, us087GoVars)

// us087Rig is the environment a test calls run in: a temporary directory that
// is the only place run can make its own (so that what it leaves behind is
// seen), and a file in place of standard error (so that what it says is read).
type us087Rig struct {
	t    *testing.T
	tmp  string
	sink *os.File
}

// us087NewRig has to come before the test changes any of the variables it is
// about, because it records what they were to put them back.
func us087NewRig(t *testing.T) *us087Rig {
	t.Helper()
	tmp := t.TempDir()
	sink, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sink.Close() })
	for _, v := range us087Restored {
		t.Setenv(v, os.Getenv(v))
	}
	for _, v := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(v, tmp)
	}
	return &us087Rig{t: t, tmp: tmp, sink: sink}
}

// run is run(tests) with standard error read back.
func (r *us087Rig) run(tests func() int) (int, string) {
	r.t.Helper()
	code := func() int {
		keep := os.Stderr
		os.Stderr = r.sink
		defer func() { os.Stderr = keep }()
		return run(tests)
	}()
	raw, err := os.ReadFile(r.sink.Name())
	if err != nil {
		r.t.Fatal(err)
	}
	return code, string(raw)
}

// us087Unset leaves name unset for the rest of the test, and as it was after.
func us087Unset(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

func us087Names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// us087Resolve is filepath.EvalSymlinks for a path that need not exist yet: the
// part that does is resolved, so that /var and /private/var on macOS, or a
// short name on Windows, are one place.
func us087Resolve(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// us087Below is p relative to base, and whether it is strictly inside it.
func us087Below(base, p string) (string, bool) {
	rel, err := filepath.Rel(us087Resolve(base), us087Resolve(p))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return rel, false
	}
	return rel, true
}

func us087Put(t *testing.T, path string, dir bool) {
	t.Helper()
	if dir {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("left behind\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// us087MustBeMadeByTheTest stops the test at once unless path is, or is inside,
// one of the directories the test made. It fails and does not skip, so that a
// run which did not give the tests a home of their own is a failing test, and
// it comes before the first write, replace or removal at a path that a
// variable or os.UserCacheDir named: run against code that did not redirect,
// the test that cleared os.UserCacheDir() emptied most of a developer's real
// cache directory. Both sides are resolved, since a temporary directory on
// macOS is behind a symbolic link.
func us087MustBeMadeByTheTest(t *testing.T, what, path string, made ...string) {
	t.Helper()
	if path != "" {
		for _, dir := range made {
			if dir == "" {
				continue
			}
			rel, err := filepath.Rel(us087Resolve(dir), us087Resolve(path))
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return
			}
		}
	}
	t.Fatalf("%s is %q, which is not inside a directory this test made (%q): the run did not give the tests a home and cache directory of their own, "+
		"and this test will not write, replace or remove anything there", what, path, made)
}

// ------------------------------------------------------------------- AC-1, AC-3

// AC-3: whichever system this is, the home and the cache directory it reads
// are inside one directory that the run made, for every variable that system
// might read -- this test runs on Windows, macOS and Linux, and on each of
// them os.UserCacheDir and os.UserHomeDir are asked as well as the variables,
// since those are what the code under test reads. What is made is removed
// once the run is over.
func TestAC3TheHomeAndCacheVariablesPointIntoOneDirectoryTheRunMade(t *testing.T) {
	rig := us087NewRig(t)
	seen := map[string]string{}
	var made []string
	code, stderr := rig.run(func() int {
		for _, v := range us087HomeVars {
			seen[v] = os.Getenv(v)
		}
		if p, err := os.UserCacheDir(); err != nil {
			t.Errorf("os.UserCacheDir: %v", err)
		} else {
			seen["os.UserCacheDir()"] = p
		}
		if p, err := os.UserHomeDir(); err != nil {
			t.Errorf("os.UserHomeDir: %v", err)
		} else {
			seen["os.UserHomeDir()"] = p
		}
		made = us087Names(t, rig.tmp)
		return 0
	})
	if code != 0 {
		t.Fatalf("Run = %d for tests that passed and left nothing:\n%s", code, stderr)
	}

	top := ""
	for name, p := range seen {
		if p == "" {
			t.Errorf("%s is not set while the tests run", name)
			continue
		}
		rel, ok := us087Below(rig.tmp, p)
		if !ok {
			t.Errorf("%s = %s, which is not inside a directory the run made under %s", name, p, rig.tmp)
			continue
		}
		first, _, _ := strings.Cut(rel, string(filepath.Separator))
		if top == "" {
			top = first
		}
		if first != top {
			t.Errorf("%s = %s is in %s, where the others are in %s: the run made one directory for them", name, p, first, top)
		}
		if !slices.Contains(made, first) {
			t.Errorf("%s = %s is in %s, which is not a directory the run made (it made %v)", name, p, first, made)
		}
	}
	if left := us087Names(t, rig.tmp); len(left) != 0 {
		t.Errorf("the run left %v in its temporary directory", left)
	}
}

// AC-1: the machine's home and cache directory are an empty directory, as for
// a developer whose home must stay as it is, and a command takes and gives
// back its lock under the cache directory it reads. Nothing is created in the
// empty one.
func TestAC1NothingIsCreatedUnderTheDirectoryTheMachineHadAsHomeAndCache(t *testing.T) {
	rig := us087NewRig(t)
	empty := t.TempDir()
	for _, v := range us087HomeVars {
		t.Setenv(v, empty)
	}

	code, stderr := rig.run(func() int {
		cache, err := os.UserCacheDir()
		if err != nil {
			t.Errorf("os.UserCacheDir: %v", err)
			return 0
		}
		us087MustBeMadeByTheTest(t, "os.UserCacheDir()", cache, rig.tmp, empty)
		lock := filepath.Join(cache, "sdlc", "locks", "a-project")
		us087Put(t, filepath.Join(lock, "owner.json"), false)
		if err := os.RemoveAll(lock); err != nil {
			t.Error(err)
		}
		return 0
	})

	if got := us087Names(t, empty); len(got) != 0 {
		t.Errorf("the run created %v under the directory the machine had as home and cache", got)
	}
	if code != 0 {
		t.Errorf("Run = %d for a lock taken and given back, which leaves only the empty directories it was taken in:\n%s", code, stderr)
	}
}

// ----------------------------------------------------------------------- AC-3

// us087Go is the go the tests are run with, found before any test changes PATH.
func us087Go(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go is not on PATH, and this package's tests are run by it: %v", err)
	}
	return path
}

// us087GoEnv is what go says name is, in the environment as it is now.
func us087GoEnv(t *testing.T, goBin, name string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), goBin, "env", name).Output()
	if err != nil {
		t.Fatalf("go env %s: %v", name, err)
	}
	return strings.TrimSpace(string(out))
}

// AC-3: a Go variable that is set is kept as it is, and go is not asked for it:
// go is not on the path here, so asking would fail the run.
func TestAC3AGoVariableThatIsSetKeepsItsValueAndGoIsNotAsked(t *testing.T) {
	rig := us087NewRig(t)
	want := map[string]string{}
	for _, v := range us087GoVars {
		want[v] = filepath.Join(t.TempDir(), "kept-"+v)
		t.Setenv(v, want[v])
	}
	t.Setenv("PATH", t.TempDir())

	got := map[string]string{}
	started := false
	code, stderr := rig.run(func() int {
		started = true
		for _, v := range us087GoVars {
			got[v] = os.Getenv(v)
		}
		return 0
	})
	if code != 0 || !started {
		t.Fatalf("Run = %d, tests started = %v, with every Go variable set and no go on the path:\n%s", code, started, stderr)
	}
	for _, v := range us087GoVars {
		if got[v] != want[v] {
			t.Errorf("%s = %q while the tests run, want %q as it was", v, got[v], want[v])
		}
	}
}

// AC-3: a Go variable that is not set is taken from go itself, once, as go
// answers it before the home is changed -- so the tests that build the binary
// find the build cache and the module cache of the developer, and not an
// empty one in the directory the run made. go is not run from inside the
// tests here: it writes its telemetry in the home it is given, from a process
// that outlives it, and that raced with the removal of the directory the run
// made.
//
// The go that is asked here is a script that writes down its arguments and
// then runs the real one, so that "once" is counted without saying what the
// answer is read from. A script cannot be put on PATH as go on Windows the same
// way, and that leg checks the answers and not the count; it is reasoned about
// and not run here.
func TestAC3AnUnsetGoVariableIsTakenFromGoEnvOnceAsItWasBeforeTheRedirection(t *testing.T) {
	goBin := us087Go(t)
	cases := [][]string{{"GOCACHE"}, {"GOMODCACHE"}, {"GOPATH"}, {"GOENV"}, us087GoVars}
	for _, unset := range cases {
		t.Run("AC3 "+strings.Join(unset, " and ")+" unset", func(t *testing.T) {
			rig := us087NewRig(t)
			kept := map[string]string{}
			for _, v := range us087GoVars {
				kept[v] = filepath.Join(t.TempDir(), "kept-"+v)
				t.Setenv(v, kept[v])
			}
			for _, v := range unset {
				us087Unset(t, v)
			}

			// What go says in the environment the run starts from, with the
			// machine's own home still in place.
			want := map[string]string{}
			for _, v := range us087GoVars {
				if slices.Contains(unset, v) {
					want[v] = us087GoEnv(t, goBin, v)
					if want[v] == "" {
						t.Fatalf("go env %s is empty, so there is nothing to keep", v)
					}
				} else {
					want[v] = kept[v]
				}
			}

			calls := ""
			if runtime.GOOS != "windows" {
				bin := t.TempDir()
				calls = filepath.Join(bin, "calls")
				script := "#!/bin/sh\necho \"$*\" >> \"$SDLCUS087_CALLS\"\nexec \"$SDLCUS087_GO\" \"$@\"\n"
				if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
				t.Setenv("SDLCUS087_CALLS", calls)
				t.Setenv("SDLCUS087_GO", goBin)
				t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			}

			got := map[string]string{}
			code, stderr := rig.run(func() int {
				for _, v := range us087GoVars {
					got[v] = os.Getenv(v)
				}
				return 0
			})
			if code != 0 {
				t.Fatalf("Run = %d:\n%s", code, stderr)
			}
			for _, v := range us087GoVars {
				if got[v] != want[v] {
					t.Errorf("%s = %q while the tests run, want %q as go answers it before the redirection", v, got[v], want[v])
				}
			}

			if calls == "" {
				return
			}
			raw, err := os.ReadFile(calls)
			if err != nil {
				t.Fatalf("go was never run, so %s was not taken from it: %v", strings.Join(unset, " and "), err)
			}
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) != 1 || !strings.HasPrefix(lines[0], "env") {
				t.Errorf("go was run as %q, want one `go env` and nothing else", lines)
			}
		})
	}
}

// AC-3: a go that cannot say stops the run before any test is started, as
// every other setup failure does, instead of running the tests with the Go
// caches lost.
func TestAC3RunStopsWithoutStartingTheTestsWhenGoEnvCannotBeAsked(t *testing.T) {
	for _, name := range []string{"there is no go on the path", "go exits with an error"} {
		t.Run("AC3 "+name, func(t *testing.T) {
			rig := us087NewRig(t)
			us087Unset(t, "GOENV")
			bin := t.TempDir()
			if name == "go exits with an error" {
				if runtime.GOOS == "windows" {
					t.Skip("the go that fails is a shell script, which cannot be put on PATH as go here; the missing go is the Windows case")
				}
				if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("PATH", bin)

			started := false
			code, stderr := rig.run(func() int {
				started = true
				return 0
			})
			if code != 1 {
				t.Errorf("Run = %d, want 1 as for every other failure to prepare the environment", code)
			}
			if started {
				t.Error("the tests were started without the Go caches that go would have said")
			}
			if strings.TrimSpace(stderr) == "" {
				t.Error("Run failed without saying why")
			}
			if left := us087Names(t, rig.tmp); len(left) != 0 {
				t.Errorf("the failed run left %v in its temporary directory", left)
			}
		})
	}
}

// AC-3: npm's cache is kept where it was, as the Go caches are, and the tests
// that install from it offline still find it. A test of an earlier story runs
// `npm ci` with npm_config_offline=true, and npm's cache is under the home
// unless this says otherwise: with the home changed it found an empty one and
// failed with ENOTCACHED. A value that is set is kept as it is. PATH is an
// empty directory, so that asking npm for it, which is a subprocess, would
// fail the run.
func TestAC3AnNpmCacheThatIsSetKeepsItsValue(t *testing.T) {
	rig := us087NewRig(t)
	want := filepath.Join(t.TempDir(), "kept-npm-cache")
	t.Setenv("npm_config_cache", want)
	t.Setenv("PATH", t.TempDir())

	got := ""
	code, stderr := rig.run(func() int {
		got = os.Getenv("npm_config_cache")
		return 0
	})
	if code != 0 {
		t.Fatalf("Run = %d with npm_config_cache set and no npm on the path:\n%s", code, stderr)
	}
	if got != want {
		t.Errorf("npm_config_cache = %q while the tests run, want %q as it was", got, want)
	}
}

// AC-3: an npm cache that is not set is the one npm would have used under the
// home the machine had before the redirection -- .npm in the home, or on
// Windows npm-cache in LocalAppData -- and not one in the directory the run
// made, which is empty and removed afterwards. Run only says where it is: the
// machine's home is not written to.
func TestAC3AnUnsetNpmCacheIsTheOneUnderTheHomeTheMachineHadBeforeTheRedirection(t *testing.T) {
	rig := us087NewRig(t)
	machine := t.TempDir()
	home, local := filepath.Join(machine, "home"), filepath.Join(machine, "local")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LocalAppData", local)
	us087Unset(t, "npm_config_cache")
	t.Setenv("PATH", t.TempDir())

	want := filepath.Join(home, ".npm")
	if runtime.GOOS == "windows" {
		want = filepath.Join(local, "npm-cache")
	}
	got := ""
	code, stderr := rig.run(func() int {
		got = os.Getenv("npm_config_cache")
		return 0
	})
	if code != 0 {
		t.Fatalf("Run = %d with npm_config_cache unset and no npm on the path:\n%s", code, stderr)
	}
	if got != want {
		t.Errorf("npm_config_cache = %q while the tests run, want %q under the home the machine had", got, want)
	}
	if _, inside := us087Below(rig.tmp, got); inside {
		t.Errorf("npm_config_cache = %q is inside the directory the run made, which is empty and removed with the run", got)
	}
	if left := us087Names(t, machine); len(left) != 0 {
		t.Errorf("the run made %v in the home the machine had", left)
	}
}

// ----------------------------------------------------------------------- AC-2

// AC-2: what a test leaves in the cache directory the run made fails the run,
// and is named. The empty directories a lock is taken in, and nothing else, are
// what a clean run leaves. A failing run stays failing whether or not there is
// a leftover, and is not reported clean because there is none.
func TestAC2WhatATestLeavesInTheCacheDirectoryFailsTheRunAndIsNamed(t *testing.T) {
	type leftover struct {
		name  string
		code  int                              // what the tests return
		leave func(t *testing.T, cache string) // what they leave in the cache directory
		fail  bool                             // whether the run fails
		named []string                         // what it names
	}
	cases := []leftover{
		{name: "a file in the cache directory", fail: true, named: []string{"us087-stray-file"},
			leave: func(t *testing.T, c string) { t.Helper(); us087Put(t, filepath.Join(c, "us087-stray-file"), false) }},
		{name: "a file in sdlc", fail: true, named: []string{"us087-stray-in-sdlc"},
			leave: func(t *testing.T, c string) {
				t.Helper()
				us087Put(t, filepath.Join(c, "sdlc", "us087-stray-in-sdlc"), false)
			}},
		{name: "a file in sdlc/locks", fail: true, named: []string{"us087-stray-in-locks"},
			leave: func(t *testing.T, c string) {
				t.Helper()
				us087Put(t, filepath.Join(c, "sdlc", "locks", "us087-stray-in-locks"), false)
			}},
		{name: "an empty directory beside sdlc", fail: true, named: []string{"us087-stray-dir"},
			leave: func(t *testing.T, c string) { t.Helper(); us087Put(t, filepath.Join(c, "us087-stray-dir"), true) }},
		{name: "an empty directory in sdlc", fail: true, named: []string{"us087-other-dir"},
			leave: func(t *testing.T, c string) {
				t.Helper()
				us087Put(t, filepath.Join(c, "sdlc", "us087-other-dir"), true)
			}},
		{name: "a lock left held", fail: true, named: []string{"us087-held-lock"},
			leave: func(t *testing.T, c string) {
				t.Helper()
				us087Put(t, filepath.Join(c, "sdlc", "locks", "us087-held-lock", "owner.json"), false)
			}},
		{name: "a lock left held with nothing in it", fail: true, named: []string{"us087-bare-lock"},
			leave: func(t *testing.T, c string) {
				t.Helper()
				us087Put(t, filepath.Join(c, "sdlc", "locks", "us087-bare-lock"), true)
			}},
		{name: "several leftovers, each of them", fail: true,
			named: []string{"us087-first", "us087-second", "us087-third"},
			leave: func(t *testing.T, c string) {
				t.Helper()
				us087Put(t, filepath.Join(c, "us087-first"), false)
				us087Put(t, filepath.Join(c, "sdlc", "us087-second"), false)
				us087Put(t, filepath.Join(c, "sdlc", "locks", "us087-third"), true)
			}},
		{name: "a file where the cache directory should be", fail: true,
			leave: func(t *testing.T, c string) {
				t.Helper()
				if err := os.RemoveAll(c); err != nil {
					t.Fatal(err)
				}
				us087Put(t, c, false)
			}},
		{name: "a leftover in a run whose tests failed", code: 1, fail: true, named: []string{"us087-stray-after-failure"},
			leave: func(t *testing.T, c string) {
				t.Helper()
				us087Put(t, filepath.Join(c, "us087-stray-after-failure"), false)
			}},
		{name: "nothing at all", leave: func(*testing.T, string) {}},
		{name: "the empty directories a lock is taken in", leave: func(t *testing.T, c string) {
			t.Helper()
			us087Put(t, filepath.Join(c, "sdlc", "locks"), true)
		}},
		{name: "an empty sdlc", leave: func(t *testing.T, c string) { t.Helper(); us087Put(t, filepath.Join(c, "sdlc"), true) }},
		{name: "a lock taken and given back", leave: func(t *testing.T, c string) {
			t.Helper()
			us087Put(t, filepath.Join(c, "sdlc", "locks", "a-project", "owner.json"), false)
			if err := os.RemoveAll(filepath.Join(c, "sdlc", "locks", "a-project")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "a run whose tests failed and left nothing", code: 1, fail: true, leave: func(*testing.T, string) {}},
	}

	for _, c := range cases {
		t.Run("AC2 "+c.name, func(t *testing.T) {
			rig := us087NewRig(t)
			code, stderr := rig.run(func() int {
				cache, err := os.UserCacheDir()
				if err != nil {
					t.Errorf("os.UserCacheDir: %v", err)
					return c.code
				}
				us087MustBeMadeByTheTest(t, "os.UserCacheDir()", cache, rig.tmp)
				c.leave(t, cache)
				return c.code
			})
			if c.fail && code == 0 {
				t.Error("the run passed")
			}
			if !c.fail && code != 0 {
				t.Errorf("Run = %d, want 0:\n%s", code, stderr)
			}
			for _, name := range c.named {
				if !strings.Contains(stderr, name) {
					t.Errorf("the run does not name %s:\n%s", name, stderr)
				}
			}
		})
	}
}

// us087Leave is what the test run that TestAC2 starts again leaves behind.
func us087Leave(t *testing.T, what string) {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	us087MustBeMadeByTheTest(t, "os.UserCacheDir()", cache, os.Getenv("SDLCUS087_MADE"))
	switch what {
	case "file":
		us087Put(t, filepath.Join(cache, "us087-left-by-the-child"), false)
	case "skeleton":
		us087Put(t, filepath.Join(cache, "sdlc", "locks"), true)
	default:
		t.Fatalf("SDLCUS087_LEAVE=%q is not one of file, skeleton", what)
	}
}

// us087Child runs the test called name in a test binary of its own, started
// the way go test starts it, whose TestMain is the Run under test.
func us087Child(t *testing.T, name, leave string) (string, error) {
	t.Helper()
	scratch := t.TempDir()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+name+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(),
		"SDLCUS087_LEAVE="+leave,
		"SDLCUS087_MADE="+scratch,
		"TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// AC-2, through TestMain and the real m.Run: a test that passes and leaves a
// file in the cache directory ends the binary with a non-zero exit status, and
// the output names the file.
func TestAC2ATestBinaryWhoseTestLeftAFileExitsNonZeroAndNamesIt(t *testing.T) {
	const name = "TestAC2ATestBinaryWhoseTestLeftAFileExitsNonZeroAndNamesIt"
	if what := os.Getenv("SDLCUS087_LEAVE"); what != "" {
		us087Leave(t, what)
		return
	}

	out, err := us087Child(t, name, "file")
	if _, ok := errors.AsType[*exec.ExitError](err); !ok {
		t.Errorf("the test binary exited cleanly (%v) with a file left in the cache directory:\n%s", err, out)
	}
	if !strings.Contains(out, "--- PASS: "+name) {
		t.Errorf("the test the binary ran did not pass, so a failure is not the leftover's:\n%s", out)
	}
	if !strings.Contains(out, "us087-left-by-the-child") {
		t.Errorf("the output does not name the file the test left:\n%s", out)
	}
}

// AC-2, through TestMain and the real m.Run: the empty directories a lock is
// taken in are not a leftover, or every package that takes a lock would fail.
func TestAC2ATestBinaryWhoseTestLeftOnlyTheEmptyLockDirectoriesExitsZero(t *testing.T) {
	const name = "TestAC2ATestBinaryWhoseTestLeftOnlyTheEmptyLockDirectoriesExitsZero"
	if what := os.Getenv("SDLCUS087_LEAVE"); what != "" {
		us087Leave(t, what)
		return
	}

	out, err := us087Child(t, name, "skeleton")
	if err != nil || !strings.Contains(out, "--- PASS: "+name) {
		t.Errorf("the test binary failed (%v) with only the empty lock directories left:\n%s", err, out)
	}
}
