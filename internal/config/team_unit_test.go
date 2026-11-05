package config

// US-109, unit rows for what the team section answers once it is read: the
// accessors every other package asks, the hook's ForRules, and doctor's
// LeftoverGate7. Each is held in-process here because coverage counts a
// package's own tests only, and the cli, hook and e2e rows that drive them
// count elsewhere.

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/sdlcerr"
)

// team parses a configuration holding only this team section, and fails the
// test where it does not load.
func team(t *testing.T, section string) Team {
	t.Helper()
	cfg, err := Parse([]byte(teamConfig(section)))
	if err != nil {
		t.Fatalf("the team section %s does not load: %v", section, err)
	}
	return cfg.Team
}

// AC-1, AC-11: an absent key reads as its default, and a set one as written.
func TestAC1AC11EveryTeamSettingLeftOutReadsAsItsDefault(t *testing.T) {
	empty := team(t, `{}`)
	if got := empty.RetriesAllowed(); got != 2 {
		t.Errorf("AC-1 retries left out = %d, want 2", got)
	}
	if got := empty.ModelList(); !slices.Equal(got, []string{"haiku", "sonnet", "opus"}) {
		t.Errorf("AC-11 models left out = %q, want haiku, sonnet, opus", got)
	}
	if got := empty.EffortList(); !slices.Equal(got, []string{"low", "medium", "high", "xhigh", "max"}) {
		t.Errorf("AC-11 efforts left out = %q, want low, medium, high, xhigh, max", got)
	}
	for _, agent := range model.Agents {
		if !empty.Enabled(agent) {
			t.Errorf("AC-1 %s is disabled with no entry", agent)
		}
		if got := empty.Instances(agent); got != 1 {
			t.Errorf("AC-1 %s has %d instances with no entry, want 1", agent, got)
		}
		if n, set := empty.MaxRounds(agent); set {
			t.Errorf("AC-7 %s has max_rounds %d with no entry: it should use loop.max_review_rounds", agent, n)
		}
		if got := empty.LowestFor(agent); got != (Lowest{}) {
			t.Errorf("AC-11 %s has the floor %+v with no entry, want none", agent, got)
		}
		if got := empty.BringBackFor(agent); len(got) != 0 {
			t.Errorf("AC-11 %s has bring_back %q with no entry", agent, got)
		}
	}
	if !empty.StaffingOn() {
		t.Error("AC-3 the staffing is off with no team section: the project manager runs unless disabled")
	}
	if got := empty.CanBlock(); len(got) != 0 {
		t.Errorf("AC-6 can_block with no entry = %v, want nothing set, so each reviewer keeps the roster's power", got)
	}
	if empty.BringsBack() {
		t.Error("AC-11 BringsBack is true with no bring_back path anywhere")
	}

	set := team(t, `{"retries": 5, "models": ["m1", "m2"], "efforts": ["e1"],
		"agents": {"red-team": {"can_block": true}, "architect": {"can_block": false, "max_rounds": 4},
			"perf": {"lowest": {"model": "m2", "effort": "e1"}, "bring_back": ["internal/hook/"]}}}`)
	if got := set.RetriesAllowed(); got != 5 {
		t.Errorf("AC-9 retries 5 reads as %d", got)
	}
	if got := set.ModelList(); !slices.Equal(got, []string{"m1", "m2"}) {
		t.Errorf("AC-11 models = %q, want the list as written, in its order", got)
	}
	if got := set.EffortList(); !slices.Equal(got, []string{"e1"}) {
		t.Errorf("AC-11 efforts = %q, want e1", got)
	}
	if got := set.CanBlock(); len(got) != 2 || got["red-team"] != true || got["architect"] != false {
		t.Errorf("AC-6 CanBlock = %v, want red-team true and architect false and no other agent", got)
	}
	if n, ok := set.MaxRounds("architect"); !ok || n != 4 {
		t.Errorf("AC-7 the architect's max_rounds = %d, %v, want 4", n, ok)
	}
	if got := set.LowestFor("perf"); got != (Lowest{Model: "m2", Effort: "e1"}) {
		t.Errorf("AC-11 perf's floor = %+v", got)
	}
	if got := set.BringBackFor("perf"); !slices.Equal(got, []string{"internal/hook/"}) {
		t.Errorf("AC-11 perf's bring_back = %q", got)
	}
	if !set.BringsBack() {
		t.Error("AC-11 BringsBack is false with a bring_back path on perf")
	}
}

// AC-8, AC-9: a value past a ceiling is refused at load, but a refused file
// keeps its values on the hook's path, so what the accessors answer is held to
// the same bounds: two billion instances expanded into reviewers ran out of
// memory, and retries at the top of an int wrapped to no budget at all.
func TestAC8AC9TheAccessorsHoldAValuePastItsCeilingToTheBound(t *testing.T) {
	for _, tc := range []struct {
		name               string
		retries, instances int
		wantR, wantI       int
	}{
		{"AC_9 AC_8 at the floor", 0, 1, 0, 1},
		{"AC_9 AC_8 at the ceiling", 20, 16, 20, 16},
		{"AC_9 AC_8 below the floor", -1, 0, 0, 1},
		{"AC_9 AC_8 far below the floor", -1 << 40, -1 << 40, 0, 1},
		{"AC_9 AC_8 past the ceiling", 21, 17, 20, 16},
		{"AC_9 AC_8 at the top of an int", int(^uint(0) >> 1), int(^uint(0) >> 1), 20, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tm := Team{Retries: &tc.retries, Agents: map[string]AgentEntry{"security": {Instances: &tc.instances}}}
			if got := tm.RetriesAllowed(); got != tc.wantR {
				t.Errorf("RetriesAllowed = %d, want %d", got, tc.wantR)
			}
			if got := tm.Instances("security"); got != tc.wantI {
				t.Errorf("Instances = %d, want %d", got, tc.wantI)
			}
			if got := len(tm.ReviewersFor(model.GateCodeReview)); got != len(model.ReviewersFor(model.GateCodeReview))+tc.wantI-1 {
				t.Errorf("the code review expects %d reviews, want the roster's plus %d instances of security", got, tc.wantI-1)
			}
		})
	}
}

// AC-4, AC-8: an instance reads its role's entry, and how many take part is its
// instances only where it is an enabled reviewer.
func TestAC4AC8AnInstanceReadsItsRolesEntryAndOnlyAnEnabledReviewerHasSeats(t *testing.T) {
	tm := team(t, `{"agents": {
		"security": {"instances": 3, "max_rounds": 2, "lowest": {"model": "sonnet"}, "bring_back": ["install.sh"]},
		"perf": {"instances": 2, "enabled": false},
		"researcher": {"instances": 4},
		"project-manager": {"enabled": true}}}`)
	if !tm.Enabled("security-3") || tm.Enabled("perf-2") || tm.Enabled("perf") {
		t.Error("AC-4 an instance is not enabled as its role is")
	}
	if n, ok := tm.MaxRounds("security-2"); !ok || n != 2 {
		t.Errorf("AC-7 security-2's max_rounds = %d, %v, want its role's 2", n, ok)
	}
	if got := tm.LowestFor("security-3"); got.Model != "sonnet" {
		t.Errorf("AC-8 security-3's floor = %+v, want its role's", got)
	}
	if got := tm.BringBackFor("security-2"); !slices.Equal(got, []string{"install.sh"}) {
		t.Errorf("AC-8 security-2's bring_back = %q, want its role's", got)
	}
	for agent, want := range map[string]int{
		"security":   3,
		"perf":       1, // disabled: its instances need no row and are not waited for
		"researcher": 1, // reviews nothing, so no instance can be named
		"architect":  1,
	} {
		if got := tm.Seats(agent); got != want {
			t.Errorf("AC-8 Seats(%s) = %d, want %d", agent, got, want)
		}
	}
	for name, want := range map[string]bool{
		"security":     true,
		"researcher":   true,
		"security-2":   true,
		"security-3":   true,
		"security-4":   false,
		"perf-2":       false,
		"researcher-2": false,
		"architect-2":  false,
		"nobody":       false,
		"":             false,
	} {
		if got := tm.Staffable(name); got != want {
			t.Errorf("AC-8 Staffable(%q) = %v, want %v", name, got, want)
		}
	}
	want := slices.Clone(model.Agents)
	i := slices.Index(want, "security")
	want = slices.Insert(want, i+1, "security-2", "security-3")
	if got := tm.Names(); !slices.Equal(got, want) {
		t.Errorf("AC-8 Names = %q, want each agent followed by its instances: %q", got, want)
	}
	var roles []string
	for _, r := range tm.ReviewersFor(model.GateCodeReview) {
		roles = append(roles, r.Role)
	}
	if got := strings.Join(roles, " "); got != "code-reviewer security security-2 security-3 perf human-advocate" {
		t.Errorf("AC-8 the code review expects %s, want each instance after its role and none of the disabled perf's", got)
	}
}

// AC-3, AC-4, AC-5, AC-8: who stands down, and whether a path can bring it back.
func TestAC3AC4AC5WhoStandsDownAndWhetherAPathCanBringItBack(t *testing.T) {
	record := model.NewRecord("US-001", time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC))
	record.Staffing = &model.Staffing{
		At: "2026-11-04T09:00:00Z",
		Agents: []model.Staffed{
			{Agent: "perf", Needed: false}, {Agent: "red-team", Needed: false},
			{Agent: "security-2", Needed: false}, {Agent: "architect", Needed: true},
		},
		BroughtBack: map[string]string{"red-team": "internal/hook/hook.go"},
	}
	record.SetSecuritySensitive(false)
	on := team(t, `{"agents": {"security": {"instances": 2}, "human-advocate": {"enabled": false},
		"verifier": {"enabled": false}, "code-reviewer": {"enabled": false}}}`)
	for _, tc := range []struct {
		name                  string
		tm                    Team
		record                *model.Record
		agent                 string
		out, returnable, gone bool
	}{
		{"AC_3 left out by the staffing", on, record, "perf", true, true, true},
		{"AC_3 left out and brought back by a path", on, record, "red-team", true, true, false},
		{"AC_3 picked", on, record, "architect", false, true, false},
		{"AC_8 an instance left out", on, record, "security-2", true, true, true},
		{"AC_8 an instance past its role's seats", on, record, "security-3", true, false, true},
		{"AC_4 a disabled reviewer", on, record, "human-advocate", true, false, true},
		{"AC_5 a disabled verifier still reviews, its gate a person's", on, record, "verifier", false, false, false},
		{"AC_5 a disabled code reviewer still reviews", on, record, "code-reviewer", false, false, false},
		{"AC_3 no staffing recorded", on, nil, "perf", false, false, false},
		{"AC_3 the project manager disabled", team(t, `{"agents": {"project-manager": {"enabled": false}}}`), record, "perf", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, returnable := tc.tm.StandsDown(tc.record, tc.agent)
			if out != tc.out || out && returnable != tc.returnable {
				t.Errorf("StandsDown(%s) = %v, %v, want %v, %v", tc.agent, out, returnable, tc.out, tc.returnable)
			}
			if got := tc.tm.StoodDown(tc.record, tc.agent); got != tc.gone {
				t.Errorf("StoodDown(%s) = %v, want %v", tc.agent, got, tc.gone)
			}
		})
	}
	// A staffing with no brought_back map at all: left out stays out.
	record.Staffing.BroughtBack = nil
	if !on.StoodDown(record, "red-team") {
		t.Error("AC-3 a reviewer the staffing left out holds its review with nothing brought back")
	}
	// AC-10: on a story recorded as sensitive, security's instance takes its role's floor.
	record.SetSecuritySensitive(true)
	if out, _ := on.StandsDown(record, "security-2"); out {
		t.Error("AC-10 security-2 stands down on a story recorded as security-sensitive")
	}
}

// AC-3: the staffing is the project manager's, so it is on exactly where the
// project manager is enabled.
func TestAC3TheStaffingIsOnExactlyWhereTheProjectManagerIsEnabled(t *testing.T) {
	for section, want := range map[string]bool{
		`{}`: true,
		`{"agents": {"project-manager": {"enabled": true}}}`:  true,
		`{"agents": {"project-manager": {"enabled": false}}}`: false,
		`{"agents": {"project-manager": {"enabled": null}}}`:  true,
		`{"agents": {"researcher": {"enabled": false}}}`:      true,
	} {
		if got := team(t, section).StaffingOn(); got != want {
			t.Errorf("AC-3 StaffingOn for %s = %v, want %v", section, got, want)
		}
	}
}

// AC-12, AC-13, T7: the hook reads a file refused only for an old key or its team
// section with the project's own values, and the refusal set aside; anything
// else answers as Load does.
func TestAC12AC13ForRulesReadsAFileRefusedForItsTeamWithItsOwnValues(t *testing.T) {
	t.Run("AC_13 a file that loads", func(t *testing.T) {
		root := repo(t)
		writeConfig(t, root, `{"version": 1, "backlog": {"path": "stories.json"}}`)
		cfg, setAside, err := ForRules(root)
		if err != nil || setAside != nil || cfg.Backlog.Path != "stories.json" {
			t.Errorf("ForRules = backlog %q, %v, %v; want the file's own and nothing set aside", cfg.Backlog.Path, setAside, err)
		}
	})
	for _, tc := range []struct{ name, body, code string }{
		{"AC_13 a team that cannot be used", `{"version": 1, "backlog": {"path": "stories.json"}, "team": {"retries": -1}}`, codeTeam},
		{"AC_12 an old key", `{"version": 1, "backlog": {"path": "stories.json"}, "staffing": {"enabled": true}}`, codeOldKey},
		{"AC_12 gate7_advisory true", `{"version": 1, "backlog": {"path": "stories.json"}, "reviews": {"gate7_advisory": true}}`, codeOldKey},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repo(t)
			writeConfig(t, root, tc.body)
			cfg, setAside, err := ForRules(root)
			if err != nil {
				t.Fatalf("ForRules refused the file: %v", err)
			}
			if cfg.Backlog.Path != "stories.json" {
				t.Errorf("ForRules read the backlog as %q, want the file's own stories.json", cfg.Backlog.Path)
			}
			if setAside == nil || !strings.Contains(setAside.Error(), tc.code) {
				t.Errorf("the refusal set aside is %v, want one under %s", setAside, tc.code)
			}
			if _, loadErr := Load(root); loadErr == nil {
				t.Error("Load accepted the file ForRules set a refusal aside for")
			}
		})
	}
	for _, tc := range []struct{ name, body string }{
		{"AC_13 a newer format beside a team problem", `{"version": 99, "team": {"retries": -1}}`},
		{"AC_13 a setting outside the team beside an old key", `{"version": 1, "loop": {"max_stop_blocks": -1}, "staffing": {}}`},
		{"AC_13 not JSON", `{"version": 1,`},
		{"AC_13 a setting of the wrong type", `{"version": 1, "team": {"retries": "two"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repo(t)
			writeConfig(t, root, tc.body)
			cfg, setAside, err := ForRules(root)
			if err == nil || setAside != nil {
				t.Fatalf("ForRules = %v, %v; want the refusal Load gives and nothing set aside", setAside, err)
			}
			if cfg.Version != 0 || cfg.Backlog.Path != "" {
				t.Errorf("ForRules answered with values from a file it refused: %+v", cfg)
			}
			_, loadErr := Load(root)
			if loadErr == nil || codeOf(t, loadErr) != codeOf(t, err) {
				t.Errorf("ForRules refused with %v, Load with %v: want the same", err, loadErr)
			}
		})
	}
	t.Run("AC_13 no file", func(t *testing.T) {
		root := repo(t)
		if _, setAside, err := ForRules(root); err == nil || setAside != nil {
			t.Errorf("ForRules with no file = %v, %v; want Load's refusal", setAside, err)
		}
	})
	t.Run("AC_13 a file that cannot be read is the refusal Load gave", func(t *testing.T) {
		root := repo(t)
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(File)), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, setAside, err := ForRules(root); err == nil || setAside != nil {
			t.Errorf("ForRules on a directory = %v, %v; want Load's refusal", setAside, err)
		}
	})
}

// AC-2: doctor reports a reviews.gate7_advisory a released init wrote, in any
// case, and nothing else.
func TestAC2LeftoverGate7FindsTheOldKeyInAnyCaseAndNothingElse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"AC_2 the key at false", `{"version": 1, "reviews": {"gate7_advisory": false}}`, true},
		{"AC_2 the key in another case", `{"version": 1, "REVIEWS": {"Gate7_Advisory": false}}`, true},
		{"AC_2 the key at null", `{"version": 1, "reviews": {"gate7_advisory": null}}`, true},
		{"AC_2 behind a byte order mark", string([]byte{0xEF, 0xBB, 0xBF}) + `{"version": 1, "reviews": {"gate7_advisory": false}}`, true},
		{"AC_2 no reviews section", `{"version": 1}`, false},
		{"AC_2 a reviews section without it", `{"version": 1, "reviews": {"policy": "REVIEW.md"}}`, false},
		{"AC_2 the key outside reviews", `{"version": 1, "gate7_advisory": false, "loop": {"gate7_advisory": false}}`, false},
		{"AC_2 reviews not an object", `{"version": 1, "reviews": "gate7_advisory"}`, false},
		{"AC_2 not JSON", `{"reviews": {"gate7_advisory": false}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := repo(t)
			writeConfig(t, root, tc.body)
			if got := LeftoverGate7(root); got != tc.want {
				t.Errorf("LeftoverGate7 = %v, want %v", got, tc.want)
			}
		})
	}
	if LeftoverGate7(repo(t)) {
		t.Error("AC-2 LeftoverGate7 is true where there is no file")
	}
}

// AC-12: the refusal maps a staffing section of any shape, and each of its keys,
// to where it went; a name from the file is quoted and cut, so none can make a
// line of the tool's own or a refusal of a megabyte.
func TestAC12TheRefusalMapsAStaffingSectionOfAnyShape(t *testing.T) {
	long := strings.Repeat("é", 100)
	for _, tc := range []struct {
		name, section string
		says          []string
	}{
		{"AC_12 not an object", `[1, 2]`, []string{"staffing", "delete it"}},
		{"AC_12 a string", `"on"`, []string{"staffing", "delete it"}},
		{"AC_12 empty", `{}`, []string{"staffing", "empty", "delete it"}},
		{"AC_12 the lists", `{"Models": ["a"], "efforts": ["b"]}`, []string{"staffing.models moved to team.models", "staffing.efforts moved to team.efforts"}},
		{"AC_12 lowest by agent", `{"lowest": {"verifier": {"model": "opus"}, "not an agent\n": {}}}`,
			[]string{"team.agents.verifier.lowest", `team.agents."not an agent\n".lowest`}},
		{"AC_12 bring_back of another shape", `{"bring_back": ["internal/"]}`, []string{"team.agents.<agent>.bring_back"}},
		{"AC_12 an unknown key", `{"colour": 1}`, []string{`"colour"`, "no place for"}},
		{"AC_12 a long key is cut", `{"` + long + `": 1}`, []string{"(cut, 200 bytes)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal := refusalOf(t, `{"version": 1, "staffing": `+tc.section+`}`)
			if refusal.Code.String() != codeOldKey {
				t.Fatalf("refused with %s, want %s: %s", refusal.Code, codeOldKey, refusal.Why)
			}
			for _, want := range tc.says {
				if !strings.Contains(refusal.Why, want) {
					t.Errorf("the refusal does not say %q: %s", want, refusal.Why)
				}
			}
			if strings.ContainsAny(refusal.Why, "\n\r") {
				t.Errorf("the refusal holds a line break from the file: %q", refusal.Why)
			}
		})
	}
}

// AC-13: a list entry that holds a control character is named by its list and
// position, quoted; a key over the name size is cut where a rune starts.
func TestAC13AListEntryWithAControlCharacterIsNamedQuoted(t *testing.T) {
	refusal := refusalOf(t, teamConfig(`{"models": ["opus", "son\u001bnet", "son\u001bnet"], "efforts": ["lo\u0085w"]}`))
	if refusal.Code.String() != codeTeam {
		t.Fatalf("refused with %s, want %s", refusal.Code, codeTeam)
	}
	for _, want := range []string{"team.models entry 2", `"son\x1bnet"`, "control character", "team.efforts entry 1", `"lo\u0085w"`} {
		if !strings.Contains(refusal.Why, want) {
			t.Errorf("AC-13 the refusal does not say %q: %s", want, refusal.Why)
		}
	}
	if strings.Count(refusal.Why, `"son\x1bnet"`) != 1 {
		t.Errorf("AC-13 an entry repeated is named more than once: %s", refusal.Why)
	}
	if strings.ContainsRune(refusal.Why, 0x1b) {
		t.Error("AC-13 the refusal holds the raw escape")
	}
	if refusal.Code != sdlcerr.TeamUnusable {
		t.Errorf("AC-13 the refusal is not TeamUnusable: %s", refusal.Code)
	}
}

// AC-13, red team R9: problems that are all different fill the refusal to its
// size and are then only counted, said as "and N more", and the agents after
// them are counted too, not each made a line.
func TestAC13ARefusalPastItsSizeCountsTheRestAsMore(t *testing.T) {
	const entries = 2000
	models := make([]string, entries)
	for i := range models {
		// Each entry distinct, each holding a control character, as JSON spells one.
		models[i] = `"m\u0001` + strconv.Itoa(i) + `"`
	}
	refusal := refusalOf(t, teamConfig(`{"models": [`+strings.Join(models, ",")+`],
		"agents": {"researcher": {"bring_back": ["internal/"]}, "nobody": {}}}`))
	if refusal.Code.String() != codeTeam {
		t.Fatalf("refused with %s, want %s", refusal.Code, codeTeam)
	}
	if got := len(refusal.What) + len(refusal.Why); got > 4096 {
		t.Errorf("AC-13 a refusal over %d distinct problems is %d bytes, want a short one", entries, got)
	}
	more := regexp.MustCompile(`; and (\d+) more$`).FindStringSubmatch(refusal.Why)
	if more == nil {
		t.Fatalf("AC-13 the refusal does not end by counting the rest: %s", refusal.Why)
	}
	kept := strings.Count(refusal.Why, "team.models entry ")
	if n, _ := strconv.Atoi(more[1]); kept == 0 || kept+n != entries+2 {
		t.Errorf("AC-13 the refusal names %d problems and counts %s more, want %d in all", kept, more[1], entries+2)
	}
	if strings.Contains(refusal.Why, "nobody") || strings.Contains(refusal.Why, "researcher") {
		t.Errorf("AC-13 an agent past the size was named: %s", refusal.Why)
	}
}

// AC-8, AC-11, AC-13: an instance's name is not a key of its own, and bring_back
// is only for a reviewer of the code, since a change brings an agent back only
// to the code review.
func TestAC8AC13AnInstancesNameAsAKeyAndBringBackOnAnotherAgentAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name, section string
		says          []string
	}{
		{"AC_8 an instance's name as a key", `{"agents": {"security-2": {"enabled": false}}}`,
			[]string{`"security-2"`, "instance", "instances on security"}},
		{"AC_13 bring_back on the architect", `{"agents": {"architect": {"bring_back": ["internal/"]}}}`,
			[]string{"team.agents.architect.bring_back", "code-reviewer", "security", "perf"}},
		{"AC_13 bring_back on a worker", `{"agents": {"researcher": {"bring_back": ["internal/"]}}}`,
			[]string{"team.agents.researcher.bring_back"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal := refusalOf(t, teamConfig(tc.section))
			if refusal.Code.String() != codeTeam {
				t.Fatalf("refused with %s, want %s: %s", refusal.Code, codeTeam, refusal.Why)
			}
			for _, want := range tc.says {
				if !strings.Contains(refusal.Why, want) {
					t.Errorf("the refusal does not say %q: %s", want, refusal.Why)
				}
			}
		})
	}
	if _, err := Parse([]byte(teamConfig(`{"agents": {"perf": {"bring_back": ["internal/"]}, "human-advocate": {"bring_back": ["docs/"]}}}`))); err != nil {
		t.Errorf("AC-11 bring_back on a reviewer of the code does not load: %v", err)
	}
}

// T11: a name from the file over the name size is cut where a rune starts, so
// the refusal is valid UTF-8 and says how long the name was.
func TestAC12ALongNameIsCutWhereARuneStarts(t *testing.T) {
	long := "a" + strings.Repeat("é", 100) // byte 64 is the middle of an é
	refusal := refusalOf(t, `{"version": 1, "staffing": {"`+long+`": 1}}`)
	if !strings.Contains(refusal.Why, "(cut, 201 bytes)") {
		t.Errorf("T11 the refusal does not say the name was cut from 201 bytes: %s", refusal.Why)
	}
	if !utf8.ValidString(refusal.Why) {
		t.Errorf("T11 the refusal cut a name inside a rune: %q", refusal.Why)
	}
	if strings.Contains(refusal.Why, long) {
		t.Error("T11 the refusal holds the whole name")
	}
}
