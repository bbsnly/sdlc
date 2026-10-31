package config

// US-114 AC-5: consolidate.nudge_after is a setting of the configuration. 3
// when absent, 0 to turn the nudge off, and a value the loop cannot honour is
// refused naming the setting, as every other limit is.
//
// The setting is read through the JSON a Config encodes to, and not through a
// field of the struct, so that these tests compile before the field exists and
// fail for the behaviour that is missing: the name of the Go field is the
// implementation's to choose, the key in the file is not.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// us114NudgeAfter is what a Config says consolidate.nudge_after is, and whether
// it says anything: the encoding of a Config is where the key shows.
func us114NudgeAfter(t *testing.T, cfg Config) (value float64, present bool) {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Consolidate map[string]any `json:"consolidate"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	v, ok := doc.Consolidate["nudge_after"]
	if !ok {
		return 0, false
	}
	n, ok := v.(float64)
	if !ok {
		t.Fatalf("consolidate.nudge_after encodes as %T, want a number", v)
	}
	return n, true
}

// AC-5: the setting is 3 when the file says nothing, and when it says
// something else about the section.
func TestAC5NudgeAfterIsThreeWhenTheFileSaysNothingOfIt(t *testing.T) {
	if n, ok := us114NudgeAfter(t, Default()); !ok || n != 3 {
		t.Errorf("Default() has consolidate.nudge_after %v (set: %v), want 3", n, ok)
	}
	for _, tc := range []struct {
		name, body string
		want       float64
	}{
		{"nothing", `{"version": 1}`, 3},
		{"an empty section", `{"version": 1, "consolidate": {}}`, 3},
		{"a note in the section", `{"version": 1, "consolidate": {"_doc": "unset"}}`, 3},
		{"another section's keys", `{"version": 1, "loop": {"max_review_rounds": 2}}`, 3},
		{"a byte order mark", "\xef\xbb\xbf" + `{"version": 1}`, 3},
		{"the setting set to nine", `{"version": 1, "consolidate": {"nudge_after": 9}}`, 9},
	} {
		t.Run(strings.ReplaceAll(tc.name, " ", "_"), func(t *testing.T) {
			cfg, err := Parse([]byte(tc.body))
			if err != nil {
				t.Fatalf("Parse refused %s: %v", tc.body, err)
			}
			if n, ok := us114NudgeAfter(t, cfg); !ok || n != tc.want {
				t.Errorf("%s gives consolidate.nudge_after %v (set: %v), want %v", tc.name, n, ok, tc.want)
			}
		})
	}
}

// AC-5: 0 is a value, and means off: it is kept, and not read as absent.
func TestAC5NudgeAfterZeroIsKeptAndIsNotTheDefault(t *testing.T) {
	cfg, err := Parse([]byte(`{"version": 1, "consolidate": {"nudge_after": 0}}`))
	if err != nil {
		t.Fatalf("Parse refused a nudge_after of 0, which is how it is turned off: %v", err)
	}
	if n, ok := us114NudgeAfter(t, cfg); !ok || n != 0 {
		t.Errorf("consolidate.nudge_after = %v (set: %v), want 0", n, ok)
	}
}

// AC-5: a negative value is refused as the other limits are, naming the
// setting; a value of the wrong type is refused naming it too.
func TestAC5AValueTheLoopCannotHonourIsRefusedNamingTheSetting(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       sdlcerr.Code
	}{
		{"negative", `{"version": 1, "consolidate": {"nudge_after": -1}}`, sdlcerr.ConfigInvalid},
		{"text", `{"version": 1, "consolidate": {"nudge_after": "three"}}`, sdlcerr.ConfigUnreadable},
		{"a numeral in quotes", `{"version": 1, "consolidate": {"nudge_after": "3"}}`, sdlcerr.ConfigUnreadable},
		{"a fraction", `{"version": 1, "consolidate": {"nudge_after": 1.5}}`, sdlcerr.ConfigUnreadable},
		{"a boolean", `{"version": 1, "consolidate": {"nudge_after": true}}`, sdlcerr.ConfigUnreadable},
	} {
		t.Run(strings.ReplaceAll(tc.name, " ", "_"), func(t *testing.T) {
			_, err := Parse([]byte(tc.body))
			var e *sdlcerr.Error
			if !errors.As(err, &e) {
				t.Fatalf("Parse(%s) = %v, want a refusal", tc.body, err)
			}
			if e.Code != tc.code {
				t.Errorf("Parse(%s) refused with %s, want %s", tc.body, e.Code, tc.code)
			}
			if !strings.Contains(e.What+" "+e.Why, "nudge_after") {
				t.Errorf("the refusal does not name the setting:\n%s\n%s", e.What, e.Why)
			}
		})
	}
}

// AC-5: sdlc doctor reads the file through UnknownKeys, and the key is one the
// configuration has, where it was reported as unknown; a misspelling of it is
// still named.
func TestAC5UnknownKeysAcceptsNudgeAfterAndNamesAMisspelling(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, `{"version": 1, "consolidate": {"nudge_after": 5}}`)
	if _, err := Load(root); err != nil {
		t.Fatalf("a configuration with consolidate.nudge_after does not load: %v", err)
	}
	if got := UnknownKeys(root); got != nil {
		t.Errorf("UnknownKeys named %q in a configuration that spells nudge_after right", got)
	}

	writeConfig(t, root, `{"version": 1, "consolidate": {"nudge_aftre": 5}}`)
	if got, want := strings.Join(UnknownKeys(root), " "), "consolidate.nudge_aftre"; got != want {
		t.Errorf("UnknownKeys = %q, want %q", got, want)
	}
}
