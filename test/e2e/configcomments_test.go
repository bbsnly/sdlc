// US-002, the comment blocks in the configuration `sdlc init` writes: AC-3 put
// to the built binary in a real repository. internal/scaffold and internal/cli
// hold the rest of the story without a build.

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// commentedSettings is the twelve top-level settings that each get a block.
var commentedSettings = []string{
	"version", "backlog", "git", "commands", "thresholds", "paths",
	"spec", "loop", "reviews", "freeze", "human_gates", "budget",
}

// objectKeys calls visit for every key of every object in raw, in document
// order, with the dotted path of the object it is in and whether that object
// has had the key already. json.Unmarshal into a map cannot say: the last of
// two equal keys wins and nothing is reported.
func objectKeys(t *testing.T, raw []byte, visit func(path, key string, repeated bool)) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func(path string) error
	walk = func(path string) error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for dec.More() {
				tok, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := tok.(string)
				if !ok {
					return fmt.Errorf("unexpected token %v where a key should be", tok)
				}
				visit(path, key, seen[key])
				seen[key] = true
				child := key
				if path != "" {
					child = path + "." + key
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case '[':
			for dec.More() {
				if err := walk(path + "[]"); err != nil {
					return err
				}
			}
		}
		_, err = dec.Token()
		return err
	}
	if err := walk(""); err != nil {
		t.Fatalf("the configuration cannot be read key by key: %v", err)
	}
}

// AC-3: `sdlc init` and then `sdlc doctor`, through the built binary, for a Go
// project and for one with no stack. The file init wrote is valid JSON, no object
// in it repeats a key, and doctor reports the configuration as ok, which is the
// loader finding no setting it does not know. Valid JSON and an ok doctor hold
// without the story as well, so the file is first asked for its blocks: without
// them the test would say nothing about the story.
func TestAC3SdlcInitThenSdlcDoctorReportsTheConfigurationWithItsBlocksAsOK(t *testing.T) {
	parallel(t)
	binary := build(t)
	for _, stack := range []struct {
		name  string
		files map[string]string
	}{
		{"a Go project", map[string]string{"go.mod": "module example.com/demo\n\ngo 1.26\n"}},
		{"no stack", nil},
	} {
		t.Run(stack.name, func(t *testing.T) {
			root := t.TempDir()
			if resolved, err := filepath.EvalSymlinks(root); err == nil {
				root = resolved
			}
			git(t, root, "init", "--quiet", "--initial-branch=main")
			for rel, body := range stack.files {
				if err := os.WriteFile(filepath.Join(root, rel), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			runTool(t, binary, root, "init")

			raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(raw) {
				t.Fatalf("the file init wrote is not valid JSON:\n%s", raw)
			}

			var top []string
			objectKeys(t, raw, func(path, key string, repeated bool) {
				if repeated {
					t.Errorf("the key %q is written twice in %q", key, path)
				}
				if path == "" {
					top = append(top, key)
				}
			})
			for _, name := range commentedSettings {
				at := slices.Index(top, name)
				if at < 1 || top[at-1] != "_"+name {
					t.Errorf("setting %q has no key %q directly before it in the file init wrote", name, "_"+name)
				}
			}

			out, _ := printed(t, binary, root, "doctor", "--json")
			var payload struct {
				Checks []struct {
					Name   string `json:"name"`
					State  string `json:"state"`
					Detail string `json:"detail"`
				} `json:"checks"`
			}
			if err := json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatalf("sdlc doctor --json is not JSON: %v\n%s", err, out)
			}
			found := false
			for _, c := range payload.Checks {
				if c.Name != "configuration" {
					continue
				}
				found = true
				if c.State != "ok" {
					t.Errorf("doctor reports the configuration as %q: %s", c.State, strings.TrimSpace(c.Detail))
				}
			}
			if !found {
				t.Errorf("doctor has no configuration check:\n%s", out)
			}
		})
	}
}
