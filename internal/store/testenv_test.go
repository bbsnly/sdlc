package store

import (
	"os"
	"testing"

	"github.com/bbsnly/sdlc/internal/testenv"
)

// Most tests here call t.Parallel, each with a directory of its own: the lock a
// command takes is keyed on the project's path, so two tests never meet at
// one. The tests that stay serial are those that change what every test shares.
// cacheDir moves the cache directory the locks are taken in through the
// environment, which go refuses in a parallel test, and the tests that swap
// guardWait, guardBeat or mkdir would change the lock code under the others.
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}
