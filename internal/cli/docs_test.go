package cli

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/policy"
	"github.com/bbsnly/sdlc/internal/scaffold"
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

// .claude-code-version was written in the first commit and read by nothing:
// no CI step installed it, no test asserted it, and CONTRIBUTING.md described
// a SDLC_ALLOW_CLI_DRIFT flag that did not exist. Meanwhile the README said
// one version and the installation page said "any recent version". Three
// statements about the same requirement, none of them checked.
func TestEveryPageAgreesOnTheClaudeCodeVersion(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("..", "..", ".claude-code-version"))
	if err != nil {
		t.Fatal(err)
	}
	pin := strings.TrimSpace(string(raw))
	if pin == "" {
		t.Fatal(".claude-code-version is empty")
	}

	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	// \W*? rather than anything that stops at `|`: the installation page
	// states it in a table row, and the first version of this test could not
	// see across the cell border -- so it checked one of the two pages.
	version := regexp.MustCompile(`Claude Code\W*?(?:v|version\s+)?(\d+\.\d+\.\d+)`)
	// Every way a page has said it, or plausibly will. A spelling this cannot
	// see is a page this test silently does not check.
	for _, said := range []string{
		"Claude Code 1.2.3", "**Claude Code** 1.2.3", "| Claude Code | 1.2.3 or newer |",
		"Claude Code v1.2.3", "Claude Code version 1.2.3",
	} {
		if !version.MatchString(said) {
			t.Fatalf("the version pattern cannot see %q", said)
		}
	}
	for _, pg := range []struct {
		name, body string
		// Required pages must state the version. A page that says nothing
		// cannot disagree, which is exactly how "any recent version" passed.
		required bool
	}{
		{"README.md", string(readme), true},
		{"docs/installation.md", page(t, "installation.md"), true},
		{"docs/getting-started.md", page(t, "getting-started.md"), false},
	} {
		found := version.FindAllStringSubmatch(pg.body, -1)
		if pg.required && len(found) == 0 {
			t.Errorf("%s states no Claude Code version; it should say %s or newer", pg.name, pin)
		}
		for _, m := range found {
			if m[1] != pin {
				t.Errorf("%s says Claude Code %s and .claude-code-version says %s",
					pg.name, m[1], pin)
			}
		}
	}
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	if !strings.Contains(page(t, "commands.md"), "troubleshooting.md") {
		t.Error("docs/commands.md never mentions the troubleshooting page")
	}
}

func TestEveryGateIsDocumented(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
		"sdlc unfreeze --reason x",
		"sdlc approve US-1",
		"sdlc ack --through HEAD",
		"echo x > CLAUDE.md",
		"sdlc review add code_review code-reviewer approve",
		"echo " + strings.Repeat("$(", 100),
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
	t.Parallel()
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
	t.Parallel()
	for _, r := range model.Reviewers {
		if _, err := os.Stat(filepath.Join("..", "..", "plugin", "agents", r.Role+".md")); err != nil {
			t.Errorf("%s reviews %s, but there is no sdlc:%s agent", r.Role, r.Gate, r.Role)
		}
	}
}

// Every agent named as a role in the registry or in the artifact table has to
// exist too, for the same reason.
func TestEveryRoleThatOwnsWorkHasAnAgent(t *testing.T) {
	t.Parallel()
	for _, a := range model.Artifacts {
		if _, err := os.Stat(filepath.Join("..", "..", "plugin", "agents", a.Role+".md")); err != nil {
			t.Errorf("%s is produced by %s, but there is no sdlc:%s agent", a.File, a.Role, a.Role)
		}
	}
}

// Every agent was told to read .sdlc/stories/<ID>/story.json, and nothing ever
// wrote one: the story is in the backlog. An agent sent to a file that is not
// there guesses at the acceptance criteria instead.
func TestThePluginNamesOnlyStoryFilesTheLoopKeeps(t *testing.T) {
	t.Parallel()
	kept := map[string]bool{model.RecordFile: true, "reviews": true}
	for _, a := range model.Artifacts {
		kept[a.File] = true
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "plugin", "agents", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	skills, err := filepath.Glob(filepath.Join("..", "..", "plugin", "skills", "*", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	inStory := regexp.MustCompile("stories/<ID>/([A-Za-z0-9_.-]+)")
	for _, f := range append(files, skills...) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "story.json") {
			t.Errorf("%s sends an agent to story.json, which the loop never writes", filepath.Base(f))
		}
		for _, m := range inStory.FindAllStringSubmatch(string(raw), -1) {
			if !kept[m[1]] {
				t.Errorf("%s names stories/<ID>/%s, which the loop does not keep", filepath.Base(f), m[1])
			}
		}
	}
}

// Rust was detected and written a whole set of commands, and no page said so:
// a reader with a Cargo.toml had no way to know init would recognise it.
func TestEveryStackInitRecognisesIsDocumented(t *testing.T) {
	t.Parallel()
	doc := page(t, "commands.md")
	for _, name := range scaffold.StackNames() {
		if !strings.Contains(doc, name+" (`") {
			t.Errorf("docs/commands.md does not say init recognises %s, or by which file", name)
		}
	}
}

// A setting nobody documented is a setting nobody can use.
func TestEverySettingIsDocumented(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// The page is headed "What init writes for a Go project", and it had drifted
// from what init writes: no smoke, fmt_file or coverage command, test dirs and
// source dirs init never chose. So it is checked against init itself, leaving
// out only the explanatory "_" keys and the commands a Go project gets empty.
func TestTheConfigurationExampleIsWhatInitWrites(t *testing.T) {
	t.Parallel()
	doc := page(t, "configuration.md")
	_, rest, _ := strings.Cut(doc, "```json\n")
	body, _, _ := strings.Cut(rest, "```")
	var shown map[string]any
	if err := json.Unmarshal([]byte(body), &shown); err != nil {
		t.Fatalf("the example is not JSON: %v", err)
	}

	root := project(t)
	writeFile(t, root, "go.mod", "module example.com/x\n")
	mustRun(t, "init")
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var written map[string]any
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatal(err)
	}
	for key := range written {
		if strings.HasPrefix(key, "_") {
			delete(written, key)
		}
	}
	if commands, ok := written["commands"].(map[string]any); ok {
		for name, command := range commands {
			if command == "" {
				delete(commands, name)
			}
		}
	}

	if !reflect.DeepEqual(shown, written) {
		want, _ := json.MarshalIndent(written, "", "  ")
		t.Errorf("docs/configuration.md shows a configuration init does not write; init writes:\n%s", want)
	}
}

// The story a new reader copies was not a backlog: it carried a "schema" field
// no backlog has, where the real one is "_schema". Decoded strictly, a field
// the loop would silently ignore is a failure here.
func TestTheStoryExampleIsABacklog(t *testing.T) {
	t.Parallel()
	_, rest, ok := strings.Cut(page(t, "getting-started.md"), "## Write a story")
	if !ok {
		t.Fatal("docs/getting-started.md no longer shows how to write a story")
	}
	_, rest, ok = strings.Cut(rest, "```json\n")
	if !ok {
		t.Fatal("docs/getting-started.md shows no example story")
	}
	body, _, _ := strings.Cut(rest, "```")

	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var backlog model.Backlog
	if err := dec.Decode(&backlog); err != nil {
		t.Fatalf("the example is not a backlog: %v\n%s", err, body)
	}
	if len(backlog.Stories) == 0 || len(backlog.Stories[0].AcceptanceCriteria) == 0 {
		t.Errorf("the example is missing the story a reader would copy it for: %+v", backlog)
	}
}

// Every page has to be reachable, or it is a page nobody reads.
func TestEveryPageIsLinkedFromSomewhere(t *testing.T) {
	t.Parallel()
	docs := filepath.Join("..", "..", "docs")
	var pages []string
	err := filepath.WalkDir(docs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		rel, err := filepath.Rel(docs, p)
		if err != nil {
			return err
		}
		pages = append(pages, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// The guides are a directory down. A walk that stopped at the top would find
	// nothing wrong with them because it would find none of them.
	if !strings.Contains(strings.Join(pages, "\n"), "guides/") {
		t.Fatalf("found no guides under docs/guides: %v", pages)
	}

	// A link is resolved from the page it is on: "../commands.md" in a guide and
	// "commands.md" beside it are the same page, and a page linking to itself
	// does not make it reachable.
	link := regexp.MustCompile(`\]\(([^)\s#]+)`)
	linked := map[string]bool{}
	for _, from := range pages {
		for _, m := range link.FindAllStringSubmatch(page(t, from), -1) {
			if to := filepath.ToSlash(filepath.Join(filepath.Dir(from), m[1])); to != from {
				linked[to] = true
			}
		}
	}
	// README.md ships where docs/ does not, so it links to the pages by URL.
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range pages {
		if !linked[p] && !strings.Contains(string(readme), "docs/"+p+")") {
			t.Errorf("nothing links to docs/%s", p)
		}
	}
}

// ------------------------------------------------------------------ US-036

// warningsSection is the "Warnings the hook prints" block of troubleshooting.md,
// up to its first subheading, with its line wrapping collapsed so a sentence
// that spans lines can be matched as one string. AC-1 and AC-2 both correct
// sentences inside it.
func warningsSection(t *testing.T) string {
	t.Helper()
	_, rest, ok := strings.Cut(page(t, "troubleshooting.md"), "## Warnings the hook prints")
	if !ok {
		t.Fatal(`docs/troubleshooting.md no longer has a "Warnings the hook prints" section`)
	}
	block, _, _ := strings.Cut(rest, "\n### ")
	return strings.Join(strings.Fields(block), " ")
}

// sentenceContaining is the first "."-bounded sentence of flat (whitespace
// already collapsed to single spaces) that holds needle, so a check can be
// about the sentence making a claim rather than the page that holds it.
func sentenceContaining(flat, needle string) (string, bool) {
	for _, s := range strings.Split(flat, ". ") {
		if strings.Contains(s, needle) {
			return s, true
		}
	}
	return "", false
}

// TestAC1AndAC4TroubleshootingNamesExactlyTheGatesThatRecheckTheFreeze drives
// a real story through every gate, breaking the frozen test immediately
// before each one is recorded and restoring it before the loop moves on, so
// the set of gate record keys that actually refuse a changed frozen test with
// SDLC-E0025 is measured from the running loop (requireEvidence's switch in
// loop.go) rather than copied from the acceptance criterion or from
// docs/enforcement.md. AC-4's evidence for this replacement is that measured
// set, not an assertion of it.
//
// It fails today because docs/troubleshooting.md still says the refusal
// lands "at the next `sdlc gate`", which is false for a story whose next gate
// to record is design_review, verifier_review, code_review or retro -- none
// of which recheck the freeze at all.
func TestAC1AndAC4TroubleshootingNamesExactlyTheGatesThatRecheckTheFreeze(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	mustRun(t, "start")

	const testFile = "internal/invoice_test.go"
	lock := filepath.Join(root, ".sdlc", "state", "tests.lock")
	var measured []string
	for _, gate := range model.Gates {
		satisfy(t, root, gate)

		// Nothing is frozen yet for dor and analysis: there is nothing to
		// break, so there is nothing to measure at this gate.
		if _, err := os.Stat(lock); err == nil {
			good, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(testFile)))
			if err != nil {
				t.Fatal(err)
			}
			writeFile(t, root, testFile, "package internal\n\n// corrupted to measure "+string(gate)+"\n")
			r := run(t, "gate", string(gate), "pass", "--note", "measuring AC-1")
			if r.code != 0 && strings.Contains(r.stderr, "SDLC-E0025") {
				measured = append(measured, string(gate))
			}
			writeFile(t, root, testFile, string(good))
		}
		mustRun(t, "gate", string(gate), "pass", "--note", "reached by the test")
	}

	want := []string{
		string(model.GateTestsFrozen), string(model.GatePlan),
		string(model.GateImplementation), string(model.GateVerification), string(model.GateCommit),
	}
	if !slices.Equal(measured, want) {
		t.Fatalf("gates that refuse a changed frozen test with SDLC-E0025 = %v, want %v", measured, want)
	}

	flat := warningsSection(t)
	if strings.Contains(flat, "next `sdlc gate`") || strings.Contains(flat, "next sdlc gate") {
		t.Error(`docs/troubleshooting.md still says a changed frozen test is refused "at the next ` +
			`sdlc gate", which is false whenever the next gate to record is one of the six just measured ` +
			`not to recheck the freeze`)
	}

	// Naming the keys is one of the two fixes the AC allows (the other drops
	// "next" instead), and if this page takes that route it has to agree with
	// docs/enforcement.md, which already names the same five correctly: all
	// five measured keys named, not some of them.
	var named []string
	for _, key := range want {
		if strings.Contains(flat, "`"+key+"`") {
			named = append(named, key)
		}
	}
	if len(named) > 0 && !slices.Equal(named, want) {
		t.Errorf("docs/troubleshooting.md names only %v of the gates that recheck the freeze, "+
			"want all of %v or none of them (dropping \"next\" instead)", named, want)
	}
}

// TestAC3AndAC4TheFrozenTestGuideBindsNarrowerToTheInterpreters checks that
// "Those are narrower" in docs/guides/when-a-frozen-test-is-wrong.md can no
// longer be read as referring to cat, grep or head -- the plural noun phrase
// it is textually closest to.
//
// AC-4's evidence: internal/shellpolicy's frozen-test-through-the-tool rule
// (in internal/policy/policy.go's registry, and exercised for real by
// shellRules above) is what makes sed, awk, gawk, perl and another
// interpreter narrower than cat, grep, head or the file tools -- a frozen
// test given to one of them as a plain argument is refused in more cases,
// anywhere it could be the program's own text and not just a file to read,
// which cat, grep and head, unable to write at all, never are. The fix is
// restoring that antecedent, not a code change, so the test checks the
// sentence makes it, the way its twin at docs/enforcement.md's
// frozen-test-through-the-tool section already does with "because each
// writes through a program of its own that this cannot read".
func TestAC3AndAC4TheFrozenTestGuideBindsNarrowerToTheInterpreters(t *testing.T) {
	t.Parallel()
	flat := strings.Join(strings.Fields(page(t, "guides/when-a-frozen-test-is-wrong.md")), " ")

	narrowerSentence, ok := sentenceContaining(flat, "narrower")
	if !ok {
		t.Fatal(`docs/guides/when-a-frozen-test-is-wrong.md no longer says anything is "narrower"`)
	}

	disambiguated := false
	for _, token := range []string{"sed", "awk", "gawk", "perl", "interpreter", "program of its own", "writes through"} {
		if strings.Contains(narrowerSentence, token) {
			disambiguated = true
			break
		}
	}
	if !disambiguated {
		t.Errorf("the sentence that says something is narrower names neither the interpreters "+
			"nor why they are narrower, so the nearest plural it can bind to is still cat, grep or "+
			"head: %q", narrowerSentence)
	}
}
