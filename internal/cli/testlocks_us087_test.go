package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// US-087: every command that writes takes a lock under the user's cache
// directory, and the tests of a package that starts with testenv.Run used to
// take theirs in the developer's own, beside the locks of a story being worked
// on the same machine. The two tests here hold the two ends of that: the lock
// of a test run is made somewhere of the run's own, and the lock of a command
// outside the tests is made where it always was.

// us087CacheIsTheTestsOwn stops the test at once, and does not skip it, unless
// the cache directory the system reads is inside one of the directories the
// test that started this one made: a command writes its lock there, and a run
// that gave the tests no cache directory of their own would have it write
// into the developer's. Both sides are resolved, since a temporary directory on
// macOS is behind a symbolic link.
func us087CacheIsTheTestsOwn(t *testing.T, made []string) {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(p string) string {
		rest := ""
		for p = filepath.Clean(p); ; p = filepath.Dir(p) {
			if r, err := filepath.EvalSymlinks(p); err == nil {
				return filepath.Join(r, rest)
			}
			if filepath.Dir(p) == p {
				return filepath.Join(p, rest)
			}
			rest = filepath.Join(filepath.Base(p), rest)
		}
	}
	for _, dir := range made {
		if dir == "" {
			continue
		}
		rel, err := filepath.Rel(resolve(dir), resolve(cache))
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return
		}
	}
	t.Fatalf("the cache directory is %q, which is not inside a directory the test made (%q): the run did not give the tests a cache directory of their own, "+
		"and this test will not take a lock there", cache, made)
}

// AC-1: the tests of this package are run with the home and cache directories
// of the machine pointing at an empty directory, and nothing named sdlc is
// created under it. The run is the real one -- this test binary again, whose
// TestMain starts with testenv.Run -- and the command in it is a real one that
// writes loop state, so it takes the lock the way every writing command does.
func TestAC1ARunOfThisPackagesTestsMakesNothingNamedSdlcUnderTheDirectoryItWasStartedWithAsHome(t *testing.T) {
	const name = "TestAC1ARunOfThisPackagesTestsMakesNothingNamedSdlcUnderTheDirectoryItWasStartedWithAsHome"
	if os.Getenv("SDLCUS087_CHILD") == "1" {
		us087CacheIsTheTestsOwn(t, filepath.SplitList(os.Getenv("SDLCUS087_MADE")))
		gitProjectInitialised(t)
		if r := run(t, "ack", "--through", "HEAD"); r.code != 0 {
			t.Fatalf("a command that writes failed with %d, so it may not have taken its lock:\n%s", r.code, r.stderr)
		}
		return
	}

	empty := t.TempDir()
	scratch := t.TempDir()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+name+"$", "-test.count=1", "-test.v")
	cmd.Env = append(os.Environ(),
		"SDLCUS087_CHILD=1",
		"SDLCUS087_MADE="+empty+string(os.PathListSeparator)+scratch,
		// Every spelling of home and cache directory, so that the one this system
		// reads is among them whatever the system is.
		"HOME="+empty,
		"USERPROFILE="+empty,
		"XDG_CACHE_HOME="+empty,
		"LocalAppData="+empty,
		// So that whatever the run itself makes lands where this test can see it.
		"TMPDIR="+scratch,
		"TMP="+scratch,
		"TEMP="+scratch,
	)
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS: "+name) {
		t.Fatalf("the run of the package's tests with the home directory at an empty one (%v):\n%s", err, out)
	}

	var found []string
	walk := filepath.WalkDir(empty, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == "sdlc" {
			found = append(found, p)
		}
		return nil
	})
	if walk != nil {
		t.Fatal(walk)
	}
	if len(found) != 0 {
		t.Errorf("the tests made %v under the home and cache directory they were started with: a command's lock went into the developer's directory", found)
	}
}

// AC-4: outside the tests a command's lock is where it has always been: a
// directory named for the project, in sdlc/locks under the system's cache
// directory. A lock left there by a process that died long ago is the way to
// see it without waiting for a live one: the next writing command breaks it
// open and takes its place, so it is gone afterwards if, and only if, that is
// where the command took its own. The path is spelt out here and not asked of
// the code, so that a change to where it is made fails this test.
func TestAC4ACommandTakesItsLockWhereTheLockOfAProjectHasAlwaysBeen(t *testing.T) {
	root := gitProjectInitialised(t)

	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	local := filepath.Join(home, "local")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CACHE_HOME", xdg)
	t.Setenv("LocalAppData", local)

	var cache string
	switch runtime.GOOS {
	case "windows":
		cache = local
	case "darwin":
		cache = filepath.Join(home, "Library", "Caches")
	default:
		cache = xdg
	}

	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(resolved))
	lock := filepath.Join(cache, "sdlc", "locks", hex.EncodeToString(sum[:16]))
	if err := os.MkdirAll(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}

	if r := run(t, "ack", "--through", "HEAD"); r.code != 0 {
		t.Fatalf("a command that writes failed with %d:\n%s", r.code, r.stderr)
	}

	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the abandoned lock at %s is still there (%v): the command took its lock somewhere else", lock, err)
	}
	if _, err := os.Stat(filepath.Join(cache, "sdlc", "locks")); err != nil {
		t.Errorf("the command made no sdlc/locks directory under the cache directory %s: %v", cache, err)
	}
}
