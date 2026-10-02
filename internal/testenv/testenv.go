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
// sent every test repository to the index of the commit being made.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// quietGit is the global configuration the tests' git reads. Git 2.55
// maintains a repository in the background after a commit, and its
// objects/maintenance.lock came and went under captureTree while it copied a
// template repository: internal/cli failed one run in 40. A test repository
// never needs collecting, so neither maintenance nor gc runs.
const quietGit = "[maintenance]\n\tauto = false\n[gc]\n\tauto = 0\n"

// Run runs m and returns its exit code, for TestMain. While it runs, git reads
// no configuration but a repository's own and the two settings that turn its
// automatic maintenance off, and stops looking for a repository at the temp
// directory; no SDLC_ or CLAUDE_ variable is set, nor any GIT_ variable but
// the three that say so.
func Run(m *testing.M) int {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "SDLC_") || strings.HasPrefix(name, "CLAUDE_") || strings.HasPrefix(name, "GIT_") {
			_ = os.Unsetenv(name)
		}
	}

	dir, err := os.MkdirTemp("", "sdlc-testenv-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, []byte(quietGit), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}
	for name, value := range map[string]string{
		"GIT_CONFIG_GLOBAL":       global,
		"GIT_CONFIG_NOSYSTEM":     "1",
		"GIT_CEILING_DIRECTORIES": os.TempDir(),
	} {
		if err := os.Setenv(name, value); err != nil {
			fmt.Fprintln(os.Stderr, "testenv:", err)
			return 1
		}
	}
	return m.Run()
}
