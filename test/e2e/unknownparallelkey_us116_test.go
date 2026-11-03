// US-116 AC-4 put to the built binary in a real repository: a project whose
// .sdlc/config.json still carries a `parallel` key from when there was a
// command that read it. Every command goes on as if the key were not there,
// `sdlc doctor` calls it a setting sdlc does not know in the words it uses for
// any other, and nothing rewrites the file. internal/config and internal/cli
// hold the same without a build.

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// unknownKeyReport is what doctor says of the configuration, from the built
// binary.
func unknownKeyReport(t *testing.T, binary, root string) (state, detail, fix string) {
	t.Helper()
	out, _ := printed(t, binary, root, "doctor", "--json")
	var payload struct {
		Checks []struct {
			Name   string `json:"name"`
			State  string `json:"state"`
			Detail string `json:"detail"`
			Fix    string `json:"fix"`
		} `json:"checks"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("sdlc doctor --json is not JSON: %v\n%s", err, out)
	}
	for _, c := range payload.Checks {
		if c.Name == "configuration" {
			return c.State, c.Detail, c.Fix
		}
	}
	t.Fatalf("doctor has no configuration check:\n%s", out)
	return "", "", ""
}

// withKey is a project, initialised and committed, whose configuration also
// holds key, set to value.
func withKey(t *testing.T, binary, key string, value any) string {
	t.Helper()
	root := project(t, binary)
	path := filepath.Join(root, ".sdlc", "config.json")
	var cfg map[string]any
	readJSON(t, path, &cfg)
	cfg[key] = value
	writeJSON(t, path, cfg)
	return root
}

func TestAC4AParallelKeyLeftInTheConfigurationChangesNoCommandAndDoctorNamesItAsAnUnknownSetting(t *testing.T) {
	parallel(t)
	binary := build(t)
	for name, value := range map[string]any{
		"a list of tools":                      map[string]any{"allow": []any{"Read", "Bash(go test:*)"}},
		"a string where the list of tools was": map[string]any{"allow": "Read"},
	} {
		t.Run(name, func(t *testing.T) {
			// What the commands print without the key is what they print with it.
			clean := project(t, binary)
			root := withKey(t, binary, "parallel", value)
			for _, args := range [][]string{{"status"}, {"story", "list"}, {"log", "--all"}} {
				want, wantErr := tool(t, binary, clean, args...)
				got, gotErr := tool(t, binary, root, args...)
				if got != want || (gotErr == nil) != (wantErr == nil) {
					t.Errorf("sdlc %s with a parallel key in the configuration:\n%s(failed: %v)\nwithout it:\n%s(failed: %v)",
						strings.Join(args, " "), got, gotErr != nil, want, wantErr != nil)
				}
			}

			// doctor names the key as it would name any other, with the same
			// fix: the same key under another name reads the same.
			const other = "unrelated_setting"
			otherRoot := withKey(t, binary, other, value)
			wantState, wantDetail, wantFix := unknownKeyReport(t, binary, otherRoot)
			if wantState != "problem" || !strings.Contains(wantDetail, other) {
				t.Fatalf("doctor does not report %s as an unknown setting (the comparison is void): %s %s", other, wantState, wantDetail)
			}
			state, detail, fix := unknownKeyReport(t, binary, root)
			if state != "problem" || !strings.Contains(detail, "parallel") {
				t.Errorf("doctor does not report parallel as an unknown setting: %s %s", state, detail)
			}
			if want := strings.ReplaceAll(wantDetail, other, "parallel"); detail != want {
				t.Errorf("doctor's words for parallel are\n%q\nbut for another unknown key, with the name changed,\n%q", detail, want)
			}
			if fix != wantFix {
				t.Errorf("doctor's fix for parallel is\n%q\nand for another unknown key\n%q", fix, wantFix)
			}

			before, err := os.ReadFile(filepath.Join(root, ".sdlc", "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			tool(t, binary, root, "status")
			after, err := os.ReadFile(filepath.Join(root, ".sdlc", "config.json"))
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Errorf("a command rewrote .sdlc/config.json:\n%s\nwas\n%s", after, before)
			}
		})
	}
}
