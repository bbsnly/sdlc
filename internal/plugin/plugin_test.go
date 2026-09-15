package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/cli"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/policy"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
)

const (
	repoRoot  = "../.."
	pluginDir = repoRoot + "/plugin"
)

func TestTheManifestAndTheMarketplaceAgree(t *testing.T) {
	manifest, err := LoadManifest(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	market, err := LoadMarketplace(repoRoot)
	if err != nil {
		t.Fatal(err)
	}

	if len(market.Plugins) != 1 {
		t.Fatalf("the marketplace lists %d plugins, want 1", len(market.Plugins))
	}
	entry := market.Plugins[0]
	if entry.Name != manifest.Name {
		t.Errorf("the marketplace calls it %q and the manifest calls it %q", entry.Name, manifest.Name)
	}
	if entry.Description != manifest.Description {
		t.Error("the description a user reads in the marketplace differs from the plugin's own")
	}
	if _, err := os.Stat(filepath.Join(repoRoot, entry.Source)); err != nil {
		t.Errorf("the marketplace points at %q, which is not there", entry.Source)
	}
	if manifest.License != "MIT" {
		t.Errorf("license = %q", manifest.License)
	}
	if manifest.Version == "" || manifest.Description == "" || manifest.Homepage == "" {
		t.Errorf("manifest = %+v", manifest)
	}
}

// The `skills` CLI (`npx skills add bbsnly/sdlc`) installs the skill without
// the plugin around it. It finds skills by looking at the standard locations,
// then at what the marketplace declares, and only then by searching the whole
// repository -- so a skill this file does not declare is installed by a
// fallback that any stray SKILL.md elsewhere in the tree would change.
func TestTheMarketplaceDeclaresEverySkill(t *testing.T) {
	market, err := LoadMarketplace(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	entry := market.Plugins[0]

	declared := map[string]bool{}
	for _, rel := range entry.Skills {
		declared[path.Clean(rel)] = true
		at := filepath.Join(repoRoot, entry.Source, filepath.FromSlash(rel), "SKILL.md")
		if _, err := os.Stat(at); err != nil {
			t.Errorf("the marketplace declares the skill %q, and there is no SKILL.md there", rel)
		}
	}
	for _, skill := range skills {
		want := "skills/" + skill.Name
		if !declared[want] {
			t.Errorf("the %s skill is not declared in the marketplace: add %q to "+
				"plugins[0].skills, or `npx skills add` finds it only by searching",
				skill.Name, "./"+want)
		}
	}
}

func TestEveryAgentAndSkillIsUsable(t *testing.T) {
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) == 0 || len(skills) == 0 {
		t.Fatalf("found %d agents and %d skills", len(agents), len(skills))
	}

	for _, c := range append(agents, skills...) {
		if c.Name == "" {
			t.Errorf("%s: no name in the frontmatter", c.Path)
		}
		if c.Description == "" {
			t.Errorf("%s: no description; this is what decides whether it is ever used", c.Path)
		}
		if strings.TrimSpace(c.Body) == "" {
			t.Errorf("%s: frontmatter with no body", c.Path)
		}
	}

	for _, problem := range flagProblems(agents, skills, humanOnlySkills) {
		t.Error(problem)
	}
}

// humanOnlySkills are the skills only a person starts. The docs promise it of
// each of them, and every other skill is one the model has to be able to start.
var humanOnlySkills = map[string]bool{"trunk-review": true, "consolidate": true}

// A skill only a person starts is trusted with what it is told not to do, and
// no hook stands behind it: with no story being worked on, every write goes
// through. So what a person relies on is pinned here, as the sentence that
// says it, with the line breaks folded away. A phrase rather than a pattern,
// because a pattern that allowed the wording to drift would allow the meaning
// to drift with it; rewording one of these means rewording the promise, and
// that is worth a failing test.
func TestAHumanOnlySkillKeepsWhatItPromises(t *testing.T) {
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	promises := map[string][]string{
		"trunk-review": {
			"Do not revert, reset, amend, rebase or rewrite a commit",
			"do not edit the backlog",
			"Add nothing to the backlog until the person says which follow-ups to add",
			"Do not run it yourself",
		},
		"consolidate": {
			"the `## SDLC Contract` section of the project's `CLAUDE.md`, and only that section",
			"Never write under `~/.claude`",
			"never write under the plugin cache",
			"Before each edit run `sdlc status --json` again",
			"make no more edits and write nothing more to the proposals file",
			"goes to the person as issue text for the plugin's issue tracker, never as an edit",
			"Never propose pinning a model or a reasoning effort",
			"write nothing at all, not even the proposals file",
			"Apply it only on a yes, and only that one",
			"A proposal that loosens a check says so first",
		},
	}
	for name := range humanOnlySkills {
		if _, ok := promises[name]; !ok {
			t.Errorf("%s is human-only and nothing pins what it promises", name)
		}
	}
	for _, s := range skills {
		want, ok := promises[s.Name]
		if !ok {
			continue
		}
		delete(promises, s.Name)
		body := strings.Join(strings.Fields(s.Body), " ")
		for _, phrase := range want {
			if !strings.Contains(body, phrase) {
				t.Errorf("%s: no longer says %q", s.Path, phrase)
			}
		}
	}
	for name := range promises {
		t.Errorf("no skill named %s to hold to its promises", name)
	}
}

// flagProblems is where a skill's invocation flags break something.
//
// A skill in humanOnly carries disable-model-invocation: true, and no other
// skill does: dropping the flag lets the model start what was promised to a
// person, and adding it to another skill silently stops the model starting it.
//
// A skill carrying disable-model-invocation: true is not delivered to an agent
// through its skills: field: measured with a plugin skill (none of four runs
// delivered it, all four did without the flag), and Claude Code's own
// documentation says a skill the model cannot invoke cannot be preloaded. The
// same flag leaves /sdlc:<skill> for a person to type and keeps the model from
// starting the skill, which is what a skill only a person should run needs. So
// it is refused where an agent preloads the skill, and nowhere else: a skill
// that quietly stops being delivered is worse than one that was never written.
//
// user-invocable: false is refused on every skill. It stopped delivery the same
// way, and anywhere else it only hides the slash command a person runs the skill
// with, leaving the model what a skill with no flag already gives it.
func flagProblems(agents, skills []Component, humanOnly map[string]bool) []string {
	byName := map[string]Component{}
	var problems []string
	for _, s := range skills {
		byName[s.Name] = s
		flag, flagged := s.Front["disable-model-invocation"]
		switch {
		case humanOnly[s.Name] && flag != "true":
			problems = append(problems, fmt.Sprintf(
				"%s: only a person starts this skill, and without disable-model-invocation: true the model can", s.Path))
		case !humanOnly[s.Name] && flagged:
			problems = append(problems, fmt.Sprintf(
				"%s: disable-model-invocation: %s -- the model never starts this skill with it; "+
					"a skill only a person starts belongs in humanOnlySkills", s.Path, flag))
		}
		if v, ok := s.Front["user-invocable"]; ok {
			problems = append(problems, fmt.Sprintf(
				"%s: user-invocable: %s -- a plugin skill has no use for it: a preloaded one is not delivered, "+
					"and any other loses the slash command a person runs it with", s.Path, v))
		}
	}
	for _, a := range agents {
		for _, name := range a.List("skills") {
			s, ok := byName[strings.TrimPrefix(name, "sdlc:")]
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: skills: names %q, which this plugin does not ship", a.Path, name))
				continue
			}
			if v, ok := s.Front["disable-model-invocation"]; ok {
				problems = append(problems, fmt.Sprintf(
					"%s: disable-model-invocation: %s -- %s preloads it, and a skill carrying this is not delivered to an agent",
					s.Path, v, a.Path))
			}
		}
	}
	return problems
}

func TestOnlyASkillNoAgentPreloadsIsKeptFromTheModel(t *testing.T) {
	skill := func(name, flag string) Component {
		front := map[string]string{"name": name}
		if key, value, ok := strings.Cut(flag, ": "); ok {
			front[key] = value
		}
		return Component{Name: name, Path: "plugin/skills/" + name + "/SKILL.md", Front: front}
	}
	agent := func(skills string) Component {
		return Component{Name: "reviewer", Path: "plugin/agents/reviewer.md",
			Front: map[string]string{"name": "reviewer", "skills": skills}}
	}
	humanOnly := skill("trunk-review", "disable-model-invocation: true")
	plain := skill("next", "")
	preloadsInABlock := Component{Name: "reviewer", Path: "plugin/agents/reviewer.md",
		Front: map[string]string{"name": "reviewer", "skills": ""},
		lists: map[string][]string{"skills": {"next", "trunk-review"}}}

	for _, c := range []struct {
		name   string
		agents []Component
		skills []Component
		want   string
	}{
		{"a human-only skill no agent preloads", []Component{agent("")}, []Component{humanOnly, plain}, ""},
		{"a plain skill an agent preloads", []Component{agent("next")}, []Component{humanOnly, plain}, ""},
		{"a human-only skill an agent preloads", []Component{agent("next, trunk-review")},
			[]Component{humanOnly, plain}, "reviewer.md preloads it"},
		{"a human-only skill preloaded by its namespaced name", []Component{agent("[sdlc:trunk-review]")},
			[]Component{humanOnly, plain}, "reviewer.md preloads it"},
		{"a human-only skill preloaded from a block list", []Component{preloadsInABlock},
			[]Component{humanOnly, plain}, "reviewer.md preloads it"},
		{"a preloaded skill the plugin does not ship", []Component{agent("gone")},
			[]Component{plain}, `names "gone"`},
		{"user-invocable: false on a skill nobody preloads", []Component{agent("")},
			[]Component{skill("quiet", "user-invocable: false")}, "user-invocable: false"},
		{"a human-only skill that lost its flag", []Component{agent("")},
			[]Component{skill("trunk-review", ""), plain}, "without disable-model-invocation"},
		{"the flag on a skill the model has to start", []Component{agent("")},
			[]Component{humanOnly, skill("next", "disable-model-invocation: true")}, "belongs in humanOnlySkills"},
	} {
		got := flagProblems(c.agents, c.skills, map[string]bool{"trunk-review": true})
		switch {
		case c.want == "" && len(got) > 0:
			t.Errorf("%s: refused: %v", c.name, got)
		case c.want != "" && (len(got) != 1 || !strings.Contains(got[0], c.want)):
			t.Errorf("%s: problems = %v, want one saying %q", c.name, got, c.want)
		}
	}
}

func TestAListInTheFrontmatterReadsInEitherForm(t *testing.T) {
	for _, text := range []string{
		"---\nname: a\nskills:\n  - next\n  - \"trunk-review\"\nmodel: inherit\n---\nbody\n",
		"---\nname: a\nskills:\n  - next\n  # the one only a person starts\n  - trunk-review\n---\nbody\n",
		"---\nname: a\nskills: next, trunk-review\n---\nbody\n",
		"---\nname: a\nskills: [next, 'trunk-review']\n---\nbody\n",
	} {
		front, lists, _, err := splitFrontmatter(text)
		if err != nil {
			t.Fatal(err)
		}
		c := Component{Front: front, lists: lists}
		if got := c.List("skills"); !slices.Equal(got, []string{"next", "trunk-review"}) {
			t.Errorf("skills read from %q = %q", text, got)
		}
		if got := c.List("tools"); got != nil {
			t.Errorf("a key that is not there read as %q", got)
		}
	}

	// Items under a nested key belong to that key, not to the one above it.
	front, lists, _, err := splitFrontmatter(
		"---\nname: a\nhooks:\n  PreToolUse:\n    - matcher: Bash\nskills:\n  - next\n---\nbody\n")
	if err != nil {
		t.Fatal(err)
	}
	c := Component{Front: front, lists: lists}
	if got := c.List("hooks"); got != nil {
		t.Errorf("a nested block's items read as the list %q", got)
	}
	if got := c.List("skills"); !slices.Equal(got, []string{"next"}) {
		t.Errorf("skills after a nested block read as %q", got)
	}
}

// The file name and the frontmatter name both end up in the namespaced id a
// user types, and a mismatch between them is invisible until it is typed.
func TestAgentFileNamesMatchTheirFrontmatter(t *testing.T) {
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		base := strings.TrimSuffix(filepath.Base(a.Path), ".md")
		if base != a.Name {
			t.Errorf("%s is named %q in its frontmatter", a.Path, a.Name)
		}
	}
}

func TestSkillDirectoryNamesMatchTheirFrontmatter(t *testing.T) {
	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		dir := filepath.Base(filepath.Dir(a2p(s.Path)))
		if dir != s.Name {
			t.Errorf("%s lives in %q but is named %q", s.Path, dir, s.Name)
		}
	}
}

func a2p(p string) string { return filepath.FromSlash(p) }

// The hook is the whole enforcement story. If its command is not there, nothing
// is enforced and nothing says so.
func TestTheHookRunsSomethingThatExists(t *testing.T) {
	hooks, err := Hooks(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	entries, ok := hooks["PreToolUse"]
	if !ok || len(entries) == 0 {
		t.Fatal("no PreToolUse hook: the plugin enforces nothing")
	}

	for _, entry := range entries {
		for _, tool := range strings.Split(entry.Matcher, "|") {
			if !governed(tool) {
				t.Errorf("the hook matches %q, which the policy does not govern: "+
					"every tool call it intercepts costs the user latency", tool)
			}
		}
		for _, h := range entry.Hooks {
			if h.Type != "command" {
				t.Errorf("hook type = %q", h.Type)
			}
			if h.Timeout <= 0 {
				t.Error("the hook has no timeout; a hook that hangs hangs the session")
			}
			// Quoted, because the plugin root is under the user's home directory
			// and a space in it split the command in two: exit 127, which Claude
			// Code treats as a hook that did not object.
			rel, ok := strings.CutPrefix(h.Command, `"${CLAUDE_PLUGIN_ROOT}/`)
			if !ok {
				t.Errorf("command %q does not start with the quoted plugin root, so it "+
					"depends on where the plugin was installed", h.Command)
				continue
			}
			script, _, quoted := strings.Cut(rel, `"`)
			if !quoted {
				t.Errorf("command %q does not close its quote", h.Command)
				continue
			}
			path := filepath.Join(pluginDir, filepath.FromSlash(script))
			if _, err := os.Stat(path); err != nil {
				t.Errorf("the hook runs %s, which is not in the repository", script)
				continue
			}
			assertExecutableInGit(t, "plugin/"+script)
			if _, err := os.Stat(path + ".cmd"); err != nil {
				t.Errorf("%s has no .cmd beside it, so Windows has no launcher", script)
			}
		}
	}
}

// A command that splits on a space in the plugin root exits 127, and Claude
// Code takes a hook that failed that way for one with nothing to say: every
// rule off, and no word about it. The plugin root is under the user's home
// directory, so each command is run as a shell runs it, from a root with a
// space in it.
func TestTheHookCommandSurvivesASpaceInThePluginRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the command string is the same everywhere; this runs it where sh is certain to be")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh to run the hook command with")
	}
	root := filepath.Join(t.TempDir(), "John Smith", "plugin")
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join(pluginDir, "bin", "sdlc-hook"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "sdlc-hook"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	// The binary the launcher finds beside it says what it was handed, so the
	// event is seen to arrive and not only the launcher to start.
	stand := "#!/bin/sh\nprintf 'handed: %s\\n' \"$*\"\n"
	if err := os.WriteFile(filepath.Join(root, "bin", "sdlc"), []byte(stand), 0o755); err != nil {
		t.Fatal(err)
	}

	hooks, err := Hooks(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for event, entries := range hooks {
		for _, entry := range entries {
			for _, h := range entry.Hooks {
				cmd := exec.CommandContext(t.Context(), sh, "-c", h.Command)
				cmd.Env = []string{"CLAUDE_PLUGIN_ROOT=" + root, "PATH=" + filepath.Dir(sh)}
				cmd.Stdin = strings.NewReader("{}")
				out, err := cmd.Output()
				if err != nil {
					t.Errorf("%s: %q did not run from %q: %v", event, h.Command, root, err)
					continue
				}
				if got, want := strings.TrimSpace(string(out)), "handed: hook "+event; got != want {
					t.Errorf("%s: %q handed the binary %q, want %q", event, h.Command, got, want)
				}
			}
		}
	}
}

// The other direction: a tool that runs commands and is missing from the
// matcher never reaches the hook, and every shell rule is off for it. Monitor
// was, and `rm .sdlc/state/tests.lock` went through it.
func TestTheHookSeesEveryToolThatRunsACommand(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(pluginDir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	matched := map[string]bool{}
	for _, entry := range cfg.Hooks["PreToolUse"] {
		for _, tool := range strings.Split(entry.Matcher, "|") {
			matched[tool] = true
		}
	}
	for tool := range shellpolicy.Tools {
		if !matched[tool] {
			t.Errorf("PreToolUse does not match %s, so no shell rule applies to it", tool)
		}
	}
	for _, tool := range []string{"Write", "Edit", "MultiEdit", "NotebookEdit"} {
		if !matched[tool] {
			t.Errorf("PreToolUse does not match %s, so no file rule applies to it", tool)
		}
	}
}

// assertExecutableInGit checks the mode recorded in the index rather than on
// disk. That is the bit that survives a clone -- and on Windows the filesystem
// carries no permission bits at all, so the working copy cannot answer this.
func assertExecutableInGit(t *testing.T, repoPath string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "--stage", "--", repoPath)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files %s: %v", repoPath, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		t.Fatalf("%s is not tracked, so it would not ship", repoPath)
	}
	if fields[0] != "100755" {
		t.Errorf("%s is mode %s in the index, want 100755: the hook could not run it "+
			"after a clone (fix with: git update-index --chmod=+x %s)",
			repoPath, fields[0], repoPath)
	}
}

// governed reports whether a tool this matcher intercepts has rules waiting for
// it. Every tool call the hook sees costs the user latency, so a matcher naming
// a tool nothing governs is pure cost.
func governed(tool string) bool {
	if shellpolicy.Tools[tool] {
		// The shell is governed by its own package, because the rules a shell
		// command needs are not the rules a file path needs.
		_, refused := shellpolicy.Inspect("rm .sdlc/state/active", shellpolicy.State{CommitReady: true})
		return refused
	}
	return !policy.Evaluate(policy.Request{
		Tool: tool, Path: ".sdlc/state/active", Story: "A-1",
	}).Allowed
}

// The launcher's failure is the message a user is most likely to be the first
// to hit, and it runs before the binary that holds the error catalogue exists.
func TestTheLauncherRefusalCarriesTheSameThreeFields(t *testing.T) {
	for _, name := range []string{"bin/sdlc-hook", "bin/sdlc-hook.cmd"} {
		raw, err := os.ReadFile(filepath.Join(pluginDir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, want := range []string{"not found", "why", "fix", "sdlc doctor"} {
			if !strings.Contains(text, want) {
				t.Errorf("%s: the not-found message is missing %q", name, want)
			}
		}
		if !strings.Contains(text, `{"continue":true,`) {
			t.Errorf("%s: a missing binary must not block the tool call", name)
		}
		// Stderr from a hook that exits 0 goes to the debug log only. Without
		// systemMessage the one message a half-installed user most needs is
		// written somewhere nobody reads.
		if !strings.Contains(text, `"systemMessage":"sdlc: the sdlc binary was not found`) {
			t.Errorf("%s: the not-found message is not in systemMessage, so nobody sees it", name)
		}
	}
}

// The plugin is installed for every session, not only for projects that use
// sdlc. Without the binary, every session everywhere was told on every tool
// call that nothing was being enforced -- the moment somebody installed the
// plugin, each project they had open started complaining. The launcher is run
// here as Claude Code runs it, with no binary anywhere it looks.
func TestTheLauncherSaysNothingOutsideAProjectThatUsesSdlc(t *testing.T) {
	tmp := t.TempDir()
	mkdir := func(parts ...string) string {
		t.Helper()
		dir := filepath.Join(append([]string{tmp}, parts...)...)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	plain := mkdir("plain")
	mkdir("plain", ".git")
	nowhere := mkdir("nowhere")
	project := mkdir("project")
	mkdir("project", ".git")
	writeConfig(t, project)
	below := mkdir("project", "internal", "invoice")
	// A repository inside a directory that uses sdlc is a project of its own.
	nested := mkdir("outer", "inner")
	writeConfig(t, filepath.Join(tmp, "outer"))
	mkdir("outer", "inner", ".git")

	cases := []struct {
		name, projectDir, cwd string
		warns                 bool
	}{
		{"a repository that does not use sdlc", plain, plain, false},
		{"a directory in no repository", nowhere, nowhere, false},
		{"no project directory, run from a plain repository", "", plain, false},
		{"a repository nested in one that uses sdlc", nested, nested, false},
		{"the root of a project that uses sdlc", project, project, true},
		{"a session opened below the root of one", below, below, true},
		{"no project directory, run from inside one", "", below, true},
		{"a project directory elsewhere, run from inside one", plain, below, true},
		{"a project directory inside one, run from elsewhere", below, plain, true},
	}
	for _, l := range launchers(t) {
		for _, c := range cases {
			t.Run(l.name+"/"+c.name, func(t *testing.T) {
				out := runLauncher(t, l, c.projectDir, c.cwd, tmp)
				warned := strings.Contains(out, "the sdlc binary was not found")
				if warned != c.warns {
					t.Errorf("warned = %v, want %v; the launcher printed %q", warned, c.warns, out)
				}
				if !c.warns && strings.TrimSpace(out) != "" {
					t.Errorf("the launcher printed %q where it has nothing to say", out)
				}
			})
		}
	}
}

// writeConfig makes dir a project that uses sdlc.
func writeConfig(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".sdlc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sdlc", "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// launcher is one way the hook command can be run: the program and arguments,
// and a PATH with no sdlc on it that still has what the program needs.
type launcher struct {
	name string
	argv []string
	path string
}

// launchers lists every launcher this platform can run. The command in
// hooks.json has no extension, so on Windows it may be run by Git Bash's sh as
// well as by cmd.exe -- and there the project directory arrives written with
// backslashes. Where both can run, both are held to the same cases.
func launchers(t *testing.T) []launcher {
	t.Helper()
	var out []launcher
	if runtime.GOOS == "windows" {
		script, err := filepath.Abs(filepath.Join(pluginDir, "bin", "sdlc-hook.cmd"))
		if err != nil {
			t.Fatal(err)
		}
		system := filepath.Join(os.Getenv("SystemRoot"), "System32")
		out = append(out, launcher{"cmd", []string{"cmd", "/c", script, "PreToolUse"}, system})
	}
	if sh, err := exec.LookPath("sh"); err == nil {
		script, err := filepath.Abs(filepath.Join(pluginDir, "bin", "sdlc-hook"))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, launcher{"sh", []string{sh, script, "PreToolUse"}, filepath.Dir(sh)})
	} else if runtime.GOOS == "windows" {
		t.Log("no sh on PATH, so the POSIX launcher is not run here")
	}
	if len(out) == 0 {
		t.Skip("nothing here can run the launcher")
	}
	return out
}

// runLauncher runs a launcher with no sdlc binary on PATH or in the plugin
// root, and returns what it wrote to standard output.
func runLauncher(t *testing.T, l launcher, projectDir, cwd, pluginRoot string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), l.argv[0], l.argv[1:]...)
	path := l.path
	// Everything the launcher looks at comes from the case, and nothing from
	// the environment the tests happen to run in.
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(name) {
		case "PATH", "PWD", "SDLC_BIN", "CLAUDE_PLUGIN_ROOT", "CLAUDE_PROJECT_DIR":
			continue
		}
		cmd.Env = append(cmd.Env, kv)
	}
	cmd.Env = append(cmd.Env, "PATH="+path, "CLAUDE_PLUGIN_ROOT="+pluginRoot)
	if projectDir != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_PROJECT_DIR="+projectDir)
	}
	cmd.Dir = cwd
	cmd.Stdin = strings.NewReader("{}")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("the launcher failed: %v", err)
	}
	return string(out)
}

// ------------------------------------------------------------------ drift

var (
	sdlcCommand = regexp.MustCompile(`(?m)^\s*sdlc\s+([a-z-]+)`)
	gateRecord  = regexp.MustCompile(`sdlc gate ([a-z_]+) ([a-z]+)`)
	agentRef    = regexp.MustCompile("`sdlc:([a-z-]+)`")
)

// A skill that tells the assistant to run a command that does not exist sends
// it into an error loop, and the assistant will usually improvise instead.
func TestSkillsOnlyNameCommandsGatesAndAgentsThatExist(t *testing.T) {
	// Taken from the command tree itself, so a renamed verb fails here rather
	// than at the moment the assistant runs it.
	verbs := map[string]bool{}
	for _, c := range cli.New(strings.NewReader(""), io.Discard, io.Discard).Commands() {
		verbs[c.Name()] = true
	}
	if len(verbs) < 5 {
		t.Fatalf("only %d commands found; the tree did not build", len(verbs))
	}
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, a := range agents {
		known[a.Name] = true
	}

	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		for _, m := range sdlcCommand.FindAllStringSubmatch(s.Body, -1) {
			if !verbs[m[1]] {
				t.Errorf("%s runs `sdlc %s`, which is not a command", s.Path, m[1])
			}
		}
		for _, m := range gateRecord.FindAllStringSubmatch(s.Body, -1) {
			if !model.Gate(m[1]).Valid() {
				t.Errorf("%s records the gate %q, which does not exist", s.Path, m[1])
			}
			if !model.GateStatus(m[2]).Valid() {
				t.Errorf("%s records the outcome %q, which does not exist", s.Path, m[2])
			}
		}
		for _, m := range agentRef.FindAllStringSubmatch(s.Body, -1) {
			if !known[m[1]] {
				t.Errorf("%s delegates to `sdlc:%s`, which this plugin does not ship", s.Path, m[1])
			}
		}
	}
}

// Error codes quoted in a skill are what it branches on, so a renumbered code
// would silently change the branch taken.
func TestSkillsOnlyQuoteErrorCodesThatExist(t *testing.T) {
	codes := regexp.MustCompile(`SDLC-E[0-9]{4}`)
	docs, err := os.ReadFile(filepath.Join(repoRoot, "docs", "troubleshooting.md"))
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for _, code := range codes.FindAllString(string(docs), -1) {
		documented[code] = true
	}

	skills, err := Skills(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range skills {
		for _, code := range codes.FindAllString(s.Body, -1) {
			if !documented[code] {
				t.Errorf("%s branches on %s, which is not a code this version raises", s.Path, code)
			}
		}
	}
}

// Which model a gate runs on is the user's decision and their bill. Pinning a
// tier in an agent file would quietly override whatever they chose with
// /model, and would go stale the moment the tiers are renamed. "inherit" means
// the whole loop follows the one dial the user already turns.
func TestNoAgentPinsAModelOrAnEffort(t *testing.T) {
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		if got := a.Front["model"]; got != "inherit" {
			t.Errorf("%s: model = %q, want \"inherit\"", a.Path, got)
		}
		if got, ok := a.Front["effort"]; ok {
			t.Errorf("%s: effort = %q; effort is the user's setting, not the plugin's", a.Path, got)
		}
	}
}

// The rules every agent shares are written into each of them rather than kept
// in one skill they all preload, which would load on every spawn. Copies drift,
// so each is held to the phrase that carries it. Line breaks fall wherever the
// prose wraps, which is why the body is compared with its whitespace folded.
func TestEveryAgentCarriesTheRulesTheyShare(t *testing.T) {
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	everyone := []string{
		"You run unattended",
		"Finish what you say you will do",
		"against a tool result from this session",
		"say which ones you could not check",
		"Summarise command output; do not paste it.",
	}
	reviewers := 0
	for _, a := range agents {
		body := strings.Join(strings.Fields(a.Body), " ")
		want := everyone
		// Whoever records a review can be tempted to overstate it or to wave it through.
		if model.IsReviewRole(a.Name) {
			reviewers++
			want = append(slices.Clone(want), "Do not inflate a finding to be heard")
		}
		switch a.Name {
		case "implementer":
			want = append(slices.Clone(want),
				"swallow an error the criterion is about",
				"add a flag or hook that only a test sets")
		case "sdet":
			want = append(slices.Clone(want),
				"mark a test skipped, todo or expected to fail",
				"mock the unit the criterion is about")
		}
		for _, phrase := range want {
			if !strings.Contains(body, phrase) {
				t.Errorf("%s: does not say %q", a.Path, phrase)
			}
		}
	}
	// A reviewer whose agent file is named otherwise would be held to nothing. The
	// roster lists a role once per gate it reviews, so it is the roles that count.
	roles := map[string]bool{}
	for _, r := range model.Reviewers {
		roles[r.Role] = true
	}
	if reviewers != len(roles) {
		t.Errorf("%d agents are review roles, and the loop has %d", reviewers, len(roles))
	}
}

// Claude Code ignores these three in a plugin's agent and warns once per file
// on every load. An agent that said acceptEdits read as though its writes went
// through unasked, while in a default session each one still asked.
func TestNoAgentSetsWhatAPluginAgentCannot(t *testing.T) {
	agents, err := Agents(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range agents {
		for _, key := range []string{"permissionMode", "hooks", "mcpServers"} {
			if got, ok := a.Front[key]; ok {
				t.Errorf("%s: %s = %q, which Claude Code ignores in a plugin agent", a.Path, key, got)
			}
		}
	}
}
