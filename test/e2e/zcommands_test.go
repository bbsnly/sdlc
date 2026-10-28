// US-069 AC-1: every command has a case in this package that ran it to the
// result the case expected, or a reason on this list why not.
//
// The name of this file is part of how it works. Go runs a package's tests in
// the order of its files' names, and the guard has to run after every test that
// can record a run; no file here sorts after this one. A test that runs in
// parallel with the others starts in that order and finishes later, so it says
// so through parallel(t), and the guard waits for every one that did. Under
// -run, -skip, -shuffle and -list the package has not run whole, and the guard
// says so instead of naming commands whose cases were left out.
//
// What counts as a run is what the launch sites below note (noteRun, noteHook)
// and nothing else:
//
//   - a command that exited 0, noted where it was started, so a refusal (which
//     exits 1 or 2) never counts, and neither does a command spelled in a
//     payload handed to the hook, which is one string and names no command;
//   - a command that was expected to exit otherwise, noted by the case that
//     compared its exit code and streams (sandbox.holds, for `sdlc doctor`);
//   - the hook, noted when it answered and exited 0 whether it let the call
//     through or refused it, through the launcher or directly;
//   - `sdlc parallel`, noted by the one case that runs it to the end it expects
//     (an interrupt that ends every session it started).
//
// A command only a person may run counts when the case runs it as a person
// (sandbox.person, which has no session and refuses a directory outside the
// sandbox); a session's attempt at it is refused and counts for nothing.

package e2e

import (
	"flag"
	"maps"
	"runtime"
	"sync"
	"testing"

	"github.com/bbsnly/sdlc/internal/testscan"
)

var ranOK = struct {
	sync.Mutex
	commands map[string]bool
}{commands: map[string]bool{}}

// noteRun records that the arguments given to the binary were run to the result
// the case expected. Arguments that name no command are not recorded.
func noteRun(args ...string) {
	if command, ok := testscan.Resolve(args); ok {
		ranOK.Lock()
		defer ranOK.Unlock()
		ranOK.commands[command] = true
	}
}

// noteHook records that the hook answered.
func noteHook() { noteRun("hook") }

// inParallel is every test that called parallel and has not finished.
var inParallel sync.WaitGroup

// parallel runs t in parallel with the package's other tests, and has the guard
// wait for it. One after another the tests took six and a half minutes on a
// ten-core Mac under the race detector, each of them mostly waiting on the
// binary and the processes it starts, in a repository of its own: nothing one
// test makes is another's. A test that sets the environment (t.Setenv) cannot
// run in parallel, and does not call this.
func parallel(t *testing.T) {
	t.Helper()
	inParallel.Add(1)
	t.Cleanup(inParallel.Done)
	t.Parallel()
}

// knownGaps is every command no case here runs, and why. A gap that stops being
// one fails, so this list cannot outlive its cause.
func knownGaps() map[string]string {
	gaps := map[string]string{
		"story":    "prints its help only; its work is done by story list",
		"artifact": "prints its help only; its work is done by artifact list and artifact write",
		"review":   "prints its help only; its work is done by review add and review list",
		"staffing": "prints its help only; its work is done by staffing show, set, amend and assign",
		"codemap":  "prints its help only; its work is done by codemap write",
	}
	if runtime.GOOS == "windows" {
		gaps["parallel"] = "its one real run starts sessions through a fake claude that waits on FIFOs, which need a POSIX shell and mkfifo"
		gaps["codemap write"] = "its cases land stories through the POSIX launcher (skipWithoutTheLauncher)"
		gaps["remap"] = "its cases land stories through the POSIX launcher (skipWithoutTheLauncher)"
	}
	return gaps
}

// partialRun says why this package has not run whole, or "" when it has.
func partialRun() string {
	for _, f := range []struct{ flag, whole, why string }{
		{"test.run", "", "-run selects tests"},
		{"test.skip", "", "-skip leaves tests out"},
		{"test.shuffle", "off", "-shuffle changes the order the guard depends on"},
		{"test.list", "", "-list runs none"},
	} {
		if fl := flag.Lookup(f.flag); fl != nil && fl.Value.String() != f.whole {
			return f.why
		}
	}
	return ""
}

func TestAC1EveryCommandAndTheHookHaveACaseThatRanItToTheResultItExpected(t *testing.T) {
	if why := partialRun(); why != "" {
		t.Skipf("the cases that run the commands are not all being run (%s), so a command with no run says nothing", why)
	}
	// Every other test has started by now, this file sorting last, and has
	// called parallel if it is going to. The guard has to be parallel too to
	// run after them, and then it holds one of go test's -parallel slots while
	// it waits; with one slot, the tests it waits for would never run.
	if fl := flag.Lookup("test.parallel"); fl != nil && fl.Value.String() == "1" {
		t.Skip("-parallel 1 leaves the guard no slot to wait in but the one the tests it waits for need, so it cannot run; " +
			"TestMain gives the package two or more unless the command line says 1")
	}
	t.Parallel()
	inParallel.Wait()
	ranOK.Lock()
	ran := maps.Clone(ranOK.commands)
	ranOK.Unlock()

	problems := testscan.Unrun(ran, knownGaps())
	for _, p := range problems {
		t.Error(p)
	}
	if len(problems) > 0 {
		t.Log("a case that failed earlier in this run records nothing, and shows here as a command with no run; " +
			"fix that first. Otherwise add a case that runs the command, or a gap above with its reason.")
	}
}
