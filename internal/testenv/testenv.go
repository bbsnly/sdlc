// Package testenv runs a package's tests in the environment they were written
// for, on whatever machine runs them.
//
// CI's runners start clean; a developer's machine does not. A global git
// configuration that signs every commit failed 118 tests on "gpg failed to
// sign", a temp directory inside a repository let git find that repository
// from a test that is outside any, and SDLC_ENFORCE=0 or SDLC_DEBUG=1 left
// exported in the shell turned enforcement off under the end-to-end tests or
// put debug lines into output the installer tests compare. Run from a git
// hook, the tests inherited the variables git exports to it: GIT_INDEX_FILE
// sent every test repository to the index of the commit being made. And every
// command that writes takes a lock under the user's cache directory, so the
// tests took theirs beside the locks of a story being worked on the same
// machine.
package testenv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// quietGit is the global configuration the tests' git reads. Git 2.55
// maintains a repository in the background after a commit, and its
// objects/maintenance.lock came and went under captureTree while it copied a
// template repository: internal/cli failed one run in 40. A test repository
// never needs collecting, so neither maintenance nor gc runs.
const quietGit = "[maintenance]\n\tauto = false\n[gc]\n\tauto = 0\n"

// goVars are the Go settings that a changed home would lose: the build cache,
// the module cache, the workspace they sit in, and the file holding the user's
// own `go env -w` settings. Each defaults to a place under the home, so a run
// with a home of its own would build every test binary from nothing and,
// offline, not at all.
var goVars = []string{"GOCACHE", "GOMODCACHE", "GOPATH", "GOENV"}

// errPrefix names this package on the line it writes to stderr when it fails.
const errPrefix = "testenv:"

// goEnvTimeout bounds the one go that Run asks. The tests' own timeout starts
// inside m.Run, so without it a go that never answers would hold the run.
const goEnvTimeout = time.Minute

// Run runs m and returns its exit code, for TestMain. While it runs, git reads
// no configuration but a repository's own and the two settings that turn its
// automatic maintenance off, and stops looking for a repository at the temp
// directory; no SDLC_ or CLAUDE_ variable is set, nor any GIT_ variable but
// the three that say so. The home and the cache directory are inside a
// directory the run made, and the Go caches stay where they were. Anything
// the tests leave in that cache directory, but the empty directories a lock is
// taken in, fails the run and is named.
func Run(m *testing.M) int {
	return run(m.Run)
}

// run is Run with the tests handed in. go test writes its coverage profile
// inside m.Run, so what Run does after the tests have run is out of the
// profile of a test that is only started by Run.
func run(tests func() int) int {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "SDLC_") || strings.HasPrefix(name, "CLAUDE_") || strings.HasPrefix(name, "GIT_") {
			_ = os.Unsetenv(name)
		}
	}

	dir, err := os.MkdirTemp("", "sdlc-testenv-")
	if err != nil {
		fmt.Fprintln(os.Stderr, errPrefix, err)
		return 1
	}
	// A directory that cannot be removed is said, and does not change the exit
	// code: a program that outlived the tests and wrote into it (go's own
	// telemetry did) left a sdlc-testenv-* in the temp directory with nothing
	// to say so.
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(os.Stderr, "testenv: could not remove", dir+":", err)
		}
	}()
	cache, err := prepare(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, errPrefix, err)
		return 1
	}

	code := tests()
	if left := leftovers(cache); len(left) > 0 {
		fmt.Fprintf(os.Stderr, "testenv: the tests left these in the cache directory the run made for them, %s:\n  %s\n"+
			"A test must put back what it takes there. A command it started and did not wait for still holds its lock, "+
			"which is a directory in sdlc/locks; a test that runs a command in a directory of its own should set "+
			"XDG_CACHE_HOME, HOME and LocalAppData to t.TempDir() first.\n"+
			"The paths are as they were when the tests ended, and the directory is removed with the run. "+
			"To find the test, run the package again with `go test -run <Name> -count=1 -v`, narrowing <Name> "+
			"until the run stops failing.\n",
			cache, strings.Join(left, "\n  "))
		if code == 0 {
			code = 1
		}
	}
	return code
}

// prepare gives the tests their environment, inside dir, and returns the cache
// directory it made for them.
func prepare(dir string) (string, error) {
	// First, while the home is still the developer's: go says where the caches
	// are by reading it, and so does npm's.
	caches, err := goCaches(dir)
	if err != nil {
		return "", err
	}
	npm := npmCache()
	global, home, cache := filepath.Join(dir, "gitconfig"), filepath.Join(dir, "home"), filepath.Join(dir, "cache")
	env := map[string]string{
		"GIT_CONFIG_GLOBAL":       global,
		"GIT_CONFIG_NOSYSTEM":     "1",
		"GIT_CEILING_DIRECTORIES": os.TempDir(),
		// Every spelling of the home and the cache directory, so that the one
		// this system reads is among them whatever the system is. The cache
		// directory is its own, so that what a program writes into the home
		// is not mistaken for what a test left in it.
		"HOME":           home,
		"USERPROFILE":    home,
		"XDG_CACHE_HOME": cache,
		"LocalAppData":   cache,
	}
	maps.Copy(env, caches)
	if npm != "" {
		env["npm_config_cache"] = npm
	}
	// The errors are joined and returned once: each is the same decision, to
	// stop before the tests start.
	err = errors.Join(os.MkdirAll(home, 0o700), os.WriteFile(global, []byte(quietGit), 0o600))
	for name, value := range env {
		err = errors.Join(err, os.Setenv(name, value))
	}
	// After the variables, which are what the system reads to find it: the
	// directory a lock is taken in is the one guardPath will read.
	userCache, cacheErr := os.UserCacheDir()
	return userCache, errors.Join(err, cacheErr, goTelemetryOff(home))
}

// goCaches is the value of each of goVars that the environment does not have,
// as go says it for the developer's own home. It asks once, and only when
// something is missing. A go that cannot say stops the run: the tests that
// build the binary would otherwise find an empty cache, or none.
func goCaches(dir string) (map[string]string, error) {
	var unset []string
	for _, name := range goVars {
		if os.Getenv(name) == "" {
			unset = append(unset, name)
		}
	}
	if len(unset) == 0 {
		return map[string]string{}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), goEnvTimeout)
	defer cancel()
	// In dir, which is made for the run and has no go.mod above it, so that go
	// has no module to read a toolchain requirement from.
	cmd := exec.CommandContext(ctx, "go", append([]string{"env", "-json"}, unset...)...)
	cmd.Dir = dir
	out, err := cmd.Output()
	var got map[string]string
	if err == nil {
		err = json.Unmarshal(out, &got)
	}
	if err != nil {
		reason := err.Error()
		if ctx.Err() != nil {
			reason = "go did not answer in " + goEnvTimeout.String()
		}
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			reason = strings.TrimRight(reason+": "+strings.TrimSpace(string(exit.Stderr)), ": ")
		}
		names := strings.Join(unset, ", ")
		return nil, fmt.Errorf("cannot ask go for %s (%s). Put go on PATH, or set %s before running the tests", names, reason, names)
	}
	return got, nil
}

// npmCache is the cache npm would have used under the home the machine had,
// for Run to say where it is once the home has changed: the test of an earlier
// story runs `npm ci` offline, and with the home changed npm found an empty
// cache and failed with ENOTCACHED. It is "" when there is nothing to say: the
// environment already names a cache (npm reads npm_config_* in either case,
// and exporting the lower-case name beside a user's upper-case one would leave
// npm to pick one of two), or the home cannot be found, which tests have
// always been run without (a container) and which stops no run. It only names
// the directory; npm is not asked, and nothing is made in the home.
func npmCache() string {
	if os.Getenv("npm_config_cache") != "" || os.Getenv("NPM_CONFIG_CACHE") != "" {
		return ""
	}
	base, err := os.UserHomeDir()
	leaf := ".npm"
	if runtime.GOOS == "windows" {
		base, err = os.UserCacheDir()
		leaf = "npm-cache"
	}
	var path string
	if err == nil {
		path = filepath.Join(base, leaf)
	}
	return path
}

// goTelemetryOff stops go writing its telemetry into the home the run made. It
// does so from a process that can outlive the command that started it, and it
// raced with the removal of the directory: a sdlc-testenv-* was left in the
// temp directory, one run in a few. Go has no environment variable for it; its
// mode file is in go's configuration directory, which is under the home where
// the system derives it from HOME (macOS, and Linux without XDG_CONFIG_HOME).
// Where it is not (Windows has %AppData%), go writes outside the run's
// directory, there is no race to stop, and nothing is written.
func goTelemetryOff(home string) error {
	cfg, _ := os.UserConfigDir()
	var err error
	if strings.HasPrefix(cfg, home+string(filepath.Separator)) {
		mode := filepath.Join(cfg, "go", "telemetry", "mode")
		err = errors.Join(os.MkdirAll(filepath.Dir(mode), 0o700), os.WriteFile(mode, []byte("off\n"), 0o600))
	}
	return err
}

// leftovers is what the tests left in cache: every file, and every directory
// but the cache directory itself and the empty sdlc/locks that a lock is taken
// in. A lock still held is a directory in it. What cannot be read is named
// too, since it cannot be shown to be empty. It reports and deletes nothing.
func leftovers(cache string) []string {
	return leftoversOn(runtime.GOOS, cache)
}

// leftoversOn is leftovers for the system named, so that what Windows is
// allowed can be tested on a machine that is not one.
func leftoversOn(goos, cache string) []string {
	sdlc := filepath.Join(cache, "sdlc")
	locks := filepath.Join(sdlc, "locks")
	var left []string
	_ = filepath.WalkDir(cache, func(path string, d fs.DirEntry, err error) error {
		// A cache directory nobody made anything in is clean.
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err == nil && d.IsDir() && (path == cache || path == sdlc || path == locks) {
			return nil
		}
		if err == nil && d.IsDir() && systemOwned(goos, cache, path) {
			return fs.SkipDir
		}
		left = append(left, path)
		if d != nil && d.IsDir() {
			return fs.SkipDir
		}
		return nil
	})
	return left
}

// systemOwned reports whether path is a directory Windows' own programs keep
// under LocalAppData, which no test put there. The installer tests passed on
// windows-11-arm and the run still failed on a "Microsoft" directory in the
// cache: a program the tests start writes its own cache there, and nothing a
// test does takes it back. sdlc keeps what it writes under "sdlc", so the name
// is never its own. A file of that name, and the name anywhere deeper, are
// still leftovers.
func systemOwned(goos, cache, path string) bool {
	return goos == "windows" && filepath.Dir(path) == cache && strings.EqualFold(filepath.Base(path), "Microsoft")
}
