// US-109 in a sandbox: a story worked under each thing a person can say in the team
// section, by the built binary, with every call of the session under test put to the hook
// first as the agent making it. internal/cli has each criterion's edges and
// internal/shellpolicy every spelling; this is the two barriers agreeing, and the walk
// from a project as `sdlc init` left it to trunk.

package e2e

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// teamAgents is a team section of the agents' entries given.
func teamAgents(entries map[string]map[string]any) map[string]any {
	agents := map[string]any{}
	for name, entry := range entries {
		agents[name] = entry
	}
	return map[string]any{"agents": agents}
}

// staffingWithCopies is staffingDocument with a needed row for each copy, at another model.
func staffingWithCopies(t *testing.T, leftOut []string, copies ...string) string {
	t.Helper()
	var doc struct {
		Agents []map[string]any `json:"agents"`
	}
	if err := json.Unmarshal([]byte(staffingDocument(t, leftOut...)), &doc); err != nil {
		t.Fatal(err)
	}
	for _, name := range copies {
		doc.Agents = append(doc.Agents, map[string]any{
			"agent": name, "needed": true, "reason": name + " reads it a second time",
			"asked_model": "opus", "asked_effort": "high", "asked_reason": "a second reader on another model",
		})
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// analysedBy starts the story, finds it ready, stores the analysis as the agent given, and
// records the staffing document through the project manager, as the runbook does. The
// analysis gate is left to the test.
func (sb sandbox) analysedBy(t *testing.T, researcher, document string) {
	t.Helper()
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	sb.works(t, tree, "", "", "gate", "dor", "pass", "--note", "the criteria are testable")
	sb.works(t, tree, researcher, sandboxAnalysis, "artifact", "write", "analysis")
	sb.works(t, tree, researcher, sandboxThreats, "artifact", "write", "threats")
	sb.works(t, tree, "project-manager", document, "staffing", "set")
}

// landed stops the story, which has to land it on trunk.
func (sb sandbox) landed(t *testing.T) {
	t.Helper()
	sb.works(t, sb.tree(), "", "", "stop")
	if got := storiesOf(t, sb.repo); got["US-001"] != "done" {
		t.Fatalf("the story is %q at the end of the run, want done", got["US-001"])
	}
}

// AC-2: no team section behaves as every default, and a file `sdlc init` wrote at an
// earlier release keeps working as it stands (D-1). The project manager is on, so the
// analysis waits for it, and that is all an upgrade asks of a story.
func TestAC2AProjectWithNoTeamSectionWorksAStoryToTrunk(t *testing.T) {
	parallel(t)
	for name, leftover := range map[string]bool{"AC-2 no team section": false, "AC-12 and the leftover of the release before": true} {
		t.Run(name, func(t *testing.T) {
			parallel(t)
			sb := newSandbox(t)
			path := filepath.Join(sb.repo, ".sdlc", "config.json")
			var cfg map[string]any
			readJSON(t, path, &cfg)
			delete(cfg, "reviews")
			if leftover {
				cfg["reviews"] = map[string]any{"gate7_advisory": false}
			}
			writeJSON(t, path, cfg)
			sb.setsTheTeam(t, nil)

			tree := sb.tree()
			doctor := sb.does(t, sb.repo, "", "", "doctor").stdout
			if said := strings.Contains(doctor, "team.agents.code-reviewer.can_block"); said != leftover {
				t.Errorf("AC-12 doctor speaks of the leftover = %v, want %v:\n%s", said, leftover, doctor)
			}
			if strings.Contains(doctor, "does not know") {
				t.Errorf("AC-2 doctor calls a file `sdlc init` wrote one with settings it does not know:\n%s", doctor)
			}
			sb.works(t, sb.repo, "", "", "start")
			sb.works(t, tree, "", "", "gate", "dor", "pass", "--note", "the criteria are testable")
			sb.works(t, tree, "researcher", sandboxAnalysis, "artifact", "write", "analysis")
			sb.works(t, tree, "researcher", sandboxThreats, "artifact", "write", "threats")
			if got := sb.statusStaffing(t); got != "none" {
				t.Errorf("AC-3 status --json says staffing %q, want none", got)
			}
			r := sb.does(t, tree, "", "", "gate", "analysis", "pass", "--security-sensitive=false")
			if r.code != 1 || !strings.Contains(r.stderr, "SDLC-E0068") {
				t.Fatalf("AC-3 the analysis passed with no staffing: exit %d\n%s%s", r.code, r.stdout, r.stderr)
			}
			sb.worksEveryGate(t)
			sb.landed(t)
		})
	}
}

// AC-12: an old key stops every command that opens the project. T7: the hook goes on
// answering, a person's commands stay refused, and an ordinary one goes through.
func TestAC12AnOldKeyStopsTheCommandsAndTheHookGoesOnAnswering(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	sb.works(t, tree, "", "", "gate", "dor", "pass", "--note", "the criteria are testable")
	setConfig(t, tree, "staffing", "enabled", true)

	for _, args := range [][]string{{"status"}, {"review", "list"}, {"gate", "analysis", "pass"}} {
		stdout, stderr, code := sb.outcome(t, sandboxSession, tree, "", args...)
		if code != 1 || stdout != "" || !strings.Contains(stderr, "SDLC-E0087") || !strings.Contains(stderr, "team.agents.project-manager") {
			t.Errorf("sdlc %s: exit %d\nstdout %q\nstderr %q\nwant SDLC-E0087 naming the key that replaced it", strings.Join(args, " "), code, stdout, stderr)
		}
	}
	stdout, _, code := sb.outcome(t, sandboxSession, tree, "", "doctor")
	if code != 1 || !strings.Contains(stdout, "SDLC-E0087") {
		t.Errorf("sdlc doctor: exit %d\n%s\nwant the refusal in its configuration check", code, stdout)
	}
	sb.allows(t, tree, "", "Bash", map[string]any{"command": "go test ./..."})
	sb.refuses(t, sandboxSession, tree, map[string]any{"command": "sdlc staffing assign security --reason x"}, "assignment-is-a-human-decision")
	sb.refuses(t, sandboxSession, tree, map[string]any{"command": "git commit -m early"}, "commit-gate")
}

// AC-4 and AC-5: a reviewer and a worker disabled, the story worked to trunk with neither,
// and the gate that was the worker's done by hand. Nothing passes for being absent.
func TestAC4AC5AStoryIsWorkedToTrunkWithAReviewerAndAWorkerDisabled(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.setsTheTeam(t, teamAgents(map[string]map[string]any{"red-team": {"enabled": false}, "researcher": {"enabled": false}}))
	tree := sb.tree()
	disabled := []string{"red-team", "researcher"}

	names := func(t *testing.T, where, out string) {
		t.Helper()
		for _, agent := range disabled {
			if !slices.ContainsFunc(strings.Split(out, "\n"), func(l string) bool {
				return strings.Contains(l, agent) && strings.Contains(l, "disabled")
			}) {
				t.Errorf("AC-4 %s does not name %s as disabled:\n%s", where, agent, out)
			}
		}
	}
	sb.works(t, sb.repo, "", "", "start")
	dor := sb.does(t, tree, "", "", "gate", "dor", "pass", "--note", "the criteria are testable")
	names(t, "the first gate", dor.stdout+dor.stderr)
	for _, want := range []string{"by hand", "researcher", "sdlc gate analysis"} {
		if !strings.Contains(dor.stdout+dor.stderr, want) {
			t.Errorf("AC-5 the gate before the researcher's does not say %q:\n%s%s", want, dor.stdout, dor.stderr)
		}
	}
	names(t, "sdlc status", sb.works(t, tree, "", "", "status"))
	names(t, "sdlc doctor", sb.does(t, tree, "", "", "doctor").stdout)

	// The staffing picks from the enabled agents.
	asked := sb.does(t, tree, "project-manager", staffingDocument(t), "staffing", "set")
	if asked.code != 1 || !strings.Contains(asked.stderr, "SDLC-E0069") || !strings.Contains(asked.stderr, "disabled") {
		t.Errorf("AC-3 a staffing that asks for a disabled agent: exit %d\n%s", asked.code, asked.stderr)
	}
	sb.works(t, tree, "project-manager", staffingDocument(t, disabled...), "staffing", "set")

	var shown struct {
		Disabled []string `json:"disabled"`
		ByHand   string   `json:"by_hand"`
		AtGate   []struct {
			Agent string `json:"agent"`
		} `json:"at_gate"`
	}
	raw := sb.works(t, tree, "", "", "staffing", "show", "--json")
	if err := json.Unmarshal([]byte(raw), &shown); err != nil {
		t.Fatal(err)
	}
	if shown.ByHand != "researcher" || len(shown.AtGate) != 0 || !slices.Contains(shown.Disabled, "red-team") {
		t.Errorf("AC-5 staffing show --json at the researcher's gate: %s", raw)
	}

	// Nothing passes for the researcher being off, and a person records it by hand.
	if r := sb.does(t, tree, "", "", "gate", "analysis", "pass", "--security-sensitive=false"); r.code != 1 || !strings.Contains(r.stderr, "SDLC-E0020") {
		t.Errorf("AC-5 the analysis passed with no documents: exit %d\n%s", r.code, r.stderr)
	}
	sb.works(t, tree, "", sandboxAnalysis, "artifact", "write", "analysis")
	sb.works(t, tree, "", sandboxThreats, "artifact", "write", "threats")
	names(t, "the analysis gate", sb.works(t, tree, "", "", "gate", "analysis", "pass", "--security-sensitive=false"))

	sb.staffedGate(t, model.GateTestsFrozen)
	sb.staffedGate(t, model.GatePlan)
	refused := sb.does(t, tree, "red-team", "# red-team\n", "review", "add", "design_review", "red-team", "block", "--important", "1", "--finding", "important the one finding")
	if refused.code != 1 || !strings.Contains(refused.stderr, "SDLC-E0027") {
		t.Errorf("AC-4 the disabled red team recorded a block: exit %d\n%s%s", refused.code, refused.stdout, refused.stderr)
	}
	sb.staffedGate(t, model.GateDesignReview, "red-team")
	sb.writesFile(t, "implementer", "invoice.go", sandboxCode)
	sb.staffedGate(t, model.GateImplementation)
	sb.staffedGate(t, model.GateVerification)
	sb.staffedGate(t, model.GateVerifierReview)
	sb.staffedGate(t, model.GateCodeReview)
	sb.staffedGate(t, model.GateCommit)
	sb.staffedGate(t, model.GateRetro)
	sb.landed(t)
}

// AC-6: a reviewer made advisory is still asked, its block no longer stops the gate, and
// `sdlc review list` says which it is.
func TestAC6AnAdvisoryArchitectsBlockDoesNotStopTheGate(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.setsTheTeam(t, teamAgents(map[string]map[string]any{"architect": {"can_block": false}, "security": {"can_block": false}}))
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	for gate := sb.nextGate(t); gate != string(model.GateDesignReview); gate = sb.nextGate(t) {
		sb.worksGate(t, model.Gate(gate))
	}
	list := sb.works(t, tree, "", "", "review", "list", "--gate", "design_review")
	if !slices.ContainsFunc(strings.Split(list, "\n"), func(l string) bool {
		return strings.Contains(l, " architect ") && strings.Contains(l, "advisory")
	}) {
		t.Errorf("AC-6 review list does not show the architect as advisory:\n%s", list)
	}
	if r := sb.does(t, tree, "", "", "gate", "design_review", "pass"); r.code != 1 || !strings.Contains(r.stderr, "architect") {
		t.Errorf("AC-6 the gate did not wait for an advisory review: exit %d\n%s", r.code, r.stderr)
	}
	sb.works(t, tree, "architect", "# architect\n", "review", "add", "design_review", "architect", "block", "--important", "1", "--finding", "important the one finding", "--note", "I would not")
	for _, r := range model.ReviewersFor(model.GateDesignReview) {
		if r.Role != "architect" {
			sb.works(t, tree, r.Role, "# "+r.Role+"\n", "review", "add", "design_review", r.Role, "approve")
		}
	}
	out := sb.works(t, tree, "", "", "gate", "design_review", "pass", "--note", "the block is on the record")
	if !strings.Contains(out, "security") || !strings.Contains(out, "can_block") {
		t.Errorf("AC-6 the gate's output does not say security cannot block:\n%s", out)
	}
	// US-117: an advisory block's finding still holds the verification gate until
	// someone decides it.
	sb.works(t, tree, "implementer", "", "finding", "close", "design_review", "architect", "F-1",
		"--declined", "--reason", "the architect is advisory here")
	sb.worksEveryGate(t)
	sb.landed(t)
}

// AC-8: two copies of a reviewer, each staffed with its own model, each recording its own
// review under its own name, and T1: nobody but the reviewer records it. The commit is
// held to a copy's stale review, which today nothing reads.
func TestAC8TwoCopiesOfAReviewerAreEachWaitedFor(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.setsTheTeam(t, teamAgents(map[string]map[string]any{"code-reviewer": {"instances": 2}}))
	tree := sb.tree()
	sb.analysedBy(t, "researcher", staffingWithCopies(t, nil, "code-reviewer-2"))
	sb.works(t, tree, "", "", "gate", "analysis", "pass", "--security-sensitive=false")

	for _, g := range []model.Gate{model.GateTestsFrozen, model.GatePlan, model.GateDesignReview} {
		sb.staffedGate(t, g)
	}
	sb.writesFile(t, "implementer", "invoice.go", sandboxCode)
	sb.staffedGate(t, model.GateImplementation)
	sb.staffedGate(t, model.GateVerification)
	sb.staffedGate(t, model.GateVerifierReview)

	got := sb.works(t, tree, "", "", "staffing", "show", "--gate", "code_review", "--json")
	var shown staffed
	if err := json.Unmarshal([]byte(got), &shown); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(shown.AtGate, func(a struct {
		Agent       string `json:"agent"`
		AskedModel  string `json:"asked_model"`
		AskedEffort string `json:"asked_effort"`
	}) bool {
		return a.Agent == "code-reviewer-2" && a.AskedModel == "opus" && a.AskedEffort == "high"
	}) {
		t.Errorf("AC-8 at_gate has no copy with what it was asked for: %s", got)
	}

	const copyReview = "sdlc review add code_review code-reviewer-2 approve"
	for _, agent := range []string{"", "implementer", "security", "code-reviewer-2"} {
		sb.refusesAs(t, agent, copyReview, "review-is-recorded-by-its-reviewer")
	}
	for _, r := range model.ReviewersFor(model.GateCodeReview) {
		sb.works(t, tree, r.Role, "# "+r.Role+"\n", "review", "add", "code_review", r.Role, "approve")
	}
	r := sb.does(t, tree, "", "", "gate", "code_review", "pass")
	if r.code != 1 || !strings.Contains(r.stderr, "SDLC-E0029") || !strings.Contains(r.stderr, "code-reviewer-2") {
		t.Errorf("AC-8 the gate does not wait for the copy: exit %d\n%s", r.code, r.stderr)
	}
	beyond := sb.does(t, tree, "code-reviewer", "# x\n", "review", "add", "code_review", "code-reviewer-3", "approve")
	if beyond.code != 1 || !strings.Contains(beyond.stderr, "SDLC-E0027") {
		t.Errorf("AC-8 a copy beyond instances was taken: exit %d\n%s%s", beyond.code, beyond.stdout, beyond.stderr)
	}
	sb.works(t, tree, "code-reviewer", "# the second reader\n", "review", "add", "code_review", "code-reviewer-2", "approve")
	if list := sb.works(t, tree, "", "", "review", "list", "--gate", "code_review"); !strings.Contains(list, "code-reviewer-2") {
		t.Errorf("AC-8 review list has no row for the copy:\n%s", list)
	}
	sb.works(t, tree, "", "", "gate", "code_review", "pass", "--note", "both read it")

	// The tree moves after the reviews: the hook holds the commit to the copy's by name.
	path := filepath.Join(tree, "invoice.go")
	reviewed := readFile(t, path)
	writeFile(t, path, reviewed+"\n// changed after the review\n")
	stdout, stderr, code := sb.hook(t, "PreToolUse", toolCall(sandboxSession, tree, "", "Bash", map[string]any{"command": "git commit -m 'feat: US-001'"}))
	if code != 0 || stdout == carryOn {
		t.Fatalf("AC-8 the hook let a commit of work changed after its review through: exit %d\n%s%s", code, stdout, stderr)
	}
	var refused refusal
	oneLine(t, stdout, &refused)
	if reason := refused.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(reason, "code-reviewer-2 (code_review)") {
		t.Errorf("AC-8 the refusal does not name the copy's stale review:\n%s", reason)
	}
	writeFile(t, path, reviewed)

	sb.staffedGate(t, model.GateCommit)
	sb.staffedGate(t, model.GateRetro)
	sb.landed(t)
}

// AC-9: a run failing past team.retries hands the story to a person, naming the agent, the
// gate and the failures. A failure below it moves nothing, and the project manager cannot
// report one: the runbook reports the project manager's.
func TestAC9ARunFailingPastRetriesIsHandedToAPerson(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.setsTheTeam(t, map[string]any{"retries": 1})
	tree := sb.tree()
	sb.works(t, sb.repo, "", "", "start")
	sb.works(t, tree, "", "", "gate", "dor", "pass", "--note", "the criteria are testable")

	const report = `sdlc team failure researcher --reason "it came back empty"`
	sb.refusesAs(t, "project-manager", report, "project-manager-only-staffs")
	sb.allows(t, tree, "", "Bash", map[string]any{"command": report})

	var first struct {
		OK         bool   `json:"ok"`
		Story      string `json:"story"`
		Agent      string `json:"agent"`
		Gate       string `json:"gate"`
		Failures   int    `json:"failures"`
		HandedOver string `json:"handed_over"`
	}
	raw := sb.works(t, tree, "", "", "team", "failure", "researcher", "--reason", "it timed out", "--json")
	if err := json.Unmarshal([]byte(raw), &first); err != nil {
		t.Fatal(err)
	}
	if !first.OK || first.Story != "US-001" || first.Agent != "researcher" || first.Gate != "analysis" || first.Failures != 1 || first.HandedOver != "" {
		t.Errorf("AC-9 the first failure: %s", raw)
	}
	if gate := sb.nextGate(t); gate != string(model.GateAnalysis) {
		t.Errorf("AC-9 a failure moved the next gate to %q", gate)
	}

	out := sb.works(t, tree, "", "", "team", "failure", "researcher", "--reason", "it came back empty")
	for _, want := range []string{"researcher", "analysis", "it timed out", "it came back empty", "sdlc approve US-001"} {
		if !strings.Contains(out, want) {
			t.Errorf("AC-9 the hand-over does not say %q:\n%s", want, out)
		}
	}
	if got := storiesOf(t, sb.repo); got["US-001"] != "awaiting_human" {
		t.Fatalf("AC-9 the story is %q after the hand-over, want awaiting_human", got["US-001"])
	}
	if r := sb.person(t, tree, "approve", "US-001"); r.code != 0 {
		t.Fatalf("approve, by the harness: exit %d\n%s", r.code, r.stderr)
	}
	sb.works(t, tree, "", "", "start")
	// Nothing passed on the failures, and the same agent has its retries again.
	sb.works(t, tree, "", "", "team", "failure", "researcher", "--reason", "once more")
	if r := sb.does(t, tree, "", "", "gate", "analysis", "pass", "--security-sensitive=false"); r.code != 1 {
		t.Errorf("AC-9 the analysis passed on the researcher's failing: exit %d\n%s", r.code, r.stdout)
	}
	sb.worksEveryGate(t)
	sb.landed(t)
}

// T5 through the built binary, in a tier that waits: a person's "the agent failed, carry on"
// with the work committed does not pass the commit gate, for either kind the story adds, and
// the answer to the pre-commit question itself does. Committed before the gate is asked, since
// with no commit the walk reaches SDLC-E0032 whether or not the closure is there (red team V1).
func TestAC9AnApprovedFailureIsNotThePreCommitAnswerInASandbox(t *testing.T) {
	parallel(t)
	binary := build(t)
	for _, kind := range []string{"agent_failed", "work_by_hand"} {
		t.Run("AC_9 "+kind, func(t *testing.T) {
			parallel(t)
			main := ccRepository(t, binary, "high")
			s := &ccStory{ontoStory: ontoBegun(t, binary, main, sessionFirst, "US-001"), main: main, cut: landTrunk(t, main)}
			for gate := s.nextGate(); gate != string(model.GateCommit); gate = s.nextGate() {
				switch model.Gate(gate) {
				case model.GateDoR, model.GateDesignReview, model.GateVerifierReview, model.GateCodeReview, model.GateCommit, model.GateRetro:
				case model.GateAnalysis:
					s.stores("researcher", "analysis", "# Analysis\n\nAC-1 is testable.\n")
					s.stores("researcher", "threats", "# Threats\n\nNothing crosses a trust boundary.\n")
				case model.GateTestsFrozen:
					s.writes("sdet", s.test, "package main\n\n// AC-1 of "+s.story+"\n")
					s.stores("sdet", "test_plan", "# Test plan\n\nAC-1 -> TestRejectsZero\n")
					s.run("sdet", "freeze")
				case model.GatePlan:
					s.stores("implementer", "plan", "# Plan\n\nOne step.\n")
				case model.GateImplementation:
					s.writes("implementer", s.source, "package main\n\n// "+s.story+"\n")
				case model.GateVerification:
					s.stores("verifier", "verification", "# Verification\n\nEvery command green.\n")
				}
				for _, r := range model.ReviewersFor(model.Gate(gate)) {
					if !r.WhenDoRAdvocate {
						s.here().runWith(t, "# "+r.Role+"\n", "review", "add", gate, r.Role, "approve")
					}
				}
				s.run("", "gate", gate, "pass", "--note", "worked by the session")
			}
			answered := func(asked string) {
				t.Helper()
				s.run("", "escalate", asked, "--message", "the bookkeeper run failed, carry on?")
				s.here().as("").run(t, "approve", s.story)
				s.here().run(t, "start", "--json")
			}
			answered(kind)
			// Past the hook, which asks the same question and is held in internal/hook: this
			// row is the gate's.
			git(t, s.tree, "add", "--", s.source, s.test)
			git(t, s.tree, "-c", "user.email=t@example.com", "-c", "user.name=Test", "commit", "--quiet", "-m", "feat: the work")
			out, code := s.here().try(t, "gate", "commit", "pass", "--note", "on the failure's answer")
			refusedWith(t, out, code != 0, "SDLC-E0037")

			answered("pre_commit_approval")
			s.run("", "gate", "commit", "pass", "--note", "a person answered the question")
		})
	}
}

// AC-3: with the project manager disabled there is no staffing, every enabled agent takes
// part, and the staffing commands say why rather than stop the runbook reading at_gate.
func TestAC3WithTheProjectManagerDisabledEveryEnabledAgentWorksAStoryToTrunk(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	sb.setsTheTeam(t, teamAgents(map[string]map[string]any{"project-manager": {"enabled": false}}))
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")

	prose := strings.ToLower(sb.works(t, tree, "", "", "staffing", "show"))
	if !strings.Contains(prose, "no staffing") || !strings.Contains(prose, "project-manager") {
		t.Errorf("AC-3 staffing show does not say there is no staffing because the project manager is off:\n%s", prose)
	}
	var shown struct {
		AtGate []map[string]any `json:"at_gate"`
		Agents []map[string]any `json:"agents"`
	}
	raw := sb.works(t, tree, "", "", "staffing", "show", "--gate", "design_review", "--json")
	if err := json.Unmarshal([]byte(raw), &shown); err != nil {
		t.Fatal(err)
	}
	if len(shown.Agents) != 0 || len(shown.AtGate) != 5 {
		t.Errorf("AC-3 staffing show --json with no staffing, at a gate with five reviewers: %s", raw)
	}
	if got := sb.statusStaffing(t); got != "" {
		t.Errorf("AC-3 status --json says staffing %q, and the runbook would delegate on none", got)
	}
	for _, args := range [][]string{{"staffing", "set"}, {"staffing", "amend", "perf", "--left-out", "--reason", "x"}} {
		r := sb.does(t, tree, "project-manager", staffingDocument(t), args...)
		if r.code != 1 || !strings.Contains(r.stderr, "SDLC-E0067") || !strings.Contains(r.stderr, "team.agents.project-manager") {
			t.Errorf("AC-3 sdlc %s: exit %d\n%s\nwant SDLC-E0067 naming the setting", strings.Join(args, " "), r.code, r.stderr)
		}
	}

	sb.worksEveryGate(t)
	for _, args := range [][]string{{"review", "list"}, {"review", "list", "--json"}, {"log"}} {
		if out := sb.works(t, tree, "", "", args...); strings.Contains(out, "left out") || strings.Contains(out, "left_out") {
			t.Errorf("AC-3 sdlc %s says someone is left out, with no staffing:\n%s", strings.Join(args, " "), out)
		}
	}
	record := readFile(t, filepath.Join(main, ".sdlc", "stories", "US-001", "gate-record.json"))
	if strings.Contains(record, `"staffing"`) {
		t.Errorf("AC-3 the record of a story with the project manager disabled holds a staffing:\n%s", record)
	}
	sb.landed(t)
}

// Red team R13 (C8): the upgrade route for a story in flight, run and not only read. A
// worktree reads its own file and a story cannot carry the edit, so the route is to commit it
// on trunk and rebase the story's branch. Before the commit gate the story's work is not
// committed, so the rebase has to keep it, and HEAD then moves off where the story started,
// which every gate before the commit refuses (SDLC-E0046) until that code's own route is
// followed. The old key reaches the story's branch the same way, by a commit on trunk and a
// rebase, since the binary under test refuses to start a story over it.
func TestAC12TheUpgradeRouteCarriesAStoryInFlightToItsNextGate(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	tree := sb.tree()
	trunk := sb.mustGit(t, sb.repo, "branch", "--show-current")
	trunkConfig := filepath.Join(sb.repo, ".sdlc", "config.json")
	committedOnTrunk := func(t *testing.T, message string) {
		t.Helper()
		sb.mustGit(t, sb.repo, "commit", "--quiet", "-m", message, "--", ".sdlc/config.json")
		sb.mustGit(t, tree, "rebase", "--quiet", "--autostash", trunk)
	}
	sb.works(t, sb.repo, "", "", "start")
	sb.worksGate(t, model.GateDoR)
	sb.worksGate(t, model.GateAnalysis)
	sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)

	setConfig(t, sb.repo, "staffing", "enabled", true)
	committedOnTrunk(t, "chore: the configuration an earlier release wrote")
	if _, stderr, code := sb.outcome(t, sandboxSession, tree, "", "status"); code == 0 || !strings.Contains(stderr, "SDLC-E0087") {
		t.Fatalf("AC-12 over an old key, the story's commands were not refused with SDLC-E0087: exit %d\n%s", code, stderr)
	}

	// The person's edit, on trunk, and the story's branch rebased onto it.
	var cfg map[string]any
	readJSON(t, trunkConfig, &cfg)
	delete(cfg, "staffing")
	writeJSON(t, trunkConfig, cfg)
	committedOnTrunk(t, "chore: the team section replaces staffing")
	if got := readFile(t, filepath.Join(tree, "invoice_test.go")); got != sandboxTest {
		t.Fatalf("AC-12 the rebase lost the story's uncommitted work: %q", got)
	}
	// HEAD has moved, so SDLC-E0046's route: the story handed to a person, who answers, and
	// started again from where trunk is.
	sb.works(t, tree, "", "", "escalate", "loop_stalled", "--message", "trunk moved under the story for the upgrade: carry on from it?")
	if r := sb.person(t, tree, "approve", "US-001"); r.code != 0 {
		t.Fatalf("approve exited %d:\n%s%s", r.code, r.stdout, r.stderr)
	}
	sb.works(t, sb.repo, "", "", "start", "US-001")
	sb.worksGate(t, model.GateTestsFrozen)
	if got := sb.nextGate(t); got != string(model.GatePlan) {
		t.Errorf("AC-12 after the upgrade route the next gate is %q, want plan", got)
	}
}
