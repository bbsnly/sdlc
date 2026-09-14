package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// setConfigOnDisk sets one top-level key of the scaffolded configuration.
func setConfigOnDisk(t *testing.T, root, key string, value any) {
	t.Helper()
	path := filepath.Join(root, ".sdlc", "config.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg[key] = value
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}

// reviews.gate7_advisory was read by nothing: the code reviewer blocked whatever
// the project said. It makes that one reviewer advisory -- still expected to
// report, no longer able to stop the gate -- and nobody else.
func TestAProjectCanMakeTheCodeReviewerAdvisory(t *testing.T) {
	for _, advisory := range []bool{false, true} {
		name := map[bool]string{false: "blocking by default", true: "advisory when configured"}[advisory]
		t.Run(name, func(t *testing.T) {
			root := gitProject(t)
			mustRun(t, "init")
			if advisory {
				setConfigOnDisk(t, root, "reviews", map[string]any{"gate7_advisory": true})
			}
			mustRun(t, "start")
			reach(t, root, model.GateCodeReview)

			approveAll(t, model.GateCodeReview, "code-reviewer")
			mustRunWith(t, "# code review\n", "review", "add", "code_review", "code-reviewer", "block",
				"--note", "the names do not say what they hold")

			r := run(t, "gate", "code_review", "pass")
			if passed := r.code == 0; passed != advisory {
				t.Errorf("code_review passed over the code reviewer's block = %v, want %v:\n%s",
					passed, advisory, r.stderr)
			}
			want := "code-reviewer   " + map[bool]string{false: "blocking", true: "advisory"}[advisory]
			if list := mustRun(t, "review", "list", "--gate", "code_review").stdout; !strings.Contains(list, want) {
				t.Errorf("review list does not show %q:\n%s", want, list)
			}
		})
	}
}

// planned is a story that has reached the design review with a plan in place.
func planned(t *testing.T) string {
	t.Helper()
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")
	reach(t, root, model.GateDesignReview)
	return root
}

// human_gates.dor_advocate_check was read by nothing. It has the human advocate
// read the story at Gate 1, and the gate waits for it the way it waits for every
// reviewer it expects.
func TestTheHumanAdvocateReadsTheStoryAtGate1WhenTheProjectAsks(t *testing.T) {
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")

	if listed := decode[reviewListPayload](t, mustRun(t, "review", "list", "--gate", "dor", "--json")); len(listed.Reviews) != 0 {
		t.Errorf("Gate 1 expects reviews nobody asked for: %+v", listed.Reviews)
	}
	if r := runWith(t, "# Human advocate\n", "review", "add", "dor", "human-advocate", "note"); !strings.Contains(r.stderr, "dor_advocate_check") {
		t.Errorf("a review nobody asked for was recorded:\n%s", r.stderr)
	}

	setConfigOnDisk(t, root, "human_gates", map[string]any{"pre_commit_pause_tiers": []string{"high"}, "dor_advocate_check": true})
	if r := run(t, "gate", "dor", "pass"); r.code == 0 || !strings.Contains(r.stderr, "human-advocate") {
		t.Fatalf("Gate 1 passed without the review the project asked for:\n%s", r.stderr)
	}
	listed := decode[reviewListPayload](t, mustRun(t, "review", "list", "--gate", "dor", "--json"))
	if len(listed.Reviews) != 1 || listed.Reviews[0].Role != "human-advocate" || listed.Reviews[0].Blocking {
		t.Errorf("reviews = %+v", listed.Reviews)
	}

	mustRunWith(t, "# Human advocate\n", "review", "add", "dor", "human-advocate", "note", "--note", "reads well")

	// The story changes after the advocate read it.
	var backlog map[string]any
	if err := json.Unmarshal([]byte(backlogBytes(t, root)), &backlog); err != nil {
		t.Fatal(err)
	}
	backlog["stories"].([]any)[0].(map[string]any)["title"] = "Something else entirely"
	changed, err := json.Marshal(backlog)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "user_stories.json", string(changed))
	if r := run(t, "gate", "dor", "pass"); !strings.Contains(r.stderr, "changed since") {
		t.Errorf("a review of the story before it changed let Gate 1 pass:\n%s", r.stderr)
	}

	mustRunWith(t, "# Human advocate\n", "review", "add", "dor", "human-advocate", "note")

	// What the advocate found goes to a person, and coming back from them moves
	// the story's status and time without changing what the advocate read.
	mustRun(t, "escalate", "spec_unclear", "--message", "the advocate asks who this is for")
	waiting := decode[reviewListPayload](t, mustRun(t, "review", "list", "--gate", "dor", "--story", "US-001", "--json"))
	if len(waiting.Reviews) != 1 || waiting.Reviews[0].Stale {
		t.Errorf("handing the story to a person made the advocate's review stale: %+v", waiting.Reviews)
	}
	mustRun(t, "approve", "US-001")
	mustRun(t, "start")
	mustRun(t, "gate", "dor", "pass", "--note", "the advocate read it")
}

func approveAll(t *testing.T, gate model.Gate, except ...string) {
	t.Helper()
	for _, r := range model.ReviewersFor(gate) {
		if !contains(except, r.Role) {
			mustRunWith(t, "# "+r.Role+"\n", "review", "add", string(gate), r.Role, "approve")
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func TestAReviewIsStoredWhereTheNextRoundCanReadIt(t *testing.T) {
	root := planned(t)

	got := decode[reviewPayload](t, mustRunWith(t, "# Design review\n\nAC-3 has no step.\n",
		"review", "add", "design_review", "architect", "block", "--note", "AC-3 has no step", "--json"))
	if got.Role != "architect" || got.Verdict != "block" || got.Round != 1 {
		t.Fatalf("review = %+v", got)
	}
	if got.Subject == "" {
		t.Error("the review does not record what was reviewed")
	}

	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(got.Path)))
	if err != nil {
		t.Fatalf("the review is not on disk: %v", err)
	}
	if !strings.Contains(string(body), "AC-3 has no step") {
		t.Errorf("stored:\n%s", body)
	}

	// A second round keeps the first. "What did the architect say last time" is
	// a question the next round needs answered.
	second := decode[reviewPayload](t, mustRunWith(t, "# Design review\n\nFixed.\n",
		"review", "add", "design_review", "architect", "approve", "--json"))
	if second.Round != 2 {
		t.Errorf("round = %d", second.Round)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(got.Path))); err != nil {
		t.Errorf("the first round was overwritten: %v", err)
	}
}

func TestAGateWaitsForEveryReviewerItExpects(t *testing.T) {
	planned(t)

	r := run(t, "gate", "design_review", "pass", "--note", "nobody objected")
	if r.code == 0 {
		t.Fatal("the design gate passed with no reviews at all")
	}
	for _, want := range []string{"SDLC-E0029", "architect", "red-team"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}

	// Advisory reviewers cannot stop the gate, but they cannot be skipped
	// either: a reviewer you can skip by not running it is not a reviewer.
	approveAll(t, model.GateDesignReview, "perf")
	if r := run(t, "gate", "design_review", "pass"); r.code == 0 {
		t.Error("the gate passed with an advisory reviewer never having reported")
	} else if !strings.Contains(r.stderr, "perf") {
		t.Errorf("the refusal does not name who is outstanding:\n%s", r.stderr)
	}

	mustRunWith(t, "# perf\n", "review", "add", "design_review", "perf", "note")
	mustRun(t, "gate", "design_review", "pass", "--note", "architect approved")
}

// This is the difference between a review and a rubber stamp. An approval of a
// plan that has since changed is not an approval.
func TestChangingWhatWasReviewedMakesTheApprovalStale(t *testing.T) {
	planned(t)
	approveAll(t, model.GateDesignReview)
	mustRun(t, "gate", "design_review", "pass", "--note", "all clear")

	// The plan is revised. Every approval of the old one is now of something
	// that is not there.
	mustRunWith(t, "# Plan\n\nNow with a step for AC-3.\n", "artifact", "write", "plan")

	list := decode[reviewListPayload](t, mustRun(t, "review", "list", "--gate", "design_review", "--json"))
	for _, r := range list.Reviews {
		if !r.Stale {
			t.Errorf("%s's approval survived a change to the plan", r.Role)
		}
	}
	if r := run(t, "gate", "design_review", "pass"); r.code == 0 {
		t.Fatal("a revised plan passed on the old reviews")
	} else if !strings.Contains(r.stderr, "changed since") {
		t.Errorf("the refusal does not explain why:\n%s", r.stderr)
	}

	approveAll(t, model.GateDesignReview)
	mustRun(t, "gate", "design_review", "pass", "--note", "reviewed again")
}

func TestABlockingReviewerStopsTheGateAndSaysWhy(t *testing.T) {
	planned(t)
	approveAll(t, model.GateDesignReview, "architect")
	mustRunWith(t, "# Design review\n", "review", "add", "design_review", "architect", "block",
		"--note", "AC-3 has no step")

	r := run(t, "gate", "design_review", "pass")
	if r.code == 0 {
		t.Fatal("the gate passed over a block")
	}
	for _, want := range []string{"SDLC-E0030", "architect blocked", "AC-3 has no step"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}

	// The way past a block is the reviewer looking again, and nothing else.
	mustRunWith(t, "# Design review\n", "review", "add", "design_review", "architect", "approve")
	mustRun(t, "gate", "design_review", "pass", "--note", "architect approved on round 2")
}

// Whether security can block is Gate 2's decision, and it has to be a real one:
// a flag nobody reads is a label.
func TestSecuritySensitivityDecidesWhetherSecurityBlocks(t *testing.T) {
	for _, c := range []struct{ sensitive, blocks bool }{{true, true}, {false, false}} {
		t.Run(map[bool]string{true: "sensitive", false: "not sensitive"}[c.sensitive], func(t *testing.T) {
			root := gitProject(t)
			mustRun(t, "init")
			mustRun(t, "start")

			mustRunWith(t, "# Analysis\n", "artifact", "write", "analysis")
			mustRunWith(t, "# Threats\n", "artifact", "write", "threats")
			mustRun(t, "gate", "dor", "pass", "--note", "ready")
			mustRun(t, "gate", "analysis", "pass",
				"--security-sensitive="+map[bool]string{true: "true", false: "false"}[c.sensitive])
			for _, gate := range []model.Gate{model.GateTestsFrozen, model.GatePlan} {
				satisfy(t, root, gate)
				mustRun(t, "gate", string(gate), "pass")
			}

			approveAll(t, model.GateDesignReview, "security")
			mustRunWith(t, "# security\n", "review", "add", "design_review", "security", "block",
				"--note", "the token reaches the log")

			r := run(t, "gate", "design_review", "pass")
			if blocked := r.code != 0; blocked != c.blocks {
				t.Errorf("security blocking = %v, want %v:\n%s", blocked, c.blocks, r.stderr)
			}
		})
	}
}

func TestAReviewFromSomebodyThisGateDoesNotExpectSaysWhoItDoes(t *testing.T) {
	planned(t)

	r := runWith(t, "# review\n", "review", "add", "design_review", "code-reviewer", "approve")
	if r.code == 0 {
		t.Fatal("a review was accepted from a role this gate does not expect")
	}
	for _, want := range []string{"SDLC-E0027", "architect", "red-team"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("the refusal is missing %q:\n%s", want, r.stderr)
		}
	}

	if r := runWith(t, "# review\n", "review", "add", "design_review", "architect", "probably"); r.code == 0 {
		t.Error("a verdict that is not a verdict was accepted")
	} else if !strings.Contains(r.stderr, "SDLC-E0028") {
		t.Errorf("stderr = %s", r.stderr)
	}
}

// The list is what a skill reads to find out who is outstanding, so it has to
// say which reviews can stop the gate and which are only on the record.
func TestReviewListSaysWhoBlocksAndWhoHasReported(t *testing.T) {
	planned(t)
	mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "approve")

	got := decode[reviewListPayload](t, mustRun(t, "review", "list", "--json"))
	seen := map[string]reviewInfo{}
	for _, r := range got.Reviews {
		seen[r.Gate+"/"+r.Role] = r
	}
	if a := seen["design_review/architect"]; !a.Blocking || a.Verdict != "approve" || a.Stale {
		t.Errorf("architect = %+v", a)
	}
	if p := seen["design_review/perf"]; p.Blocking || p.Verdict != "" {
		t.Errorf("perf = %+v", p)
	}
	if _, ok := seen["code_review/code-reviewer"]; !ok {
		t.Error("the list does not reach the later gates")
	}

	if out := mustRun(t, "review", "list").stdout; !strings.Contains(out, "not reviewed") ||
		!strings.Contains(out, "blocking") {
		t.Errorf("the prose does not say where things stand:\n%s", out)
	}
}
