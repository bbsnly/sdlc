package main

// US-069 AC-2, the program: `./task test-report` runs it. testscan has the
// report and is tested next door; what can go wrong here is the exit code, the
// streams and the way the task reaches it.
//
//	func run(args []string, stdout, stderr io.Writer) int
//
//	run(nil)                 exit 0, the report for the module in the current
//	                         directory on stdout, nothing on stderr
//	run({"-C", dir})         the same for the module at dir
//	a directory it cannot    exit 1, nothing on stdout, "testreport: ..." on stderr
//	read
//	words it does not know   exit 2, nothing on stdout, "testreport: ..." on stderr
//	                         that says what it takes
//
// The exit is 0 whether or not the report lists anything: it is a report, not
// a check; the guard in test/e2e is the check.

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/testscan"
)

// thisCheckout is this repository, three directories up.
const thisCheckout = "../../.."

func runReport(args ...string) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return out.String(), errOut.String(), code
}

func TestAC2TheReportOfThisRepositoryIsPrintedAndExitsZero(t *testing.T) {
	stdout, stderr, code := runReport("-C", thisCheckout)
	if code != 0 || stderr != "" {
		t.Fatalf("test-report: exit %d, stderr %q; want 0 and nothing", code, stderr)
	}
	classes := map[string]string{"test/e2e": "e2e", "internal/testscan": "unit", "npm/test": "other", "test/install": "other"}
	seen := map[string]bool{}
	for l := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
		f := strings.Fields(l)
		switch {
		case len(f) == 0:
		case f[0] == "command" && len(f) >= 2 && slices.Contains(testscan.Commands(), strings.Join(f[1:], " ")):
		case f[0] == "gate" && len(f) == 2 && model.Gate(f[1]).Valid():
		case len(f) == 3 && (f[0] == "e2e" || f[0] == "unit" || f[0] == "other"):
			seen[f[2]] = true
			if want, ok := classes[f[2]]; ok && f[0] != want {
				t.Errorf("%s is counted as %s, want %s", f[2], f[0], want)
			}
		default:
			t.Errorf("a line of none of the three shapes: %q", l)
		}
	}
	for pkg := range classes {
		if !seen[pkg] {
			t.Errorf("the report has no count for %s:\n%s", pkg, stdout)
		}
	}
	// A path from a machine is refused in anything tracked, and the report is
	// meant to be pasted: every path in it is relative to the module.
	if strings.Contains(stdout, thisCheckout) || strings.Contains(stdout, "\\") || strings.Contains(stdout, string(os.PathSeparator)+"Users") {
		t.Errorf("the report names a path outside the module or in a platform's spelling:\n%s", stdout)
	}
}

func TestAC2TheExitIsZeroWhereNoTestRunsAnythingAndNotWhereThereIsNothingToRead(t *testing.T) {
	root := t.TempDir()

	stdout, stderr, code := runReport("-C", root)
	flat := strings.Join(strings.Fields(stdout), " ") + " "
	if code != 0 || stderr != "" || !strings.Contains(flat, "command approve ") || !strings.Contains(flat, "gate dor ") {
		t.Errorf("a module with no test: exit %d\nstdout %q\nstderr %q\nwant 0, and every command and gate listed",
			code, stdout, stderr)
	}

	stdout, stderr, code = runReport("-C", filepath.Join(root, "absent"))
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "testreport: ") {
		t.Errorf("a directory that is not there: exit %d\nstdout %q\nstderr %q\nwant 1, nothing on stdout, \"testreport: ...\"",
			code, stdout, stderr)
	}

	for _, args := range [][]string{{"extra"}, {"-C", root, "extra"}, {"-nosuchflag"}} {
		stdout, stderr, code = runReport(args...)
		if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "testreport: ") {
			t.Errorf("test-report %s: exit %d\nstdout %q\nstderr %q\nwant 2, nothing on stdout, \"testreport: ...\"",
				strings.Join(args, " "), code, stdout, stderr)
		}
	}
}

// The task is how a person reaches the program, and nothing else runs it.
func TestAC2TheTaskfileHasATestReportTaskThatRunsTheProgram(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(thisCheckout, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	start := slices.Index(lines, "  test-report:")
	if start < 0 {
		t.Fatal("Taskfile.yml has no task test-report")
	}
	end := start + 1
	for end < len(lines) && (strings.TrimSpace(lines[end]) == "" || strings.HasPrefix(lines[end], "   ")) {
		end++
	}
	body := strings.Join(lines[start:end], "\n")
	for _, want := range []string{"desc:", "go run ./internal/tools/testreport"} {
		if !strings.Contains(body, want) {
			t.Errorf("the task test-report has no %q:\n%s", want, body)
		}
	}
}
