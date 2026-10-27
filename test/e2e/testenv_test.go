package e2e

import (
	"flag"
	"os"
	"runtime"
	"strconv"
	"testing"

	"github.com/bbsnly/sdlc/internal/testenv"
)

func TestMain(m *testing.M) {
	flag.Parse()
	atLeastTwoParallel()
	os.Exit(testenv.Run(m))
}

// atLeastTwoParallel gives the package's parallel tests two slots or more, as
// many as go test would (GOMAXPROCS), unless the command line said how many.
// The guard in zcommands_test.go waits in a slot of its own for the tests
// that run in parallel, and on a machine of one CPU go test gives one.
func atLeastTwoParallel() {
	given := false
	flag.Visit(func(f *flag.Flag) { given = given || f.Name == "test.parallel" })
	if given || runtime.GOMAXPROCS(0) >= 2 {
		return
	}
	if err := flag.Set("test.parallel", strconv.Itoa(2)); err != nil {
		panic(err)
	}
}
