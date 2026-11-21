package cli

import (
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// US-131 AC-6 with the settled IQ-3, IQ-4 and IQ-11. A reviewer's limit-th
// stopping block no longer hands the story over: it sends the gate to an arbiter,
// <role>-arbiter, whose review then stands for the role at that gate. The arbiter
// is refused before the reviewer reaches its limit, and the reviewer once an
// arbiter stands (T-1 of the threats); the arbiter's own limit-th block hands the
// story over as review_not_converging.

// arReview is what `sdlc review add --json` says, with the arbiter it sends the
// gate to.
type arReview struct {
	reviewPayload
	Arbiter string `json:"arbiter"`
}

// arAdd records a review of the design by name, and reads what it says.
func arAdd(t *testing.T, name, verdict string, i int) arReview {
	t.Helper()
	args := []string{"review", "add", "design_review", name, verdict, "--note", "round " + strconv.Itoa(i), "--json"}
	if verdict == "block" {
		args = append(args, "--important", "1", "--finding", "important the one finding")
	}
	return decode[arReview](t, mustRunWith(t, "# "+name+"\n", args...))
}

// arRefused is a review that is refused and records nothing.
func arRefused(t *testing.T, root, name, verdict string) {
	t.Helper()
	before := dcRecordText(t, root)
	files := hoReviewFiles(t, root)
	args := []string{"review", "add", "design_review", name, verdict}
	if verdict == "block" {
		args = append(args, "--important", "1", "--finding", "important the one finding")
	}
	r := runWith(t, "# "+name+"\n", args...)
	if r.code == 0 {
		t.Errorf("AC-6 %s's %s was recorded:\n%s", name, verdict, r.stdout)
		return
	}
	if !strings.Contains(r.stderr, "SDLC-E") {
		t.Errorf("AC-6 %s's %s was refused without a code:\n%s", name, verdict, r.stderr)
	}
	if after := dcRecordText(t, root); after != before {
		t.Errorf("AC-6 the refused %s by %s changed the record", verdict, name)
	}
	if after := hoReviewFiles(t, root); strings.Join(after, ",") != strings.Join(files, ",") {
		t.Errorf("AC-6 the refused %s by %s left a review file", verdict, name)
	}
}

func TestAC6TheReviewersLimitThBlockSendsTheGateToAnArbiterAndHandsNothingOver(t *testing.T) {
	t.Parallel()
	root := planned(t)
	if first := arAdd(t, "architect", "block", 1); first.HandedOver != "" || first.Arbiter != "" {
		t.Fatalf("AC-6 the first block went somewhere: %+v", first)
	}
	second := arAdd(t, "architect", "block", 2)
	if second.HandedOver != "" {
		t.Errorf("AC-6 the reviewer's limit-th block handed the story over: %+v", second)
	}
	if second.Arbiter != "architect-arbiter" {
		t.Errorf("AC-6 the reviewer's limit-th block names arbiter %q, want architect-arbiter", second.Arbiter)
	}
	record := readRecord(t, root, "US-001")
	if len(record.Escalations) != 0 {
		t.Errorf("AC-6 the story was escalated: %+v", record.Escalations)
	}
	if hoActive(t) != "US-001" {
		t.Error("AC-6 the iteration ended at the reviewer's limit")
	}

	// Without --json it says so in words.
	root = planned(t)
	mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "block", "--important", "1",
		"--finding", "important the one finding")
	out := mustRunWith(t, "# architect\n", "review", "add", "design_review", "architect", "block", "--important", "1",
		"--finding", "important the one finding").stdout
	if !strings.Contains(out, "architect-arbiter") || strings.Contains(out, "handed to a person") {
		t.Errorf("AC-6 the reviewer's limit-th block does not say the gate goes to architect-arbiter:\n%s", out)
	}
	_ = root
}

func TestAC6AnArbitersApprovalStandsInPlaceOfTheReviewersBlock(t *testing.T) {
	t.Parallel()
	planned(t)
	arAdd(t, "architect", "block", 1)
	arAdd(t, "architect", "block", 2)
	got := arAdd(t, "architect-arbiter", "approve", 1)
	if got.Role != "architect-arbiter" || got.Round != 1 {
		t.Errorf("AC-6 the arbiter's review is recorded as %s round %d, want architect-arbiter round 1", got.Role, got.Round)
	}
	approveAll(t, model.GateDesignReview, "architect")
	if r := run(t, "gate", "design_review", "pass", "--note", "the arbiter ruled"); r.code != 0 {
		t.Errorf("AC-6 the design review did not pass on the arbiter's approval:\n%s", r.stderr)
	}
	for _, info := range decode[reviewListPayload](t, mustRun(t, "review", "list", "--gate", "design_review", "--json")).Reviews {
		if model.BaseRole(strings.TrimSuffix(info.Role, "-arbiter")) == "architect" && info.Verdict == "block" {
			t.Errorf("AC-6 review list shows the architect's block standing: %+v", info)
		}
	}
}

// T-1: the arbiter's name does not dodge a blocking review.
func TestAC6AnArbiterBeforeTheReviewersLimitIsRefusedAndRecordsNothing(t *testing.T) {
	t.Parallel()
	root := planned(t)
	for _, verdict := range []string{"approve", "note", "block"} {
		arRefused(t, root, "architect-arbiter", verdict)
		arRefused(t, root, "Architect-Arbiter", verdict)
	}
	arAdd(t, "architect", "block", 1)
	arRefused(t, root, "architect-arbiter", "approve")
	// An approval is no block, and an advisory reviewer's block stops nothing.
	arAdd(t, "architect", "approve", 2)
	arRefused(t, root, "architect-arbiter", "approve")
	for i := range 3 {
		arAdd(t, "red-team", "block", i+1)
	}
	arRefused(t, root, "red-team-arbiter", "approve")
	// The architect's second block that stops the gate is its limit: from there
	// the same name that was refused above is taken.
	if got := arAdd(t, "architect", "block", 3); got.Arbiter != "architect-arbiter" || got.HandedOver != "" {
		t.Fatalf("AC-6 the architect's block at its limit did not send the gate to architect-arbiter: %+v", got)
	}
	arAdd(t, "architect-arbiter", "approve", 1)
}

// T-1: once an arbiter stands, the reviewer it replaced is refused at that gate.
func TestAC6OnceAnArbiterStandsTheReviewerItReplacedIsRefused(t *testing.T) {
	t.Parallel()
	root := planned(t)
	arAdd(t, "architect", "block", 1)
	arAdd(t, "architect", "block", 2)
	arAdd(t, "architect-arbiter", "block", 1)
	for _, verdict := range []string{"approve", "block", "note"} {
		arRefused(t, root, "architect", verdict)
		arRefused(t, root, " Architect ", verdict)
	}
	// Another reviewer at the gate is not held by it.
	arAdd(t, "red-team", "approve", 1)
}

// T-1: an arbiter stands only for its own role, at its own gate; a seat past the
// role's instances has no arbiter; a spelling of the name in another case or with
// spaces is the same name.
func TestAC6AnArbiterStandsOnlyForItsOwnRoleAtItsOwnGate(t *testing.T) {
	t.Parallel()
	root := planned(t)
	arAdd(t, "architect", "block", 1)
	arAdd(t, "architect", "block", 2)
	for _, name := range []string{"red-team-arbiter", "code-reviewer-arbiter", "verifier-arbiter", "architect-2-arbiter",
		"architect-arbiter-arbiter", "arbiter", "researcher-arbiter"} {
		arRefused(t, root, name, "approve")
	}
	for _, gate := range []string{"verifier_review", "code_review"} {
		if r := runWith(t, "# architect\n", "review", "add", gate, "architect-arbiter", "approve", "--story", "US-001"); r.code == 0 {
			t.Errorf("AC-6 architect-arbiter's review was recorded at %s", gate)
		}
	}
	got := decode[arReview](t, mustRunWith(t, "# architect\n", "review", "add", "design_review", " Architect-Arbiter ", "approve", "--json"))
	if got.Role != "architect-arbiter" {
		t.Errorf("AC-6 the arbiter's name spelled otherwise was recorded as %q", got.Role)
	}
}

func TestAC6TheArbitersOwnLimitThBlockHandsTheStoryOver(t *testing.T) {
	t.Parallel()
	root := planned(t)
	arAdd(t, "architect", "block", 1)
	arAdd(t, "architect", "block", 2)
	if first := arAdd(t, "architect-arbiter", "block", 1); first.HandedOver != "" || first.Arbiter != "" {
		t.Fatalf("AC-6 the arbiter's first block went somewhere: %+v", first)
	}
	second := arAdd(t, "architect-arbiter", "block", 2)
	if second.HandedOver != "review_not_converging" {
		t.Fatalf("AC-6 the arbiter's limit-th block did not hand the story over: %+v", second)
	}
	record := readRecord(t, root, "US-001")
	pending, ok := record.PendingEscalation()
	if !ok || pending.Type != "review_not_converging" || !strings.Contains(pending.Message, "architect-arbiter") {
		t.Errorf("AC-6 the question a person reads does not name the arbiter: %+v", pending)
	}
	if r := run(t, "start"); !strings.Contains(r.stderr, "SDLC-E0036") {
		t.Errorf("AC-6 the story carried on past the hand-over:\n%s", r.stderr)
	}
}

// IQ-11: the arbiter's limit is its role's own max_rounds, else the loop's.
func TestAC6TheArbitersLimitIsItsRolesOwnMaxRounds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name        string
		entry       map[string]any
		loop, limit int
	}{
		{"AC-6 the role's max_rounds of 1", map[string]any{"max_rounds": 1}, 2, 1},
		{"AC-6 the role's max_rounds of 3", map[string]any{"max_rounds": 3}, 2, 3},
		{"AC-6 the loop's 3 with no setting", map[string]any{}, 3, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := teamProject(t, teamOf(agentEntries{"architect": c.entry, "project-manager": {"enabled": false}}))
			setConfigOnDisk(t, root, "loop", map[string]any{"max_review_rounds": c.loop, "max_rework_rounds": 3, "max_stop_blocks": 3})
			// A loop of 2 is the default, so the file may not change: git
			// refuses a commit of nothing.
			wtCommit(t, root, "story")
			mustRun(t, "start")
			reach(t, root, model.GateDesignReview)
			for i := 1; i <= c.limit; i++ {
				got := arAdd(t, "architect", "block", i)
				if want := i == c.limit; (got.Arbiter != "") != want || got.HandedOver != "" {
					t.Fatalf("AC-6 the architect's block %d: %+v", i, got)
				}
			}
			for i := 1; i <= c.limit; i++ {
				got := arAdd(t, "architect-arbiter", "block", i)
				if want := i == c.limit; (got.HandedOver == "review_not_converging") != want {
					t.Fatalf("AC-6 the arbiter's block %d of %d: %+v", i, c.limit, got)
				}
			}
		})
	}
}

// A person's answer starts both counts again: the arbiter is refused until the
// reviewer reaches its limit once more, and the reviewer may review again.
func TestAC6APersonsAnswerStartsTheReviewersAndTheArbitersCountsAgain(t *testing.T) {
	t.Parallel()
	root := planned(t)
	arAdd(t, "architect", "block", 1)
	arAdd(t, "architect", "block", 2)
	arAdd(t, "architect-arbiter", "block", 1)
	arAdd(t, "architect-arbiter", "block", 2)
	mustRun(t, "approve", "US-001")
	mustRun(t, "start")
	arRefused(t, root, "architect-arbiter", "approve")
	if got := arAdd(t, "architect", "block", 3); got.HandedOver != "" || got.Arbiter != "" {
		t.Errorf("AC-6 the first block after a person's answer went somewhere: %+v", got)
	}
}

// IQ-3: once the reviewer reaches its limit, `staffing show --gate` lists the
// arbiter at the gate, one step above the reviewer: the next effort, at the effort
// ceiling the next model, at both the reviewer's own. No staffing row is written.
func TestAC6StaffingShowListsTheArbiterOneStepAboveTheReviewer(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name                        string
		model, effort, wantM, wantE string
	}{
		{"AC-6 the next effort", "sonnet", "medium", "sonnet", "high"},
		{"AC-6 the next model at the effort ceiling", "sonnet", "max", "opus", "max"},
		{"AC-6 the reviewer's own at both ceilings", "opus", "max", "opus", "max"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			root := gitProjectInitialised(t)
			stfProjectFrom(t, root)
			mustRun(t, "start")
			mustRun(t, "gate", "dor", "pass", "--note", "the criteria are testable")
			stfSet(t, stfFull().with("architect", func(a *stfAgent) { a.AskedModel, a.AskedEffort = c.model, c.effort }))
			for _, gate := range []model.Gate{model.GateAnalysis, model.GateTestsFrozen, model.GatePlan} {
				satisfy(t, root, gate)
				mustRun(t, "gate", string(gate), "pass", "--note", "reached by the test")
			}
			if _, ok := stfShow(t, "--gate", "design_review").atGate("architect-arbiter"); ok {
				t.Error("AC-6 the arbiter is at the gate before the architect has blocked")
			}
			arAdd(t, "architect", "block", 1)
			arAdd(t, "architect", "block", 2)
			at, ok := stfShow(t, "--gate", "design_review").atGate("architect-arbiter")
			if !ok {
				t.Fatal("AC-6 staffing show --gate design_review does not list architect-arbiter once the architect reached its limit")
			}
			if at.AskedModel != c.wantM || at.AskedEffort != c.wantE {
				t.Errorf("AC-6 the arbiter is asked for %s at %s, want %s at %s", at.AskedModel, at.AskedEffort, c.wantM, c.wantE)
			}
			if staffing := readRecord(t, root, "US-001").Staffing; staffing != nil {
				for _, row := range staffing.Agents {
					if row.Agent == "architect-arbiter" {
						t.Error("AC-6 a staffing row was written for the arbiter")
					}
				}
			}
			// The arbiter's review is taken in a staffed project.
			arAdd(t, "architect-arbiter", "approve", 1)
		})
	}
}

// arAddAt records a review at a gate by name, and reads what it says.
func arAddAt(t *testing.T, gate model.Gate, name, verdict string, i int) arReview {
	t.Helper()
	args := []string{"review", "add", string(gate), name, verdict, "--note", "round " + strconv.Itoa(i), "--json"}
	if verdict == "block" {
		args = append(args, "--important", "1", "--finding", "important the one finding")
	}
	return decode[arReview](t, mustRunWith(t, "# "+name+"\n", args...))
}

// arRefusedAt is a review at a gate that is refused and records nothing, in the
// record or in the reviews directory. It returns what the refusal said.
func arRefusedAt(t *testing.T, root string, gate model.Gate, name, verdict string) string {
	t.Helper()
	before := dcRecordText(t, root)
	files := hoReviewFiles(t, root)
	args := []string{"review", "add", string(gate), name, verdict}
	if verdict == "block" {
		args = append(args, "--important", "1", "--finding", "important the one finding")
	}
	r := runWith(t, "# "+name+"\n", args...)
	if r.code == 0 {
		t.Errorf("AC-6 %s's %s at %s was recorded:\n%s", name, verdict, gate, r.stdout)
	}
	if after := dcRecordText(t, root); after != before {
		t.Errorf("AC-6 the refused %s by %s at %s changed the record", verdict, name, gate)
	}
	if after := hoReviewFiles(t, root); strings.Join(after, ",") != strings.Join(files, ",") {
		t.Errorf("AC-6 the refused %s by %s at %s left a review file", verdict, name, gate)
	}
	return r.stderr
}

// arStanding is (*model.Record).StandingReview(gate, role), asked through the
// method set so that this package compiles before the method is written. The
// test plan names it.
func arStanding(t *testing.T, record *model.Record, gate model.Gate, role string) (model.Review, bool) {
	t.Helper()
	m := reflect.ValueOf(record).MethodByName("StandingReview")
	if !m.IsValid() {
		t.Fatal("AC-6 Record has no StandingReview method: " +
			"func (r *Record) StandingReview(g Gate, role string) (Review, bool)")
	}
	f, ok := reflect.TypeAssert[func(model.Gate, string) (model.Review, bool)](m)
	if !ok {
		t.Fatalf("AC-6 StandingReview is %s, want func(Gate, string) (Review, bool)", m.Type())
	}
	return f(gate, role)
}

// Security F-1 of the design review: the limit bounds the reviewer. From its
// limit-th block until its arbiter has reviewed, the reviewer's own name is
// refused, or the runbook could send the work back to it until it approved and
// pass the gate with neither the arbiter nor a person.
func TestAC6TheReviewerIsRefusedFromItsLimitThBlockUntilItsArbiterReviews(t *testing.T) {
	t.Parallel()
	root := planned(t)
	if first := arAdd(t, "architect", "block", 1); first.Arbiter != "" || first.HandedOver != "" {
		t.Fatalf("AC-6 the first block went somewhere: %+v", first)
	}
	if second := arAdd(t, "architect", "block", 2); second.Arbiter != "architect-arbiter" || second.HandedOver != "" {
		t.Fatalf("AC-6 the architect's limit-th block was not taken and sent to architect-arbiter: %+v", second)
	}
	for _, verdict := range []string{"approve", "block"} {
		refusal := arRefusedAt(t, root, model.GateDesignReview, "architect", verdict)
		if !strings.Contains(refusal, "SDLC-E0027") || !strings.Contains(refusal, "architect-arbiter") {
			t.Errorf("AC-6 the architect's %s past its limit was not refused with SDLC-E0027 naming architect-arbiter:\n%s",
				verdict, refusal)
		}
	}
	// The arbiter it names is the one whose review is taken.
	if got := arAdd(t, "architect-arbiter", "approve", 1); got.Role != "architect-arbiter" {
		t.Errorf("AC-6 the arbiter's review is recorded as %q", got.Role)
	}
}

// arPausedBacklog is a backlog of one high-tier story, which a project that
// pauses that tier holds to a person's pre-commit approval.
const arPausedBacklog = `{"stories":[
  {"id":"US-001","title":"Refunds","status":"ready","risk_tier":"high","priority":1,
   "acceptance_criteria":[{"id":"AC-1","text":"WHEN a paid invoice is refunded, the money goes back"}]}]}`

// Architect F-1 of the design review: an arbiter's standing ends only at a
// person's answer to a hand-over of that gate's reviews. A high-tier story is
// asked pre_commit_approval after Gates 6 and 7; were that answer the end of
// the standing, the block the arbiter overruled would stand again, refuse the
// commit gate, and the reviewer could reopen what the arbiter settled.
func TestAC6AnArbitersStandingSurvivesAPersonsAnswerThatIsNotAboutItsGatesReviews(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name       string
		gate       model.Gate
		role, kind string
		paused     bool
	}{
		{"AC-6 code_review then pre_commit_approval", model.GateCodeReview, "code-reviewer", "pre_commit_approval", true},
		{"AC-6 verifier_review then pre_commit_approval", model.GateVerifierReview, "verifier", "pre_commit_approval", true},
		{"AC-6 code_review then agent_failed", model.GateCodeReview, "code-reviewer", model.KindAgentFailed, false},
		{"AC-6 code_review then loop_stalled", model.GateCodeReview, "code-reviewer", "loop_stalled", false},
		{"AC-6 code_review then work_by_hand", model.GateCodeReview, "code-reviewer", model.KindWorkByHand, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var root string
			if c.paused {
				root = gitProjectInitialised(t)
				writeFile(t, root, "user_stories.json", arPausedBacklog)
				mustRun(t, "start")
				reach(t, root, c.gate)
			} else {
				root = startedAt(t, c.gate)
			}
			arbiter := c.role + "-arbiter"
			for i := 1; i <= 2; i++ {
				got := arAddAt(t, c.gate, c.role, "block", i)
				if want := map[bool]string{true: arbiter}[i == 2]; got.Arbiter != want || got.HandedOver != "" {
					t.Fatalf("AC-6 %s's block %d at %s: %+v, want the second to send the gate to %s", c.role, i, c.gate, got, arbiter)
				}
			}
			arAddAt(t, c.gate, arbiter, "approve", 1)
			// The runbook declines what the arbiter overruled, so that no open
			// finding holds the commit gate in place of the reviews.
			for _, rev := range readRecord(t, root, "US-001").Reviews {
				if rev.Gate != c.gate || rev.Role != c.role {
					continue
				}
				for _, f := range rev.Findings {
					mustRun(t, "finding", "close", string(c.gate), c.role, f.ID, "--declined", "--reason", "the arbiter overruled it")
				}
			}
			approveAll(t, c.gate, c.role)
			mustRun(t, "gate", string(c.gate), "pass", "--note", "the arbiter ruled")
			for _, gate := range model.Gates[slices.Index(model.Gates, c.gate)+1 : slices.Index(model.Gates, model.GateCommit)] {
				satisfy(t, root, gate)
				mustRun(t, "gate", string(gate), "pass", "--note", "reached by the test")
			}
			mustRun(t, "escalate", c.kind, "--message", "ready to commit?")
			mustRun(t, "approve", "US-001")
			mustRun(t, "start")

			record := readRecord(t, root, "US-001")
			if got, ok := arStanding(t, &record, c.gate, c.role); !ok || got.Role != arbiter || got.Verdict != model.VerdictApprove {
				t.Errorf("AC-6 after a person's answer to %s the review that stands for %s at %s is %+v (%v), want %s's approval",
					c.kind, c.role, c.gate, got, ok, arbiter)
			}
			for _, verdict := range []string{"approve", "block"} {
				if refusal := arRefusedAt(t, root, c.gate, c.role, verdict); !strings.Contains(refusal, "SDLC-E0027") {
					t.Errorf("AC-6 after a person's answer to %s, %s's %s at %s was not refused with SDLC-E0027:\n%s",
						c.kind, c.role, verdict, c.gate, refusal)
				}
			}
			// Nothing need have changed since the last commit.
			wtCommit(t, root, "story")
			if r := run(t, "gate", "commit", "pass", "--note", "on trunk"); r.code != 0 || strings.Contains(r.stderr, c.role+" blocked") {
				t.Errorf("AC-6 after a person's answer to %s the commit gate was held to %s's overruled block:\n%s",
					c.kind, c.role, r.stderr)
			}
		})
	}
}
