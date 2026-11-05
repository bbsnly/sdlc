package cli

// US-109: one team section configures every agent, with the project manager always
// staffing. Every project here is one the test makes. None reads this repository's own
// configuration, which is the person's to edit (ANALYSIS.md, section 5).
//
// ANALYSIS.md section 0 holds the person's answers, which override the backlog's wording
// of AC-7 (D-3: max_rounds counts blocks that stop the gate), AC-12 (D-1:
// reviews.gate7_advisory is refused only when true) and AC-13 (D-2: SDLC-E0045 stays
// live). The compliance half of AC-6 and AC-10 has no observable behaviour in this
// story: no agent of that name ships (section 9).
//
// What the tests bind that the criteria leave to the design:
//
//	sdlc team failure AGENT --reason TEXT      and, with --json, {ok, story, agent, gate,
//	                                           failures, handed_over}
//	"disabled": [names]                        on `status --json`, `staffing show --json`
//	                                           and `gate --json`, left out when empty
//	"by_hand": "agent"                         on `staffing show --json`: the disabled
//	                                           agent whose work the gate is, for the runbook
//	`staffing show` with the project manager
//	disabled                                   exit 0, no rows, at_gate as every enabled agent
//	a staffing row for each copy, `security-2` the rows of `staffing set` and `show`
//	SDLC-E0087 and SDLC-E0088                  an old key, and a team that cannot be used

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/shellpolicy"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	teamCodeOld = "SDLC-E0087" // an old setting the team replaced
	teamCodeBad = "SDLC-E0088" // a team section that cannot be used
)

type agentEntries map[string]map[string]any

func teamOf(entries agentEntries) map[string]any {
	agents := map[string]any{}
	for name, entry := range entries {
		agents[name] = entry
	}
	return map[string]any{"agents": agents}
}

// teamProject is a project with the team section given, committed, and nothing started.
// The section replaces the one the helpers write, so the project manager is on unless it
// says otherwise.
func teamProject(t *testing.T, section map[string]any) string {
	t.Helper()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "team", section)
	commitEverything(t, root)
	return root
}

// teamAtAnalysis is teamProject with its story started and found ready.
func teamAtAnalysis(t *testing.T, section map[string]any) string {
	t.Helper()
	root := teamProject(t, section)
	mustRun(t, "start")
	mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	return root
}

// teamStaffedAnalysed is teamAtAnalysis with the staffing recorded and the analysis passed.
func teamStaffedAnalysed(t *testing.T, section map[string]any, doc stfDoc, sensitive bool) string {
	t.Helper()
	root := teamAtAnalysis(t, section)
	stfSet(t, doc)
	stfAnalysed(t, sensitive)
	return root
}

// teamThrough takes a started story, with the project manager disabled, through every
// gate before upTo, its analysis saying what sensitive says.
func teamThrough(t *testing.T, root string, upTo model.Gate, sensitive bool) {
	t.Helper()
	mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	stfAnalysed(t, sensitive)
	stfThrough(t, root, upTo)
}

// plus is the document with a needed row for a copy, with its own model and effort.
func (d stfDoc) plus(name, askedModel, askedEffort string) stfDoc {
	out := stfDoc{Agents: slices.Clone(d.Agents)}
	out.Agents = append(out.Agents, stfAgent{Agent: name, Needed: true, Reason: name + " has work on this story",
		AskedModel: askedModel, AskedEffort: askedEffort, AskedReason: "a copy reads it with another model"})
	return out
}

// leaving is the document with the row for an agent saying it is not needed.
func (d stfDoc) leaving(name string) stfDoc {
	return d.with(name, func(a *stfAgent) {
		*a = stfAgent{Agent: name, Needed: false, Reason: name + " is not asked for here"}
	})
}

// teamShown is `sdlc staffing show --json`, with what this story adds.
type teamShown struct {
	stfShown
	Disabled []string `json:"disabled"`
	ByHand   string   `json:"by_hand"`
}

func teamShow(t *testing.T, args ...string) teamShown {
	t.Helper()
	return decode[teamShown](t, mustRun(t, append(append([]string{"staffing", "show"}, args...), "--json")...))
}

func atGateNames(s teamShown) []string {
	names := make([]string, 0, len(s.AtGate))
	for _, a := range s.AtGate {
		names = append(names, a.Agent)
	}
	return names
}

// saysDisabled holds output to a line that names the agent and says it is disabled.
func saysDisabled(t *testing.T, where, out string, agents ...string) {
	t.Helper()
	lines := strings.Split(out, "\n")
	for _, agent := range agents {
		if !slices.ContainsFunc(lines, func(l string) bool {
			return strings.Contains(l, agent) && strings.Contains(strings.ToLower(l), "disabled")
		}) {
			t.Errorf("%s does not name %s as disabled:\n%s", where, agent, out)
		}
	}
}

// teamWhy is a refusal with its fix line taken out: what it says of this case, which a fix
// naming every case cannot stand in for.
func teamWhy(stderr string) string {
	var kept []string
	for line := range strings.SplitSeq(stderr, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "fix ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// doctorConfiguration is the configuration check of `sdlc doctor --json`.
func doctorConfiguration(t *testing.T) check {
	t.Helper()
	got := decode[doctorPayload](t, run(t, "doctor", "--json"))
	for _, c := range got.Checks {
		if c.Name == checkConfiguration {
			return c
		}
	}
	t.Fatalf("doctor has no configuration check: %+v", got.Checks)
	return check{}
}

// doctorProblemsBesideTheSetUp is what doctor calls a problem, past the two checks that
// are about the test's project and not about the team: blank commands, a problem in a
// project gitProjectInitialised made, and sdlc on PATH, which reads the runner. A frozen
// exit 0 here could not pass under any implementation, because of those two. This holds
// what that exit was for: a team setting is no problem of its own, in any check, since
// any problem makes doctor exit 1 and doctor's exit code does not change in this story.
func doctorProblemsBesideTheSetUp(t *testing.T) []check {
	t.Helper()
	var problems []check
	for _, c := range decode[doctorPayload](t, run(t, "doctor", "--json")).Checks {
		if c.State == stateProblem && c.Name != checkCommands && c.Name != checkSdlcOnPath {
			problems = append(problems, c)
		}
	}
	return problems
}

// ---------------------------------------------------------------------- AC-1

// AC-1 and AC-2: absent means the defaults, whatever is absent: the section, its entries,
// or a key of an entry. The default can_block reproduces what the released tool does.
func TestAC1EveryAgentIsOnByDefaultAndEachReviewerBlocksAsTheReleasedToolDoes(t *testing.T) {
	t.Parallel()
	for name, section := range map[string]map[string]any{
		"AC_1 an empty section": {},
		"AC_1 empty entries": teamOf(agentEntries{"architect": {}, "security": {}, "red-team": {}, "perf": {},
			"human-advocate": {}, "verifier": {}, "code-reviewer": {}, "project-manager": {}}),
		"AC_1 null keys": teamOf(agentEntries{"architect": {"enabled": nil, "can_block": nil, "max_rounds": nil, "instances": nil}}),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := teamProject(t, section)
			mustRun(t, "start")
			if got := teamShowDisabledInStatus(t); len(got) != 0 {
				t.Errorf("AC-1 status names %q as disabled with nothing set", got)
			}
			if got := decode[statusPayload](t, mustRun(t, "status", "--json")); got.Staffing != "none" {
				t.Errorf("AC-1 status --json says staffing %q, want none: the project manager is on by default", got.Staffing)
			}
			for _, r := range decode[reviewListPayload](t, mustRun(t, "review", "list", "--json")).Reviews {
				wantBlocking := r.Role == "architect" || r.Role == "verifier" || r.Role == "code-reviewer" || r.Role == "security"
				if r.Blocking != wantBlocking {
					t.Errorf("AC-1 %s at %s: blocking %v, want %v", r.Role, r.Gate, r.Blocking, wantBlocking)
				}
				if r.BlocksOnBreach != (r.Role == "perf") {
					t.Errorf("AC-1 %s at %s: blocks on breach %v", r.Role, r.Gate, r.BlocksOnBreach)
				}
				if r.LeftOut {
					t.Errorf("AC-1 %s at %s is left out with nothing recorded", r.Role, r.Gate)
				}
			}
			_ = root
		})
	}
}

func teamShowDisabledInStatus(t *testing.T) []string {
	t.Helper()
	var got struct {
		Disabled []string `json:"disabled"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, "status", "--json").stdout), &got); err != nil {
		t.Fatal(err)
	}
	return got.Disabled
}

// ---------------------------------------------------------------------- AC-2

// D-3 settled it: init writes no max_rounds, because the default is a reference to
// another setting, and written per agent it would stop following it.
func TestAC2InitSpellsOutTheTeamDefaults(t *testing.T) {
	t.Parallel()
	root := gitProject(t)
	mustRun(t, "init")
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Team *struct {
			Retries *int     `json:"retries"`
			Models  []string `json:"models"`
			Efforts []string `json:"efforts"`
			Agents  map[string]struct {
				Enabled   *bool           `json:"enabled"`
				CanBlock  *bool           `json:"can_block"`
				Instances *int            `json:"instances"`
				MaxRounds json.RawMessage `json:"max_rounds"`
				Lowest    json.RawMessage `json:"lowest"`
			} `json:"agents"`
		} `json:"team"`
		Reviews json.RawMessage `json:"reviews"`
		Comment []string        `json:"_team"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.Team == nil {
		t.Fatalf("AC-2 init wrote no team section:\n%s", raw)
	}
	if file.Team.Retries == nil || *file.Team.Retries != 2 {
		t.Errorf("AC-2 team.retries = %v, want 2", file.Team.Retries)
	}
	if want := []string{"haiku", "sonnet", "opus"}; !slices.Equal(file.Team.Models, want) {
		t.Errorf("AC-11 team.models = %q, want %q", file.Team.Models, want)
	}
	if want := []string{"low", "medium", "high", "xhigh", "max"}; !slices.Equal(file.Team.Efforts, want) {
		t.Errorf("AC-11 team.efforts = %q, want %q", file.Team.Efforts, want)
	}
	if file.Reviews != nil {
		t.Errorf("AC-2 init still writes a reviews section: %s", file.Reviews)
	}
	blocks := map[string]bool{"architect": true, "security": true, "perf": true, "verifier": true, "code-reviewer": true,
		"red-team": false, "human-advocate": false}
	for _, agent := range stfAgents {
		entry, ok := file.Team.Agents[agent]
		if !ok {
			t.Errorf("AC-2 init writes no entry for %s", agent)
			continue
		}
		if entry.Enabled == nil || !*entry.Enabled {
			t.Errorf("AC-2 %s is not written as enabled", agent)
		}
		if entry.Instances == nil || *entry.Instances != 1 {
			t.Errorf("AC-2 %s is not written with instances 1", agent)
		}
		if want, reviewer := blocks[agent]; reviewer && (entry.CanBlock == nil || *entry.CanBlock != want) {
			t.Errorf("AC-2 %s: can_block is written as %v, want %v", agent, entry.CanBlock, want)
		}
		if entry.MaxRounds != nil {
			t.Errorf("AC-2 %s: init writes max_rounds, which would stop following loop.max_review_rounds", agent)
		}
		if entry.Lowest != nil {
			t.Errorf("AC-11 %s: init writes a lowest, and there is no default one", agent)
		}
	}
	// The form of the others: a title, what changing it costs, the address of its heading.
	if len(file.Comment) < 3 {
		t.Fatalf("AC-2 the team section's comment block is %q", file.Comment)
	}
	if last := file.Comment[len(file.Comment)-1]; !strings.HasSuffix(last, "docs/configuration.md#team") {
		t.Errorf("AC-2 the block does not end with the address of the section's heading: %q", last)
	}
	// What init wrote is a file nothing has to be said about.
	commitEverything(t, root)
	c := doctorConfiguration(t)
	if c.State != stateOK {
		t.Errorf("AC-2 doctor on the file init wrote: %+v", c)
	}
	for _, word := range []string{"disabled", "can_block", "instances", "max_rounds"} {
		if strings.Contains(c.Detail, word) {
			t.Errorf("AC-2 doctor reports %q on a file with every default: %s", word, c.Detail)
		}
	}
}

// teamReleasedFile is the file `sdlc init` wrote at v0.5.0, less its comments: no team
// section, and the key AC-12 refuses only when true (D-1).
const teamReleasedFile = `{
  "version": 1,
  "backlog": { "path": "user_stories.json" },
  "git": { "trunk_branch": "main", "remote": false, "worktrees": false },
  "commands": { "test": "" },
  "thresholds": { "coverage_min": 80, "mutation_min": 70 },
  "paths": {
    "tests": { "dirs": ["tests/","test/"], "file_globs": ["*_test.go"] },
    "src": ["src/"]
  },
  "spec": { "paths": ["docs/", "spec/", "README.md"] },
  "loop": { "max_review_rounds": 2, "max_rework_rounds": 3, "max_stop_blocks": 3 },
  "reviews": { "gate7_advisory": false },
  "freeze": { "allow_new_test_files": false },
  "human_gates": { "pre_commit_pause_tiers": ["high"], "dor_advocate_check": false },
  "budget": { "per_story_usd": 60, "alert_fractions": [0.5, 0.8, 1.0] }
}
`

// AC-2's last clause holds as written (D-1): a project init wrote keeps working, with the
// project manager on by default, and doctor reports the leftover without failing a script.
func TestAC2AFileInitWroteAtAnEarlierReleaseKeepsWorkingAsItStands(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	writeFile(t, root, ".sdlc/config.json", teamReleasedFile)
	commitEverything(t, root)

	c := doctorConfiguration(t)
	if c.State != stateOK {
		t.Errorf("AC-2 doctor reports a problem on a file init wrote: %+v", c)
	}
	for _, want := range []string{"reviews.gate7_advisory", "team.agents.code-reviewer.can_block"} {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("AC-12 doctor's leftover line does not name %s: %s", want, c.Detail)
		}
	}
	if extra := doctorProblemsBesideTheSetUp(t); len(extra) != 0 {
		t.Errorf("AC-2 doctor makes a problem of a file init wrote, which would fail every script on every project: %+v", extra)
	}

	mustRun(t, "start")
	mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	stfRefused(t, run(t, "gate", "analysis", "pass"), stfCodeNone)
	stfSet(t, stfFull())
	mustRun(t, "gate", "analysis", "pass", "--security-sensitive=false")
	// The leftover means nothing: the code reviewer blocks, as it always did here.
	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--json"))
	if r, ok := listed.review("code_review", "code-reviewer"); !ok || !r.Blocking {
		t.Errorf("AC-2 the code reviewer is %+v with gate7_advisory false", r)
	}
}

// ---------------------------------------------------------------------- AC-3

func TestAC3WithNoTeamSectionTheAnalysisGateWaitsForAStaffing(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	teamOutOfTheFile(t, root)
	commitEverything(t, root)
	mustRun(t, "start")
	mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	r := stfUnchanged(t, root, "", "gate", "analysis", "pass", "--security-sensitive=false")
	stfRefused(t, r, stfCodeNone)
	if fix := stfFixLine(r.stderr); !strings.Contains(fix, "sdlc:project-manager") {
		t.Errorf("AC-3 the refusal does not send the reader to the project manager: %s", fix)
	}
	stfSet(t, stfFull())
	mustRun(t, "gate", "analysis", "pass", "--security-sensitive=false")
}

// AC-3 "cannot block": a left-out reviewer's review is recorded today, and its block
// counts towards a hand-over. It is refused, in the pattern of a review a gate does not
// expect, so no new code (ANALYSIS.md section 10). Refused to the block and to anything
// else, and nothing is counted.
func TestAC3AnAgentTheStaffingDidNotPickCannotReviewOrBlock(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "perf", "red-team")
	stfThrough(t, root, model.GateDesignReview, "perf", "red-team")
	// A block names an important finding, as US-115 requires: without one it is refused
	// for that, and the row would pass with the reviewer still taken.
	for _, verdict := range [][]string{{"block", "--important", "1", "--finding", "important the one finding"}, {"approve"}, {"note"}} {
		for _, role := range []string{"perf", "red-team"} {
			r := stfUnchanged(t, root, "# "+role+"\n", append([]string{"review", "add", "design_review", role}, verdict...)...)
			stfRefused(t, r, "SDLC-E0027", role)
		}
	}
	for range 3 {
		r := runWith(t, "# perf\n", "review", "add", "design_review", "perf", "block", "--important", "1", "--finding", "important the one finding")
		if strings.Contains(r.stdout, "handed") || r.code == 0 {
			t.Errorf("AC-3 a block from a reviewer left out was taken: exit %d\n%s", r.code, r.stdout)
		}
	}
	if got := decode[activePayload](t, mustRun(t, "status", "--json")); got.Active == nil {
		t.Error("AC-3 a reviewer left out handed the story over")
	}
	// Picked after all, it reviews, from then on.
	mustRun(t, "staffing", "amend", "perf", "--needed", "--model", "sonnet", "--effort", "medium", "--reason", "the plan touches a hot path")
	mustRunWith(t, "# perf\n", "review", "add", "design_review", "perf", "note")
}

func TestAC3TheProjectManagerPicksFromTheEnabledAgentsOnly(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"red-team": {"enabled": false}, "researcher": {"enabled": false}})

	t.Run("AC_3 a row that asks for a disabled agent is refused", func(t *testing.T) {
		t.Parallel()
		root := teamAtAnalysis(t, section)
		r := stfUnchanged(t, root, stfFull().leaving("researcher").text(t), "staffing", "set")
		stfRefused(t, r, stfCodeIncomplete, "red-team", "disabled")
		r = stfUnchanged(t, root, stfFull().leaving("red-team").text(t), "staffing", "set")
		stfRefused(t, r, stfCodeIncomplete, "researcher", "disabled")
	})

	t.Run("AC_3 a row that leaves a disabled agent out is taken, worker or not", func(t *testing.T) {
		t.Parallel()
		root := teamAtAnalysis(t, section)
		stfSet(t, stfFull().leaving("red-team").leaving("researcher"))
		for _, args := range [][]string{
			{"staffing", "amend", "red-team", "--needed", "--model", "sonnet", "--effort", "low", "--reason", "after all"},
			{"staffing", "amend", "researcher", "--needed", "--model", "sonnet", "--effort", "low", "--reason", "after all"},
		} {
			r := stfUnchanged(t, root, "", args...)
			if r.code == 0 || !strings.Contains(r.stderr, "disabled") {
				t.Errorf("AC-3 an amendment brought in a disabled agent: exit %d\n%s", r.code, r.stderr)
			}
		}
		// The exception is the disabled agent's, and reaches no other agent of the floor.
		r := stfUnchanged(t, root, "", "staffing", "amend", "verifier", "--left-out", "--reason", "it is off for researcher, why not here")
		stfRefused(t, r, stfCodeFloor, "verifier")
	})
}

// AC-3: nothing that looks like a switch turns the staffing off. Only a person's
// `enabled: false` on the project manager means no staffing.
func TestAC3ThereIsNoSettingThatTurnsTheStaffingOff(t *testing.T) {
	t.Parallel()
	root := teamProject(t, map[string]any{"enabled": false})
	if got := doctorConfiguration(t); got.State != stateProblem || !strings.Contains(got.Detail, "team.enabled") {
		t.Errorf("AC-3 doctor does not call team.enabled a setting it does not know: %+v", got)
	}
	mustRun(t, "start")
	if got := decode[statusPayload](t, mustRun(t, "status", "--json")); got.Staffing != "none" {
		t.Errorf("AC-3 team.enabled false changed the staffing to %q", got.Staffing)
	}
	_ = root
}

// AC-3 and the notes: with the project manager disabled there is no staffing, and every
// enabled agent takes part in every story. `staffing show` says why, and at_gate is still
// what the runbook reads before every delegation.
func TestAC3WithTheProjectManagerDisabledThereIsNoStaffingAndEveryEnabledAgentTakesPart(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"project-manager": {"enabled": false}, "red-team": {"enabled": false}})
	root := teamAtAnalysis(t, section)

	got := teamShow(t)
	if !got.OK || len(got.Agents) != 0 {
		t.Errorf("AC-3 staffing show --json has rows with no staffing: %+v", got)
	}
	if !slices.Contains(got.Disabled, "project-manager") || !slices.Contains(got.Disabled, "red-team") {
		t.Errorf("AC-4 staffing show --json does not name the disabled agents: %+v", got.Disabled)
	}
	if want := []string{"researcher"}; !slices.Equal(atGateNames(got), want) {
		t.Errorf("AC-3 at_gate at the analysis is %q, want %q", atGateNames(got), want)
	}
	prose := mustRun(t, "staffing", "show").stdout
	lower := strings.ToLower(prose)
	if !strings.Contains(lower, "no staffing") || (!strings.Contains(lower, "project manager") && !strings.Contains(lower, "project-manager")) {
		t.Errorf("AC-3 staffing show does not say no staffing exists because the project manager is off:\n%s", prose)
	}
	saysDisabled(t, "staffing show", prose, "project-manager", "red-team")

	var status map[string]any
	if err := json.Unmarshal([]byte(mustRun(t, "status", "--json").stdout), &status); err != nil {
		t.Fatal(err)
	}
	if _, ok := status["staffing"]; ok {
		t.Errorf("AC-3 status --json has a staffing, so the runbook's `none` test would fire: %v", status["staffing"])
	}

	stfAnalysed(t, true)
	stfThrough(t, root, model.GateDesignReview, "red-team")
	if want := []string{"implementer"}; slices.Equal(atGateNames(teamShow(t)), want) {
		t.Errorf("AC-3 at_gate lists only the worker at design_review: %+v", teamShow(t).AtGate)
	}
	design := atGateNames(teamShow(t, "--gate", "design_review"))
	for _, want := range []string{"architect", "security", "perf", "human-advocate"} {
		if !slices.Contains(design, want) {
			t.Errorf("AC-3 at_gate at design_review has no %s: %q", want, design)
		}
	}
	if slices.Contains(design, "red-team") {
		t.Errorf("AC-4 at_gate at design_review has the disabled red team: %q", design)
	}
	r := run(t, "gate", "design_review", "pass")
	stfRefused(t, r, "SDLC-E0029", "architect", "security", "perf", "human-advocate")
	// By what it waits for and not by the name: a refused gate names the disabled agents too.
	if strings.Contains(r.stderr, "red-team has not") {
		t.Errorf("AC-4 design_review waits for the disabled red team:\n%s", r.stderr)
	}
}

// ANALYSIS.md section 6: a record is not read for who waits while the project manager is
// off, and applies again once it is back.
func TestAC3DisablingTheProjectManagerMidStoryStopsTheRecordDecidingWhoWaits(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "red-team", "perf")
	stfThrough(t, root, model.GateDesignReview, "red-team", "perf")
	if r := run(t, "gate", "design_review", "pass"); strings.Contains(r.stderr, "red-team") {
		t.Fatalf("the staffing left the red team out and the gate waits for it:\n%s", r.stderr)
	}

	setConfigOnDisk(t, root, "team", teamOf(agentEntries{"project-manager": {"enabled": false}}))
	r := run(t, "gate", "design_review", "pass")
	stfRefused(t, r, "SDLC-E0029", "red-team", "perf")
	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "design_review", "--json"))
	if got, _ := listed.review("design_review", "red-team"); got.LeftOut {
		t.Errorf("AC-3 the red team reads as left out with no staffing in force: %+v", got)
	}
	if !strings.Contains(stfRecord(t, root), `"staffing"`) {
		t.Error("AC-3 disabling the project manager took the staffing off the record, where `sdlc log` reads it")
	}

	setConfigOnDisk(t, root, "team", stfSection())
	if r := run(t, "gate", "design_review", "pass"); strings.Contains(r.stderr, "red-team") {
		t.Errorf("AC-3 with the project manager back the record does not apply again:\n%s", r.stderr)
	}
}

// AC-3 and T10: every reader asks one rule. A second copy is how a gate and its list
// disagree. For each state: `review list`, `staffing show`'s at_gate and the gate's own
// refusal say the same of whether the gate waits, and the hook's half is
// TestAC4TheHookAsksTheSameRuleOfADisabledReviewersStaleReview.
func TestAC3TheGateItsListAndTheStaffingAskOneRuleOfWhoIsWaitedFor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		section map[string]any
		doc     stfDoc
		role    string
		waited  bool
	}{
		{"AC_3 picked", map[string]any{}, stfFull(), "human-advocate", true},
		{"AC_3 left out", map[string]any{}, stfFull().leaving("human-advocate"), "human-advocate", false},
		{"AC_4 needed in the row, disabled in the file", teamOf(agentEntries{"human-advocate": {"enabled": false}}),
			stfFull().leaving("human-advocate"), "human-advocate", false},
		{"AC_8 a copy picked", teamOf(agentEntries{"perf": {"instances": 2}}),
			stfFull().plus("perf-2", "sonnet", "low"), "perf-2", true},
		{"AC_8 a copy left out", teamOf(agentEntries{"perf": {"instances": 2}}),
			stfFull().plus("perf-2", "sonnet", "low").leaving("perf-2"), "perf-2", false},
		{"AC_4 a copy of an agent disabled", teamOf(agentEntries{"perf": {"instances": 2, "enabled": false}}),
			stfFull().leaving("perf"), "perf-2", false},
		{"AC_3 the second copy beyond instances", teamOf(agentEntries{"perf": {"instances": 2}}),
			stfFull().plus("perf-2", "sonnet", "low"), "perf-3", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := teamStaffedAnalysed(t, tc.section, tc.doc, false)
			stfThrough(t, root, model.GateDesignReview, "human-advocate", "perf", "perf-2")

			named := slices.Contains(atGateNames(teamShow(t, "--gate", "design_review")), tc.role)
			if named != tc.waited {
				t.Errorf("staffing show: at_gate names %s = %v, want %v", tc.role, named, tc.waited)
			}
			listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "design_review", "--json"))
			entry, ok := listed.review("design_review", tc.role)
			if waits := ok && !entry.LeftOut; waits != tc.waited {
				t.Errorf("review list: %s waits = %v (%+v), want %v", tc.role, waits, entry, tc.waited)
			}
			r := run(t, "gate", "design_review", "pass")
			if inRefusal := strings.Contains(r.stderr, tc.role+" has not reviewed"); inRefusal != tc.waited {
				t.Errorf("the gate: waits for %s = %v, want %v:\n%s%s", tc.role, inRefusal, tc.waited, r.stdout, r.stderr)
			}
		})
	}
}

// ---------------------------------------------------------------------- AC-4

// The person's decision at Gate 4 (red team C5, architect note 3): a disabled agent's
// instances need no row and are not waited for. For the verifier and the code reviewer,
// whose gate stays the person's, that leaves one review to record by hand: the base role's.
// With the plan's words the verifier was a story that could not pass analysis.
func TestAC4ADisabledAgentsInstancesNeedNoRowAndAreNotWaitedFor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		agent string
		gate  model.Gate
	}{
		{"verifier", model.GateVerifierReview},
		{"code-reviewer", model.GateCodeReview},
	} {
		t.Run("AC_4 "+tc.agent, func(t *testing.T) {
			t.Parallel()
			instance := tc.agent + "-2"
			root := teamAtAnalysis(t, teamOf(agentEntries{tc.agent: {"instances": 2, "enabled": false}}))
			stfSet(t, stfFull().leaving(tc.agent))
			stfAnalysed(t, false)
			stfThrough(t, root, tc.gate, tc.agent)

			got := teamShow(t, "--gate", string(tc.gate))
			if at := atGateNames(got); slices.Contains(at, instance) || slices.Contains(at, tc.agent) {
				t.Errorf("AC-4 at_gate at %s delegates to a disabled agent: %q", tc.gate, at)
			}
			if got.ByHand != tc.agent {
				t.Errorf("AC-5 by_hand at %s = %q, want the base role, %s", tc.gate, got.ByHand, tc.agent)
			}
			listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", string(tc.gate), "--json"))
			if entry, ok := listed.review(string(tc.gate), instance); ok && !entry.LeftOut {
				t.Errorf("AC-4 review list waits for %s: %+v", instance, entry)
			}
			refusal := run(t, "gate", string(tc.gate), "pass")
			stfRefused(t, refusal, "SDLC-E0029", tc.agent)
			if strings.Contains(refusal.stderr, instance) {
				t.Errorf("AC-4 the gate waits for %s:\n%s", instance, refusal.stderr)
			}
			unwaited := stfUnchanged(t, root, "# by hand\n", "review", "add", string(tc.gate), instance, "approve")
			stfRefused(t, unwaited, "SDLC-E0027", instance)
			// Red team R5 (V16): the instance is within instances, and its role is disabled. Told
			// "past instances", a person would raise a setting that changes nothing.
			why := strings.ToLower(teamWhy(unwaited.stderr))
			if !strings.Contains(why, "disabled") || regexp.MustCompile(`\bpast\b`).MatchString(why) {
				t.Errorf("AC-4 the refusal of %s does not say past its fix line that its role is disabled:\n%s", instance, unwaited.stderr)
			}
			// The gate's other reviewers are not this row's business, and the base role is.
			stfReviewed(t, tc.gate, tc.agent)
			mustRunWith(t, "# by hand\n", "review", "add", string(tc.gate), tc.agent, "approve")
			mustRun(t, "gate", string(tc.gate), "pass", "--note", "a person read it")
		})
	}
}

// Red team V4: `review list` read the row and ignored `enabled`, and every row that read it
// had a staffing that said left out already. Human advocate 8: the two mean different next
// steps, ask the project manager again or edit the file, so a person is told which.
func TestAC4ReviewListAndStaffingShowSayDisabledOrLeftOutWithWhatToDo(t *testing.T) {
	t.Parallel()
	type listed struct {
		Reviews []struct {
			Gate     string `json:"gate"`
			Role     string `json:"role"`
			LeftOut  bool   `json:"left_out"`
			Disabled bool   `json:"disabled"`
		} `json:"reviews"`
	}
	// `staffing show` opens with what each reviewer's bring_back says, and those lines are not
	// the agent's row.
	lineNaming := func(out, agent string) string {
		for line := range strings.SplitSeq(out, "\n") {
			if strings.Contains(line, agent) && !strings.Contains(line, "is brought back to") {
				return line
			}
		}
		return ""
	}
	root := stfStaffed(t, "perf")
	stfThrough(t, root, model.GateDesignReview, "perf")
	// Every row says needed but perf's, and this is the file's to say.
	setTeamAgentOnDisk(t, root, "human-advocate", map[string]any{"enabled": false})

	for _, r := range decode[listed](t, mustRun(t, "review", "list", "--gate", "design_review", "--json")).Reviews {
		switch r.Role {
		case "human-advocate":
			if !r.LeftOut || !r.Disabled {
				t.Errorf("AC-4 review list --json, a row that says needed and disabled in the file: %+v", r)
			}
		case "perf":
			if !r.LeftOut || r.Disabled {
				t.Errorf("AC-4 review list --json, a reviewer the staffing left out: %+v", r)
			}
		case "architect":
			if r.LeftOut || r.Disabled {
				t.Errorf("AC-4 review list --json, a reviewer nothing touched: %+v", r)
			}
		}
	}
	for name, out := range map[string]string{
		"sdlc review list":   mustRun(t, "review", "list", "--gate", "design_review").stdout,
		"sdlc staffing show": mustRun(t, "staffing", "show", "--gate", "design_review").stdout,
	} {
		line := lineNaming(out, "human-advocate")
		for _, want := range []string{"disabled", "team.agents.human-advocate.enabled"} {
			if !strings.Contains(line, want) {
				t.Errorf("AC-4 %s: the line for a disabled agent does not say %q:\n%s", name, want, out)
			}
		}
		line = lineNaming(out, "perf")
		if !strings.Contains(line, "left out") || strings.Contains(line, "disabled") {
			t.Errorf("AC-4 %s: the line for an agent the staffing left out does not say so:\n%s", name, out)
		}
		if strings.Contains(out, "stood down") {
			t.Errorf("AC-4 %s says stood down, which is the code's word and no person's:\n%s", name, out)
		}
	}
	// The reason is what a person asks the project manager about.
	if line := lineNaming(mustRun(t, "review", "list", "--gate", "design_review").stdout, "perf"); !strings.Contains(line, "nothing to read") {
		t.Errorf("AC-4 sdlc review list does not give the staffing's reason for leaving perf out: %s", line)
	}
}

// Red team V6: step 9 promises every outcome, and the rows read passes and one fail at the
// retro. A refusal is where a person most wants to know who is not coming.
func TestAC4ARefusedGateAndAFailedGateNameTheDisabledAgents(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"red-team": {"enabled": false}, "bookkeeper": {"enabled": false}})
	root := teamStaffedAnalysed(t, section, stfFull().leaving("red-team").leaving("bookkeeper"), false)
	stfThrough(t, root, model.GateDesignReview)

	refused := run(t, "gate", "design_review", "pass", "--note", "nobody has reviewed yet")
	if refused.code == 0 {
		t.Fatalf("the design review passed with no review:\n%s", refused.stdout)
	}
	saysDisabled(t, "a refused sdlc gate design_review pass", refused.stdout+refused.stderr, "red-team", "bookkeeper")

	stfWorked(t, root, model.GateDesignReview)
	stfReviewed(t, model.GateDesignReview, "red-team")
	mustRun(t, "gate", "design_review", "pass", "--note", "reviewed")
	failed := mustRun(t, "gate", "implementation", "fail", "--note", "it does not build")
	saysDisabled(t, "sdlc gate implementation fail", failed.stdout+failed.stderr, "red-team", "bookkeeper")
	asJSON := run(t, "gate", "implementation", "fail", "--note", "it still does not build", "--json")
	if !strings.Contains(asJSON.stdout, `"disabled"`) {
		t.Errorf("AC-4 gate --json on a fail has no disabled list:\n%s", asJSON.stdout)
	}
}

// AC-4: `sdlc staffing show`, `sdlc status`, `sdlc doctor` and the output of every gate
// name the agents that are disabled, workers included. A worker's name is held by the
// same assertion as a reviewer's, so a list of workers alone would not pass.
func TestAC4StaffingShowStatusDoctorAndEveryGateNameTheDisabledAgents(t *testing.T) {
	t.Parallel()
	disabled := []string{"red-team", "bookkeeper"}
	root := teamStaffedAnalysedPassingNothing(t, teamOf(agentEntries{"red-team": {"enabled": false}, "bookkeeper": {"enabled": false}}))

	saysDisabled(t, "sdlc staffing show", mustRun(t, "staffing", "show").stdout, disabled...)
	saysDisabled(t, "sdlc status", mustRun(t, "status").stdout, disabled...)
	got := teamShow(t)
	for _, name := range disabled {
		if !slices.Contains(got.Disabled, name) {
			t.Errorf("AC-4 staffing show --json: disabled = %q, has no %s", got.Disabled, name)
		}
		if !slices.Contains(teamShowDisabledInStatus(t), name) {
			t.Errorf("AC-4 status --json: disabled = %q, has no %s", teamShowDisabledInStatus(t), name)
		}
	}
	c := doctorConfiguration(t)
	if c.State != stateOK {
		t.Errorf("AC-4 doctor calls a person's setting a problem: %+v", c)
	}
	saysDisabled(t, "sdlc doctor --json", c.Detail, disabled...)
	if extra := doctorProblemsBesideTheSetUp(t); len(extra) != 0 {
		t.Errorf("AC-4 doctor makes a problem of a person's setting: %+v", extra)
	}
	saysDisabled(t, "sdlc doctor", run(t, "doctor").stdout, disabled...)

	r := run(t, "gate", "analysis", "pass", "--security-sensitive=false")
	if r.code != 0 {
		t.Fatalf("the analysis gate: %s", r.stderr)
	}
	saysDisabled(t, "sdlc gate analysis pass", r.stdout+r.stderr, disabled...)

	// Every gate after it, passing or failing, whichever gives the output.
	for _, gate := range model.Gates {
		if gate == model.GateDoR || gate == model.GateAnalysis {
			continue
		}
		stfWorked(t, root, gate)
		if gate == model.GateRetro {
			// A disabled bookkeeper: the person's document, and nothing waived.
			mustRunWith(t, "# Retro\n", "artifact", "write", "retro")
		}
		stfReviewed(t, gate, "red-team")
		out := mustRun(t, "gate", string(gate), "pass", "--note", "worked by the test")
		saysDisabled(t, "sdlc gate "+string(gate)+" pass", out.stdout+out.stderr, disabled...)
		if gate == model.GateTestsFrozen {
			var asJSON struct {
				Disabled []string `json:"disabled"`
			}
			if err := json.Unmarshal([]byte(mustRun(t, "gate", string(gate), "pass", "--json").stdout), &asJSON); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(asJSON.Disabled, "red-team") || !slices.Contains(asJSON.Disabled, "bookkeeper") {
				t.Errorf("AC-4 gate --json: disabled = %q", asJSON.Disabled)
			}
		}
	}
	failed := run(t, "gate", "retro", "fail", "--note", "again")
	saysDisabled(t, "sdlc gate retro fail", failed.stdout+failed.stderr, disabled...)
}

// teamStaffedAnalysedPassingNothing is a project with the team given, a staffing that
// leaves its disabled agents out, and the analysis's documents stored but its gate not
// recorded.
func teamStaffedAnalysedPassingNothing(t *testing.T, section map[string]any) string {
	t.Helper()
	root := teamAtAnalysis(t, section)
	stfSet(t, stfFull().leaving("red-team").leaving("bookkeeper"))
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	return root
}

// ANALYSIS.md section 6: `staffing show` with the project manager on and nothing recorded
// stays SDLC-E0068, and its reason says who is disabled too.
func TestAC4StaffingShowWithNothingRecordedStillNamesTheDisabledAgents(t *testing.T) {
	t.Parallel()
	root := teamAtAnalysis(t, teamOf(agentEntries{"red-team": {"enabled": false}}))
	stfRefused(t, stfUnchanged(t, root, "", "staffing", "show"), stfCodeNone, "red-team", "disabled")
}

// AC-4: nothing is printed where nothing is disabled. TestAC2WithEveryDefault... holds
// this byte for byte; this holds it with --json, so that an empty list is not said.
func TestAC4WithNothingDisabledNoOutputSaysSo(t *testing.T) {
	t.Parallel()
	teamAtAnalysis(t, map[string]any{})
	for _, args := range [][]string{{"status"}, {"status", "--json"}} {
		out := mustRun(t, args...).stdout
		if strings.Contains(out, "disabled") {
			t.Errorf("AC-4 sdlc %s speaks of disabled agents with none:\n%s", strings.Join(args, " "), out)
		}
	}
	stfSet(t, stfFull())
	if out := mustRun(t, "staffing", "show", "--json").stdout; strings.Contains(out, "disabled") || strings.Contains(out, "by_hand") {
		t.Errorf("AC-4 staffing show --json speaks of disabled agents with none:\n%s", out)
	}
}

// AC-4 and T10: `enabled: false` decides who runs. It beats a row that says needed, a
// path that would bring the reviewer back, and the floor; a row left out stays left out
// when the agent is enabled again.
func TestAC4NoGateWaitsForADisabledReviewerWhateverElseSaysItShould(t *testing.T) {
	t.Parallel()

	t.Run("AC_4 a row that says needed, from before the setting", func(t *testing.T) {
		t.Parallel()
		root := stfStaffed(t)
		stfThrough(t, root, model.GateDesignReview)
		setTeamAgentOnDisk(t, root, "human-advocate", map[string]any{"enabled": false})
		if at := atGateNames(teamShow(t)); slices.Contains(at, "human-advocate") {
			t.Errorf("AC-4 at_gate has the disabled human advocate: %q", at)
		}
		r := run(t, "gate", "design_review", "pass")
		if strings.Contains(r.stderr, "human-advocate has not") {
			t.Errorf("AC-4 design_review waits for the disabled human advocate:\n%s", r.stderr)
		}
		stfRefused(t, runWith(t, "# human-advocate\n", "review", "add", "design_review", "human-advocate", "block", "--important", "1", "--finding", "important the one finding"), "SDLC-E0027", "human-advocate")
	})

	t.Run("AC_4 a changed path that would bring it back", func(t *testing.T) {
		t.Parallel()
		section := teamOf(agentEntries{"security": {"enabled": false, "bring_back": []string{"internal/hook/"}}})
		root := teamStaffedAnalysed(t, section, stfFull().leaving("security"), false)
		stfThrough(t, root, model.GateImplementation, "security")
		writeFile(t, root, "internal/hook/extra.go", "// the story's change\n")
		stfThrough(t, root, model.GateCodeReview, "security")
		listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "code_review", "--json"))
		if got, ok := listed.review("code_review", "security"); ok && (got.BroughtBackBy != "" || !got.LeftOut) {
			t.Errorf("AC-4 a path brought back a disabled reviewer: %+v", got)
		}
		stfReviewed(t, model.GateCodeReview, "security")
		mustRun(t, "gate", "code_review", "pass", "--note", "approved")
	})

	t.Run("AC_10 the floor on a story recorded as sensitive", func(t *testing.T) {
		t.Parallel()
		section := teamOf(agentEntries{"security": {"enabled": false}})
		root := teamAtAnalysis(t, section)
		stfSet(t, stfFull().leaving("security"))
		mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
		mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
		out := mustRun(t, "gate", "analysis", "pass", "--security-sensitive")
		saysDisabled(t, "AC-10 the gate over a sensitive story", out.stdout+out.stderr, "security")
		stfThrough(t, root, model.GateDesignReview, "security")
		stfReviewed(t, model.GateDesignReview, "security")
		out = mustRun(t, "gate", "design_review", "pass", "--note", "approved")
		saysDisabled(t, "AC-10 the next gate", out.stdout+out.stderr, "security")
	})

	t.Run("AC_4 enabled again, a row left out stays left out", func(t *testing.T) {
		t.Parallel()
		section := teamOf(agentEntries{"human-advocate": {"enabled": false}})
		root := teamStaffedAnalysed(t, section, stfFull().leaving("human-advocate"), false)
		setConfigOnDisk(t, root, "team", map[string]any{})
		listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "design_review", "--json"))
		if got, _ := listed.review("design_review", "human-advocate"); !got.LeftOut {
			t.Errorf("AC-4 enabled again, the row is not read: %+v", got)
		}
	})

	// Two of a person's statements meet: `enabled: false` decides who runs, and the
	// assignment stays on the record, for when it is enabled again (ANALYSIS.md, section 6).
	t.Run("AC_4 an assignment on a disabled agent is kept and not acted on", func(t *testing.T) {
		t.Parallel()
		root := stfStaffed(t)
		stfThrough(t, root, model.GateDesignReview)
		mustRun(t, "staffing", "assign", "human-advocate", "--model", "opus", "--effort", "high", "--reason", "mine")
		setTeamAgentOnDisk(t, root, "human-advocate", map[string]any{"enabled": false})
		if at := atGateNames(teamShow(t)); slices.Contains(at, "human-advocate") {
			t.Errorf("AC-4 an assignment made a disabled agent one to delegate to: %q", at)
		}
		setConfigOnDisk(t, root, "team", stfSection())
		at, ok := stfShow(t).atGate("human-advocate")
		if !ok || at.AskedModel != "opus" {
			t.Errorf("AC-4 enabled again, the assignment is gone: %+v", at)
		}
	})
}

// ---------------------------------------------------------------------- AC-5

// AC-5: the agents that do a gate's work, each of them, and the gates it does.
func TestAC5ADisabledWorkersGateIsNotDelegatedAndIsThePersonsToDoByHand(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		agent string
		gates []model.Gate
	}{
		{"researcher", []model.Gate{model.GateAnalysis}},
		{"sdet", []model.Gate{model.GateTestsFrozen}},
		{"implementer", []model.Gate{model.GatePlan, model.GateImplementation}},
		{"verifier", []model.Gate{model.GateVerification, model.GateVerifierReview}},
		{"code-reviewer", []model.Gate{model.GateCodeReview}},
		{"bookkeeper", []model.Gate{model.GateRetro}},
	} {
		t.Run("AC_5 "+tc.agent, func(t *testing.T) {
			t.Parallel()
			teamAtAnalysis(t, teamOf(agentEntries{tc.agent: {"enabled": false}}))
			stfSet(t, stfFull().leaving(tc.agent))
			for _, gate := range tc.gates {
				got := teamShow(t, "--gate", string(gate))
				if slices.Contains(atGateNames(got), tc.agent) {
					t.Errorf("AC-5 at_gate at %s still delegates to %s: %q", gate, tc.agent, atGateNames(got))
				}
				if got.ByHand != tc.agent {
					t.Errorf("AC-5 by_hand at %s = %q, want %s: the runbook reads this", gate, got.ByHand, tc.agent)
				}
				out := mustRun(t, "staffing", "show", "--gate", string(gate)).stdout
				for _, want := range []string{"by hand", tc.agent, "disabled", "sdlc gate " + string(gate)} {
					if !strings.Contains(out, want) {
						t.Errorf("AC-5 staffing show --gate %s does not say %q:\n%s", gate, want, out)
					}
				}
			}
			// A gate that is another's is nobody's by hand.
			if other := teamShow(t, "--gate", string(otherGateThan(tc.gates))); other.ByHand != "" {
				t.Errorf("AC-5 by_hand at a gate %s does not work: %q", tc.agent, other.ByHand)
			}
		})
	}
	// Red team R10 (V21): with the project manager disabled as well there is no staffing to
	// read, and `staffing show` answers on another branch. Without by_hand there, the runbook
	// neither delegates the gate nor hands it to the person, and meets the Stop guard.
	t.Run("AC_5 with the project manager disabled too", func(t *testing.T) {
		t.Parallel()
		teamProject(t, teamOf(agentEntries{"project-manager": {"enabled": false}, "verifier": {"enabled": false}}))
		mustRun(t, "start")
		mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
		for _, gate := range []model.Gate{model.GateVerification, model.GateVerifierReview} {
			got := teamShow(t, "--gate", string(gate))
			if slices.Contains(atGateNames(got), "verifier") {
				t.Errorf("AC-5 at_gate at %s still delegates to the disabled verifier: %q", gate, atGateNames(got))
			}
			if got.ByHand != "verifier" {
				t.Errorf("AC-5 by_hand at %s = %q with the project manager disabled, want verifier", gate, got.ByHand)
			}
			out := mustRun(t, "staffing", "show", "--gate", string(gate)).stdout
			for _, want := range []string{"by hand", "verifier", "disabled", "sdlc gate " + string(gate)} {
				if !strings.Contains(out, want) {
					t.Errorf("AC-5 staffing show --gate %s with the project manager disabled does not say %q:\n%s", gate, want, out)
				}
			}
		}
	})
}

// Red team R8 (V19): a fixed by-hand line, printed wherever any worker is disabled, passes
// every row that looks for the words. The gates that are not the disabled worker's say nothing
// of the kind: here the bookkeeper's gate is the retro, and the story is at the tests.
func TestAC5NoGateButTheDisabledWorkersIsSaidToBeByHand(t *testing.T) {
	t.Parallel()
	teamAtAnalysis(t, teamOf(agentEntries{"bookkeeper": {"enabled": false}}))
	stfSet(t, stfFull().leaving("bookkeeper"))
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	out := mustRun(t, "gate", "analysis", "pass", "--security-sensitive=false")
	if strings.Contains(strings.ToLower(out.stdout+out.stderr), "by hand") {
		t.Errorf("AC-5 the analysis gate says a gate is by hand, and the next is the SDET's:\n%s%s", out.stdout, out.stderr)
	}
	if status := mustRun(t, "status").stdout; strings.Contains(strings.ToLower(status), "by hand") {
		t.Errorf("AC-5 status at tests_frozen says a gate is by hand:\n%s", status)
	}
	if got := teamShow(t); got.ByHand != "" {
		t.Errorf("AC-5 by_hand at tests_frozen = %q, and that gate is the SDET's", got.ByHand)
	}
	if got := teamShow(t, "--gate", "retro"); got.ByHand != "bookkeeper" {
		t.Errorf("AC-5 by_hand at the retro = %q, want the disabled bookkeeper", got.ByHand)
	}
}

func otherGateThan(gates []model.Gate) model.Gate {
	for _, g := range []model.Gate{model.GateAnalysis, model.GateRetro} {
		if !slices.Contains(gates, g) {
			return g
		}
	}
	return model.GateDesignReview
}

// AC-5: the gate before it says so, and status does at the gate it is.
func TestAC5TheGatesOutputAndStatusSayTheNextGateIsThePersons(t *testing.T) {
	t.Parallel()
	root := teamProject(t, teamOf(agentEntries{"researcher": {"enabled": false}}))
	mustRun(t, "start")
	out := mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	for _, want := range []string{"by hand", "researcher", "sdlc gate analysis"} {
		if !strings.Contains(out.stdout+out.stderr, want) {
			t.Errorf("AC-5 the gate before the researcher's does not say %q:\n%s%s", want, out.stdout, out.stderr)
		}
	}
	status := mustRun(t, "status").stdout
	for _, want := range []string{"by hand", "researcher", "sdlc gate analysis"} {
		if !strings.Contains(status, want) {
			t.Errorf("AC-5 status at the researcher's gate does not say %q:\n%s", want, status)
		}
	}
	_ = root
}

// Red team V5: model.Doers has no code reviewer and no verifier_review, so words built on
// it say nothing of a gate only a person can now do. Only the researcher's gate was read in
// the gate's output and in status, and the other six in `staffing show` alone. This is each of
// the other gates: the output of the gate before it, and status once that gate is next.
func TestAC5EveryWorkersGateIsSaidToBeThePersonsBeforeItAndAtIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		agent        string
		before, gate model.Gate
	}{
		{"sdet", model.GateAnalysis, model.GateTestsFrozen},
		{"implementer", model.GateTestsFrozen, model.GatePlan},
		{"implementer", model.GateDesignReview, model.GateImplementation},
		{"verifier", model.GateImplementation, model.GateVerification},
		{"verifier", model.GateVerification, model.GateVerifierReview},
		{"code-reviewer", model.GateVerifierReview, model.GateCodeReview},
		{"bookkeeper", model.GateCommit, model.GateRetro},
	} {
		t.Run("AC_5 "+tc.agent+" at "+string(tc.gate), func(t *testing.T) {
			t.Parallel()
			root := teamAtAnalysis(t, teamOf(agentEntries{tc.agent: {"enabled": false}}))
			stfSet(t, stfFull().leaving(tc.agent))
			mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
			mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
			var out result
			if tc.before == model.GateAnalysis {
				out = mustRun(t, "gate", "analysis", "pass", "--security-sensitive=false")
			} else {
				mustRun(t, "gate", "analysis", "pass", "--security-sensitive=false")
				stfThrough(t, root, tc.before, tc.agent)
				stfWorked(t, root, tc.before)
				stfReviewed(t, tc.before, tc.agent)
				out = mustRun(t, "gate", string(tc.before), "pass", "--note", "reached by the test")
			}
			want := []string{"by hand", tc.agent, "sdlc gate " + string(tc.gate)}
			for _, word := range want {
				if !strings.Contains(out.stdout+out.stderr, word) {
					t.Errorf("AC-5 the gate before %s does not say %q:\n%s%s", tc.gate, word, out.stdout, out.stderr)
				}
			}
			status := mustRun(t, "status").stdout
			for _, word := range want {
				if !strings.Contains(status, word) {
					t.Errorf("AC-5 status at %s does not say %q:\n%s", tc.gate, word, status)
				}
			}
		})
	}
}

// AC-5: nothing passes the gate on the agent's absence alone. Each gate still wants what
// it wants of the work, and once that is there, it passes with no refusal for the setting.
func TestAC5NothingPassesAGateOnADisabledWorkersAbsenceAndTheSettingRaisesNoRefusal(t *testing.T) {
	t.Parallel()

	t.Run("AC_5 the researcher's documents", func(t *testing.T) {
		t.Parallel()
		root := teamAtAnalysis(t, teamOf(agentEntries{"researcher": {"enabled": false}}))
		stfSet(t, stfFull().leaving("researcher"))
		stfRefused(t, stfUnchanged(t, root, "", "gate", "analysis", "pass"), "SDLC-E0020")
		stfAnalysed(t, false)
	})

	t.Run("AC_5 the verifier's review, which a person records", func(t *testing.T) {
		t.Parallel()
		section := teamOf(agentEntries{"verifier": {"enabled": false}})
		root := teamStaffedAnalysed(t, section, stfFull().leaving("verifier"), false)
		stfThrough(t, root, model.GateVerifierReview, "verifier")
		stfRefused(t, run(t, "gate", "verifier_review", "pass"), "SDLC-E0029", "verifier")
		mustRunWith(t, "# verifier, by hand\n", "review", "add", "verifier_review", "verifier", "approve")
		mustRun(t, "gate", "verifier_review", "pass", "--note", "a person read it")
	})

	t.Run("AC_5 the code reviewer's review, which a person records", func(t *testing.T) {
		t.Parallel()
		section := teamOf(agentEntries{"code-reviewer": {"enabled": false}})
		root := teamStaffedAnalysed(t, section, stfFull().leaving("code-reviewer"), false)
		stfThrough(t, root, model.GateCodeReview, "code-reviewer")
		stfRefused(t, run(t, "gate", "code_review", "pass"), "SDLC-E0029", "code-reviewer")
		// A person's block still stops it: disabling is who runs, not who may block.
		mustRunWith(t, "# code reviewer, by hand\n", "review", "add", "code_review", "code-reviewer", "block", "--important", "1", "--finding", "important the one finding", "--note", "no")
		stfRefused(t, run(t, "gate", "code_review", "pass"), "SDLC-E0030", "code-reviewer")
		// The gate's other reviewers still review, and are not what this row is about.
		stfReviewed(t, model.GateCodeReview, "code-reviewer")
		mustRunWith(t, "# code reviewer, by hand\n", "review", "add", "code_review", "code-reviewer", "approve")
		mustRun(t, "gate", "code_review", "pass", "--note", "a person read it")
	})

	t.Run("AC_5 the bookkeeper's retro", func(t *testing.T) {
		t.Parallel()
		section := teamOf(agentEntries{"bookkeeper": {"enabled": false}})
		root := teamStaffedAnalysed(t, section, stfFull().leaving("bookkeeper"), false)
		stfThrough(t, root, model.GateRetro)
		stfRefused(t, run(t, "gate", "retro", "pass"), "SDLC-E0020")
		mustRunWith(t, "# Retro\n", "artifact", "write", "retro")
		mustRun(t, "gate", "retro", "pass", "--note", "a person wrote it")
	})

	t.Run("AC_5 the sdet's frozen tests", func(t *testing.T) {
		t.Parallel()
		section := teamOf(agentEntries{"sdet": {"enabled": false}})
		root := teamStaffedAnalysed(t, section, stfFull().leaving("sdet"), false)
		if r := run(t, "gate", "tests_frozen", "pass"); r.code == 0 {
			t.Fatalf("AC-5 tests_frozen passed with no tests and no freeze:\n%s", r.stdout)
		}
		stfWorked(t, root, model.GateTestsFrozen)
		mustRun(t, "gate", "tests_frozen", "pass", "--note", "a person wrote them")
	})

	t.Run("AC_5 the implementer's plan", func(t *testing.T) {
		t.Parallel()
		section := teamOf(agentEntries{"implementer": {"enabled": false}})
		root := teamStaffedAnalysed(t, section, stfFull().leaving("implementer"), false)
		stfThrough(t, root, model.GatePlan)
		stfRefused(t, run(t, "gate", "plan", "pass"), "SDLC-E0020")
		mustRunWith(t, "# Plan\n", "artifact", "write", "plan")
		mustRun(t, "gate", "plan", "pass", "--note", "a person wrote it")
	})
}

// ---------------------------------------------------------------------- AC-6

func TestAC6CanBlockDecidesWhetherABlockStopsTheGate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		role    string
		gate    model.Gate
		entry   map[string]any
		stops   bool
		approve bool // whether anything short of an approval also holds the gate
	}{
		{"AC_6 the architect, advisory", "architect", model.GateDesignReview, map[string]any{"can_block": false}, false, false},
		{"AC_6 the architect, by default", "architect", model.GateDesignReview, map[string]any{}, true, true},
		{"AC_6 the verifier, advisory", "verifier", model.GateVerifierReview, map[string]any{"can_block": false}, false, false},
		{"AC_6 the code reviewer, advisory", "code-reviewer", model.GateCodeReview, map[string]any{"can_block": false}, false, false},
		{"AC_6 perf, advisory", "perf", model.GateCodeReview, map[string]any{"can_block": false}, false, false},
		{"AC_6 perf, by default", "perf", model.GateCodeReview, map[string]any{}, true, false},
		{"AC_6 the red team, blocking", "red-team", model.GateDesignReview, map[string]any{"can_block": true}, true, false},
		{"AC_6 the red team, by default", "red-team", model.GateDesignReview, map[string]any{}, false, false},
		{"AC_6 the human advocate, blocking", "human-advocate", model.GateCodeReview, map[string]any{"can_block": true}, true, false},
		{"AC_6 the human advocate, by default", "human-advocate", model.GateCodeReview, map[string]any{}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			section := teamOf(agentEntries{tc.role: tc.entry, "project-manager": {"enabled": false}})
			root := teamProject(t, section)
			mustRun(t, "start")
			reach(t, root, tc.gate)
			stfReviewed(t, tc.gate, tc.role)
			mustRunWith(t, "# "+tc.role+"\n", "review", "add", string(tc.gate), tc.role, "block", "--important", "1", "--finding", "important the one finding", "--note", "I object")

			r := run(t, "gate", string(tc.gate), "pass", "--note", "over the block")
			if passed := r.code == 0; passed == tc.stops {
				t.Fatalf("AC-6 %s's block with %v: passed = %v\n%s%s", tc.role, tc.entry, passed, r.stdout, r.stderr)
			}
			if tc.stops {
				stfRefused(t, r, "SDLC-E0030", tc.role)
				mustRunWith(t, "# "+tc.role+"\n", "review", "add", string(tc.gate), tc.role, "note")
				r = run(t, "gate", string(tc.gate), "pass", "--note", "the block is withdrawn")
				if passed := r.code == 0; passed == tc.approve {
					t.Errorf("AC-6 %s with a note after its block: passed = %v, and a note holds the gate = %v\n%s", tc.role, passed, tc.approve, r.stderr)
				}
			}
		})
	}
}

// AC-6: an advisory review is still required, never skipped (the glossary).
func TestAC6AnAdvisoryReviewIsStillRequired(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"architect": {"can_block": false}, "project-manager": {"enabled": false}})
	root := teamProject(t, section)
	mustRun(t, "start")
	reach(t, root, model.GateDesignReview)
	stfReviewed(t, model.GateDesignReview, "architect")
	stfRefused(t, run(t, "gate", "design_review", "pass"), "SDLC-E0029", "architect")
}

func TestAC6ReviewListShowsWhichReviewerCanBlock(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"architect": {"can_block": false}, "red-team": {"can_block": true}, "project-manager": {"enabled": false}})
	teamProject(t, section)
	mustRun(t, "start")
	listed := decode[reviewListPayload](t, mustRun(t, "review", "list", "--gate", "design_review", "--json"))
	power := map[string]reviewInfo{}
	for _, r := range listed.Reviews {
		power[r.Role] = r
	}
	if a := power["architect"]; a.Blocking || a.BlocksOnBreach {
		t.Errorf("AC-6 review list --json has the advisory architect as %+v", a)
	}
	if rt := power["red-team"]; !rt.Blocking && !rt.BlocksOnBreach {
		t.Errorf("AC-6 review list --json has the blocking red team as %+v", rt)
	}
	lines := strings.Split(mustRun(t, "review", "list", "--gate", "design_review").stdout, "\n")
	for role, advisory := range map[string]bool{"architect": true, "red-team": false, "security": false} {
		i := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, " "+role+" ") })
		if i < 0 {
			t.Errorf("AC-6 review list has no line for %s:\n%s", role, strings.Join(lines, "\n"))
			continue
		}
		if said := strings.Contains(lines[i], "advisory"); said != advisory {
			t.Errorf("AC-6 %s: the line says advisory = %v, want %v: %q", role, said, advisory, lines[i])
		}
	}
}

// AC-6: which the configuration allows, and every gate says so by name, sensitive or not.
func TestAC6EveryGatesOutputSaysSoWhereSecurityCannotBlock(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"security": {"can_block": false}, "project-manager": {"enabled": false}})
	root := teamProject(t, section)
	mustRun(t, "start")
	saysSecurity := func(where, out string) {
		t.Helper()
		if !slices.ContainsFunc(strings.Split(out, "\n"), func(l string) bool {
			return strings.Contains(l, "security") && strings.Contains(l, "can_block")
		}) {
			t.Errorf("AC-6 %s does not say by name that security cannot block:\n%s", where, out)
		}
	}
	for _, gate := range model.Gates {
		satisfy(t, root, gate)
		r := mustRun(t, "gate", string(gate), "pass", "--note", "worked by the test")
		saysSecurity("sdlc gate "+string(gate)+" pass", r.stdout+r.stderr)
	}
	// It is advisory, on a story that reads as sensitive: nothing has said otherwise.
	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--json"))
	if got, _ := listed.review("code_review", "security"); got.Blocking {
		t.Errorf("AC-6 security with can_block false is %+v", got)
	}
}

// Security note 3: a session between stories can blank a reviewer as it can blank a command,
// and the verifier and the code reviewer stand behind the commit. Their gate says so too, as
// security's says it at every gate: named in `review list` and doctor alone, nothing at the
// gate a person watches would show it.
func TestAC6TheVerifierAndTheCodeReviewerSayAtTheirGateThatTheyCannotBlock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		agent string
		gate  model.Gate
	}{
		{"verifier", model.GateVerifierReview},
		{"code-reviewer", model.GateCodeReview},
	} {
		t.Run("AC_6 "+tc.agent, func(t *testing.T) {
			t.Parallel()
			root := teamProject(t, teamOf(agentEntries{tc.agent: {"can_block": false}, "project-manager": {"enabled": false}}))
			mustRun(t, "start")
			reach(t, root, tc.gate)
			satisfy(t, root, tc.gate)
			r := mustRun(t, "gate", string(tc.gate), "pass", "--note", "worked by the test")
			if !slices.ContainsFunc(strings.Split(r.stdout+r.stderr, "\n"), func(l string) bool {
				return strings.Contains(l, tc.agent) && strings.Contains(l, "can_block")
			}) {
				t.Errorf("AC-6 the gate does not say by name that %s cannot block:\n%s%s", tc.agent, r.stdout, r.stderr)
			}
		})
	}
}

// ANALYSIS.md section 10: can_block true on security keeps what it has always meant. It
// blocks only on a story recorded as sensitive.
func TestAC6SecurityWithCanBlockTrueStillBlocksOnlyASensitiveStory(t *testing.T) {
	t.Parallel()
	for _, sensitive := range []bool{false, true} {
		t.Run("AC_6 sensitive "+strconv.FormatBool(sensitive), func(t *testing.T) {
			t.Parallel()
			section := teamOf(agentEntries{"security": {"can_block": true}, "project-manager": {"enabled": false}})
			root := teamProject(t, section)
			mustRun(t, "start")
			teamThrough(t, root, model.GateDesignReview, sensitive)
			stfReviewed(t, model.GateDesignReview, "security")
			mustRunWith(t, "# security\n", "review", "add", "design_review", "security", "block", "--important", "1", "--finding", "important the one finding", "--note", "I object")
			r := run(t, "gate", "design_review", "pass", "--note", "over the block")
			if stopped := r.code != 0; stopped != sensitive {
				t.Errorf("AC-6 security's block on a story sensitive = %v: stopped = %v\n%s", sensitive, stopped, r.stderr)
			}
		})
	}
}

// ---------------------------------------------------------------------- AC-7 (D-3)

// teamBlocks has a reviewer block n times at a gate, and returns what each said.
func teamBlocks(t *testing.T, gate model.Gate, role string, n int) []reviewPayload {
	t.Helper()
	out := make([]reviewPayload, 0, n)
	for i := range n {
		out = append(out, decode[reviewPayload](t, mustRunWith(t, "# "+role+"\n",
			"review", "add", string(gate), role, "block", "--important", "1", "--finding", "important the one finding", "--note", "objection "+strconv.Itoa(i+1), "--json")))
	}
	return out
}

func TestAC7MaxRoundsHandsTheStoryOverOnceTheAgentHasBlockedThatManyTimes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		entry     map[string]any
		loop      int
		handsOver int // the block that hands over; 0 is none in five
	}{
		{"AC_7 one", map[string]any{"max_rounds": 1}, 2, 1},
		{"AC_7 three, above the loop's two", map[string]any{"max_rounds": 3}, 2, 3},
		{"AC_7 one, below the loop's three", map[string]any{"max_rounds": 1}, 3, 1},
		{"AC_7 no setting takes the loop's", map[string]any{}, 3, 3},
		{"AC_7 the loop's 0 is off for an agent with no setting", map[string]any{}, 0, 0},
		{"AC_7 the agent's setting is read where the loop's is off", map[string]any{"max_rounds": 2}, 0, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			setConfigOnDisk(t, root, "team", teamOf(agentEntries{"architect": tc.entry, "project-manager": {"enabled": false}}))
			setConfigOnDisk(t, root, "loop", map[string]any{"max_review_rounds": tc.loop, "max_rework_rounds": 3, "max_stop_blocks": 3})
			commitEverything(t, root)
			mustRun(t, "start")
			reach(t, root, model.GateDesignReview)
			for i := 1; i <= 5; i++ {
				got := teamBlocks(t, model.GateDesignReview, "architect", 1)[0]
				if want := i == tc.handsOver; (got.HandedOver != "") != want {
					t.Fatalf("AC-7 block %d: handed over %q, want a hand-over = %v", i, got.HandedOver, want)
				}
				if got.HandedOver != "" {
					if got.HandedOver != "review_not_converging" {
						t.Errorf("AC-7 the kind of hand-over is %q, which the guide and a person's approval read", got.HandedOver)
					}
					return
				}
			}
		})
	}
}

func TestAC7TheHandOverSaysWhichAgentAndGateAndWhatThePersonCanDo(t *testing.T) {
	t.Parallel()
	root := teamProject(t, teamOf(agentEntries{"architect": {"max_rounds": 1}, "project-manager": {"enabled": false}}))
	mustRun(t, "start")
	reach(t, root, model.GateDesignReview)
	out := mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "block", "--important", "1", "--finding", "important the one finding", "--note", "AC-3 has no step").stdout
	for _, want := range []string{"architect", "design_review", "team.agents.architect.max_rounds", "sdlc approve US-001", "AC-3 has no step"} {
		if !strings.Contains(out, want) {
			t.Errorf("AC-7 the hand-over does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "loop.max_review_rounds") {
		t.Errorf("AC-7 the hand-over names the setting that did not apply:\n%s", out)
	}
	// And with no agent setting, it names the loop's, as it always did.
	root = teamProject(t, teamOf(agentEntries{"project-manager": {"enabled": false}}))
	mustRun(t, "start")
	reach(t, root, model.GateDesignReview)
	teamBlocks(t, model.GateDesignReview, "architect", 1)
	second := mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "block", "--important", "1", "--finding", "important the one finding").stdout
	if !strings.Contains(second, "loop.max_review_rounds") {
		t.Errorf("AC-7 the hand-over does not name the setting that applied:\n%s", second)
	}
}

// D-3: what counts is a block that stops the gate, since a person's last decision. The
// evidence that decided it: 46 of 51 stories reached two rounds by one reviewer at one
// gate, against 16 with two blocks. So an approval never escalates.
func TestAC7OnlyABlockThatStopsTheGateCounts(t *testing.T) {
	t.Parallel()

	t.Run("AC_7 approvals and notes", func(t *testing.T) {
		t.Parallel()
		root := teamProject(t, teamOf(agentEntries{"architect": {"max_rounds": 1}, "project-manager": {"enabled": false}}))
		mustRun(t, "start")
		reach(t, root, model.GateDesignReview)
		for range 4 {
			for _, verdict := range []string{"approve", "note"} {
				got := decode[reviewPayload](t, mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", verdict, "--json"))
				if got.HandedOver != "" {
					t.Fatalf("AC-7 %s handed the story over: %+v", verdict, got)
				}
			}
		}
	})

	for _, tc := range []struct {
		name, role string
		gate       model.Gate
		entry      map[string]any
	}{
		{"AC_7 an advisory agent, by default", "red-team", model.GateDesignReview, map[string]any{"max_rounds": 1}},
		{"AC_7 an advisory agent, by setting", "architect", model.GateDesignReview, map[string]any{"max_rounds": 1, "can_block": false}},
		{"AC_7 the human advocate", "human-advocate", model.GateDesignReview, map[string]any{"max_rounds": 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := teamProject(t, teamOf(agentEntries{tc.role: tc.entry, "project-manager": {"enabled": false}}))
			mustRun(t, "start")
			reach(t, root, tc.gate)
			for _, got := range teamBlocks(t, tc.gate, tc.role, 3) {
				if got.HandedOver != "" {
					t.Fatalf("AC-7 a block that cannot stop the gate handed the story over: %+v", got)
				}
			}
		})
	}

	// Each of these stops the gate, so it counts.
	for _, tc := range []struct {
		name, role string
		entry      map[string]any
	}{
		{"AC_7 perf, whose block stops the gate", "perf", map[string]any{"max_rounds": 1}},
		{"AC_7 the red team, once it can block", "red-team", map[string]any{"max_rounds": 1, "can_block": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := teamProject(t, teamOf(agentEntries{tc.role: tc.entry, "project-manager": {"enabled": false}}))
			mustRun(t, "start")
			reach(t, root, model.GateDesignReview)
			if got := teamBlocks(t, model.GateDesignReview, tc.role, 1)[0]; got.HandedOver == "" {
				t.Errorf("AC-7 %s's block stops the gate and did not count: %+v", tc.role, got)
			}
		})
	}

	// Security on a story not recorded as sensitive cannot stop the gate, so its blocks
	// are not counted there; on a sensitive story they are.
	for _, sensitive := range []bool{false, true} {
		t.Run("AC_7 security on a story sensitive "+strconv.FormatBool(sensitive), func(t *testing.T) {
			t.Parallel()
			root := teamProject(t, teamOf(agentEntries{"security": {"max_rounds": 1}, "project-manager": {"enabled": false}}))
			mustRun(t, "start")
			teamThrough(t, root, model.GateDesignReview, sensitive)
			if got := teamBlocks(t, model.GateDesignReview, "security", 1)[0]; (got.HandedOver != "") != sensitive {
				t.Errorf("AC-7 security's block with the story sensitive = %v: %+v", sensitive, got)
			}
		})
	}
}

// As both existing limits do: a person's decision starts the count again.
func TestAC7APersonsDecisionStartsTheCountAgainAndAnotherGateIsAnotherCount(t *testing.T) {
	t.Parallel()
	root := teamProject(t, teamOf(agentEntries{"security": {"max_rounds": 3}, "project-manager": {"enabled": false}}))
	mustRun(t, "start")
	teamThrough(t, root, model.GateDesignReview, true)
	for i, got := range teamBlocks(t, model.GateDesignReview, "security", 2) {
		if got.HandedOver != "" {
			t.Fatalf("AC-7 block %d of 3 handed the story over: %+v", i+1, got)
		}
	}
	// Another gate: its own count, or this block would be the third.
	if got := teamBlocks(t, model.GateCodeReview, "security", 1)[0]; got.HandedOver != "" {
		t.Errorf("AC-7 a block at another gate was counted with this one's: %+v", got)
	}
	if got := teamBlocks(t, model.GateDesignReview, "security", 1)[0]; got.HandedOver == "" {
		t.Fatal("AC-7 the third block did not hand the story over")
	}
	mustRun(t, "approve", "US-001")
	mustRun(t, "start")
	if got := teamBlocks(t, model.GateDesignReview, "security", 1)[0]; got.HandedOver != "" {
		t.Errorf("AC-7 a block after a person's decision was counted with the ones before it: %+v", got)
	}
}

// D-3: max_rounds does nothing on an agent that cannot stop a gate, and the same holds
// for one that does not review. Neither is refused, so doctor says so: a person
// believing in a limit that is not there is the failure.
func TestAC7DoctorSaysWhereMaxRoundsDoesNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, agent string
		entry       map[string]any
		nothing     bool
	}{
		{"AC_7 the red team by default", "red-team", map[string]any{"max_rounds": 1}, true},
		{"AC_7 the human advocate by default", "human-advocate", map[string]any{"max_rounds": 1}, true},
		{"AC_7 a blocking reviewer made advisory", "architect", map[string]any{"max_rounds": 1, "can_block": false}, true},
		{"AC_7 security made advisory", "security", map[string]any{"max_rounds": 1, "can_block": false}, true},
		{"AC_7 an agent that does not review", "researcher", map[string]any{"max_rounds": 1}, true},
		{"AC_7 the project manager", "project-manager", map[string]any{"max_rounds": 1}, true},
		{"AC_7 the red team once it can block", "red-team", map[string]any{"max_rounds": 1, "can_block": true}, false},
		{"AC_7 security, per story and so not doctor's to say", "security", map[string]any{"max_rounds": 1}, false},
		{"AC_7 perf, whose block stops the gate", "perf", map[string]any{"max_rounds": 1}, false},
		{"AC_7 the architect", "architect", map[string]any{"max_rounds": 1}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			teamProject(t, teamOf(agentEntries{tc.agent: tc.entry}))
			c := doctorConfiguration(t)
			if c.State != stateOK {
				t.Errorf("AC-7 doctor makes a problem of it: %+v", c)
			}
			if extra := doctorProblemsBesideTheSetUp(t); len(extra) != 0 {
				t.Errorf("AC-7 doctor makes a problem of it: %+v", extra)
			}
			said := slices.ContainsFunc(strings.Split(c.Detail, "\n"), func(l string) bool {
				return strings.Contains(l, tc.agent) && strings.Contains(l, "max_rounds") && strings.Contains(l, "nothing")
			})
			if said != tc.nothing {
				t.Errorf("AC-7 doctor says max_rounds does nothing on %s = %v, want %v:\n%s", tc.agent, said, tc.nothing, c.Detail)
			}
		})
	}
}

// ---------------------------------------------------------------------- AC-8

func TestAC8TheProjectManagerStaffsEachCopyWithAModelAndEffortOfItsOwn(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"security": {"instances": 3}})
	doc := stfFull().plus("security-2", "opus", "high").plus("security-3", "haiku", "low")

	t.Run("AC_8 each copy has its row, and at_gate lists it with what it asked for", func(t *testing.T) {
		t.Parallel()
		teamStaffedAnalysed(t, section, doc, true)
		got := stfShow(t, "--gate", "code_review")
		for name, want := range map[string]stfAtGate{
			"security":   {AskedModel: "sonnet", AskedEffort: "medium"},
			"security-2": {AskedModel: "opus", AskedEffort: "high"},
			"security-3": {AskedModel: "haiku", AskedEffort: "low"},
		} {
			at, ok := got.atGate(name)
			if !ok || at.AskedModel != want.AskedModel || at.AskedEffort != want.AskedEffort {
				t.Errorf("AC-8 at_gate has %s as %+v, want %+v: %+v", name, at, want, got.AtGate)
			}
			if a := got.agent(t, name); !a.Needed {
				t.Errorf("AC-8 %s is shown as %+v", name, a)
			}
		}
		// A copy is shown under its name, not folded into its role's line.
		prose := mustRun(t, "staffing", "show", "--gate", "code_review").stdout
		if !strings.Contains(prose, "security-2") || !strings.Contains(prose, "security-3") {
			t.Errorf("AC-8 staffing show does not name the copies:\n%s", prose)
		}
	})

	t.Run("AC_8 a staffing that misses a copy, or names one too many, is refused", func(t *testing.T) {
		t.Parallel()
		root := teamAtAnalysis(t, section)
		for _, tc := range []struct {
			doc  stfDoc
			says string
		}{
			{stfFull().plus("security-2", "opus", "high"), "security-3"},
			{stfFull().plus("security-3", "opus", "high"), "security-2"},
			{doc.plus("security-4", "opus", "high"), "security-4"},
			{doc.plus("researcher-2", "opus", "high"), "researcher-2"},
			{doc.plus("security-02", "opus", "high"), "security-02"},
			{doc.plus("perf-2", "opus", "high"), "perf-2"},
		} {
			r := stfUnchanged(t, root, tc.doc.text(t), "staffing", "set")
			stfRefused(t, r, stfCodeIncomplete, tc.says)
			if regexp.MustCompile(`(?i)\bcop(y|ies)\b`).MatchString(r.stderr) {
				t.Errorf("AC-8 the refusal says copy where the word is instance:\n%s", r.stderr)
			}
		}
		// A copy is held to what its role is: the lists, the lowest, the floor.
		lowest := teamOf(agentEntries{"security": {"instances": 2, "lowest": map[string]any{"model": "opus"}}})
		other := teamAtAnalysis(t, lowest)
		r := stfUnchanged(t, other, stfFull().with("security", func(a *stfAgent) { a.AskedModel = "opus" }).plus("security-2", "haiku", "low").text(t), "staffing", "set")
		stfRefused(t, r, stfCodeLimits, "security-2", "opus")
		stfSet(t, stfFull().with("security", func(a *stfAgent) { a.AskedModel = "opus" }).plus("security-2", "opus", "low"))
		r = stfUnchanged(t, other, "", "staffing", "amend", "security-2", "--model", "haiku", "--reason", "cheaper")
		stfRefused(t, r, stfCodeLimits, "security-2", "opus")
		mustRun(t, "staffing", "amend", "security-2", "--effort", "max", "--reason", "harder")
		// The verifier's copy is never left out.
		verifier := teamAtAnalysis(t, teamOf(agentEntries{"verifier": {"instances": 2}}))
		r = stfUnchanged(t, verifier, stfFull().plus("verifier-2", "opus", "high").leaving("verifier-2").text(t), "staffing", "set")
		stfRefused(t, r, stfCodeFloor, "verifier-2")
	})
}

// AC-8: the gate waits for all n, each under its own name.
func TestAC8TheGateWaitsForEveryCopyEachRecordingItsOwnReview(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"human-advocate": {"instances": 2}, "code-reviewer": {"instances": 2}})
	doc := stfFull().plus("human-advocate-2", "sonnet", "low").plus("code-reviewer-2", "opus", "high")
	root := teamStaffedAnalysed(t, section, doc, false)
	stfThrough(t, root, model.GateDesignReview)
	stfReviewed(t, model.GateDesignReview)
	r := run(t, "gate", "design_review", "pass")
	stfRefused(t, r, "SDLC-E0029", "human-advocate-2")

	// The name is what the person types in any case, and is stored in one spelling.
	rec := decode[reviewPayload](t, mustRunWith(t, "# copy\n", "review", "add", "design_review", "HUMAN-ADVOCATE-2", "note", "--json"))
	if rec.Role != "human-advocate-2" || rec.Round != 1 || !strings.Contains(rec.Path, "human-advocate-2") {
		t.Errorf("AC-8 a copy's review was recorded as %+v", rec)
	}
	mustRun(t, "gate", "design_review", "pass", "--note", "both copies reported")

	stfThrough(t, root, model.GateCodeReview)
	// A block from one copy stops the gate, whatever the other said: each is a reviewer.
	stfReviewed(t, model.GateCodeReview)
	mustRunWith(t, "# copy\n", "review", "add", "code_review", "human-advocate-2", "note")
	r = run(t, "gate", "code_review", "pass")
	stfRefused(t, r, "SDLC-E0029", "code-reviewer-2")
	mustRunWith(t, "# copy\n", "review", "add", "code_review", "code-reviewer-2", "block", "--important", "1", "--finding", "important the one finding", "--note", "the second reader objects")
	r = run(t, "gate", "code_review", "pass")
	stfRefused(t, r, "SDLC-E0030", "code-reviewer-2")
	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "code_review", "--json"))
	if base, _ := listed.review("code_review", "code-reviewer"); base.Verdict != "approve" {
		t.Errorf("AC-8 the copy's block was recorded on its role: %+v", base)
	}
	if copyOf, ok := listed.review("code_review", "code-reviewer-2"); !ok || copyOf.Verdict != "block" || !copyOf.Blocking {
		t.Errorf("AC-8 review list has no blocking row for the copy: %+v", listed.Reviews)
	}
	if prose := mustRun(t, "review", "list", "--gate", "code_review").stdout; !strings.Contains(prose, "code-reviewer-2") {
		t.Errorf("AC-8 review list does not name the copy:\n%s", prose)
	}
	mustRunWith(t, "# copy\n", "review", "add", "code_review", "code-reviewer-2", "approve")
	mustRun(t, "gate", "code_review", "pass", "--note", "both approve")

	// The commit gate asks Gates 6 and 7 again, the copies' reviews with them: a change to
	// the tree stales every review, and the one left stale is what the gate names.
	writeFile(t, root, "internal/after.go", "package internal\n")
	stfReviewed(t, model.GateVerifierReview)
	stfReviewed(t, model.GateCodeReview)
	mustRunWith(t, "# copy\n", "review", "add", "code_review", "human-advocate-2", "note")
	r = run(t, "gate", "commit", "pass", "--note", "committed")
	if r.code == 0 || !strings.Contains(r.stderr, "code-reviewer-2") {
		t.Errorf("AC-8 the commit gate does not hold the story to the copy's stale review: exit %d\n%s", r.code, r.stderr)
	}
	if strings.Contains(r.stderr, "human-advocate-2") {
		t.Errorf("AC-8 the commit gate holds the story to a copy that reviewed this tree:\n%s", r.stderr)
	}
}

// AC-8: the count is cli's to enforce, because the hook reads no configuration.
func TestAC8ANameNotAmongTheCopiesIsNotAReviewer(t *testing.T) {
	t.Parallel()
	root := teamStaffedAnalysed(t, teamOf(agentEntries{"human-advocate": {"instances": 2}}),
		stfFull().plus("human-advocate-2", "sonnet", "low"), false)
	stfThrough(t, root, model.GateDesignReview)
	for _, name := range []string{
		"human-advocate-3", "human-advocate-1", "human-advocate-02", "human-advocate--2", "human-advocate-2x",
		"human-advocate-٢", "architect-2", "researcher-2",
	} {
		r := stfUnchanged(t, root, "# x\n", "review", "add", "design_review", name, "note")
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0027") {
			t.Errorf("AC-8 review add %s: exit %d\n%s%s", name, r.code, r.stdout, r.stderr)
		}
	}
	// Instances lowered mid-story: a copy's review stands on the record, and the gate no
	// longer waits for it or holds to it.
	mustRunWith(t, "# copy\n", "review", "add", "design_review", "human-advocate-2", "block", "--important", "1", "--finding", "important the one finding")
	setTeamAgentOnDisk(t, root, "human-advocate", map[string]any{"instances": 1})
	stfReviewed(t, model.GateDesignReview)
	mustRun(t, "gate", "design_review", "pass", "--note", "one copy now")
}

// AC-1 with AC-8: can_block and max_rounds are the role's, and a copy has them too, with
// its own count, because each records under its own name.
func TestAC8ACopyTakesItsRolesSettingsAndCountsItsOwnBlocks(t *testing.T) {
	t.Parallel()
	t.Run("AC_8 advisory", func(t *testing.T) {
		t.Parallel()
		root := teamStaffedAnalysed(t, teamOf(agentEntries{"architect": {"instances": 2, "can_block": false}}),
			stfFull().plus("architect-2", "sonnet", "low"), false)
		stfThrough(t, root, model.GateDesignReview)
		listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "design_review", "--json"))
		for _, role := range []string{"architect", "architect-2"} {
			if got, ok := listed.review("design_review", role); !ok || got.Blocking {
				t.Errorf("AC-8 %s is %+v, with can_block false on the role", role, got)
			}
		}
	})
	t.Run("AC_7 its own count", func(t *testing.T) {
		t.Parallel()
		root := teamStaffedAnalysed(t, teamOf(agentEntries{"architect": {"instances": 2, "max_rounds": 2}}),
			stfFull().plus("architect-2", "sonnet", "low"), false)
		stfThrough(t, root, model.GateDesignReview)
		if got := teamBlocks(t, model.GateDesignReview, "architect", 1)[0]; got.HandedOver != "" {
			t.Fatalf("AC-7 the first block handed over: %+v", got)
		}
		if got := teamBlocks(t, model.GateDesignReview, "architect-2", 1)[0]; got.HandedOver != "" {
			t.Fatalf("AC-7 the copy's first block spent the role's budget: %+v", got)
		}
		got := decode[reviewPayload](t, mustRunWith(t, "# architect-2\n", "review", "add", "design_review", "architect-2", "block", "--important", "1", "--finding", "important the one finding", "--json"))
		if got.HandedOver == "" {
			t.Fatalf("AC-7 the copy's second block did not hand over: %+v", got)
		}
		if r := run(t, "start"); !strings.Contains(r.stderr, "architect-2") {
			t.Errorf("AC-7 the hand-over does not name the copy:\n%s", r.stderr)
		}
	})
	t.Run("AC_3 left out, it is refused as its role is", func(t *testing.T) {
		t.Parallel()
		root := teamStaffedAnalysed(t, teamOf(agentEntries{"red-team": {"instances": 2}}),
			stfFull().plus("red-team-2", "sonnet", "low").leaving("red-team-2"), false)
		stfThrough(t, root, model.GateDesignReview, "red-team-2")
		stfRefused(t, runWith(t, "# x\n", "review", "add", "design_review", "red-team-2", "block", "--important", "1", "--finding", "important the one finding"), "SDLC-E0027", "red-team-2")
	})
}

// Red team R1 (V13): decision 9 is that a session cannot amend a block away, and the loop
// that refused it compared the roster's roles by exact name, so an instance's block matched
// nothing. The block is under the instance's own name, so a reading of the base role's
// latest review would miss it too.
func TestAC8AnAmendmentCannotLeaveOutAnInstanceWhoseLatestReviewIsABlock(t *testing.T) {
	t.Parallel()
	root := teamStaffedAnalysed(t, teamOf(agentEntries{"architect": {"instances": 2}}),
		stfFull().plus("architect-2", "sonnet", "low"), false)
	stfThrough(t, root, model.GateDesignReview)
	mustRunWith(t, "# x\n", "review", "add", "design_review", "architect-2", "block", "--important", "1", "--finding", "important the one finding", "--note", "no")
	stfRefused(t, stfUnchanged(t, root, "", "staffing", "amend", "architect-2", "--left-out", "--reason", "x"), stfCodeBlocked, "architect-2")
	// The base role has not blocked, so it is the project manager's to leave out.
	mustRun(t, "staffing", "amend", "architect", "--left-out", "--reason", "one reader is enough")
	// Rework is the way on, as for the role.
	mustRunWith(t, "# x\n", "review", "add", "design_review", "architect-2", "approve")
	mustRun(t, "staffing", "amend", "architect-2", "--left-out", "--reason", "nothing left for it to read")
}

// Red team R2 (C9): init writes `instances` on all twelve agents, so a person may raise it on
// a worker. Read as seats there, the staffing would need a row InstanceOf refuses, and the
// analysis gate could never pass. It has no effect (ANALYSIS.md section 10).
func TestAC8InstancesOnAnAgentThatDoesNotReviewHasNoEffect(t *testing.T) {
	t.Parallel()
	root := teamAtAnalysis(t, teamOf(agentEntries{"researcher": {"instances": 2}, "project-manager": {"instances": 2}}))
	stfSet(t, stfFull())
	at := atGateNames(teamShow(t))
	if !slices.Contains(at, "researcher") || slices.Contains(at, "researcher-2") || slices.Contains(at, "project-manager-2") {
		t.Errorf("AC-8 at_gate at the analysis is %q, want the researcher alone and no instance of a worker", at)
	}
	stfAnalysed(t, false)
	stfRefused(t, stfUnchanged(t, root, "", "team", "failure", "researcher-2", "--reason", "it returned nothing"), stfCodeIncomplete, "researcher-2")
}

// Red team R3 (V14): bring_back is the role's setting, and every instance of the role comes
// back on a listed path, each under its own name. A reading keyed by the base role brings
// perf back and leaves perf-2 out, which fails towards fewer reviewers.
func TestAC7AC8APathBringsBackEveryInstanceOfItsRole(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"perf": {"instances": 2, "bring_back": []string{"internal/hot/"}}})
	root := teamStaffedAnalysed(t, section, stfFull("perf").plus("perf-2", "sonnet", "low").leaving("perf-2"), false)
	stfThrough(t, root, model.GateImplementation, "perf")
	writeFile(t, root, "internal/hot/loop.go", "// the story's change\n")
	stfThrough(t, root, model.GateCodeReview, "perf")
	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "code_review", "--json"))
	for _, name := range []string{"perf", "perf-2"} {
		if got, ok := listed.review("code_review", name); !ok || got.LeftOut || !strings.HasPrefix(got.BroughtBackBy, "internal/hot/") {
			t.Errorf("AC-7 %s was not brought back by internal/hot/loop.go: %+v", name, got)
		}
	}
	stfReviewed(t, model.GateCodeReview, "perf")
	mustRunWith(t, "# perf\n", "review", "add", "code_review", "perf", "approve")
	stfRefused(t, run(t, "gate", "code_review", "pass"), "SDLC-E0029", "perf-2")
	mustRunWith(t, "# perf-2\n", "review", "add", "code_review", "perf-2", "approve")
	mustRun(t, "gate", "code_review", "pass", "--note", "both instances read the hot path")
}

// Red team R11 (V22, C10): a person assigns to an instance as to its role, within instances
// and not past it. And instances raised after the staffing: the new instance has no row, so
// it is not left out and the gate waits for it, and the project manager can give it one.
func TestAC8APersonAssignsAnInstanceAndOneRaisedMidStoryIsWaitedForAndStaffed(t *testing.T) {
	t.Parallel()
	t.Run("AC_8 assign", func(t *testing.T) {
		t.Parallel()
		root := teamStaffedAnalysed(t, teamOf(agentEntries{"security": {"instances": 2}}),
			stfFull().plus("security-2", "sonnet", "low"), false)
		mustRun(t, "staffing", "assign", "security-2", "--model", "opus", "--effort", "high", "--reason", "mine")
		if a := stfShow(t).agent(t, "security-2"); a.AskedModel != "opus" || a.AskedEffort != "high" {
			t.Errorf("AC-8 the person's assignment to security-2 is not what the staffing asks for: %+v", a)
		}
		stfRefused(t, stfUnchanged(t, root, "", "staffing", "assign", "security-3", "--model", "opus", "--effort", "high", "--reason", "mine"),
			stfCodeIncomplete, "security-3")
	})
	t.Run("AC_8 raised mid-story", func(t *testing.T) {
		t.Parallel()
		root := teamStaffedAnalysed(t, map[string]any{}, stfFull(), false)
		stfThrough(t, root, model.GateDesignReview)
		setTeamAgentOnDisk(t, root, "perf", map[string]any{"instances": 2})
		stfReviewed(t, model.GateDesignReview)
		stfRefused(t, run(t, "gate", "design_review", "pass"), "SDLC-E0029", "perf-2")
		mustRun(t, "staffing", "amend", "perf-2", "--needed", "--model", "sonnet", "--effort", "low", "--reason", "a second reader")
		if at, ok := stfShow(t).atGate("perf-2"); !ok || at.AskedModel != "sonnet" {
			t.Errorf("AC-8 the amendment did not staff perf-2: %+v", at)
		}
		mustRunWith(t, "# perf-2\n", "review", "add", "design_review", "perf-2", "approve")
		mustRun(t, "gate", "design_review", "pass", "--note", "both instances read it")
	})
}

// Red team R12 (H6): a model name is printed as it stands, so one holding a newline made a
// line of the tool's own in `staffing show`, which the project manager and the session read.
// It is refused, or shown so that it makes no line.
func TestAC13AModelNameCannotMakeALineOfTheToolsOwn(t *testing.T) {
	t.Parallel()
	root := teamProject(t, map[string]any{"models": []string{"haiku", "sonnet", "opus\n  next   retro"}})
	if r := run(t, "start"); r.code != 0 {
		stfRefused(t, r, teamCodeBad, "team.models")
		return
	}
	mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	stfSet(t, stfFull())
	out := mustRun(t, "staffing", "show").stdout
	if !strings.Contains(out, "sonnet") {
		t.Errorf("AC-11 staffing show does not list the project's models:\n%s", out)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.HasPrefix(line, "  next") {
			t.Errorf("H6 a model name made a line of the tool's own in staffing show: %q\n%s", line, out)
		}
	}
	_ = root
}

// With the project manager off there is no row to ask, and the gate still waits for all n.
func TestAC8WithTheProjectManagerDisabledEveryCopyStillTakesPart(t *testing.T) {
	t.Parallel()
	section := teamOf(agentEntries{"project-manager": {"enabled": false}, "human-advocate": {"instances": 2}})
	root := teamProject(t, section)
	mustRun(t, "start")
	reach(t, root, model.GateDesignReview)
	if at := atGateNames(teamShow(t)); !slices.Contains(at, "human-advocate") || !slices.Contains(at, "human-advocate-2") {
		t.Errorf("AC-8 at_gate is %q", at)
	}
	stfReviewed(t, model.GateDesignReview)
	stfRefused(t, run(t, "gate", "design_review", "pass"), "SDLC-E0029", "human-advocate-2")
}

// `instances` on an agent that is not a reviewer has no effect (ANALYSIS.md section 10),
// is not refused, and doctor reports it, as AC-12 asks of the instances above 1.
func TestAC12DoctorReportsTheTeamOtherwise(t *testing.T) {
	t.Parallel()
	teamProject(t, teamOf(agentEntries{
		"red-team":   {"enabled": false},
		"verifier":   {"instances": 3},
		"researcher": {"instances": 2},
		"architect":  {"can_block": false},
		"perf":       {"can_block": true},
		"sdet":       {"can_block": true},
		"bookkeeper": {"can_block": false},
	}))
	c := doctorConfiguration(t)
	if c.State != stateOK {
		t.Fatalf("AC-12 doctor makes a problem of a person's settings: %+v", c)
	}
	for _, want := range [][]string{
		{"red-team", "disabled"},
		{"verifier", "instances", "3"},
		{"researcher", "instances", "2"},
		{"architect", "can_block"},
		// Red team V12: can_block on an agent that reviews nothing is a setting a person
		// believes in that is not there.
		{"sdet", "can_block", "nothing"},
		{"bookkeeper", "can_block", "nothing"},
	} {
		if !slices.ContainsFunc(strings.Split(c.Detail, "\n"), func(l string) bool {
			for _, w := range want {
				if !strings.Contains(l, w) {
					return false
				}
			}
			return true
		}) {
			t.Errorf("AC-12 doctor has no line naming %q:\n%s", want, c.Detail)
		}
	}
	// What is the default is not reported: perf's can_block is true by default.
	for line := range strings.SplitSeq(c.Detail, "\n") {
		if strings.Contains(line, "perf") && strings.Contains(line, "can_block") {
			t.Errorf("AC-12 doctor reports a can_block that does not differ from the default: %s", line)
		}
	}
}

// Human advocate 1: the released reason sends a person to dor_advocate_check, which does
// nothing here. E0027 now covers three cases the story adds, and each asks something else of
// a person: edit the file, ask the project manager again, or use a name that has a seat. The
// reason says which, and the sentence about dor_advocate_check stays on its own branch.
func TestAC3AReviewTheGateDoesNotWaitForSaysWhyInWordsAPersonCanActOn(t *testing.T) {
	t.Parallel()
	word := regexp.MustCompile(`(?i)\bcop(y|ies)\b`)
	for _, tc := range []struct {
		name, who string
		section   map[string]any
		doc       stfDoc
		says      []string
		// why and notWhy are held outside the fix line: red team R5 (V16) found two of the
		// three cases passed on a fix line that named every case, under one reason for all.
		why, notWhy []string
	}{
		{"AC_4 disabled", "human-advocate", teamOf(agentEntries{"human-advocate": {"enabled": false}}),
			stfFull().leaving("human-advocate"), []string{"disabled", "team.agents.human-advocate.enabled"},
			[]string{"disabled"}, []string{"instances"}},
		{"AC_3 not picked", "human-advocate", map[string]any{},
			stfFull().leaving("human-advocate"), []string{"staffing"},
			[]string{"staffing"}, []string{"disabled", "instances"}},
		{"AC_8 past instances", "human-advocate-3", teamOf(agentEntries{"human-advocate": {"instances": 2}}),
			stfFull().plus("human-advocate-2", "sonnet", "low"), []string{"instances"},
			[]string{"instances"}, []string{"disabled"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := teamStaffedAnalysed(t, tc.section, tc.doc, false)
			stfThrough(t, root, model.GateDesignReview, "human-advocate")
			r := stfUnchanged(t, root, "# x\n", "review", "add", "design_review", tc.who, "note")
			stfRefused(t, r, "SDLC-E0027", tc.says...)
			why := strings.ToLower(teamWhy(r.stderr))
			for _, want := range tc.why {
				if !strings.Contains(why, want) {
					t.Errorf("AC-3 past its fix line, the refusal does not say %q, which is this case:\n%s", want, r.stderr)
				}
			}
			for _, wrong := range tc.notWhy {
				if strings.Contains(why, wrong) {
					t.Errorf("AC-3 past its fix line, the refusal says %q, which is another case:\n%s", wrong, r.stderr)
				}
			}
			if strings.Contains(r.stderr, "dor_advocate_check") {
				t.Errorf("AC-3 the refusal sends a person to a setting that does nothing here:\n%s", r.stderr)
			}
			if word.MatchString(r.stderr) {
				t.Errorf("AC-8 the refusal says copy where the word is instance:\n%s", r.stderr)
			}
		})
	}
	t.Run("AC_3 the one case dor_advocate_check is for", func(t *testing.T) {
		t.Parallel()
		root := teamProject(t, map[string]any{})
		mustRun(t, "start")
		stfRefused(t, stfUnchanged(t, root, "# x\n", "review", "add", "dor", "human-advocate", "note"), "SDLC-E0027", "dor_advocate_check")
	})
}

// T1, in cli: the names cli accepts lie inside the names the shell rule recognises. The
// shell rule reads no configuration, so it has to recognise whatever `instances` says.
// FindReviewer is the one function both ask; this holds it from the outside, with every
// spelling THREATS.md T1 and ANALYSIS.md section 7 name, as the words a shell would hand
// sdlc after reading them. A name the hook passes and cli accepts, from another agent, is
// the bug.
func TestAC8EveryNameCliAcceptsIsOneTheShellRuleKeepsToItsReviewer(t *testing.T) {
	t.Parallel()
	root := teamStaffedAnalysed(t, teamOf(agentEntries{"code-reviewer": {"instances": 9}}), func() stfDoc {
		doc := stfFull()
		for i := 2; i <= 9; i++ {
			doc = doc.plus("code-reviewer-"+strconv.Itoa(i), "sonnet", "low")
		}
		return doc
	}(), false)
	stfThrough(t, root, model.GateCodeReview)

	accepted := 0
	for _, name := range []string{
		"code-reviewer", "code-reviewer-2", "code-reviewer-3", "code-reviewer-9", "CODE-REVIEWER-2", " code-reviewer-2", "code-reviewer-2 ",
		"code-reviewer-02", "code-reviewer-1", "code-reviewer--2", "code-reviewer-2x", "code-reviewer-٢",
	} {
		r := runWith(t, "# review\n", "review", "add", "code_review", name, "approve")
		if r.code != 0 {
			continue
		}
		accepted++
		command := "sdlc review add code_review " + strconv.Quote(name) + " approve"
		for _, agent := range []string{"implementer", "sdet", "researcher", "security", "red-team", ""} {
			f, ok := shellpolicy.Inspect(command, shellpolicy.State{Agent: agent})
			if !ok || f.Rule != "review-is-recorded-by-its-reviewer" {
				t.Errorf("T1 cli accepted %q, and the hook lets %q run it: %+v, %v", name, agent, f, ok)
			}
		}
	}
	if accepted < 6 {
		t.Errorf("T1 cli accepted %d of the names, want the role, its copies and their spellings", accepted)
	}
}

// ---------------------------------------------------------------------- AC-9

// teamFailure is one `sdlc team failure`, with --json.
type teamFailure struct {
	OK         bool   `json:"ok"`
	Story      string `json:"story"`
	Agent      string `json:"agent"`
	Gate       string `json:"gate"`
	Failures   int    `json:"failures"`
	HandedOver string `json:"handed_over"`
}

func teamFails(t *testing.T, agent, reason string) teamFailure {
	t.Helper()
	return decode[teamFailure](t, mustRun(t, "team", "failure", agent, "--reason", reason, "--json"))
}

// `retries: 2` is three runs: the third failure escalates. `retries: 0` escalates on the
// first, and handOverAt reads a limit of 0 as off: a direct reuse would turn the strictest
// setting into no limit at all (T6).
func TestAC9AFailedRunIsRetriedUpToTheSettingThenHandedToAPerson(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		section   map[string]any
		handsOver int
	}{
		{"AC_9 the default is two retries", map[string]any{}, 3},
		{"AC_9 no team section", nil, 3},
		{"AC_9 none", map[string]any{"retries": 0}, 1},
		{"AC_9 one", map[string]any{"retries": 1}, 2},
		{"AC_9 four", map[string]any{"retries": 4}, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			if tc.section == nil {
				teamOutOfTheFile(t, root)
			} else {
				setConfigOnDisk(t, root, "team", tc.section)
			}
			commitEverything(t, root)
			mustRun(t, "start")
			mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
			for i := 1; i <= tc.handsOver; i++ {
				got := teamFails(t, "researcher", "run "+strconv.Itoa(i)+" returned nothing usable")
				if !got.OK || got.Story != "US-001" || got.Agent != "researcher" || got.Gate != "analysis" || got.Failures != i {
					t.Errorf("AC-9 failure %d: %+v", i, got)
				}
				if (got.HandedOver != "") != (i == tc.handsOver) {
					t.Fatalf("AC-9 failure %d of %d: handed over %q", i, tc.handsOver, got.HandedOver)
				}
			}
			if got := decode[activePayload](t, mustRun(t, "status", "--json")); got.Active != nil {
				t.Errorf("AC-9 the iteration did not end at the hand-over: active %v", *got.Active)
			}
		})
	}
}

// AC-9: the agent, the gate and the failures named. A model wrote the reasons, so they are
// quoted, cannot end a line, and carry no control character (T11).
func TestAC9TheHandOverNamesTheAgentTheGateAndTheFailures(t *testing.T) {
	t.Parallel()
	root := teamAtAnalysis(t, map[string]any{"retries": 1})
	forged := "  next   retro"
	mustRun(t, "team", "failure", "researcher", "--reason", "it timed out")
	out := mustRun(t, "team", "failure", "researcher", "--reason", "came back empty\n"+forged+"\x1b[2J\r").stdout
	for _, want := range []string{"researcher", "analysis", "it timed out", "came back empty", "sdlc approve US-001", "handed to a person"} {
		if !strings.Contains(out, want) {
			t.Errorf("AC-9 the hand-over does not say %q:\n%s", want, out)
		}
	}
	r := run(t, "start")
	if !strings.Contains(r.stderr, "SDLC-E0036") || !strings.Contains(r.stderr, "researcher") || !strings.Contains(r.stderr, "analysis") {
		t.Errorf("AC-9 the story carried on past the hand-over, or it does not say why:\n%s%s", r.stdout, r.stderr)
	}
	for _, args := range [][]string{{"status"}, {"log"}} {
		got := mustRun(t, args...).stdout
		if strings.ContainsAny(got, "\x1b\r") {
			t.Errorf("T11 sdlc %s printed a control character from a reason: %q", strings.Join(args, " "), got)
		}
		for line := range strings.SplitSeq(got, "\n") {
			if line == forged {
				t.Errorf("T11 sdlc %s printed a line a reason made up as one of its own:\n%s", strings.Join(args, " "), got)
			}
		}
	}
	if strings.ContainsAny(out, "\x1b\r") {
		t.Errorf("T11 the hand-over printed a control character: %q", out)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if line == forged {
			t.Errorf("T11 the hand-over printed a line a reason made up as one of its own:\n%s", out)
		}
	}
	if raw := stfRecord(t, root); !strings.Contains(raw, "it timed out") {
		t.Errorf("AC-9 the reason is not on the record:\n%s", raw)
	}
}

// ANALYSIS.md section 8, settled: per agent name per gate, since a person's last
// decision. A count per story would have a failure at Gate 2 spend a reviewer's retries
// at Gate 7. Different reasons count alike: SinceDecision matches the whole message, so
// the model-written reason cannot be in the message that is counted.
func TestAC9TheCountIsPerAgentPerGateSinceAPersonsLastDecision(t *testing.T) {
	t.Parallel()
	section := map[string]any{"retries": 1, "agents": map[string]any{"human-advocate": map[string]any{"instances": 2}}}
	root := teamStaffedAnalysed(t, section, stfFull().plus("human-advocate-2", "sonnet", "low"), false)

	// Another agent: another count.
	if got := teamFails(t, "sdet", "one"); got.HandedOver != "" || got.Gate != "tests_frozen" {
		t.Fatalf("AC-9 %+v", got)
	}
	if got := teamFails(t, "project-manager", "one"); got.HandedOver != "" || got.Failures != 1 {
		t.Fatalf("AC-9 another agent's failure was counted with the first: %+v", got)
	}
	// Another gate: another count, for the same agent.
	stfThrough(t, root, model.GateDesignReview)
	if got := teamFails(t, "sdet", "two"); got.HandedOver != "" || got.Failures != 1 || got.Gate != "design_review" {
		t.Fatalf("AC-9 a failure at another gate was counted with the first: %+v", got)
	}
	// A copy counts under its own name.
	if got := teamFails(t, "human-advocate", "one"); got.HandedOver != "" {
		t.Fatalf("AC-9 %+v", got)
	}
	if got := teamFails(t, "human-advocate-2", "one"); got.HandedOver != "" || got.Agent != "human-advocate-2" || got.Failures != 1 {
		t.Fatalf("AC-9 a copy's failure was counted with its role's: %+v", got)
	}
	// A different reason is the same run failing again.
	got := teamFails(t, "human-advocate", "something else altogether")
	if got.HandedOver == "" || got.Failures != 2 {
		t.Fatalf("AC-9 a second failure with another reason was not counted with the first: %+v", got)
	}
	// A person's decision starts the count again, and it is the same story from there.
	mustRun(t, "approve", "US-001")
	mustRun(t, "start")
	if got := teamFails(t, "human-advocate", "after the person said carry on"); got.HandedOver != "" || got.Failures != 1 {
		t.Errorf("AC-9 a failure after a person's decision was counted with the ones before it: %+v", got)
	}
}

// T6: a false report gets nothing but a person's attention. Failures change the count and
// the hand-over and no gate, review, staffing row, or freeze, and nobody takes the agent's
// place: the gate still wants what it wanted.
func TestAC9RecordingFailuresChangesNothingButTheCountAndTheHandOver(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "perf", "red-team")
	stfThrough(t, root, model.GateDesignReview, "perf", "red-team")

	snapshot := func() map[string]json.RawMessage {
		t.Helper()
		var doc map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stfRecord(t, root)), &doc); err != nil {
			t.Fatal(err)
		}
		delete(doc, "events")
		delete(doc, "updated")
		delete(doc, "escalations")
		return doc
	}
	freeze := func() string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "state", "tests.lock"))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	beforeRecord, beforeFreeze := snapshot(), freeze()
	beforeStaffing := mustRun(t, "staffing", "show", "--json").stdout
	head := decode[statusPayload](t, mustRun(t, "status", "--json")).NextGate

	teamFails(t, "architect", "first run")
	teamFails(t, "architect", "second run")
	// Below the limit, the gate wants what it wanted, and no substitute appears.
	r := run(t, "gate", "design_review", "pass")
	stfRefused(t, r, "SDLC-E0029", "architect")
	if got := decode[statusPayload](t, mustRun(t, "status", "--json")).NextGate; got != head {
		t.Errorf("T6 a failure moved the next gate from %s to %s", head, got)
	}
	teamFails(t, "architect", "third run")
	mustRun(t, "approve", "US-001")
	mustRun(t, "start")

	afterRecord, afterFreeze := snapshot(), freeze()
	for key, was := range beforeRecord {
		if key == "approvals" {
			continue
		}
		if string(afterRecord[key]) != string(was) {
			t.Errorf("T6 a failure changed the record's %s:\nbefore %s\nafter  %s", key, was, afterRecord[key])
		}
	}
	if afterFreeze != beforeFreeze {
		t.Error("T6 a failure changed the freeze")
	}
	if after := mustRun(t, "staffing", "show", "--json").stdout; after != beforeStaffing {
		t.Errorf("T6 a failure changed the staffing:\n%s\nwas:\n%s", after, beforeStaffing)
	}
	// Nothing passes on its absence, after a person's approval either.
	stfRefused(t, run(t, "gate", "design_review", "pass"), "SDLC-E0029", "architect")
}

// AC-9's last sentence: the project manager's own run. Recorded by the session, not by the
// project manager, whose list refuses it (the shell rule's half is in internal/shellpolicy).
// With no staffing, the analysis cannot pass after the hand-over either.
func TestAC9HoldsForTheProjectManagersOwnRun(t *testing.T) {
	t.Parallel()
	root := teamAtAnalysis(t, map[string]any{"retries": 0})
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	got := teamFails(t, "project-manager", "it returned no staffing")
	if got.HandedOver == "" {
		t.Fatalf("AC-9 retries 0: %+v", got)
	}
	mustRun(t, "approve", "US-001")
	mustRun(t, "start")
	stfRefused(t, run(t, "gate", "analysis", "pass"), stfCodeNone)
	_ = root
}

func TestAC9TheCommandIsRefusedWhatItCannotCount(t *testing.T) {
	t.Parallel()
	root := teamAtAnalysis(t, map[string]any{"agents": map[string]any{"security": map[string]any{"instances": 2}}})
	long := strings.Repeat("a", 1001)
	for name, args := range map[string][]string{
		"AC_9 an agent nobody ships":   {"team", "failure", "designer", "--reason", "x"},
		"AC_9 a copy beyond instances": {"team", "failure", "security-3", "--reason", "x"},
		"AC_9 a copy of a non-worker":  {"team", "failure", "researcher-2", "--reason", "x"},
		"AC_9 a misspelt copy":         {"team", "failure", "security-02", "--reason", "x"},
		"AC_9 a path":                  {"team", "failure", "../../etc", "--reason", "x"},
		"AC_9 no agent":                {"team", "failure", "--reason", "x"},
		"AC_9 no reason":               {"team", "failure", "researcher"},
		"AC_9 a blank reason":          {"team", "failure", "researcher", "--reason", " \t"},
		"AC_9 a reason over the limit": {"team", "failure", "researcher", "--reason", long},
	} {
		t.Run(name, func(t *testing.T) {
			r := stfUnchanged(t, root, "", args...)
			if !strings.Contains(r.stderr, "SDLC-E") || stfFixLine(r.stderr) == "" {
				t.Errorf("the refusal carries no code or no fix:\n%s", r.stderr)
			}
		})
	}
	stfRefused(t, stfUnchanged(t, root, "", "team", "failure", "researcher", "--reason", long), "SDLC-E0026")
	mustRun(t, "team", "failure", "researcher", "--reason", long[:1000])
	if got := teamFails(t, "security-2", "a copy"); got.Agent != "security-2" {
		t.Errorf("AC-9 a copy's own name was not taken: %+v", got)
	}
}

func TestAC9TheCommandNeedsAStoryAndTheSessionHoldingIt(t *testing.T) {
	t.Parallel()
	root := teamProject(t, map[string]any{})
	r := run(t, "team", "failure", "researcher", "--reason", "x")
	stfRefused(t, r, "SDLC-E0011")

	setenv(t, store.SessionEnv, "session-holding-the-story")
	mustRun(t, "start")
	setenv(t, store.SessionEnv, "another-session")
	r = stfUnchanged(t, root, "", "team", "failure", "researcher", "--reason", "x")
	if !sessionRefused(r) {
		t.Errorf("T6 another session's failure was not refused as another session's:\n%s", r.stderr)
	}
	setenv(t, store.SessionEnv, "session-holding-the-story")
	mustRun(t, "team", "failure", "researcher", "--reason", "x")

	// A group that only helps, as `staffing` does.
	if out := mustRun(t, "team").stdout; !strings.Contains(out, "failure") {
		t.Errorf("`sdlc team` alone does not name failure:\n%s", out)
	}
}

// Red team V9: the name is read as FindReviewer reads it, folded and trimmed, and a budget
// counted on the raw word would give each spelling its own. A spelling is either the same
// agent or refused.
func TestAC9ANameInAnotherSpellingCountsAsTheSameAgent(t *testing.T) {
	t.Parallel()
	for _, spelling := range []string{"SECURITY", " security", "security "} {
		t.Run("AC_9 "+spelling, func(t *testing.T) {
			t.Parallel()
			teamAtAnalysis(t, map[string]any{"retries": 5})
			want := 1
			if r := run(t, "team", "failure", spelling, "--reason", "first", "--json"); r.code == 0 {
				if got := decode[teamFailure](t, r); got.Agent != "security" || got.Failures != 1 {
					t.Errorf("AC-9 %q was taken as %+v, want security's first", spelling, got)
				}
				want = 2
			} else if !strings.Contains(r.stderr, "SDLC-E") {
				t.Fatalf("AC-9 %q: %s", spelling, r.stderr)
			}
			if got := teamFails(t, "security", "second"); got.Failures != want {
				t.Errorf("AC-9 security's count after %q is %d, want %d", spelling, got.Failures, want)
			}
		})
	}
}

// Human advocate 2: the first failures of an agent sit on the record, and the reasons appeared
// only in the hand-over once the budget was spent. A person looking at a slow story can see
// that the project manager has failed twice.
func TestAC9StatusCountsAnAgentsFailuresUntilAPersonDecides(t *testing.T) {
	t.Parallel()
	teamAtAnalysis(t, map[string]any{"retries": 2})
	saysFailed := func(out, agent, n string) bool {
		return slices.ContainsFunc(strings.Split(out, "\n"), func(l string) bool {
			return strings.Contains(l, agent) && strings.Contains(strings.ToLower(l), "failed") && strings.Contains(l, n)
		})
	}
	if out := mustRun(t, "status").stdout; strings.Contains(strings.ToLower(out), "failed") {
		t.Errorf("AC-9 status speaks of failures with none:\n%s", out)
	}
	teamFails(t, "researcher", "it timed out")
	if out := mustRun(t, "status").stdout; !saysFailed(out, "researcher", "1") {
		t.Errorf("AC-9 status does not say the researcher has failed once:\n%s", out)
	}
	teamFails(t, "researcher", "it came back empty")
	if out := mustRun(t, "status").stdout; !saysFailed(out, "researcher", "2") {
		t.Errorf("AC-9 status does not say the researcher has failed twice:\n%s", out)
	}
	// A person's decision starts the count again, so status stops saying it.
	teamFails(t, "researcher", "it failed a third time")
	mustRun(t, "approve", "US-001")
	mustRun(t, "start")
	if out := mustRun(t, "status").stdout; saysFailed(out, "researcher", "") {
		t.Errorf("AC-9 status still counts failures a person has decided on:\n%s", out)
	}
}

// Human advocate 2, the log's half. `sdlc log` lists the stories whose commit gate has passed,
// so the failures are read from one, and a person's decision between them loses none: the log
// is what is left to read once the status has stopped saying it. A model wrote the reasons, so
// they cannot start a line of their own there or carry a control character (T11).
func TestAC9AFailureIsShownWithItsReasonInTheLogOfACommittedStory(t *testing.T) {
	t.Parallel()
	root := teamProject(t, map[string]any{"retries": 1, "agents": map[string]any{"project-manager": map[string]any{"enabled": false}}})
	forged := "  failed   nobody"
	mustRun(t, "start")
	teamFails(t, "researcher", "it timed out")
	teamFails(t, "researcher", "it came back empty\n"+forged+"\x1b[2J")
	mustRun(t, "approve", "US-001")
	mustRun(t, "start")
	reach(t, root, model.GateCommit)
	commitEverything(t, root)
	mustRun(t, "gate", "commit", "pass", "--note", "committed")

	out := mustRun(t, "log", "--all").stdout
	lines := strings.Split(out, "\n")
	for _, reason := range []string{"it timed out", "it came back empty"} {
		if !slices.ContainsFunc(lines, func(l string) bool {
			return strings.Contains(l, "researcher") && strings.Contains(l, "dor") && strings.Contains(l, reason)
		}) {
			t.Errorf("AC-9 sdlc log does not show a failure with its agent, its gate and its reason, %q:\n%s", reason, out)
		}
	}
	if strings.ContainsAny(out, "\x1b\r") {
		t.Errorf("T11 sdlc log printed a control character from a reason: %q", out)
	}
	if slices.Contains(lines, forged) {
		t.Errorf("T11 sdlc log printed a line a reason made up as one of its own:\n%s", out)
	}
}

// T5: a person who answers "the agent failed, carry on" on a high-tier story, with the tree
// as it will be committed, has not given the pre-commit approval. The plan closes this for
// the two kinds the story adds, so the commit gate is refused and the person is asked again.
// The tree is committed after the restart: with no commit the walk reaches SDLC-E0032
// whether or not the closure is there, and a row that took any refusal for a closure passed
// with none (red team V1).
func TestAC9AnApprovalOfAFailuresHandOverIsNotAPreCommitApprovalWithoutSayingSo(t *testing.T) {
	t.Parallel()
	atTheCommitGate := func(t *testing.T, tier string) string {
		t.Helper()
		root := gitProjectInitialised(t)
		setConfigOnDisk(t, root, "team", map[string]any{"retries": 0, "agents": map[string]any{"project-manager": map[string]any{"enabled": false}}})
		setStoryRiskTier(t, root, tier)
		commitEverything(t, root)
		mustRun(t, "start")
		reach(t, root, model.GateCommit)
		return root
	}
	escalated := func(kind string) func(t *testing.T) string {
		return func(t *testing.T) string {
			t.Helper()
			return mustRun(t, "escalate", kind, "--message", "the bookkeeper's run failed, carry on?").stdout
		}
	}
	reported := func(t *testing.T) string {
		t.Helper()
		return mustRun(t, "team", "failure", "bookkeeper", "--reason", "it returned nothing").stdout
	}
	for _, tc := range []struct {
		name       string
		handOver   func(t *testing.T) string
		answers    bool
		saysItself bool
	}{
		{"AC_9 a failure past team.retries", reported, false, true},
		{"AC_9 a hand-over of the kind a failure makes", escalated("agent_failed"), false, false},
		{"AC_5 a by-hand gate's hand-over", escalated("work_by_hand"), false, false},
		{"AC_9 the control, a person asked the question itself", escalated("pre_commit_approval"), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := atTheCommitGate(t, "high")
			handOver := tc.handOver(t)
			answer := mustRun(t, "approve", "US-001").stdout
			mustRun(t, "start")
			commitEverything(t, root)
			r := run(t, "gate", "commit", "pass", "--note", "committed")
			if tc.answers {
				if r.code != 0 {
					t.Fatalf("T5 the pre-commit question was answered, and the gate refused it:\n%s", r.stderr)
				}
				// Red team R7 (V18): the words are for an answer that leaves the question open. Said
				// on every approval, a person who just answered it is told they did not.
				if strings.Contains(strings.ToLower(answer), "pre-commit") {
					t.Errorf("T5 the answer to the pre-commit question says it leaves that question open:\n%s", answer)
				}
				return
			}
			if r.code == 0 {
				t.Fatalf("T5 approving this hand-over passed the commit gate:\n%s", r.stdout)
			}
			if !strings.Contains(r.stderr, "SDLC-E0037") {
				t.Fatalf("T5 the commit gate refused for another reason, want SDLC-E0037:\n%s", r.stderr)
			}
			// Both messages say so where the tier waits: a person who reads them is not surprised
			// to be asked again.
			if !strings.Contains(strings.ToLower(answer), "pre-commit") {
				t.Errorf("T5 the approval does not say it leaves the pre-commit question open:\n%s", answer)
			}
			if tc.saysItself && !strings.Contains(strings.ToLower(handOver), "pre-commit") {
				t.Errorf("T5 the hand-over does not say its answer leaves the pre-commit question open:\n%s", handOver)
			}
			// Asked again, the answer to that question is the one that counts.
			mustRun(t, "escalate", "pre_commit_approval", "--message", "ready to commit?")
			mustRun(t, "approve", "US-001")
			mustRun(t, "start")
			commitEverything(t, root)
			if r := run(t, "gate", "commit", "pass", "--note", "committed"); r.code != 0 {
				t.Errorf("T5 the person answered the pre-commit question, and the gate still refuses:\n%s", r.stderr)
			}
		})
	}
	t.Run("AC_9 a tier that does not wait is told nothing of it", func(t *testing.T) {
		t.Parallel()
		root := atTheCommitGate(t, "low")
		mustRun(t, "escalate", "agent_failed", "--message", "the bookkeeper's run failed, carry on?")
		answer := mustRun(t, "approve", "US-001").stdout
		mustRun(t, "start")
		if strings.Contains(strings.ToLower(answer), "pre-commit") {
			t.Errorf("T5 the approval speaks of a question this tier never asks:\n%s", answer)
		}
		commitEverything(t, root)
		if r := run(t, "gate", "commit", "pass", "--note", "committed"); r.code != 0 {
			t.Errorf("T5 a tier that does not wait was held:\n%s", r.stderr)
		}
	})
	// Red team R7 (V18): the hand-over `team failure` writes is the one whose words this story
	// chooses, and on a tier that does not wait neither it nor its answer speaks of the question.
	t.Run("AC_9 a failure past team.retries on a tier that does not wait", func(t *testing.T) {
		t.Parallel()
		root := atTheCommitGate(t, "low")
		handOver := reported(t)
		answer := mustRun(t, "approve", "US-001").stdout
		for what, text := range map[string]string{"the hand-over": handOver, "the approval": answer} {
			if strings.Contains(strings.ToLower(text), "pre-commit") {
				t.Errorf("T5 %s speaks of a question this tier never asks:\n%s", what, text)
			}
		}
		mustRun(t, "start")
		commitEverything(t, root)
		if r := run(t, "gate", "commit", "pass", "--note", "committed"); r.code != 0 {
			t.Errorf("T5 a tier that does not wait was held:\n%s", r.stderr)
		}
	})
}

// Security review note 2. approvedCommit is who holds a story to a person's answer, and an
// approved failure names no commit: read as the latest answer it left the story held to
// nothing, or to a commit nobody answered for. Rejected stays the end of it: approved and
// then sent back is sent back.
func TestAC9ThePersonsAnswerToTheCommitIsReadPastAnApprovedFailure(t *testing.T) {
	t.Parallel()
	named, other := strings.Repeat("a", 40), strings.Repeat("b", 40)
	answer := model.Approval{Decision: model.DecisionApproved, Type: "pre_commit_approval", Commit: named}
	for _, tc := range []struct {
		name      string
		approvals []model.Approval
		want      string
	}{
		{"AC_9 an approved failure after an answer", []model.Approval{answer, {Decision: model.DecisionApproved, Type: "agent_failed"}}, named},
		{"AC_5 an approved by-hand hand-over after an answer", []model.Approval{answer, {Decision: model.DecisionApproved, Type: "work_by_hand"}}, named},
		{"AC_9 one that names a commit does not stand for one", []model.Approval{answer, {Decision: model.DecisionApproved, Type: "agent_failed", Commit: other}}, named},
		{"AC_9 alone it names nothing", []model.Approval{{Decision: model.DecisionApproved, Type: "agent_failed", Commit: other}}, ""},
		{"AC_5 alone it names nothing", []model.Approval{{Decision: model.DecisionApproved, Type: "work_by_hand", Commit: other}}, ""},
		{"AC_9 rejected, it sends the work back", []model.Approval{answer, {Decision: model.DecisionRejected, Type: "agent_failed"}}, ""},
		{"AC_9 sent back and then a failure", []model.Approval{answer, {Decision: model.DecisionRejected, Type: "pre_commit_approval"}, {Decision: model.DecisionApproved, Type: "agent_failed"}}, ""},
		{"AC_9 a released kind answers as it did", []model.Approval{answer, {Decision: model.DecisionApproved, Type: "review_not_converging", Commit: other}}, other},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := approvedCommit(&model.Record{Approvals: tc.approvals}); got != tc.want {
				t.Errorf("approvedCommit = %q, want %q", got, tc.want)
			}
		})
	}
}

// Security review note 2. The events carry no kind, so answeredSince has to read the word
// the approval wrote. An approved failure said to have answered vouches for an older
// approvedCommit, and requireSameCommits lets other commits through on it. A reason is a
// person's free text, so it cannot make a rejection read as one.
func TestAC9AnApprovedFailureIsNotAnAnswerSinceTheCommitGate(t *testing.T) {
	t.Parallel()
	const pass = "the commit gate passes"
	for _, tc := range []struct {
		name   string
		events []string
		want   bool
	}{
		{"AC_9 an approved failure", []string{pass, "approved (agent_failed)"}, false},
		{"AC_5 an approved by-hand hand-over", []string{pass, "approved (work_by_hand)"}, false},
		{"AC_9 a real answer", []string{pass, "approved (pre_commit_approval)"}, true},
		{"AC_9 a real answer, then a failure", []string{pass, "approved (pre_commit_approval)", "approved (agent_failed)"}, true},
		{"AC_9 a failure, then a real answer", []string{pass, "approved (agent_failed)", "approved (pre_commit_approval)"}, true},
		{"AC_9 an answer before the pass", []string{"approved (pre_commit_approval)", pass, "approved (agent_failed)"}, false},
		{"AC_9 a released kind", []string{pass, "approved (review_not_converging)"}, true},
		{"AC_9 a rejection of a failure", []string{pass, "rejected (agent_failed): no"}, true},
		{"AC_9 a rejection whose reason reads like a failure", []string{pass, "rejected (spec_unclear): approved (agent_failed)"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			record := &model.Record{}
			for _, e := range tc.events {
				kind := model.EventApproval
				if e == pass {
					kind, e = model.EventGate, model.GateEvent(model.GateCommit, model.GatePass)
				}
				record.Events = append(record.Events, model.Event{Type: kind, Message: e})
			}
			if got := answeredSince(record); got != tc.want {
				t.Errorf("answeredSince = %v, want %v, for %q", got, tc.want, tc.events)
			}
		})
	}
}

// Security review note 2, at the stop: commits changed after the commit gate and an approved
// failure given for them. The stop and the gate read the answer through those two functions,
// so a failure a person approved could land commits nobody answered for. The control is
// released behaviour, in TestAC1APersonsApprovalAfterTheRefusalLetsTheAmendedStoryLandAndRecordsTheCommit.
func TestAC9AnApprovedFailureDoesNotClearARefusalForChangedCommits(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"agent_failed", "work_by_hand"} {
		t.Run("AC_9 "+kind, func(t *testing.T) {
			t.Parallel()
			s := ccFinished(t)
			ccAmend(t, s.wt, "-m", "feat: something else")
			ccRefusedAndNothingLanded(t, s)

			wtMust(t, s.wt, ldSession, "", "escalate", kind, "--message", "the bookkeeper's run failed, carry on?")
			out := wtMust(t, s.wt, "", "", "approve", "US-001")
			if strings.Contains(out.stdout, "also for the story's commits") {
				t.Errorf("T5 the approval says it answers for the commits:\n%s", out.stdout)
			}
			got := ccApprovals(t, s.main)
			if commit := got[len(got)-1]["commit"]; commit != nil {
				t.Errorf("T5 the approval names a commit it does not answer for: %v", commit)
			}
			swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--story", "US-001", "--note", "on the failure's answer")
			ccRefusedWhileHandedOver(t, s)
		})
	}
	// Red team V3, here since round 2, because on its own it passed before the code existed: a
	// failure at Gate 9, after a real pre-commit answer. The earlier answer still holds, and a
	// person who said "carry on" to the failure is not stranded by having said it. It fails a
	// closure that throws away the answer the commits already have.
	t.Run("AC_9 a failure at the last gate leaves the earlier answer standing", func(t *testing.T) {
		t.Parallel()
		s := ccFinished(t)
		tip := swRev(t, s.wt, "HEAD")
		wtMust(t, s.wt, ldSession, "", "escalate", "agent_failed", "--message", "the bookkeeper's run failed, carry on?")
		wtMust(t, s.wt, "", "", "approve", "US-001")
		ldLanded(t, s.main, s.wt, tip)
	})
}

// Security review note 2: a story held only to a person's answer, the record's own commit
// gone. An approved failure read as the latest answer leaves recorded and answered both empty,
// and requireCommitsApproved answers "no anchor" and lands whatever the tip is.
func TestAC9AnApprovedFailureDoesNotLeaveAStoryWithNoAnchor(t *testing.T) {
	t.Parallel()
	s := ccFinished(t)
	ccAmend(t, s.wt, "-m", "feat: answered for")
	ccRefusedAndNothingLanded(t, s)
	ccHandedOverAndApproved(t, s.wt)

	// The record as a story held to the answer alone has it, with the failure approved after
	// it: no commit named, since answerForCommits writes none for this kind. Written by hand
	// because the iteration ended with the first answer, and a finished story cannot be
	// started again to hand over.
	doc := ccRecordJSON(t, s.main)
	delete(doc, "commit")
	approvals, _ := doc["approvals"].([]any)
	previous, _ := approvals[len(approvals)-1].(map[string]any)
	doc["approvals"] = append(approvals, map[string]any{
		"at": previous["at"], "decision": model.DecisionApproved, "type": "agent_failed", "tree_hash": previous["tree_hash"],
	})
	events, _ := doc["events"].([]any)
	doc["events"] = append(events, map[string]any{"at": previous["at"], "type": model.EventApproval, "message": "approved (agent_failed)"})
	ccSaveRecord(t, s.main, doc)

	ccAmend(t, s.wt, "-m", "feat: nobody answered for this")
	swRefused(t, ldNotApproved, s.wt, ldSession, "stop")
	if got := wtStatuses(t, s.main)["US-001"]; got == string(model.StatusDone) {
		t.Error("T5 a story held to a person's answer landed on commits nobody answered for")
	}
}

// setStoryRiskTier sets US-001's risk tier in the backlog.
func setStoryRiskTier(t *testing.T, root, tier string) {
	t.Helper()
	path := filepath.Join(root, "user_stories.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var backlog map[string]any
	if err := json.Unmarshal(raw, &backlog); err != nil {
		t.Fatal(err)
	}
	stories, _ := backlog["stories"].([]any)
	for _, s := range stories {
		if story, ok := s.(map[string]any); ok && story["id"] == "US-001" {
			story["risk_tier"] = tier
		}
	}
	out, err := json.MarshalIndent(backlog, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// ---------------------------------------------------------------------- AC-10

// AC-10: the floor is a refusal naming who staffs again; the tool does not add the agent.
func TestAC10AStaffingThatLeavesSecurityOutOfASensitiveStoryIsRefused(t *testing.T) {
	t.Parallel()
	saysStaffAgain := func(t *testing.T, r result) {
		t.Helper()
		stfRefused(t, r, stfCodeFloor, "security")
		said := strings.ToLower(r.stderr)
		if !strings.Contains(said, "project manager") && !strings.Contains(said, "project-manager") {
			t.Errorf("AC-10 the refusal does not say the project manager staffs the story again:\n%s", r.stderr)
		}
	}
	t.Run("AC_10 at the document", func(t *testing.T) {
		t.Parallel()
		// Nothing said yet reads as sensitive.
		root := teamAtAnalysis(t, map[string]any{})
		saysStaffAgain(t, stfUnchanged(t, root, stfFull("security").text(t), "staffing", "set"))
	})
	t.Run("AC_10 at an amendment", func(t *testing.T) {
		t.Parallel()
		root := teamAtAnalysis(t, map[string]any{})
		stfSet(t, stfFull())
		stfAnalysed(t, true)
		saysStaffAgain(t, stfUnchanged(t, root, "", "staffing", "amend", "security", "--left-out", "--reason", "it is a rename"))
	})
	t.Run("AC_10 a copy, at the gate that says the story is sensitive", func(t *testing.T) {
		t.Parallel()
		root := teamStaffedAnalysed(t, teamOf(agentEntries{"security": {"instances": 2}}),
			stfFull().plus("security-2", "opus", "high"), false)
		mustRun(t, "staffing", "amend", "security-2", "--left-out", "--reason", "one reader is enough")
		r := stfUnchanged(t, root, "", "gate", "analysis", "pass", "--security-sensitive")
		stfRefused(t, r, stfCodeFloor, "security-2")
	})
	t.Run("AC_10 disabled, the refusal does not apply and the gate says so", func(t *testing.T) {
		t.Parallel()
		root := teamAtAnalysis(t, teamOf(agentEntries{"security": {"enabled": false}}))
		stfSet(t, stfFull("security"))
		mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
		mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
		out := mustRun(t, "gate", "analysis", "pass", "--security-sensitive")
		saysDisabled(t, "AC-10 the gate", out.stdout+out.stderr, "security")
		_ = root
	})
}

// ---------------------------------------------------------------------- AC-11

func TestAC11TheListsAndEachAgentsFloorAreReadFromTheTeam(t *testing.T) {
	t.Parallel()
	t.Run("AC_11 the defaults with nothing set", func(t *testing.T) {
		t.Parallel()
		root := gitProjectInitialised(t)
		teamOutOfTheFile(t, root)
		commitEverything(t, root)
		mustRun(t, "start")
		mustRun(t, "gate", "dor", "pass")
		r := stfUnchanged(t, root, stfFull().with("sdet", func(a *stfAgent) { a.AskedModel = "fable" }).text(t), "staffing", "set")
		stfRefused(t, r, stfCodeLimits, "sdet", "fable", "haiku", "sonnet", "opus", "team.models")
		r = stfUnchanged(t, root, stfFull().with("sdet", func(a *stfAgent) { a.AskedEffort = "extreme" }).text(t), "staffing", "set")
		stfRefused(t, r, stfCodeLimits, "sdet", "extreme", "low", "medium", "high", "xhigh", "max", "team.efforts")
		// No default lowest: every agent may be asked for the lowest name in each list.
		stfSet(t, stfFull().with("verifier", func(a *stfAgent) { a.AskedModel, a.AskedEffort = "haiku", "low" }))
		out := mustRun(t, "staffing", "show").stdout
		for _, want := range []string{"haiku, sonnet, opus", "low, medium, high, xhigh, max"} {
			if !strings.Contains(out, want) {
				t.Errorf("AC-11 staffing show does not say what may be asked for (%q):\n%s", want, out)
			}
		}
	})
	t.Run("AC_11 names no provider has, ranked by the list and not by name", func(t *testing.T) {
		t.Parallel()
		section := map[string]any{
			"models": []string{"zeta", "alpha"}, "efforts": []string{"9", "1"},
			"agents": map[string]any{"verifier": map[string]any{"lowest": map[string]any{"model": "alpha", "effort": "1"}}},
		}
		root := teamAtAnalysis(t, section)
		doc := stfFull()
		for i := range doc.Agents {
			doc.Agents[i].AskedModel, doc.Agents[i].AskedEffort = "zeta", "9"
		}
		r := stfUnchanged(t, root, doc.text(t), "staffing", "set")
		stfRefused(t, r, stfCodeLimits, "verifier", "alpha")
		stfSet(t, doc.with("verifier", func(a *stfAgent) { a.AskedModel, a.AskedEffort = "alpha", "1" }))
		r = stfUnchanged(t, root, "", "staffing", "amend", "verifier", "--model", "zeta", "--reason", "cheaper")
		stfRefused(t, r, stfCodeLimits, "verifier", "alpha", "team.agents.verifier.lowest")
	})
}

// AC-11: bring_back moved into the entry of the reviewer it names, and does what it did.
func TestAC11BringBackInAnAgentsEntryBringsItBack(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "perf", "red-team")
	stfThrough(t, root, model.GateImplementation, "perf", "red-team")
	writeFile(t, root, "internal/hot/loop.go", "// the story's change\n")
	stfThrough(t, root, model.GateCodeReview, "perf", "red-team")
	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "code_review", "--json"))
	if got, _ := listed.review("code_review", "perf"); got.LeftOut || !strings.HasPrefix(got.BroughtBackBy, "internal/hot/") {
		t.Errorf("AC-11 perf's bring_back did not bring it back: %+v", got)
	}
}

// ---------------------------------------------------------------- AC-12 and AC-13

// teamOpeningCommands is every command that opens the project (ANALYSIS.md, section 4).
// `artifact list` is not among them: it prints the table it was built with, and opens nothing.
var teamOpeningCommands = [][]string{
	{"status"}, {"status", "--json"}, {"story", "list"}, {"log"}, {"ack", "--through", "HEAD"}, {"start"}, {"stop"},
	{"gate", "dor", "pass"}, {"codemap", "write"}, {"review", "list"}, {"review", "add", "dor", "human-advocate", "note"},
	{"staffing", "show"}, {"staffing", "set"}, {"freeze"}, {"unfreeze", "--reason", "x"},
	{"escalate", "spec_unclear", "--message", "x"}, {"approve", "US-001"}, {"release", "US-001", "--reason", "x"},
	{"cost"}, {"trace", "US-001"}, {"team", "failure", "researcher", "--reason", "x"},
}

// teamOpeningRefused holds every command that opens the project to the refusal.
func teamOpeningRefused(t *testing.T, code string, says ...string) {
	t.Helper()
	for _, args := range teamOpeningCommands {
		// `codemap write` reads its sections before it opens the project, so what it reads has
		// to be sections for the row to reach the opening.
		stdin := "# stdin\n"
		if args[0] == "codemap" {
			stdin = cmRefund
		}
		r := runWith(t, stdin, args...)
		if r.code == 0 || !strings.Contains(r.stderr+r.stdout, code) {
			t.Errorf("sdlc %s: exit %d, want %s\n%s%s", strings.Join(args, " "), r.code, code, r.stdout, r.stderr)
			continue
		}
		for _, want := range says {
			if !strings.Contains(r.stderr+r.stdout, want) {
				t.Errorf("sdlc %s: the refusal does not say %q:\n%s%s", strings.Join(args, " "), want, r.stdout, r.stderr)
			}
		}
	}
	// What does not open the project keeps working, so a person can read the fix.
	for _, args := range [][]string{{"version"}, {"help"}, {"status", "--help"}, {"gate", "--help"}} {
		if r := run(t, args...); r.code != 0 {
			t.Errorf("sdlc %s exits %d in a project that is refused:\n%s", strings.Join(args, " "), r.code, r.stderr)
		}
	}
	// And doctor reports it in its configuration check, with the fix, and exits 1.
	r := run(t, "doctor")
	if r.code == 0 {
		t.Errorf("doctor exits 0 in a project every other command refuses:\n%s", r.stdout)
	}
	c := doctorConfiguration(t)
	if c.State != stateProblem || !strings.Contains(c.Detail, code) || c.Fix == "" {
		t.Errorf("doctor's configuration check: %+v, want a problem with %s and the fix", c, code)
	}
	for _, want := range says {
		if !strings.Contains(c.Detail, want) {
			t.Errorf("doctor does not say %q: %s", want, c.Detail)
		}
	}
	if strings.Contains(c.Fix, "--force") {
		t.Errorf("doctor sends a person to overwrite the file: %s", c.Fix)
	}
}

// AC-12: the old keys stop every command, in a started story and with none.
func TestAC12AnOldKeyIsRefusedByEveryCommandThatOpensTheProjectAndDoctorGivesTheSameFix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		key    string
		value  any
		starts bool
		says   []string
	}{
		{"AC_12 a staffing section", "staffing", map[string]any{"enabled": true, "models": []string{"opus"}, "efforts": []string{"low"},
			"lowest": map[string]any{"verifier": map[string]any{"model": "opus"}}}, true,
			[]string{"staffing.models", "team.models", "staffing.lowest", "team.agents.verifier.lowest"}},
		{"AC_12 a staffing section that is off", "staffing", map[string]any{"enabled": false}, false,
			[]string{"staffing.enabled", "team.agents.project-manager"}},
		{"AC_12 null", "staffing", nil, false, []string{"staffing"}},
		{"AC_12 gate7_advisory true", "reviews", map[string]any{"gate7_advisory": true}, true,
			[]string{"reviews.gate7_advisory", "team.agents.code-reviewer.can_block"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			if tc.starts {
				mustRun(t, "start")
			}
			setConfigOnDisk(t, root, tc.key, tc.value)
			teamOpeningRefused(t, teamCodeOld, tc.says...)
		})
	}
}

// AC-13: what cannot be used stops every command, the roster's rows included.
func TestAC13ATeamThatCannotBeUsedIsRefusedByEveryCommandThatOpensTheProject(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		section map[string]any
		says    []string
	}{
		{"AC_13 a key that is not an agent", teamOf(agentEntries{"designer": {"enabled": false}}),
			[]string{"team.agents", "designer", "the agents are", "project-manager"}},
		{"AC_13 an agent in another case", teamOf(agentEntries{"Security": {"enabled": false}}),
			[]string{"team.agents", "Security"}},
		{"AC_8 a copy's name is not a key", teamOf(agentEntries{"security-2": {"enabled": false}}),
			[]string{"team.agents", "security-2"}},
		{"AC_13 a bring_back on a reviewer of the plan", teamOf(agentEntries{"architect": {"bring_back": []string{"internal/"}}}),
			[]string{"team.agents.architect.bring_back", "reviewers there are"}},
		{"AC_13 a bring_back on an agent that reviews nothing", teamOf(agentEntries{"researcher": {"bring_back": []string{"internal/"}}}),
			[]string{"team.agents.researcher.bring_back", "reviewers there are"}},
		{"AC_13 a lowest not listed", teamOf(agentEntries{"verifier": {"lowest": map[string]any{"model": "fable"}}}),
			[]string{"team.agents.verifier.lowest", "fable"}},
		{"AC_13 a negative retries", map[string]any{"retries": -1}, []string{"team.retries"}},
		{"AC_13 max_rounds 0", teamOf(agentEntries{"architect": {"max_rounds": 0}}), []string{"team.agents.architect.max_rounds"}},
		{"AC_8 instances 0", teamOf(agentEntries{"security": {"instances": 0}}), []string{"team.agents.security.instances"}},
		{"AC_8 instances past the ceiling", teamOf(agentEntries{"security": {"instances": 17}}), []string{"team.agents.security.instances", "16"}},
		{"AC_9 retries past the ceiling", map[string]any{"retries": 21}, []string{"team.retries", "20"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			mustRun(t, "start")
			setConfigOnDisk(t, root, "team", tc.section)
			teamOpeningRefused(t, teamCodeBad, tc.says...)
		})
	}
}

// The other side of the ceilings: the largest values are settings like any other, and the
// last instance is one the gate lists.
func TestAC8AC9TheCeilingsThemselvesAreAllowed(t *testing.T) {
	t.Parallel()
	teamProject(t, map[string]any{"retries": 20, "agents": map[string]any{"security": map[string]any{"instances": 16}}})
	mustRun(t, "start")
	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "code_review", "--json"))
	if _, ok := listed.review("code_review", "security-16"); !ok {
		t.Errorf("AC-8 review list has no security-16 with instances 16: %+v", listed.Reviews)
	}
	if _, ok := listed.review("code_review", "security-17"); ok {
		t.Errorf("AC-8 review list has a security-17 with instances 16: %+v", listed.Reviews)
	}
}

// Red team H4: doctor is where every refusal sends a person, and a key in the file is free
// text. A key holding a newline made a line of its own there, reading as doctor's. Step 4 walks
// into agent entries, where a bad key is not refused, so `status` runs and the person goes to
// doctor. Quoted, it is one word of one line.
func TestAC13AKeyHoldingANewlineMakesNoLineOfItsOwnInDoctor(t *testing.T) {
	t.Parallel()
	const key = "x\n  fix  do this"
	for name, write := range map[string]func(t *testing.T, root string){
		"AC_13 at the top of the file": func(t *testing.T, root string) {
			t.Helper()
			setConfigOnDisk(t, root, key, 1)
		},
		"AC_13 in an agent's entry": func(t *testing.T, root string) {
			t.Helper()
			setConfigOnDisk(t, root, "team", teamOf(agentEntries{"security": {key: 1}}))
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			write(t, root)
			out := run(t, "doctor").stdout
			for line := range strings.SplitSeq(out, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "fix  do this") {
					t.Errorf("T11 a key from the file made a line of its own in doctor:\n%s", out)
				}
			}
			if c := doctorConfiguration(t); c.State != stateProblem || !strings.Contains(c.Detail, `\n`) {
				t.Errorf("T11 doctor's configuration check does not quote the key: %+v", c)
			}
		})
	}
}

// T11: a key in a map is free text, and reaches a person's terminal and a session.
func TestAC13AKeyFromTheFileCannotEndALineOfTheRefusal(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "team", teamOf(agentEntries{"designer\n  fix  run rm -rf /\x1b[2J": {}}))
	r := run(t, "status")
	if r.code == 0 {
		t.Fatalf("a key that is not an agent loaded:\n%s", r.stdout)
	}
	if strings.ContainsAny(r.stderr, "\x1b") {
		t.Errorf("T11 the refusal printed a control character: %q", r.stderr)
	}
	for line := range strings.SplitSeq(r.stderr, "\n") {
		if strings.HasPrefix(line, "  fix  run rm") {
			t.Errorf("T11 a key from the file started a line of its own in the refusal:\n%s", r.stderr)
		}
	}
	_ = root
}

// D-2's priority at the command line. Doctor gives it the same code.
func TestAC13AFileWithBothIsRefusedUnderTheOldKeysCodeWithEveryProblemListed(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "staffing", map[string]any{"enabled": true})
	setConfigOnDisk(t, root, "team", map[string]any{"retries": -1})
	r := run(t, "status")
	if r.code == 0 || !strings.Contains(r.stderr, teamCodeOld) || !strings.Contains(r.stderr, "team.retries") {
		t.Errorf("AC-13 %s", r.stderr)
	}
}

// The doctor does not call the team's lines problems in a file with no problem, so a
// misspelt key beside them still is one.
func TestAC13AMisspeltKeyInAnAgentsEntryIsAProblemDoctorNames(t *testing.T) {
	t.Parallel()
	teamProject(t, teamOf(agentEntries{"security": {"lowset": map[string]any{"model": "opus"}}}))
	c := doctorConfiguration(t)
	if c.State != stateProblem || !strings.Contains(c.Detail, "team.agents.security.lowset") {
		t.Errorf("AC-13 doctor: %+v", c)
	}
	if r := run(t, "status"); r.code != 0 {
		t.Errorf("AC-13 a misspelt key refuses a command, which an unknown setting never did:\n%s", r.stderr)
	}
}

// `reviews.other` is a setting nobody knows, as it is today, beside the leftover that is not.
func TestAC2ASettingNobodyKnowsBesideTheLeftoverIsAProblemAndTheLeftoverIsNot(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "reviews", map[string]any{"gate7_advisory": false, "other": 1})
	c := doctorConfiguration(t)
	if c.State != stateProblem || !strings.Contains(c.Detail, "reviews.other") {
		t.Errorf("AC-2 doctor: %+v", c)
	}
	if r := run(t, "status"); r.code != 0 {
		t.Errorf("AC-2 %s", r.stderr)
	}
	_ = root
}

// config.File is what a refusal names, and the one place the fix sends a person.
func TestAC12TheRefusalSendsAPersonToEditTheFile(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "staffing", map[string]any{})
	r := run(t, "status")
	fix := stfFixLine(r.stderr)
	if !strings.Contains(fix, config.File) || strings.Contains(fix, "--force") {
		t.Errorf("AC-12 the fix does not send a person to edit %s: %q\n%s", config.File, fix, r.stderr)
	}
	_ = root
}
