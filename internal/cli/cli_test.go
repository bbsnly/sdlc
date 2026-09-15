package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

func TestVersionPrintsOneLine(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Execute([]string{"version"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if lines := strings.Count(strings.TrimSpace(out.String()), "\n"); lines != 0 {
		t.Errorf("version should print one line, got %q", out.String())
	}
}

func TestShortVersionHasNoDecoration(t *testing.T) {
	var out, errb bytes.Buffer
	Execute([]string{"version", "--short"}, strings.NewReader(""), &out, &errb)
	if strings.ContainsAny(out.String(), "()") {
		t.Errorf("--short should print only the version, got %q", out.String())
	}
}

// --json works on every command, and a script reading the version is the first
// to rely on it: version printed prose whatever it was asked.
func TestVersionAnswersInJSONWhenAsked(t *testing.T) {
	for _, tc := range []struct {
		args []string
		keys string
	}{
		{[]string{"version", "--json"}, "commit date dirty protocol version"},
		{[]string{"version", "--short", "--json"}, "version"},
	} {
		var out, errb bytes.Buffer
		if code := Execute(tc.args, strings.NewReader(""), &out, &errb); code != 0 {
			t.Fatalf("%v: exit %d: %s", tc.args, code, errb.String())
		}
		var got map[string]any
		if err := json.Unmarshal(out.Bytes(), &got); err != nil || got["version"] == "" {
			t.Errorf("%v printed %q, not JSON with a version: %v", tc.args, out.String(), err)
		}
		var keys []string
		for k := range got {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if strings.Join(keys, " ") != tc.keys {
			t.Errorf("%v printed %v, want %s", tc.args, keys, tc.keys)
		}
	}
}

// The shell rules find the subcommand past the flags, and a flag that takes a
// value they did not know hid it: `sdlc --reason x unfreeze` ran unfreeze while
// the rules read `x`. Every flag that takes a value is one they know.
func TestTheShellRulesKnowEveryFlagThatTakesAValue(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, flags := range []*pflag.FlagSet{cmd.LocalFlags(), cmd.PersistentFlags()} {
			flags.VisitAll(func(f *pflag.Flag) {
				if f.Shorthand != "" {
					t.Errorf("%s -%s: the shell rules read long flags only", cmd.CommandPath(), f.Shorthand)
				}
				takesValue := f.NoOptDefVal == ""
				if takesValue != shellpolicy.ValueFlags["--"+f.Name] {
					t.Errorf("%s --%s takes a value: %v, and shellpolicy.ValueFlags says %v",
						cmd.CommandPath(), f.Name, takesValue, !takesValue)
				}
			})
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(New(strings.NewReader(""), io.Discard, io.Discard))
}

func TestBareInvocationHelpsRatherThanErrors(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Execute(nil, strings.NewReader(""), &out, &errb); code != 0 {
		t.Errorf("a bare invocation should teach, not fail: exit %d", code)
	}
	if !strings.Contains(out.String(), "Available Commands") {
		t.Errorf("expected help, got %q", out.String())
	}
}

func TestUnknownCommandFailsWithAMessage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Execute([]string{"nope"}, strings.NewReader(""), &out, &errb); code == 0 {
		t.Error("an unknown command should fail")
	}
	if !strings.Contains(errb.String(), "nope") {
		t.Errorf("the error should name what was not understood, got %q", errb.String())
	}
}

// --json promises a failure in JSON with a code to act on. A command line that
// did not parse came back as prose on standard error, or as JSON carrying only
// cobra's message.
func TestACommandLineThatDoesNotParseFailsLikeEverythingElse(t *testing.T) {
	for _, args := range [][]string{
		{"gate", "dor", "pass", "--bogus"},
		{"nosuch"},
		{"gate", "dor"},
		{"review", "add", "code_review"},
	} {
		name := strings.Join(args, " ")

		var out, errb bytes.Buffer
		if code := Execute(append(args, "--json"), strings.NewReader(""), &out, &errb); code == 0 {
			t.Errorf("sdlc %s --json succeeded", name)
		}
		var got struct {
			OK    bool   `json:"ok"`
			Error string `json:"error"`
			Code  string `json:"code"`
			Fix   string `json:"fix"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Errorf("sdlc %s --json did not fail in JSON: %v\nstdout: %q\nstderr: %q",
				name, err, out.String(), errb.String())
			continue
		}
		if got.OK || got.Error == "" || got.Code != "SDLC-E0034" || !strings.Contains(got.Fix, "--help") {
			t.Errorf("sdlc %s --json = %+v", name, got)
		}

		out.Reset()
		errb.Reset()
		Execute(args, strings.NewReader(""), &out, &errb)
		if !strings.Contains(errb.String(), "SDLC-E0034") || !strings.Contains(errb.String(), "--help") {
			t.Errorf("sdlc %s says nothing a person can act on:\n%s", name, errb.String())
		}
	}
}

// Every verb carries the shape the documentation depends on. This is the seed
// of the --help shape test that later rows extend.
func TestEveryCommandHasAShortAndAnExample(t *testing.T) {
	var walk func(*cobra.Command, string)
	walk = func(c *cobra.Command, path string) {
		for _, sub := range c.Commands() {
			name := strings.TrimSpace(path + " " + sub.Name())
			if sub.Name() == "help" || sub.Name() == "completion" {
				continue // cobra writes these; we do not
			}
			if sub.Short == "" {
				t.Errorf("%s: Short is empty", name)
			}
			if strings.HasSuffix(sub.Short, ".") {
				t.Errorf("%s: Short should not end in a period (cobra convention)", name)
			}
			if sub.Example == "" {
				t.Errorf("%s: no Example; a verb that does anything non-obvious needs one", name)
			}
			walk(sub, name)
		}
	}
	walk(New(strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}), "")
}
