package e2e

import (
	"os"
	"testing"

	"github.com/bbsnly/sdlc/internal/testenv"
)

func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m))
}
