package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionPrintsOneLine(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Execute([]string{"version"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if lines := strings.Count(strings.TrimSpace(out.String()), "\n"); lines != 0 {
		t.Errorf("version should print one line, got %q", out.String())
	}
}

func TestShortVersionHasNoDecoration(t *testing.T) {
	var out, errb bytes.Buffer
	Execute([]string{"version", "--short"}, &out, &errb)
	if strings.ContainsAny(out.String(), "()") {
		t.Errorf("--short should print only the version, got %q", out.String())
	}
}

func TestBareInvocationHelpsRatherThanErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Execute(nil, &out, &errb); code != 0 {
		t.Errorf("a bare invocation should teach, not fail: exit %d", code)
	}
	if !strings.Contains(out.String(), "Available Commands") {
		t.Errorf("expected help, got %q", out.String())
	}
}

func TestUnknownCommandFailsWithAMessage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Execute([]string{"nope"}, &out, &errb); code == 0 {
		t.Error("an unknown command should fail")
	}
	if !strings.Contains(errb.String(), "nope") {
		t.Errorf("the error should name what was not understood, got %q", errb.String())
	}
}

// Every verb carries the shape the documentation depends on. This is the seed
// of the --help shape test that later rows extend.
func TestEveryCommandHasAShortAndAnExample(t *testing.T) {
	root := New(&bytes.Buffer{}, &bytes.Buffer{})
	for _, c := range root.Commands() {
		if c.Name() == "help" || c.Name() == "completion" {
			continue // cobra writes these; we do not
		}
		if c.Short == "" {
			t.Errorf("%s: Short is empty", c.Name())
		}
		if strings.HasSuffix(c.Short, ".") {
			t.Errorf("%s: Short should not end in a period (cobra convention)", c.Name())
		}
		if c.Example == "" {
			t.Errorf("%s: no Example; a verb that does anything non-obvious needs one", c.Name())
		}
	}
}
