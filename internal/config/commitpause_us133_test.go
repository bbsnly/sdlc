package config

// US-133 AC-1 and AC-3: the configuration names, beside the tiers that pause
// the commit gate, the paths that do. human_gates.pre_commit_pause_paths is a
// list of slash-separated globs from the project root, as git names a path.
//
// These hold the package to the file and not to a field: the key is one the
// configuration has (UnknownKeys does not name it), a list of globs loads, and
// a glob that cannot mean what it says is refused when the file loads, under
// SDLC-E0045 and naming the entry, as paths.conflict_markers_intended is. A
// glob that does not parse matches nothing, so a pause a person listed would
// be no pause at all (THREATS T5): the refusal at load is what keeps it from
// reaching the commit gate. Matching itself, and the fold of case it takes,
// is held through the commit gate in internal/cli.

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

const cpPathsKey = "pre_commit_pause_paths"

func cpHumanGates(list string) string {
	return `{"version":1,"human_gates":{"pre_commit_pause_tiers":["high"],"` + cpPathsKey + `":` + list + `}}`
}

func TestAC1PreCommitPausePathsIsASettingTheConfigurationHas(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, cpHumanGates(`["internal/hook/**","internal/cli/approve.go"]`))
	if _, err := Load(root); err != nil {
		t.Fatalf("AC-1 a list of globs under human_gates.%s did not load: %v", cpPathsKey, err)
	}
	if unknown := UnknownKeys(root); slices.Contains(unknown, "human_gates."+cpPathsKey) {
		t.Errorf("AC-1 UnknownKeys = %q: human_gates.%s is read as a misspelt setting, so it pauses nothing", unknown, cpPathsKey)
	}
}

// A misspelling of the key is still named, so a pause a person believed in and
// did not get is said by doctor.
func TestAC1AMisspeltPreCommitPausePathsIsNamedAsUnknown(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"version":1,"human_gates":{"pre_commit_pause_path":["internal/hook/**"]}}`)
	if got := UnknownKeys(root); !slices.Contains(got, "human_gates.pre_commit_pause_path") {
		t.Errorf("AC-1 UnknownKeys = %q, does not name human_gates.pre_commit_pause_path", got)
	}
}

func TestAC1PreCommitPausePathsThatAreFilesOrGlobsLoad(t *testing.T) {
	for _, list := range []string{
		`[]`,
		`["internal/cli/approve.go"]`,
		`["internal/hook/**"]`,
		`["internal/hook","internal/hook/**"]`,
		`["**/approve.go"]`,
		`["plugin/skills/*/SKILL.md","internal/cli/[af]*.go"]`,
		`["docs/price list.md"]`,
	} {
		t.Run("AC_1 "+list, func(t *testing.T) {
			root := repo(t)
			writeConfig(t, root, cpHumanGates(list))
			if _, err := Load(root); err != nil {
				t.Errorf("AC-1 Load refused %s: %v", list, err)
			}
		})
	}
}

// AC-1: a glob that does not parse is refused, and so is one that cannot be a
// slash-separated path from the root: either matches no file git names, so the
// pause a person listed would be off without a word (AC-3, THREATS T5).
func TestAC1AndAC3PreCommitPausePathsThatCannotMeanWhatTheySayAreRefusedAtLoad(t *testing.T) {
	for name, list := range map[string]string{
		"AC_1 a class that is never closed":       `["internal/[hook/**"]`,
		"AC_1 a bad class after a good glob":      `["internal/hook/**","internal/cli/[a-.go"]`,
		"AC_1 a backslash at the end":             `["internal/hook\\"]`,
		"AC_1 an empty glob":                      `[""]`,
		"AC_3 a glob from the filesystem's root":  `["/internal/hook/**"]`,
		"AC_3 a Windows separator":                `["internal\\hook\\**"]`,
		"AC_3 a glob that leaves the repository":  `["../internal/hook/**"]`,
		"AC_3 a glob that is not in its plainest": `["./internal/hook/**"]`,
	} {
		t.Run(name, func(t *testing.T) {
			root := repo(t)
			writeConfig(t, root, cpHumanGates(list))
			_, err := Load(root)
			if err == nil {
				t.Fatalf("Load accepted human_gates.%s %s", cpPathsKey, list)
			}
			var e *sdlcerr.Error
			if !errors.As(err, &e) {
				t.Fatalf("err = %v, want an sdlcerr.Error", err)
			}
			if e.Code != sdlcerr.ConfigInvalid {
				t.Errorf("code = %s, want %s", e.Code, sdlcerr.ConfigInvalid)
			}
			if said := e.What + " " + e.Why + " " + err.Error(); !strings.Contains(said, "human_gates."+cpPathsKey) {
				t.Errorf("the refusal does not name human_gates.%s: %q", cpPathsKey, said)
			}
		})
	}
}

// The refusal names which entry, so a person with a long list can find it.
func TestAC1TheRefusalOfABadPausePathNamesTheEntry(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, cpHumanGates(`["internal/hook/**","internal/cli/approve.go","internal/[x"]`))
	_, err := Load(root)
	if err == nil {
		t.Fatal("Load accepted an unclosed class")
	}
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an sdlcerr.Error", err)
	}
	said := e.What + " " + e.Why
	if !strings.Contains(said, "human_gates."+cpPathsKey+"[2]") || !strings.Contains(said, "internal/[x") {
		t.Errorf("AC-1 the refusal does not name the third entry and what it says: %q", said)
	}
	if strings.Contains(said, cpPathsKey+"[0]") || strings.Contains(said, cpPathsKey+"[1]") {
		t.Errorf("AC-1 the refusal names an entry that reads: %q", said)
	}
}

// A list is what the setting takes; a single string is a mistake to say, not
// a glob to guess at.
func TestAC1PreCommitPausePathsOfTheWrongTypeIsRefused(t *testing.T) {
	for name, value := range map[string]string{
		"AC_1 a string":        `"internal/hook/**"`,
		"AC_1 a list of lists": `[["internal/hook/**"]]`,
		"AC_1 a number":        `3`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(cpHumanGates(value)))
			if err == nil {
				t.Fatalf("Parse accepted human_gates.%s of %s", cpPathsKey, value)
			}
			if !strings.Contains(err.Error(), cpPathsKey) {
				t.Errorf("the refusal does not name %s: %v", cpPathsKey, err)
			}
		})
	}
}
