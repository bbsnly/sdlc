package cli

// US-096 AC-9 (from US-097) where the refusal is made: every fix of
// SDLC-E0037 -- at the commit gate, at the stop for work changed since its
// approval with an iteration running and with none, and for the story's
// commits -- says what a person types and, apart from it, what an assistant
// does, with no literal "..." in a command a person would paste. And the
// scout findings of ANALYSIS.md D7 in the files the story changes: the
// landing's and status's fixes paste no "..." either. The AC numbers in this
// file are US-096's. Helpers carry an af prefix.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
	"github.com/bbsnly/sdlc/internal/store"
)

// afAssistant is where a fix turns to the assistant: a sentence, or a clause
// after a semicolon, that begins with it.
var afAssistant = regexp.MustCompile(`(?:^|[.;] )[Aa]n assistant\b[^.]*(?:\.|$)`)

// afNoDots fails where text prints a literal "...", which a person pastes as it
// stands.
func afNoDots(t *testing.T, where, text string) {
	t.Helper()
	for _, dots := range []string{"...", "…"} {
		if strings.Contains(text, dots) {
			t.Errorf("%s prints a literal %q in what a person pastes:\n%s", where, dots, text)
		}
	}
}

// afPersonAndAssistant holds an SDLC-E0037 fix to AC-9: no "...", a part of its
// own for an assistant that hands the story over with `sdlc escalate` and
// stops, and, outside it, what a person types to answer, slash command first,
// with the story named.
func afPersonAndAssistant(t *testing.T, where, fix, id string) {
	t.Helper()
	flat := cmFlat(fix)
	afNoDots(t, where, flat)
	at := afAssistant.FindStringIndex(flat)
	if at == nil {
		t.Errorf("%s has no part of its own for an assistant:\n%s", where, flat)
		return
	}
	person, assistant := flat[:at[0]]+flat[at[1]:], flat[at[0]:at[1]]
	if !strings.Contains(assistant, "sdlc escalate") {
		t.Errorf("%s does not tell an assistant to hand the story over with sdlc escalate:\n%s", where, assistant)
	}
	if strings.Contains(person, "sdlc escalate") {
		t.Errorf("%s sets the assistant's sdlc escalate among what a person does:\n%s", where, person)
	}
	if !strings.Contains(person, `"/sdlc:approve `+id+`", or "sdlc approve `+id+`"`) || !strings.Contains(strings.ToLower(person), "a person") {
		t.Errorf("%s does not say what a person types to answer, /sdlc:approve %s first:\n%s", where, id, person)
	}
}

// AC-9: at the commit gate, a story in a tier that waits for a person and that
// nobody approved is refused with the catalogue's fix, which holds to AC-9.
func TestAC9TheCommitGatesSDLCE0037FixSaysWhatAPersonTypesAndApartWhatAnAssistantDoes(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"PAY-1","title":"Refunds","status":"ready","risk_tier":"High","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN a paid invoice is refunded, the money goes back"}]}]}`)
	mustRun(t, "start")
	reach(t, root, model.GateCommit)
	commitEverything(t, root)
	r := run(t, "gate", "commit", "pass", "--note", "on trunk")
	if r.code == 0 {
		t.Fatal("a high-risk story was committed with nobody asked")
	}
	_, fix := ldRefusal(t, r.stdout+r.stderr, ldNotApproved)
	afPersonAndAssistant(t, "the commit gate's SDLC-E0037", fix, "<ID>")
}

// AC-9: at the stop, the work changed since a person approved it, with the
// iteration running: the fix holds to AC-9 and still names the worktree.
func TestAC9TheStopsSDLCE0037FixForWorkChangedSinceItsApprovalSaysWhatAPersonTypesAndApartWhatAnAssistantDoes(t *testing.T) {
	t.Parallel()
	_, wt := ldWalked(t, ldWalk{tier: "high"})
	ldWork(t, wt)
	swPass(t, wt, ldSession, model.GateImplementation, model.GateCommit)
	ldApproved(t, wt)
	swPass(t, wt, ldSession, model.GateCommit, "")
	ldBookkeeping(t, wt)
	lxChangeAfterTheReviews(t, wt)
	ldReviewAgain(t, wt)

	out := swRefused(t, ldNotApproved, wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	afPersonAndAssistant(t, "the stop's SDLC-E0037 with the iteration running", fix, "US-001")
	suSays(t, cmFlat(fix), worktreeIn("US-001"))
}

// AC-9: at the stop, the work changed after a person's answer and no
// iteration running, and then at the commit gate that answer's way back in
// leads to: both fixes hold to AC-9.
func TestAC9TheSDLCE0037FixesWithNoIterationRunningSayWhatAPersonTypesAndApartWhatAnAssistantDoes(t *testing.T) {
	t.Parallel()
	s := ccFinished(t)
	ccHandedOverAndApproved(t, s.wt)
	lxChangeAfterTheReviews(t, s.wt)
	ldBookkeeping(t, s.wt)
	ldMoveTrunk(t, s.main)
	cmReviewAgain(t, s.wt)

	out := swRefused(t, ldNotApproved, s.wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	afPersonAndAssistant(t, "the stop's SDLC-E0037 with no iteration running", fix, "US-001")
	printed := cmFixCommand(t, cmFlat(fix), "sdlc gate commit fail --story US-001 --note ")
	note := strings.Trim(strings.TrimPrefix(printed, "sdlc gate commit fail --story US-001 --note "), "'")
	wtMust(t, s.wt, ldSession, "", "gate", "commit", "fail", "--story", "US-001", "--note", note)
	swStarted(t, s.wt, ldSession)
	gate := swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "the work as it now is")
	_, gateFix := ldRefusal(t, gate, ldNotApproved)
	afPersonAndAssistant(t, "the commit gate's SDLC-E0037 after the way back in", gateFix, "<ID>")
}

// AC-9: the landing's refusal for commits made again after the commit gate,
// with no iteration running, and the commit gate's for the same commits once
// the story is taken up again: both fixes hold to AC-9.
func TestAC9TheSDLCE0037FixesForTheStorysCommitsSayWhatAPersonTypesAndApartWhatAnAssistantDoes(t *testing.T) {
	t.Parallel()
	s := cmHandedOverThenChanged(t)
	out := swRefused(t, ldNotApproved, s.wt, ldSession, "stop")
	_, fix := ldRefusal(t, out, ldNotApproved)
	afPersonAndAssistant(t, "the landing's SDLC-E0037 for the story's commits", fix, "US-001")
	printed := cmFixCommand(t, cmFlat(fix), "sdlc gate commit fail --story US-001 --note ")
	note := strings.Trim(strings.TrimPrefix(printed, "sdlc gate commit fail --story US-001 --note "), "'")
	wtMust(t, s.wt, ldSession, "", "gate", "commit", "fail", "--story", "US-001", "--note", note)
	swStarted(t, s.wt, ldSession)
	gate := swRefused(t, ldNotApproved, s.wt, ldSession, "gate", "commit", "pass", "--note", "again")
	_, gateFix := ldRefusal(t, gate, ldNotApproved)
	afPersonAndAssistant(t, "the commit gate's SDLC-E0037 for the story's commits", gateFix, "US-001")
}

// ANALYSIS D7, the scout rule: the landing's fixes that send a person back
// into a finished story -- the way back in, the reopen, the freeze's, and the
// one for a finished story -- paste no literal "...".
func TestD7TheLandingsFixesPasteNoDots(t *testing.T) {
	t.Parallel()
	l := &lander{id: "US-100", m: mainSettings{trunk: "main"}}
	for where, fix := range map[string]string{
		"SDLC-E0033's fix with no iteration running": waitingToLand("US-100", "main", false).Fix,
		"the reopen's fix":                           l.reopenFix(),
		"the way back in":                            reopenRoute("US-100", model.GateCodeReview),
		"the freeze's way back in":                   unfreezeRoute("US-100"),
	} {
		afNoDots(t, where, fix)
	}
}

// ANALYSIS D7: status's closing line for a story held by a checkout that is
// gone pastes no literal "..." either.
func TestD7StatusSaysHowToMoveAStoryFromAGoneCheckoutWithoutDots(t *testing.T) {
	t.Parallel()
	holders := map[string]store.Holder{"US-001": {Checkout: "/gone/checkout", Claimed: true}}
	_, next := notHeldHere("US-001", false, holders, true, true)
	if next == "" {
		t.Fatal("status says nothing of a story held by a checkout that is gone")
	}
	afNoDots(t, "status's line for a story held by a gone checkout", next)
	suSays(t, next, "`/sdlc:release US-001 --reason ", "`sdlc release US-001 --reason ")
}
