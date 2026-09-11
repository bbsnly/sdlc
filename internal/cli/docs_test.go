package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/policy"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

// The documentation is part of the product, and prose drifts from code in one
// direction: the code changes and the prose does not. These tests are what stop
// a reader following an instruction that used to be true.

func page(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
	if err != nil {
		t.Fatalf("docs/%s: %v", name, err)
	}
	return string(raw)
}

// commands walks the real command tree, so a new verb arrives here without
// anybody remembering to add it.
func commands(t *testing.T) []*cobra.Command {
	t.Helper()
	var out []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			out = append(out, sub)
			walk(sub)
		}
	}
	walk(New(strings.NewReader(""), io.Discard, io.Discard))
	return out
}

// path is the command as a person types it: "review add", not "add".
func path(c *cobra.Command) string {
	return strings.TrimPrefix(c.CommandPath(), "sdlc ")
}

func TestEveryCommandIsDocumented(t *testing.T) {
	doc := page(t, "commands.md")
	for _, c := range commands(t) {
		if c.HasSubCommands() && !c.Runnable() {
			// A group with nothing to run of its own is documented through its
			// children, which is how a reader looks for it.
			continue
		}
		if !strings.Contains(doc, "## `sdlc "+path(c)) {
			t.Errorf("docs/commands.md has no section for `sdlc %s`", path(c))
		}
	}
}

func TestEveryFlagIsDocumented(t *testing.T) {
	doc := page(t, "commands.md")
	for _, c := range commands(t) {
		c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name == "help" {
				return
			}
			if !strings.Contains(doc, "`--"+f.Name+"`") {
				t.Errorf("docs/commands.md does not mention --%s (sdlc %s)", f.Name, path(c))
			}
		})
	}
}

// Every command that fails has to be lookupable, and the page that tells a
// reader where to look is the one they are already reading.
func TestTheCommandPageSendsPeopleToTheCodes(t *testing.T) {
	if !strings.Contains(page(t, "commands.md"), "troubleshooting.md") {
		t.Error("docs/commands.md never mentions the troubleshooting page")
	}
}

func TestEveryGateIsDocumented(t *testing.T) {
	loop, commandsDoc := page(t, "the-loop.md"), page(t, "commands.md")
	for _, g := range model.Gates {
		if !strings.Contains(loop, string(g)) {
			t.Errorf("docs/the-loop.md never mentions the %s gate", g)
		}
		if !strings.Contains(commandsDoc, "`"+string(g)+"`") {
			t.Errorf("docs/commands.md does not list the %s gate", g)
		}
	}
}

func TestEveryDocumentAGateProducesIsDocumented(t *testing.T) {
	loop, commandsDoc := page(t, "the-loop.md"), page(t, "commands.md")
	for _, a := range model.Artifacts {
		if !strings.Contains(loop, a.File) {
			t.Errorf("docs/the-loop.md never mentions %s", a.File)
		}
		if !strings.Contains(commandsDoc, "`"+a.Name+"`") {
			t.Errorf("docs/commands.md does not list the %s document", a.Name)
		}
	}
}

// A rule that refuses something a reader cannot look up is a rule they will
// work around.
func TestEveryRuleIsDocumented(t *testing.T) {
	doc := page(t, "enforcement.md")
	for _, r := range policy.Rules {
		if !strings.Contains(doc, "### `"+r.ID+"`") {
			t.Errorf("docs/enforcement.md has no section for the %s rule", r.ID)
		}
	}
	for _, id := range shellRules(t) {
		if !strings.Contains(doc, "### `"+id+"`") {
			t.Errorf("docs/enforcement.md has no section for the %s rule", id)
		}
	}
}

// shellRules is the set the shell policy can actually produce, found by running
// it rather than by keeping a second list beside the first.
func shellRules(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, command := range []string{
		"rm .sdlc/state/active",
		"git commit -m x",
		"export SDLC_ENFORCE=0",
	} {
		f, ok := shellpolicy.Inspect(command, shellpolicy.State{})
		if !ok {
			t.Fatalf("%s was allowed; this test no longer covers what it thinks", command)
		}
		out = append(out, f.Rule)
	}
	return out
}

func TestEveryAgentIsDocumented(t *testing.T) {
	doc := page(t, "agents.md")
	entries, err := os.ReadDir(filepath.Join("..", "..", "plugin", "agents"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".md")
		if !strings.Contains(doc, "`sdlc:"+name+"`") {
			t.Errorf("docs/agents.md has no row for sdlc:%s", name)
		}
	}
}

// Every reviewer the loop waits for has to be an agent that exists, or the gate
// waits forever.
func TestEveryReviewerHasAnAgent(t *testing.T) {
	for _, r := range model.Reviewers {
		if _, err := os.Stat(filepath.Join("..", "..", "plugin", "agents", r.Role+".md")); err != nil {
			t.Errorf("%s reviews %s, but there is no sdlc:%s agent", r.Role, r.Gate, r.Role)
		}
	}
}

// Every agent named as a role in the registry or in the artifact table has to
// exist too, for the same reason.
func TestEveryRoleThatOwnsWorkHasAnAgent(t *testing.T) {
	for _, a := range model.Artifacts {
		if _, err := os.Stat(filepath.Join("..", "..", "plugin", "agents", a.Role+".md")); err != nil {
			t.Errorf("%s is produced by %s, but there is no sdlc:%s agent", a.File, a.Role, a.Role)
		}
	}
}

// A setting nobody documented is a setting nobody can use.
func TestEverySettingIsDocumented(t *testing.T) {
	doc := page(t, "configuration.md")
	for _, key := range jsonKeys(reflect.TypeOf(config.Default())) {
		if !strings.Contains(doc, key) {
			t.Errorf("docs/configuration.md never mentions %q", key)
		}
	}
}

// jsonKeys is every field name the configuration file can contain, found from
// the struct tags so that a new setting cannot be added without the test
// noticing.
func jsonKeys(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		out = append(out, name)
		if f.Type.Kind() == reflect.Struct {
			out = append(out, jsonKeys(f.Type)...)
		}
	}
	return out
}

// The example in the configuration page is the first thing a reader copies, so
// it has to be a file this version would actually accept.
func TestTheConfigurationExampleIsAConfiguration(t *testing.T) {
	doc := page(t, "configuration.md")
	_, rest, ok := strings.Cut(doc, "```json\n")
	if !ok {
		t.Fatal("docs/configuration.md shows no example")
	}
	body, _, _ := strings.Cut(rest, "```")

	var cfg config.Config
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("the example is not valid configuration: %v\n%s", err, body)
	}
	if cfg.Version == 0 || cfg.Backlog.Path == "" || len(cfg.Paths.Tests.FileGlobs) == 0 {
		t.Errorf("the example is missing the fields a reader would copy it for: %+v", cfg)
	}
}

// Every page has to be reachable, or it is a page nobody reads.
func TestEveryPageIsLinkedFromSomewhere(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "docs"))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, e := range entries {
		all.WriteString(page(t, e.Name()))
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	all.Write(readme)

	for _, e := range entries {
		others := strings.ReplaceAll(all.String(), page(t, e.Name()), "")
		if !strings.Contains(others, "("+e.Name()+")") && !strings.Contains(others, "docs/"+e.Name()+")") {
			t.Errorf("nothing links to docs/%s", e.Name())
		}
	}
}
