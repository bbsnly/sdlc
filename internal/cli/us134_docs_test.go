package cli

// US-134 AC-8, AC-10 and AC-17: the pages a person reads say what the binary now
// runs, where and when. What the configuration page has to show of each stack is
// read from what `sdlc init` writes for it, never listed here, so a command
// changed in init and not on the page fails.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/scaffold"
)

// us134Section is the text under heading on the page name, with its lines
// joined, so that where a line wraps decides nothing.
func us134Section(t *testing.T, name, heading string) string {
	t.Helper()
	text, ok := docSection(page(t, name), heading)
	if !ok {
		t.Fatalf("docs/%s has no %q", name, heading)
	}
	return strings.Join(strings.Fields(text), " ")
}

func us134Says(t *testing.T, where, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s does not say %q", where, w)
		}
	}
}

// us134Stacks is the commands `sdlc init` writes for each stack, by name.
func us134Stacks(t *testing.T) map[string]map[string]string {
	t.Helper()
	markers := map[string]map[string]string{
		"Go":     {"go.mod": "module example.com/demo\n"},
		"Rust":   {"Cargo.toml": "[package]\nname = \"demo\"\n"},
		"Node":   {"package.json": `{"scripts":{"test":"vitest run","build":"tsc"}}`},
		"Python": {"pyproject.toml": "[project]\nname = \"demo\"\n"},
	}
	stacks := map[string]map[string]string{}
	for _, name := range scaffold.StackNames() {
		files, ok := markers[name]
		if !ok {
			t.Fatalf("init supports %s, which this test has no project for", name)
		}
		root := t.TempDir()
		for rel, body := range files {
			if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		stack, _ := scaffold.Detect(root)
		stacks[name] = stack.Commands
	}
	return stacks
}

// AC-8: `sdlc gate` says the implementation pass runs the configured commands,
// what a refusal carries and how it counts, and what it records; `sdlc freeze`
// says it runs the new and changed tests and refuses where they pass.
func TestAC8TheCommandsPageSaysWhatTheGateAndTheFreezeRun(t *testing.T) {
	t.Parallel()
	gate := us134Section(t, "commands.md", "## `sdlc gate GATE STATUS`")
	us134Says(t, "AC-8 docs/commands.md under `sdlc gate`", gate,
		"sdlc gate implementation pass", "SDLC-E0095", ".sdlc/stories/", "SDLC_RUN_DIR", "tree hash",
		"loop.max_rework_rounds")
	freeze := us134Section(t, "commands.md", "## `sdlc freeze`")
	us134Says(t, "AC-8 docs/commands.md under `sdlc freeze`", freeze,
		"commands.test_files", "SDLC_TEST_FILES", "commands.test", ".sdlc/stories/", "pass")
}

// AC-8: the loop's page says it at Gates 3, 5 and 6.
func TestAC8TheLoopSaysItAtGates3And5And6(t *testing.T) {
	t.Parallel()
	us134Says(t, "AC-8 docs/the-loop.md at Gate 3", us134Section(t, "the-loop.md", "## Gate 3 — `tests_frozen`"),
		"commands.test_files", "sdlc freeze", "refuse")
	us134Says(t, "AC-8 docs/the-loop.md at Gate 5", us134Section(t, "the-loop.md", "## Gate 5 — `implementation`"),
		"sdlc gate implementation pass", "SDLC-E0095", "loop.max_rework_rounds", "commands.test_files")
	us134Says(t, "AC-8 docs/the-loop.md at Gate 6", us134Section(t, "the-loop.md", "## Gate 6 — `verification`, then `verifier_review`"),
		"run by sdlc", "tree hash")
}

// AC-10 and AC-17: the configuration page documents the narrow command and the
// two variables, and shows, for every stack init supports, the commands.test_files
// init writes and the test and coverage pair that shares one run; init writes no
// coverage for some stacks, and the page shows a pair for them all the same.
func TestAC10AndAC17TheConfigurationPageShowsEachStacksPairAndNarrowCommand(t *testing.T) {
	t.Parallel()
	doc := page(t, "configuration.md")
	us134Says(t, "AC-17 docs/configuration.md", doc, "commands.test_files", "SDLC_TEST_FILES", "SDLC_RUN_DIR")
	for name, commands := range us134Stacks(t) {
		for _, key := range []string{"test", "coverage", "test_files"} {
			command := commands[key]
			if command == "" {
				if key == "test_files" {
					t.Errorf("AC-17 init writes no commands.test_files for %s", name)
				}
				continue
			}
			if !slices.ContainsFunc(us134Spellings(t, command), func(s string) bool { return strings.Contains(doc, s) }) {
				t.Errorf("AC-10 and AC-17 docs/configuration.md does not show the commands.%s init writes for %s: %s", key, name, command)
			}
		}
	}
	// The run directory's own section, with any subsections it has: every stack
	// named in it, each with a test and a coverage that use the directory. The
	// smallest section below the page's title that holds them all, so that one
	// table, one block or a subsection a stack are each a way to write it.
	stacks := scaffold.StackNames()
	holds := func(section string) bool {
		return strings.Count(section, "SDLC_RUN_DIR") >= 2*len(stacks) &&
			!slices.ContainsFunc(stacks, func(name string) bool { return !strings.Contains(section, name) })
	}
	section, found := "", false
	for _, heading := range us134Headings(doc) {
		if text, ok := docSection(doc, heading); ok && holds(text) && (!found || len(text) < len(section)) {
			section, found = text, true
		}
	}
	if !found {
		t.Errorf("AC-10 docs/configuration.md has no section on SDLC_RUN_DIR that shows a test and a coverage using it for each of %q", stacks)
	}
}

// us134Spellings is command as the page may show it: as it is, in a JSON
// string as a person writes one, or as init writes one, with <, > and &
// escaped.
func us134Spellings(t *testing.T, command string) []string {
	t.Helper()
	var plain strings.Builder
	enc := json.NewEncoder(&plain)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(command); err != nil {
		t.Fatal(err)
	}
	escaped, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	unquote := func(s string) string { return strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(s), `"`), `"`) }
	return []string{command, unquote(plain.String()), unquote(string(escaped))}
}

// us134Headings is every heading of doc below its title, outside fenced code:
// a shell comment in an example is not one.
func us134Headings(doc string) []string {
	var out []string
	fenced := false
	for line := range strings.SplitSeq(doc, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if hashes := len(line) - len(strings.TrimLeft(line, "#")); !fenced && hashes > 1 && strings.HasPrefix(line[hashes:], " ") {
			out = append(out, strings.TrimRight(line, " "))
		}
	}
	return out
}

// us134Sentences is text cut at each stop that a space or the end follows, so
// that `commands.test_files` and `.sdlc/config.json` are not cut in two.
var us134Sentences = regexp.MustCompile(`(?:[^.!?;]|[.!?;][^\s])+[.!?;]?`)

// us134FreezeSays holds that one sentence of the `sdlc freeze` section matches
// every pattern: the meaning, in whatever words the page puts it.
func us134FreezeSays(t *testing.T, what, text string, all ...*regexp.Regexp) {
	t.Helper()
	for _, s := range us134Sentences.FindAllString(text, -1) {
		if !slices.ContainsFunc(all, func(re *regexp.Regexp) bool { return !re.MatchString(s) }) {
			return
		}
	}
	t.Errorf("AC-13 docs/commands.md under `sdlc freeze` has no sentence saying %s:\n%s", what, text)
}

// AC-8 and AC-13, security review F-4: `sdlc freeze` says what a freeze it took
// proves, that the narrow command exited non-zero, and what it does not, why:
// a runner that found no tests, or a wrong commands.test_files, exits non-zero
// too. The test author's own run and the test reviewer are what show each test
// failing for the right reason.
func TestAC8AndAC13TheFreezeSectionSaysATakenFreezeProvesANonZeroExitAndNotWhy(t *testing.T) {
	t.Parallel()
	freeze := us134Section(t, "commands.md", "## `sdlc freeze`")
	us134FreezeSays(t, "that a taken freeze proves a non-zero exit and not why", freeze,
		regexp.MustCompile(`(?i)non-?zero|other than (0|zero)\b`),
		regexp.MustCompile(`(?i)\b(not|never|no|nothing)\b|n't\b`),
		regexp.MustCompile(`(?i)\b(why|reason)\b`))
	us134FreezeSays(t, "that a runner that found no tests exits non-zero too", freeze,
		regexp.MustCompile(`(?i)\b(found|finds|collected|collects|ran|runs) no tests?\b|\bno tests? (were |was )?(found|collected)\b`))
	us134FreezeSays(t, "that a wrong commands.test_files exits non-zero too", freeze,
		regexp.MustCompile("(?i)\\b(wrong|mistaken|broken|misconfigured|faulty)\\b[^.;]*commands\\.test_files|commands\\.test_files`?[^.;]*\\b(wrong|mistaken|broken|misconfigured|faulty)\\b"))
	us134FreezeSays(t, "that the test author's own run and the test reviewer show each test failing for the right reason", freeze,
		regexp.MustCompile(`(?i)\b(SDET|test author)\b`),
		regexp.MustCompile(`(?i)\breviewer\b`),
		regexp.MustCompile(`(?i)\bright reason\b`))
}

// us134NotReadBy is what commands-run-as-configured cannot read and the page
// has to say (design review, security round 3, F-19), each as the patterns
// one passage must all match: the binary run under another name, inline
// interpreter code that builds the name, and a made word that is the whole
// call behind a program. Every one of them is allowed by the rule, so the
// docs sentence that a result `run by sdlc` was run from a line that changed
// nothing is true only of a line the rule could read.
var us134NotReadBy = []struct {
	what string
	all  []*regexp.Regexp
}{
	{"the binary run under another name: a copy, a hard link, or `go build -o`", []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bcop(y|ies|ied)\b|\bcp\b`),
		regexp.MustCompile(`(?i)\bhard[- ]links?\b`),
		regexp.MustCompile(`go build -o`),
	}},
	{"inline interpreter code that builds the name", []*regexp.Regexp{
		regexp.MustCompile(`(?i)\binterpreters?\b|python3? -c|node -e|perl -e`),
	}},
	{"a made word that is the whole call behind a program", []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsubstitutions?\b|\bmade\b|\$\(`),
		regexp.MustCompile(`(?i)\bwhole\b|\bentire\b`),
		regexp.MustCompile(`(?i)\bbehind\b|\bwrappers?\b`),
	}},
}

// AC-1, AC-8 and AC-13, security round 3 F-19: docs/enforcement.md says, in the
// rule's own section and in Not held, what the rule cannot read, and the pages
// say a result `run by sdlc` was run from a line the rule could read, not from
// a line that changed nothing.
func TestAC1AC8AC13TheEnforcementPageSaysWhatTheConfiguredRunRuleCannotRead(t *testing.T) {
	t.Parallel()
	doc := page(t, "enforcement.md")
	rule, ok := docSection(doc, "### `commands-run-as-configured`")
	if !ok {
		t.Fatal("AC-1 AC-13 docs/enforcement.md has no section headed ### `commands-run-as-configured`")
	}
	notHeld, ok := docSection(doc, "### Not held")
	if !ok {
		t.Fatal("docs/enforcement.md has no ### Not held")
	}
	var held []string
	for _, b := range docBullets(notHeld) {
		if strings.Contains(b, "commands-run-as-configured") {
			held = append(held, b)
		}
	}
	for _, where := range []struct{ name, text string }{
		{"the `commands-run-as-configured` section", rule},
		{"the Not held bullets that name commands-run-as-configured", strings.Join(held, "\n")},
	} {
		text := strings.Join(strings.Fields(where.text), " ")
		for _, gap := range us134NotReadBy {
			if slices.ContainsFunc(gap.all, func(re *regexp.Regexp) bool { return !re.MatchString(text) }) {
				t.Errorf("AC-1 AC-13 docs/enforcement.md, in %s, does not name %s", where.name, gap.what)
			}
		}
	}
	for _, name := range []string{"enforcement.md", "commands.md"} {
		if text := strings.Join(strings.Fields(page(t, name)), " "); !strings.Contains(text, "from a line the rule could read") {
			t.Errorf("AC-8 docs/%s does not say that a result run by sdlc was run from a line the rule could read", name)
		}
	}
}
