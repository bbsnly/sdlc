package main

// US-038, the program: `./task hook-time` runs it, and it times
// shellpolicy.Inspect on three commands that are slow by construction.
//
// Run through the real Inspect under the race detector, the fifteen calls take
// about 130 s, so nothing here does that. The tests hand the program an Inspect
// and a clock of their own, and these two functions are what it has to offer:
//
//	func run(args []string, stdout, stderr io.Writer,
//		inspect func(string, shellpolicy.State) (shellpolicy.Finding, bool),
//		now func() time.Time) int
//
//	func state(dir string, powerShell bool) shellpolicy.State
//
//	run({"-perl=3000", "-here-string=1000", "-dot-glob=1000"}, ...)
//	    each budget a whole number of milliseconds above zero. It makes an empty
//	    directory with os.MkdirTemp("", ...), hands each of the three commands to
//	    inspect five times with state(that directory, ...), reads now() before
//	    and after each call, and removes the directory on every way out.
//	exit 0      one line on stdout for each shape, holding its name ("perl",
//	            "here-string", "dot-glob"), its fastest run and its budget in
//	            milliseconds, each written as digits with no thousands separator
//	exit not 0  a budget that is missing or is not such a number, a directory
//	            that could not be made, a fastest run over its budget, or an
//	            answer that is not today's on any one of the five calls. Stderr
//	            says which and what to do.
//
// main is run(os.Args[1:], os.Stdout, os.Stderr, shellpolicy.Inspect, time.Now)
// and nothing more.

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// The names the program prints, which are the names AC-4 gives the budgets.
const (
	perl       = "perl"
	hereString = "here-string"
	dotGlob    = "dot-glob"
	unknown    = "a command of none of the three shapes"
)

var shapeNames = []string{perl, hereString, dotGlob}

// The three commands, written out from AC-1 and from nothing in the program:
// a test that asked the program's own generator would agree with whatever it
// made (THREATS T12).
var described = map[string]string{
	// `perl`, one word that is a dash and then only the letter a, the file
	// name `file`.
	perl: "perl -" + strings.Repeat("a", 8181) + " file",
	// 630 blocks of the opener, the one-line body x and the closer.
	hereString: strings.Repeat("$s = @'\nx\n'@\n", 630),
	// One path of 4,093 `a/` segments that ends in dot-star.
	dotGlob: "rm " + strings.Repeat("a/", 4093) + ".*",
}

// The lengths AC-1 gives, as figures, so that a slip in the table above is
// caught by something other than itself.
var describedLength = map[string]int{perl: 8192, hereString: 8190, dotGlob: 8191}

// What Inspect answers today: the removal refused, the other two allowed.
var refusedToday = map[string]bool{perl: false, hereString: false, dotGlob: true}

func shapeOf(command string) string {
	switch {
	case strings.HasPrefix(command, "perl"):
		return perl
	case strings.HasPrefix(command, "$s"):
		return hereString
	case strings.HasPrefix(command, "rm"):
		return dotGlob
	}
	return unknown
}

// leg is one run of the program against an Inspect and a clock the test owns.
// The clock moves only inside inspect, by what the test says that call took, so
// a timing is what the program measured and never what the machine was doing.
type leg struct {
	t    *testing.T
	base string

	mu sync.Mutex
	at time.Time
	// took is how long each call for a shape takes; a call it does not list
	// takes a millisecond.
	took map[string][]time.Duration
	// refuses, when set, is the answer for one call; today's answer otherwise.
	refuses func(shape string, call int) bool
	// during, when set, runs inside every call.
	during func(shape string, s shellpolicy.State)

	commands map[string][]string
	states   map[string][]shellpolicy.State
	total    int
}

// newLeg points the temporary directory at one of the test's own, so that the
// directory the program makes can be seen, and nothing is left in the real one.
func newLeg(t *testing.T) *leg {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pointTempAt(t, base)
	return &leg{
		t: t, base: base, at: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		took: map[string][]time.Duration{}, commands: map[string][]string{},
		states: map[string][]shellpolicy.State{},
	}
}

// pointTempAt sets what os.TempDir reads on every platform.
func pointTempAt(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, dir)
	}
}

func (l *leg) now() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.at
}

func (l *leg) inspect(command string, s shellpolicy.State) (shellpolicy.Finding, bool) {
	l.mu.Lock()
	shape := shapeOf(command)
	call := len(l.commands[shape])
	l.commands[shape] = append(l.commands[shape], command)
	l.states[shape] = append(l.states[shape], s)
	l.total++
	took := time.Millisecond
	if call < len(l.took[shape]) {
		took = l.took[shape][call]
	}
	l.at = l.at.Add(took)
	refused := refusedToday[shape]
	if l.refuses != nil {
		refused = l.refuses(shape, call)
	}
	during := l.during
	l.mu.Unlock()

	if during != nil {
		during(shape, s)
	}
	if refused {
		return shellpolicy.Finding{
			Rule: "loop-state-through-the-tool", Reason: "the test's Inspect refuses it",
			Route: "nothing; it is only ever classified",
		}, true
	}
	return shellpolicy.Finding{}, false
}

func (l *leg) run(args ...string) (stdout, stderr string, code int) {
	l.t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut, l.inspect, l.now)
	return out.String(), errOut.String(), code
}

func budgets(perlMS, hereStringMS, dotGlobMS int) []string {
	return []string{
		"-perl=" + strconv.Itoa(perlMS),
		"-here-string=" + strconv.Itoa(hereStringMS),
		"-dot-glob=" + strconv.Itoa(dotGlobMS),
	}
}

func ms(values ...float64) []time.Duration {
	out := make([]time.Duration, len(values))
	for i, v := range values {
		out[i] = time.Duration(v * float64(time.Millisecond))
	}
	return out
}

var figure = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)?`)

// figures is every number written in text, so that 1234 is found in "1234 ms"
// and in "1234.000 ms" alike and is not found in "12340".
func figures(text string) []float64 {
	var out []float64
	for _, f := range figure.FindAllString(text, -1) {
		v, err := strconv.ParseFloat(f, 64)
		if err == nil {
			out = append(out, v)
		}
	}
	return out
}

func flat(text string) string { return strings.Join(strings.Fields(text), " ") }

func nonEmptyLines(text string) []string {
	var out []string
	for l := range strings.SplitSeq(text, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func entries(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

// ---- AC-1: what is timed, and with what state ----

func TestAC1EachShapeIsHandedToInspectFiveTimesWithTheMatchersAndTheResolverWired(t *testing.T) {
	l := newLeg(t)
	_, stderr, code := l.run(budgets(3000, 1000, 1000)...)
	if code != 0 {
		t.Fatalf("exit %d with every run inside its budget; stderr:\n%s", code, stderr)
	}
	if l.total != 15 {
		t.Errorf("Inspect was called %d times, want 15: five for each of three shapes", l.total)
	}
	if n := len(l.commands[unknown]); n != 0 {
		t.Errorf("%d calls were for a command of none of the three shapes", n)
	}
	const newTestFile = "internal/widget/added_after_the_freeze_test.go"
	for _, shape := range shapeNames {
		if n := len(l.states[shape]); n != 5 {
			t.Errorf("%s: timed %d times, want 5", shape, n)
		}
		for i, s := range l.states[shape] {
			// Without the resolver the perl shape takes about 600 ms and not
			// 1,100, and a fourfold slowdown passes its budget.
			if s.Resolve == nil {
				t.Errorf("%s, call %d: State.Resolve is not set", shape, i+1)
			}
			if s.NewTest == nil || !s.NewTest(newTestFile) {
				t.Errorf("%s, call %d: State.NewTest does not call %s a new test file", shape, i+1, newTestFile)
			}
			if s.ImplementerTest == nil || !s.ImplementerTest(newTestFile) {
				t.Errorf("%s, call %d: State.ImplementerTest does not call %s a test file", shape, i+1, newTestFile)
			}
			if len(s.Frozen) == 0 {
				t.Errorf("%s, call %d: State.Frozen is empty, so the session holds no frozen tests", shape, i+1)
			}
			if want := shape == hereString; s.PowerShell != want {
				t.Errorf("%s, call %d: State.PowerShell is %v, want %v", shape, i+1, s.PowerShell, want)
			}
		}
	}
}

func TestAC1TheResolverReadsAnEmptyTemporaryDirectoryTheLegMakesAndRemoves(t *testing.T) {
	for _, tc := range []struct {
		name string
		took map[string][]time.Duration
		fail bool
	}{
		{name: "a run that passes"},
		{name: "a run that fails", took: map[string][]time.Duration{dotGlob: ms(5000, 5000, 5000, 5000, 5000)}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newLeg(t)
			if tc.took != nil {
				l.took = tc.took
			}
			elsewhere := t.TempDir()
			checked := 0
			l.during = func(shape string, s shellpolicy.State) {
				made := entries(t, l.base)
				if len(made) != 1 || !made[0].IsDir() {
					t.Errorf("%s: the temporary directory holds %d entries while a shape is timed, want the one directory the leg made", shape, len(made))
					return
				}
				dir := filepath.Join(l.base, made[0].Name())
				if inside := entries(t, dir); len(inside) != 0 {
					t.Errorf("%s: the directory the leg made holds %d entries, want none", shape, len(inside))
				}
				if s.Resolve == nil {
					t.Errorf("%s: State.Resolve is not set", shape)
					return
				}
				if got := s.Resolve(filepath.Join(dir, "a", "b")); got != "a/b" {
					t.Errorf("%s: Resolve of a/b under the directory the leg made = %q, want %q", shape, got, "a/b")
				}
				if got := s.Resolve(filepath.Join(elsewhere, "a", "b")); got != "" {
					t.Errorf("%s: Resolve of a path outside the directory the leg made = %q, want it empty", shape, got)
				}
				checked++
			}
			_, _, code := l.run(budgets(3000, 1000, 1000)...)
			if failed := code != 0; failed != tc.fail {
				t.Errorf("exit %d", code)
			}
			if checked == 0 {
				t.Error("no call of Inspect was made with the directory in place")
			}
			if left := entries(t, l.base); len(left) != 0 {
				t.Errorf("the leg left %d entries in the temporary directory, the first %s", len(left), left[0].Name())
			}
		})
	}
}

func TestAC1TheLegFailsWithoutTimingAnythingWhenItCannotMakeItsDirectory(t *testing.T) {
	l := newLeg(t)
	pointTempAt(t, filepath.Join(l.base, "not", "there"))
	stdout, stderr, code := l.run(budgets(3000, 1000, 1000)...)
	if code == 0 {
		t.Errorf("exit 0 with no directory to resolve against; stdout:\n%s", stdout)
	}
	if strings.TrimSpace(stderr) == "" {
		t.Error("nothing on stderr says why the leg did not measure")
	}
	if l.total != 0 {
		t.Errorf("Inspect was called %d times with no directory made", l.total)
	}
	if lines := nonEmptyLines(stdout); len(lines) != 0 {
		t.Errorf("a figure was printed for a shape that was not timed:\n%s", stdout)
	}
}

// What internal/hook builds for the implementer's command on a story whose
// tests are frozen in a project that does not allow new test files
// (inspectShell and frozenTestsIn). Field by field, because the leg holds a
// second copy of that wiring and nothing else says when the two part.
func TestAC1TheStateIsTheOneTheHookBuildsForAnImplementerUnderAFreeze(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := state(dir, false)

	t.Run("AC-1 every field of State is set, or left unset, as the hook has it", func(t *testing.T) {
		set := []string{
			"Frozen", "NewTest", "ImplementerTest", "Agent", "Resolve", "Backlog",
			"Trunk", "IsTrunk", "Fresh", "Commits", "Nested", "Carried", "Checkouts",
			"Root", "Outside",
			// US-108 AC-7: the person's Claude Code configuration, asked in
			// every session in a participating project.
			"ClaudeConfig", "ClaudeConfigHeld",
			// US-023 AC-11: the person's ~/.agents, asked the same way.
			"AgentsConfig", "AgentsConfigHeld",
		}
		for _, name := range []string{"AgentsConfig", "AgentsConfigHeld"} {
			if _, ok := reflect.TypeOf(s).FieldByName(name); !ok {
				t.Errorf("US-023 AC-11 shellpolicy.State has no field %s for the person's ~/.agents", name)
			}
		}
		// IsTest stands in only for a freeze that cannot be read, Shared is set
		// only where the repository has other checkouts, and Dir is empty for a
		// session at the project root.
		unset := []string{"CommitReady", "StoryFinished", "Committed", "IsTest", "Dir", "Shared"}
		// CommitWhy is prose, and PowerShell is the caller's to say.
		either := []string{"CommitWhy", "PowerShell"}

		v := reflect.ValueOf(s)
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			switch {
			case slices.Contains(set, f.Name):
				if v.Field(i).IsZero() {
					t.Errorf("State.%s is not set; the hook sets it", f.Name)
				}
			case slices.Contains(unset, f.Name):
				if !v.Field(i).IsZero() {
					t.Errorf("State.%s is set; the hook leaves it unset for this session", f.Name)
				}
			case slices.Contains(either, f.Name):
			default:
				t.Errorf("shellpolicy.State has a field %s this test does not know. Read how "+
					"internal/hook sets it in inspectShell, wire it the same way in the leg's state, "+
					"and add it to one of the lists here", f.Name)
			}
		}
	})

	t.Run("AC-1 the role is the implementer, the one role both matchers are set for", func(t *testing.T) {
		if s.Agent != "implementer" {
			t.Errorf("State.Agent = %q, want %q", s.Agent, "implementer")
		}
	})

	t.Run("AC-1 the matchers tell a frozen test, a new test and a source file apart", func(t *testing.T) {
		if s.NewTest == nil || s.ImplementerTest == nil {
			t.Fatal("a matcher is not set")
		}
		for _, frozen := range s.Frozen {
			if s.NewTest(frozen) {
				t.Errorf("NewTest calls the frozen %s a new test file", frozen)
			}
			if !s.ImplementerTest(frozen) {
				t.Errorf("ImplementerTest does not call the frozen %s a test file", frozen)
			}
		}
		if p := "internal/widget/added_after_the_freeze_test.go"; !s.NewTest(p) || !s.ImplementerTest(p) {
			t.Errorf("%s: NewTest and ImplementerTest should both say yes", p)
		}
		if p := "internal/widget/widget.go"; s.NewTest(p) || s.ImplementerTest(p) {
			t.Errorf("%s: NewTest and ImplementerTest should both say no", p)
		}
	})

	t.Run("AC-1 the resolver places a word under the directory it was given", func(t *testing.T) {
		if s.Resolve == nil {
			t.Fatal("State.Resolve is not set")
		}
		for word, want := range map[string]string{
			"a/b":                                 "a/b",
			filepath.Join(dir, "a", "b"):          "a/b",
			filepath.Join(t.TempDir(), "a", "b"):  "",
			filepath.Join(filepath.Dir(dir), "x"): "",
		} {
			if got := s.Resolve(word); got != want {
				t.Errorf("Resolve(%q) = %q, want %q", word, got, want)
			}
		}
	})

	t.Run("AC-1 the resolver reads the disk, as the hook's does", func(t *testing.T) {
		linked, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(linked, "real"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("real", filepath.Join(linked, "link")); err != nil {
			t.Skipf("this platform will not make a symbolic link here: %v", err)
		}
		if got := state(linked, false).Resolve("link/file"); got != "real/file" {
			t.Errorf("Resolve of a word through a link = %q, want %q: a resolver that does not "+
				"read the disk costs the perl shape half its time", got, "real/file")
		}
	})

	t.Run("AC-1 the questions the hook answers can be asked", func(t *testing.T) {
		if s.Trunk == nil || s.IsTrunk == nil || s.Fresh == nil || s.Commits == nil ||
			s.Nested == nil || s.Carried == nil || s.Checkouts == nil {
			t.Fatal("a function the hook sets is not set")
		}
		s.Trunk()
		s.IsTrunk("main")
		s.Fresh()
		s.Commits()
		s.Nested("a")
		s.Carried("PATH")
		s.Checkouts()
	})

	t.Run("AC-1 the here-string shape is read as PowerShell", func(t *testing.T) {
		if s.PowerShell {
			t.Error("state(dir, false) is PowerShell's")
		}
		if !state(dir, true).PowerShell {
			t.Error("state(dir, true) is not PowerShell's")
		}
	})
}

// run takes its Inspect and its clock from main, and every test above hands it
// its own. What main hands it is read from the source, since running main costs
// the fifteen real calls.
func TestAC1TheProgramTimesShellpolicyInspectOnTheRealClockInProcess(t *testing.T) {
	t.Run("AC-1 main hands run shellpolicy.Inspect and time.Now", func(t *testing.T) {
		sources, err := filepath.Glob("*.go")
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		var body *ast.BlockStmt
		for _, name := range sources {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range file.Decls {
				if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "main" {
					body = fn.Body
				}
			}
		}
		if body == nil {
			t.Fatal("no func main in this directory: the program does not exist")
		}
		selector := func(e ast.Expr, pkg, name string) bool {
			sel, ok := e.(*ast.SelectorExpr)
			if !ok {
				return false
			}
			x, ok := sel.X.(*ast.Ident)
			return ok && x.Name == pkg && sel.Sel.Name == name
		}
		found := false
		ast.Inspect(body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if fn, ok := call.Fun.(*ast.Ident); !ok || fn.Name != "run" {
				return true
			}
			inspect := slices.ContainsFunc(call.Args, func(e ast.Expr) bool { return selector(e, "shellpolicy", "Inspect") })
			clock := slices.ContainsFunc(call.Args, func(e ast.Expr) bool { return selector(e, "time", "Now") })
			found = found || (inspect && clock)
			return true
		})
		if !found {
			t.Error("main does not call run with shellpolicy.Inspect and time.Now")
		}
	})

	deps := func(t *testing.T, pkg string) []string {
		t.Helper()
		out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", pkg).Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", pkg, err)
		}
		return strings.Fields(string(out))
	}
	t.Run("AC-1 the program links the classifier, the matchers and the resolver", func(t *testing.T) {
		list := deps(t, ".")
		for _, want := range []string{"internal/shellpolicy", "internal/testset", "internal/pathrules"} {
			if !slices.ContainsFunc(list, func(p string) bool { return strings.HasSuffix(p, "/"+want) }) {
				t.Errorf("the program does not import %s", want)
			}
		}
	})
	t.Run("AC-1 the released binary does not link the program", func(t *testing.T) {
		// Beside the test above, which fails while there is no program: on its
		// own this one would pass over a directory with nothing in it.
		if list := deps(t, "."); len(list) < 2 {
			t.Fatalf("the program has no dependencies to speak of: %v", list)
		}
		for _, pkg := range deps(t, "../../../cmd/sdlc") {
			if strings.Contains(pkg, "hooktime") {
				t.Errorf("the binary links %s", pkg)
			}
		}
	})
}

// ---- AC-2: what a passing run prints ----

func TestAC2OneLineForEachShapeGivesItsNameItsFastestOfFiveRunsAndItsBudget(t *testing.T) {
	// The fastest run is first, in the middle and last, and never the median;
	// the two cases share no figure, so a leg that printed the same line twice
	// fails one of them.
	for _, tc := range []struct {
		name    string
		budgets []int
		took    map[string][]time.Duration
		fastest map[string]float64
	}{
		{
			name:    "the story's budgets",
			budgets: []int{3000, 1000, 1000},
			took: map[string][]time.Duration{
				perl:       ms(1049, 1093, 1062, 1077, 1055),
				hereString: ms(401, 284, 277, 283, 290),
				dotGlob:    ms(408, 372, 361, 399, 352),
			},
			fastest: map[string]float64{perl: 1049, hereString: 277, dotGlob: 352},
		},
		{
			name:    "other budgets and other timings",
			budgets: []int{2999, 998, 997},
			took: map[string][]time.Duration{
				perl:       ms(2100, 2050, 2998, 2001, 1234),
				hereString: ms(321, 900, 800, 700, 600),
				dotGlob:    ms(950, 940, 456, 930, 920),
			},
			fastest: map[string]float64{perl: 1234, hereString: 321, dotGlob: 456},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newLeg(t)
			l.took = tc.took
			stdout, stderr, code := l.run(budgets(tc.budgets[0], tc.budgets[1], tc.budgets[2])...)
			if code != 0 {
				t.Fatalf("exit %d with every fastest run inside its budget; stderr:\n%s", code, stderr)
			}
			lines := nonEmptyLines(stdout)
			if len(lines) != 3 {
				t.Fatalf("%d lines on stdout, want one for each of three shapes:\n%s", len(lines), stdout)
			}
			for i, shape := range shapeNames {
				var mine []string
				for _, line := range lines {
					if strings.Contains(line, shape) {
						mine = append(mine, line)
					}
				}
				if len(mine) != 1 {
					t.Errorf("%d lines name %s, want one:\n%s", len(mine), shape, stdout)
					continue
				}
				for _, other := range shapeNames {
					if other != shape && strings.Contains(mine[0], other) {
						t.Errorf("the line for %s also names %s: %q", shape, other, mine[0])
					}
				}
				got := figures(mine[0])
				if !slices.Contains(got, tc.fastest[shape]) {
					t.Errorf("the line for %s does not give its fastest run, %v ms: %q", shape, tc.fastest[shape], mine[0])
				}
				if !slices.Contains(got, float64(tc.budgets[i])) {
					t.Errorf("the line for %s does not give its budget, %d ms: %q", shape, tc.budgets[i], mine[0])
				}
			}
		})
	}
}

// ---- AC-3: when the leg fails, and what it says ----

func TestAC3AShapeWhoseFastestRunIsOverItsBudgetFailsTheLegAndSaysWhatToDo(t *testing.T) {
	for _, tc := range []struct {
		shape   string
		took    []time.Duration
		fastest float64
		budget  float64
	}{
		{perl, ms(3999, 3456, 3789, 4100, 3500), 3456, 3000},
		{hereString, ms(1234, 1300, 1400, 1500, 1600), 1234, 1000},
		{dotGlob, ms(2517, 2400, 2300, 2200, 1609), 1609, 1000},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			l := newLeg(t)
			l.took[tc.shape] = tc.took
			_, stderr, code := l.run(budgets(3000, 1000, 1000)...)
			if code == 0 {
				t.Fatalf("exit 0 with the fastest %s run at %v ms against a budget of %v ms", tc.shape, tc.fastest, tc.budget)
			}
			said := flat(stderr)
			if !strings.Contains(said, tc.shape) {
				t.Errorf("stderr does not name the shape %s:\n%s", tc.shape, stderr)
			}
			got := figures(said)
			if !slices.Contains(got, tc.fastest) {
				t.Errorf("stderr does not say the fastest run took %v ms:\n%s", tc.fastest, stderr)
			}
			if !slices.Contains(got, tc.budget) {
				t.Errorf("stderr does not give the budget, %v ms:\n%s", tc.budget, stderr)
			}
			// The next step begins with running it again on a quiet machine:
			// the same tree goes over when the machine is busy.
			again := strings.Index(said, "./task hook-time")
			quiet := strings.Index(said, "quiet")
			if again < 0 || quiet < 0 {
				t.Fatalf("stderr does not say to run ./task hook-time again on a quiet machine:\n%s", stderr)
			}
			for _, later := range []string{"Taskfile.yml", "raise", "internal/shellpolicy"} {
				if at := strings.Index(said, later); at >= 0 && (at < again || at < quiet) {
					t.Errorf("stderr mentions %q before it says to run the leg again on a quiet machine:\n%s", later, stderr)
				}
			}
		})
	}
}

func TestAC3ARunExactlyAtItsBudgetPassesAndOneHalfAMillisecondOverFails(t *testing.T) {
	for _, shape := range shapeNames {
		budget := map[string]float64{perl: 3000, hereString: 1000, dotGlob: 1000}[shape]
		t.Run(shape+" at its budget", func(t *testing.T) {
			l := newLeg(t)
			l.took[shape] = ms(budget+50, budget, budget+10, budget+20, budget+30)
			if _, stderr, code := l.run(budgets(3000, 1000, 1000)...); code != 0 {
				t.Errorf("exit %d with the fastest run at the budget, which is not over it; stderr:\n%s", code, stderr)
			}
		})
		t.Run(shape+" half a millisecond over", func(t *testing.T) {
			l := newLeg(t)
			l.took[shape] = ms(budget+50, budget+0.5, budget+10, budget+20, budget+30)
			_, stderr, code := l.run(budgets(3000, 1000, 1000)...)
			if code == 0 {
				t.Error("exit 0 with the fastest run half a millisecond over the budget")
			}
			if !strings.Contains(stderr, shape) {
				t.Errorf("stderr does not name the shape %s:\n%s", shape, stderr)
			}
		})
	}
}

func TestAC3FourRunsOverBudgetDoNotFailAShapeWhoseFifthIsInsideIt(t *testing.T) {
	for at := range 5 {
		l := newLeg(t)
		for _, shape := range shapeNames {
			took := ms(9000, 9000, 9000, 9000, 9000)
			took[at] = 900 * time.Millisecond
			l.took[shape] = took
		}
		stdout, stderr, code := l.run(budgets(3000, 1000, 1000)...)
		if code != 0 {
			t.Errorf("exit %d with run %d of five inside the budget; the leg takes the fastest. stderr:\n%s", code, at+1, stderr)
		}
		for _, line := range nonEmptyLines(stdout) {
			if !slices.Contains(figures(line), 900) {
				t.Errorf("with run %d the fastest at 900 ms, a line gives another figure: %q", at+1, line)
			}
		}
	}
}

func TestAC3AnAnswerThatIsNotTodaysFailsTheLegHoweverFastTheShapeWas(t *testing.T) {
	for _, tc := range []struct {
		shape string
		now   string
	}{
		{perl, "refused"},
		{hereString, "refused"},
		{dotGlob, "allowed"},
	} {
		t.Run(tc.shape+" is "+tc.now, func(t *testing.T) {
			l := newLeg(t)
			l.refuses = func(shape string, _ int) bool {
				if shape == tc.shape {
					return !refusedToday[shape]
				}
				return refusedToday[shape]
			}
			_, stderr, code := l.run(budgets(3000, 1000, 1000)...)
			if code == 0 {
				t.Fatalf("exit 0 with the %s shape %s and every run a millisecond long", tc.shape, tc.now)
			}
			said := flat(stderr)
			if !strings.Contains(said, tc.shape) {
				t.Errorf("stderr does not name the shape %s:\n%s", tc.shape, stderr)
			}
			if !strings.Contains(said, tc.now) {
				t.Errorf("stderr does not say the shape is now %s:\n%s", tc.now, stderr)
			}
			// What to do next: the classifier is where the answer changed, and
			// a quiet machine gives the same answer as a busy one.
			if !strings.Contains(said, "internal/shellpolicy") {
				t.Errorf("stderr does not send the reader to internal/shellpolicy:\n%s", stderr)
			}
			if strings.Contains(said, "quiet") {
				t.Errorf("stderr sends the reader to a quiet machine for an answer that does not depend on one:\n%s", stderr)
			}
		})
	}
}

// A floor taken from a run that answered something else is not a timing of the
// shape: the fastest way to classify a command is to stop reading it.
func TestAC3AnAnswerThatChangesOnOneRunOfFiveStillFailsTheLeg(t *testing.T) {
	for _, shape := range shapeNames {
		for at := range 5 {
			l := newLeg(t)
			took := ms(300, 300, 300, 300, 300)
			took[at] = time.Millisecond
			l.took[shape] = took
			l.refuses = func(s string, call int) bool {
				if s == shape && call == at {
					return !refusedToday[s]
				}
				return refusedToday[s]
			}
			_, stderr, code := l.run(budgets(3000, 1000, 1000)...)
			if code == 0 {
				t.Errorf("exit 0 with run %d of the %s shape answered differently", at+1, shape)
			}
			if !strings.Contains(stderr, shape) {
				t.Errorf("run %d: stderr does not name the shape %s:\n%s", at+1, shape, stderr)
			}
		}
	}
}

// init-time once passed on every Windows run because nothing had been measured
// and nothing compared as 0 ms. A budget the leg cannot read is not a budget of
// zero, and not one of infinity.
func TestAC3ABudgetThatIsMissingOrIsNotAWholeNumberAboveZeroFailsBeforeAnythingIsTimed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		names string
	}{
		{"no budget at all", nil, ""},
		{"no perl budget", []string{"-here-string=1000", "-dot-glob=1000"}, perl},
		{"no here-string budget", []string{"-perl=3000", "-dot-glob=1000"}, hereString},
		{"no dot-glob budget", []string{"-perl=3000", "-here-string=1000"}, dotGlob},
		{"a budget of zero", []string{"-perl=0", "-here-string=1000", "-dot-glob=1000"}, perl},
		{"a budget below zero", []string{"-perl=3000", "-here-string=-1000", "-dot-glob=1000"}, hereString},
		{"a budget that is empty", []string{"-perl=3000", "-here-string=1000", "-dot-glob="}, dotGlob},
		{"a budget that is a word", []string{"-perl=3000", "-here-string=1000", "-dot-glob=fast"}, dotGlob},
		{"a budget an unset variable left as its template", []string{"-perl=<no value>", "-here-string=1000", "-dot-glob=1000"}, perl},
		{"a word after the budgets", append(budgets(3000, 1000, 1000), "extra"), ""},
		{"a flag it does not take", append(budgets(3000, 1000, 1000), "-median"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newLeg(t)
			stdout, stderr, code := l.run(tc.args...)
			if code == 0 {
				t.Errorf("exit 0; stdout:\n%s", stdout)
			}
			if strings.TrimSpace(stderr) == "" {
				t.Error("nothing on stderr says what the leg takes")
			}
			if tc.names != "" && !strings.Contains(stderr, tc.names) {
				t.Errorf("stderr does not name the budget that is wrong, %s:\n%s", tc.names, stderr)
			}
			if l.total != 0 {
				t.Errorf("Inspect was called %d times before the budgets were read", l.total)
			}
			if lines := nonEmptyLines(stdout); len(lines) != 0 {
				t.Errorf("a figure was printed for a shape that was not timed:\n%s", stdout)
			}
			if left := entries(t, l.base); len(left) != 0 {
				t.Errorf("the leg left %d entries in the temporary directory", len(left))
			}
		})
	}
}

// ---- AC-6: the commands themselves ----

func TestAC6EachTimedShapeIsTheCommandAC1DescribesAtTheLengthAC1Gives(t *testing.T) {
	t.Run("AC-6 the commands this test expects are the longest whole ones not over 8,192 bytes", func(t *testing.T) {
		// One more unit of each shape: a letter, a block, a segment.
		step := map[string]int{perl: 1, hereString: len("$s = @'\nx\n'@\n"), dotGlob: len("a/")}
		for _, shape := range shapeNames {
			n := len(described[shape])
			if n != describedLength[shape] {
				t.Fatalf("this test's own %s command is %d bytes, and AC-1 says %d", shape, n, describedLength[shape])
			}
			if n > 8192 || n+step[shape] <= 8192 {
				t.Fatalf("this test's own %s command is %d bytes, which is not the longest whole one within 8,192", shape, n)
			}
		}
	})

	l := newLeg(t)
	if _, stderr, code := l.run(budgets(3000, 1000, 1000)...); code != 0 {
		t.Fatalf("exit %d with every run inside its budget; stderr:\n%s", code, stderr)
	}
	if n := len(l.commands[unknown]); n != 0 {
		t.Errorf("%d timed commands are of none of the three shapes; the first begins %q", n, head(l.commands[unknown][0]))
	}
	for _, shape := range shapeNames {
		t.Run("AC-6 "+shape, func(t *testing.T) {
			timed := l.commands[shape]
			if len(timed) != 5 {
				t.Errorf("timed %d times, want 5", len(timed))
			}
			want := described[shape]
			for i, got := range timed {
				if len(got) != describedLength[shape] {
					t.Errorf("run %d: the command is %d bytes, and AC-1 says %d", i+1, len(got), describedLength[shape])
				}
				if got == want {
					continue
				}
				at := 0
				for at < len(got) && at < len(want) && got[at] == want[at] {
					at++
				}
				t.Errorf("run %d: the command is not the one AC-1 describes. It parts from it at byte %d: timed %q, described %q",
					i+1, at, head(got[at:]), head(want[at:]))
			}
		})
	}
}

// head is enough of a command to recognise, not 8 KB of it.
func head(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}
