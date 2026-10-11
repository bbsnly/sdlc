package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// runMain runs the real main() as a subprocess -- via linkcheckHelperEnv, set
// up in TestMain -- and reports what it wrote and how it exited. checkLinks
// and checkHygiene are exercised directly elsewhere; this is the only seam
// that reaches main's own flag parsing, mode dispatch, sorting and exit codes.
func runMain(t *testing.T, args []string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], args...)
	cmd.Env = append(os.Environ(), linkcheckHelperEnv+"=1")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		exitCode = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running linkcheck: %v", err)
	}
	return out.String(), errb.String(), exitCode
}

func TestMainExitsCleanAndSaysNothingWhenThereIsNothingToReport(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "nothing to see here\n")

	out, errb, code := runMain(t, []string{"-mode=links", "-C", root})
	if code != 0 {
		t.Fatalf("exit %d, stderr=%q", code, errb)
	}
	if out != "" || errb != "" {
		t.Errorf("a clean run should say nothing, got stdout=%q stderr=%q", out, errb)
	}
}

// The one problem, the count and the fix all have to reach a person who never
// sees the Go values behind them, only what main printed to stderr.
func TestMainReportsBrokenLinksToStderrAndExitsOne(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "[x](./missing.md)\n")

	out, errb, code := runMain(t, []string{"-mode=links", "-C", root})
	if code != 1 {
		t.Fatalf("exit %d, stderr=%q", code, errb)
	}
	if out != "" {
		t.Errorf("findings belong on stderr, not stdout: %q", out)
	}
	for _, want := range []string{"does not resolve", "1 problem(s) in links mode", "Fix the link, or move the target"} {
		if !strings.Contains(errb, want) {
			t.Errorf("stderr should contain %q, got %q", want, errb)
		}
	}
}

// The hygiene mode's own fix message differs from the link mode's; a run has
// to reach it to prove main picked the right one rather than always the first.
func TestMainReportsHygieneProblemsWithTheHygieneFixAndExitsOne(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"plan/notes.md": "internal\n",
		"README.md":     "fine\n",
	})

	_, errb, code := runMain(t, []string{"-mode=hygiene", "-C", root})
	if code != 1 {
		t.Fatalf("exit %d, stderr=%q", code, errb)
	}
	if !strings.Contains(errb, "internal working document is tracked") {
		t.Errorf("stderr should carry the finding, got %q", errb)
	}
	if !strings.Contains(errb, "problem(s) in hygiene mode") || !strings.Contains(errb, "Move the file under plan/") {
		t.Errorf("stderr should carry the hygiene fix, got %q", errb)
	}
}

// checkHygiene's own error (not a git repository) has to reach the person as
// "linkcheck: <why>" on stderr with exit 2, the same as any other tool failure.
func TestMainReportsAModeThatCannotRunAndExitsTwo(t *testing.T) {
	root := t.TempDir() // never a git repository

	_, errb, code := runMain(t, []string{"-mode=hygiene", "-C", root})
	if code != 2 {
		t.Fatalf("exit %d, stderr=%q", code, errb)
	}
	if !strings.HasPrefix(errb, "linkcheck: ") || !strings.Contains(errb, "git ls-files") {
		t.Errorf("stderr = %q", errb)
	}
}

// The findings are sorted by file then by line before a person reads them; out
// of order, they read as noise rather than a list to work through top to
// bottom.
func TestMainSortsFindingsByFileThenByLine(t *testing.T) {
	root := t.TempDir()
	write(t, root, "z.md", "[a](./gone.md)\n[b](./gone.md)\n")
	write(t, root, "a.md", "[c](./gone.md)\n")

	_, errb, code := runMain(t, []string{"-mode=links", "-C", root})
	if code != 1 {
		t.Fatalf("exit %d, stderr=%q", code, errb)
	}
	var findings []string
	for l := range strings.SplitSeq(strings.TrimRight(errb, "\n"), "\n") {
		if strings.HasPrefix(l, "a.md:") || strings.HasPrefix(l, "z.md:") {
			findings = append(findings, l)
		}
	}
	want := []string{"a.md:1:", "z.md:1:", "z.md:2:"}
	if len(findings) != len(want) {
		t.Fatalf("findings = %v, want %d of them", findings, len(want))
	}
	for i, prefix := range want {
		if !strings.HasPrefix(findings[i], prefix) {
			t.Errorf("findings out of order: %v, want prefixes %v", findings, want)
		}
	}
}

func TestMainRejectsAnUnknownModeAndExitsTwo(t *testing.T) {
	_, errb, code := runMain(t, []string{"-mode=bogus", "-C", t.TempDir()})
	if code != 2 {
		t.Fatalf("exit %d, stderr=%q", code, errb)
	}
	if !strings.Contains(errb, `unknown mode "bogus"`) || !strings.Contains(errb, "expected links or hygiene") {
		t.Errorf("stderr = %q", errb)
	}
}
