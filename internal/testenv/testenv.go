// Package testenv runs a package's tests in the environment they were written
// for, on whatever machine runs them.
//
// CI's runners start clean; a developer's machine does not. A global git
// configuration that signs every commit failed 118 tests on "gpg failed to
// sign", a temp directory inside a repository let git find that repository
// from a test that is outside any, and SDLC_ENFORCE=0 or SDLC_DEBUG=1 left
// exported in the shell turned enforcement off under the end-to-end tests or
// put debug lines into output the installer tests compare.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run runs m and returns its exit code, for TestMain. While it runs, git reads
// no configuration but a repository's own and stops looking for a repository
// at the temp directory, and no SDLC_ or CLAUDE_ variable is set.
func Run(m *testing.M) int {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "SDLC_") || strings.HasPrefix(name, "CLAUDE_") {
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
	if err := os.WriteFile(global, nil, 0o600); err != nil {
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
