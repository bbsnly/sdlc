package cli

// US-107 in process: a stop that waits for the loop's state while another
// stop holds it past the whole ten seconds, seen without loading the machine
// and without ten seconds on the wall (AC-1), and the one refusal that a stop
// is run again for, said and counted (AC-2). The AC numbers in this file are
// US-107's; TestAC13TwoStoriesStoppedAtTheSameMomentBothLandOneAfterTheOther,
// in landing_ontrunk_test.go, is the criterion's own test and uses what is
// here.
//
// Two seams, both fields of commandEnv that only a test in this package sets
// (ANALYSIS.md §7): lockClock, the clock every wait of the run for the
// project's lock is measured on, and pauseLanding, called by a stop as it
// begins to land, with the project's lock and the landing lock held. A run
// from Execute or ExecuteIn has neither: the wait is ten seconds of the real
// clock and nothing pauses.
//
// Helpers carry an lw prefix, for lock wait.

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
	"github.com/bbsnly/sdlc/internal/store"
)

// lwTries is how many times a stop refused for the loop's state is run again:
// the runbook's "stop again, up to three times" (plugin/skills/next/SKILL.md).
const lwTries = 3

// lwStillRunning is what the one refusal a stop is run again for says: the
// project's lock, held by another command past the whole wait.
const lwStillRunning = "another sdlc command is still running"

// lwPatience bounds a wait for something that has to happen; a deadline, not
// a pause.
const lwPatience = 3 * time.Minute

// lwClock is a store.Clock that leaps a minute every time it is slept on, and
// takes no time on the wall doing it: a wait measured on it runs out at its
// second look.
type lwClock struct {
	mu    sync.Mutex
	now   time.Time
	slept int
}

func newLWClock() *lwClock {
	return &lwClock{now: time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC)}
}

func (c *lwClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *lwClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d + time.Minute)
	c.slept++
}

func (c *lwClock) sleeps() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.slept
}

// lwStop is `sdlc stop --json` in dir from session, as otStop runs it, with
// the run's lock clock and pause set. Either may be nil.
func lwStop(dir, session string, clock store.Clock, pause func()) result {
	var out, errb strings.Builder
	getenv := func(name string) string {
		if name == store.SessionEnv {
			return session
		}
		return os.Getenv(name)
	}
	env := commandEnv{dir: dir, getenv: getenv, lockClock: clock, pauseLanding: pause}
	code := executeAt(env, []string{"stop", "--json"}, strings.NewReader(""), &out, &errb)
	return result{stdout: out.String(), stderr: errb.String(), code: code}
}

// lwRefusal is the JSON document a refused `--json` command printed.
type lwRefusal struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	Why   string `json:"why"`
	Code  string `json:"code"`
}

func lwRefusalOf(r result) (lwRefusal, bool) {
	var doc lwRefusal
	if err := json.Unmarshal([]byte(r.stdout), &doc); err != nil {
		return lwRefusal{}, false
	}
	return doc, true
}

// lwWaitedOut reports whether r is a stop refused because another command
// held the loop's state past the whole wait: SDLC-E0006, saying another sdlc
// command is still running. Nothing else. The same code refuses a landing
// still running after eight minutes and a second stop of a story already
// landing, and running either again would hide what went wrong.
func lwWaitedOut(r result) bool {
	doc, ok := lwRefusalOf(r)
	return ok && r.code == 1 && !doc.OK &&
		doc.Code == sdlcerr.StateUnwritable.String() && doc.Error == lwStillRunning
}

// lwStopAgain runs a stop of story with attempt, try 0 first, and where it is
// refused because another command held the loop's state past the wait, runs
// it again, up to lwTries times, saying so each time. It returns the last
// result and how many times it ran the stop again.
func lwStopAgain(t *testing.T, story string, attempt func(try int) result) (result, int) {
	t.Helper()
	r := attempt(0)
	again := 0
	for again < lwTries && lwWaitedOut(r) {
		again++
		t.Logf("US-107 AC-2: the stop of %s was refused with %s, another command holding the loop's state past the whole wait; "+
			"as the runbook allows, it is run again, time %d of at most %d:\n%s",
			story, sdlcerr.StateUnwritable, again, lwTries, r.stdout)
		r = attempt(again)
	}
	return r, again
}

// lwLockRoot is where the project of the checkout main keeps its lock, and
// the store that reads it.
func lwLockRoot(t *testing.T, main string) (string, *store.Store) {
	t.Helper()
	cfg, err := config.Load(main)
	if err != nil {
		t.Fatal(err)
	}
	s := store.New(&config.Project{Root: main, Config: cfg})
	return s.LockRoot(), s
}

// lwPaused is a stop run beside the test that pauses where it begins to land,
// holding the loop's state, until the test lets it go.
type lwPaused struct {
	reached  chan struct{}
	finished chan struct{}
	calls    atomic.Int32
	letGo    func()
	r        result
}

// lwPausedStop begins `sdlc stop` in dir from ldSession with its pause set.
// inside, where it is given, runs at the first pause, while the stop holds
// what it holds there. The stop is let go and waited for when the test ends,
// whatever the test did: a paused stop holds the loop's state, and one that
// outlived its test would hold it for every test after.
func lwPausedStop(t *testing.T, dir string, inside func()) *lwPaused {
	t.Helper()
	p := &lwPaused{reached: make(chan struct{}), finished: make(chan struct{})}
	release := make(chan struct{})
	var letGo, reach sync.Once
	p.letGo = func() { letGo.Do(func() { close(release) }) }
	pause := func() {
		p.calls.Add(1)
		reach.Do(func() {
			if inside != nil {
				inside()
			}
			close(p.reached)
		})
		<-release
	}
	go func() {
		defer close(p.finished)
		p.r = lwStop(dir, ldSession, nil, pause)
	}()
	t.Cleanup(func() {
		p.letGo()
		select {
		case <-p.finished:
		case <-time.After(lwPatience):
			t.Errorf("the stop paused in %s had not returned %s after it was let go", dir, lwPatience)
		}
	})
	return p
}

// waitReached returns once the stop has paused, and ends the test where it
// returned without pausing.
func (p *lwPaused) waitReached(t *testing.T) {
	t.Helper()
	select {
	case <-p.reached:
	case <-p.finished:
		t.Fatalf("the stop returned without pausing where it begins to land (exit %d):\n%s%s", p.r.code, p.r.stdout, p.r.stderr)
	case <-time.After(lwPatience):
		t.Fatalf("the stop had not paused where it begins to land after %s", lwPatience)
	}
}

// result lets the stop go and returns what it said.
func (p *lwPaused) result(t *testing.T) result {
	t.Helper()
	p.letGo()
	select {
	case <-p.finished:
		return p.r
	case <-time.After(lwPatience):
		t.Fatalf("the stop had not returned %s after it was let go", lwPatience)
		return result{}
	}
}

// AC-1: the pause is where a stop begins to land, and there it holds the
// project's lock and the lock every landing holds: another command waiting for
// the loop's state is refused naming the stop, and another landing cannot
// begin. It pauses once, and let go, the stop lands the story.
func TestAC1AStopPausedAsItBeginsToLandHoldsTheLoopsStateAndTheLandingLockAndLandsOnceLetGo(t *testing.T) {
	t.Parallel()
	main := lxRepository(t, nil, nil, wtStory{id: "US-001"})
	otIdentity(t, main)
	wt := ldFinished(t, main)
	root, s := lwLockRoot(t, main)

	var projectErr, landingErr error
	p := lwPausedStop(t, wt, func() {
		g, err := store.LockOn(root, "status", newLWClock())
		g.Release()
		projectErr = err
		turn, err := s.LockLandings("US-002", 0)
		turn.Release()
		landingErr = err
	})
	p.waitReached(t)
	r := p.result(t)

	var e *sdlcerr.Error
	switch {
	case projectErr == nil:
		t.Error("the project's lock was free while the stop paused where it begins to land")
	case !errors.As(projectErr, &e) || e.What != lwStillRunning || !strings.Contains(e.Why, "`sdlc stop`"):
		t.Errorf("waiting for the loop's state while the stop paused: %v, want it held by `sdlc stop`", projectErr)
	}
	if !errors.Is(landingErr, store.ErrLandingHeld) {
		t.Errorf("taking the landing lock while the stop paused: %v, want it held by the stop", landingErr)
	}
	if r.code != 0 {
		t.Errorf("the stop exited %d once let go, want the story landed:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if got := p.calls.Load(); got != 1 {
		t.Errorf("the stop paused %d times, want once", got)
	}
	if got := wtStatuses(t, main)["US-001"]; got != "done" {
		t.Errorf("US-001 is %s after the stop, want done", got)
	}
}

// AC-1: a stop with nothing to land never reaches the pause, which is the
// landing's and nobody else's.
func TestAC1AStopWithNothingToLandNeverPauses(t *testing.T) {
	t.Parallel()
	main := lxRepository(t, nil, nil, wtStory{id: "US-001"})
	var calls atomic.Int32
	r := lwStop(main, "", nil, func() { calls.Add(1) })
	if r.code != 0 {
		t.Fatalf("stop with no iteration running exited %d:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if got := calls.Load(); got != 0 {
		t.Errorf("a stop that landed nothing paused %d times", got)
	}
}

// AC-1: a stop given a clock waits for the loop's state on that clock, and
// once ten seconds of it have passed with another command holding the state,
// is refused as a stop is after ten real seconds: SDLC-E0006, naming who holds
// it and for how long. That refusal, and only that, is the one AC-2 runs a
// stop again for.
func TestAC1AStopWaitingForTheLoopsStateRunsOutOnTheClockItWasGiven(t *testing.T) {
	t.Parallel()
	main := lxRepository(t, nil, nil, wtStory{id: "US-001"})
	root, _ := lwLockRoot(t, main)
	held, err := store.Lock(root, "review add")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(held.Release)

	clock := newLWClock()
	r := lwStop(main, "", clock, nil)
	doc, ok := lwRefusalOf(r)
	if r.code != 1 || !ok {
		t.Fatalf("stop exited %d with the loop's state held, want it refused in JSON:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if !lwWaitedOut(r) {
		t.Errorf("the refusal is %s %q, want %s %q", doc.Code, doc.Error, sdlcerr.StateUnwritable, lwStillRunning)
	}
	for _, want := range []string{"`sdlc review add`", "longer than 10s"} {
		if !strings.Contains(doc.Why, want) {
			t.Errorf("why = %q, want it to say %q", doc.Why, want)
		}
	}
	if clock.sleeps() == 0 {
		t.Error("the stop never slept on the clock it was given: its wait was not measured on it")
	}
}

// AC-1, THREATS: a test that ends with a stop paused lets it go and waits for
// it, and the stop gives the loop's state back. Nothing a test pauses holds
// the project once the test is over.
func TestAC1AStopPausedByATestIsLetGoAndGivesTheLoopsStateBackWhenTheTestEnds(t *testing.T) {
	t.Parallel()
	main := lxRepository(t, nil, nil, wtStory{id: "US-001"})
	otIdentity(t, main)
	wt := ldFinished(t, main)
	root, _ := lwLockRoot(t, main)

	var p *lwPaused
	t.Run("a test that ends with its stop paused", func(t *testing.T) {
		p = lwPausedStop(t, wt, nil)
		p.waitReached(t)
	})
	select {
	case <-p.finished:
	default:
		t.Fatal("the stop was still running after the test that paused it had ended")
	}
	if p.r.code != 0 {
		t.Errorf("the stop let go at the end of its test exited %d:\n%s%s", p.r.code, p.r.stdout, p.r.stderr)
	}
	g, err := store.LockOn(root, "status", newLWClock())
	if err != nil {
		t.Fatalf("the loop's state was still held after the test that paused the stop had ended: %v", err)
	}
	g.Release()
}

// AC-2: of the refusals a stop can meet, the one it is run again for is
// SDLC-E0006 saying another command is still running, and no other: not the
// same code for a landing that outran the eight-minute wait, for another stop
// landing the same story, or for one landing another story; not the same
// words under another code; not a stop that worked. And it is run again at
// most three times.
func TestAC2OnlyARefusalForTheLoopsStateHeldPastTheWaitIsRunAgainAndAtMostThreeTimes(t *testing.T) {
	t.Parallel()
	refused := func(code sdlcerr.Code, what string) result {
		raw, err := json.Marshal(newErrorPayload(sdlcerr.New(code, what, "the reason").WithFix("the fix")))
		if err != nil {
			t.Fatal(err)
		}
		return result{stdout: string(raw) + "\n", code: 1}
	}
	cases := []struct {
		name  string
		r     result
		again bool
	}{
		{"the loop's state held past the wait", refused(sdlcerr.StateUnwritable, lwStillRunning), true},
		{"a landing still running after eight minutes", refused(sdlcerr.StateUnwritable, "another story was still landing after 8 minutes"), false},
		{"another stop landing the same story", refused(sdlcerr.StateUnwritable, "another sdlc stop is landing US-001"), false},
		{"another story landing", refused(sdlcerr.StateUnwritable, "another story is still landing"), false},
		{"the lock could not be made", refused(sdlcerr.StateUnwritable, "the loop's state could not be locked"), false},
		{"the same words under another code", refused(sdlcerr.LandingCheckFailed, lwStillRunning), false},
		{"a stop that landed", result{stdout: `{"ok":true,"story":"US-001","done":true}` + "\n"}, false},
		{"a stop that printed no JSON", result{stderr: "sdlc: " + lwStillRunning + "\n", code: 1}, false},
	}
	for _, tc := range cases {
		t.Run("US-107 AC-2 "+tc.name, func(t *testing.T) {
			if got := lwWaitedOut(tc.r); got != tc.again {
				t.Errorf("run again = %v, want %v for:\n%s%s", got, tc.again, tc.r.stdout, tc.r.stderr)
			}
			tries := 0
			_, again := lwStopAgain(t, "US-001", func(int) result { tries++; return tc.r })
			want := map[bool]int{true: lwTries, false: 0}[tc.again]
			if again != want || tries != want+1 {
				t.Errorf("the stop was run %d times and again %d times, want %d and %d", tries, again, want+1, want)
			}
		})
	}
}

// AC-1, THREATS: nothing but a test sets either seam. No code of this package
// outside its tests puts a lock clock or a pause into a commandEnv, so neither
// can come from the environment, a flag or a file; and no package but this one
// and the store's own waits for the project's lock on a clock of its own.
func TestAC1NoCodeOutsideTheTestsSetsTheLockClockOrThePause(t *testing.T) {
	t.Parallel()
	seams := map[string]bool{"lockClock": true, "pauseLanding": true}
	fset := token.NewFileSet()
	parse := func(path string) *ast.File {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return file
	}

	names, err := filepath.Glob("*.go")
	if err != nil || len(names) == 0 {
		t.Fatalf("this package's sources: %v", err)
	}
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		ast.Inspect(parse(name), func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if id, ok := n.Type.(*ast.Ident); !ok || id.Name != "commandEnv" {
					return true
				}
				for _, elt := range n.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						t.Errorf("%s: a commandEnv is built without naming its fields, which could set a seam", fset.Position(n.Pos()))
						break
					}
					if key, ok := kv.Key.(*ast.Ident); ok && seams[key.Name] {
						t.Errorf("%s: a commandEnv is built with %s set outside the tests", fset.Position(kv.Pos()), key.Name)
					}
				}
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if sel, ok := lhs.(*ast.SelectorExpr); ok && seams[sel.Sel.Name] {
						t.Errorf("%s: %s is set outside the tests", fset.Position(sel.Pos()), sel.Sel.Name)
					}
				}
			}
			return true
		})
	}

	module := filepath.Join("..", "..")
	err = filepath.WalkDir(module, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != module && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		if strings.HasSuffix(dir, "internal/cli") || strings.HasSuffix(dir, "internal/store") {
			return nil
		}
		ast.Inspect(parse(path), func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "LockOn" {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "store" {
					t.Errorf("%s: store.LockOn is called outside internal/cli", fset.Position(sel.Pos()))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
