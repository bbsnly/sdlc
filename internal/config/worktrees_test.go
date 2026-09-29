package config

// US-029.2 AC-9: git.worktrees turns the story worktrees `sdlc start` makes on
// and off, and a configuration that does not name it has them on. The key is
// API and the Go field behind it is not, so these read the setting back the way
// the file spells it: the loaded configuration, marshalled, has git.worktrees
// false only where the file turned it off.

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// worktreesSetting is git.worktrees in cfg as JSON writes it: "false", "true",
// "null", or "" where the git section leaves it out altogether.
func worktreesSetting(t *testing.T, cfg Config) string {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Git map[string]json.RawMessage `json:"git"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return string(doc.Git["worktrees"])
}

func TestAC9StoryWorktreesAreOnUnlessTheConfigurationTurnsThemOff(t *testing.T) {
	for _, tc := range []struct {
		name string
		git  string
		on   bool
	}{
		{"AC-9 the key absent", `{"trunk_branch": "main", "remote": false}`, true},
		{"AC-9 the git section absent", ``, true},
		{"AC-9 true", `{"trunk_branch": "main", "worktrees": true}`, true},
		{"AC-9 false", `{"trunk_branch": "main", "worktrees": false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repo(t)
			body := `{"version": 1}`
			if tc.git != "" {
				body = `{"version": 1, "git": ` + tc.git + `}`
			}
			writeConfig(t, root, body)
			cfg, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			got := worktreesSetting(t, cfg)
			if on := got != "false"; on != tc.on {
				t.Errorf("git.worktrees reads back as %q from %s, want on = %v", got, body, tc.on)
			}
			// false is the one value that turns them off, so it has to be
			// there to read: a switch that loads as nothing at all was never read.
			if !tc.on && got != "false" {
				t.Errorf("git.worktrees false reads back as %q", got)
			}
		})
	}
	if got := worktreesSetting(t, Default()); got == "false" {
		t.Error("the defaults turn story worktrees off, and an absent key means on")
	}
}

// A setting the configuration has is not one doctor calls misspelled.
func TestAC9TheWorktreesSwitchIsASettingTheConfigurationKnows(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"version": 1, "git": {"trunk_branch": "main", "worktrees": false, "worktree": false}}`)
	got := UnknownKeys(root)
	if slices.Contains(got, "git.worktrees") {
		t.Errorf("git.worktrees is reported as a setting sdlc does not know: %v", got)
	}
	// The misspelling beside it still is: the switch is one key, not a prefix.
	if !slices.Contains(got, "git.worktree") {
		t.Errorf("git.worktree, a misspelling, is not reported: %v", got)
	}
}

// A switch written as a string is not a switch turned on or off: read as
// neither, "no" would leave worktrees on with nothing said.
func TestAC9AWorktreesSwitchThatIsNotTrueOrFalseIsRefusedByName(t *testing.T) {
	for _, value := range []string{`"no"`, `"false"`, `0`, `{}`} {
		t.Run("AC-9 "+value, func(t *testing.T) {
			root := repo(t)
			writeConfig(t, root, `{"version": 1, "git": {"trunk_branch": "main", "worktrees": `+value+`}}`)
			_, err := Load(root)
			if err == nil {
				t.Fatalf("git.worktrees %s was accepted", value)
			}
			if got := codeOf(t, err); got != sdlcerr.ConfigUnreadable {
				t.Errorf("git.worktrees %s was refused with %s", value, got)
			}
			var e *sdlcerr.Error
			if !errors.As(err, &e) || !strings.Contains(e.Why, "git.worktrees") || !strings.Contains(e.Why, "true or false") {
				t.Errorf("the refusal does not name git.worktrees and what it takes: %v", err)
			}
		})
	}
}
