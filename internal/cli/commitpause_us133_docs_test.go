package cli

// US-133 AC-1 and AC-6 in the documentation: docs/configuration.md gives, as
// its example of human_gates.pre_commit_pause_paths, the paths that hold
// sdlc's own commands that are a person's, the session check, the freeze and
// the hook; and the three pages that describe the commit gate's pause
// describe the paths and the event.
//
// The example is held by what it does, not by how it is written: each glob
// loads as a setting, and every file of this repository that holds one of the
// things AC-1 names is matched by one of them. A file moved or renamed fails
// here first, so the example cannot drift from the tree it is the example of.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
)

// cpdHeld is each thing AC-1 names, and a file of this repository that holds it.
var cpdHeld = []struct{ what, file string }{
	{"the approve command", "internal/cli/approve.go"},
	{"the unfreeze command and the freeze", "internal/cli/freeze.go"},
	{"the freeze's test set", "internal/testset/testset.go"},
	{"the ack command", "internal/cli/ack.go"},
	{"the remap command", "internal/cli/remap.go"},
	{"the assign command", "internal/cli/staffing.go"},
	{"the session check", "internal/cli/inclaudecode.go"},
	{"the session check in the hook", "internal/hook/typedby.go"},
	{"the hook", "internal/hook/hook.go"},
	{"the hook's host profile", "internal/hook/claudecode/transcript.go"},
	{"the plugin's hook configuration", "plugin/hooks/hooks.json"},
}

// cpdSection is a heading's text with its line wrapping collapsed, failing the
// test where the page has no such heading.
func cpdSection(t *testing.T, name, heading string) string {
	t.Helper()
	text, ok := docSection(page(t, name), heading)
	if !ok {
		t.Fatalf("docs/%s has no %q", name, heading)
	}
	return text
}

// cpdPathsIn is the list under pre_commit_pause_paths, at any depth, of a JSON
// document, and whether there is one.
func cpdPathsIn(doc any) ([]string, bool) {
	v, ok := doc.(map[string]any)
	if !ok {
		return nil, false
	}
	if list, ok := v["pre_commit_pause_paths"].([]any); ok {
		var out []string
		for _, item := range list {
			s, isString := item.(string)
			if !isString {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	for _, inner := range v {
		if out, ok := cpdPathsIn(inner); ok {
			return out, true
		}
	}
	return nil, false
}

// AC-1: the human_gates section shows, in a json block of its own, a list of
// pre_commit_pause_paths that loads, and that pauses on every file of this
// repository holding approve, unfreeze, ack, remap and assign, the session
// check, the freeze and the hook.
func TestAC1TheConfigurationPageGivesSdlcsOwnPathsAsTheExampleOfPreCommitPausePaths(t *testing.T) {
	t.Parallel()
	section := cpdSection(t, "configuration.md", "## `human_gates`")
	var globs []string
	found := false
	for _, f := range ccmFences(section) {
		if f.lang != "json" || !strings.Contains(f.body, "pre_commit_pause_paths") {
			continue
		}
		var doc any
		if err := json.Unmarshal([]byte(f.body), &doc); err != nil {
			t.Fatalf("AC-1 the example of pre_commit_pause_paths is not JSON: %v\n%s", err, f.body)
		}
		globs, found = cpdPathsIn(doc)
		break
	}
	if !found || len(globs) == 0 {
		t.Fatal("AC-1 the human_gates section of docs/configuration.md has no json block listing pre_commit_pause_paths")
	}
	list, err := json.Marshal(globs)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Parse([]byte(`{"version":1,"human_gates":{"pre_commit_pause_paths":` + string(list) + `}}`)); err != nil {
		t.Errorf("AC-1 the example's globs do not load as a configuration: %v", err)
	}
	for _, h := range cpdHeld {
		if _, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(h.file))); err != nil {
			t.Errorf("%s is no longer at %s, which this test names: %v", h.what, h.file, err)
			continue
		}
		matched := false
		for _, g := range globs {
			matched = matched || config.MatchGlob(g, h.file)
		}
		if !matched {
			t.Errorf("AC-1 no glob of the example, %q, pauses on %s, which holds %s", globs, h.file, h.what)
		}
	}
}

// AC-6: each page that describes the commit gate's pause describes the paths
// and the event, and says that keeping pre_commit_pause_tiers ["high"] keeps
// every high-tier story pausing as today.
func TestAC6TheThreePagesDescribeThePathsTheEventAndKeepingTheHighTier(t *testing.T) {
	t.Parallel()
	for _, p := range []struct{ name, heading string }{
		{"configuration.md", "## `human_gates`"},
		{"the-loop.md", "## Gate 8 — `commit`"},
		{"troubleshooting.md", "### SDLC-E0037"},
	} {
		t.Run("AC-6 docs/"+p.name, func(t *testing.T) {
			t.Parallel()
			flat := cpFlat(cpdSection(t, p.name, p.heading))
			for _, want := range []string{"pre_commit_pause_paths", "landed_without_pause"} {
				if !strings.Contains(flat, want) {
					t.Errorf("AC-6 docs/%s, %s, does not describe %s", p.name, p.heading, want)
				}
			}
			// Any sentence, not the first: the table already says the default
			// is ["high"] in a sentence of its own.
			kept := false
			for s := range strings.SplitSeq(flat, ". ") {
				kept = kept || (strings.Contains(s, "pre_commit_pause_tiers") &&
					(strings.Contains(s, `["high"]`) || strings.Contains(s, `[ "high" ]`)))
			}
			if !kept {
				t.Errorf(`AC-6 docs/%s, %s, has no sentence saying that pre_commit_pause_tiers ["high"] keeps every high-tier story pausing`, p.name, p.heading)
			}
		})
	}
}
