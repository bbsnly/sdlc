package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// planned is a story that has reached the design review with a plan in place.
func planned(t *testing.T) string {
	t.Helper()
	root := gitProject(t)
	mustRun(t, "init")
	mustRun(t, "start")
	reach(t, root, model.GateDesignReview)
	return root
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
