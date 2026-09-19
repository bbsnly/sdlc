package cli

import (
	"strings"
	"testing"
)

// A status the backlog invented -- by a hand edit, or an older build -- is
// still counted, rather than vanishing from a total a person might rely on.
func TestABacklogStatusOutsideTheNineIsStillCounted(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"US-001","title":"Something odd","status":"blocked","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN nothing, THEN nothing"}]}]}`)

	out := mustRun(t, "status").stdout
	if !strings.Contains(out, "1 blocked") {
		t.Errorf("a status outside the nine was not counted:\n%s", out)
	}
	if got := decode[statusPayload](t, mustRun(t, "status", "--json")); got.Backlog["blocked"] != 1 {
		t.Errorf("backlog = %v, want blocked counted", got.Backlog)
	}
}

// A project that tracks no budget and has spent nothing has no cost line to
// show: it never has to look at a "$0.00" it never asked to see.
func TestStatusSaysNothingAboutCostWithNoBudgetAndNoSpend(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setBudgetOnDisk(t, root, 0)
	mustRun(t, "start")

	out := mustRun(t, "status").stdout
	if strings.Contains(out, "cost") {
		t.Errorf("status mentions cost with no budget and nothing spent:\n%s", out)
	}
	if got := decode[statusPayload](t, mustRun(t, "status", "--json")); got.Cost != nil {
		t.Errorf("cost = %+v, want nil", got.Cost)
	}
}

// Spending something is worth saying even where no budget was ever set: "what
// did this cost" has an answer either way.
func TestStatusShowsTheSpendEvenWithNoBudgetSet(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setBudgetOnDisk(t, root, 0)
	mustRun(t, "start")
	mustRun(t, "cost", "add", "--usd", "3")

	out := mustRun(t, "status").stdout
	if !strings.Contains(out, "cost   $3.00") {
		t.Errorf("status does not show the spend with no budget set:\n%s", out)
	}
	if strings.Contains(out, "$3.00 of") {
		t.Errorf("status shows a budget that was never set:\n%s", out)
	}
}
