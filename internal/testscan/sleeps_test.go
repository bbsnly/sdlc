package testscan

// US-069 AC-3: a test that sleeps has to be on a named list, with a reason.
//
//	type Sleep struct{ File, Func, Reason string }
//
//	func Sleeps(root string) ([]Sleep, error)
//	    Every function in a *_test.go file under root that calls time.Sleep,
//	    once each however many calls it holds, with no Reason, sorted by File and
//	    then Func. File is slash-separated and relative to root. Func is the
//	    name of the declaration the call is in, a closure counting as the
//	    function it is written in: "TestX", "suite.TestY" for a method whatever
//	    its receiver (pointer or not), and "(package level)" for a call in the
//	    initialiser of a package-level variable. The file is parsed, not
//	    searched, so a sleep named in a string or a comment is not a call, and
//	    the package may be imported under another name. A file that is not
//	    *_test.go is not read, and .git, .sdlc, node_modules, dist and testdata
//	    are not entered (.sdlc/worktrees holds whole older copies of the
//	    repository, which are not this one's tests). A directory that is not
//	    there, and a test file that does not parse, are errors.
//
//	func CheckSleeps(root string, allowed []Sleep) ([]string, error)
//	    What is wrong with the sleeps under root against the list given, one
//	    line each, sorted, and nothing when the list is the sleeps there are:
//
//	      "<File>: <Func> calls time.Sleep and is not on the list"
//	      "<File>: <Func> is on the list but calls no time.Sleep"
//	      "<File>: <Func> is on the list without a reason"
//
//	An entry that is on the list and calls no time.Sleep is the second line
//	whatever its Reason; a reason that is only blank space is none.

import (
	"slices"
	"testing"
)

const sleepFixtureA = `package a

import "time"

type suite struct{}

func TestPlain(t *testing.T) {
	time.Sleep(1)
	time.Sleep(2)
}

func (s *suite) TestMethod(t *testing.T) { time.Sleep(1) }

func helper() {
	go func() { time.Sleep(1) }()
}

var warmUp = func() { time.Sleep(1) }

// time.Sleep(1) in a comment is no call.
func TestMentions(t *testing.T) {
	_ = "time.Sleep(1)"
	other.Sleep(1)
}
`

func sleepFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put(t, root, "a/a_test.go", sleepFixtureA)
	put(t, root, "a/b_test.go", "package a\n\nimport clock \"time\"\n\nfunc TestAliased() { clock.Sleep(1) }\n")
	// Not test files, or not this repository's tests.
	put(t, root, "a/a.go", "package a\n\nimport \"time\"\n\nfunc Production() { time.Sleep(1) }\n")
	for _, skipped := range []string{
		".sdlc/worktrees/old/a_test.go", ".git/a_test.go", "node_modules/n/a_test.go", "dist/a_test.go",
		"testdata/a_test.go", "a/testdata/a_test.go",
	} {
		put(t, root, skipped, "package old\n\nimport \"time\"\n\nfunc TestOld() { time.Sleep(1) }\n")
	}
	return root
}

func TestAC3EveryTestFunctionThatSleepsIsFoundOnceByParsingTheFile(t *testing.T) {
	got, err := Sleeps(sleepFixture(t))
	if err != nil {
		t.Fatalf("Sleeps: %v", err)
	}
	want := []Sleep{
		{File: "a/a_test.go", Func: "(package level)"},
		{File: "a/a_test.go", Func: "TestPlain"},
		{File: "a/a_test.go", Func: "helper"},
		{File: "a/a_test.go", Func: "suite.TestMethod"},
		{File: "a/b_test.go", Func: "TestAliased"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Sleeps =\n%+v\nwant\n%+v", got, want)
	}
	if _, err := Sleeps(t.TempDir() + "/absent"); err == nil {
		t.Error("Sleeps of a directory that is not there said nothing was wrong")
	}
	// A test file that cannot be read is an error, not a file with no sleep in
	// it: a guard that skipped it would pass on exactly the file it could not see.
	broken := t.TempDir()
	put(t, broken, "a/a_test.go", "package a\n\nfunc TestBroken( {\n")
	if _, err := Sleeps(broken); err == nil {
		t.Error("Sleeps of a test file that does not parse said nothing was wrong")
	}
}

func TestAC3ASleepOffTheListAndAnEntryThatMatchesNothingAreBothNamed(t *testing.T) {
	root := sleepFixture(t)

	problems, err := CheckSleeps(root, []Sleep{
		{File: "a/a_test.go", Func: "TestPlain", Reason: "waits for a reader to let go"},
		{File: "a/b_test.go", Func: "TestAliased", Reason: "gives a goroutine time to start"},
		{File: "a/a_test.go", Func: "helper", Reason: "  "},
		{File: "a/a_test.go", Func: "Gone", Reason: "the test was removed"},
	})
	if err != nil {
		t.Fatalf("CheckSleeps: %v", err)
	}
	want := []string{
		"a/a_test.go: (package level) calls time.Sleep and is not on the list",
		"a/a_test.go: Gone is on the list but calls no time.Sleep",
		"a/a_test.go: helper is on the list without a reason",
		"a/a_test.go: suite.TestMethod calls time.Sleep and is not on the list",
	}
	if !slices.Equal(problems, want) {
		t.Errorf("CheckSleeps =\n%q\nwant\n%q", problems, want)
	}

	problems, err = CheckSleeps(root, []Sleep{
		{File: "a/a_test.go", Func: "(package level)", Reason: "r"},
		{File: "a/a_test.go", Func: "TestPlain", Reason: "r"},
		{File: "a/a_test.go", Func: "helper", Reason: "r"},
		{File: "a/a_test.go", Func: "suite.TestMethod", Reason: "r"},
		{File: "a/b_test.go", Func: "TestAliased", Reason: "r"},
	})
	if err != nil || len(problems) != 0 {
		t.Errorf("a list that is the sleeps there are: %q, %v; want nothing wrong", problems, err)
	}

	if _, err := CheckSleeps(root+"/absent", nil); err == nil {
		t.Error("CheckSleeps of a directory that is not there said nothing was wrong")
	}
}

// allowedSleeps is every sleep in a test file today, and why it is there. A
// sleep is a wall-clock wait, which makes a run slow on a busy runner and
// flaky on a slow one; each of these waits for something that happens in
// another goroutine, which no channel the code under test offers can be
// waited on instead, and each was kept rather than removed (US-069 leaves them
// alone). A new one needs a person to add it here, with the reason.
var allowedSleeps = []Sleep{
	{"internal/fsx/fsx_test.go", "TestWriteFileAtomicWaitsForAReaderToLetGo",
		"a goroutine holds the file open for 100 ms before closing it, so the atomic write has to retry (replacePoll in fsx.go)"},
	{"internal/hook/stop_test.go", "stopWhileLocked",
		"gives the Stop hook, running in a goroutine, 300 ms to reach the lock that is held before during(root) is run"},
	{"internal/store/guard_test.go", "TestALockStillHeldIsNotTakenForAbandoned",
		"polls every 5 ms, bounded by a 2 s deadline, until a held lock has refreshed its modification time"},
	{"internal/store/guard_test.go", "TestNoChangeIsLostUnderContention",
		"1 ms inside the lock makes read-change-write slow enough to lose updates if the lock did not hold"},
	{"internal/store/guard_test.go", "TestCommandsBreakingOpenOneAbandonedLockDoNotAllGetIt",
		"1 ms while a command holds the lock widens the overlap window whose size is being counted"},
	{"internal/store/guard_test.go", "TestAnInterruptedCommandGivesTheLockBack",
		"a goroutine finishes its write after 20 ms, so the holder of the lock is given time to let go"},
}

func TestAC3NoTestCallsTimeSleepWhereTheNamedListDoesNotSayWhy(t *testing.T) {
	problems, err := CheckSleeps("../..", allowedSleeps)
	if err != nil {
		t.Fatalf("CheckSleeps: %v", err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}
