package shellx

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// The variables a run of the configured commands is handed. The binary names
// the paths and knows nothing of what a tool puts in them.
const (
	RunDirVar    = "SDLC_RUN_DIR"
	TestFilesVar = "SDLC_TEST_FILES"
)

// RunDir is the directory of one run of the configured commands, made new in
// the system's temporary directory and never under the project. Its run/ is
// handed on as SDLC_RUN_DIR, empty when the first command begins, so that a
// coverage command reads the profile its own run's test command wrote and no
// earlier one; a narrow run's list of test files sits beside it.
type RunDir struct {
	dir, run, list string
}

// NewRunDir makes the directory of a run, mode 0700 under an unpredictable
// name, and registers it for removal by Remove and by StopAll.
func NewRunDir() (*RunDir, error) {
	dir, err := os.MkdirTemp(os.TempDir(), "sdlc-run-")
	if err != nil {
		return nil, err
	}
	Register(dir)
	run := filepath.Join(dir, "run")
	if err := os.Mkdir(run, 0o700); err != nil {
		_ = Remove(dir)
		return nil, err
	}
	return &RunDir{dir: dir, run: run}, nil
}

// Path is the run's directory, which holds everything below.
func (d *RunDir) Path() string { return d.dir }

// Run is the directory handed on as SDLC_RUN_DIR.
func (d *RunDir) Run() string { return d.run }

// ListTests writes names, one per line, to the list file handed on as
// SDLC_TEST_FILES. Each name is the caller's to have checked for a line break.
func (d *RunDir) ListTests(names []string) error {
	list := filepath.Join(d.dir, "test-files")
	var b strings.Builder
	for _, name := range names {
		b.WriteString(name)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(list, []byte(b.String()), 0o600); err != nil {
		return err
	}
	d.list = list
	return nil
}

// Env is base with any SDLC_RUN_DIR or SDLC_TEST_FILES it held taken out and
// the run's own added, slash-separated as every path inside the loop is. A
// value the invoker set would otherwise reach the commands in place of the
// run's, or beside it on Windows, where names differ only in case.
func (d *RunDir) Env(base []string) []string {
	env := slices.DeleteFunc(slices.Clone(base), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return sameName(name, RunDirVar) || sameName(name, TestFilesVar)
	})
	env = append(env, RunDirVar+"="+filepath.ToSlash(d.run))
	if d.list != "" {
		env = append(env, TestFilesVar+"="+filepath.ToSlash(d.list))
	}
	return env
}

// Close removes the run's directory and everything in it.
func (d *RunDir) Close() error { return Remove(d.dir) }

// sameName compares two variable names as the system does.
func sameName(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// removals holds the paths a run made that go when it ends: its directory and
// the temporary output file. An interrupt removes them through StopAll, so
// that a run stopped with Ctrl+C leaves nothing behind.
var removals struct {
	sync.Mutex
	paths []string
}

// Register has path removed by Remove, or by StopAll.
func Register(path string) {
	removals.Lock()
	defer removals.Unlock()
	if !slices.Contains(removals.paths, path) {
		removals.paths = append(removals.paths, path)
	}
}

// Remove deletes path, with everything under it, and forgets it.
func Remove(path string) error {
	removals.Lock()
	removals.paths = slices.DeleteFunc(removals.paths, func(p string) bool { return p == path })
	removals.Unlock()
	return os.RemoveAll(path)
}

// Keep forgets path without deleting it: an output file that could not be
// put in place holds what the commands printed, and is kept for a person.
func Keep(path string) {
	removals.Lock()
	defer removals.Unlock()
	removals.paths = slices.DeleteFunc(removals.paths, func(p string) bool { return p == path })
}

// removeAll deletes every registered path, once the commands that may write
// to them have been stopped.
func removeAll() {
	removals.Lock()
	paths := removals.paths
	removals.paths = nil
	removals.Unlock()
	for _, p := range paths {
		_ = os.RemoveAll(p)
	}
}
