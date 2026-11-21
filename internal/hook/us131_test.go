package hook

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/config"
	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// US-131 AC-6 and T-1 of the threats, through the hook.

// An arbiter runs as its role's agent, so its review is the role's to record, as
// an instance's is; no other agent, and not the main conversation, records it.
// Today the name is no reviewer's, so the rule reads no role and lets it by.
func TestAC6TheHookKeepsAnArbitersReviewToItsRolesAgent(t *testing.T) {
	t.Parallel()
	root := staffedProject(t)
	for _, recorded := range []string{
		"sdlc review add design_review architect-arbiter approve",
		"sdlc review add design_review Architect-Arbiter approve",
		"sdlc review add code_review code-reviewer-arbiter approve",
	} {
		owner := "sdlc:architect"
		if strings.Contains(recorded, "code-reviewer") {
			owner = "sdlc:code-reviewer"
		}
		for _, agent := range []string{"", "sdlc:implementer", "sdlc:red-team", "sdlc:verifier", "general-purpose"} {
			if agent == owner {
				continue
			}
			refusedUnder(t, call(t, command(root, agent, recorded), noEnv), "review-is-recorded-by-its-reviewer", who(agent), recorded)
		}
		if r := call(t, command(root, owner, recorded), noEnv); denied(r) {
			t.Errorf("AC-6 %s was refused its own arbiter's review: %s", owner, r.HookSpecificOutput.PermissionDecisionReason)
		}
	}
}

// arCommit asks the hook about a commit on a record that passed every gate before
// it, every reviewer of Gates 6 and 7 having approved this tree, with the
// verifier's reviews then replaced by those given, oldest first. Each review
// carries the event `sdlc review add` writes with it.
func arCommit(t *testing.T, verifier []model.Review) reply {
	t.Helper()
	root := loopProject(t)
	write(t, root, ".sdlc/config.json", teamFile(`"project-manager":{"enabled":false}`))
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	initRepo := exec.CommandContext(t.Context(), "git", "init", "--quiet")
	initRepo.Dir = root
	if out, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	write(t, root, "invoice.go", "package invoice\n")
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := store.New(&config.Project{Root: root, Config: cfg}).ReviewSubject(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	record := model.NewRecord("A-1", at)
	for _, g := range model.GateCommit.Before() {
		record.SetGate(g, model.GatePass, "", at)
	}
	record.SetSecuritySensitive(true)
	for _, gate := range []model.Gate{model.GateVerifierReview, model.GateCodeReview} {
		for _, r := range model.ReviewersFor(gate) {
			if r.Role == "verifier" {
				continue
			}
			record.AddReview(model.Review{Gate: gate, Role: r.Role, Verdict: model.VerdictApprove, Subject: tree}, at)
		}
	}
	for i, rev := range verifier {
		when := at.Add(time.Duration(i+1) * time.Minute)
		rev.Gate = model.GateVerifierReview
		if rev.Subject == "" {
			rev.Subject = tree
		}
		record.AddReview(rev, when)
		record.Append(model.EventReview, model.ReviewEvent(rev.Gate, rev.Role, rev.Verdict), when)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	write(t, root, ".sdlc/stories/A-1/gate-record.json", string(raw))
	return call(t, command(root, "", "git commit -m done"), noEnv)
}

const arOldTree = "the tree before the rework"

// IQ-4: the hook reads the review that stands for the verifier, the arbiter's
// where one stands. A verifier block of an older tree that its arbiter replaced
// with an approval of this one does not hold the commit; today it does.
func TestAC6ACommitIsNotHeldToAReviewersStaleBlockItsArbiterReplaced(t *testing.T) {
	got := arCommit(t, []model.Review{
		{Role: "verifier", Verdict: model.VerdictBlock, Subject: arOldTree},
		{Role: "verifier", Verdict: model.VerdictBlock, Subject: arOldTree},
		{Role: "verifier-arbiter", Verdict: model.VerdictApprove},
	})
	if denied(got) {
		t.Errorf("AC-6 the commit was held to the verifier's stale block, which its arbiter's approval replaced: %s",
			got.HookSpecificOutput.PermissionDecisionReason)
	}
}

// T-1: the arbiter's review is held to the tree as every reviewer's is. An arbiter
// that approved an older tree holds the commit, however fresh the reviewer's last
// block; today the arbiter's name is not read at all.
func TestAC6ACommitIsHeldToAnArbitersApprovalOfAnOlderTree(t *testing.T) {
	got := arCommit(t, []model.Review{
		{Role: "verifier", Verdict: model.VerdictBlock},
		{Role: "verifier", Verdict: model.VerdictBlock},
		{Role: "verifier-arbiter", Verdict: model.VerdictApprove, Subject: arOldTree},
	})
	if !denied(got) {
		t.Fatal("AC-6 the commit went through on an arbiter's approval of an older tree")
	}
	if reason := got.HookSpecificOutput.PermissionDecisionReason; !strings.Contains(reason, "verifier-arbiter") {
		t.Errorf("AC-6 the refusal does not name the arbiter whose review is stale: %s", reason)
	}
}

// IQ-3: an arbiter runs as its role's agent type, so the trace tells it from the
// reviewer by the reviews the run recorded, and records the model and effort
// asked of that one: the step above the reviewer's row for the arbiter, the row
// for the reviewer. Where the run cannot be told, nothing is said, since a model
// nobody asked of it would be on the record.
func TestAC6TheTraceRecordsTheStepAboveForAnArbitersRunAndTheRowForTheReviewers(t *testing.T) {
	t.Parallel()
	team := config.Team{Models: []string{"haiku", "sonnet", "opus"}, Efforts: []string{"low", "medium", "high"}}
	at := time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)
	// atDesignReview is a record at the design review, with the architect's row
	// asked for askedModel at askedEffort, and the events given.
	atDesignReview := func(askedModel, askedEffort string, events ...model.Event) *model.Record {
		r := model.NewRecord("US-1", at)
		for _, g := range model.Gates[:slices.Index(model.Gates, model.GateDesignReview)] {
			r.Gates[g] = model.GateResult{Status: model.GatePass}
		}
		r.Staffing = &model.Staffing{Agents: []model.Staffed{{Agent: "architect", Needed: true, AskedModel: askedModel, AskedEffort: askedEffort}}}
		r.Events = append(r.Events, events...)
		return r
	}
	start := func(id string) model.Event { return model.Event{Type: model.EventAgentStart, AgentID: id} }
	review := func(role string, v model.Verdict) model.Event {
		return model.Event{Type: model.EventReview, Message: model.ReviewEvent(model.GateDesignReview, role, v)}
	}
	blocked := review("architect", model.VerdictBlock)
	for _, c := range []struct {
		name                  string
		record                *model.Record
		wantModel, wantEffort string
	}{
		{"AC-6 the arbiter's run is asked the next effort", atDesignReview("sonnet", "medium",
			start("r1"), blocked, start("r2"), blocked, start("r3"), review("architect-arbiter", model.VerdictApprove)), "sonnet", "high"},
		{"AC-6 the arbiter's run at the effort ceiling is asked the next model", atDesignReview("sonnet", "high",
			start("r1"), blocked, start("r2"), blocked, start("r3"), review("architect-arbiter", model.VerdictBlock)), "opus", "high"},
		{"AC-6 the reviewer's own run is asked its row", atDesignReview("sonnet", "medium",
			start("r1"), blocked, start("r3"), blocked), "sonnet", "medium"},
		{"AC-6 a run that recorded both names says nothing", atDesignReview("sonnet", "medium",
			start("r3"), blocked, review("architect-arbiter", model.VerdictApprove)), "", ""},
		{"AC-6 a run that recorded nothing once the role has blocked the gate says nothing", atDesignReview("sonnet", "medium",
			start("r1"), blocked, start("r3")), "", ""},
		{"AC-6 a run whose start is not on the record, once the role has blocked, says nothing", atDesignReview("sonnet", "medium",
			start("r1"), blocked, review("architect-arbiter", model.VerdictApprove)), "", ""},
		{"AC-6 a run that recorded nothing before any block is asked its row", atDesignReview("sonnet", "medium",
			start("r3")), "sonnet", "medium"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m, e := askedFor(c.record, team, "sdlc:architect", "r3")
			if m != c.wantModel || e != c.wantEffort {
				t.Errorf("asked for %q at %q, want %q at %q", m, e, c.wantModel, c.wantEffort)
			}
		})
	}
}
