package cli

// US-026: a staffing made when a story starts says which agents it needs and
// what model and effort each is asked to run at.
//
// The story's criteria say "plan"; its seventh decision names the thing the
// staffing, because plan already names Gate 4's record and PLAN.md. So no name
// below says plan for it.
//
// Every project here is one the test makes and turns the staffing on in. None
// reads this repository's own configuration (decision 11).
//
// What the tests bind that the criteria leave to the design (ANALYSIS.md, "The
// surface, as proposed"): the subcommands show, set, amend and assign and their
// flags; the document `staffing set` reads, which is the `agents` list
// `staffing show --json` prints; the fields of that output; the settings under
// `staffing`; and the codes SDLC-E0067 to SDLC-E0074.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

const (
	stfCodeOff        = "SDLC-E0067" // a staffing command where the staffing is off
	stfCodeNone       = "SDLC-E0068" // no staffing recorded
	stfCodeIncomplete = "SDLC-E0069" // a staffing that is not whole, or a second one
	stfCodeFloor      = "SDLC-E0070" // leaving out an agent that is never left out
	stfCodeLimits     = "SDLC-E0071" // a model or effort outside what is allowed
	stfCodePersons    = "SDLC-E0072" // changing what a person assigned
	stfCodeBlocked    = "SDLC-E0073" // leaving out a reviewer whose latest review blocks
	stfCodeSession    = "SDLC-E0074" // staffing assign from a Claude Code session
)

// stfAgents is every agent the plugin ships once this story has added the
// project manager. TestAC12TheStaffingCoversExactlyTheAgentsThePluginShips
// holds it to plugin/agents.
var stfAgents = []string{
	"architect", "bookkeeper", "code-reviewer", "human-advocate", "implementer", "perf",
	"project-manager", "red-team", "researcher", "sdet", "security", "verifier",
}

// stfAgent is one agent of a staffing, as `staffing set` reads it and
// `staffing show --json` prints it.
type stfAgent struct {
	Agent       string `json:"agent"`
	Needed      bool   `json:"needed"`
	Reason      string `json:"reason,omitempty"`
	AskedModel  string `json:"asked_model,omitempty"`
	AskedEffort string `json:"asked_effort,omitempty"`
	AskedReason string `json:"asked_reason,omitempty"`
	// What only the tool writes.
	SetBy         string `json:"set_by,omitempty"`
	BroughtBackBy string `json:"brought_back_by,omitempty"`
}

type stfDoc struct {
	Agents []stfAgent `json:"agents"`
}

type stfAtGate struct {
	Agent       string `json:"agent"`
	AskedModel  string `json:"asked_model"`
	AskedEffort string `json:"asked_effort"`
}

type stfAmendment struct {
	At     string `json:"at"`
	Agent  string `json:"agent"`
	Change string `json:"change"`
	Reason string `json:"reason"`
	By     string `json:"by"`
}

type stfShown struct {
	OK         bool           `json:"ok"`
	Story      string         `json:"story"`
	Gate       string         `json:"gate"`
	Agents     []stfAgent     `json:"agents"`
	AtGate     []stfAtGate    `json:"at_gate"`
	Amendments []stfAmendment `json:"amendments"`
}

func (s stfShown) agent(t *testing.T, name string) stfAgent {
	t.Helper()
	for _, a := range s.Agents {
		if a.Agent == name {
			return a
		}
	}
	t.Fatalf("staffing show --json has no agent %q: %+v", name, s.Agents)
	return stfAgent{}
}

func (s stfShown) atGate(name string) (stfAtGate, bool) {
	for _, a := range s.AtGate {
		if a.Agent == name {
			return a, true
		}
	}
	return stfAtGate{}, false
}

// stfSection is the staffing section the tests turn on.
func stfSection() map[string]any {
	return map[string]any{
		"enabled": true,
		"models":  []string{"haiku", "sonnet", "opus"},
		"efforts": []string{"low", "medium", "high", "xhigh", "max"},
		"lowest": map[string]any{
			"verifier": map[string]any{"model": "opus", "effort": "high"},
			"security": map[string]any{"model": "sonnet", "effort": "medium"},
		},
		"bring_back": map[string]any{
			"security": []string{"internal/hook/", "install.sh"},
			"perf":     []string{"internal/hot/"},
		},
	}
}

// stfProject is a project with the staffing on, committed, and nothing started.
func stfProject(t *testing.T) string {
	t.Helper()
	root := gitProjectInitialised(t)
	setConfigOnDisk(t, root, "staffing", stfSection())
	commitEverything(t, root)
	return root
}

// stfBegun is stfProject with its story started and found ready: where the
// runbook delegates to the project manager.
func stfBegun(t *testing.T) string {
	t.Helper()
	root := stfProject(t)
	mustRun(t, "start")
	mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
	return root
}

// stfFull is a staffing that names every agent, each needed at sonnet and
// medium but the verifier, which the project keeps at opus and high, and
// leaves out the ones named.
func stfFull(leftOut ...string) stfDoc {
	var doc stfDoc
	for _, name := range stfAgents {
		a := stfAgent{Agent: name, Needed: true, Reason: name + " has work on this story",
			AskedModel: "sonnet", AskedEffort: "medium", AskedReason: "the change is small and a later gate checks it"}
		if name == "verifier" {
			a.AskedModel, a.AskedEffort, a.AskedReason = "opus", "high", "nothing checks the checker"
		}
		if slices.Contains(leftOut, name) {
			a = stfAgent{Agent: name, Needed: false, Reason: "the story gives " + name + " nothing to read"}
		}
		doc.Agents = append(doc.Agents, a)
	}
	return doc
}

func (d stfDoc) with(name string, change func(a *stfAgent)) stfDoc {
	out := stfDoc{Agents: slices.Clone(d.Agents)}
	for i := range out.Agents {
		if out.Agents[i].Agent == name {
			change(&out.Agents[i])
		}
	}
	return out
}

func (d stfDoc) without(name string) stfDoc {
	return stfDoc{Agents: slices.DeleteFunc(slices.Clone(d.Agents), func(a stfAgent) bool { return a.Agent == name })}
}

func (d stfDoc) text(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func stfSet(t *testing.T, doc stfDoc) {
	t.Helper()
	mustRunWith(t, doc.text(t), "staffing", "set")
}

func stfShow(t *testing.T, args ...string) stfShown {
	t.Helper()
	got := decode[stfShown](t, mustRun(t, append(append([]string{"staffing", "show"}, args...), "--json")...))
	if !got.OK {
		t.Fatalf("staffing show --json is not ok: %+v", got)
	}
	return got
}

// stfRecord is the story's record as it is on disk, which is where a refused
// command must have changed nothing.
func stfRecord(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// stfFixLine is what a refusal prints after "fix".
func stfFixLine(stderr string) string {
	for line := range strings.SplitSeq(stderr, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "fix "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// stfRefused holds a command to a refusal with the code given, that says what
// to do next, names each thing in says, and wrote nothing to standard output.
func stfRefused(t *testing.T, r result, code string, says ...string) {
	t.Helper()
	if r.code == 0 {
		t.Fatalf("went ahead where it should have been refused with %s:\n%s%s", code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, code) {
		t.Errorf("refused with something other than %s:\n%s", code, r.stderr)
	}
	if stfFixLine(r.stderr) == "" {
		t.Errorf("the refusal does not say what to do next:\n%s", r.stderr)
	}
	for _, want := range says {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal does not say %q:\n%s", want, r.stderr)
		}
	}
	if r.stdout != "" {
		t.Errorf("a refusal wrote to standard output, which a skill reads as data: %q", r.stdout)
	}
}

// stfUnchanged runs a command that has to be refused and leave the record as
// it was.
func stfUnchanged(t *testing.T, root, stdin string, args ...string) result {
	t.Helper()
	before := stfRecord(t, root)
	r := runWith(t, stdin, args...)
	if r.code == 0 {
		t.Errorf("sdlc %s went ahead:\n%s", strings.Join(args, " "), r.stdout)
	}
	if after := stfRecord(t, root); after != before {
		t.Errorf("sdlc %s was refused and changed the record all the same:\n%s", strings.Join(args, " "), after)
	}
	return r
}

func stfAnalysed(t *testing.T, sensitive bool) {
	t.Helper()
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	flag := "--security-sensitive=false"
	if sensitive {
		flag = "--security-sensitive"
	}
	mustRun(t, "gate", "analysis", "pass", flag)
}

// stfWorked does a gate's own work, leaving its reviews to the test.
func stfWorked(t *testing.T, root string, gate model.Gate) {
	t.Helper()
	switch gate {
	case model.GateTestsFrozen:
		writeFile(t, root, "internal/invoice_test.go", "package internal\n\n// AC-1\n")
		mustRunWith(t, "# Test plan\n", "artifact", "write", "test_plan")
		mustRun(t, "freeze")
	case model.GatePlan:
		mustRunWith(t, "# Plan\n", "artifact", "write", "plan")
	case model.GateVerification:
		mustRunWith(t, "# Verification\n", "artifact", "write", "verification")
	case model.GateRetro:
		mustRunWith(t, "# Retro\n", "artifact", "write", "retro")
	case model.GateCommit:
		commitEverything(t, root)
	case model.GateDoR, model.GateAnalysis, model.GateDesignReview, model.GateImplementation, model.GateVerifierReview,
		model.GateCodeReview:
		// Left to the test.
	}
}

// stfReviewed has every reviewer of a gate approve, but the ones named.
func stfReviewed(t *testing.T, gate model.Gate, except ...string) {
	t.Helper()
	for _, r := range model.ReviewersFor(gate) {
		if r.WhenDoRAdvocate || slices.Contains(except, r.Role) {
			continue
		}
		mustRunWith(t, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
	}
}

// stfThrough takes a story whose analysis has passed through every gate before
// upTo, or through all of them where upTo is "", with no review from anyone in
// out.
func stfThrough(t *testing.T, root string, upTo model.Gate, out ...string) {
	t.Helper()
	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	for _, gate := range model.Gates {
		if gate == upTo {
			return
		}
		if status.Gates[string(gate)] == string(model.GatePass) {
			continue
		}
		stfWorked(t, root, gate)
		stfReviewed(t, gate, out...)
		mustRun(t, "gate", string(gate), "pass", "--note", "reached by the test helper")
	}
}

// stfStaffed is a story with the staffing recorded and the analysis passed as
// not security-sensitive, with everybody in out left out: security by an
// amendment once the analysis has said the story is not sensitive, the others
// in the staffing itself.
func stfStaffed(t *testing.T, out ...string) string {
	t.Helper()
	root := stfBegun(t)
	stfSet(t, stfFull(slices.DeleteFunc(slices.Clone(out), func(n string) bool { return n == "security" })...))
	stfAnalysed(t, false)
	if slices.Contains(out, "security") {
		mustRun(t, "staffing", "amend", "security", "--left-out", "--reason", "nothing here crosses a trust boundary")
	}
	return root
}

// ---------------------------------------------------------------------- AC-1

func TestAC1TheAnalysisGateDoesNotPassWhileNoStaffingIsRecorded(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")

	r := stfUnchanged(t, root, "", "gate", "analysis", "pass", "--security-sensitive=false")
	stfRefused(t, r, stfCodeNone)
	if fix := stfFixLine(r.stderr); !strings.Contains(fix, "sdlc:project-manager") {
		t.Errorf("the refusal does not send the reader to the project manager: %s", fix)
	}
	if got := decode[statusPayload](t, mustRun(t, "status", "--json")); got.NextGate != string(model.GateAnalysis) {
		t.Errorf("the next gate is %q, want analysis: it has not passed", got.NextGate)
	}

	stfSet(t, stfFull())
	if r := run(t, "gate", "analysis", "pass", "--security-sensitive=false"); r.code != 0 {
		t.Fatalf("the analysis gate refused a story whose staffing is recorded:\n%s", r.stderr)
	}
}

func TestAC1TheStaffingSaysForEveryAgentWhetherItIsNeededWhyAndWhatModelAndEffortIsAskedFor(t *testing.T) {
	t.Parallel()
	stfBegun(t)
	doc := stfFull("architect", "red-team", "perf", "human-advocate")
	r := mustRunWith(t, doc.text(t), "staffing", "set")
	if r.stdout == "" {
		t.Error("staffing set printed nothing: whoever ran it cannot tell it was recorded")
	}

	got := stfShow(t)
	if got.Story != "US-001" {
		t.Errorf("story = %q", got.Story)
	}
	if len(got.Agents) != len(stfAgents) {
		t.Fatalf("the staffing shows %d agents, and the plugin ships %d: %+v", len(got.Agents), len(stfAgents), got.Agents)
	}
	for _, want := range doc.Agents {
		a := got.agent(t, want.Agent)
		if a.Needed != want.Needed || a.Reason != want.Reason {
			t.Errorf("%s: needed %v because %q, want %v because %q", a.Agent, a.Needed, a.Reason, want.Needed, want.Reason)
		}
		if a.AskedModel != want.AskedModel || a.AskedEffort != want.AskedEffort || a.AskedReason != want.AskedReason {
			t.Errorf("%s: asked for %q at %q because %q, want %q at %q because %q", a.Agent,
				a.AskedModel, a.AskedEffort, a.AskedReason, want.AskedModel, want.AskedEffort, want.AskedReason)
		}
		// Decision 8: a person chose it only where the person's command ran.
		// This staffing was recorded from a terminal, with `set`, and is not one.
		if a.SetBy == "person" {
			t.Errorf("%s is recorded as a person's choice, and `staffing set` recorded it", a.Agent)
		}
	}
	if len(got.Amendments) != 0 {
		t.Errorf("a staffing just recorded has amendments: %+v", got.Amendments)
	}
}

func TestAC1AStaffingThatDoesNotCoverEveryAgentWithAReasonAModelAndAnEffortIsRefused(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	full := stfFull("perf")
	for _, tc := range []struct {
		name string
		doc  stfDoc
		says string
	}{
		{"AC_1 no row for the project manager", full.without("project-manager"), "project-manager"},
		{"AC_1 no row for a reviewer", full.without("red-team"), "red-team"},
		{"AC_1 no row for an agent that does a gate's work", full.without("sdet"), "sdet"},
		{"AC_1 an agent the plugin does not ship", stfDoc{Agents: append(slices.Clone(full.Agents),
			stfAgent{Agent: "designer", Needed: false, Reason: "there is no interface"})}, "designer"},
		{"AC_1 an agent named twice", stfDoc{Agents: append(slices.Clone(full.Agents), full.Agents[0])}, full.Agents[0].Agent},
		{"AC_1 needed with no reason", full.with("architect", func(a *stfAgent) { a.Reason = "" }), "architect"},
		{"AC_1 left out with no reason", full.with("perf", func(a *stfAgent) { a.Reason = "" }), "perf"},
		{"AC_1 a reason that is only white space", full.with("perf", func(a *stfAgent) { a.Reason = " \t\n" }), "perf"},
		{"AC_1 needed with no model", full.with("researcher", func(a *stfAgent) { a.AskedModel = "" }), "researcher"},
		{"AC_1 needed with no effort", full.with("researcher", func(a *stfAgent) { a.AskedEffort = "" }), "researcher"},
		{"AC_1 needed with no reason for the model and effort", full.with("implementer", func(a *stfAgent) { a.AskedReason = "" }), "implementer"},
		{"AC_1 no agents at all", stfDoc{Agents: []stfAgent{}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := stfUnchanged(t, root, tc.doc.text(t), "staffing", "set")
			stfRefused(t, r, stfCodeIncomplete)
			if tc.says != "" && !strings.Contains(r.stderr, tc.says) {
				t.Errorf("the refusal does not name %q:\n%s", tc.says, r.stderr)
			}
		})
	}
	// Nothing refused was kept: the analysis gate still has no staffing.
	mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
	mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
	stfRefused(t, run(t, "gate", "analysis", "pass"), stfCodeNone)
}

func TestAC1AStaffingIsRecordedOnceAndAfterThatOnlyAmended(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	stfSet(t, stfFull("perf"))
	r := stfUnchanged(t, root, stfFull().text(t), "staffing", "set")
	stfRefused(t, r, stfCodeIncomplete, "staffing amend")
	if a := stfShow(t).agent(t, "perf"); a.Needed {
		t.Error("a second `staffing set` replaced the first")
	}
}

func TestAC1TheStaffingIsReadFromAFileOrFromStandardInputAndNothingElseIsTakenForOne(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	for name, tc := range map[string]struct {
		stdin string
		args  []string
	}{
		"AC_1 nothing on standard input":    {"", []string{"staffing", "set"}},
		"AC_1 not JSON":                     {"needed: everyone\n", []string{"staffing", "set"}},
		"AC_1 JSON that is not a staffing":  {`["architect"]`, []string{"staffing", "set"}},
		"AC_1 a file that is not there":     {"", []string{"staffing", "set", "--file", filepath.Join(root, "no-such-staffing.json")}},
		"AC_1 a needed that is not a truth": {`{"agents":[{"agent":"architect","needed":"yes","reason":"x"}]}`, []string{"staffing", "set"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := stfUnchanged(t, root, tc.stdin, tc.args...)
			if !strings.Contains(r.stderr, "SDLC-E") || stfFixLine(r.stderr) == "" {
				t.Errorf("the refusal carries no code or no fix:\n%s", r.stderr)
			}
		})
	}

	// From a file, outside the tree the reviews are stamped against.
	file := filepath.Join(wtTempDir(t), "staffing.json")
	if err := os.WriteFile(file, []byte(stfFull("red-team").text(t)), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, "staffing", "set", "--file", file)
	if a := stfShow(t).agent(t, "red-team"); a.Needed || a.Reason == "" {
		t.Errorf("the staffing read from --file is not the one recorded: %+v", a)
	}
}

func TestAC1TheStaffingCommandsNeedAStoryAndStaffingAloneHelps(t *testing.T) {
	t.Parallel()
	stfProject(t)
	// No story is being worked on, and none is named.
	for _, args := range [][]string{
		{"staffing", "show"},
		{"staffing", "set"},
		{"staffing", "amend", "perf", "--left-out", "--reason", "x"},
	} {
		r := runWith(t, stfFull().text(t), args...)
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E") || stfFixLine(r.stderr) == "" {
			t.Errorf("sdlc %s with no story being worked on: exit %d\n%s%s", strings.Join(args, " "), r.code, r.stdout, r.stderr)
		}
	}
	r := mustRun(t, "staffing")
	for _, want := range []string{"show", "set", "amend", "assign"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("`sdlc staffing` alone does not name %s:\n%s", want, r.stdout)
		}
	}
}

// A story another session holds is not staffed from this one: the command
// itself refuses, as every command that writes the story does.
func TestAC1AStaffingIsNotRecordedFromASessionThatDoesNotHoldTheStory(t *testing.T) {
	t.Parallel()
	root := stfProject(t)
	setenv(t, store.SessionEnv, "session-holding-the-story")
	mustRun(t, "start")
	mustRun(t, "gate", "dor", "pass")
	setenv(t, store.SessionEnv, "another-session")
	r := stfUnchanged(t, root, stfFull().text(t), "staffing", "set")
	if !sessionRefused(r) {
		t.Errorf("another session's `staffing set` was not refused as another session's:\n%s", r.stderr)
	}
	setenv(t, store.SessionEnv, "session-holding-the-story")
	stfSet(t, stfFull())
	setenv(t, store.SessionEnv, "another-session")
	r = stfUnchanged(t, root, "", "staffing", "amend", "perf", "--left-out", "--reason", "x")
	if !sessionRefused(r) {
		t.Errorf("another session's `staffing amend` was not refused as another session's:\n%s", r.stderr)
	}
}

// ---------------------------------------------------------------------- AC-2

func TestAC2ShowPrintsTheStaffingAndNamesWhoIsNeededAtTheGateTheStoryIsAt(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	doc := stfFull("red-team", "perf").
		with("researcher", func(a *stfAgent) { a.AskedModel, a.AskedEffort = "opus", "xhigh" }).
		with("architect", func(a *stfAgent) { a.AskedModel, a.AskedEffort = "opus", "high" }).
		with("code-reviewer", func(a *stfAgent) { a.AskedModel, a.AskedEffort = "opus", "max" }).
		with("bookkeeper", func(a *stfAgent) { a.AskedModel, a.AskedEffort = "haiku", "low" })
	stfSet(t, doc)

	t.Run("AC_2 in --json, at the gate the story is at", func(t *testing.T) {
		got := stfShow(t)
		if got.Gate != string(model.GateAnalysis) {
			t.Errorf("gate = %q, want analysis: dor has passed and nothing else", got.Gate)
		}
		at, ok := got.atGate("researcher")
		if !ok || at.AskedModel != "opus" || at.AskedEffort != "xhigh" {
			t.Errorf("at the analysis gate the output does not name the researcher at opus and xhigh: %+v", got.AtGate)
		}
		for _, other := range []string{"architect", "code-reviewer", "bookkeeper", "red-team"} {
			if _, ok := got.atGate(other); ok {
				t.Errorf("%s is named as needed at the analysis gate: %+v", other, got.AtGate)
			}
		}
	})

	t.Run("AC_2 in prose", func(t *testing.T) {
		out := mustRun(t, "staffing", "show").stdout
		lines := strings.Split(out, "\n")
		for _, a := range doc.Agents {
			if !a.Needed {
				if !slices.ContainsFunc(lines, func(l string) bool {
					return strings.Contains(l, a.Agent) && strings.Contains(l, "left out")
				}) {
					t.Errorf("no line says %s is left out:\n%s", a.Agent, out)
				}
			} else if !slices.ContainsFunc(lines, func(l string) bool {
				return strings.Contains(l, a.Agent) && strings.Contains(l, a.AskedModel) && strings.Contains(l, a.AskedEffort)
			}) {
				t.Errorf("no line gives %s with %s and %s:\n%s", a.Agent, a.AskedModel, a.AskedEffort, out)
			}
			if !strings.Contains(out, a.Reason) {
				t.Errorf("the reason for %s is not shown (%q):\n%s", a.Agent, a.Reason, out)
			}
		}
		if !strings.Contains(out, "analysis") {
			t.Errorf("the prose does not say which gate the story is at:\n%s", out)
		}
	})

	stfAnalysed(t, true)
	stfThrough(t, root, model.GateDesignReview)

	t.Run("AC_2 at a reviewed gate it names the reviewers needed there and no one left out", func(t *testing.T) {
		got := stfShow(t)
		if got.Gate != string(model.GateDesignReview) {
			t.Fatalf("gate = %q, want design_review", got.Gate)
		}
		for name, want := range map[string]stfAtGate{
			"architect":      {AskedModel: "opus", AskedEffort: "high"},
			"security":       {AskedModel: "sonnet", AskedEffort: "medium"},
			"human-advocate": {AskedModel: "sonnet", AskedEffort: "medium"},
		} {
			at, ok := got.atGate(name)
			if !ok || at.AskedModel != want.AskedModel || at.AskedEffort != want.AskedEffort {
				t.Errorf("design_review does not name %s at %s and %s: %+v", name, want.AskedModel, want.AskedEffort, got.AtGate)
			}
		}
		for _, out := range []string{"red-team", "perf", "code-reviewer", "bookkeeper"} {
			if _, ok := got.atGate(out); ok {
				t.Errorf("%s is named as needed at design_review: %+v", out, got.AtGate)
			}
		}
	})

	t.Run("AC_2 for a gate named with --gate", func(t *testing.T) {
		got := stfShow(t, "--gate", "code_review")
		if got.Gate != string(model.GateCodeReview) {
			t.Errorf("gate = %q, want code_review", got.Gate)
		}
		if at, ok := got.atGate("code-reviewer"); !ok || at.AskedModel != "opus" || at.AskedEffort != "max" {
			t.Errorf("code_review does not name the code reviewer at opus and max: %+v", got.AtGate)
		}
		if _, ok := got.atGate("architect"); ok {
			t.Errorf("the architect is named at code_review, which it does not review: %+v", got.AtGate)
		}
		retro := stfShow(t, "--gate", "retro", "--story", "US-001")
		if at, ok := retro.atGate("bookkeeper"); !ok || at.AskedModel != "haiku" || at.AskedEffort != "low" {
			t.Errorf("retro does not name the bookkeeper at haiku and low: %+v", retro.AtGate)
		}
		r := run(t, "staffing", "show", "--gate", "no_such_gate")
		if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0013") {
			t.Errorf("a gate this version does not know was not refused as one:\n%s%s", r.stdout, r.stderr)
		}
	})
}

// With the staffing on and none recorded there is nothing to show, and the
// command says so as a refusal. An answer of nobody would read, to the session
// that asks who is needed at a gate, as a staffing that leaves everybody out,
// which is the opposite of what the gates do with none recorded (T5: everyone,
// never no one).
func TestAC2ShowRefusesWhereNoStaffingIsRecordedAndNamesNobodyAsNeeded(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	for _, args := range [][]string{
		{"staffing", "show"},
		{"staffing", "show", "--gate", "code_review"},
		{"staffing", "show", "--story", "US-001"},
	} {
		r := stfUnchanged(t, root, "", args...)
		stfRefused(t, r, stfCodeNone)
		if fix := stfFixLine(r.stderr); !strings.Contains(fix, "sdlc:project-manager") {
			t.Errorf("sdlc %s: the refusal does not send the reader to the project manager: %s", strings.Join(args, " "), fix)
		}
	}
	r := stfUnchanged(t, root, "", "staffing", "show", "--json")
	var got struct {
		OK     *bool           `json:"ok"`
		Code   string          `json:"code"`
		Fix    string          `json:"fix"`
		Agents json.RawMessage `json:"agents"`
		AtGate json.RawMessage `json:"at_gate"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil {
		t.Fatalf("staffing show --json with none recorded did not print JSON: %v\n%s%s", err, r.stdout, r.stderr)
	}
	if got.OK == nil || *got.OK || got.Code != stfCodeNone || !strings.Contains(got.Fix, "sdlc:project-manager") {
		t.Errorf("staffing show --json with none recorded: want ok false, %s and a fix naming the project manager:\n%s", stfCodeNone, r.stdout)
	}
	if got.Agents != nil || got.AtGate != nil {
		t.Errorf("staffing show --json names agents where no staffing is recorded:\n%s", r.stdout)
	}

	// Recorded, it is shown.
	stfSet(t, stfFull())
	if shown := stfShow(t); len(shown.Agents) != len(stfAgents) {
		t.Errorf("the staffing shows %d agents once recorded, want %d", len(shown.Agents), len(stfAgents))
	}
}

// stfRanAt is the ways of saying what model or effort an agent ran at: in
// words, and as a field of the JSON. The environment can override both and the
// tool observes no run, so nothing it prints may claim one.
//
// Both are held to a model or an effort on purpose. The first list of phrases
// here was any `"ran` and any `runs on`, which the released `range_source` of
// `sdlc log --json` and doctor's "every gate that runs one is skipped" both
// match: a rule no output could keep without changing what a release prints.
var stfRanAt = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(ran|runs|running|was run|were run) (at|on|with) (the |a |an )?(model|effort|haiku|sonnet|opus|low|medium|high|xhigh|max)\b`),
	regexp.MustCompile(`(?i)"(ran|run|used|actual)_(model|effort)"|"(model|effort)_(ran|run|used)"`),
}

func stfSaysNothingRan(t *testing.T, what, text string) {
	t.Helper()
	for _, ran := range stfRanAt {
		if found := ran.FindString(text); found != "" {
			t.Errorf("%s says %q, and the tool cannot know what an agent ran at:\n%s", what, found, text)
		}
	}
}

func TestAC2TheOutputCallsItWhatWasAskedForAndNeverWhatAnAgentRanAt(t *testing.T) {
	t.Parallel()
	// The patterns see the claim, and do not see what a release already prints.
	for _, said := range []string{"the verifier ran at opus", "runs on the model", "was run at high", `"ran_model":"opus"`, `"effort_used":"high"`} {
		if !slices.ContainsFunc(stfRanAt, func(ran *regexp.Regexp) bool { return ran.MatchString(said) }) {
			t.Fatalf("no pattern sees %q", said)
		}
	}
	for _, golden := range []string{"prose.golden", "json.golden"} {
		released, err := os.ReadFile(filepath.Join("testdata", "staffing-off", golden))
		if err != nil {
			t.Fatal(err)
		}
		stfSaysNothingRan(t, "what the released tool prints ("+golden+")", string(released))
	}
	root := stfStaffed(t, "perf")
	mustRun(t, "staffing", "amend", "researcher", "--model", "opus", "--reason", "the corpus has to be designed")

	prose := mustRun(t, "staffing", "show").stdout
	if !strings.Contains(strings.ToLower(prose), "asked for") {
		t.Errorf("staffing show does not call the model and effort what was asked for:\n%s", prose)
	}
	raw := mustRun(t, "staffing", "show", "--json").stdout
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	for _, bare := range []string{`"model":`, `"effort":`} {
		if strings.Contains(raw, bare) {
			t.Errorf("staffing show --json has a bare %s field, which reads as what ran; it is what was asked for:\n%s", bare, raw)
		}
	}
	if !strings.Contains(raw, `"asked_model":`) || !strings.Contains(raw, `"asked_effort":`) {
		t.Errorf("staffing show --json does not name the fields as asked for:\n%s", raw)
	}

	stfThrough(t, root, "", "perf")
	outputs := map[string]string{
		"staffing show":        prose,
		"staffing show --json": raw,
		"review list":          mustRun(t, "review", "list").stdout,
		"review list --json":   mustRun(t, "review", "list", "--json").stdout,
		"log":                  mustRun(t, "log").stdout,
		"log --json":           mustRun(t, "log", "--json").stdout,
		"status":               mustRun(t, "status").stdout,
	}
	for _, args := range [][]string{
		{"staffing", "--help"}, {"staffing", "show", "--help"}, {"staffing", "set", "--help"},
		{"staffing", "amend", "--help"}, {"staffing", "assign", "--help"},
	} {
		outputs[strings.Join(args, " ")] = mustRun(t, args...).stdout
	}
	for what, text := range outputs {
		stfSaysNothingRan(t, "sdlc "+what, text)
	}
}

// ---------------------------------------------------------------------- AC-3

func TestAC3AGateWaitsOnlyForTheReviewersTheStaffingSaysTheStoryNeeds(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "architect", "red-team", "perf", "human-advocate", "security")
	stfThrough(t, root, model.GateDesignReview)

	// Everybody Gate 4 lists is left out, so nobody is waited for.
	if r := run(t, "gate", "design_review", "pass", "--note", "nobody is needed"); r.code != 0 {
		t.Fatalf("design_review refused a pass for want of a review the staffing leaves out:\n%s", r.stderr)
	}
	stfThrough(t, root, model.GateCodeReview)
	r := run(t, "gate", "code_review", "pass")
	stfRefused(t, r, "SDLC-E0029", "code-reviewer")
	for _, out := range []string{"security", "perf", "human-advocate"} {
		if strings.Contains(r.stderr, out) {
			t.Errorf("code_review waits for %s, which the staffing leaves out:\n%s", out, r.stderr)
		}
	}
	mustRunWith(t, "# code-reviewer\n", "review", "add", "code_review", "code-reviewer", "approve")
	mustRun(t, "gate", "code_review", "pass", "--note", "approved")

	// And the commit gate, which asks Gates 6 and 7 again, asks for no more.
	stfThrough(t, root, "")
	if got := decode[statusPayload](t, mustRun(t, "status", "--json")); got.NextGate != "" {
		t.Errorf("the story stopped at %q with every needed review in", got.NextGate)
	}
}

func TestAC3AReviewerTheStaffingKeepsIsStillWaitedFor(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "red-team", "perf")
	stfThrough(t, root, model.GateDesignReview)

	r := run(t, "gate", "design_review", "pass")
	stfRefused(t, r, "SDLC-E0029", "architect", "security", "human-advocate")
	for _, out := range []string{"red-team", "perf"} {
		if strings.Contains(r.stderr, out) {
			t.Errorf("design_review waits for %s, which the staffing leaves out:\n%s", out, r.stderr)
		}
	}
	// A block from a reviewer the story needs stops the gate as it always did.
	stfReviewed(t, model.GateDesignReview, "red-team", "perf", "architect")
	mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "block", "--note", "AC-3 has no step")
	stfRefused(t, run(t, "gate", "design_review", "pass"), "SDLC-E0030", "architect")
}

// A reviewer that reported and is then left out leaves a review behind. Once
// what it read has changed that review is stale, and the gate must not be held
// to it: nobody is going to bring it up to date.
func TestAC3AStaleReviewFromAReviewerSinceLeftOutDoesNotHoldTheGate(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "red-team")
	stfThrough(t, root, model.GateDesignReview)
	mustRunWith(t, "# perf\n", "review", "add", "design_review", "perf", "approve")
	mustRunWith(t, "# Plan\n\nA second step.\n", "artifact", "write", "plan")
	mustRun(t, "staffing", "amend", "perf", "--left-out", "--reason", "the plan no longer touches a hot path")
	stfReviewed(t, model.GateDesignReview, "red-team", "perf")

	if r := run(t, "gate", "design_review", "pass", "--note", "approved"); r.code != 0 {
		t.Fatalf("design_review was held to the stale review of a reviewer the staffing now leaves out:\n%s", r.stderr)
	}
}

// ---------------------------------------------------------------------- AC-4

func TestAC4AnAgentThatDoesAGatesWorkTheVerifierAndTheCodeReviewerAreNeverLeftOut(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"researcher", "sdet", "implementer", "bookkeeper", "verifier", "code-reviewer"} {
		t.Run("AC_4 "+name, func(t *testing.T) {
			t.Parallel()
			root := stfBegun(t)
			r := stfUnchanged(t, root, stfFull(name).text(t), "staffing", "set")
			stfRefused(t, r, stfCodeFloor, name)

			stfSet(t, stfFull())
			r = stfUnchanged(t, root, "", "staffing", "amend", name, "--left-out", "--reason", "to save the tokens")
			stfRefused(t, r, stfCodeFloor, name)
			if a := stfShow(t).agent(t, name); !a.Needed {
				t.Errorf("%s is left out after the refusal", name)
			}
		})
	}
}

func TestAC4SecurityIsNeverLeftOutOnceTheAnalysisHasRecordedTheStoryAsSecuritySensitive(t *testing.T) {
	t.Parallel()
	t.Run("AC_4 recorded as sensitive", func(t *testing.T) {
		root := stfBegun(t)
		stfSet(t, stfFull())
		stfAnalysed(t, true)
		r := stfUnchanged(t, root, "", "staffing", "amend", "security", "--left-out", "--reason", "it is only a rename")
		stfRefused(t, r, stfCodeFloor, "security")
	})
	t.Run("AC_4 recorded with nothing said, which reads as sensitive", func(t *testing.T) {
		root := stfBegun(t)
		stfSet(t, stfFull())
		mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
		mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
		mustRun(t, "gate", "analysis", "pass")
		r := stfUnchanged(t, root, "", "staffing", "amend", "security", "--left-out", "--reason", "it is only a rename")
		stfRefused(t, r, stfCodeFloor, "security")
	})
	t.Run("AC_4 recorded as not sensitive, security is the staffing's to decide", func(t *testing.T) {
		stfBegun(t)
		stfSet(t, stfFull())
		stfAnalysed(t, false)
		mustRun(t, "staffing", "amend", "security", "--left-out", "--reason", "nothing here crosses a trust boundary")
		if a := stfShow(t).agent(t, "security"); a.Needed {
			t.Error("security is still needed on a story that is not security-sensitive, after an amendment left it out")
		}
	})
}

// T3 of the threats: the floor is asked when a gate asks, not only when the
// staffing is written. A story recorded as sensitive after security was left
// out waits for security again, with no amendment, and nothing passes in
// between.
func TestAC4RecordingTheStoryAsSecuritySensitiveBringsSecurityBackWhateverTheStaffingSays(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "security", "red-team", "perf", "human-advocate")
	mustRun(t, "gate", "analysis", "pass", "--security-sensitive")
	stfThrough(t, root, model.GateDesignReview)

	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--json"))
	for _, gate := range []string{"design_review", "code_review"} {
		r, ok := listed.review(gate, "security")
		if !ok || r.LeftOut || !r.Blocking {
			t.Errorf("%s: security is not listed as a needed, blocking reviewer on a story recorded as sensitive: %+v", gate, r)
		}
	}
	mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "approve")
	stfRefused(t, run(t, "gate", "design_review", "pass"), "SDLC-E0029", "security")
	mustRunWith(t, "# security\n", "review", "add", "design_review", "security", "approve")
	mustRun(t, "gate", "design_review", "pass", "--note", "approved")
}

// ---------------------------------------------------------------------- AC-5

func TestAC5AModelOrAnEffortOutsideWhatTheConfigurationAllowsIsRefusedAndTheRefusalSaysWhatIsAllowed(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	models, efforts := []string{"haiku", "sonnet", "opus"}, []string{"low", "medium", "high", "xhigh", "max"}

	r := stfUnchanged(t, root, stfFull().with("researcher", func(a *stfAgent) { a.AskedModel = "fable" }).text(t), "staffing", "set")
	stfRefused(t, r, stfCodeLimits, append([]string{"researcher", "fable"}, models...)...)
	r = stfUnchanged(t, root, stfFull().with("sdet", func(a *stfAgent) { a.AskedEffort = "extreme" }).text(t), "staffing", "set")
	stfRefused(t, r, stfCodeLimits, append([]string{"sdet", "extreme"}, efforts...)...)
	// A name in another case is another name: the list is the project's.
	r = stfUnchanged(t, root, stfFull().with("sdet", func(a *stfAgent) { a.AskedModel = "Opus" }).text(t), "staffing", "set")
	stfRefused(t, r, stfCodeLimits, "sdet")

	stfSet(t, stfFull())
	r = stfUnchanged(t, root, "", "staffing", "amend", "researcher", "--model", "fable", "--reason", "the newest one")
	stfRefused(t, r, stfCodeLimits, append([]string{"researcher", "fable"}, models...)...)
	r = stfUnchanged(t, root, "", "staffing", "amend", "researcher", "--effort", "extreme", "--reason", "all of it")
	stfRefused(t, r, stfCodeLimits, append([]string{"researcher", "extreme"}, efforts...)...)
}

func TestAC5AModelOrAnEffortBelowTheLowestTheConfigurationSetsForThatAgentIsRefused(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	verifier := func(model, effort string) string {
		return stfFull().with("verifier", func(a *stfAgent) { a.AskedModel, a.AskedEffort = model, effort }).text(t)
	}

	// The verifier's lowest is opus and high: what is allowed is from there up.
	r := stfUnchanged(t, root, verifier("sonnet", "high"), "staffing", "set")
	stfRefused(t, r, stfCodeLimits, "verifier", "opus")
	r = stfUnchanged(t, root, verifier("opus", "medium"), "staffing", "set")
	stfRefused(t, r, stfCodeLimits, "verifier", "high", "xhigh", "max")
	// Security's lowest is sonnet and medium.
	r = stfUnchanged(t, root, stfFull().with("security", func(a *stfAgent) { a.AskedModel = "haiku" }).text(t), "staffing", "set")
	stfRefused(t, r, stfCodeLimits, "security", "sonnet", "opus")

	// At the lowest, and above it, is allowed; an agent with no lowest takes
	// the lowest the project allows anyone.
	stfSet(t, stfFull().
		with("verifier", func(a *stfAgent) { a.AskedModel, a.AskedEffort = "opus", "max" }).
		with("bookkeeper", func(a *stfAgent) { a.AskedModel, a.AskedEffort = "haiku", "low" }))

	r = stfUnchanged(t, root, "", "staffing", "amend", "verifier", "--model", "sonnet", "--reason", "it is a small change")
	stfRefused(t, r, stfCodeLimits, "verifier", "opus")
	r = stfUnchanged(t, root, "", "staffing", "amend", "verifier", "--effort", "low", "--reason", "it is a small change")
	stfRefused(t, r, stfCodeLimits, "verifier", "high", "xhigh", "max")
	mustRun(t, "staffing", "amend", "verifier", "--effort", "high", "--reason", "the lowest the project allows it")
	if a := stfShow(t).agent(t, "verifier"); a.AskedModel != "opus" || a.AskedEffort != "high" {
		t.Errorf("the verifier is asked for %s at %s, want opus at high", a.AskedModel, a.AskedEffort)
	}
}

// ---------------------------------------------------------------------- AC-6

func TestAC6AnAmendmentRecordsWhatChangedWhenAndWhy(t *testing.T) {
	t.Parallel()
	stfStaffed(t, "perf")
	before := time.Now().Add(-time.Minute)

	amendments := []struct {
		args   []string
		agent  string
		reason string
		// change is what the record of the change has to name.
		change string
	}{
		{[]string{"red-team", "--left-out"}, "red-team", "the plan adds no input a stranger controls", ""},
		{[]string{"perf", "--needed", "--model", "sonnet", "--effort", "high"}, "perf", "the plan now touches the hook's path", "sonnet"},
		{[]string{"researcher", "--model", "opus"}, "researcher", "its first run fell short", "opus"},
		{[]string{"implementer", "--effort", "low"}, "implementer", "what is left is a rename", "low"},
	}
	for _, a := range amendments {
		r := mustRun(t, append(append([]string{"staffing", "amend"}, a.args...), "--reason", a.reason)...)
		if !strings.Contains(r.stdout, a.agent) {
			t.Errorf("staffing amend %s does not say what it changed:\n%s", strings.Join(a.args, " "), r.stdout)
		}
	}

	got := stfShow(t)
	if len(got.Amendments) != len(amendments) {
		t.Fatalf("the record shows %d amendments, and %d were made: %+v", len(got.Amendments), len(amendments), got.Amendments)
	}
	for i, want := range amendments {
		a := got.Amendments[i]
		if a.Agent != want.agent || a.Reason != want.reason {
			t.Errorf("amendment %d is %s because %q, want %s because %q", i+1, a.Agent, a.Reason, want.agent, want.reason)
		}
		if a.Change == "" || !strings.Contains(a.Change, want.change) {
			t.Errorf("amendment %d does not say what changed (want it to name %q): %q", i+1, want.change, a.Change)
		}
		at, err := time.Parse(time.RFC3339, a.At)
		if err != nil || at.Before(before) || at.After(time.Now().Add(time.Minute)) {
			t.Errorf("amendment %d does not say when: %q (%v)", i+1, a.At, err)
		}
		if a.By == "person" {
			t.Errorf("amendment %d is recorded as a person's, and `staffing amend` made it", i+1)
		}
	}
	// What changed is told apart: a model raised names what it was raised from.
	if c := got.Amendments[2].Change; !strings.Contains(c, "sonnet") {
		t.Errorf("the change of the researcher's model does not say what it was before: %q", c)
	}
	for name, want := range map[string]stfAgent{
		"red-team":    {Needed: false},
		"perf":        {Needed: true, AskedModel: "sonnet", AskedEffort: "high"},
		"researcher":  {Needed: true, AskedModel: "opus", AskedEffort: "medium"},
		"implementer": {Needed: true, AskedModel: "sonnet", AskedEffort: "low"},
	} {
		a := got.agent(t, name)
		if a.Needed != want.Needed || a.AskedModel != want.AskedModel || a.AskedEffort != want.AskedEffort {
			t.Errorf("%s after its amendment: needed %v, %q at %q; want needed %v, %q at %q", name,
				a.Needed, a.AskedModel, a.AskedEffort, want.Needed, want.AskedModel, want.AskedEffort)
		}
	}
	if a := got.agent(t, "red-team"); a.Reason != amendments[0].reason {
		t.Errorf("red-team's reason is %q, want the amendment's", a.Reason)
	}

	prose := mustRun(t, "staffing", "show").stdout
	for _, a := range amendments {
		if !strings.Contains(prose, a.reason) {
			t.Errorf("staffing show does not give the amendment's reason %q:\n%s", a.reason, prose)
		}
	}
}

func TestAC6AnAmendmentThatSaysNothingOrNamesNobodyIsRefused(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	// Nothing to amend yet.
	r := stfUnchanged(t, root, "", "staffing", "amend", "perf", "--left-out", "--reason", "nothing to measure")
	stfRefused(t, r, stfCodeNone, "sdlc:project-manager")

	stfSet(t, stfFull("perf"))
	for name, args := range map[string][]string{
		"AC_6 no reason":                   {"staffing", "amend", "red-team", "--left-out"},
		"AC_6 a reason that is empty":      {"staffing", "amend", "red-team", "--left-out", "--reason", "  "},
		"AC_6 nothing to change":           {"staffing", "amend", "red-team", "--reason", "just checking"},
		"AC_6 needed and left out at once": {"staffing", "amend", "red-team", "--needed", "--left-out", "--reason", "both"},
		"AC_6 an agent nobody ships":       {"staffing", "amend", "designer", "--left-out", "--reason", "no interface"},
		"AC_6 no agent":                    {"staffing", "amend", "--left-out", "--reason", "whoever"},
	} {
		t.Run(name, func(t *testing.T) {
			r := stfUnchanged(t, root, "", args...)
			if !strings.Contains(r.stderr, "SDLC-E") || stfFixLine(r.stderr) == "" {
				t.Errorf("the refusal carries no code or no fix:\n%s", r.stderr)
			}
		})
	}
	// An agent brought in has to come with what it is asked to run at.
	r = stfUnchanged(t, root, "", "staffing", "amend", "perf", "--needed", "--reason", "the plan touches a hot path")
	if !strings.Contains(r.stderr, "SDLC-E") {
		t.Errorf("perf was brought in with no model and no effort, or the refusal has no code:\n%s", r.stderr)
	}
}

func TestAC6AGateWaitsForAReviewerBroughtInFromThenOn(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "perf", "human-advocate")
	stfThrough(t, root, model.GateCodeReview, "perf", "human-advocate")

	mustRun(t, "staffing", "amend", "perf", "--needed", "--model", "sonnet", "--effort", "medium",
		"--reason", "the implementation put a loop on the hook's path")

	// From then on: the gate already passed without perf stays passed.
	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if status.Gates["design_review"] != "pass" || status.NextGate != string(model.GateCodeReview) {
		t.Errorf("bringing perf in reopened a gate that had passed: %+v, next %q", status.Gates, status.NextGate)
	}
	stfReviewed(t, model.GateCodeReview, "perf", "human-advocate")
	r := run(t, "gate", "code_review", "pass")
	stfRefused(t, r, "SDLC-E0029", "perf")
	if strings.Contains(r.stderr, "human-advocate") {
		t.Errorf("code_review waits for the human advocate, which is still left out:\n%s", r.stderr)
	}
	mustRunWith(t, "# perf\n", "review", "add", "code_review", "perf", "approve")
	mustRun(t, "gate", "code_review", "pass", "--note", "approved")

	// The commit gate asks Gates 6 and 7 again, so a reviewer brought in after
	// code review passed is waited for there.
	mustRun(t, "staffing", "amend", "human-advocate", "--needed", "--model", "sonnet", "--effort", "low",
		"--reason", "the refusal's wording changed")
	// Asked before the commit is made: the released gate asks for the reviews
	// first and for a committed tree after, so the answer is about the review.
	r = run(t, "gate", "commit", "pass", "--note", "committed")
	if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0029") || !strings.Contains(r.stderr, "human-advocate") {
		t.Fatalf("the commit gate did not wait for a reviewer brought in after code review passed: exit %d\n%s", r.code, r.stderr)
	}
	// The review is a file in this checkout, so it is recorded before the
	// commit. Recorded after, it is uncommitted work, and the released gate
	// refuses that (SDLC-E0032) whatever the staffing says.
	mustRunWith(t, "# human-advocate\n", "review", "add", "code_review", "human-advocate", "approve")
	commitEverything(t, root)
	mustRun(t, "gate", "commit", "pass", "--note", "committed")
}

// ---------------------------------------------------------------- decision 9

func TestDecision9AnAmendmentCannotLeaveOutAReviewerWhoseLatestReviewAtAGateNotYetPassedIsABlock(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t)
	stfThrough(t, root, model.GateDesignReview)
	mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "block", "--note", "AC-3 has no step")
	mustRunWith(t, "# security\n", "review", "add", "design_review", "security", "block", "--note", "the path is not checked")

	r := stfUnchanged(t, root, "", "staffing", "amend", "architect", "--left-out", "--reason", "the design is simple after all")
	stfRefused(t, r, stfCodeBlocked, "architect")
	fix := strings.ToLower(stfFixLine(r.stderr))
	if !strings.Contains(fix, "rework") || !strings.Contains(fix, "person") {
		t.Errorf("the refusal does not say the way on is rework or a person: %s", fix)
	}
	// Security advises on a story that is not sensitive, and its block is a
	// block all the same.
	r = stfUnchanged(t, root, "", "staffing", "amend", "security", "--left-out", "--reason", "it only advises here")
	stfRefused(t, r, stfCodeBlocked, "security")

	// The plan changes, so the block is of a plan that is no longer there. It
	// is still the reviewer's latest word.
	mustRunWith(t, "# Plan\n\nOne more line.\n", "artifact", "write", "plan")
	r = stfUnchanged(t, root, "", "staffing", "amend", "architect", "--left-out", "--reason", "the plan changed")
	stfRefused(t, r, stfCodeBlocked, "architect")

	// Rework is the way on: once the architect has approved, its latest review
	// is not a block, and the staffing is the project manager's again.
	mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "approve")
	mustRun(t, "staffing", "amend", "architect", "--left-out", "--reason", "nothing left for it to read")
}

func TestDecision9ABlockAtAGateThatHasPassedDoesNotHoldTheStaffingAndOneAtAGateReopenedDoes(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t)
	stfThrough(t, root, model.GateDesignReview)
	// The human advocate and red-team advise: the gate passes over their blocks.
	stfReviewed(t, model.GateDesignReview, "human-advocate", "red-team")
	mustRunWith(t, "# human-advocate\n", "review", "add", "design_review", "human-advocate", "block", "--note", "the message is unclear")
	mustRunWith(t, "# red-team\n", "review", "add", "design_review", "red-team", "block", "--note", "the input is not bounded")
	mustRun(t, "gate", "design_review", "pass", "--note", "approved over two advisory blocks")

	mustRun(t, "staffing", "amend", "human-advocate", "--left-out", "--reason", "no message changes from here on")

	// Reopened, the gate has not passed, and red-team's block is at it.
	mustRun(t, "gate", "design_review", "fail", "--note", "the plan is reworked")
	r := stfUnchanged(t, root, "", "staffing", "amend", "red-team", "--left-out", "--reason", "it has said its piece")
	stfRefused(t, r, stfCodeBlocked, "red-team")
}

// ---------------------------------------------------------------------- AC-7

// stfReview is one line of `sdlc review list --json`, with what this story adds.
type stfReview struct {
	Gate          string `json:"gate"`
	Role          string `json:"role"`
	Blocking      bool   `json:"blocking"`
	Verdict       string `json:"verdict"`
	Stale         bool   `json:"stale"`
	LeftOut       bool   `json:"left_out"`
	Reason        string `json:"reason"`
	BroughtBackBy string `json:"brought_back_by"`
}

type stfReviewList struct {
	OK      bool        `json:"ok"`
	Reviews []stfReview `json:"reviews"`
}

func (l stfReviewList) review(gate, role string) (stfReview, bool) {
	for _, r := range l.Reviews {
		if r.Gate == gate && r.Role == role {
			return r, true
		}
	}
	return stfReview{}, false
}

func TestAC7AChangeAtCodeReviewThatTouchesAListedPathBringsTheReviewerBackAndTheOutputNamesThePath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// file is what the implementation adds; path is the listed path that
		// has to be named, or "" where nothing listed is touched.
		file, path, role string
		// committed is whether the test goes on to the commit, where the
		// gate asks code review's question again of a change that is by then
		// in a commit and no longer in the working tree.
		committed bool
	}{
		{"AC_7 a new file in a new directory under a listed directory", "internal/hook/extra.go", "internal/hook/", "security", false},
		{"AC_7 a file deeper under a listed directory", "internal/hook/rules/deep/rule.go", "internal/hook/", "security", false},
		{"AC_7 a listed file", "install.sh", "install.sh", "security", false},
		{"AC_7 another reviewer's listed path", "internal/hot/loop.go", "internal/hot/", "perf", false},
		{"AC_7 a change already committed when the commit gate asks again", "internal/hook/extra.go", "internal/hook/", "security", true},
		// T8: macOS and Windows fold case, so a listed path spelled in another
		// case is the listed path, as it is for every other path rule.
		{"AC_7 a listed directory spelled in another case", "internal/HOOK/extra.go", "internal/hook/", "security", false},
		{"AC_7 a path nobody listed", "internal/cold/extra.go", "", "security", false},
		{"AC_7 a directory whose name only starts like a listed one", "internal/hooked/extra.go", "", "security", false},
		{"AC_7 a file whose name only starts like a listed one", "install.sh.bak", "", "security", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := stfStaffed(t, "security", "perf", "human-advocate", "red-team")
			stfThrough(t, root, model.GateImplementation, "security", "perf", "human-advocate", "red-team")
			writeFile(t, root, tc.file, "// the story's change\n")
			stfThrough(t, root, model.GateCodeReview, "security", "perf", "human-advocate", "red-team")
			mustRunWith(t, "# code-reviewer\n", "review", "add", "code_review", "code-reviewer", "approve")

			listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "code_review", "--json"))
			got, _ := listed.review("code_review", tc.role)
			prose := mustRun(t, "review", "list", "--gate", "code_review").stdout
			if tc.path == "" {
				if !got.LeftOut || got.BroughtBackBy != "" {
					t.Errorf("%s was brought back by a change that touches no listed path: %+v", tc.role, got)
				}
				mustRun(t, "gate", "code_review", "pass", "--note", "approved")
				return
			}

			under := func(named string) bool { return strings.HasPrefix(strings.ToLower(named), tc.path) }
			names := func(text string) bool { return strings.Contains(strings.ToLower(text), tc.path) }
			if got.LeftOut || !under(got.BroughtBackBy) {
				t.Errorf("review list --json does not show %s brought back by %s: %+v", tc.role, tc.path, got)
			}
			if !slices.ContainsFunc(strings.Split(prose, "\n"), func(l string) bool {
				return strings.Contains(l, tc.role) && names(l) && !strings.Contains(l, "left out")
			}) {
				t.Errorf("review list does not name %s beside %s as what brought it back:\n%s", tc.path, tc.role, prose)
			}
			if a := stfShow(t).agent(t, tc.role); !under(a.BroughtBackBy) {
				t.Errorf("staffing show --json does not say what brought %s back: %+v", tc.role, a)
			}
			if _, ok := stfShow(t).atGate(tc.role); !ok {
				t.Errorf("staffing show does not name %s as needed at code_review", tc.role)
			}
			// The others the staffing leaves out stay out.
			if other, _ := listed.review("code_review", "human-advocate"); !other.LeftOut {
				t.Errorf("the human advocate came back with %s: %+v", tc.role, other)
			}

			r := run(t, "gate", "code_review", "pass")
			stfRefused(t, r, "SDLC-E0029", tc.role)
			if !names(r.stderr) {
				t.Errorf("the refusal does not name %s, the path that brought %s back:\n%s", tc.path, tc.role, r.stderr)
			}
			mustRunWith(t, "# "+tc.role+"\n", "review", "add", "code_review", tc.role, "approve")
			mustRun(t, "gate", "code_review", "pass", "--note", "approved")
			if !tc.committed {
				return
			}

			// T8: the story's commit is made once code review has passed, the
			// only point at which the released tool lets it be made (SDLC-E0046
			// holds every gate before the commit to a HEAD that has not moved).
			// The change is then in a commit and not in the working tree, and it
			// is still what brought the reviewer back.
			commitEverything(t, root)
			after := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "code_review", "--json"))
			if got, _ := after.review("code_review", tc.role); got.LeftOut || !under(got.BroughtBackBy) {
				t.Errorf("with the change committed, review list --json no longer shows %s brought back by %s: %+v", tc.role, tc.path, got)
			}
			if a := stfShow(t, "--gate", "code_review").agent(t, tc.role); !under(a.BroughtBackBy) {
				t.Errorf("with the change committed, staffing show --json no longer says what brought %s back: %+v", tc.role, a)
			}
			if other, _ := after.review("code_review", "human-advocate"); !other.LeftOut {
				t.Errorf("with the change committed, the human advocate came back with %s: %+v", tc.role, other)
			}
			mustRun(t, "gate", "commit", "pass", "--note", "committed")
		})
	}
}

// ------------------------------------------------------ AC-8 and decision 8

func TestAC8WhatAPersonAssignsIsUsedAtEveryGateAndNoStaffingOrAmendmentChangesIt(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "architect", "red-team")

	r := mustRun(t, "staffing", "assign", "architect", "--model", "opus", "--effort", "high", "--reason", "I want the design read")
	if !strings.Contains(r.stdout, "architect") {
		t.Errorf("staffing assign does not say what it set:\n%s", r.stdout)
	}
	mustRun(t, "staffing", "assign", "security", "--effort", "max", "--reason", "this one touches the hook")

	check := func(when string) {
		t.Helper()
		got := stfShow(t)
		a := got.agent(t, "architect")
		if !a.Needed || a.AskedModel != "opus" || a.AskedEffort != "high" || a.SetBy != "person" {
			t.Errorf("%s: the architect is %+v, want needed at opus and high, set by a person", when, a)
		}
		s := got.agent(t, "security")
		if !s.Needed || s.AskedModel != "sonnet" || s.AskedEffort != "max" || s.SetBy != "person" {
			t.Errorf("%s: security is %+v, want needed at sonnet and max, set by a person", when, s)
		}
		for _, gate := range []string{"design_review", "code_review"} {
			at, ok := stfShow(t, "--gate", gate).atGate("security")
			if !ok || at.AskedEffort != "max" {
				t.Errorf("%s: at %s security is not asked for at the effort a person set: %+v", when, gate, at)
			}
		}
	}
	check("after the assignment")

	got := stfShow(t)
	if n := len(got.Amendments); n < 2 || got.Amendments[n-2].By != "person" || got.Amendments[n-1].By != "person" ||
		got.Amendments[n-2].Agent != "architect" || got.Amendments[n-1].Reason != "this one touches the hook" {
		t.Errorf("the record does not show the two assignments as a person's, with why: %+v", got.Amendments)
	}
	prose := mustRun(t, "staffing", "show").stdout
	if !slices.ContainsFunc(strings.Split(prose, "\n"), func(l string) bool {
		return strings.Contains(l, "architect") && strings.Contains(l, "person")
	}) {
		t.Errorf("staffing show does not say a person chose the architect:\n%s", prose)
	}

	for _, args := range [][]string{
		{"architect", "--left-out"},
		{"architect", "--model", "sonnet"},
		{"architect", "--effort", "low"},
		{"security", "--effort", "medium"},
		{"security", "--left-out"},
	} {
		r := stfUnchanged(t, root, "", append(append([]string{"staffing", "amend"}, args...), "--reason", "to save the tokens")...)
		stfRefused(t, r, stfCodePersons, args[0], "staffing assign")
	}
	check("after the amendments that were refused")

	// The gate waits for the reviewer a person required.
	stfThrough(t, root, model.GateDesignReview)
	stfReviewed(t, model.GateDesignReview, "architect", "red-team")
	stfRefused(t, run(t, "gate", "design_review", "pass"), "SDLC-E0029", "architect")
	mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "approve")
	mustRun(t, "gate", "design_review", "pass", "--note", "approved")
	check("a gate later")
}

func TestAC8AnAssignmentMadeBeforeTheStaffingIsRecordedIsOneTheStaffingCannotChange(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	mustRun(t, "staffing", "assign", "perf", "--model", "opus", "--effort", "high", "--reason", "the budget is tight on this one")

	r := stfRunSet(t, stfFull("perf"))
	stfRefused(t, r, stfCodePersons, "perf", "staffing assign")
	r = stfRunSet(t, stfFull())
	stfRefused(t, r, stfCodePersons, "perf", "opus")
	if raw := stfRecord(t, root); strings.Contains(raw, "has work on this story") {
		t.Errorf("a staffing that was refused was recorded:\n%s", raw)
	}

	stfSet(t, stfFull().with("perf", func(a *stfAgent) { a.AskedModel, a.AskedEffort = "opus", "high" }))
	if a := stfShow(t).agent(t, "perf"); !a.Needed || a.AskedModel != "opus" || a.AskedEffort != "high" || a.SetBy != "person" {
		t.Errorf("perf is %+v, want what the person assigned, recorded as a person's", a)
	}
}

func stfRunSet(t *testing.T, doc stfDoc) result {
	t.Helper()
	return runWith(t, doc.text(t), "staffing", "set")
}

func TestAC8APersonChangesTheirOwnAssignmentWithTheSameCommand(t *testing.T) {
	t.Parallel()
	stfStaffed(t)
	mustRun(t, "staffing", "assign", "researcher", "--model", "opus", "--reason", "the first read was thin")
	mustRun(t, "staffing", "assign", "researcher", "--model", "haiku", "--effort", "low", "--reason", "it was fine after all")
	if a := stfShow(t).agent(t, "researcher"); a.AskedModel != "haiku" || a.AskedEffort != "low" || a.SetBy != "person" {
		t.Errorf("the researcher is %+v, want the person's second assignment", a)
	}
}

func TestDecision8AnAssignmentIsHeldToTheConfiguredLimitsAndCannotLeaveAnAgentOut(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "perf")
	for _, tc := range []struct {
		args []string
		code string
		says []string
	}{
		{[]string{"researcher", "--model", "fable"}, stfCodeLimits, []string{"haiku", "sonnet", "opus"}},
		{[]string{"researcher", "--effort", "extreme"}, stfCodeLimits, []string{"low", "max"}},
		{[]string{"verifier", "--model", "sonnet"}, stfCodeLimits, []string{"verifier", "opus"}},
		{[]string{"verifier", "--effort", "medium"}, stfCodeLimits, []string{"verifier", "high"}},
	} {
		r := stfUnchanged(t, root, "", append(append([]string{"staffing", "assign"}, tc.args...), "--reason", "mine to choose")...)
		stfRefused(t, r, tc.code, tc.says...)
	}
	// Leaving an agent out is not something the person's command does, and
	// neither is saying nothing, or naming nobody the plugin ships.
	for name, args := range map[string][]string{
		"decision 8 --left-out":           {"staffing", "assign", "red-team", "--left-out", "--reason", "no need"},
		"decision 8 --needed=false":       {"staffing", "assign", "red-team", "--needed=false", "--reason", "no need"},
		"decision 8 no reason":            {"staffing", "assign", "red-team", "--model", "opus"},
		"decision 8 an empty reason":      {"staffing", "assign", "red-team", "--model", "opus", "--reason", ""},
		"decision 8 an agent not shipped": {"staffing", "assign", "designer", "--model", "opus", "--reason", "mine"},
		"decision 8 no agent":             {"staffing", "assign", "--model", "opus", "--reason", "mine"},
	} {
		t.Run(name, func(t *testing.T) {
			r := stfUnchanged(t, root, "", args...)
			if !strings.Contains(r.stderr, "SDLC-E") || stfFixLine(r.stderr) == "" {
				t.Errorf("the refusal carries no code or no fix:\n%s", r.stderr)
			}
		})
	}
	if a := stfShow(t).agent(t, "red-team"); !a.Needed || a.SetBy == "person" {
		t.Errorf("red-team after the refusals: %+v", a)
	}
}

// The hook is the first barrier and refuses the command to any session. This
// is the second, in the command, for a spelling the hook cannot read: it
// refuses wherever it sees a Claude Code session id, as `sdlc parallel` does,
// the session working the story included.
func TestDecision8AssignIsRefusedWhereverTheCommandSeesAClaudeCodeSession(t *testing.T) {
	t.Parallel()
	root := stfProject(t)
	setenv(t, store.SessionEnv, "session-holding-the-story")
	mustRun(t, "start")
	mustRun(t, "gate", "dor", "pass")
	stfSet(t, stfFull("perf"))

	for _, session := range []string{"session-holding-the-story", "another-session"} {
		setenv(t, store.SessionEnv, session)
		for _, args := range [][]string{
			{"perf", "--model", "opus", "--effort", "high", "--reason", "the person said so"},
			{"security", "--effort", "max", "--reason", "x"},
			// Arguments that would be refused on their own account: the
			// session is refused first, so the error answers nothing.
			{"designer", "--model", "opus", "--reason", "x"},
			{"verifier", "--model", "fable", "--reason", "x"},
			{"perf", "--model", "opus"},
		} {
			r := stfUnchanged(t, root, "", append([]string{"staffing", "assign"}, args...)...)
			stfRefused(t, r, stfCodeSession)
			if fix := strings.ToLower(stfFixLine(r.stderr)); !strings.Contains(fix, "person") || !strings.Contains(fix, "terminal") {
				t.Errorf("the refusal does not say a person runs it in their own terminal: %s", fix)
			}
			for _, other := range []string{stfCodeLimits, stfCodeIncomplete, "SDLC-E0026", "SDLC-E0048"} {
				if strings.Contains(r.stderr, other) {
					t.Errorf("the refusal to a session carries %s, so something was read before the session was refused:\n%s", other, r.stderr)
				}
			}
		}
	}
	setenv(t, store.SessionEnv, "session-holding-the-story")
	if a := stfShow(t).agent(t, "perf"); a.Needed || a.SetBy == "person" {
		t.Errorf("a session's assignment was kept: %+v", a)
	}
	// A person, in a terminal, is not asked for anything else.
	setenv(t, store.SessionEnv, "")
	mustRun(t, "staffing", "assign", "perf", "--model", "opus", "--effort", "high", "--reason", "the person said so")
}

// That a person chose something comes from which command ran, never from a
// field another command can send.
func TestDecision8NoFieldOfAStaffingCanSayAPersonChoseIt(t *testing.T) {
	t.Parallel()
	stfBegun(t)
	var doc map[string][]map[string]any
	if err := json.Unmarshal([]byte(stfFull("perf").text(t)), &doc); err != nil {
		t.Fatal(err)
	}
	for _, a := range doc["agents"] {
		a["set_by"], a["by"], a["person"], a["assigned_by"] = "person", "person", true, "person"
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	// Refused or taken without the fields: either way nothing a person did
	// not choose is recorded as theirs.
	if r := runWith(t, string(raw), "staffing", "set"); r.code != 0 {
		if !strings.Contains(r.stderr, "SDLC-E") || stfFixLine(r.stderr) == "" {
			t.Fatalf("the refusal carries no code or no fix:\n%s", r.stderr)
		}
		stfSet(t, stfFull("perf"))
	}
	got := stfShow(t)
	for _, a := range got.Agents {
		if a.SetBy == "person" {
			t.Errorf("%s is recorded as a person's choice, from a field `staffing set` was sent", a.Agent)
		}
	}
	// An amendment by the project manager's command does not unlock it: the
	// staffing is still the project manager's to amend.
	mustRun(t, "staffing", "amend", "perf", "--needed", "--model", "sonnet", "--effort", "low", "--reason", "there is a loop")
	mustRun(t, "staffing", "amend", "perf", "--left-out", "--reason", "the loop went")
}

// ---------------------------------------------------------------------- AC-9

func TestAC9ReviewListShowsEveryReviewerTheStaffingLeavesOutWithItsReason(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "red-team", "perf")
	mustRun(t, "staffing", "amend", "human-advocate", "--left-out", "--reason", "no message a person reads changes")
	stfThrough(t, root, model.GateDesignReview)

	reasons := map[string]string{
		"red-team":       "the story gives red-team nothing to read",
		"perf":           "the story gives perf nothing to read",
		"human-advocate": "no message a person reads changes",
	}
	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--json"))
	prose := mustRun(t, "review", "list").stdout
	lines := strings.Split(prose, "\n")
	for _, r := range model.Reviewers {
		if r.WhenDoRAdvocate {
			continue
		}
		got, ok := listed.review(string(r.Gate), r.Role)
		if !ok {
			t.Errorf("review list --json has no entry for %s at %s", r.Role, r.Gate)
			continue
		}
		reason, out := reasons[r.Role]
		if got.LeftOut != out || got.Reason != reason {
			t.Errorf("%s at %s: left out %v because %q, want %v because %q", r.Role, r.Gate, got.LeftOut, got.Reason, out, reason)
		}
		line := slices.IndexFunc(lines, func(l string) bool {
			return strings.Contains(l, string(r.Gate)) && strings.Contains(l, " "+r.Role+" ")
		})
		if line < 0 {
			t.Errorf("review list has no line for %s at %s:\n%s", r.Role, r.Gate, prose)
			continue
		}
		if said := strings.Contains(lines[line], "left out"); said != out {
			t.Errorf("the line for %s at %s says left out = %v, want %v: %q", r.Role, r.Gate, said, out, lines[line])
		}
	}
	for role, reason := range reasons {
		if !strings.Contains(prose, reason) {
			t.Errorf("review list does not give the reason %s is left out (%q):\n%s", role, reason, prose)
		}
	}
	// One gate's worth, and another story's, read the same way.
	only := decode[stfReviewList](t, mustRun(t, "review", "list", "--gate", "code_review", "--story", "US-001", "--json"))
	if r, ok := only.review("code_review", "perf"); !ok || !r.LeftOut || r.Reason != reasons["perf"] {
		t.Errorf("review list --gate code_review does not show perf left out with its reason: %+v", only.Reviews)
	}
	if r, ok := only.review("code_review", "code-reviewer"); !ok || r.LeftOut || r.Reason != "" {
		t.Errorf("the code reviewer is shown as left out: %+v", r)
	}
}

// stfLogged is `sdlc log --json`, with what this story adds to an entry.
type stfLogged struct {
	Entries []struct {
		Story    string `json:"story"`
		Staffing *struct {
			Agents     []stfAgent     `json:"agents"`
			Amendments []stfAmendment `json:"amendments"`
		} `json:"staffing"`
	} `json:"entries"`
}

func TestAC9TheLogShowsTheStaffingAndEveryAmendmentToIt(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "red-team", "perf")
	mustRun(t, "staffing", "amend", "researcher", "--model", "opus", "--reason", "its first run fell short")
	mustRun(t, "staffing", "assign", "architect", "--effort", "max", "--reason", "I want the design read twice")
	mustRun(t, "staffing", "amend", "human-advocate", "--left-out", "--reason", "no message a person reads changes")
	stfThrough(t, root, "", "red-team", "perf", "human-advocate")

	got := decode[stfLogged](t, mustRun(t, "log", "--json"))
	if len(got.Entries) != 1 || got.Entries[0].Staffing == nil {
		t.Fatalf("sdlc log --json has no staffing for the story: %+v", got.Entries)
	}
	staffing := got.Entries[0].Staffing
	if len(staffing.Agents) != len(stfAgents) {
		t.Errorf("the log's staffing has %d agents, want %d", len(staffing.Agents), len(stfAgents))
	}
	for _, a := range staffing.Agents {
		switch a.Agent {
		case "red-team", "perf", "human-advocate":
			if a.Needed || a.Reason == "" {
				t.Errorf("the log shows %s as %+v, want left out with its reason", a.Agent, a)
			}
		case "researcher":
			if a.AskedModel != "opus" {
				t.Errorf("the log shows the researcher asked for at %q, want opus", a.AskedModel)
			}
		case "architect":
			if a.AskedEffort != "max" || a.SetBy != "person" {
				t.Errorf("the log shows the architect as %+v, want max, set by a person", a)
			}
		}
	}
	reasons := []string{"its first run fell short", "I want the design read twice", "no message a person reads changes"}
	if len(staffing.Amendments) != len(reasons) {
		t.Fatalf("the log shows %d amendments, want %d: %+v", len(staffing.Amendments), len(reasons), staffing.Amendments)
	}
	for i, reason := range reasons {
		a := staffing.Amendments[i]
		if a.Reason != reason || a.At == "" || a.Change == "" || a.Agent == "" {
			t.Errorf("amendment %d in the log is %+v, want what changed, when, and %q", i+1, a, reason)
		}
	}
	if staffing.Amendments[1].By != "person" || staffing.Amendments[0].By == "person" {
		t.Errorf("the log does not tell a person's assignment from an amendment: %+v", staffing.Amendments)
	}

	prose := mustRun(t, "log").stdout
	if !strings.Contains(prose, "staffing") {
		t.Errorf("sdlc log does not show the staffing:\n%s", prose)
	}
	for _, want := range append(reasons, "the story gives perf nothing to read", "red-team", "perf", "human-advocate") {
		if !strings.Contains(prose, want) {
			t.Errorf("sdlc log does not show %q:\n%s", want, prose)
		}
	}
	// The story is done: its staffing is still there to read.
	mustRun(t, "stop")
	if again := decode[stfLogged](t, mustRun(t, "log", "--json")); len(again.Entries) != 1 || again.Entries[0].Staffing == nil {
		t.Errorf("the staffing went from the log when the iteration ended: %+v", again.Entries)
	}
	if a := stfShow(t, "--story", "US-001").agent(t, "architect"); a.SetBy != "person" {
		t.Errorf("staffing show --story does not read a finished story's staffing: %+v", a)
	}
}

// T7 of the threats: a reason is written by a model, or typed by a person, and
// is printed back into a session's context and a person's terminal.
func TestAC9AReasonCannotControlTheTerminalOrPassForALineOfTheToolsOwn(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	forged := "  code_review     code-reviewer   left out"
	hostile := "nothing to read\n" + forged + "\x1b[2J\x1b]0;owned\x07\r"
	// A reason like this is either refused where it is given or made safe
	// where it is printed. Where it is refused, the story goes on with one
	// that is not hostile.
	if r := stfRunSet(t, stfFull("perf").with("red-team", func(a *stfAgent) { *a = stfAgent{Agent: "red-team", Needed: false, Reason: hostile} })); r.code != 0 {
		stfSet(t, stfFull("perf", "red-team"))
	}
	stfAnalysed(t, false)
	if r := run(t, "staffing", "amend", "human-advocate", "--left-out", "--reason", hostile); r.code != 0 {
		mustRun(t, "staffing", "amend", "human-advocate", "--left-out", "--reason", "nothing to read")
	}
	if r := run(t, "staffing", "assign", "architect", "--effort", "max", "--reason", hostile); r.code != 0 {
		mustRun(t, "staffing", "assign", "architect", "--effort", "max", "--reason", "mine to choose")
	}
	stfThrough(t, root, "", "red-team", "perf", "human-advocate")

	for _, args := range [][]string{{"staffing", "show"}, {"review", "list"}, {"log"}, {"status"}} {
		out := mustRun(t, args...).stdout
		if strings.ContainsAny(out, "\x1b\x07\r") {
			t.Errorf("sdlc %s printed a control character from a reason: %q", strings.Join(args, " "), out)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if line == forged {
				t.Errorf("sdlc %s printed a line a reason made up as one of its own:\n%s", strings.Join(args, " "), out)
			}
		}
	}
	if a := stfShow(t).agent(t, "red-team"); a.Needed || a.Reason == "" {
		t.Errorf("red-team is not left out with a reason: %+v", a)
	}
}

// --------------------------------------------------------------------- AC-10

func TestAC10TheStaffingCommandsSayTheStaffingIsOffWhereTheConfigurationDoesNotTurnItOn(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	mustRun(t, "start")
	mustRun(t, "gate", "dor", "pass")
	for _, tc := range []struct {
		stdin string
		args  []string
	}{
		{"", []string{"staffing", "show"}},
		{"", []string{"staffing", "show", "--json"}},
		{stfFull().text(t), []string{"staffing", "set"}},
		{"", []string{"staffing", "amend", "perf", "--left-out", "--reason", "nothing to measure"}},
		{"", []string{"staffing", "assign", "perf", "--model", "opus", "--reason", "mine"}},
	} {
		r := stfUnchanged(t, root, tc.stdin, tc.args...)
		if !strings.Contains(r.stderr+r.stdout, stfCodeOff) || !strings.Contains(r.stderr+r.stdout, "staffing.enabled") {
			t.Errorf("sdlc %s where the staffing is off: want %s and the setting that turns it on\n%s%s",
				strings.Join(tc.args, " "), stfCodeOff, r.stdout, r.stderr)
		}
	}
	// And nothing is asked of the story: the analysis gate passes as it did.
	stfAnalysed(t, false)
}

// A project that turns the staffing on and has stories begun before it did:
// they have no staffing, and every reviewer is waited for (T5: never nobody).
func TestAC10AStoryWithTheStaffingOnAndNoneRecordedWaitsForEveryReviewer(t *testing.T) {
	t.Parallel()
	root := startedAt(t, model.GateDesignReview)
	setConfigOnDisk(t, root, "staffing", stfSection())

	r := run(t, "gate", "design_review", "pass")
	stfRefused(t, r, "SDLC-E0029", "architect", "red-team", "security", "perf", "human-advocate")
	listed := decode[stfReviewList](t, mustRun(t, "review", "list", "--json"))
	if len(listed.Reviews) != 10 {
		t.Errorf("review list shows %d reviews, want the 10 the released tool waits for: %+v", len(listed.Reviews), listed.Reviews)
	}
	for _, review := range listed.Reviews {
		if review.LeftOut {
			t.Errorf("%s at %s is left out, and no staffing says so", review.Role, review.Gate)
		}
	}
	stfReviewed(t, model.GateDesignReview)
	mustRun(t, "gate", "design_review", "pass", "--note", "approved")
}

// A staffing section that is on and cannot be used refuses to load: it is
// never read as "anything is allowed" (T6).
func TestAC5AStaffingSectionThatIsOnAndCannotBeUsedStopsEveryCommandAndDoctorNamesIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(section map[string]any)
		// names is what the refusal has to name.
		names []string
	}{
		{"AC_5 no models allowed", func(s map[string]any) { s["models"] = []string{} }, []string{"staffing.models"}},
		{"AC_5 a lowest that is not allowed",
			func(s map[string]any) { s["lowest"] = map[string]any{"verifier": map[string]any{"model": "fable"}} },
			[]string{"staffing.lowest", "verifier", "fable"}},
		{"AC_5 a lowest for an agent the plugin does not ship",
			func(s map[string]any) { s["lowest"] = map[string]any{"designer": map[string]any{"model": "opus"}} },
			[]string{"staffing.lowest", "designer"}},
		{"AC_7 a bring_back for a reviewer that does not review at code review",
			func(s map[string]any) { s["bring_back"] = map[string]any{"architect": []string{"internal/"}} },
			[]string{"staffing.bring_back", "architect"}},
		{"AC_7 a bring_back for an agent that reviews nothing",
			func(s map[string]any) { s["bring_back"] = map[string]any{"researcher": []string{"internal/"}} },
			[]string{"staffing.bring_back", "researcher"}},
		{"AC_7 a bring_back for nobody the plugin ships",
			func(s map[string]any) { s["bring_back"] = map[string]any{"designer": []string{"ui/"}} },
			[]string{"staffing.bring_back", "designer"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			mustRun(t, "start")
			section := stfSection()
			tc.change(section)
			setConfigOnDisk(t, root, "staffing", section)

			for _, args := range [][]string{{"status"}, {"gate", "dor", "pass"}, {"staffing", "show"}, {"review", "list"}} {
				r := run(t, args...)
				if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0045") {
					t.Errorf("sdlc %s: exit %d, want SDLC-E0045\n%s", strings.Join(args, " "), r.code, r.stderr)
				}
				for _, want := range tc.names {
					if !strings.Contains(r.stderr, want) {
						t.Errorf("sdlc %s: the refusal does not name %q:\n%s", strings.Join(args, " "), want, r.stderr)
					}
				}
			}
			r := run(t, "doctor")
			if r.code == 0 {
				t.Errorf("doctor exits 0 with a staffing section sdlc cannot use:\n%s", r.stdout)
			}
			for _, want := range tc.names {
				if !strings.Contains(r.stdout, want) {
					t.Errorf("doctor does not name %q:\n%s", want, r.stdout)
				}
			}
		})
	}
}

// ------------------------------------- what the plan settled, and nothing ran

// PLAN.md, step 6: with the staffing on, `sdlc status` says whether one is
// recorded, in a line of the prose and as `staffing` in --json, which is what
// the runbook reads to know whether to delegate to the project manager.
func TestAC1StatusSaysWhetherAStaffingIsRecordedSoTheRunbookKnowsWhenToDelegate(t *testing.T) {
	t.Parallel()
	stfBegun(t)
	type status struct {
		Staffing string `json:"staffing"`
	}
	line := func() string {
		t.Helper()
		for l := range strings.SplitSeq(mustRun(t, "status").stdout, "\n") {
			if strings.Contains(l, "staffing") {
				return l
			}
		}
		t.Fatal("sdlc status has no line for the staffing in a project that turns it on")
		return ""
	}

	if got := decode[status](t, mustRun(t, "status", "--json")); got.Staffing != "none" {
		t.Errorf("status --json says staffing %q before one is recorded, want none", got.Staffing)
	}
	// None recorded is everyone, never no one (T5), and the line says so.
	if l := line(); !strings.Contains(l, "none recorded") || !strings.Contains(l, "every agent is needed") {
		t.Errorf("status does not say none is recorded and everybody is needed: %q", l)
	}

	stfSet(t, stfFull())
	if got := decode[status](t, mustRun(t, "status", "--json")); got.Staffing != "recorded" {
		t.Errorf("status --json says staffing %q once one is recorded, want recorded", got.Staffing)
	}
	if l := line(); !strings.Contains(l, "recorded") || !strings.Contains(l, "every agent is needed") || strings.Contains(l, "left out") {
		t.Errorf("status does not say a staffing that keeps everybody is recorded: %q", l)
	}

	mustRun(t, "staffing", "amend", "perf", "--left-out", "--reason", "nothing to measure")
	if l := line(); !strings.Contains(l, "left out") || !strings.Contains(l, "perf") || strings.Contains(l, "architect") {
		t.Errorf("status does not name who the staffing leaves out, and only them: %q", l)
	}
}

// A staffing is recorded for the story being worked on and for no other
// (PLAN.md, step 5): `set` names it or leaves it out.
func TestAC1AStaffingIsRecordedOnlyForTheStoryBeingWorkedOn(t *testing.T) {
	t.Parallel()
	root := stfProject(t)
	addStory(t, root)
	commitEverything(t, root)
	mustRun(t, "start", "US-001")
	mustRun(t, "gate", "dor", "pass")

	r := stfUnchanged(t, root, stfFull().text(t), "staffing", "set", "--story", "US-002")
	stfRefused(t, r, "SDLC-E0034", "US-001", "US-002")
	if fix := stfFixLine(r.stderr); !strings.Contains(fix, "--story") {
		t.Errorf("the refusal does not say what to do about --story: %s", fix)
	}
	mustRunWith(t, stfFull().text(t), "staffing", "set", "--story", "US-001")
	if got := stfShow(t); got.Story != "US-001" || len(got.Agents) != len(stfAgents) {
		t.Errorf("the staffing named for the story being worked on was not recorded: %+v", got)
	}
}

// AC-8: a person assigns for a story, and the story that most needs it is one
// that waits for them, which is no longer being worked on. They name it. A
// story nobody has started has no staffing to assign in, and one the backlog
// does not have is said to be that.
func TestAC8APersonAssignsToAStoryThatWaitsForThemByNamingIt(t *testing.T) {
	t.Parallel()
	root := stfProject(t)
	addStory(t, root)
	commitEverything(t, root)
	mustRun(t, "start", "US-001")
	mustRun(t, "gate", "dor", "pass")
	stfSet(t, stfFull("red-team"))

	assign := func(story string) []string {
		return []string{"staffing", "assign", "red-team", "--story", story, "--model", "opus", "--effort", "high", "--reason", "I want it attacked"}
	}
	r := stfUnchanged(t, root, "", assign("US-002")...)
	stfRefused(t, r, "SDLC-E0034", "US-002", "has not been started")
	if fix := stfFixLine(r.stderr); !strings.Contains(fix, "/sdlc:next") {
		t.Errorf("the refusal does not say how the story is started: %s", fix)
	}
	r = stfUnchanged(t, root, "", assign("US-404")...)
	stfRefused(t, r, "SDLC-E0009", "US-404")

	mustRun(t, "escalate", "spec_unclear", "--message", "is red-team wanted here?")
	// Nothing is being worked on now, so the story has to be named.
	r = stfUnchanged(t, root, "", "staffing", "assign", "red-team", "--model", "opus", "--effort", "high", "--reason", "I want it attacked")
	stfRefused(t, r, "SDLC-E0011")
	if out := mustRun(t, assign("US-001")...).stdout; !strings.Contains(out, "US-001") || !strings.Contains(out, "red-team") {
		t.Errorf("staffing assign --story does not say what it set, and for which story:\n%s", out)
	}
	a := stfShow(t, "--story", "US-001").agent(t, "red-team")
	if !a.Needed || a.AskedModel != "opus" || a.AskedEffort != "high" || a.SetBy != "person" {
		t.Errorf("red-team on the story that waits is %+v, want needed at opus and high, set by a person", a)
	}
}

// PLAN.md, step 5: an assignment needs both a model and an effort where the
// agent has neither yet, which is before a staffing is recorded and for an
// agent the staffing leaves out. One alone would require the agent with half
// of what a delegation is given.
func TestAC8AnAssignmentThatRequiresAnAgentSaysBothWhatModelAndWhatEffort(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	for _, attr := range [][]string{{"--model", "opus"}, {"--effort", "high"}} {
		r := stfUnchanged(t, root, "", append([]string{"staffing", "assign", "perf", "--reason", "mine"}, attr...)...)
		stfRefused(t, r, "SDLC-E0034", "perf", "--model", "--effort")
	}
	stfSet(t, stfFull("perf"))
	for _, attr := range [][]string{{"--model", "opus"}, {"--effort", "high"}} {
		r := stfUnchanged(t, root, "", append([]string{"staffing", "assign", "perf", "--reason", "mine"}, attr...)...)
		stfRefused(t, r, "SDLC-E0034", "perf", "--model", "--effort")
	}
	if a := stfShow(t).agent(t, "perf"); a.Needed || a.SetBy == "person" {
		t.Errorf("perf after the refusals: %+v", a)
	}
	// An agent that is needed already has both, and one is enough to change.
	mustRun(t, "staffing", "assign", "architect", "--effort", "max", "--reason", "mine")
	if a := stfShow(t).agent(t, "architect"); a.AskedModel != "sonnet" || a.AskedEffort != "max" || a.SetBy != "person" {
		t.Errorf("the architect is %+v, want sonnet at max, set by a person", a)
	}
}

// AC-6: an amendment records what changed, so one that changes nothing is
// refused: `--needed` for an agent that is needed, with no model or effort.
func TestAC6AnAmendmentThatOnlySaysAnAgentAlreadyNeededIsNeededIsRefused(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	stfSet(t, stfFull("perf"))
	r := stfUnchanged(t, root, "", "staffing", "amend", "architect", "--needed", "--reason", "to be sure")
	stfRefused(t, r, "SDLC-E0034", "architect", "already needed")
	if got := stfShow(t); len(got.Amendments) != 0 {
		t.Errorf("an amendment that changed nothing is on the record: %+v", got.Amendments)
	}
}

// T7 and PLAN.md, step 5: a reason is printed back to every reader after, so
// it is bounded at 1,000 characters, and the refusal says the bound.
func TestAC6AReasonLongerThanAThousandCharactersIsRefusedAndTheRefusalSaysTheLimit(t *testing.T) {
	t.Parallel()
	root := stfBegun(t)
	stfSet(t, stfFull("perf"))
	long := strings.Repeat("a", 1001)
	for _, args := range [][]string{
		{"staffing", "amend", "architect", "--model", "opus", "--reason", long},
		{"staffing", "assign", "architect", "--model", "opus", "--reason", long},
	} {
		r := stfUnchanged(t, root, "", args...)
		stfRefused(t, r, "SDLC-E0026")
		if fix := stfFixLine(r.stderr); !strings.Contains(fix, "1000") {
			t.Errorf("sdlc %s: the fix does not say how long a reason may be: %s", strings.Join(args[:3], " "), fix)
		}
	}
	// At the bound it is taken.
	mustRun(t, "staffing", "amend", "architect", "--model", "opus", "--reason", long[:1000])
	if a := stfShow(t).agent(t, "architect"); a.AskedModel != "opus" {
		t.Errorf("an amendment with a reason of 1,000 characters was not recorded: %+v", a)
	}
}

// AC-2 and AC-7 in prose: `staffing show` says, on the agent's own line, that
// a reviewer the staffing leaves out was brought back and by which path; and a
// reviewer the floor keeps in, that the staffing asked nothing for, is shown as
// needed with nothing asked for, never as asked for a model it was not.
func TestAC2ShowSaysInProseWhoWasBroughtBackAndWhoIsNeededWithNothingAskedFor(t *testing.T) {
	t.Parallel()
	root := stfStaffed(t, "security", "perf", "red-team")
	// The agent's own line is the last that starts with its name: the lines
	// above the agents say what the project's bring_back lists, by reviewer.
	line := func(agent string) string {
		t.Helper()
		found := ""
		for l := range strings.SplitSeq(mustRun(t, "staffing", "show").stdout, "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), agent+" ") {
				found = l
			}
		}
		if found == "" {
			t.Fatalf("staffing show has no line for %s", agent)
		}
		return found
	}

	// The floor: recorded as sensitive, security takes part whatever the
	// staffing says (AC-4), and the staffing asked nothing for it.
	mustRun(t, "gate", "analysis", "pass", "--security-sensitive")
	if l := line("security"); strings.Contains(l, "left out") || !strings.Contains(l, "needed") || !strings.Contains(l, "nothing asked for") {
		t.Errorf("security on a sensitive story, left out by the staffing, is shown as: %q", l)
	}
	if a := stfShow(t).agent(t, "security"); !a.Needed || a.AskedModel != "" || a.AskedEffort != "" {
		t.Errorf("staffing show --json has security as %+v, want needed with nothing asked for", a)
	}
	mustRun(t, "gate", "analysis", "pass", "--security-sensitive=false")
	if l := line("security"); !strings.Contains(l, "left out") {
		t.Errorf("security on a story recorded as not sensitive again is shown as: %q", l)
	}

	stfThrough(t, root, model.GateImplementation, "security", "perf", "red-team")
	writeFile(t, root, "internal/hot/loop.go", "// the story's change\n")
	stfThrough(t, root, model.GateCodeReview, "security", "perf", "red-team")
	if l := line("perf"); !strings.Contains(l, "brought back") || !strings.Contains(l, "internal/hot/") {
		t.Errorf("staffing show does not say on perf's line what brought it back: %q", l)
	}
	if l := line("red-team"); strings.Contains(l, "brought back") || !strings.Contains(l, "left out") {
		t.Errorf("red-team, which no path brings back, is shown as: %q", l)
	}
}

// --------------------------------------------------------------------- AC-12

func TestAC12TheStaffingCoversExactlyTheAgentsThePluginShips(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(filepath.Join("..", "..", "plugin", "agents"))
	if err != nil {
		t.Fatal(err)
	}
	var shipped []string
	for _, e := range entries {
		shipped = append(shipped, strings.TrimSuffix(e.Name(), ".md"))
	}
	slices.Sort(shipped)
	if !slices.Equal(shipped, stfAgents) {
		t.Fatalf("plugin/agents ships %q, and a staffing names %q", shipped, stfAgents)
	}
	// The binary's own list is the same one: a staffing of exactly these is
	// taken, and TestAC1AStaffingThatDoesNotCoverEveryAgent... has it refuse
	// one fewer and one more.
	stfBegun(t)
	stfSet(t, stfFull())
}
