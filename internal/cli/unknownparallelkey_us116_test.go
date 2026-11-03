package cli

// US-116 AC-4: a project whose .sdlc/config.json still carries a `parallel`
// key, from when there was a command that read it, loads, every command acts as
// if the key were not there, and `sdlc doctor` calls it a setting sdlc does not
// know in the words it uses for any other, with the same fix. Nothing about
// the key is special, so the tests compare it with another unknown key rather
// than with a sentence of their own, and they go through the command surface
// so that no test names a function the implementation has yet to write.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
)

// us116Values are what a project may have left under `parallel`. The list of
// tools is what the setting took; a string where that list went was refused
// as an unreadable configuration while the setting existed, and a number,
// which no setting of this shape takes, was too.
var us116Values = map[string]any{
	"a list of tools":                      map[string]any{"allow": []any{"Read", "Bash(go test:*)"}},
	"a string where the list of tools was": map[string]any{"allow": "Read"},
	"a number":                             3,
}

// us116SetKey adds key, holding value, to the configuration sdlc init wrote.
func us116SetKey(t *testing.T, root, key string, value any) []byte {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(config.File))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg[key] = value
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

// us116ConfigurationCheck is what doctor says of the configuration.
func us116ConfigurationCheck(t *testing.T, args ...string) check {
	t.Helper()
	r := run(t, append([]string{"doctor", "--json"}, args...)...)
	var payload doctorPayload
	if err := json.Unmarshal([]byte(r.stdout), &payload); err != nil {
		t.Fatalf("doctor --json is not one JSON object: %v\n%s%s", err, r.stdout, r.stderr)
	}
	for _, c := range payload.Checks {
		if c.Name == "configuration" {
			return c
		}
	}
	t.Fatalf("doctor ran no configuration check:\n%s", r.stdout)
	return check{}
}

func TestAC4DoctorCallsAParallelKeyASettingItDoesNotKnowInTheWordsItUsesForAnyOther(t *testing.T) {
	t.Parallel()
	for name, value := range us116Values {
		t.Run(name, func(t *testing.T) {
			// Another key with the same value stands for "any other": the
			// sentence and the fix must come out the same with only the name
			// changed, which a branch written for `parallel` could not manage.
			const other = "unrelated_setting"
			root := gitProjectInitialised(t)
			us116SetKey(t, root, other, value)
			want := us116ConfigurationCheck(t)

			root = gitProjectInitialised(t)
			us116SetKey(t, root, "parallel", value)
			got := us116ConfigurationCheck(t)

			if got.State != stateProblem {
				t.Fatalf("doctor's configuration check is %q for a parallel key, want %q:\n%s", got.State, stateProblem, got.Detail)
			}
			if want.State != stateProblem || !strings.Contains(want.Detail, other) {
				t.Fatalf("doctor does not report %s as an unknown setting (the comparison is void):\n%s", other, want.Detail)
			}
			if !strings.Contains(got.Detail, "parallel") {
				t.Errorf("doctor does not name parallel:\n%s", got.Detail)
			}
			if wantDetail := strings.ReplaceAll(want.Detail, other, "parallel"); got.Detail != wantDetail {
				t.Errorf("doctor's words for parallel are\n%q\nbut for another unknown key they are, with the name changed,\n%q", got.Detail, wantDetail)
			}
			if got.Fix != want.Fix {
				t.Errorf("doctor's fix for parallel is\n%q\nand for another unknown key\n%q", got.Fix, want.Fix)
			}
		})
	}
}

func TestAC4EveryCommandActsAsIfAParallelKeyWereNotThereAndLeavesTheFileAsItIs(t *testing.T) {
	t.Parallel()
	for name, value := range us116Values {
		t.Run(name, func(t *testing.T) {
			root := gitProjectInitialised(t)
			mustRun(t, "start")
			without := map[string]result{}
			commands := [][]string{{"status"}, {"status", "--json"}, {"story", "list"}, {"log", "--all"}}
			for _, args := range commands {
				without[strings.Join(args, " ")] = run(t, args...)
			}

			raw := us116SetKey(t, root, "parallel", value)
			for _, args := range commands {
				key := strings.Join(args, " ")
				got, want := run(t, args...), without[key]
				if got != want {
					t.Errorf("sdlc %s with a parallel key in the configuration:\nexit %d, stdout %q, stderr %q\nwithout it:\nexit %d, stdout %q, stderr %q",
						key, got.code, got.stdout, got.stderr, want.code, want.stdout, want.stderr)
				}
			}
			// doctor reports the key (the test above), and still reads the
			// rest of the project: it does not stop at the configuration.
			if c := us116ConfigurationCheck(t); c.Detail == "" {
				t.Error("doctor says nothing of the configuration")
			}

			after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(config.File)))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(raw) {
				t.Errorf("a command rewrote .sdlc/config.json:\n%s\nwas\n%s", after, raw)
			}
		})
	}
}
