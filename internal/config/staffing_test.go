package config

// US-026: the `staffing` section of the configuration, which turns the
// staffing on and bounds it (AC-5, AC-7, AC-10).
//
//	"staffing": {
//	  "enabled": true,
//	  "models":  ["haiku", "sonnet", "opus"],          lowest first
//	  "efforts": ["low", "medium", "high", "xhigh", "max"],
//	  "lowest":  { "verifier": { "model": "opus", "effort": "high" } },
//	  "bring_back": { "security": ["internal/hook/", "install.sh"] }
//	}
//
// The tests read it through Parse and UnknownKeys, which exist, and through
// what is refused: T6 of the threats asks that a section that is on and cannot
// be used refuses to load, so that it is never read as "anything is allowed",
// and that a mistake in it is named, so that nobody believes in a protection
// that is not there. internal/cli has what the section then does, and the
// mistakes that take knowing which agents there are and which of them review
// the code: this package knows neither today, and where that is checked is the
// design's to say.

import (
	"errors"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

func staffingConfig(section string) []byte {
	return []byte(`{"version": 1, "staffing": ` + section + `}`)
}

const staffingValid = `{"enabled": true,
	"models": ["haiku", "sonnet", "opus"],
	"efforts": ["low", "medium", "high", "xhigh", "max"],
	"lowest": {"verifier": {"model": "opus", "effort": "high"}, "security": {"model": "sonnet"}, "bookkeeper": {"effort": "low"}},
	"bring_back": {"security": ["internal/hook/", "install.sh"], "perf": ["internal/hot/"], "human-advocate": ["docs/"]}}`

// AC-10: a configuration written before the setting existed loads, and so
// does one that has the section and leaves it off, whatever else it says.
func TestAC10AConfigurationWithNoStaffingSectionOrWithItOffLoads(t *testing.T) {
	for name, body := range map[string][]byte{
		"AC_10 no section":         []byte(`{"version": 1}`),
		"AC_10 enabled false":      staffingConfig(`{"enabled": false}`),
		"AC_10 an empty section":   staffingConfig(`{}`),
		"AC_10 off with the rest":  staffingConfig(strings.Replace(staffingValid, `"enabled": true`, `"enabled": false`, 1)),
		"AC_10 off with no models": staffingConfig(`{"enabled": false, "models": [], "efforts": []}`),
	} {
		if _, err := Parse(body); err != nil {
			t.Errorf("%s: the configuration does not load: %v", name, err)
		}
	}
	// And the format's version stays where it was: an older sdlc reads a file
	// this one wrote, and reports the section as a setting it does not know.
	if FormatVersion != 1 {
		t.Errorf("the configuration format is version %d, want 1: the staffing section does not change it", FormatVersion)
	}
	// The section is one the configuration has: doctor does not report it as
	// a setting it does not know.
	root := repo(t)
	writeConfig(t, root, string(staffingConfig(staffingValid)))
	if _, err := Load(root); err != nil {
		t.Fatalf("a configuration that turns the staffing on does not load: %v", err)
	}
	if got := UnknownKeys(root); got != nil {
		t.Errorf("UnknownKeys named %q in a staffing section that is spelled right", got)
	}
}

func TestAC5AMisspelledStaffingSettingIsNamed(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"version": 1, "staffing": {"enabled": true, "models": ["opus"], "efforts": ["high"],
		"lowset": {}, "bring_bak": {}, "enable": true}}`)
	got := strings.Join(UnknownKeys(root), " ")
	if want := "staffing.bring_bak staffing.enable staffing.lowset"; got != want {
		t.Errorf("UnknownKeys = %q, want %q", got, want)
	}
}

// AC-5 and T6: with the staffing on, a section the tool cannot use is refused
// with the setting named, never loaded as a staffing with no bounds.
func TestAC5AStaffingSectionThatIsOnAndCannotBeUsedDoesNotLoad(t *testing.T) {
	for _, tc := range []struct {
		name, section string
		// names is what the refusal has to name.
		names []string
	}{
		{"AC_5 no models", `{"enabled": true, "efforts": ["low"]}`, []string{"staffing.models"}},
		{"AC_5 an empty list of models", `{"enabled": true, "models": [], "efforts": ["low"]}`, []string{"staffing.models"}},
		{"AC_5 no efforts", `{"enabled": true, "models": ["opus"]}`, []string{"staffing.efforts"}},
		{"AC_5 an empty list of efforts", `{"enabled": true, "models": ["opus"], "efforts": []}`, []string{"staffing.efforts"}},
		{"AC_5 on and nothing else", `{"enabled": true}`, []string{"staffing.models", "staffing.efforts"}},
		{"AC_5 a model with no name", `{"enabled": true, "models": ["opus", ""], "efforts": ["low"]}`, []string{"staffing.models"}},
		{"AC_5 an effort with no name", `{"enabled": true, "models": ["opus"], "efforts": [" "]}`, []string{"staffing.efforts"}},
		{"AC_5 a model listed twice", `{"enabled": true, "models": ["opus", "sonnet", "opus"], "efforts": ["low"]}`, []string{"staffing.models", "opus"}},
		{"AC_5 an effort listed twice", `{"enabled": true, "models": ["opus"], "efforts": ["low", "low"]}`, []string{"staffing.efforts", "low"}},
		{"AC_5 a lowest model that is not allowed",
			`{"enabled": true, "models": ["sonnet", "opus"], "efforts": ["low"], "lowest": {"verifier": {"model": "fable"}}}`,
			[]string{"staffing.lowest", "verifier", "fable"}},
		{"AC_5 a lowest effort that is not allowed",
			`{"enabled": true, "models": ["opus"], "efforts": ["low", "high"], "lowest": {"security": {"effort": "extreme"}}}`,
			[]string{"staffing.lowest", "security", "extreme"}},
		{"AC_7 a bring_back path that names nothing",
			`{"enabled": true, "models": ["opus"], "efforts": ["low"], "bring_back": {"security": ["internal/hook/", ""]}}`,
			[]string{"staffing.bring_back", "security"}},
		{"AC_5 every problem in one message",
			`{"enabled": true, "models": [], "efforts": ["low", "low"], "lowest": {"verifier": {"effort": "extreme"}}}`,
			[]string{"staffing.models", "staffing.efforts", "staffing.lowest"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse(staffingConfig(tc.section))
			if err == nil {
				t.Fatalf("the section loaded: %+v", cfgOf(cfg))
			}
			var refusal *sdlcerr.Error
			if !errors.As(err, &refusal) || refusal.Code != sdlcerr.ConfigInvalid {
				t.Fatalf("refused with %v, want %s", err, sdlcerr.ConfigInvalid)
			}
			if refusal.Fix == "" {
				t.Error("the refusal does not say what to do")
			}
			for _, want := range tc.names {
				if !strings.Contains(refusal.Why, want) {
					t.Errorf("the refusal does not name %q: %s", want, refusal.Why)
				}
			}
		})
	}
}

// A setting of the wrong type is valid JSON, and the message names it, as it
// does for every other setting.
func TestAC5AStaffingSettingOfTheWrongTypeIsNamed(t *testing.T) {
	for setting, section := range map[string]string{
		"staffing.enabled":    `{"enabled": "yes"}`,
		"staffing.models":     `{"enabled": true, "models": "opus", "efforts": ["low"]}`,
		"staffing.efforts":    `{"enabled": true, "models": ["opus"], "efforts": {"low": true}}`,
		"staffing.lowest":     `{"enabled": true, "models": ["opus"], "efforts": ["low"], "lowest": ["verifier"]}`,
		"staffing.bring_back": `{"enabled": true, "models": ["opus"], "efforts": ["low"], "bring_back": "internal/"}`,
	} {
		_, err := Parse(staffingConfig(section))
		var refusal *sdlcerr.Error
		if !errors.As(err, &refusal) || refusal.Code != sdlcerr.ConfigUnreadable {
			t.Errorf("%s of the wrong type: %v, want %s", setting, err, sdlcerr.ConfigUnreadable)
			continue
		}
		if !strings.Contains(refusal.Why, setting) {
			t.Errorf("the refusal does not name %s: %s", setting, refusal.Why)
		}
	}
}

// What is allowed is what loads: a lowest that names only a model or only an
// effort, at either end of its list, and a bring_back for each reviewer the
// staffing may leave out of the code review.
func TestAC5AStaffingSectionInsideItsOwnBoundsLoads(t *testing.T) {
	for name, section := range map[string]string{
		"AC_5 the whole section":  staffingValid,
		"AC_5 the least there is": `{"enabled": true, "models": ["opus"], "efforts": ["high"]}`,
		"AC_5 a lowest at the bottom of both lists": `{"enabled": true, "models": ["haiku", "opus"], "efforts": ["low", "max"],
			"lowest": {"bookkeeper": {"model": "haiku", "effort": "low"}}}`,
		"AC_5 a lowest at the top of both lists": `{"enabled": true, "models": ["haiku", "opus"], "efforts": ["low", "max"],
			"lowest": {"verifier": {"model": "opus", "effort": "max"}, "project-manager": {"model": "opus"}}}`,
		"AC_7 nobody brought back": `{"enabled": true, "models": ["opus"], "efforts": ["high"], "bring_back": {}}`,
	} {
		if _, err := Parse(staffingConfig(section)); err != nil {
			t.Errorf("%s: does not load: %v", name, err)
		}
		// Loaded as the section it is, not passed over as a setting the
		// configuration does not have.
		root := repo(t)
		writeConfig(t, root, string(staffingConfig(section)))
		if got := UnknownKeys(root); got != nil {
			t.Errorf("%s: UnknownKeys named %q", name, got)
		}
	}
}
