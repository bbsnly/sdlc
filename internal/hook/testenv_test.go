package hook

import (
	"os"
	"testing"

	"github.com/bbsnly/sdlc/internal/testenv"
)

// Most tests here call t.Parallel, and the process-wide state they would share
// is why some do not. A test that sets HOME, XDG_CACHE_HOME, GIT_DIR or another
// variable (the hook reads the home with os.UserHomeDir, and git reads the
// rest), or that swaps the default logger, stays serial: go refuses t.Setenv
// in a parallel test, and the logger would be swapped under the others. The
// repository helpers do not set GIT_DIR, GIT_WORK_TREE or GIT_COMMON_DIR for
// the same reason, since the run starts without them.
func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}
