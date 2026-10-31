package config

// US-112.6 AC-3: budget.gate_hours is judged by the loader, whatever the JSON
// holds. The tests name no field of Config: the shape it is kept in is the
// planner's, and what a caller sees is Parse's answer and what marshals back.

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

func gateHoursBody(value string) string {
	return `{"version":1,"budget":{"per_story_usd":60,"gate_hours":` + value + `}}`
}

// holdsTheAllowedKeys holds a refusal to naming every gate key, which is what a
// person who mistyped one needs in front of them.
func holdsTheAllowedKeys(t *testing.T, what, said string) {
	t.Helper()
	for _, g := range model.Gates {
		if !strings.Contains(said, string(g)) {
			t.Errorf("%s: the refusal does not name the gate key %s:\n%s", what, g, said)
		}
	}
}

// whyOf is the part of a refusal that says what is wrong, which Error leaves out.
func whyOf(t *testing.T, err error) string {
	t.Helper()
	var e *sdlcerr.Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v, want an sdlcerr.Error", err)
	}
	return e.Why
}

// keptHours is the numbers a loaded configuration writes back. An accepted value
// is only accepted if it reaches the configuration: a loader that ignores the key
// also accepts anything.
func keptHours(t *testing.T, cfg Config) map[string]float64 {
	t.Helper()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Budget struct {
			GateHours map[string]float64 `json:"gate_hours"`
		} `json:"budget"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("gate_hours did not marshal as a map of numbers: %v\n%s", err, raw)
	}
	return doc.Budget.GateHours
}

func TestAC3EveryGateKeyIsAcceptedWithANumber(t *testing.T) {
	for _, g := range model.Gates {
		t.Run(string(g), func(t *testing.T) {
			cfg, err := Parse([]byte(gateHoursBody(`{"` + string(g) + `":0.5}`)))
			if err != nil {
				t.Fatalf("a positive number for %s was refused: %v", g, err)
			}
			if got := keptHours(t, cfg); len(got) != 1 || got[string(g)] != 0.5 {
				t.Errorf("the number for %s did not reach the configuration: %v", g, got)
			}
		})
	}
}

func TestAC3ANumberIsAnyNumberThatIsNotNegative(t *testing.T) {
	for _, value := range []string{"0", "0.0", "1", "2.5", "0.27", "1e308", "1e-9"} {
		t.Run(value, func(t *testing.T) {
			cfg, err := Parse([]byte(gateHoursBody(`{"plan":` + value + `}`)))
			if err != nil {
				t.Fatalf("%s was refused: %v", value, err)
			}
			want, err := strconv.ParseFloat(value, 64)
			if err != nil {
				t.Fatal(err)
			}
			got, kept := keptHours(t, cfg)["plan"]
			if !kept || got != want {
				t.Errorf("%s did not reach the configuration: %v, kept %v", value, got, kept)
			}
		})
	}
}

// Each of these is valid JSON and valid as far as a typed decoder goes, or gets
// E0004 from one; AC-3 and the person's answer (ANALYSIS section 6) say E0045,
// the key, and the keys allowed. null was read as 0, which is off, and Plan as a
// second key nothing reads: a person who wrote either believed in a number that
// was not there.
func TestAC3AValueOrAKeyTheLoopCannotUseIsRefusedWithE0045NamingTheKeyAndTheGateKeys(t *testing.T) {
	for _, tc := range []struct{ name, value, key string }{
		{"a negative number", `{"plan":-1}`, "plan"},
		{"a tiny negative number", `{"retro":-1e-9}`, "retro"},
		{"a string", `{"plan":"1"}`, "plan"},
		{"true", `{"design_review":true}`, "design_review"},
		{"false", `{"plan":false}`, "plan"},
		{"null", `{"verifier_review":null}`, "verifier_review"},
		{"a list", `{"plan":[1]}`, "plan"},
		{"a map", `{"plan":{"hours":1}}`, "plan"},
		{"a number too large to hold", `{"plan":1e999}`, "plan"},
		{"a key in another case", `{"Plan":1}`, "Plan"},
		{"a key in capitals", `{"DOR":1}`, "DOR"},
		{"a key that is no gate", `{"reviews":1}`, "reviews"},
		{"a gate's number, not its key", `{"gate4":1}`, "gate4"},
		{"a key with a space", `{"plan ":1}`, `plan `},
		{"gate_hours is a list", `[1]`, "budget.gate_hours"},
		{"gate_hours is a number", `3`, "budget.gate_hours"},
		{"gate_hours is a string", `"plan"`, "budget.gate_hours"},
		{"gate_hours is true", `true`, "budget.gate_hours"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(gateHoursBody(tc.value)))
			if err == nil {
				t.Fatalf("%s was accepted", tc.value)
			}
			if got := codeOf(t, err); got != sdlcerr.ConfigInvalid {
				t.Errorf("%s: code = %s, want %s: %v", tc.value, got, sdlcerr.ConfigInvalid, err)
			}
			why := whyOf(t, err)
			if !strings.Contains(why, "gate_hours") || !strings.Contains(why, tc.key) {
				t.Errorf("%s: why = %q, want it to name gate_hours and %q", tc.value, why, tc.key)
			}
			holdsTheAllowedKeys(t, tc.value, why)
		})
	}
}

// An empty key names no gate and has nothing to quote, so it has its own row:
// the refusal still says what is allowed.
func TestAC3AnEmptyKeyIsRefusedWithE0045(t *testing.T) {
	_, err := Parse([]byte(gateHoursBody(`{"":1}`)))
	if err == nil {
		t.Fatal("an empty key was accepted")
	}
	if got := codeOf(t, err); got != sdlcerr.ConfigInvalid {
		t.Errorf("code = %s, want %s", got, sdlcerr.ConfigInvalid)
	}
	holdsTheAllowedKeys(t, "an empty key", whyOf(t, err))
}

// validate gives every problem in one message, and this one is no exception: a
// person fixes the file once.
func TestAC3EveryBadEntryIsNamedInOneRefusalAlongsideOtherProblems(t *testing.T) {
	body := `{"version":1,"thresholds":{"coverage_min":-1},"budget":{"gate_hours":{"Plan":1,"retro":-2,"dor":"x"}}}`
	_, err := Parse([]byte(body))
	if err == nil {
		t.Fatal("it was accepted")
	}
	if got := codeOf(t, err); got != sdlcerr.ConfigInvalid {
		t.Errorf("code = %s, want %s", got, sdlcerr.ConfigInvalid)
	}
	for _, want := range []string{"thresholds.coverage_min", "Plan", "retro", `dor`} {
		if why := whyOf(t, err); !strings.Contains(why, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, why)
		}
	}
}

// A map's order is the runtime's own: read twice, a refusal with its problems in
// another order is a different message for one file.
func TestAC3TheRefusalIsTheSameEveryTime(t *testing.T) {
	body := []byte(gateHoursBody(`{"Plan":1,"retro":-2,"dor":"x","nope":1,"analysis":null}`))
	_, first := Parse(body)
	if first == nil {
		t.Fatal("it was accepted")
	}
	for range 50 {
		_, again := Parse(body)
		if again == nil || whyOf(t, again) != whyOf(t, first) {
			t.Fatalf("the refusal changed from\n%v\nto\n%v", first, again)
		}
	}
}

// A setting of another type stays what it was: gate_hours is the deliberate
// exception beside TestLoadNamesTheSettingOfTheWrongType, not a new rule for the
// file. budget's other keys are the ones next door.
func TestAC3OtherBudgetSettingsOfTheWrongTypeStayE0004(t *testing.T) {
	for _, body := range []string{
		`{"budget":{"per_story_usd":"60","gate_hours":{"plan":1}}}`,
		`{"budget":{"alert_fractions":"half","gate_hours":{"plan":1}}}`,
	} {
		_, err := Parse([]byte(body))
		if err == nil {
			t.Fatalf("%s was accepted", body)
		}
		if got := codeOf(t, err); got != sdlcerr.ConfigUnreadable {
			t.Errorf("%s: code = %s, want %s", body, got, sdlcerr.ConfigUnreadable)
		}
	}
	// Beside them, gate_hours of the same wrong type is the exception.
	_, err := Parse([]byte(`{"budget":{"per_story_usd":60,"gate_hours":{"plan":"60"}}}`))
	if err == nil {
		t.Fatal("a string for a gate's number was accepted")
	}
	if got := codeOf(t, err); got != sdlcerr.ConfigInvalid {
		t.Errorf("gate_hours of the wrong type: code = %s, want %s", got, sdlcerr.ConfigInvalid)
	}
}

// Tests and tools write a Config back to disk. A nil map written as null would
// make a file the loader then refuses.
func TestAC3AConfigWithNoNumbersMarshalsWithNoGateHoursKey(t *testing.T) {
	loaded, err := Parse([]byte(`{"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, cfg := range map[string]Config{"the defaults": Default(), "the zero value": {}, "a file without it": loaded} {
		raw, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Contains(string(raw), "gate_hours") {
			t.Errorf("%s marshals with the key:\n%s", name, raw)
		}
	}
	raw, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(raw); err != nil {
		t.Errorf("a Config marshalled back does not load: %v\n%s", err, raw)
	}
	// One with a number does carry the key, so its absence above is the rule at work
	// and not a setting that is never written.
	set, err := Parse([]byte(gateHoursBody(`{"plan":0.5}`)))
	if err != nil {
		t.Fatal(err)
	}
	if got := keptHours(t, set); got["plan"] != 0.5 {
		t.Errorf("a Config with a number marshals gate_hours as %v", got)
	}
}

// A Config that carries numbers has to write them back in a form that loads as
// the same numbers: the map is the setting, not a detail of one load.
func TestAC3AConfigWithNumbersKeepsThemWhenMarshalledBack(t *testing.T) {
	first, err := Parse([]byte(gateHoursBody(`{"plan":0.5,"retro":1e308,"dor":0}`)))
	if err != nil {
		t.Fatal(err)
	}
	once, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Parse(once)
	if err != nil {
		t.Fatalf("what it marshalled to does not load: %v\n%s", err, once)
	}
	twice, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(once) != string(twice) {
		t.Errorf("the setting changed on its way back:\n%s\n%s", once, twice)
	}
	var doc struct {
		Budget struct {
			GateHours map[string]float64 `json:"gate_hours"`
		} `json:"budget"`
	}
	if err := json.Unmarshal(once, &doc); err != nil {
		t.Fatalf("gate_hours did not marshal as a map of numbers: %v\n%s", err, once)
	}
	if got := doc.Budget.GateHours; len(got) != 3 || got["plan"] != 0.5 || got["retro"] != 1e308 || got["dor"] != 0 {
		t.Errorf("gate_hours marshalled as %v", got)
	}
}

// UnknownKeys is what doctor reports as settings that change nothing, and these
// change something: the map's keys are judged by validate, not there.
func TestAC3UnknownKeysDoesNotReportGateHoursOrItsKeys(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, gateHoursBody(`{"plan":0.5,"retro":0}`))
	if got := UnknownKeys(root); len(got) != 0 {
		t.Errorf("UnknownKeys = %v, want none", got)
	}
}

// The landing reads trunk's configuration with Parse, and the file with Load:
// the same bytes get the same refusal.
func TestAC3LoadAndParseRefuseABadGateHoursAlike(t *testing.T) {
	body := gateHoursBody(`{"Plan":1}`)
	root := repo(t)
	writeConfig(t, root, body)
	_, loadErr := Load(root)
	_, parseErr := Parse([]byte(body))
	if loadErr == nil || parseErr == nil {
		t.Fatalf("Load said %v and Parse said %v", loadErr, parseErr)
	}
	if whyOf(t, loadErr) != whyOf(t, parseErr) {
		t.Errorf("Load said\n%v\nParse said\n%v", loadErr, parseErr)
	}
	if got := codeOf(t, loadErr); got != sdlcerr.ConfigInvalid {
		t.Errorf("code = %s, want %s", got, sdlcerr.ConfigInvalid)
	}
}
