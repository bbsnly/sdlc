package config

// US-109: the `team` section, which replaces `staffing` and `reviews.gate7_advisory`.
//
//	"team": {
//	  "retries": 2,
//	  "models":  ["haiku", "sonnet", "opus"],           lowest first
//	  "efforts": ["low", "medium", "high", "xhigh", "max"],
//	  "agents": {
//	    "security": {
//	      "enabled": true, "can_block": true, "max_rounds": 2, "instances": 1,
//	      "lowest": {"model": "sonnet", "effort": "medium"},
//	      "bring_back": ["internal/hook/"]
//	    }
//	  }
//	}
//
// The tests read it through Parse and UnknownKeys, which exist, and through what is
// refused. The codes are found by number, since the number is what is released. A key
// that is not an agent, and a bring_back on an agent that does not review the code,
// take knowing the roster: internal/cli holds those, because where the check lives is
// the design's to say (ANALYSIS.md, section 3).

import (
	"encoding/json"
	"errors"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

const (
	codeOldKey = "SDLC-E0087"
	codeTeam   = "SDLC-E0088"
)

func refusalOf(t *testing.T, raw string) *sdlcerr.Error {
	t.Helper()
	_, err := Parse([]byte(raw))
	if err == nil {
		t.Fatalf("the file loaded: %s", raw)
	}
	var refusal *sdlcerr.Error
	if !errors.As(err, &refusal) {
		t.Fatalf("refused with %v, which carries no code", err)
	}
	return refusal
}

func teamConfig(section string) string { return `{"version": 1, "team": ` + section + `}` }

// AC-1 and AC-2: every key and every entry may be absent, and null reads as absent, as
// it does for every setting encoding/json reads into a value.
func TestAC1ATeamSectionAbsentOrEmptyOrWithEveryKeyLoads(t *testing.T) {
	for name, body := range map[string]string{
		"AC_2 no section":     `{"version": 1}`,
		"AC_1 an empty one":   teamConfig(`{}`),
		"AC_1 null":           teamConfig(`null`),
		"AC_1 no agents":      teamConfig(`{"agents": {}}`),
		"AC_1 agents null":    teamConfig(`{"agents": null}`),
		"AC_1 an empty entry": teamConfig(`{"agents": {"security": {}}}`),
		"AC_1 an entry null":  teamConfig(`{"agents": {"security": null}}`),
		"AC_1 retries zero":   teamConfig(`{"retries": 0}`),
		"AC_1 keys null":      teamConfig(`{"agents": {"security": {"enabled": null, "can_block": null, "max_rounds": null, "instances": null}}}`),
		"AC_1 retries null":   teamConfig(`{"retries": null, "models": null, "efforts": null}`),
		"AC_1 every key": teamConfig(`{"retries": 3, "models": ["m1", "m2"], "efforts": ["e1", "e2"],
			"agents": {"security": {"enabled": false, "can_block": false, "max_rounds": 1, "instances": 2,
				"lowest": {"model": "m2", "effort": "e1"}, "bring_back": ["internal/hook/", "install.sh"]}}}`),
	} {
		if _, err := Parse([]byte(body)); err != nil {
			t.Errorf("%s: the configuration does not load: %v", name, err)
		}
	}
	root := repo(t)
	writeConfig(t, root, teamConfig(`{"retries": 3, "models": ["m1"], "efforts": ["e1"],
		"agents": {"security": {"enabled": false, "can_block": false, "max_rounds": 1, "instances": 2,
			"lowest": {"model": "m1"}, "bring_back": ["internal/"]}}}`))
	if got := UnknownKeys(root); got != nil {
		t.Errorf("AC-1 UnknownKeys named %q in a team section that is spelled right", got)
	}
	if FormatVersion != 1 {
		t.Errorf("the configuration format is version %d, want 1: the story adds a section and does not change the format", FormatVersion)
	}
}

// AC-11: model and effort names are opaque strings. Nothing ranks one by its name, and
// the list's order is the ranking.
func TestAC11ModelAndEffortNamesAreOpaque(t *testing.T) {
	for name, section := range map[string]string{
		"AC_11 names no provider has":        `{"models": ["z-9", "a b c", "ОПУС"], "efforts": ["0", "!"]}`,
		"AC_11 a lowest at the bottom":       `{"models": ["z", "a"], "efforts": ["9", "1"], "agents": {"bookkeeper": {"lowest": {"model": "z", "effort": "9"}}}}`,
		"AC_11 a lowest at the top":          `{"models": ["z", "a"], "efforts": ["9", "1"], "agents": {"verifier": {"lowest": {"model": "a", "effort": "1"}}}}`,
		"AC_11 a lowest with only a model":   `{"models": ["z"], "efforts": ["9"], "agents": {"verifier": {"lowest": {"model": "z"}}}}`,
		"AC_11 a lowest with only an effort": `{"models": ["z"], "efforts": ["9"], "agents": {"verifier": {"lowest": {"effort": "9"}}}}`,
	} {
		if _, err := Parse([]byte(teamConfig(section))); err != nil {
			t.Errorf("%s: does not load: %v", name, err)
		}
	}
	// A lowest is read against the defaults where the section sets no list.
	if _, err := Parse([]byte(teamConfig(`{"agents": {"verifier": {"lowest": {"model": "opus", "effort": "max"}}}}`))); err != nil {
		t.Errorf("AC-11 a lowest from the default lists does not load: %v", err)
	}
	refusal := refusalOf(t, teamConfig(`{"agents": {"verifier": {"lowest": {"model": "fable"}}}}`))
	if refusal.Code.String() != codeTeam || !strings.Contains(refusal.Why, "haiku") {
		t.Errorf("AC-11 a lowest outside the default lists: %s %s", refusal.Code, refusal.Why)
	}
}

// AC-13: what cannot be used is refused with the key named and what is allowed said.
// E0045 is not the code: it stays for what is outside the team (D-2). A value that is
// off by one is the boundary each row sits on, and the row beside it loads.
func TestAC13ATeamSectionThatCannotBeUsedIsRefusedWithTheKeyNamed(t *testing.T) {
	for _, tc := range []struct {
		name, section string
		says          []string
	}{
		{"AC_13 a negative retries", `{"retries": -1}`, []string{"team.retries", "0"}},
		{"AC_13 max_rounds 0", `{"agents": {"architect": {"max_rounds": 0}}}`, []string{"team.agents.architect.max_rounds", "1"}},
		{"AC_13 max_rounds below 0", `{"agents": {"architect": {"max_rounds": -3}}}`, []string{"team.agents.architect.max_rounds", "1"}},
		{"AC_8 instances 0", `{"agents": {"security": {"instances": 0}}}`, []string{"team.agents.security.instances", "1"}},
		{"AC_8 instances below 0", `{"agents": {"security": {"instances": -1}}}`, []string{"team.agents.security.instances", "1"}},
		{"AC_13 a model listed twice", `{"models": ["opus", "sonnet", "opus"]}`, []string{"team.models", "opus"}},
		{"AC_13 an effort listed twice", `{"efforts": ["low", "low"]}`, []string{"team.efforts", "low"}},
		{"AC_13 a blank model", `{"models": ["opus", ""]}`, []string{"team.models"}},
		{"AC_13 a blank effort", `{"efforts": [" "]}`, []string{"team.efforts"}},
		{"AC_13 no models", `{"models": []}`, []string{"team.models"}},
		{"AC_13 no efforts", `{"efforts": []}`, []string{"team.efforts"}},
		{"AC_13 a lowest model not listed", `{"models": ["sonnet", "opus"], "agents": {"verifier": {"lowest": {"model": "fable"}}}}`,
			[]string{"team.agents.verifier.lowest", "fable", "team.models"}},
		{"AC_13 a lowest effort not listed", `{"efforts": ["low", "high"], "agents": {"security": {"lowest": {"effort": "extreme"}}}}`,
			[]string{"team.agents.security.lowest", "extreme", "team.efforts"}},
		{"AC_13 every problem in one message", `{"retries": -1, "models": [], "agents": {"architect": {"max_rounds": 0}, "perf": {"instances": 0}}}`,
			[]string{"team.retries", "team.models", "team.agents.architect.max_rounds", "team.agents.perf.instances"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal := refusalOf(t, teamConfig(tc.section))
			if refusal.Code.String() != codeTeam {
				t.Fatalf("refused with %s, want %s: %s", refusal.Code, codeTeam, refusal.Why)
			}
			if refusal.Fix == "" {
				t.Error("the refusal does not say what to do")
			}
			for _, want := range tc.says {
				if !strings.Contains(refusal.Why, want) {
					t.Errorf("the refusal does not say %q: %s", want, refusal.Why)
				}
			}
		})
	}
	for name, section := range map[string]string{
		"AC_13 retries 0":    `{"retries": 0}`,
		"AC_13 max_rounds 1": `{"agents": {"architect": {"max_rounds": 1}}}`,
		"AC_8 instances 1":   `{"agents": {"security": {"instances": 1}}}`,
		"AC_8 instances 5":   `{"agents": {"security": {"instances": 5}}}`,
	} {
		if _, err := Parse([]byte(teamConfig(section))); err != nil {
			t.Errorf("%s: does not load: %v", name, err)
		}
	}
}

// AC-13 and T14: a bring_back path matched nothing and read as a protection. These run
// whatever else the section says, on every load: today they run only when the staffing
// is on.
func TestAC13ABringBackPathThatIsNotPlainAndRelativeIsRefused(t *testing.T) {
	for _, bad := range []string{``, `.`, `./internal`, `../outside`, `/etc`, `internal/../..`, `a\\b`, `internal/*`, `in?ternal`, `[a]`, `internal//hook`} {
		section := `{"agents": {"security": {"bring_back": ["internal/hook/", "` + bad + `"]}}}`
		refusal := refusalOf(t, teamConfig(section))
		if refusal.Code.String() != codeTeam || !strings.Contains(refusal.Why, "team.agents.security.bring_back") {
			t.Errorf("AC-13 bring_back %q: %s %s", bad, refusal.Code, refusal.Why)
		}
		if !strings.Contains(refusal.Why, "relative") {
			t.Errorf("AC-13 bring_back %q: the refusal does not say what is allowed: %s", bad, refusal.Why)
		}
	}
	for _, good := range []string{`internal/hook/`, `install.sh`, `a/b/c.go`, `docs/`} {
		if _, err := Parse([]byte(teamConfig(`{"agents": {"security": {"bring_back": ["` + good + `"]}}}`))); err != nil {
			t.Errorf("AC-13 bring_back %q does not load: %v", good, err)
		}
	}
}

// A setting of the wrong type is valid JSON, and the refusal names it, as it does for
// every other setting. The code is SDLC-E0004, the one decoding a wrong type has always
// been: that is what puts it on the branch where the hook reads no value from the file, since
// a file the decoder could not finish is not a file that was read (red team C4;
// internal/hook/team_test.go holds that half). SDLC-E0088 is for values that decoded.
func TestAC13ATeamSettingOfTheWrongTypeIsRefusedAndNamed(t *testing.T) {
	for setting, section := range map[string]string{
		"AC_13 team is a list":      `[]`,
		"AC_13 team is a string":    `"on"`,
		"AC_13 team is a number":    `1`,
		"AC_13 retries":             `{"retries": "two"}`,
		"AC_13 retries a fraction":  `{"retries": 1.5}`,
		"AC_13 models":              `{"models": "opus"}`,
		"AC_13 efforts":             `{"efforts": {"low": true}}`,
		"AC_13 agents":              `{"agents": ["security"]}`,
		"AC_13 an entry":            `{"agents": {"security": true}}`,
		"AC_13 enabled":             `{"agents": {"security": {"enabled": "no"}}}`,
		"AC_13 can_block":           `{"agents": {"security": {"can_block": 0}}}`,
		"AC_13 max_rounds":          `{"agents": {"security": {"max_rounds": "2"}}}`,
		"AC_13 instances":           `{"agents": {"security": {"instances": 1.5}}}`,
		"AC_13 lowest":              `{"agents": {"security": {"lowest": ["opus"]}}}`,
		"AC_13 bring_back":          `{"agents": {"security": {"bring_back": "internal/"}}}`,
		"AC_13 a bring_back of num": `{"agents": {"security": {"bring_back": [1]}}}`,
	} {
		t.Run(setting, func(t *testing.T) {
			refusal := refusalOf(t, teamConfig(section))
			if refusal.Code != sdlcerr.ConfigUnreadable {
				t.Fatalf("refused with %s, want %s", refusal.Code, sdlcerr.ConfigUnreadable)
			}
			if !strings.Contains(strings.ToLower(refusal.What+" "+refusal.Why), "team") {
				t.Errorf("the refusal does not name the team section: %s: %s", refusal.What, refusal.Why)
			}
		})
	}
}

// The person's decision at Gate 4: a value far past any use is refused rather than expanded.
// Instances are read per reviewer, per gate, on every hook call, and out of memory in Go is
// fatal, so the hook's recover does not answer it; and `retries + 1` wraps at the top of an
// int, which turns the strictest budget into none (security note 5, red team H3). Each row is
// a call that returns, whatever the value: the validation counts nothing out.
func TestAC8AC9InstancesAndRetriesHaveACeiling(t *testing.T) {
	for name, section := range map[string]string{
		"AC_8 instances 16": `{"agents": {"security": {"instances": 16}}}`,
		"AC_9 retries 20":   `{"retries": 20}`,
	} {
		if _, err := Parse([]byte(teamConfig(section))); err != nil {
			t.Errorf("%s: does not load: %v", name, err)
		}
	}
	for _, tc := range []struct {
		name, section string
		says          []string
	}{
		{"AC_8 instances 17", `{"agents": {"security": {"instances": 17}}}`, []string{"team.agents.security.instances", "16"}},
		{"AC_8 instances on an agent that does not review", `{"agents": {"researcher": {"instances": 17}}}`, []string{"team.agents.researcher.instances", "16"}},
		{"AC_8 instances 2000000000", `{"agents": {"code-reviewer": {"instances": 2000000000}}}`, []string{"team.agents.code-reviewer.instances", "16"}},
		{"AC_9 retries 21", `{"retries": 21}`, []string{"team.retries", "20"}},
		{"AC_9 retries 2000000000", `{"retries": 2000000000}`, []string{"team.retries", "20"}},
		{"AC_9 retries at the top of an int", `{"retries": 9223372036854775807}`, []string{"team.retries", "20"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			refusal := refusalOf(t, teamConfig(tc.section))
			runtime.ReadMemStats(&after)
			if refusal.Code.String() != codeTeam {
				t.Fatalf("refused with %s, want %s: %s", refusal.Code, codeTeam, refusal.Why)
			}
			for _, want := range tc.says {
				if !strings.Contains(refusal.Why, want) {
					t.Errorf("the refusal does not say %q: %s", want, refusal.Why)
				}
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
				t.Errorf("the validation allocated %d bytes for one value", grew)
			}
		})
	}
}

// Red team V11: the section's validation sits on every load, and a refusal goes through to a
// person's terminal and a session. One name repeated is one problem, not a problem per repeat,
// so a list of 100,000 makes a short message. Size and not time: time is not a thing a test
// holds without a clock.
func TestAC13OneRefusalDoesNotGrowWithItsDuplicates(t *testing.T) {
	const repeats = 100000
	many := func(name string) string { return strings.TrimSuffix(strings.Repeat(`"`+name+`",`, repeats), ",") }
	// Red team R9 (V20): a dedupe alone passes the four rows of duplicates, so the fixed size,
	// "more follow" and the cut of a long name are reached only by these: problems that are
	// all different, and one name of a megabyte.
	distinct := make([]string, repeats)
	for i := range distinct {
		distinct[i] = `"../outside-` + strconv.Itoa(i) + `"`
	}
	for name, section := range map[string]string{
		"AC_13 a model":                    `{"models": [` + many("opus") + `]}`,
		"AC_13 an effort":                  `{"efforts": [` + many("low") + `]}`,
		"AC_13 a bring_back path":          `{"agents": {"security": {"bring_back": [` + many("../outside") + `]}}}`,
		"AC_13 a model and effort":         `{"models": [` + many("opus") + `], "efforts": [` + many("low") + `]}`,
		"AC_13 bring_back paths all bad":   `{"agents": {"security": {"bring_back": [` + strings.Join(distinct, ",") + `]}}}`,
		"AC_13 an agent key of a megabyte": `{"agents": {"` + strings.Repeat("d", 1<<20) + `": {"enabled": false}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			refusal := refusalOf(t, teamConfig(section))
			if refusal.Code.String() != codeTeam {
				t.Fatalf("refused with %s, want %s", refusal.Code, codeTeam)
			}
			if got := len(refusal.What) + len(refusal.Why); got > 4096 {
				t.Errorf("a refusal over %d duplicates is %d bytes, want a short one", repeats, got)
			}
		})
	}
}

// Human advocate 5: `staffing.enabled` has no key of its own to move to, and the two values
// ask two different things of a person. True is now the default and the key is deleted; false
// is the same as disabling the project manager. The words are the part a person acts on, and
// reading them the same way is how the setting comes back wrong.
func TestAC12TheRefusalSaysWhatToDoWithEachValueOfStaffingEnabled(t *testing.T) {
	on := refusalOf(t, gate7File(`"staffing":{"enabled":true}`))
	off := refusalOf(t, gate7File(`"staffing":{"enabled":false}`))
	if on.Why == off.Why {
		t.Errorf("AC-12 the refusal reads the same for true and false: %s", on.Why)
	}
	for _, want := range []string{"staffing.enabled", "default"} {
		if !strings.Contains(on.Why, want) {
			t.Errorf("AC-12 staffing.enabled true: the refusal does not say %q: %s", want, on.Why)
		}
	}
	if !regexp.MustCompile(`(?i)\b(delete|remove)\b`).MatchString(on.Why) {
		t.Errorf("AC-12 staffing.enabled true: the refusal does not say to take the key out: %s", on.Why)
	}
	for _, want := range []string{"staffing.enabled", "team.agents.project-manager.enabled", "false"} {
		if !strings.Contains(off.Why, want) {
			t.Errorf("AC-12 staffing.enabled false: the refusal does not say %q: %s", want, off.Why)
		}
	}
}

// T8: a floor or an advisory code review is never lost without notice, and once the
// fields go, Unmarshal drops the keys, so detection has its own read of the file. The
// rule (D-1): refuse where a bool field tagged gate7_advisory, in a struct field tagged
// reviews, reads true after decoding: what the released tool read and acted on.
// TestAC12TheOldKeyTableIsWhatGosDecoderReads holds this table to the decoder itself.
var gate7Table = []struct {
	name, file string
	refused    bool
}{
	{"AC_12 true", `"reviews":{"gate7_advisory":true}`, true},
	{"AC_12 the section in another case", `"Reviews":{"gate7_advisory":true}`, true},
	{"AC_12 both in capitals", `"REVIEWS":{"GATE7_ADVISORY":true}`, true},
	{"AC_12 the key in another case", `"reviews":{"Gate7_Advisory":true}`, true},
	{"AC_12 true then null, which has no effect", `"reviews":{"gate7_advisory":true,"gate7_advisory":null}`, true},
	{"AC_12 the section then null, which has no effect", `"reviews":{"gate7_advisory":true},"reviews":null`, true},
	{"AC_12 the section then an empty one, which sets nothing", `"reviews":{"gate7_advisory":true},"reviews":{}`, true},
	{"AC_12 the section then an empty one in another case", `"reviews":{"gate7_advisory":true},"Reviews":{}`, true},
	{"AC_12 false then true in another case", `"reviews":{"gate7_advisory":false,"Gate7_Advisory":true}`, true},
	{"AC_12 false then true", `"reviews":{"gate7_advisory":false,"gate7_advisory":true}`, true},
	{"AC_2 true then false", `"reviews":{"gate7_advisory":true,"gate7_advisory":false}`, false},
	{"AC_2 false", `"reviews":{"gate7_advisory":false}`, false},
	{"AC_2 null", `"reviews":{"gate7_advisory":null}`, false},
	{"AC_2 an empty section", `"reviews":{}`, false},
	{"AC_2 the section null", `"reviews":null`, false},
	{"AC_2 no section", ``, false},
	{"AC_2 false beside a setting nobody knows", `"reviews":{"gate7_advisory":false,"other":1}`, false},
}

func gate7File(members string) string {
	if members == "" {
		return `{"version":1}`
	}
	return `{"version":1,` + members + `}`
}

func TestAC12Gate7AdvisoryIsRefusedWhereTheReleasedToolReadItAsTrueAndOnlyThere(t *testing.T) {
	for _, tc := range gate7Table {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(gate7File(tc.file)))
			if !tc.refused {
				if err != nil {
					t.Fatalf("a file that holds nothing to lose was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("the file loaded, and an advisory code review is lost without notice")
			}
			var refusal *sdlcerr.Error
			if !errors.As(err, &refusal) || refusal.Code.String() != codeOldKey {
				t.Fatalf("refused with %v, want %s", err, codeOldKey)
			}
			for _, want := range []string{"reviews.gate7_advisory", "team.agents.code-reviewer.can_block"} {
				if !strings.Contains(refusal.Why, want) {
					t.Errorf("the refusal does not say %q: %s", want, refusal.Why)
				}
			}
		})
	}
}

// T8's table came from the package doc and was not run at Gate 2. This runs it, against
// the decoder the released tool used, with the struct the released tool had: a row that
// reads otherwise is a wrong row, and fails here and not as a bug in the implementation.
func TestAC12TheOldKeyTableIsWhatGosDecoderReads(t *testing.T) {
	for _, tc := range gate7Table {
		var released struct {
			Reviews struct {
				Gate7Advisory bool `json:"gate7_advisory"`
			} `json:"reviews"`
		}
		if err := json.Unmarshal([]byte(gate7File(tc.file)), &released); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if released.Reviews.Gate7Advisory != tc.refused {
			t.Errorf("%s: the released tool read %v, and the row says refused = %v", tc.name, released.Reviews.Gate7Advisory, tc.refused)
		}
	}
}

// A value of the wrong type was refused before the story, so no project relies on it,
// and a person who wrote "true" believed in something: it stays SDLC-E0004 (D-1).
func TestAC12Gate7AdvisoryOfTheWrongTypeStaysTheSettingOfTheWrongType(t *testing.T) {
	for _, file := range []string{
		`"reviews":{"gate7_advisory":"true"}`,
		`"reviews":{"gate7_advisory":1}`,
		`"reviews":5`,
		`"reviews":"on"`,
		`"reviews":[]`,
	} {
		refusal := refusalOf(t, gate7File(file))
		if refusal.Code != sdlcerr.ConfigUnreadable {
			t.Errorf("%s: refused with %s, want %s", file, refusal.Code, sdlcerr.ConfigUnreadable)
		}
		if !strings.Contains(refusal.Why, "reviews") {
			t.Errorf("%s: the refusal does not name the setting: %s", file, refusal.Why)
		}
	}
}

// T8: any key that folds to `staffing`, at any value. A section that was off with a
// lowest in it must not slip through by being off, and `init` never wrote the key, so
// nothing is refused that nobody put there.
func TestAC12AStaffingSectionIsRefusedInAnyCaseAtAnyValue(t *testing.T) {
	for name, member := range map[string]string{
		"AC_12 on":                   `"staffing":{"enabled":true,"models":["opus"],"efforts":["low"]}`,
		"AC_12 another case":         `"Staffing":{"enabled":true,"models":["opus"],"efforts":["low"]}`,
		"AC_12 capitals":             `"STAFFING":{"enabled":true,"models":["opus"],"efforts":["low"]}`,
		"AC_12 empty":                `"staffing":{}`,
		"AC_12 null":                 `"staffing":null`,
		"AC_12 off with a lowest":    `"staffing":{"enabled":false,"lowest":{"verifier":{"model":"opus"}}}`,
		"AC_12 a value of no shape":  `"staffing":3`,
		"AC_12 a list":               `"staffing":[]`,
		"AC_12 beside a team":        `"staffing":{},"team":{"retries":1}`,
		"AC_12 then null":            `"staffing":{"enabled":true},"staffing":null`,
		"AC_12 a second in its case": `"staffing":{},"Staffing":null`,
	} {
		t.Run(name, func(t *testing.T) {
			refusal := refusalOf(t, gate7File(member))
			if refusal.Code.String() != codeOldKey {
				t.Fatalf("refused with %s, want %s: %s", refusal.Code, codeOldKey, refusal.Why)
			}
			if !strings.Contains(refusal.Why, "staffing") || !strings.Contains(refusal.Why, "team") {
				t.Errorf("the refusal does not name the old key and the new one: %s", refusal.Why)
			}
		})
	}
	// `_staffing_doc` is a comment, and a key that only starts like one is no key at all.
	for _, member := range []string{`"_staffing":{"enabled":true}`, `"_staffing_doc":["a comment"]`} {
		if _, err := Parse([]byte(gate7File(member))); err != nil {
			t.Errorf("%s is a comment and was refused: %v", member, err)
		}
	}
}

// AC-12: each old key found is named with the key it moves to, and the one with no
// destination is named as having none.
func TestAC12TheRefusalMapsEachOldKeyToItsNewOne(t *testing.T) {
	refusal := refusalOf(t, gate7File(`"staffing":{"enabled":true,"models":["m"],"efforts":["e"],
		"lowest":{"verifier":{"model":"m"}},"bring_back":{"security":["internal/"]}},
		"reviews":{"gate7_advisory":true}`))
	for _, want := range []string{
		"staffing.models", "team.models",
		"staffing.efforts", "team.efforts",
		"staffing.lowest", "team.agents.verifier.lowest",
		"staffing.bring_back", "team.agents.security.bring_back",
		"staffing.enabled", "team.agents.project-manager",
		"reviews.gate7_advisory", "team.agents.code-reviewer.can_block",
	} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("AC-12 the refusal does not say %q: %s", want, refusal.Why)
		}
	}
	// A free-text key cannot start a line of its own in the refusal (T11).
	refusal = refusalOf(t, gate7File(`"staffing":{"lowest":{"x\ny":{"model":"m"}}}`))
	if strings.ContainsAny(refusal.What+refusal.Why, "\n\r") {
		t.Errorf("T11 a key from the file reached the refusal unquoted: %q", refusal.Why)
	}
}

// D-2: three codes can apply to one file, and validate promises every problem in one
// message. The code is what a person has to do first.
func TestAC13OneRefusalListsEveryProblemUnderTheCodeAPersonFixesFirst(t *testing.T) {
	both := gate7File(`"staffing":{},"team":{"retries":-1},"loop":{"max_review_rounds":-1}`)
	refusal := refusalOf(t, both)
	if refusal.Code.String() != codeOldKey {
		t.Errorf("AC-13 an old key, a bad team and a bad limit: refused with %s, want %s", refusal.Code, codeOldKey)
	}
	for _, want := range []string{"staffing", "team.retries", "loop.max_review_rounds"} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("AC-13 the one refusal does not list %q: %s", want, refusal.Why)
		}
	}
	refusal = refusalOf(t, gate7File(`"team":{"retries":-1},"loop":{"max_review_rounds":-1}`))
	if refusal.Code.String() != codeTeam {
		t.Errorf("AC-13 a bad team and a bad limit: refused with %s, want %s", refusal.Code, codeTeam)
	}
	for _, want := range []string{"team.retries", "loop.max_review_rounds"} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("AC-13 the one refusal does not list %q: %s", want, refusal.Why)
		}
	}
	// A team problem beside one from outside the team is one refusal, and config.ForRules
	// keeps a file's values only where nothing from outside the team is in it: a version from
	// a newer format is never read as this one. Go gives a package's test no way to call a
	// function that is not there yet, and every package here compiles before the code is
	// written (TEST-PLAN.md), so ForRules' own table is held at its two callers:
	// TestAC13InitForceKeepsAFilesOwnPlacesWhereItWasReadAndNotWhereItWasNot in
	// internal/scaffold, and TestAC13TheHookReadsTheProjectsOwnSettingsFromARefusedFile in
	// internal/hook.
	refusal = refusalOf(t, `{"version":2,"team":{"retries":-1}}`)
	if refusal.Code.String() != codeTeam {
		t.Errorf("AC-13 a bad team and a version too high: refused with %s, want %s", refusal.Code, codeTeam)
	}
	for _, want := range []string{"team.retries", "version"} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("AC-13 the one refusal does not list %q: %s", want, refusal.Why)
		}
	}
	// E0045 stays live for everything outside the team.
	for _, body := range []string{
		`{"version":2}`,
		`{"version":0}`,
		`{"version":1,"backlog":{"path":"../out.json"}}`,
		`{"version":1,"loop":{"max_review_rounds":-1}}`,
		`{"version":1,"consolidate":{"nudge_after":-1}}`,
		`{"version":1,"budget":{"gate_hours":{"nonsense":1}}}`,
		`{"version":1,"reviews":{"gate7_advisory":false},"loop":{"max_stop_blocks":-1}}`,
	} {
		refusal := refusalOf(t, body)
		if refusal.Code != sdlcerr.ConfigInvalid {
			t.Errorf("AC-13 %s: refused with %s, want %s: it stays live outside the team", body, refusal.Code, sdlcerr.ConfigInvalid)
		}
	}
}

// T8: a misspelt key inside an agent's entry is a floor believed in that is not there.
// UnknownKeys does not walk maps today, so `lowset` passes in silence.
func TestAC13AMisspeltKeyInsideAnAgentsEntryIsNamed(t *testing.T) {
	root := repo(t)
	writeConfig(t, root, teamConfig(`{"retrys": 1, "agents": {
		"security": {"lowset": {"model": "opus"}, "can_blok": false, "instances": 2},
		"perf": {"enable": false, "bring_bak": []},
		"verifier": {"lowest": {"mdl": "opus"}}}}`))
	got := strings.Join(UnknownKeys(root), " ")
	want := "team.agents.perf.bring_bak team.agents.perf.enable team.agents.security.can_blok " +
		"team.agents.security.lowset team.agents.verifier.lowest.mdl team.retrys"
	if got != want {
		t.Errorf("AC-13 UnknownKeys = %q, want %q", got, want)
	}
}

// AC-13, "a duplicated name": encoding/json keeps the last of a repeated object key, so
// `"perf": {"enabled": false}, "perf": {"enabled": true}` loaded with perf enabled and the
// line a person wrote to disable it read in silence. The refusal names the repeated agent,
// once however often it repeats, and says it is repeated; the roster list in another
// refusal names every agent bare, so the name is asked for as a key or quoted.
func TestAC13AnAgentNamedTwiceInTeamAgentsIsRefusedWithTheNameGiven(t *testing.T) {
	repeated := []string{"more than once", "twice", "repeat", "duplicat"}
	for _, tc := range []struct {
		name, section, agent, other string
	}{
		{"AC_13 the later entry turning it back on", `{"agents": {"perf": {"enabled": false}, "perf": {"enabled": true}}}`, "perf", "security"},
		{"AC_13 two entries the same", `{"agents": {"perf": {}, "perf": {}}}`, "perf", "security"},
		{"AC_13 apart, with another agent between", `{"agents": {"security": {"instances": 2}, "perf": {}, "security": {"instances": 3}}}`, "security", "perf"},
		{"AC_13 three times is one problem", `{"agents": {"red-team": {}, "red-team": {}, "red-team": {"can_block": true}}}`, "red-team", "perf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal := refusalOf(t, `{"version": 1, "team": `+tc.section+`}`)
			if refusal.Code.String() != codeTeam {
				t.Fatalf("refused with %s, want %s: %s", refusal.Code, codeTeam, refusal.Why)
			}
			if refusal.Fix == "" {
				t.Error("the refusal does not say what to do")
			}
			named := func(agent string) int {
				return strings.Count(refusal.Why, "team.agents."+agent) + strings.Count(refusal.Why, strconv.Quote(agent))
			}
			if got := named(tc.agent); got != 1 {
				t.Errorf("the refusal names %s %d times, want once: %s", tc.agent, got, refusal.Why)
			}
			if named(tc.other) != 0 {
				t.Errorf("the refusal names %s, which is given once: %s", tc.other, refusal.Why)
			}
			if !strings.Contains(refusal.Why, "team.agents") {
				t.Errorf("the refusal does not say where: %s", refusal.Why)
			}
			if !slices.ContainsFunc(repeated, func(w string) bool { return strings.Contains(strings.ToLower(refusal.Why), w) }) {
				t.Errorf("the refusal does not say the name is repeated: %s", refusal.Why)
			}
		})
	}
	for name, section := range map[string]string{
		"AC_13 each agent once":        `{"agents": {"perf": {"enabled": false}, "security": {"instances": 2}}}`,
		"AC_13 a key repeated nowhere": `{"agents": {"perf": {"enabled": false, "can_block": true}}}`,
	} {
		if _, err := Parse([]byte(teamConfig(section))); err != nil {
			t.Errorf("%s: does not load: %v", name, err)
		}
	}
}

// D-1: with the field gone, UnknownKeys would report reviews as unknown and every file
// `init` ever wrote would fail doctor. Any other key under reviews is unknown, as today.
func TestAC2TheLeftoverGate7AdvisoryIsNotAnUnknownKeyAndItsNeighbourIs(t *testing.T) {
	for body, want := range map[string]string{
		`{"version":1,"reviews":{"gate7_advisory":false}}`:           "",
		`{"version":1,"reviews":{"gate7_advisory":null}}`:            "",
		`{"version":1,"reviews":{}}`:                                 "",
		`{"version":1,"reviews":{"Gate7_Advisory":false}}`:           "",
		`{"version":1,"reviews":{"gate7_advisory":false,"other":1}}`: "reviews.other",
	} {
		root := repo(t)
		writeConfig(t, root, body)
		if got := strings.Join(UnknownKeys(root), " "); got != want {
			t.Errorf("AC-2 %s: UnknownKeys = %q, want %q", body, got, want)
		}
	}
}

// T7: the validation sits on the hook's path, and no JSON value may panic it. Each of
// these either loads or is refused with a code.
func TestAC13NoJSONValueInTheTeamSectionPanicsTheValidation(t *testing.T) {
	deep := strings.Repeat(`{"a":`, 2000) + `1` + strings.Repeat(`}`, 2000)
	for name, body := range map[string]string{
		"team null":             teamConfig(`null`),
		"team a list":           teamConfig(`[null, [], {}]`),
		"agents a string":       teamConfig(`{"agents": "x"}`),
		"an entry a list":       teamConfig(`{"agents": {"security": []}}`),
		"lowest null":           teamConfig(`{"agents": {"security": {"lowest": null}}}`),
		"bring_back null":       teamConfig(`{"agents": {"security": {"bring_back": null}}}`),
		"bring_back of nulls":   teamConfig(`{"agents": {"security": {"bring_back": [null, null]}}}`),
		"models of nulls":       teamConfig(`{"models": [null], "efforts": [null]}`),
		"an empty key":          teamConfig(`{"agents": {"": {}}}`),
		"a huge list":           teamConfig(`{"models": [` + strings.TrimSuffix(strings.Repeat(`"m",`, 100000), ",") + `]}`),
		"a huge number":         teamConfig(`{"retries": 1e400, "agents": {"security": {"instances": 99999999999999999999}}}`),
		"deep nesting in entry": teamConfig(`{"agents": {"security": {"lowest": ` + deep + `}}}`),
		"deep nesting as team":  teamConfig(deep),
		"staffing deep":         gate7File(`"staffing":` + deep),
		"reviews deep":          gate7File(`"reviews":` + deep),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(body)); err != nil {
				if _, ok := errors.AsType[*sdlcerr.Error](err); !ok {
					t.Errorf("refused with no code: %v", err)
				}
			}
			root := repo(t)
			writeConfig(t, root, body)
			_ = UnknownKeys(root)
		})
	}
}

// D-1's direction: no reader takes the old name to mean anything but a refusal or
// nothing. `false` beside can_block changes nothing; `true` beside it is refused, since
// AC-12 keys on the old key.
func TestAC12Gate7AdvisoryBesideCanBlockIsReadOnlyToRefuse(t *testing.T) {
	refusal := refusalOf(t, gate7File(`"reviews":{"gate7_advisory":true},"team":{"agents":{"code-reviewer":{"can_block":false}}}`))
	if refusal.Code.String() != codeOldKey {
		t.Errorf("AC-12 true beside can_block false: refused with %s, want %s", refusal.Code, codeOldKey)
	}
	if _, err := Parse([]byte(gate7File(`"reviews":{"gate7_advisory":false},"team":{"agents":{"code-reviewer":{"can_block":false}}}`))); err != nil {
		t.Errorf("AC-12 false beside can_block false was refused: %v", err)
	}
}
