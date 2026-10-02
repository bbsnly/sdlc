package cli

// US-050 AC-8: this package's tests run in parallel. A process has one working
// directory and one environment, and Go refuses t.Chdir and t.Setenv in a
// parallel test for that reason; so each test keeps its own here, and every
// command it runs is handed them through ExecuteIn rather than reading the
// process's. One test at a time, with a real git repository or several each,
// the package took six minutes.
//
// A test that has to change something only the process has -- PATH, which git
// and the shell are found on, the variables os.UserCacheDir reads, or git's own
// variables, which reach every git sdlc runs -- changes it with t.Setenv as
// before, and is not parallel. Go runs those first, one at a time, before any
// parallel test starts, so nothing parallel ever sees what they set.

import (
	"bytes"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/bbsnly/sdlc/internal/store"
)

// sdlcVariables are the variables ExecuteIn reads through the getenv it is
// given, which are the only ones a test sets for itself rather than for the
// process: the session a command runs in, and where doctor looks for the
// binary the hooks run.
var sdlcVariables = []string{store.SessionEnv, "SDLC_BIN", "HOME", "LOCALAPPDATA"}

// invocation is where a test's commands run, and the variables it set for them.
type invocation struct {
	dir string
	env map[string]string
}

// getenv answers what the test set, and the process's own value for anything
// else, as a command run in a shell with those variables exported would see.
func (inv invocation) getenv(name string) string {
	if v, ok := inv.env[name]; ok {
		return v
	}
	return os.Getenv(name)
}

var (
	invocationsMu sync.Mutex
	// invocations is each test's invocation by the test's full name. A subtest
	// that sets nothing runs its commands where its parent does, as it would
	// after its parent's t.Chdir; one that sets something starts from a copy of
	// its parent's, which goes when the subtest ends.
	invocations = map[string]*invocation{}
)

// packageDir is the process's working directory, which no test changes: where
// a command a test runs before it chooses a directory runs, as it always has.
var packageDir = sync.OnceValue(func() string {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	return wd
})

// nearestInvocation is the invocation of name or of its nearest parent, with
// invocationsMu held.
func nearestInvocation(name string) invocation {
	for {
		if inv, ok := invocations[name]; ok {
			return invocation{dir: inv.dir, env: maps.Clone(inv.env)}
		}
		i := strings.LastIndex(name, "/")
		if i < 0 {
			return invocation{dir: packageDir()}
		}
		name = name[:i]
	}
}

// invocationOf is what t's commands run with.
func invocationOf(t testing.TB) invocation {
	invocationsMu.Lock()
	defer invocationsMu.Unlock()
	return nearestInvocation(t.Name())
}

// ownInvocation applies change to t's own invocation, making it first from its
// parent's.
func ownInvocation(t testing.TB, change func(*invocation)) {
	invocationsMu.Lock()
	defer invocationsMu.Unlock()
	name := t.Name()
	inv, ok := invocations[name]
	if !ok {
		parent := nearestInvocation(name)
		inv = &parent
		if inv.env == nil {
			inv.env = map[string]string{}
		}
		invocations[name] = inv
		t.Cleanup(func() {
			invocationsMu.Lock()
			defer invocationsMu.Unlock()
			delete(invocations, name)
		})
	}
	change(inv)
}

// chdir is t.Chdir for the commands t runs, and only for them: the process
// stays where it is.
func chdir(t testing.TB, dir string) {
	t.Helper()
	if !filepath.IsAbs(dir) {
		t.Fatalf("chdir(%q): a test's directory is absolute, so that nothing depends on the process's", dir)
	}
	ownInvocation(t, func(inv *invocation) { inv.dir = dir })
}

// setenv is t.Setenv for the commands t runs, and only for them. It takes only
// the variables ExecuteIn reads through getenv: anything else has to be set for
// the process, with t.Setenv, in a test that is not parallel.
func setenv(t testing.TB, name, value string) {
	t.Helper()
	if !slices.Contains(sdlcVariables, name) {
		t.Fatalf("setenv(%q): ExecuteIn does not read %s through getenv; set it with t.Setenv in a test that is not parallel", name, name)
	}
	ownInvocation(t, func(inv *invocation) { inv.env[name] = value })
}

// workdir is the directory t's commands run in: what os.Getwd was before
// ExecuteIn, and what a relative path in a test is relative to.
func workdir(t testing.TB) string {
	return invocationOf(t).dir
}

// execute runs one command as t's commands run.
func execute(t testing.TB, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	t.Helper()
	inv := invocationOf(t)
	code := ExecuteIn(inv.dir, inv.getenv, args, stdin, stdout, stderr)
	// ExecuteIn has the directory it was given, and a command that moved the
	// process would move every other test running at the same time.
	if wd, err := os.Getwd(); err != nil || wd != packageDir() {
		t.Errorf("after sdlc %s the process's working directory is %q (%v), not %s: ExecuteIn must leave it alone",
			strings.Join(args, " "), wd, err, packageDir())
	}
	return code
}

// runIn runs one command in dir with nothing the test set, which is what a
// template is built with: the test that happens to build it must not be left
// anywhere by doing so.
func runIn(t testing.TB, dir string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := ExecuteIn(dir, os.Getenv, args, strings.NewReader(""), &out, &errb)
	r := result{stdout: out.String(), stderr: errb.String(), code: code}
	if r.code != 0 {
		t.Fatalf("sdlc %s in %s exited %d:\n%s%s", strings.Join(args, " "), dir, r.code, r.stdout, r.stderr)
	}
	return r
}
