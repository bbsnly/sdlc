package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func runTime(stdin string, args ...string) (stdout, stderr string, code int) {
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader(stdin), &out, &errOut)
	return out.String(), errOut.String(), code
}

// events is go test -json of three packages, as go 1.27 wrote it: one with a
// test that fails in a subtest beside one that passes and one that skips, one
// that passes, and one that does not build.
const events = `{"ImportPath":"example.com/m/c [example.com/m/c.test]","Action":"build-output","Output":"# example.com/m/c [example.com/m/c.test]\n"}
{"ImportPath":"example.com/m/c [example.com/m/c.test]","Action":"build-output","Output":"c/c_test.go:5:33: undefined: undefined\n"}
{"ImportPath":"example.com/m/c [example.com/m/c.test]","Action":"build-fail"}
{"Action":"start","Package":"example.com/m/a"}
{"Action":"start","Package":"example.com/m/b"}
{"Action":"start","Package":"example.com/m/c"}
{"Action":"output","Package":"example.com/m/c","Output":"FAIL\texample.com/m/c [build failed]\n","OutputType":"frame"}
{"Action":"fail","Package":"example.com/m/c","Elapsed":0,"FailedBuild":"example.com/m/c [example.com/m/c.test]"}
{"Action":"run","Package":"example.com/m/a","Test":"TestPasses"}
{"Action":"run","Package":"example.com/m/b","Test":"TestPasses"}
{"Action":"output","Package":"example.com/m/a","Test":"TestPasses","Output":"=== RUN   TestPasses\n","OutputType":"frame"}
{"Action":"output","Package":"example.com/m/b","Test":"TestPasses","Output":"=== RUN   TestPasses\n","OutputType":"frame"}
{"Action":"output","Package":"example.com/m/a","Test":"TestPasses","Output":"    a_test.go:5: quiet\n"}
{"Action":"output","Package":"example.com/m/b","Test":"TestPasses","Output":"    b_test.go:5: quiet\n"}
{"Action":"output","Package":"example.com/m/b","Test":"TestPasses","Output":"--- PASS: TestPasses (0.00s)\n","OutputType":"frame"}
{"Action":"output","Package":"example.com/m/a","Test":"TestPasses","Output":"--- PASS: TestPasses (0.00s)\n","OutputType":"frame"}
{"Action":"pass","Package":"example.com/m/a","Test":"TestPasses","Elapsed":1.5}
{"Action":"run","Package":"example.com/m/a","Test":"TestFails"}
{"Action":"output","Package":"example.com/m/a","Test":"TestFails","Output":"=== RUN   TestFails\n","OutputType":"frame"}
{"Action":"output","Package":"example.com/m/a","Test":"TestFails","Output":"    a_test.go:8: said\n"}
{"Action":"pass","Package":"example.com/m/b","Test":"TestPasses","Elapsed":0.25}
{"Action":"output","Package":"example.com/m/b","Output":"PASS\n","OutputType":"frame"}
{"Action":"run","Package":"example.com/m/a","Test":"TestFails/ok"}
{"Action":"output","Package":"example.com/m/a","Test":"TestFails/ok","Output":"=== RUN   TestFails/ok\n","OutputType":"frame"}
{"Action":"output","Package":"example.com/m/a","Test":"TestFails/ok","Output":"    a_test.go:9: sub quiet\n"}
{"Action":"output","Package":"example.com/m/a","Test":"TestFails/ok","Output":"--- PASS: TestFails/ok (0.00s)\n","OutputType":"frame"}
{"Action":"pass","Package":"example.com/m/a","Test":"TestFails/ok","Elapsed":9}
{"Action":"run","Package":"example.com/m/a","Test":"TestFails/bad"}
{"Action":"output","Package":"example.com/m/a","Test":"TestFails/bad","Output":"=== RUN   TestFails/bad\n","OutputType":"frame"}
{"Action":"output","Package":"example.com/m/a","Test":"TestFails/bad","Output":"    a_test.go:10: sub said\n","OutputType":"error"}
{"Action":"output","Package":"example.com/m/a","Test":"TestFails/bad","Output":"--- FAIL: TestFails/bad (0.00s)\n","OutputType":"frame"}
{"Action":"fail","Package":"example.com/m/a","Test":"TestFails/bad","Elapsed":0}
{"Action":"output","Package":"example.com/m/a","Test":"TestFails","Output":"--- FAIL: TestFails (0.00s)\n","OutputType":"frame"}
{"Action":"fail","Package":"example.com/m/a","Test":"TestFails","Elapsed":9.25}
{"Action":"run","Package":"example.com/m/a","Test":"TestSkips"}
{"Action":"output","Package":"example.com/m/a","Test":"TestSkips","Output":"=== RUN   TestSkips\n","OutputType":"frame"}
{"Action":"output","Package":"example.com/m/a","Test":"TestSkips","Output":"    a_test.go:13: why\n"}
{"Action":"output","Package":"example.com/m/a","Test":"TestSkips","Output":"--- SKIP: TestSkips (0.00s)\n","OutputType":"frame"}
{"Action":"skip","Package":"example.com/m/a","Test":"TestSkips","Elapsed":0}
{"Action":"output","Package":"example.com/m/a","Output":"FAIL\n","OutputType":"frame"}
{"Action":"output","Package":"example.com/m/b","Output":"ok  \texample.com/m/b\t0.31s\n"}
{"Action":"output","Package":"example.com/m/a","Output":"FAIL\texample.com/m/a\t10.8s\n","OutputType":"frame"}
{"Action":"fail","Package":"example.com/m/a","Elapsed":10.8}
{"Action":"pass","Package":"example.com/m/b","Elapsed":0.31}
`

func TestTheOutputIsWhatGoTestPrintsWithoutJSON(t *testing.T) {
	stdout, stderr, code := runTime(events)
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q; want 0 and nothing", code, stderr)
	}
	want := "# example.com/m/c [example.com/m/c.test]\n" +
		"c/c_test.go:5:33: undefined: undefined\n" +
		"FAIL\texample.com/m/c [build failed]\n" +
		"--- FAIL: TestFails (0.00s)\n" +
		"    a_test.go:8: said\n" +
		"    --- FAIL: TestFails/bad (0.00s)\n" +
		"        a_test.go:10: sub said\n" +
		"FAIL\n" +
		"FAIL\texample.com/m/a\t10.8s\n" +
		"ok  \texample.com/m/b\t0.31s\n"
	if got, _, _ := strings.Cut(stdout, "\ntime per package"); got != want {
		t.Errorf("the output before the summary is\n%s\nwant\n%s", got, want)
	}
}

func TestThePackagesAndTheSlowestTestsAreListedSlowestFirst(t *testing.T) {
	stdout, _, _ := runTime(events, "-n", "2")
	_, summary, ok := strings.Cut(stdout, "\ntime per package, slowest first:\n")
	if !ok {
		t.Fatalf("no summary in\n%s", stdout)
	}
	want := "    10.80s  example.com/m/a\n" +
		"     0.31s  example.com/m/b\n" +
		"     0.00s  example.com/m/c\n" +
		"the 2 slowest tests:\n" +
		// A subtest is in its test's time, so only top-level tests are named.
		"     9.25s  example.com/m/a TestFails\n" +
		"     1.50s  example.com/m/a TestPasses\n"
	if summary != want {
		t.Errorf("the summary is\n%s\nwant\n%s", summary, want)
	}
}

func TestAPackageOverHalfTheLimitIsNamed(t *testing.T) {
	stdout, _, _ := runTime(events, "-limit", "20s")
	over := regexp.MustCompile(`(?m)^testtime: (\S+) took (\S+), more than half of go test's limit of 20s\.`).FindAllStringSubmatch(stdout, -1)
	if len(over) != 1 || over[0][1] != "example.com/m/a" || over[0][2] != "10.80s" {
		t.Errorf("want example.com/m/a named once, at 10.80s; got %q in\n%s", over, stdout)
	}
	if !strings.Contains(stdout, "./task profile PKG=") {
		t.Errorf("the warning does not say what to do:\n%s", stdout)
	}
	if stdout, _, _ := runTime(events, "-limit", "22s"); strings.Contains(stdout, "more than half") {
		t.Errorf("a package at less than half the limit is named:\n%s", stdout)
	}
}

// go test killed at its limit stops writing mid-package: what that package
// said is all printed, the lines of the test that was running among them.
func TestAPackageThatNeverEndedIsPrintedWhole(t *testing.T) {
	in := `{"Action":"start","Package":"example.com/m/a"}
{"Action":"run","Package":"example.com/m/a","Test":"TestHangs"}
{"Action":"output","Package":"example.com/m/a","Test":"TestHangs","Output":"=== RUN   TestHangs\n"}
{"Action":"output","Package":"example.com/m/a","Test":"TestHangs","Output":"panic: test timed out after 5m0s\n"}
{"Action":"output","Package":"example.com/m/a","Output":"FAIL\texample.com/m/a\t300.1s\n"}
`
	stdout, _, code := runTime(in)
	if code != 0 || stdout != "panic: test timed out after 5m0s\nFAIL\texample.com/m/a\t300.1s\n" {
		t.Errorf("exit %d, output\n%q", code, stdout)
	}
}

func TestALineThatIsNotJSONIsPassedThrough(t *testing.T) {
	stdout, _, _ := runTime("go: downloading example.com/x v1.0.0\n{not json\nno newline at the end")
	if stdout != "go: downloading example.com/x v1.0.0\n{not json\nno newline at the end\n" {
		t.Errorf("output %q", stdout)
	}
}

func TestWordsItDoesNotTakeExitTwo(t *testing.T) {
	for _, args := range [][]string{{"-x"}, {"extra"}, {"-n", "-1"}, {"-limit", "soon"}} {
		stdout, stderr, code := runTime(events, args...)
		if code != 2 || stdout != "" || !strings.Contains(stderr, "testtime: takes only") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
}

// The fixture above is go's own output, and go can change it. This runs the
// go that runs the tests, on a module like the one the fixture came from, and
// holds this program's lines to those go test prints without -json.
func TestOnRealGoTestOutputTheLinesAreGoTestsOwn(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not on PATH:", err)
	}
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.com/m\n\ngo 1.21\n",
		"a/a_test.go":  "package a\n\nimport \"testing\"\n\nfunc TestPasses(t *testing.T) { t.Log(\"quiet\") }\n\nfunc TestFails(t *testing.T) {\n\tt.Log(\"said\")\n\tt.Run(\"ok\", func(t *testing.T) { t.Log(\"sub quiet\") })\n\tt.Run(\"bad\", func(t *testing.T) { t.Error(\"sub said\") })\n}\n\nfunc TestSkips(t *testing.T) { t.Skip(\"why\") }\n",
		"b/b_test.go":  "package b\n\nimport \"testing\"\n\nfunc TestPasses(t *testing.T) { t.Log(\"quiet\") }\n",
		"c/c_test.go":  "package c\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) { undefined() }\n",
		"d/d_test.go":  "package d\n",
		"e/e.go":       "package e\n",
		"f/f_test.go":  "package f\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\nfunc TestMain(m *testing.M) { fmt.Println(\"from TestMain\"); m.Run() }\n\nfunc TestPasses(t *testing.T) {}\n",
		"g/g_test.go":  "package g\n\nimport \"testing\"\n\nfunc TestPanics(t *testing.T) { panic(\"boom\") }\n",
		"h/h_test.go":  "package h\n\nimport \"testing\"\n\nfunc TestParallel(t *testing.T) {\n\tfor _, n := range []string{\"x\", \"y\"} {\n\t\tt.Run(n, func(t *testing.T) { t.Parallel(); t.Log(n) })\n\t}\n}\n",
		"h/h2_test.go": "package h\n\nimport \"testing\"\n\nfunc TestParallelFails(t *testing.T) { t.Parallel(); t.Fatal(\"stopped\") }\n",
	}
	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	goTest := func(args ...string) string {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "go", append([]string{"test", "-count=1"}, append(args, "./...")...)...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS=", "GOTOOLCHAIN=local", "GOWORK=off")
		out, err := cmd.CombinedOutput()
		if _, failed := errors.AsType[*exec.ExitError](err); err != nil && !failed {
			t.Fatalf("go test %q did not run: %v\n%s", args, err, out)
		}
		return string(out)
	}
	plain := goTest()
	stdout, stderr, code := runTime(goTest("-json"))
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	got, _, _ := strings.Cut(stdout, "\ntime per package")
	gotBlocks, wantBlocks := packages(got), packages(plain)
	for _, b := range gotBlocks {
		if !slices.Contains(wantBlocks, b) {
			t.Errorf("from go test -json, a package printed\n%s\nwhich go test without it did not", b)
		}
	}
	for _, b := range wantBlocks {
		if !slices.Contains(gotBlocks, b) {
			t.Errorf("go test printed\n%s\nwhich this did not, from go test -json", b)
		}
	}
}

// varies is what differs between two runs: times, and the numbers and
// addresses in a goroutine dump.
var varies = regexp.MustCompile(`\d+\.\d+s\b|goroutine \d+|0x[0-9a-f]+`)

// packages is out cut into what each package printed, each ending on its ok,
// FAIL or ? line, sorted: packages finish in a different order each run, and
// -json does not print the FAIL go prints last. The compiler's lines, which go
// prints before any package's, are a block of their own.
func packages(out string) []string {
	var blocks []string
	var b, compiler strings.Builder
	for l := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(l, "# example.com/") || strings.HasPrefix(l, "c/c_test.go:") {
			compiler.WriteString(l + "\n")
			continue
		}
		b.WriteString(varies.ReplaceAllString(l, "#") + "\n")
		if f := strings.Fields(l); len(f) >= 2 && (f[0] == "ok" || f[0] == "FAIL" || f[0] == "?") && strings.HasPrefix(f[1], "example.com/") {
			blocks = append(blocks, b.String())
			b.Reset()
		}
	}
	if rest := b.String(); rest != "FAIL\n" && rest != "" {
		blocks = append(blocks, rest)
	}
	blocks = append(blocks, compiler.String())
	slices.Sort(blocks)
	return blocks
}
