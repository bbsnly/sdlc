package main

// US-038, the program: `./task hook-time` runs it, and it times
// shellpolicy.Inspect on commands that are slow by construction. US-035 adds
// two shapes to the three US-038 named, each held to the budget of the perl
// shape:
//
//   - AC-10 (from US-103): `touch`, `cp`, `sed` or `tee` and one single-dash
//     word of letters and a file name, at 8,192 bytes, timed as one shape for
//     each of the four tools, so that a fix keyed on one tool's name does not
//     pass for the others. Allowed, as today.
//   - AC-6 (from US-052): a command of many words, each through a chain of
//     255 links of its own, made in the leg's own temporary directory. Refused,
//     since each word goes through more than 64 links (AC-5). Where the leg
//     cannot make a link (Windows without the privilege), that shape is
//     skipped and the leg says so and why, and the others are still timed and
//     judged (decided D5).
//
// Run through the real Inspect under the race detector, the calls take
// minutes, so nothing here does that. The tests hand the program an Inspect
// and a clock of their own, and these are what it has to offer:
//
//	func run(args []string, stdout, stderr io.Writer,
//		inspect func(string, shellpolicy.State) (shellpolicy.Finding, bool),
//		now func() time.Time) int
//
//	func state(dir string, powerShell bool) shellpolicy.State
//
//	var symlink = os.Symlink
//	    every link the leg makes is made through it, so that a test can
//	    stand for a platform that will not make one
//
//	run({"-perl=3000", "-here-string=1000", "-dot-glob=1000"}, ...)
//	    each budget a whole number of milliseconds above zero. It makes its
//	    directories with os.MkdirTemp("", ...), hands each shape's command to
//	    inspect five times with state(the directory, ...), reads now() before
//	    and after each call, and removes what it made on every way out.
//	exit 0      one line on stdout for each shape timed, holding its name, its
//	            fastest run and its budget in milliseconds, each written as
//	            digits with no thousands separator; and, for a shape skipped, a
//	            line that names it, says it was skipped, and gives the reason
//	exit not 0  a budget that is missing or is not such a number, a directory
//	            that could not be made, a fastest run over its budget, or an
//	            answer that is not the expected one on any one of the five
//	            calls. Stderr says which and what to do.
//
// main is run(os.Args[1:], os.Stdout, os.Stderr, shellpolicy.Inspect, time.Now)
// and nothing more.

import (
	"bytes"
	"errors"
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

	"github.com/bbsnly/sdlc/internal/pathrules"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// The names the program prints. The first three are the names AC-4 of US-038
// gives the budgets; the rest are US-035's.
const (
	perl       = "perl"
	hereString = "here-string"
	dotGlob    = "dot-glob"
	sdTouch    = "single-dash-touch"
	sdCp       = "single-dash-cp"
	sdSed      = "single-dash-sed"
	sdTee      = "single-dash-tee"
	links      = "links"
	unknown    = "a command of none of the shapes"
)

// shapeNames are every shape; withoutLinks the ones that need no link made.
var (
	shapeNames   = []string{perl, hereString, dotGlob, sdTouch, sdCp, sdSed, sdTee, links}
	withoutLinks = []string{perl, hereString, dotGlob, sdTouch, sdCp, sdSed, sdTee}
	singleDash   = []string{sdTouch, sdCp, sdSed, sdTee}
)

// singleDashCommand is tool, one single-dash word of the letter a, and the file
// name `file`, 8,192 bytes in all (AC-10).
func singleDashCommand(tool string) string {
	return tool + " -" + strings.Repeat("a", 8192-len(tool)-len(" -")-len(" file")) + " file"
}

// The commands, written out from AC-1 of US-038 and AC-10 of US-035 and from
// nothing in the program: a test that asked the program's own generator would
// agree with whatever it made (THREATS T12 of US-038). The links shape is held
// to its description by what it is on disk, in
// TestAC6TheLinksShapeIsManyWordsEachThroughAChainOf255LinksOfItsOwn.
var described = map[string]string{
	// `perl`, one word that is a dash and then only the letter a, the file
	// name `file`.
	perl: "perl -" + strings.Repeat("a", 8181) + " file",
	// 630 blocks of the opener, the one-line body x and the closer.
	hereString: strings.Repeat("$s = @'\nx\n'@\n", 630),
	// One path of 4,093 `a/` segments that ends in dot-star.
	dotGlob: "rm " + strings.Repeat("a/", 4093) + ".*",
	sdTouch: "touch -" + strings.Repeat("a", 8180) + " file",
	sdCp:    "cp -" + strings.Repeat("a", 8183) + " file",
	sdSed:   "sed -" + strings.Repeat("a", 8182) + " file",
	sdTee:   "tee -" + strings.Repeat("a", 8182) + " file",
}

// The lengths AC-1 and AC-10 give, as figures, so that a slip in the table
// above is caught by something other than itself.
var describedLength = map[string]int{
	perl: 8192, hereString: 8190, dotGlob: 8191,
	sdTouch: 8192, sdCp: 8192, sdSed: 8192, sdTee: 8192,
}

// The answer the leg expects of Inspect: the removal refused, and the words
// through 255 links each refused for going through more than 64 (AC-5); the
// rest allowed, as today.
var refusedToday = map[string]bool{
	perl: false, hereString: false, dotGlob: true,
	sdTouch: false, sdCp: false, sdSed: false, sdTee: false,
	links: true,
}

// budgetFlag is the budget each shape is held to: its own, or the perl shape's
// (AC-10, and AC-6 within the leg's budget).
var budgetFlag = map[string]string{
	perl: perl, hereString: hereString, dotGlob: dotGlob,
	sdTouch: perl, sdCp: perl, sdSed: perl, sdTee: perl, links: perl,
}

func shapeOf(command string) string {
	switch {
	case strings.HasPrefix(command, "perl"):
		return perl
	case strings.HasPrefix(command, "$s"):
		return hereString
	case strings.HasPrefix(command, "rm"):
		return dotGlob
	case strings.HasPrefix(command, "touch -"):
		return sdTouch
	case strings.HasPrefix(command, "cp -"):
		return sdCp
	case strings.HasPrefix(command, "sed -"):
		return sdSed
	case strings.HasPrefix(command, "tee -"):
		return sdTee
	case strings.HasPrefix(command, "touch "):
		return links
	}
	return unknown
}

// errNoLinkHere stands for a platform that will not make a link: Windows
// without Developer Mode or an administrator.
var errNoLinkHere = errors.New("zq7: a required privilege is not held by the client")

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
	// refuses, when set, is the answer for one call; the expected one
	// otherwise.
	refuses func(shape string, call int) bool
	// during, when set, runs inside every call.
	during func(shape string, command string, s shellpolicy.State)

	commands map[string][]string
	states   map[string][]shellpolicy.State
	total    int
}

// newLeg points the temporary directory at one of the test's own, so that the
// directories the program makes can be seen, and nothing is left in the real
// one. No link can be made: the links shape is skipped, which is what most
// tests want, since it is tens of thousands of links for each run of the leg.
// newLegWithLinks makes them.
func newLeg(t *testing.T) *leg {
	t.Helper()
	noLinks(t)
	return legIn(t)
}

// newLegWithLinks is newLeg on a platform that makes links, or a skip.
func newLegWithLinks(t *testing.T) *leg {
	t.Helper()
	probe := t.TempDir()
	if err := os.Symlink("target", filepath.Join(probe, "link")); err != nil {
		t.Skipf("this platform will not make a link here, so the leg skips the links shape: %v", err)
	}
	return legIn(t)
}

// legFor builds the leg with links or without. Choosing one constructor matters:
// newLeg has symlink fail until the test ends, so a newLegWithLinks after it
// made no link, and three links subtests timed nothing while they passed.
func legFor(t *testing.T, withLinks bool) *leg {
	t.Helper()
	if withLinks {
		return newLegWithLinks(t)
	}
	return newLeg(t)
}

// noLinks has the leg's symlink fail, as Windows does without the privilege.
func noLinks(t *testing.T) {
	t.Helper()
	before := symlink
	symlink = func(string, string) error { return errNoLinkHere }
	t.Cleanup(func() { symlink = before })
}

func legIn(t *testing.T) *leg {
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
		during(shape, command, s)
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

// budgetFor is the budget shape is held to, of the three given.
func budgetFor(shape string, perlMS, hereStringMS, dotGlobMS int) int {
	return map[string]int{perl: perlMS, hereString: hereStringMS, dotGlob: dotGlobMS}[budgetFlag[shape]]
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

// names reports whether line names shape: as a word, and not as a part of
// another shape's name.
func names(line, shape string) bool {
	return regexp.MustCompile(`(^|[^a-z-])` + regexp.QuoteMeta(shape) + `($|[^a-z-])`).MatchString(line)
}

// skipLines are the lines of out that say the links shape was skipped.
func skipLines(out string) []string {
	var found []string
	for _, line := range nonEmptyLines(out) {
		if names(line, links) && strings.Contains(strings.ToLower(line), "skip") {
			found = append(found, line)
		}
	}
	return found
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
	for _, tc := range []struct {
		name   string
		newLeg func(*testing.T) *leg
		shapes []string
	}{
		{"where no link can be made", newLeg, withoutLinks},
		{"where links can be made", newLegWithLinks, shapeNames},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := tc.newLeg(t)
			_, stderr, code := l.run(budgets(3000, 1000, 1000)...)
			if code != 0 {
				t.Fatalf("exit %d with every run inside its budget; stderr:\n%s", code, stderr)
			}
			if want := 5 * len(tc.shapes); l.total != want {
				t.Errorf("Inspect was called %d times, want %d: five for each of %d shapes", l.total, want, len(tc.shapes))
			}
			if n := len(l.commands[unknown]); n != 0 {
				t.Errorf("%d calls were for a command of none of the shapes", n)
			}
			const newTestFile = "internal/widget/added_after_the_freeze_test.go"
			for _, shape := range tc.shapes {
				if n := len(l.states[shape]); n != 5 {
					t.Errorf("%s: timed %d times, want 5", shape, n)
				}
				for i, s := range l.states[shape] {
					// Without the resolver the perl shape takes about 600 ms
					// and not 1,100, and a fourfold slowdown passes its budget.
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
		})
	}
}

// The directory a shape is resolved against is one the leg made in the
// temporary directory, empty for every shape but the links one, and nothing
// the leg made is left behind.
func TestAC1TheResolverReadsATemporaryDirectoryTheLegMakesAndRemoves(t *testing.T) {
	for _, tc := range []struct {
		name  string
		links bool
		took  map[string][]time.Duration
		fail  bool
	}{
		{name: "a run that passes"},
		{name: "a run that fails", took: map[string][]time.Duration{dotGlob: ms(5000, 5000, 5000, 5000, 5000)}, fail: true},
		{name: "a run that passes, with links", links: true},
		{name: "a run that fails, with links", links: true, took: map[string][]time.Duration{links: ms(5000, 5000, 5000, 5000, 5000)}, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := legFor(t, tc.links)
			if tc.took != nil {
				l.took = tc.took
			}
			elsewhere := t.TempDir()
			checked := 0
			l.during = func(shape, _ string, s shellpolicy.State) {
				dir := filepath.FromSlash(s.Root)
				if filepath.Dir(dir) != l.base {
					t.Errorf("%s: State.Root is %s, which is not a directory the leg made in the temporary directory %s", shape, s.Root, l.base)
					return
				}
				if inside := entries(t, dir); shape != links && len(inside) != 0 {
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
	for _, line := range nonEmptyLines(stdout) {
		if len(figures(line)) > 0 {
			t.Errorf("a figure was printed for a shape that was not timed: %q", line)
		}
	}
}

// hookState is what internal/hook's inspectShell does with each field of the
// State it builds: the fields of its composite literal, and the fields it
// assigns afterwards, which it sets only where something holds.
func hookState(t *testing.T) (literal, assigned map[string]bool) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join("..", "..", "hook", "hook.go"), nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var body *ast.BlockStmt
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "inspectShell" {
			body = fn.Body
		}
	}
	if body == nil {
		t.Fatal("internal/hook/hook.go has no func inspectShell, so this test cannot say what State the hook builds")
	}
	literal, assigned = map[string]bool{}, map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CompositeLit:
			if sel, ok := n.Type.(*ast.SelectorExpr); ok && sel.Sel.Name == "State" {
				for _, e := range n.Elts {
					if kv, ok := e.(*ast.KeyValueExpr); ok {
						if key, ok := kv.Key.(*ast.Ident); ok {
							literal[key.Name] = true
						}
					}
				}
			}
		case *ast.AssignStmt:
			for _, lhs := range n.Lhs {
				if sel, ok := lhs.(*ast.SelectorExpr); ok {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "state" {
						assigned[sel.Sel.Name] = true
					}
				}
			}
		}
		return true
	})
	if len(literal) == 0 {
		t.Fatal("inspectShell builds no shellpolicy.State literal this test can read")
	}
	return literal, assigned
}

// What internal/hook builds for the implementer's command on a story whose
// tests are frozen in a project that does not allow new test files
// (inspectShell and frozenTestsIn). Field by field, because the leg holds a
// second copy of that wiring and nothing else says when the two part. A field
// added to State after these lists were written is held to what the hook does
// with it, read from the hook's source: set in the hook's literal, set here;
// set by the hook only where something holds, either; never set by the hook,
// never here.
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
		literal, assigned := hookState(t)

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
			case slices.Contains(either, f.Name), assigned[f.Name] && !literal[f.Name]:
			case literal[f.Name]:
				if v.Field(i).IsZero() {
					t.Errorf("State.%s is not set; internal/hook sets it in inspectShell's State, and the leg "+
						"times the classifier the hook runs, so it sets it the same way", f.Name)
				}
			default:
				if !v.Field(i).IsZero() {
					t.Errorf("State.%s is set; internal/hook never sets it in inspectShell, so the leg "+
						"would time a classifier the hook does not run", f.Name)
				}
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

	t.Run("AC-6 the bound on links is the hook's, so the links shape is refused as the hook refuses it", func(t *testing.T) {
		linked, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(linked, "real"), 0o755); err != nil {
			t.Fatal(err)
		}
		for i := range 65 {
			to := "real"
			if i < 64 {
				to = "chain" + strconv.Itoa(i+1)
			}
			if err := os.Symlink(to, filepath.Join(linked, "chain"+strconv.Itoa(i))); err != nil {
				t.Skipf("this platform will not make a symbolic link here: %v", err)
			}
		}
		ls := state(linked, false)
		if ls.Bound == nil {
			t.Fatal("State.Bound is not set")
		}
		if err := ls.Bound("notes.txt"); err != nil {
			t.Errorf("Bound(notes.txt) = %v, want nil", err)
		}
		if err := ls.Bound("chain0/x"); !errors.Is(err, pathrules.ErrTooManyLinks) {
			t.Errorf("Bound of a word through 65 links = %v, want ErrTooManyLinks", err)
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
// the real calls.
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

// passingLines holds stdout to one line for each shape timed, which names that
// shape and no other, and gives its fastest run and its budget.
func passingLines(t *testing.T, stdout string, timed []string, fastest map[string]float64, budget func(string) int) {
	t.Helper()
	lines := nonEmptyLines(stdout)
	for _, line := range lines {
		named := 0
		for _, shape := range shapeNames {
			if names(line, shape) {
				named++
			}
		}
		if named != 1 {
			t.Errorf("a line on stdout names %d shapes, want one: %q", named, line)
		}
	}
	for _, shape := range timed {
		var mine []string
		for _, line := range lines {
			if names(line, shape) {
				mine = append(mine, line)
			}
		}
		if len(mine) != 1 {
			t.Errorf("%d lines name %s, want one:\n%s", len(mine), shape, stdout)
			continue
		}
		got := figures(mine[0])
		if !slices.Contains(got, fastest[shape]) {
			t.Errorf("the line for %s does not give its fastest run, %v ms: %q", shape, fastest[shape], mine[0])
		}
		if !slices.Contains(got, float64(budget(shape))) {
			t.Errorf("the line for %s does not give its budget, %d ms: %q", shape, budget(shape), mine[0])
		}
	}
}

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
				sdTouch:    ms(1101, 1102, 1103, 1104, 1105),
				sdCp:       ms(1206, 1202, 1203, 1204, 1205),
				sdSed:      ms(1306, 1307, 1308, 1309, 1301),
				sdTee:      ms(1406, 1407, 1401, 1409, 1408),
			},
			fastest: map[string]float64{perl: 1049, hereString: 277, dotGlob: 352, sdTouch: 1101, sdCp: 1202, sdSed: 1301, sdTee: 1401},
		},
		{
			name:    "other budgets and other timings",
			budgets: []int{2999, 998, 997},
			took: map[string][]time.Duration{
				perl:       ms(2100, 2050, 2998, 2001, 1234),
				hereString: ms(321, 900, 800, 700, 600),
				dotGlob:    ms(950, 940, 456, 930, 920),
				sdTouch:    ms(2501, 2502, 2503, 2504, 1511),
				sdCp:       ms(1612, 2502, 2503, 2504, 2505),
				sdSed:      ms(2501, 1713, 2503, 2504, 2505),
				sdTee:      ms(2501, 2502, 2503, 1814, 2505),
			},
			fastest: map[string]float64{perl: 1234, hereString: 321, dotGlob: 456, sdTouch: 1511, sdCp: 1612, sdSed: 1713, sdTee: 1814},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := newLeg(t)
			l.took = tc.took
			stdout, stderr, code := l.run(budgets(tc.budgets[0], tc.budgets[1], tc.budgets[2])...)
			if code != 0 {
				t.Fatalf("exit %d with every fastest run inside its budget; stderr:\n%s", code, stderr)
			}
			passingLines(t, stdout, withoutLinks, tc.fastest, func(shape string) int {
				return budgetFor(shape, tc.budgets[0], tc.budgets[1], tc.budgets[2])
			})
		})
	}
}

// AC-6 and AC-10: the shapes US-035 adds are held to the perl shape's budget,
// and say so: with the perl budget at 2,999 ms, each of their lines gives 2999.
func TestAC6AC10TheNewShapesAreHeldToThePerlBudgetAndPrintIt(t *testing.T) {
	l := newLegWithLinks(t)
	l.took[links] = ms(77, 75, 76, 78, 79)
	stdout, stderr, code := l.run(budgets(2999, 998, 997)...)
	if code != 0 {
		t.Fatalf("exit %d with every fastest run inside its budget; stderr:\n%s", code, stderr)
	}
	fastest := map[string]float64{perl: 1, hereString: 1, dotGlob: 1, sdTouch: 1, sdCp: 1, sdSed: 1, sdTee: 1, links: 75}
	passingLines(t, stdout, shapeNames, fastest, func(shape string) int { return budgetFor(shape, 2999, 998, 997) })
	if skipped := skipLines(stdout + stderr); len(skipped) != 0 {
		t.Errorf("the links shape was timed and also said to be skipped: %q", skipped)
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
		{sdTouch, ms(17500, 17400, 17300, 17200, 17100), 17100, 3000},
		{sdCp, ms(3100, 3200, 3300, 3400, 3500), 3100, 3000},
		{sdSed, ms(4100, 3900, 3800, 3700, 3600), 3600, 3000},
		{sdTee, ms(5000, 4000, 3001.5, 4500, 4600), 3001.5, 3000},
		{links, ms(22800, 22700, 22600, 22500, 22400), 22400, 3000},
	} {
		t.Run(tc.shape, func(t *testing.T) {
			l := legFor(t, tc.shape == links)
			l.took[tc.shape] = tc.took
			_, stderr, code := l.run(budgets(3000, 1000, 1000)...)
			if code == 0 {
				t.Fatalf("exit 0 with the fastest %s run at %v ms against a budget of %v ms", tc.shape, tc.fastest, tc.budget)
			}
			said := flat(stderr)
			if !names(said, tc.shape) {
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
	for _, shape := range withoutLinks {
		budget := float64(budgetFor(shape, 3000, 1000, 1000))
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
			if !names(stderr, shape) {
				t.Errorf("stderr does not name the shape %s:\n%s", shape, stderr)
			}
		})
	}
}

func TestAC3FourRunsOverBudgetDoNotFailAShapeWhoseFifthIsInsideIt(t *testing.T) {
	for at := range 5 {
		l := newLeg(t)
		for _, shape := range withoutLinks {
			took := ms(9000, 9000, 9000, 9000, 9000)
			took[at] = 900 * time.Millisecond
			l.took[shape] = took
		}
		stdout, stderr, code := l.run(budgets(3000, 1000, 1000)...)
		if code != 0 {
			t.Errorf("exit %d with run %d of five inside the budget; the leg takes the fastest. stderr:\n%s", code, at+1, stderr)
		}
		for _, line := range nonEmptyLines(stdout) {
			if names(line, links) {
				continue
			}
			if !slices.Contains(figures(line), 900) {
				t.Errorf("with run %d the fastest at 900 ms, a line gives another figure: %q", at+1, line)
			}
		}
	}
}

func TestAC3AnAnswerThatIsNotTheExpectedOneFailsTheLegHoweverFastTheShapeWas(t *testing.T) {
	for _, shape := range shapeNames {
		now := "allowed"
		if !refusedToday[shape] {
			now = "refused"
		}
		t.Run(shape+" is "+now, func(t *testing.T) {
			l := legFor(t, shape == links)
			l.refuses = func(s string, _ int) bool {
				if s == shape {
					return !refusedToday[s]
				}
				return refusedToday[s]
			}
			_, stderr, code := l.run(budgets(3000, 1000, 1000)...)
			if code == 0 {
				t.Fatalf("exit 0 with the %s shape %s and every run a millisecond long", shape, now)
			}
			said := flat(stderr)
			if !names(said, shape) {
				t.Errorf("stderr does not name the shape %s:\n%s", shape, stderr)
			}
			if !strings.Contains(said, now) {
				t.Errorf("stderr does not say the shape is now %s:\n%s", now, stderr)
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
	for _, shape := range withoutLinks {
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
			if !names(stderr, shape) {
				t.Errorf("run %d: stderr does not name the shape %s:\n%s", at+1, shape, stderr)
			}
		}
	}
}

// init-time once passed on every Windows run because nothing had been measured
// and nothing compared as 0 ms. A budget the leg cannot read is not a budget of
// zero, and not one of infinity. The shapes US-035 adds take no flag of their
// own: they are held to the perl budget.
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
		{"a budget of its own for a shape held to perl's", append(budgets(3000, 1000, 1000), "-links=3000"), ""},
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
				t.Errorf("a line was printed for a shape that was not timed:\n%s", stdout)
			}
			if left := entries(t, l.base); len(left) != 0 {
				t.Errorf("the leg left %d entries in the temporary directory", len(left))
			}
		})
	}
}

// ---- AC-6 of US-038, and AC-10 of US-035: the commands themselves ----

func TestAC6AC10EachTimedShapeIsTheCommandItsCriterionDescribes(t *testing.T) {
	t.Run("AC-6 the commands this test expects are the longest whole ones not over 8,192 bytes", func(t *testing.T) {
		// One more unit of each shape: a letter, a block, a segment.
		step := map[string]int{perl: 1, hereString: len("$s = @'\nx\n'@\n"), dotGlob: len("a/"), sdTouch: 1, sdCp: 1, sdSed: 1, sdTee: 1}
		for _, shape := range withoutLinks {
			n := len(described[shape])
			if n != describedLength[shape] {
				t.Fatalf("this test's own %s command is %d bytes, and its criterion says %d", shape, n, describedLength[shape])
			}
			if n > 8192 || n+step[shape] <= 8192 {
				t.Fatalf("this test's own %s command is %d bytes, which is not the longest whole one within 8,192", shape, n)
			}
		}
		for i, tool := range []string{"touch", "cp", "sed", "tee"} {
			if got := singleDashCommand(tool); got != described[singleDash[i]] {
				t.Fatalf("this test's own %s command does not agree with AC-10's words", singleDash[i])
			}
		}
	})

	l := newLeg(t)
	if _, stderr, code := l.run(budgets(3000, 1000, 1000)...); code != 0 {
		t.Fatalf("exit %d with every run inside its budget; stderr:\n%s", code, stderr)
	}
	if n := len(l.commands[unknown]); n != 0 {
		t.Errorf("%d timed commands are of none of the shapes; the first begins %q", n, head(l.commands[unknown][0]))
	}
	for _, shape := range withoutLinks {
		t.Run("AC-6 "+shape, func(t *testing.T) {
			timed := l.commands[shape]
			if len(timed) != 5 {
				t.Errorf("timed %d times, want 5", len(timed))
			}
			want := described[shape]
			for i, got := range timed {
				if len(got) != describedLength[shape] {
					t.Errorf("run %d: the command is %d bytes, and its criterion says %d", i+1, len(got), describedLength[shape])
				}
				if got == want {
					continue
				}
				at := 0
				for at < len(got) && at < len(want) && got[at] == want[at] {
					at++
				}
				t.Errorf("run %d: the command is not the one its criterion describes. It parts from it at byte %d: timed %q, described %q",
					i+1, at, head(got[at:]), head(want[at:]))
			}
		})
	}
}

// linksCrossed is how many symbolic links the OS follows to place word, read
// from root as the shell would, with every link and the place it was met
// added to seen. It is a walk of its own, in this test, so that the program's
// fixture is held to what is on disk and not to the resolver it times.
func linksCrossed(t *testing.T, root, word string, seen map[string]int, owner int) int {
	t.Helper()
	volume := filepath.VolumeName(root)
	dest := root
	rest := strings.Split(filepath.ToSlash(word), "/")
	crossed := 0
	for len(rest) > 0 && crossed <= 10000 {
		name := rest[0]
		rest = rest[1:]
		switch name {
		case "", ".":
			continue
		case "..":
			dest = filepath.Dir(dest)
			continue
		}
		next := filepath.Join(dest, name)
		info, err := os.Lstat(next)
		if err != nil {
			return crossed // the rest is not there, and holds no link
		}
		if info.Mode()&os.ModeSymlink == 0 {
			dest = next
			continue
		}
		crossed++
		if before, ok := seen[next]; ok && before != owner {
			t.Errorf("word %d goes through %s, a link of word %d's chain: the words share their links", owner+1, next, before+1)
		}
		seen[next] = owner
		target, err := os.Readlink(next)
		if err != nil {
			t.Fatalf("readlink %s: %v", next, err)
		}
		if filepath.IsAbs(target) {
			dest = volume + string(filepath.Separator)
		}
		rest = append(strings.Split(filepath.ToSlash(target), "/"), rest...)
	}
	return crossed
}

// AC-6: the links shape is touch and many words, at least fifty, each through a
// chain of 255 links of its own made in the directory the State resolves
// against, which is the worst a word can be (EvalSymlinks follows 255) and
// which no memo of one word helps another with.
func TestAC6TheLinksShapeIsManyWordsEachThroughAChainOf255LinksOfItsOwn(t *testing.T) {
	l := newLegWithLinks(t)
	checked := 0
	l.during = func(shape, command string, s shellpolicy.State) {
		if shape != links {
			return
		}
		checked++
		words := strings.Fields(strings.TrimPrefix(command, "touch "))
		if len(words) < 50 {
			t.Errorf("the links shape has %d words, want at least fifty", len(words))
		}
		seen := map[string]int{}
		distinct := map[string]bool{}
		for i, word := range words {
			distinct[word] = true
			if n := linksCrossed(t, filepath.FromSlash(s.Root), word, seen, i); n != 255 {
				t.Errorf("word %d of the links shape, %s, goes through %d links, want 255", i+1, word, n)
			}
		}
		if len(distinct) != len(words) {
			t.Errorf("the links shape names %d words and only %d of them differ", len(words), len(distinct))
		}
	}
	stdout, stderr, code := l.run(budgets(3000, 1000, 1000)...)
	if code != 0 {
		t.Fatalf("exit %d with every run inside its budget; stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if checked != 5 {
		t.Errorf("the links shape was timed %d times, want 5", checked)
	}
}

// D5: where the leg cannot make a link, it skips the links shape, says so and
// why, never silently, and still times and judges every other shape. A shape
// skipped is not a pass bought by a failure: a slow perl shape still fails the
// leg.
func TestAC6WhereNoLinkCanBeMadeTheLinksShapeIsSkippedSayingWhyAndTheRestStillCount(t *testing.T) {
	t.Run("AC-6 the skip is said, with its reason, and the leg passes", func(t *testing.T) {
		l := newLeg(t)
		stdout, stderr, code := l.run(budgets(3000, 1000, 1000)...)
		if code != 0 {
			t.Fatalf("exit %d with the links shape skipped and every other run inside its budget; stderr:\n%s", code, stderr)
		}
		said := skipLines(stdout + "\n" + stderr)
		if len(said) != 1 {
			t.Fatalf("%d lines say the links shape was skipped, want one; stdout:\n%s\nstderr:\n%s", len(said), stdout, stderr)
		}
		if !strings.Contains(said[0], errNoLinkHere.Error()) {
			t.Errorf("the line that says the links shape was skipped does not give the reason, %q: %q", errNoLinkHere, said[0])
		}
		if n := len(l.commands[links]); n != 0 {
			t.Errorf("Inspect was handed the links shape %d times, though its links could not be made", n)
		}
		for _, shape := range withoutLinks {
			if n := len(l.commands[shape]); n != 5 {
				t.Errorf("%s was timed %d times with the links shape skipped, want 5", shape, n)
			}
		}
	})
	t.Run("AC-6 the other shapes are still judged", func(t *testing.T) {
		l := newLeg(t)
		l.took[perl] = ms(4000, 4000, 4000, 4000, 4000)
		if _, _, code := l.run(budgets(3000, 1000, 1000)...); code == 0 {
			t.Error("exit 0 with the perl shape over its budget and the links shape skipped")
		}
	})
}

// head is enough of a command to recognise, not 8 KB of it.
func head(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}
