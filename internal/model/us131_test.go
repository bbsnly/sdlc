package model

import (
	"reflect"
	"testing"
	"time"
)

// US-131 AC-6 and the settled IQ-4: an arbiter is a fresh instance of a reviewer's
// role, recorded as <role>-arbiter (or <role>-<n>-arbiter for an instance), and
// its review stands for that role at that gate in place of the reviewer's
// latest. Both readings are the closed one InstanceOf is: a name the hook and
// sdlc read differently would let a review through one of them.

func TestAC6AnArbitersNameReadsAsItsRolesReviewerAtItsRolesGate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		gate       Gate
		name, role string
		base       string
	}{
		{GateDesignReview, "architect-arbiter", "architect-arbiter", "architect"},
		// FindReviewer lowercases and trims, so the hook and sdlc read these alike.
		{GateDesignReview, "Architect-Arbiter", "architect-arbiter", "architect"},
		{GateDesignReview, " architect-arbiter ", "architect-arbiter", "architect"},
		{GateVerifierReview, "verifier-arbiter", "verifier-arbiter", "verifier"},
		{GateCodeReview, "code-reviewer-arbiter", "code-reviewer-arbiter", "code-reviewer"},
		{GateCodeReview, "perf-arbiter", "perf-arbiter", "perf"},
		{GateCodeReview, "security-2-arbiter", "security-2-arbiter", "security"},
	} {
		t.Run("AC-6 "+c.name, func(t *testing.T) {
			t.Parallel()
			r, ok := FindReviewer(c.gate, c.name)
			if !ok {
				t.Fatalf("AC-6 %s does not expect a review from %q", c.gate, c.name)
			}
			if r.Role != c.role {
				t.Errorf("AC-6 %q is recorded as %q, want %q", c.name, r.Role, c.role)
			}
			if got := BaseRole(r.Role); got != c.base {
				t.Errorf("AC-6 BaseRole(%q) = %q, want %q: the arbiter reviews with its role's power", r.Role, got, c.base)
			}
			role, _ := FindReviewer(c.gate, c.base)
			if r.Blocking != role.Blocking || r.BlocksOnBreach != role.BlocksOnBreach || r.WhenSecuritySensitive != role.WhenSecuritySensitive {
				t.Errorf("AC-6 %q blocks otherwise than %s: %+v against %+v", c.name, c.base, r, role)
			}
		})
	}
}

// T-1 of the threats: an arbiter stands only in place of its own role, at its
// own gate, and a name that is not a role's arbiter is no name. The real one is
// read first, so the refusals below are of names beside one the roster knows.
func TestAC6ANameThatIsNotARolesArbiterAtThatGateIsNoReviewer(t *testing.T) {
	t.Parallel()
	if r, ok := FindReviewer(GateDesignReview, "architect-arbiter"); !ok || BaseRole(r.Role) != "architect" {
		t.Fatalf("AC-6 design_review does not read architect-arbiter as the architect's arbiter: %+v, %v", r, ok)
	}
	for _, c := range []struct {
		gate Gate
		name string
	}{
		{GateVerifierReview, "architect-arbiter"},
		{GateCodeReview, "architect-arbiter"},
		{GateDesignReview, "code-reviewer-arbiter"},
		{GateDesignReview, "verifier-arbiter"},
		{GateDesignReview, "researcher-arbiter"},
		{GateDesignReview, "implementer-arbiter"},
		{GateDesignReview, "arbiter"},
		{GateDesignReview, "-arbiter"},
		{GateDesignReview, "architect-arbiter-arbiter"},
		{GateDesignReview, "architect-arbiter-2"},
		{GateDesignReview, "architect-1-arbiter"},
		{GateDesignReview, "architect-02-arbiter"},
		{GateDesignReview, "architectarbiter"},
		{GateDesignReview, "architect_arbiter"},
	} {
		t.Run("AC-6 "+string(c.gate)+" "+c.name, func(t *testing.T) {
			t.Parallel()
			if r, ok := FindReviewer(c.gate, c.name); ok {
				t.Errorf("AC-6 %s expects a review from %q, read as %+v", c.gate, c.name, r)
			}
		})
	}
	// An agent that reviews nothing has no arbiter to stand for it.
	if got := BaseRole("researcher-arbiter"); got == "researcher" {
		t.Error("AC-6 researcher-arbiter reads as the researcher, which reviews nothing")
	}
}

// standingReview is (*Record).StandingReview(gate, role), the one rule for which
// review stands for a role at a gate (IQ-4), asked through the method set so that
// this package compiles before the method is written. The test plan names it.
func standingReview(t *testing.T, r *Record, g Gate, role string) (Review, bool) {
	t.Helper()
	m := reflect.ValueOf(r).MethodByName("StandingReview")
	if !m.IsValid() {
		t.Fatal("AC-6 Record has no StandingReview method: the review that stands for a role at a gate is decided in one place, " +
			"func (r *Record) StandingReview(g Gate, role string) (Review, bool)")
	}
	f, ok := reflect.TypeAssert[func(Gate, string) (Review, bool)](m)
	if !ok {
		t.Fatalf("AC-6 StandingReview is %s, want func(Gate, string) (Review, bool)", m.Type())
	}
	return f(g, role)
}

// a131 builds a record the way the commands do: each review with its event, and
// a person's answer as `sdlc approve` writes it, each a minute after the last.
type a131 struct {
	r  *Record
	at time.Time
}

func newA131() *a131 {
	at := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	return &a131{r: NewRecord("US-1", at), at: at}
}

func (b *a131) review(g Gate, role string, v Verdict, note string) *a131 {
	b.at = b.at.Add(time.Minute)
	b.r.AddReview(Review{Gate: g, Role: role, Verdict: v, Subject: "tree", Note: note}, b.at)
	b.r.Append(EventReview, ReviewEvent(g, role, v), b.at)
	return b
}

func (b *a131) answered() *a131 {
	b.at = b.at.Add(time.Minute)
	b.r.Escalate("review_not_converging", "the arbiter kept blocking", "tree", b.at)
	b.at = b.at.Add(time.Minute)
	if a, ok := b.r.Decide(true, "", "tree", b.at); ok {
		b.r.Append(EventApproval, ApprovalEvent(a.Decision, a.Type), b.at)
	}
	return b
}

func TestAC6TheArbitersLatestReviewStandsForItsRoleSinceAPersonLastAnswered(t *testing.T) {
	t.Parallel()
	const architect, arbiter = "architect", "architect-arbiter"
	for _, c := range []struct {
		name string
		make func() *a131
		gate Gate
		role string
		// want is the note of the review that stands, "" where none does.
		want string
	}{
		{"AC-6 no review", newA131, GateDesignReview, architect, ""},
		{"AC-6 the reviewer's own, with no arbiter", func() *a131 {
			return newA131().review(GateDesignReview, architect, VerdictBlock, "first").review(GateDesignReview, architect, VerdictApprove, "second")
		}, GateDesignReview, architect, "second"},
		{"AC-6 the arbiter's in place of the reviewer's blocks", func() *a131 {
			return newA131().review(GateDesignReview, architect, VerdictBlock, "b1").review(GateDesignReview, architect, VerdictBlock, "b2").
				review(GateDesignReview, arbiter, VerdictApprove, "ruled")
		}, GateDesignReview, architect, "ruled"},
		{"AC-6 the arbiter's latest, a block after its approval", func() *a131 {
			return newA131().review(GateDesignReview, architect, VerdictBlock, "b1").review(GateDesignReview, architect, VerdictBlock, "b2").
				review(GateDesignReview, arbiter, VerdictApprove, "a1").review(GateDesignReview, arbiter, VerdictBlock, "a2")
		}, GateDesignReview, architect, "a2"},
		{"AC-6 a person's answer ends the arbiter's standing", func() *a131 {
			return newA131().review(GateDesignReview, architect, VerdictBlock, "b1").review(GateDesignReview, architect, VerdictBlock, "b2").
				review(GateDesignReview, arbiter, VerdictBlock, "a1").answered()
		}, GateDesignReview, architect, "b2"},
		{"AC-6 the reviewer's after a person's answer", func() *a131 {
			return newA131().review(GateDesignReview, architect, VerdictBlock, "b1").review(GateDesignReview, architect, VerdictBlock, "b2").
				review(GateDesignReview, arbiter, VerdictBlock, "a1").answered().review(GateDesignReview, architect, VerdictApprove, "fresh")
		}, GateDesignReview, architect, "fresh"},
		{"AC-6 an arbiter after a person's answer stands again", func() *a131 {
			return newA131().review(GateDesignReview, architect, VerdictBlock, "b1").answered().
				review(GateDesignReview, architect, VerdictBlock, "b2").review(GateDesignReview, architect, VerdictBlock, "b3").
				review(GateDesignReview, arbiter, VerdictApprove, "ruled")
		}, GateDesignReview, architect, "ruled"},
		{"AC-6 another role's arbiter does not stand for this one", func() *a131 {
			return newA131().review(GateDesignReview, architect, VerdictBlock, "b1").review(GateDesignReview, "red-team-arbiter", VerdictApprove, "other")
		}, GateDesignReview, architect, "b1"},
		{"AC-6 an arbiter at another gate does not stand at this one", func() *a131 {
			return newA131().review(GateCodeReview, "security", VerdictBlock, "code").review(GateDesignReview, "security-arbiter", VerdictApprove, "design")
		}, GateCodeReview, "security", "code"},
		{"AC-6 an instance's arbiter stands for the instance", func() *a131 {
			return newA131().review(GateCodeReview, "security-2", VerdictBlock, "s2").review(GateCodeReview, "security-2-arbiter", VerdictApprove, "ruled")
		}, GateCodeReview, "security-2", "ruled"},
		{"AC-6 an instance's arbiter does not stand for the role's first instance", func() *a131 {
			return newA131().review(GateCodeReview, "security", VerdictApprove, "s1").
				review(GateCodeReview, "security-2", VerdictBlock, "s2").review(GateCodeReview, "security-2-arbiter", VerdictApprove, "ruled")
		}, GateCodeReview, "security", "s1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := standingReview(t, c.make().r, c.gate, c.role)
			switch {
			case c.want == "" && ok:
				t.Errorf("a review stands where none was recorded: %+v", got)
			case c.want != "" && !ok:
				t.Errorf("no review stands, want the one noted %q", c.want)
			case got.Note != c.want:
				t.Errorf("the review that stands is %s's %s noted %q, want the one noted %q", got.Role, got.Verdict, got.Note, c.want)
			}
		})
	}
}

// T-2 of the threats: a decision is not a person's answer, so it neither ends an
// arbiter's standing nor starts a reviewer's count again.
func TestAC6ADecisionEventIsNotAPersonsAnswer(t *testing.T) {
	t.Parallel()
	b := newA131().review(GateDesignReview, "architect", VerdictBlock, "b1")
	b.at = b.at.Add(time.Minute)
	b.r.Append("decision", "design_review architect-arbiter: decline F-1", b.at)
	b.review(GateDesignReview, "architect", VerdictBlock, "b2")
	if n := b.r.SinceDecision(EventReview, ReviewEvent(GateDesignReview, "architect", VerdictBlock)); n != 2 {
		t.Errorf("AC-6 the architect's blocks since a person answered = %d, want 2: a decision is no answer", n)
	}
	b.review(GateDesignReview, "architect-arbiter", VerdictApprove, "ruled")
	b.at = b.at.Add(time.Minute)
	b.r.Append("decision", "design_review architect-arbiter: decline F-1", b.at)
	if got, ok := standingReview(t, b.r, GateDesignReview, "architect"); !ok || got.Note != "ruled" {
		t.Errorf("AC-6 a decision ended the arbiter's standing: %+v %v", got, ok)
	}
	if _, ok := b.r.LatestDecision(); ok {
		t.Error("AC-6 a decision event reads as a person's answer")
	}
}

// handOver is a hand-over as the store writes it: the escalation and its
// escalate event, in one save.
func (b *a131) handOver(kind string) *a131 {
	b.at = b.at.Add(time.Minute)
	b.r.Escalate(kind, "asked", "tree", b.at)
	b.r.Append(EventEscalate, kind+": asked", b.at)
	return b
}

// answer is a person's answer as `sdlc approve` writes it, to whatever is
// pending.
func (b *a131) answer() *a131 {
	b.at = b.at.Add(time.Minute)
	if a, ok := b.r.Decide(true, "", "tree", b.at); ok {
		b.r.Append(EventApproval, ApprovalEvent(a.Decision, a.Type), b.at)
	}
	return b
}

// gate is the design review's outcome, as `sdlc gate` writes it.
func (b *a131) gate(s GateStatus) *a131 {
	b.at = b.at.Add(time.Minute)
	b.r.Append(EventGate, GateEvent(GateDesignReview, s), b.at)
	return b
}

// The arbiter's standing ends at a person's answer to a hand-over of its role's
// dispute at the gate, read from the escalate events a hand-over writes, and at
// no other answer (architect F-1 of the design review).
func TestAC6AnArbitersStandingIsReadFromTheHandOversAPersonsAnswerResolved(t *testing.T) {
	t.Parallel()
	const architect, arbiter = "architect", "architect-arbiter"
	// ruled is the architect at its limit and its arbiter's approval.
	ruled := func() *a131 {
		return newA131().review(GateDesignReview, architect, VerdictBlock, "b1").review(GateDesignReview, architect, VerdictBlock, "b2").
			review(GateDesignReview, arbiter, VerdictApprove, "ruled")
	}
	// disputed is the arbiter at its own limit, handed over.
	disputed := func() *a131 {
		return ruled().review(GateDesignReview, arbiter, VerdictBlock, "a1").review(GateDesignReview, arbiter, VerdictBlock, "a2").
			handOver(KindReviewNotConverging)
	}
	for _, c := range []struct {
		name string
		make func() *a131
		want string
	}{
		{"AC-6 the answer to the arbiter's hand-over ends its standing", func() *a131 { return disputed().answer() }, "b2"},
		{"AC-6 a hand-over nobody has answered leaves the arbiter standing", disputed, "a2"},
		{"AC-6 a pre-commit answer leaves the arbiter standing", func() *a131 {
			return ruled().gate(GatePass).handOver("pre_commit_approval").answer()
		}, "ruled"},
		{"AC-6 an answer to another role's dispute leaves the arbiter standing", func() *a131 {
			return ruled().review(GateDesignReview, "security", VerdictBlock, "s1").handOver(KindReviewNotConverging).answer()
		}, "ruled"},
		{"AC-6 an answer to the dispute at another gate leaves the arbiter standing", func() *a131 {
			return ruled().review(GateCodeReview, arbiter, VerdictBlock, "c1").handOver(KindReviewNotConverging).answer()
		}, "ruled"},
		{"AC-6 a hand-over of a kind that is not the review's leaves the arbiter standing", func() *a131 {
			return ruled().review(GateDesignReview, arbiter, VerdictBlock, "a1").handOver("gate_failing").answer()
		}, "a1"},
		{"AC-6 the answer to the reviewer's own hand-over ends the arbiter's standing", func() *a131 {
			return ruled().review(GateDesignReview, architect, VerdictBlock, "b3").handOver(KindReviewNotConverging).answer()
		}, "b3"},
		{"AC-6 a hand-over stacked on the review's does not hide it from the answer", func() *a131 {
			return disputed().gate(GateFail).handOver("gate_failing").answer()
		}, "b2"},
		{"AC-6 a later pre-commit answer does not undo an earlier answer to the review's", func() *a131 {
			return disputed().answer().gate(GatePass).handOver("pre_commit_approval").answer()
		}, "b2"},
		{"AC-6 an arbiter that reviews after the answer stands again", func() *a131 {
			return disputed().answer().review(GateDesignReview, architect, VerdictBlock, "b3").review(GateDesignReview, architect, VerdictBlock, "b4").
				review(GateDesignReview, arbiter, VerdictApprove, "ruled again")
		}, "ruled again"},
		{"AC-6 a record from before escalate events, then a pre-commit answer", func() *a131 {
			return ruled().review(GateDesignReview, arbiter, VerdictBlock, "a1").answered().
				gate(GatePass).handOver("pre_commit_approval").answer()
		}, "b2"},
		{"AC-6 of two answers from before escalate events, the later one ends the standing", func() *a131 {
			return ruled().review(GateDesignReview, arbiter, VerdictBlock, "a1").answered().
				review(GateDesignReview, architect, VerdictBlock, "b3").review(GateDesignReview, architect, VerdictBlock, "b4").
				review(GateDesignReview, arbiter, VerdictBlock, "a2").answered()
		}, "b4"},
		{"AC-6 the findings a block lists between it and its hand-over do not hide which block raised it", func() *a131 {
			b := ruled().review(GateDesignReview, arbiter, VerdictBlock, "a1")
			b.at = b.at.Add(time.Minute)
			b.r.Append(EventFindings, "design_review architect-arbiter listed F-1 to F-1", b.at)
			return b.handOver(KindReviewNotConverging).answer()
		}, "b2"},
		{"AC-6 a hand-over that is the record's first event was raised by no block", func() *a131 {
			b := newA131().handOver(KindReviewNotConverging).answer()
			return b.review(GateDesignReview, architect, VerdictBlock, "b1").review(GateDesignReview, architect, VerdictBlock, "b2").
				review(GateDesignReview, arbiter, VerdictApprove, "ruled")
		}, "ruled"},
		{"AC-6 an approval event with no answer to pair with is no answer", func() *a131 {
			b := ruled().review(GateDesignReview, arbiter, VerdictBlock, "a1")
			b.at = b.at.Add(time.Minute)
			b.r.Append(EventApproval, ApprovalEvent(DecisionApproved, KindReviewNotConverging), b.at)
			return b
		}, "a1"},
		{"AC-6 an escalate event with no escalation to pair with is no hand-over", func() *a131 {
			b := ruled().handOver("pre_commit_approval").review(GateDesignReview, arbiter, VerdictBlock, "a1")
			b.at = b.at.Add(time.Minute)
			b.r.Append(EventEscalate, KindReviewNotConverging+": asked", b.at)
			return b.answer()
		}, "a1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, ok := standingReview(t, c.make().r, GateDesignReview, architect)
			if !ok || got.Note != c.want {
				t.Errorf("the review that stands is %s's %s noted %q (%v), want the one noted %q", got.Role, got.Verdict, got.Note, ok, c.want)
			}
		})
	}
}

func TestAC3ADecisionsMessageIsItsGateWhoAndTheQuestion(t *testing.T) {
	t.Parallel()
	c := Choice{Gate: GateAnalysis, By: "researcher", Question: "Which store holds it", Options: []string{"a", "b"}, Chosen: "b", Reason: "r"}
	if got, want := DecisionMessage(c), "analysis researcher: Which store holds it"; got != want {
		t.Errorf("AC-3 DecisionMessage = %q, want %q", got, want)
	}
	done := Choice{Gate: AfterLastGate, By: "bookkeeper", Question: "Q"}
	if got, want := DecisionMessage(done), "done bookkeeper: Q"; got != want {
		t.Errorf("AC-3 DecisionMessage at done = %q, want %q", got, want)
	}
}

// An sdlc older than the decision event keeps its type and message and drops
// the choice, so Decision reads the message where the choice is gone.
func TestAC3ADecisionIsReadFromItsChoiceOrFromItsMessageWhereTheChoiceIsGone(t *testing.T) {
	t.Parallel()
	full := Choice{Gate: GateDesignReview, By: "architect-arbiter", Question: "Decline F-1", Options: []string{"add a step", "decline F-1"},
		Chosen: "decline F-1", Reason: "step 2 covers it"}
	if got := (Event{Type: EventDecision, Message: DecisionMessage(full), Choice: &full}).Decision(); !reflect.DeepEqual(got, full) {
		t.Errorf("AC-3 a decision with its choice reads as %+v, want %+v", got, full)
	}
	for _, c := range []struct {
		name, message string
		gate          Gate
	}{
		{"AC-3 a gate's decision", "design_review architect-arbiter: Decline F-1", GateDesignReview},
		{"AC-3 a decision once every gate passed", "done bookkeeper: Keep the log", AfterLastGate},
		{"AC-3 a first word that is no gate", "later researcher: Something", ""},
		{"AC-3 one word", "question", ""},
		{"AC-3 nothing", "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Event{Type: EventDecision, Message: c.message}.Decision()
			want := Choice{Gate: c.gate, Question: c.message}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Decision with no choice = %+v, want %+v", got, want)
			}
		})
	}
}

// The arbiter rule covers the test reviewer too, a person's decision on the
// story: test-reviewer-arbiter, and an instance's arbiter, review at the tests
// gate with the test reviewer's power, and at no other gate.
func TestAC6TheTestReviewersArbiterReadsAsItsReviewerAtTheTestsGateOnly(t *testing.T) {
	t.Parallel()
	role, ok := FindReviewer(GateTestsFrozen, "test-reviewer")
	if !ok {
		t.Fatal("AC-6 tests_frozen expects no review from test-reviewer")
	}
	for _, c := range []struct{ name, recorded, base string }{
		{"test-reviewer-arbiter", "test-reviewer-arbiter", "test-reviewer"},
		{" Test-Reviewer-Arbiter ", "test-reviewer-arbiter", "test-reviewer"},
		{"test-reviewer-2-arbiter", "test-reviewer-2-arbiter", "test-reviewer"},
	} {
		r, ok := FindReviewer(GateTestsFrozen, c.name)
		if !ok || r.Role != c.recorded || BaseRole(r.Role) != c.base {
			t.Errorf("AC-6 FindReviewer(tests_frozen, %q) = %+v, %v, want %s standing for %s", c.name, r, ok, c.recorded, c.base)
			continue
		}
		if r.Blocking != role.Blocking || r.BlocksOnBreach != role.BlocksOnBreach {
			t.Errorf("AC-6 %q blocks otherwise than the test reviewer: %+v against %+v", c.name, r, role)
		}
	}
	for _, g := range []Gate{GateDesignReview, GateVerifierReview, GateCodeReview} {
		if r, ok := FindReviewer(g, "test-reviewer-arbiter"); ok {
			t.Errorf("AC-6 %s expects a review from test-reviewer-arbiter, read as %+v", g, r)
		}
	}
	for _, name := range []string{"test-reviewer-arbiter-arbiter", "sdet-arbiter", "test-arbiter"} {
		if r, ok := FindReviewer(GateTestsFrozen, name); ok {
			t.Errorf("AC-6 tests_frozen expects a review from %q, read as %+v", name, r)
		}
	}
}
