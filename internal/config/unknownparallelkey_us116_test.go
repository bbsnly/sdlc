package config

// US-116 AC-4: a project's configuration that still carries a `parallel` key
// loads as if the key were not there, and UnknownKeys, which `sdlc doctor`
// reports from, names it as it names any other setting sdlc does not have. The
// key is not refused, not rewritten and not given a branch of its own: the
// tests set it beside another unknown key and expect the two to be treated alike.

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// us116Leftovers are what a project may have under `parallel`. A list of tools
// is what the setting took; a string where that list went, and a number, were
// refused as a setting of the wrong type while the setting existed.
var us116Leftovers = map[string]string{
	"a list of tools":                      `{"allow":["Read","Bash(go test:*)"]}`,
	"an empty list of tools":               `{"allow":[]}`,
	"a string where the list of tools was": `{"allow":"Read"}`,
	"a setting the old section never had":  `{"anything":true}`,
	"a number":                             `3`,
	"nothing":                              `null`,
}

func TestAC4AParallelKeyLoadsAndTheConfigurationIsTheOneWithoutIt(t *testing.T) {
	want, err := Parse([]byte(`{"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range us116Leftovers {
		t.Run(name, func(t *testing.T) {
			got, err := Parse([]byte(`{"version":1,"parallel":` + value + `}`))
			if err != nil {
				t.Fatalf("Parse refused a parallel key holding %s: %v", name, err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("a parallel key holding %s changed the configuration:\n%+v\nwant\n%+v", name, got, want)
			}

			root := repo(t)
			body := `{"version":1,"parallel":` + value + `}`
			writeConfig(t, root, body)
			loaded, err := Load(root)
			if err != nil {
				t.Fatalf("Load refused a parallel key holding %s: %v", name, err)
			}
			if !reflect.DeepEqual(loaded, want) {
				t.Errorf("a parallel key holding %s changed what Load returns:\n%+v\nwant\n%+v", name, loaded, want)
			}
			if after, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(File))); err != nil || string(after) != body {
				t.Errorf("Load rewrote the file: %q (%v)", after, err)
			}
		})
	}
}

func TestAC4UnknownKeysNamesAParallelKeyAsItNamesAnyOtherUnknownSetting(t *testing.T) {
	for name, value := range us116Leftovers {
		t.Run(name, func(t *testing.T) {
			// Alone, it is named by its own name and nothing under it: an
			// unknown key is not descended into.
			root := repo(t)
			writeConfig(t, root, `{"version":1,"parallel":`+value+`}`)
			if got := strings.Join(UnknownKeys(root), " "); got != "parallel" {
				t.Errorf("UnknownKeys = %q for a parallel key holding %s, want %q", got, name, "parallel")
			}

			// Beside another unknown key holding the same value, the two come
			// out alike, sorted among the rest.
			writeConfig(t, root, `{"version":1,"parallel":`+value+`,"unrelated_setting":`+value+`,"loop":{"max_stop_block":0}}`)
			if got, want := strings.Join(UnknownKeys(root), " "), "loop.max_stop_block parallel unrelated_setting"; got != want {
				t.Errorf("UnknownKeys = %q, want %q", got, want)
			}
		})
	}
}
