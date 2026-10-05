package cli

import (
	"flag"
	"os"
	"runtime"
	"strconv"
	"testing"

	"github.com/bbsnly/sdlc/internal/testenv"
)

func TestMain(m *testing.M) {
	// The fake claude of US-058 is this binary again: see parallel_fake_test.go.
	if os.Getenv(plChildEnv) != "" {
		os.Exit(plChildMain())
	}
	flag.Parse()
	raiseParallel()
	os.Exit(testenv.Run(m))
}

// parallelPerCPU is how many of this package's parallel tests run at once for
// each CPU go test may use.
//
// US-055 AC-5: go test runs GOMAXPROCS parallel tests at once unless told
// otherwise, and these tests spend their time waiting for git, not computing.
// On windows-latest, with 4 vCPUs, 126 of them were still waiting for a slot
// when go test's ten minutes ran out (CI run 36964608101), with the four
// running ones all waiting on a child process. Raising -parallel is not
// raising a limit (ANALYSIS.md, D2); no timeout changes.
const parallelPerCPU = 2

// parallelFromCommandLine is whether the command line said how many parallel
// tests to run at once, which raiseParallel then leaves as it is.
var parallelFromCommandLine bool

// raiseParallel sets -test.parallel to parallelPerCPU for each CPU, unless the
// command line set it. It reads nothing from the environment, so it cannot
// carry the machine's own settings past testenv.Run; and it has to come
// before Run, because testing reads the value when m.Run starts the tests,
// which Run does.
func raiseParallel() {
	flag.Visit(func(f *flag.Flag) {
		parallelFromCommandLine = parallelFromCommandLine || f.Name == "test.parallel"
	})
	if parallelFromCommandLine {
		return
	}
	n := parallelPerCPU * runtime.GOMAXPROCS(0)
	if err := flag.Set("test.parallel", strconv.Itoa(n)); err != nil {
		panic(err)
	}
}

// AC-5: the package's parallel tests run more than GOMAXPROCS at once unless
// the command line says how many, and as many as it says when it does.
func TestAC5ParallelTestsRunMoreThanOnePerCPUUnlessTheCommandLineSaysOtherwise(t *testing.T) {
	got := flag.Lookup("test.parallel")
	if got == nil {
		t.Fatal("go test's -test.parallel flag is not registered")
	}
	if parallelFromCommandLine {
		t.Skipf("-test.parallel=%s was given on the command line, which wins", got.Value)
	}
	if want := strconv.Itoa(parallelPerCPU * runtime.GOMAXPROCS(0)); got.Value.String() != want {
		t.Errorf("-test.parallel = %s, want %s: %d for each of GOMAXPROCS's %d",
			got.Value, want, parallelPerCPU, runtime.GOMAXPROCS(0))
	}
}
