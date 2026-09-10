package plugin

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/cli"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/policy"
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

	// A skill flagged user-invocable: false or disable-model-invocation: true is
	// not delivered to agents through a plugin, even though the same flag works
	// outside one. A skill that quietly stops being delivered is worse than one
	// that was never written.
	for _, s := range skills {
		for _, flag := range []string{"user-invocable", "disable-model-invocation"} {
			if v, ok := s.Front[flag]; ok {
				t.Errorf("%s: %s: %s -- a plugin skill carrying this is not delivered to agents",
					s.Path, flag, v)
			}
		}
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
			if !policyGoverns(tool) {
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
			rel, ok := strings.CutPrefix(h.Command, "${CLAUDE_PLUGIN_ROOT}/")
			if !ok {
				t.Errorf("command %q is not relative to the plugin root, so it "+
					"depends on where the plugin was installed", h.Command)
				continue
			}
			script := strings.Fields(rel)[0]
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

func policyGoverns(tool string) bool {
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
		if !strings.Contains(text, `{"continue":true}`) {
			t.Errorf("%s: a missing binary must not block the tool call", name)
		}
	}
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
	for _, c := range cli.New(io.Discard, io.Discard).Commands() {
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
