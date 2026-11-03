package scaffold

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

const rpPolicy = "REVIEW.md"

// rpSections is a policy's `## ` sections, each by the words of its heading in
// lower case, with its lines joined so that where a line wraps decides nothing.
func rpSections(t *testing.T, text string) map[string]string {
	t.Helper()
	sections := map[string]string{}
	parts := regexp.MustCompile(`(?m)^## `).Split(text, -1)
	for _, part := range parts[1:] {
		heading, body, _ := strings.Cut(part, "\n")
		sections[strings.ToLower(strings.TrimSpace(heading))] = strings.Join(strings.Fields(body), " ")
	}
	return sections
}

// rpSection is the one section whose heading holds the word and none of the
// words that mark another section, so that a statement has to be under its own
// heading and not merely somewhere in the file.
func rpSection(t *testing.T, text, word string, not ...string) string {
	t.Helper()
	var found []string
	for heading, body := range rpSections(t, text) {
		if !strings.Contains(heading, word) || slices.ContainsFunc(not, func(n string) bool { return strings.Contains(heading, n) }) {
			continue
		}
		found = append(found, body)
	}
	if len(found) != 1 {
		t.Fatalf("the policy has %d sections for %q, want 1:\n%s", len(found), word, text)
	}
	return found[0]
}

func rpInit(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		touch(t, root, rel, body)
	}
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	return root, read(t, root, rpPolicy)
}

// configuredPolicy is reviews.policy as the file on disk has it, read as a
// person's editor would, with no help from the configuration's type.
func configuredPolicy(t *testing.T, root string) string {
	t.Helper()
	var doc struct {
		Reviews struct {
			Gate7Advisory bool   `json:"gate7_advisory"`
			Policy        string `json:"policy"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal([]byte(read(t, root, config.File)), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Reviews.Policy
}

// US-115 AC-2. Each statement is looked for by itself under the heading it
// belongs to: one pasted sentence, or a list under the wrong heading, does not
// pass.
func TestAC2InitWritesAPolicyWithEverySectionUnderItsOwnHeading(t *testing.T) {
	t.Parallel()
	_, text := rpInit(t, map[string]string{"go.mod": "module example.com/x\n"})

	t.Run("AC-2 the passes, in order", func(t *testing.T) {
		t.Parallel()
		section := strings.ToLower(rpSection(t, text, "pass"))
		at := -1
		for _, pass := range []string{"criteria", "design", "security", "tests", "documents and messages"} {
			i := strings.Index(section, pass)
			if i < 0 {
				t.Errorf("the passes do not name %q: %s", pass, section)
				continue
			}
			if i < at {
				t.Errorf("%q comes before the pass it follows: %s", pass, section)
			}
			at = i
		}
	})
	t.Run("AC-2 what counts as important", func(t *testing.T) {
		t.Parallel()
		section := strings.ToLower(rpSection(t, text, "important"))
		for _, row := range []string{
			"criterion not met", "defect", "security or data hole", "false statement", "architecture rule",
			// Q4: what a role already blocks on has to count, or a block on it
			// could not name an important finding.
			"gaming", "plan deviation", "ignored design finding", "performance budget",
		} {
			t.Run(row, func(t *testing.T) {
				t.Parallel()
				if !strings.Contains(section, row) {
					t.Errorf("what counts as important does not name %q: %s", row, section)
				}
			})
		}
	})
	t.Run("AC-2 what counts as a nit", func(t *testing.T) {
		t.Parallel()
		section := strings.ToLower(rpSection(t, text, "nit", "cap"))
		for _, row := range []string{"naming", "wording", "layout"} {
			if !strings.Contains(section, row) {
				t.Errorf("what counts as a nit does not name %q: %s", row, section)
			}
		}
		if !strings.Contains(section, "not wrong") {
			t.Errorf("a nit is not said to be what is not wrong: %s", section)
		}
	})
	t.Run("AC-2 the nit cap", func(t *testing.T) {
		t.Parallel()
		section := strings.ToLower(rpSection(t, text, "cap"))
		if !regexp.MustCompile(`\b5\b`).MatchString(section) || !strings.Contains(section, "per review") {
			t.Errorf("the cap is not 5 per review: %s", section)
		}
		if !strings.Contains(section, "count") {
			t.Errorf("the rest are not said to be given as a count: %s", section)
		}
	})
	t.Run("AC-2 what not to review", func(t *testing.T) {
		t.Parallel()
		section := strings.ToLower(rpSection(t, text, "not to review"))
		for _, row := range []string{"generated files", "command"} {
			if !strings.Contains(section, row) {
				t.Errorf("what not to review does not name %q: %s", row, section)
			}
		}
	})
}

// The commands init wrote are the non-empty ones of the stack it recognised, so
// the list changes with the project: a list written once for Go names a linter
// a Python project does not have. An unrecognised stack wrote none.
func TestAC2ThePolicyNamesOnlyTheCommandsInitWrote(t *testing.T) {
	t.Parallel()
	every := []string{"smoke", "build", "test", "lint", "fmt", "fmt_check", "fmt_file", "coverage", "mutation"}
	for name, files := range map[string]map[string]string{
		"an unrecognised stack": nil,
		"go":                    {"go.mod": "module example.com/x\n"},
		"rust":                  {"Cargo.toml": "[package]\nname = \"x\"\n"},
		"python":                {"pyproject.toml": "[project]\nname = \"x\"\n"},
		"node with one script":  {"package.json": `{"scripts":{"test":"jest"}}`},
	} {
		t.Run("AC-2 "+name, func(t *testing.T) {
			t.Parallel()
			root, text := rpInit(t, files)
			cfg, err := config.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			section := rpSection(t, text, "not to review")
			for _, key := range every {
				command := cfg.Commands[key]
				named := strings.Contains(section, "`"+key+"`")
				switch {
				case command == "" && named:
					t.Errorf("%s has no %s command, and the policy names one: %s", name, key, section)
				case command != "" && !named && !strings.Contains(section, command):
					t.Errorf("init wrote the %s command %q, and the policy does not name it: %s", key, command, section)
				}
			}
			if strings.Contains(text, "{{") || strings.Contains(text, "<no value>") {
				t.Errorf("the template was not filled in:\n%s", text)
			}
		})
	}
}

// markdownlint does not read the templates, so this test is what holds them, as
// TestContractFragmentIsWellFormedMarkdown does the contract's.
func TestAC2ThePolicyIsWellFormedMarkdownWithATitle(t *testing.T) {
	t.Parallel()
	for _, files := range []map[string]string{nil, {"go.mod": "module example.com/x\n"}} {
		_, text := rpInit(t, files)
		if !strings.HasPrefix(text, "# ") {
			t.Errorf("the policy does not open with a title:\n%s", text)
		}
		if !strings.HasSuffix(text, "\n") || strings.HasSuffix(text, "\n\n") {
			t.Errorf("the policy does not end with one newline:\n%q", text)
		}
		lines := strings.Split(text, "\n")
		for i, line := range lines[:len(lines)-1] {
			if strings.HasPrefix(line, "#") && strings.TrimSpace(lines[i+1]) != "" {
				t.Errorf("line %d: a heading needs a blank line after it: %q", i+1, line)
			}
			if strings.HasPrefix(line, "- ") && i > 0 {
				prev := lines[i-1]
				if strings.TrimSpace(prev) != "" && !strings.HasPrefix(prev, "- ") && !strings.HasPrefix(prev, "  ") {
					t.Errorf("line %d: a list needs a blank line before it: %q", i+1, line)
				}
			}
		}
	}
}

func TestAC2InitSaysItWroteThePolicyAndKeepsOneThatIsThere(t *testing.T) {
	t.Parallel()
	t.Run("AC-2 a new project", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		res, err := Init(root, false)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(res.Created, rpPolicy) || slices.Contains(res.Kept, rpPolicy) {
			t.Errorf("Created = %v, Kept = %v", res.Created, res.Kept)
		}
		if _, err := os.Stat(filepath.Join(root, rpPolicy)); err != nil {
			t.Error(err)
		}
	})
	t.Run("AC-2 one that is there", func(t *testing.T) {
		t.Parallel()
		const mine = "# Ours\n\nWe review what we like.\n"
		root := t.TempDir()
		touch(t, root, rpPolicy, mine)
		res, err := Init(root, false)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(res.Kept, rpPolicy) || slices.Contains(res.Created, rpPolicy) {
			t.Errorf("Created = %v, Kept = %v", res.Created, res.Kept)
		}
		if got := read(t, root, rpPolicy); got != mine {
			t.Errorf("the policy was changed:\n%s", got)
		}
	})
	t.Run("AC-2 --force keeps it too", func(t *testing.T) {
		t.Parallel()
		const mine = "# Ours\n"
		root := t.TempDir()
		if _, err := Init(root, false); err != nil {
			t.Fatal(err)
		}
		touch(t, root, rpPolicy, mine)
		res, err := Init(root, true)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(res.Kept, rpPolicy) {
			t.Errorf("Kept = %v", res.Kept)
		}
		if got := read(t, root, rpPolicy); got != mine {
			t.Errorf("--force changed the policy:\n%s", got)
		}
	})
	t.Run("AC-2 in a linked worktree it goes in that checkout", func(t *testing.T) {
		t.Parallel()
		root, shared := t.TempDir(), t.TempDir()
		res, err := InitAt(root, shared, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(root, rpPolicy)); err != nil {
			t.Errorf("the checkout has no policy: %v", err)
		}
		if _, err := os.Stat(filepath.Join(shared, rpPolicy)); err == nil {
			t.Error("the policy went to the main checkout, which reviewers of this one do not read")
		}
		if !slices.Contains(res.Created, rpPolicy) {
			t.Errorf("Created = %v, want the path as the checkout's", res.Created)
		}
	})
}

// The person's Q6: reviews.policy says where this repository keeps a file, as
// backlog.path and the trunk do, so --force keeps it while it restores settings.
func TestAC1InitForceKeepsACustomPolicyPath(t *testing.T) {
	t.Parallel()
	t.Run("AC-1 the path and the file at it", func(t *testing.T) {
		t.Parallel()
		const mine = "# Ours\n"
		root := t.TempDir()
		if _, err := Init(root, false); err != nil {
			t.Fatal(err)
		}
		touch(t, root, "docs/review-policy.md", mine)
		touch(t, root, config.File, `{"version":1,"reviews":{"policy":"docs/review-policy.md","gate7_advisory":true}}`)
		if err := os.Remove(filepath.Join(root, rpPolicy)); err != nil {
			t.Fatal(err)
		}
		res, err := Init(root, true)
		if err != nil {
			t.Fatal(err)
		}
		if got := configuredPolicy(t, root); got != "docs/review-policy.md" {
			t.Errorf("--force left reviews.policy as %q", got)
		}
		cfg, err := config.Load(root)
		if err != nil {
			t.Fatalf("the configuration --force wrote does not load: %v", err)
		}
		if cfg.Reviews.Gate7Advisory {
			t.Error("--force did not restore reviews.gate7_advisory, a setting it is for")
		}
		if unknown := config.UnknownKeys(root); len(unknown) > 0 {
			t.Errorf("the configuration --force wrote has settings sdlc does not know: %v", unknown)
		}
		if got := read(t, root, "docs/review-policy.md"); got != mine {
			t.Errorf("--force changed the policy:\n%s", got)
		}
		if !slices.Contains(res.Kept, "docs/review-policy.md") {
			t.Errorf("Kept = %v", res.Kept)
		}
		if _, err := os.Stat(filepath.Join(root, rpPolicy)); err == nil {
			t.Error("--force wrote a default policy nothing reads, beside the one configured")
		}
	})
	// The architect's note on step 9 and red team's gaming vector: a path joined
	// into the JSON with no encoding writes a configuration that will not load, or a
	// different path. A letter outside the BMP is where %q is not JSON. Where the
	// validation refuses the path instead, --force has nothing to keep.
	for name, path := range map[string]string{
		"a quotation mark":             `docs/a"b.md`,
		"a backslash":                  `docs/a\b.md`,
		"a letter outside the BMP":     "docs/" + string(rune(0x1d4ab)) + ".md",
		"a quotation mark and a comma": `docs/a","x":"b.md`,
	} {
		t.Run("AC-1 the path as written: "+name, func(t *testing.T) {
			t.Parallel()
			if runtime.GOOS == "windows" && strings.ContainsAny(path, `"\`) {
				t.Skip("Windows has no file name holding this character")
			}
			root := t.TempDir()
			if _, err := Init(root, false); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(map[string]any{"version": 1, "reviews": map[string]any{"policy": path}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := config.Parse(raw); err != nil {
				return
			}
			touch(t, root, config.File, string(raw))
			if _, err := Init(root, true); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(root)
			if err != nil {
				t.Fatalf("the configuration --force wrote does not load: %v", err)
			}
			if got := configuredPolicy(t, root); got != path {
				t.Errorf("--force left reviews.policy as %q, want %q", got, path)
			}
			if got := cfg.ReviewPolicyPath(); got != path {
				t.Errorf("ReviewPolicyPath = %q, want %q", got, path)
			}
			if unknown := config.UnknownKeys(root); len(unknown) > 0 {
				t.Errorf("the configuration --force wrote has settings sdlc does not know: %v", unknown)
			}
		})
	}
	t.Run("AC-2 a configured path with no file gets the template there", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if _, err := Init(root, false); err != nil {
			t.Fatal(err)
		}
		touch(t, root, config.File, `{"version":1,"reviews":{"policy":"later/policy.md"}}`)
		if err := os.Remove(filepath.Join(root, rpPolicy)); err != nil {
			t.Fatal(err)
		}
		res, err := Init(root, true)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(res.Created, "later/policy.md") {
			t.Errorf("Created = %v", res.Created)
		}
		if !strings.Contains(read(t, root, "later/policy.md"), "## ") {
			t.Error("the file at the configured path is not the template")
		}
	})
	for name, broken := range map[string]string{
		"not JSON":            `{not json`,
		"a policy it refuses": `{"version":1,"reviews":{"policy":"../outside.md"}}`,
		"a control in a path": `{"version":1,"reviews":{"policy":"a\nb.md"}}`,
	} {
		t.Run("AC-1 a configuration that does not read has nothing to keep: "+name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			if _, err := Init(root, false); err != nil {
				t.Fatal(err)
			}
			touch(t, root, config.File, broken)
			if _, err := Init(root, true); err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(root); err != nil {
				t.Fatalf("--force left a configuration that does not load: %v", err)
			}
			if got := configuredPolicy(t, root); got != "" && got != rpPolicy {
				t.Errorf("reviews.policy is %q", got)
			}
			if !strings.Contains(read(t, root, rpPolicy), "## ") {
				t.Error("the default policy is not there")
			}
		})
	}
	t.Run("AC-1 --force writes nothing about a policy nobody named", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if _, err := Init(root, false); err != nil {
			t.Fatal(err)
		}
		if _, err := Init(root, true); err != nil {
			t.Fatal(err)
		}
		if got := configuredPolicy(t, root); got != "" && got != rpPolicy {
			t.Errorf("reviews.policy is %q", got)
		}
		if !strings.Contains(read(t, root, rpPolicy), "## ") {
			t.Error("the default policy is not there")
		}
	})
}

// A policy that cannot be written fails init, saying which file, as every other
// file init writes does. Nothing is reported as written.
func TestAC2InitReportsAPolicyItCouldNotWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if _, err := Init(root, false); err != nil {
		t.Fatal(err)
	}
	touch(t, root, "blocked", "a file where the policy's directory would be\n")
	touch(t, root, config.File, `{"version":1,"reviews":{"policy":"blocked/policy.md"}}`)
	res, err := Init(root, true)
	if err == nil {
		t.Fatalf("init reported success: %+v", res)
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) || e.Code != sdlcerr.StateUnwritable {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(e.What, "blocked/policy.md") {
		t.Errorf("the refusal does not name the file: %s", e.What)
	}
}
