// US-026 in a sandbox: one story worked with the staffing on, by the built
// binary, with every call of the session under test put to the hook first as
// the agent making it. internal/cli has each criterion's edges and
// internal/shellpolicy every spelling; this is the two barriers agreeing, and
// the one thing neither can show alone: that `git commit` is not held to the
// stale review of a reviewer the staffing has since left out (AC-3).
//
// The sandbox is a project `sdlc init` set up, which has no staffing section.
// The test turns it on there, as a person would, and commits it. Nothing reads
// this repository's own configuration (decision 11).

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// staffedAgents is every agent the plugin ships once the project manager is
// one of them.
var staffedAgents = []string{
	"architect", "bookkeeper", "code-reviewer", "human-advocate", "implementer", "perf",
	"project-manager", "red-team", "researcher", "sdet", "security", "verifier",
}

// staffingDocument is what the project manager gives `sdlc staffing set`:
// everybody needed at sonnet and medium, the verifier at opus and high, and
// the ones named left out.
func staffingDocument(t *testing.T, leftOut ...string) string {
	t.Helper()
	type agent struct {
		Agent       string `json:"agent"`
		Needed      bool   `json:"needed"`
		Reason      string `json:"reason"`
		AskedModel  string `json:"asked_model,omitempty"`
		AskedEffort string `json:"asked_effort,omitempty"`
		AskedReason string `json:"asked_reason,omitempty"`
	}
	var agents []agent
	for _, name := range staffedAgents {
		a := agent{Agent: name, Needed: true, Reason: name + " has work on this story",
			AskedModel: "sonnet", AskedEffort: "medium", AskedReason: "a small change that a later gate checks"}
		if name == "verifier" {
			a.AskedModel, a.AskedEffort, a.AskedReason = "opus", "high", "nothing checks the checker"
		}
		if slices.Contains(leftOut, name) {
			a = agent{Agent: name, Needed: false, Reason: "the story gives " + name + " nothing to read"}
		}
		agents = append(agents, a)
	}
	raw, err := json.Marshal(map[string]any{"agents": agents})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// staffed is `sdlc staffing show --json`, as far as this test reads it.
type staffed struct {
	OK     bool   `json:"ok"`
	Gate   string `json:"gate"`
	Agents []struct {
		Agent         string `json:"agent"`
		Needed        bool   `json:"needed"`
		Reason        string `json:"reason"`
		AskedModel    string `json:"asked_model"`
		AskedEffort   string `json:"asked_effort"`
		SetBy         string `json:"set_by"`
		BroughtBackBy string `json:"brought_back_by"`
	} `json:"agents"`
	AtGate []struct {
		Agent       string `json:"agent"`
		AskedModel  string `json:"asked_model"`
		AskedEffort string `json:"asked_effort"`
	} `json:"at_gate"`
	Amendments []struct {
		At     string `json:"at"`
		Agent  string `json:"agent"`
		Change string `json:"change"`
		Reason string `json:"reason"`
		By     string `json:"by"`
	} `json:"amendments"`
}

// turnsTheStaffingOn is the person's step: the section written into the
// sandbox's configuration and committed to trunk, before any story starts.
func (sb sandbox) turnsTheStaffingOn(t *testing.T) {
	t.Helper()
	path := filepath.Join(sb.repo, ".sdlc", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["staffing"] = map[string]any{
		"enabled": true,
		"models":  []string{"haiku", "sonnet", "opus"},
		"efforts": []string{"low", "medium", "high", "xhigh", "max"},
		"lowest":  map[string]any{"verifier": map[string]any{"model": "opus", "effort": "high"}},
		"bring_back": map[string]any{
			"security": []string{"guarded/"},
		},
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
	sb.mustGit(t, sb.repo, "add", "-A")
	sb.mustGit(t, sb.repo, "commit", "--quiet", "-m", "chore: turn the staffing on")
}

// refusesAs puts a command of the session under test to the hook as the agent
// given, "" for the main conversation. The hook has to refuse it under one of
// the rules, with a route.
func (sb sandbox) refusesAs(t *testing.T, agent, command string, rules ...string) {
	t.Helper()
	stdout, stderr, code := sb.hook(t, "PreToolUse",
		toolCall(sandboxSession, sb.tree(), agent, "Bash", map[string]any{"command": command}))
	who := "the main conversation"
	if agent != "" {
		who = "sdlc:" + agent
	}
	if code != 0 {
		t.Fatalf("the hook exited %d for %q, so its reply is never read:\n%s", code, command, stderr)
	}
	if stdout == carryOn {
		t.Errorf("the hook let %s run %q", who, command)
		return
	}
	var r refusal
	oneLine(t, stdout, &r)
	reason := r.HookSpecificOutput.PermissionDecisionReason
	if r.HookSpecificOutput.HookEventName != "PreToolUse" || r.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("the hook's reply to %q is not a refusal of a tool call: %q", command, stdout)
	}
	if !slices.ContainsFunc(rules, func(rule string) bool { return strings.Contains(reason, "["+rule+"]") }) {
		t.Errorf("%s running %q was refused under none of %q:\n%s", who, command, rules, reason)
	}
	if !strings.Contains(reason, "Instead:") {
		t.Errorf("the refusal of %q offers no route:\n%s", command, reason)
	}
}

// shows is `sdlc staffing show --json`, run by the main conversation.
func (sb sandbox) shows(t *testing.T, args ...string) staffed {
	t.Helper()
	var got staffed
	out := sb.works(t, sb.tree(), "", "", append(append([]string{"staffing", "show"}, args...), "--json")...)
	if err := json.Unmarshal([]byte(out), &got); err != nil || !got.OK {
		t.Fatalf("sdlc staffing show --json printed something else: %v\n%s", err, out)
	}
	return got
}

// staffedGate is worksGate for a story with a staffing: the gate's work, a
// review from every reviewer of the gate that is not in out, and the pass.
func (sb sandbox) staffedGate(t *testing.T, gate model.Gate, out ...string) {
	t.Helper()
	tree := sb.tree()
	switch gate {
	case model.GateTestsFrozen:
		sb.writesFile(t, "sdet", "invoice_test.go", sandboxTest)
		sb.works(t, tree, "sdet", sandboxTestPlan, "artifact", "write", "test_plan")
		sb.works(t, tree, "sdet", "", "freeze")
	case model.GatePlan:
		sb.works(t, tree, "implementer", sandboxPlan, "artifact", "write", "plan")
	case model.GateVerification:
		sb.works(t, tree, "verifier", sandboxVerification, "artifact", "write", "verification")
	case model.GateCommit:
		sb.commits(t)
	case model.GateRetro:
		sb.works(t, tree, "bookkeeper", sandboxRetro, "artifact", "write", "retro")
	}
	for _, r := range model.ReviewersFor(gate) {
		if r.WhenDoRAdvocate || slices.Contains(out, r.Role) {
			continue
		}
		sb.works(t, tree, r.Role, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
	}
	sb.works(t, tree, "", "", "gate", string(gate), "pass", "--note", "worked by the session under test")
}

func TestUS026AStoryIsStaffedByTheProjectManagerAndWorkedToTrunkInTheSandbox(t *testing.T) {
	sb := newSandbox(t)
	sb.turnsTheStaffingOn(t)
	main, tree := sb.repo, sb.tree()

	sb.works(t, main, "", "", "start")
	sb.works(t, tree, "", "", "gate", "dor", "pass", "--note", "the criteria are testable")
	sb.works(t, tree, "researcher", sandboxAnalysis, "artifact", "write", "analysis")
	sb.works(t, tree, "researcher", sandboxThreats, "artifact", "write", "threats")

	t.Run("AC-1 the analysis gate does not pass while no staffing is recorded", func(t *testing.T) {
		r := sb.does(t, tree, "", "", "gate", "analysis", "pass", "--security-sensitive=false")
		if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, "SDLC-E0068") || !strings.Contains(r.stderr, "sdlc:project-manager") {
			t.Fatalf("gate analysis pass with no staffing: exit %d\nstdout %q\nstderr %q\nwant SDLC-E0068, sending the reader to sdlc:project-manager",
				r.code, r.stdout, r.stderr)
		}
		if gate := sb.nextGate(t); gate != string(model.GateAnalysis) {
			t.Errorf("the next gate is %q, want analysis", gate)
		}
	})

	set := "sdlc staffing set <<'SDLC_DOCUMENT'\n" + staffingDocument(t) + "\nSDLC_DOCUMENT"
	t.Run("AC-6 the hook refuses the staffing to everyone in the session but the project manager", func(t *testing.T) {
		for _, agent := range []string{"", "researcher", "implementer", "verifier", "security"} {
			sb.refusesAs(t, agent, set, "staffing-is-recorded-by-the-project-manager")
			sb.refusesAs(t, agent, `sdlc staffing amend perf --left-out --reason "nothing to measure"`,
				"staffing-is-recorded-by-the-project-manager")
		}
	})

	t.Run("AC-1 the project manager records the staffing through the sdlc command", func(t *testing.T) {
		// Red-team and the human advocate have nothing to read on this story.
		out := sb.works(t, tree, "project-manager", staffingDocument(t, "red-team", "human-advocate"), "staffing", "set")
		if !strings.Contains(out, "US-001") {
			t.Errorf("staffing set does not say which story it staffed:\n%s", out)
		}
		got := sb.shows(t)
		if len(got.Agents) != len(staffedAgents) {
			t.Fatalf("the staffing shows %d agents, and the plugin ships %d", len(got.Agents), len(staffedAgents))
		}
		for _, a := range got.Agents {
			leftOut := a.Agent == "red-team" || a.Agent == "human-advocate"
			if a.Needed == leftOut || a.Reason == "" || (a.Needed && (a.AskedModel == "" || a.AskedEffort == "")) {
				t.Errorf("%s is recorded as %+v", a.Agent, a)
			}
			if a.SetBy == "person" {
				t.Errorf("%s is recorded as a person's choice, and the project manager recorded it", a.Agent)
			}
		}
	})
	if t.Failed() {
		t.Fatal("no staffing was recorded, so nothing after this can be run")
	}

	t.Run("AC-12 the hook refuses the project manager every other sdlc command that writes", func(t *testing.T) {
		const own = "project-manager-only-staffs"
		for command, rules := range map[string][]string{
			"sdlc gate analysis pass --security-sensitive=false": {own},
			"sdlc review add code_review code-reviewer approve":  {own, "review-is-recorded-by-its-reviewer"},
			"sdlc artifact write analysis":                       {own},
			"sdlc escalate spec_unclear --message x":             {own},
			"sdlc freeze":                                        {own},
			"sdlc start":                                         {own},
			"sdlc cost add --usd 1":                              {own},
			"sdlc approve US-001":                                {own, "approval-is-a-human-decision"},
			`sdlc unfreeze --reason "x"`:                         {own, "unfreeze-is-a-human-decision"},
			"sdlc ack --through HEAD":                            {own, "acknowledgement-is-a-human-decision"},
			"sdlc stop":                                          {own, "stop-is-a-human-decision"},
			"sdlc staffing assign security --reason x":           {"assignment-is-a-human-decision"},
		} {
			sb.refusesAs(t, "project-manager", command, rules...)
		}
		// Marking a story ready is an edit of the backlog, which is nobody's
		// while a story is worked.
		stdout, _, _ := sb.hook(t, "PreToolUse", toolCall(sandboxSession, tree, "project-manager", "Bash",
			map[string]any{"command": `echo '{"stories":[]}' > ` + filepath.ToSlash(filepath.Join(main, "user_stories.json"))}))
		if stdout == carryOn {
			t.Error("the hook let the project manager write the backlog")
		}
		// What it reads, it reads.
		for _, command := range []string{"sdlc staffing show --json", "sdlc status --json", "sdlc review list", "git log --oneline -3"} {
			sb.allows(t, tree, "project-manager", "Bash", map[string]any{"command": command})
		}
	})

	assign := []string{"staffing", "assign", "architect", "--model", "opus", "--effort", "high", "--reason", "I want the design read"}
	t.Run("AC-8 only a person assigns, and what they assign is not the project manager's to change", func(t *testing.T) {
		for _, session := range []string{sandboxSession, sandboxOther} {
			sb.refuses(t, session, tree, "Bash", map[string]any{"command": "sdlc " + strings.Join(assign[:8], " ") + ` "mine"`},
				"assignment-is-a-human-decision")
		}
		// The second barrier, for a spelling the hook cannot read: the command
		// refuses wherever it sees a Claude Code session.
		stdout, stderr, code := sb.outcome(t, sandboxSession, tree, "", assign...)
		if code != 1 || stdout != "" || !strings.Contains(stderr, "SDLC-E0074") {
			t.Errorf("staffing assign from the session working the story: exit %d\nstdout %q\nstderr %q\nwant SDLC-E0074", code, stdout, stderr)
		}
		if a := sb.shows(t); slices.ContainsFunc(a.Agents, func(x struct {
			Agent         string `json:"agent"`
			Needed        bool   `json:"needed"`
			Reason        string `json:"reason"`
			AskedModel    string `json:"asked_model"`
			AskedEffort   string `json:"asked_effort"`
			SetBy         string `json:"set_by"`
			BroughtBackBy string `json:"brought_back_by"`
		}) bool {
			return x.SetBy == "person"
		}) {
			t.Error("a session's assignment was recorded as a person's")
		}

		r := sb.person(t, tree, assign...)
		if r.code != 0 || r.stderr != "" || !strings.Contains(r.stdout, "architect") {
			t.Fatalf("staffing assign, by a person: exit %d\nstdout %q\nstderr %q", r.code, r.stdout, r.stderr)
		}
		got := sb.shows(t)
		for _, a := range got.Agents {
			if a.Agent == "architect" && (!a.Needed || a.AskedModel != "opus" || a.AskedEffort != "high" || a.SetBy != "person") {
				t.Errorf("the architect is recorded as %+v, want needed at opus and high, set by a person", a)
			}
		}
		// The hook lets the project manager amend; the command refuses this one.
		for _, args := range [][]string{
			{"staffing", "amend", "architect", "--left-out", "--reason", "to save the tokens"},
			{"staffing", "amend", "architect", "--model", "sonnet", "--reason", "to save the tokens"},
		} {
			refused := sb.does(t, tree, "project-manager", "", args...)
			if refused.code != 1 || !strings.Contains(refused.stderr, "SDLC-E0072") {
				t.Errorf("sdlc %s: exit %d\n%s\nwant SDLC-E0072", strings.Join(args, " "), refused.code, refused.stderr)
			}
		}
	})

	sb.works(t, tree, "", "", "gate", "analysis", "pass", "--security-sensitive=false")

	t.Run("AC-6 the project manager amends the staffing once the analysis is recorded, and the record says what changed and why", func(t *testing.T) {
		const why = "the analysis found nothing that crosses a trust boundary"
		sb.works(t, tree, "project-manager", "", "staffing", "amend", "security", "--left-out", "--reason", why)
		got := sb.shows(t)
		last := got.Amendments[len(got.Amendments)-1]
		if last.Agent != "security" || last.Reason != why || last.At == "" || last.Change == "" || last.By == "person" {
			t.Errorf("the amendment is recorded as %+v", last)
		}
	})

	t.Run("AC-2 the main conversation reads who is needed at the gate, and what is asked for each", func(t *testing.T) {
		got := sb.shows(t)
		if got.Gate != string(model.GateTestsFrozen) {
			t.Errorf("gate = %q, want tests_frozen", got.Gate)
		}
		if !slices.ContainsFunc(got.AtGate, func(a struct {
			Agent       string `json:"agent"`
			AskedModel  string `json:"asked_model"`
			AskedEffort string `json:"asked_effort"`
		}) bool {
			return a.Agent == "sdet" && a.AskedModel == "sonnet" && a.AskedEffort == "medium"
		}) {
			t.Errorf("at tests_frozen the output does not name the sdet at sonnet and medium: %+v", got.AtGate)
		}
		prose := sb.works(t, tree, "", "", "staffing", "show")
		if !strings.Contains(strings.ToLower(prose), "asked for") {
			t.Errorf("staffing show does not call the model and effort what was asked for:\n%s", prose)
		}
		for _, ran := range []string{"ran at", "ran on", "runs at", "running at"} {
			if strings.Contains(strings.ToLower(prose), ran) {
				t.Errorf("staffing show says %q, and the tool cannot know what an agent ran at:\n%s", ran, prose)
			}
		}
	})

	out := []string{"red-team", "human-advocate", "security"}
	sb.staffedGate(t, model.GateTestsFrozen)
	sb.staffedGate(t, model.GatePlan)

	t.Run("AC-9 review list shows who is left out, and why", func(t *testing.T) {
		list := sb.works(t, tree, "", "", "review", "list")
		for _, role := range out {
			if !slices.ContainsFunc(strings.Split(list, "\n"), func(l string) bool {
				return strings.Contains(l, role) && strings.Contains(l, "left out")
			}) {
				t.Errorf("review list has no line saying %s is left out:\n%s", role, list)
			}
		}
		for _, why := range []string{"the story gives red-team nothing to read", "the analysis found nothing that crosses a trust boundary"} {
			if !strings.Contains(list, why) {
				t.Errorf("review list does not give the reason %q:\n%s", why, list)
			}
		}
	})

	t.Run("AC-3 a gate passes with no review from a reviewer the staffing leaves out", func(t *testing.T) {
		sb.staffedGate(t, model.GateDesignReview, out...)
	})
	if t.Failed() {
		t.Fatal("the design review did not pass, so nothing after this can be run")
	}

	// The implementation: the story's code, and a file under a path the
	// project lists for security.
	sb.writesFile(t, "implementer", "invoice.go", sandboxCode)
	sb.writesFile(t, "implementer", "guarded/rule.go", "package guarded\n")
	sb.staffedGate(t, model.GateImplementation)
	sb.staffedGate(t, model.GateVerification)
	sb.staffedGate(t, model.GateVerifierReview)

	t.Run("AC-7 a change that touches a listed path brings the reviewer back, and the output names the path", func(t *testing.T) {
		sb.works(t, tree, "code-reviewer", "# code-reviewer\n", "review", "add", "code_review", "code-reviewer", "approve")
		sb.works(t, tree, "perf", "# perf\n", "review", "add", "code_review", "perf", "approve")
		list := sb.works(t, tree, "", "", "review", "list", "--gate", "code_review")
		if !slices.ContainsFunc(strings.Split(list, "\n"), func(l string) bool {
			return strings.Contains(l, "security") && strings.Contains(l, "guarded/") && !strings.Contains(l, "left out")
		}) {
			t.Errorf("review list does not show security brought back by guarded/:\n%s", list)
		}
		r := sb.does(t, tree, "", "", "gate", "code_review", "pass")
		if r.code != 1 || !strings.Contains(r.stderr, "SDLC-E0029") || !strings.Contains(r.stderr, "security") || !strings.Contains(r.stderr, "guarded/") {
			t.Errorf("gate code_review pass without security: exit %d\n%s\nwant SDLC-E0029 naming security and guarded/", r.code, r.stderr)
		}
		if !strings.Contains(r.stderr, "human-advocate") {
			return
		}
		t.Errorf("code_review waits for the human advocate, which no path brought back:\n%s", r.stderr)
	})

	// Rework: the code changes after perf read it, so perf's review is of a
	// tree that is no longer there. The project manager then leaves perf out,
	// and nobody is going to bring that review up to date.
	sb.works(t, tree, "", "", "gate", "implementation", "fail", "--note", "the rule is to be bounded")
	sb.writesFile(t, "implementer", "guarded/rule.go", "package guarded\n\nconst Bound = 8\n")
	sb.staffedGate(t, model.GateImplementation)
	sb.staffedGate(t, model.GateVerification)
	sb.staffedGate(t, model.GateVerifierReview)
	sb.works(t, tree, "project-manager", "", "staffing", "amend", "perf", "--left-out", "--reason", "the rework took the loop out")
	sb.works(t, tree, "code-reviewer", "# code-reviewer\n", "review", "add", "code_review", "code-reviewer", "approve")
	sb.works(t, tree, "security", "# security\n", "review", "add", "code_review", "security", "approve")

	t.Run("AC-3 neither the gate nor git commit is held to the stale review of a reviewer since left out", func(t *testing.T) {
		sb.works(t, tree, "", "", "gate", "code_review", "pass", "--note", "approved")

		// The opposite row first: a reviewer the staffing keeps is still held
		// to. The work changes after the reviews, and the hook refuses the
		// commit over the reviewers who are needed, without sending anybody
		// back to perf, which is not.
		rule := filepath.Join(tree, "guarded", "rule.go")
		reviewed := readFile(t, rule)
		writeFile(t, rule, reviewed+"\n// changed after the review\n")
		commit := toolCall(sandboxSession, tree, "", "Bash", map[string]any{"command": "git commit -m 'feat: US-001'"})
		stdout, stderr, code := sb.hook(t, "PreToolUse", commit)
		if code != 0 || stdout == carryOn {
			t.Fatalf("the hook let a commit of work changed after its review through: exit %d\n%s%s", code, stdout, stderr)
		}
		var r refusal
		oneLine(t, stdout, &r)
		reason := r.HookSpecificOutput.PermissionDecisionReason
		for _, kept := range []string{"[commit-gate]", "verifier (verifier_review)", "code-reviewer (code_review)", "security (code_review)"} {
			if !strings.Contains(reason, kept) {
				t.Errorf("the refusal of a commit changed after its review does not say %q:\n%s", kept, reason)
			}
		}
		if strings.Contains(reason, "perf") {
			t.Errorf("the refusal sends the reader back to perf, which the staffing leaves out:\n%s", reason)
		}
		writeFile(t, rule, reviewed)

		// commits puts `git commit` to the hook, which has to let it through:
		// perf's review of the tree before the rework is still on the record.
		sb.commits(t)
		sb.works(t, tree, "", "", "gate", "commit", "pass", "--note", "committed")
	})
	if t.Failed() {
		t.Fatal("the story was not committed, so nothing after this can be run")
	}
	sb.staffedGate(t, model.GateRetro)

	t.Run("AC-9 the log shows the staffing and every amendment to it", func(t *testing.T) {
		prose := sb.works(t, tree, "", "", "log")
		for _, want := range []string{
			"staffing",
			"I want the design read",
			"the analysis found nothing that crosses a trust boundary",
			"the rework took the loop out",
		} {
			if !strings.Contains(prose, want) {
				t.Errorf("sdlc log does not show %q:\n%s", want, prose)
			}
		}
		var logged struct {
			Entries []struct {
				Staffing *struct {
					Agents     []map[string]any `json:"agents"`
					Amendments []map[string]any `json:"amendments"`
				} `json:"staffing"`
			} `json:"entries"`
		}
		raw := sb.works(t, tree, "", "", "log", "--json")
		if err := json.Unmarshal([]byte(raw), &logged); err != nil {
			t.Fatal(err)
		}
		if len(logged.Entries) != 1 || logged.Entries[0].Staffing == nil ||
			len(logged.Entries[0].Staffing.Agents) != len(staffedAgents) || len(logged.Entries[0].Staffing.Amendments) != 3 {
			t.Errorf("sdlc log --json does not carry the staffing of 12 agents and its 3 amendments:\n%s", raw)
		}
	})

	// The run completes: the story lands on trunk.
	sb.works(t, tree, "", "", "stop")
	if got := storiesOf(t, main); got["US-001"] != "done" {
		t.Fatalf("the story is %q at the end of the run, want done", got["US-001"])
	}
}

// AC-10 beside it: the same sandbox with the configuration as `sdlc init`
// wrote it. TestAC2EveryCommandThatNeedsNoPersonRunsToTheEndInTheSandbox holds
// every command's prose there to what it was before this story; this holds
// that the new command says the staffing is off and that nothing about it
// reaches the output or the record.
func TestUS026WithTheStaffingOffTheSandboxAsksNothingOfAStory(t *testing.T) {
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")

	t.Run("AC-10 the staffing commands say it is off", func(t *testing.T) {
		for _, args := range [][]string{{"staffing", "show"}, {"staffing", "show", "--json"}} {
			r := sb.does(t, tree, "", "", args...)
			if r.code != 1 || !strings.Contains(r.stdout+r.stderr, "SDLC-E0067") || !strings.Contains(r.stdout+r.stderr, "staffing.enabled") {
				t.Errorf("sdlc %s with the staffing off: exit %d\nstdout %q\nstderr %q\nwant SDLC-E0067 naming staffing.enabled",
					strings.Join(args, " "), r.code, r.stdout, r.stderr)
			}
		}
	})
	sb.worksEveryGate(t)
	t.Run("AC-10 nothing about the staffing reaches the output or the record", func(t *testing.T) {
		for _, args := range [][]string{
			{"status"}, {"status", "--json"}, {"review", "list"}, {"review", "list", "--json"}, {"log"}, {"log", "--json"},
		} {
			if out := sb.works(t, tree, "", "", args...); strings.Contains(out, "staffing") || strings.Contains(out, "left out") || strings.Contains(out, "left_out") {
				t.Errorf("sdlc %s says something about the staffing:\n%s", strings.Join(args, " "), out)
			}
		}
		record := readFile(t, filepath.Join(main, ".sdlc", "stories", "US-001", "gate-record.json"))
		if strings.Contains(record, "staffing") {
			t.Errorf("the record of a story worked with the staffing off holds one:\n%s", record)
		}
	})
}
