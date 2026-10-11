package testscan

// US-069 AC-2: what `./task test-report` prints, from the sources alone.
//
//	func Report(root string, w io.Writer) error
//	    Reads the test files under root, and the command tree, and writes to w
//	    lines of three shapes only, blank lines between them allowed and spaces
//	    in a line only a separator:
//
//	      command <path>             a command in Commands() that no test in
//	                                 test/e2e runs, in Commands() order
//	      gate <key>                 a gate record key (model.Gates) that no test
//	                                 in test/e2e passes, in model.Gates order
//	      <class> <count> <package>  e2e, unit or other; the tests in the package
//	                                 (the directory, slash-separated, relative to
//	                                 root), in order of package
//
//	A test "runs" a command when the command's words stand as separate string
//	literals among the arguments of a call, or as the elements of a []string
//	literal, in a *_test.go file under test/e2e: "approve", "US-001" and not
//	"approve US-001". The words need not be first: the leading arguments may be
//	anything (t, a directory, "" for no standard input), and the deepest command
//	they name is the one run. A gate key is passed when it is the literal after
//	"gate". The arguments of a call whose function or method name begins with
//	"refus" are left out, because a helper of that name expects the command to
//	be refused. Files that are not *_test.go are not read.
//
//	A test is a top-level function (no receiver) named Test followed by nothing
//	or by a character that is not a lower-case letter, in a *_test.go file;
//	TestMain is not one. The class is e2e for test/e2e, other for any other
//	directory under test/ and for a directory with *.test.js files (a test there
//	is a line that begins `test(`), and unit for every other directory with
//	*_test.go files. .git, .sdlc, node_modules, dist and testdata are not
//	entered. Only the sources are read: nothing is run. A root that is not
//	there, and a *_test.go file that does not parse, are errors.
//
// The tool is internal/tools/testreport (package main), which prints Report for
// the module it is run in, or the one -C names, and exits 0 either way.

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// put writes a file of the fixture module, which is only ever parsed.
func put(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// lines is what a report said, a line to an element, with the spaces in each
// squeezed to one and the blank lines gone.
func lines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestAC2TheReportNamesWhatNoE2ECaseRunsAndCountsTheTestsOfEveryPackage(t *testing.T) {
	root := t.TempDir()
	put(t, root, "test/e2e/a_test.go", `package e2e

import "testing"

func TestA(t *testing.T) {
	runTool(t, binary, dir, "approve", "US-001", "--reject", "why")
	steps := []string{"story", "list"}
	sb.works(t, tree, "researcher", doc, "artifact", "write", "analysis")
	sb.works(t, tree, "", "", "gate", "dor", "pass")
	refused(t, wt, "", "release", "US-001")
	sb.refusesAs(t, "implementer", "freeze")
	launcher(t, binary, root, "{\"tool_input\":{\"command\":\"sdlc status\"}}")
	hook := []string{"hook", event}
}

func TestB(t *testing.T) {}

func TestMain(m *testing.M) {}
`)
	// Not a test file, so not read.
	put(t, root, "test/e2e/helper.go", "package e2e\n\nvar steps = []string{\"init\"}\n")
	put(t, root, "internal/foo/foo.go", "package foo\n\nfunc TestNotATest(t *testing.T) {}\n")
	put(t, root, "internal/foo/foo_test.go", `package foo

import "testing"

func TestOne(t *testing.T)   {}
func TestTwo(t *testing.T)   {}
func TestThree(t *testing.T) {}
func TestMain(m *testing.M)  {}
func TestingHelper()         {}
func (s suite) TestMethod()  {}
func BenchmarkX(b *testing.B) {}
func ExampleX()              {}
`)
	put(t, root, "internal/foo/foo_ext_test.go", "package foo_test\n\nimport \"testing\"\n\nfunc TestFour(t *testing.T) {}\n")
	put(t, root, "test/install/i_test.go", "package install\n\nimport \"testing\"\n\nfunc TestI(t *testing.T) {}\n")
	put(t, root, "npm/test/a.test.js", "test('one', () => {});\ntest('two', () => {\n  test('nested', () => {});\n});\n")
	for _, skipped := range []string{
		".sdlc/worktrees/old/internal/foo/foo_test.go",
		".git/hooks/foo_test.go",
		"node_modules/pkg/foo_test.go",
		"dist/foo_test.go",
		"internal/foo/testdata/foo_test.go",
	} {
		put(t, root, skipped, "package old\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) {}\n")
	}
	put(t, root, "node_modules/pkg/y.test.js", "test('vendored', () => {});\n")

	seen := map[string]bool{"approve": true, "story list": true, "artifact write": true, "gate": true, "hook": true}
	var want []string
	for _, c := range Commands() {
		if !seen[c] {
			want = append(want, "command "+c)
		}
	}
	for _, g := range model.Gates {
		if g != model.GateDoR {
			want = append(want, "gate "+string(g))
		}
	}
	want = append(want, "unit 4 internal/foo", "other 2 npm/test", "e2e 2 test/e2e", "other 1 test/install")

	var out strings.Builder
	if err := Report(root, &out); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if got := lines(out.String()); !slices.Equal(got, want) {
		t.Errorf("the report:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// The controls that make the exact comparison mean something: each of
	// these was written into the fixture as something a test does not run.
	for _, notRun := range []string{"command release", "command freeze", "command status", "command init"} {
		if !slices.Contains(lines(out.String()), notRun) {
			t.Errorf("the report does not list %q, which only a refused call, a payload or a non-test file mentions", notRun)
		}
	}
}

func TestAC2TheReportListsNothingWhenEveryCommandAndGateIsRunAndIsAnErrorWhereThereIsNothingToRead(t *testing.T) {
	root := t.TempDir()
	var b strings.Builder
	b.WriteString("package e2e\n\nfunc TestEverything(t *testing.T) {\n")
	for _, c := range Commands() {
		b.WriteString("\trunTool(t, binary, dir")
		for _, word := range strings.Fields(c) {
			b.WriteString(", \"" + word + "\"")
		}
		b.WriteString(")\n")
	}
	for _, g := range model.Gates {
		b.WriteString("\trunTool(t, binary, dir, \"gate\", \"" + string(g) + "\", \"pass\")\n")
	}
	b.WriteString("}\n")
	put(t, root, "test/e2e/all_test.go", b.String())

	var out strings.Builder
	if err := Report(root, &out); err != nil {
		t.Fatalf("Report: %v", err)
	}
	if got, want := lines(out.String()), []string{"e2e 1 test/e2e"}; !slices.Equal(got, want) {
		t.Errorf("the report of a module where everything is run:\n%s\nwant just %q", strings.Join(got, "\n"), want)
	}

	if err := Report(filepath.Join(root, "absent"), io.Discard); err == nil {
		t.Error("Report of a directory that is not there said nothing was wrong")
	}
	// The same for a test file that does not parse, in test/e2e and elsewhere.
	for _, rel := range []string{"test/e2e/a_test.go", "internal/foo/foo_test.go"} {
		broken := t.TempDir()
		put(t, broken, rel, "package a\n\nfunc TestBroken( {\n")
		if err := Report(broken, io.Discard); err == nil {
			t.Errorf("Report of a module whose %s does not parse said nothing was wrong", rel)
		}
	}
}
