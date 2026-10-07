package main

import (
	"strings"
	"testing"
)

func TestAC1AWhitespaceOnlyLinkTargetIsReportedWithFileAndLineAndExitsOne(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "intro\n\nsee [a]( ) here\n")

	out, errb, code := runMain(t, []string{"-mode=links", "-C", root})
	if strings.Contains(errb, "panic") || strings.Contains(errb, "goroutine") {
		t.Fatalf("linkcheck panicked: %q", errb)
	}
	if code != 1 {
		t.Fatalf("exit %d, want 1; stderr=%q", code, errb)
	}
	if out != "" {
		t.Errorf("findings belong on stderr, got stdout %q", out)
	}
	if !strings.Contains(errb, "README.md:3: ") || !strings.Contains(errb, "no target") {
		t.Errorf("stderr should name README.md:3 and say the link has no target, got %q", errb)
	}
	if !strings.Contains(errb, "1 problem(s) in links mode") {
		t.Errorf("want exactly one problem, got %q", errb)
	}
}

func TestAC1BTabAndNoBreakSpaceOnlyTargetsAreReportedNotPanicked(t *testing.T) {
	for name, body := range map[string]string{
		"tab":    "[a](\t)\n",
		"nbsp":   "[a]( )\n",
		"image":  "![alt]( )\n",
		"spaces": "[a](   )\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "README.md", body)
			_, errb, code := runMain(t, []string{"-mode=links", "-C", root})
			if code != 1 || !strings.Contains(errb, "README.md:1: ") || !strings.Contains(errb, "no target") {
				t.Errorf("exit %d, stderr=%q", code, errb)
			}
		})
	}
}

func TestAC2EmptyTargetAndBrokenLinkAreBothReported(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "[a]( )\n\n[b](./missing.md)\n")

	_, errb, code := runMain(t, []string{"-mode=links", "-C", root})
	if code != 1 {
		t.Fatalf("exit %d, want 1; stderr=%q", code, errb)
	}
	for _, want := range []string{"README.md:1: ", "no target", "README.md:3: ", "does not resolve", "2 problem(s) in links mode"} {
		if !strings.Contains(errb, want) {
			t.Errorf("stderr should contain %q, got %q", want, errb)
		}
	}
}

func TestAC2TwoEmptyTargetsOnOneLineAndAnotherFileAreAllReported(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "[a]( ) and [b]( )\n")
	write(t, root, "docs/x.md", "[c](./gone.md)\n")

	_, errb, code := runMain(t, []string{"-mode=links", "-C", root})
	if code != 1 {
		t.Fatalf("exit %d, want 1; stderr=%q", code, errb)
	}
	if !strings.Contains(errb, "docs/x.md:1: ") || !strings.Contains(errb, "does not resolve") {
		t.Errorf("the broken link in the second file is missing: %q", errb)
	}
	if strings.Count(errb, "no target") != 2 {
		t.Errorf("want two no-target findings, got %q", errb)
	}
}

// Pins existing behaviour: surrounding whitespace and a title are dropped.
func TestAC3TargetWithSurroundingWhitespaceAndTitleIsCheckedAsThePath(t *testing.T) {
	root := t.TempDir()
	write(t, root, "guide/a.md", "[a]( x.md \"title\" )\n")
	write(t, root, "guide/x.md", "x\n")

	_, errb, code := runMain(t, []string{"-mode=links", "-C", root})
	if code != 0 || errb != "" {
		t.Errorf("an existing target with a title should pass: exit %d, stderr=%q", code, errb)
	}

	root = t.TempDir()
	write(t, root, "guide/a.md", "[a]( y.md \"title\" )\n")
	_, errb, code = runMain(t, []string{"-mode=links", "-C", root})
	if code != 1 || !strings.Contains(errb, "y.md") || !strings.Contains(errb, "does not resolve") {
		t.Errorf("a missing target should be reported as docs/y.md: exit %d, stderr=%q", code, errb)
	}
	if strings.Contains(errb, "no target") {
		t.Errorf("a link with a target must not be called empty: %q", errb)
	}
}

// Pins existing behaviour: nothing inside a fence is scanned.
func TestAC4EmptyTargetInsideAFencedBlockIsIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, root, "README.md", "```\n[a]( )\n```\n\n~~~\n[b]( )\n~~~\n")

	out, errb, code := runMain(t, []string{"-mode=links", "-C", root})
	if code != 0 || out != "" || errb != "" {
		t.Errorf("fenced links should be ignored: exit %d, stdout=%q stderr=%q", code, out, errb)
	}
}
