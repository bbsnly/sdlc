package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A loop that runs on its own spends money on its own, and until this existed
// `budget.per_story_usd` was written into every project's configuration,
// documented as what a story is expected to cost, and read by nothing.
func TestCostIsRecordedAgainstTheStory(t *testing.T) {
	gitProject(t)
	initialised(t)
	mustRun(t, "start")

	if got := mustRun(t, "cost").stdout; !strings.Contains(got, "$0.00") {
		t.Errorf("a story with nothing spent does not say so: %q", got)
	}
	mustRun(t, "cost", "add", "--usd", "1.42", "--note", "gate 4")
	mustRun(t, "cost", "add", "--usd", "0.58")

	p := decode[costPayload](t, mustRun(t, "cost", "--json"))
	if p.SpentUSD != 2 {
		t.Errorf("spent = %v, want 2", p.SpentUSD)
	}
	if p.BudgetUSD != 60 {
		t.Errorf("budget = %v, want the default 60", p.BudgetUSD)
	}

	// And status, which is what a runner and a session both read.
	s := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if s.Cost == nil || s.Cost.SpentUSD != 2 {
		t.Errorf("status does not report the spend: %+v", s.Cost)
	}
}

// The alert fires once, where it is crossed. An alert that repeated on every
// entry after it would be noise, and noise is how a warning stops being read.
func TestAnAlertFiresOnceWhenItIsCrossed(t *testing.T) {
	gitProject(t)
	initialised(t)
	mustRun(t, "start")

	// The default budget is $60, alerting at a half, four fifths and all of it.
	quiet := mustRun(t, "cost", "add", "--usd", "20")
	if quiet.stderr != "" {
		t.Errorf("an alert fired below the first fraction: %q", quiet.stderr)
	}
	half := mustRun(t, "cost", "add", "--usd", "15")
	if !strings.Contains(half.stderr, "50%") {
		t.Errorf("crossing half the budget said nothing: %q", half.stderr)
	}
	again := mustRun(t, "cost", "add", "--usd", "1")
	if strings.Contains(again.stderr, "50%") {
		t.Errorf("the same alert fired twice: %q", again.stderr)
	}

	// One entry can cross more than one fraction, and both are worth saying.
	rest := mustRun(t, "cost", "add", "--usd", "30")
	for _, want := range []string{"80%", "100%"} {
		if !strings.Contains(rest.stderr, want) {
			t.Errorf("crossing %s said nothing: %q", want, rest.stderr)
		}
	}
	// Being over budget does not stop the story: work stranded between gates
	// costs more than the overspend.
	if r := run(t, "cost", "add", "--usd", "5"); r.code != 0 {
		t.Errorf("recording spend past the budget was refused: %s%s", r.stdout, r.stderr)
	}
}

// A runner that asks for --json still has a person watching its terminal, and
// the warning is for them.
func TestAnAlertReachesStandardErrorWithJSONToo(t *testing.T) {
	gitProject(t)
	initialised(t)
	mustRun(t, "start")

	r := mustRun(t, "cost", "add", "--usd", "35", "--json")
	if !strings.Contains(r.stderr, "50%") {
		t.Errorf("with --json the alert did not reach standard error: %q", r.stderr)
	}
	if p := decode[costPayload](t, r); len(p.Alerts) != 1 {
		t.Errorf("alerts = %v, want the one crossed", p.Alerts)
	}
}

// A project that alerts only at a half would otherwise run past its budget
// without a word.
func TestUsingUpTheBudgetIsSaidWhateverTheAlertsAre(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setConfigOnDisk(t, root, "budget", map[string]any{"per_story_usd": 10, "alert_fractions": []float64{0.5}})
	mustRun(t, "start")

	mustRun(t, "cost", "add", "--usd", "6")
	r := mustRun(t, "cost", "add", "--usd", "5")
	for _, want := range []string{"whole $10.00 budget", "`sdlc stop`"} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("using up the budget did not say %q: %q", want, r.stderr)
		}
	}
	if again := mustRun(t, "cost", "add", "--usd", "1"); strings.Contains(again.stderr, "whole") {
		t.Errorf("the budget being used up was said again: %q", again.stderr)
	}
}

// A headless runner learns what an iteration cost only once the session is
// over, and a session that finished its story has ended the iteration by then.
// Recording only against the story being worked on refused every one of them.
func TestARunnerRecordsCostAfterTheIterationHasEnded(t *testing.T) {
	gitProject(t)
	initialised(t)
	mustRun(t, "start")
	id := decode[statusPayload](t, mustRun(t, "status", "--json")).Active
	mustRun(t, "stop")

	r := run(t, "cost", "add", "--usd", "1.50")
	if r.code == 0 {
		t.Fatal("with no iteration running and no story named, the amount was recorded")
	}
	if !strings.Contains(r.stderr, "--story") {
		t.Errorf("the refusal does not say how to name the story: %s", r.stderr)
	}

	mustRun(t, "cost", "add", "--story", id, "--usd", "1.50")
	if p := decode[costPayload](t, mustRun(t, "cost", "--story", id, "--json")); p.Story != id || p.SpentUSD != 1.5 {
		t.Errorf("cost --story %s = %+v, want 1.50 spent on it", id, p)
	}
	if r := run(t, "cost", "add", "--story", "NOPE-1", "--usd", "1"); r.code == 0 {
		t.Error("an amount was recorded against a story that is not in the backlog")
	}
}

// An amount that arrives empty from a shell substitution that produced nothing
// would otherwise be recorded as a free story -- the one wrong answer nobody
// would question.
func TestAnAmountThatIsNotAnAmountIsRefused(t *testing.T) {
	gitProject(t)
	initialised(t)
	mustRun(t, "start")

	for _, bad := range []string{"", "null", "n/a", "-1", "NaN"} {
		t.Run(bad, func(t *testing.T) {
			if r := run(t, "cost", "add", "--usd", bad); r.code == 0 {
				t.Errorf("%q was recorded as a spend", bad)
			}
		})
	}
	// A dollar sign in front is what a person pastes, and it means the number.
	if r := run(t, "cost", "add", "--usd", "$1.25"); r.code != 0 {
		t.Errorf("$1.25 was refused: %s%s", r.stdout, r.stderr)
	}
}

// A project that sets no budget still gets the total, because "what did this
// cost" is a question with an answer either way.
func TestWithNoBudgetTheSpendIsStillKept(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setBudgetOnDisk(t, root, 0)
	mustRun(t, "start")

	mustRun(t, "cost", "add", "--usd", "3")
	p := decode[costPayload](t, mustRun(t, "cost", "--json"))
	if p.SpentUSD != 3 || p.BudgetUSD != 0 {
		t.Errorf("spent = %v, budget = %v", p.SpentUSD, p.BudgetUSD)
	}
	if got := mustRun(t, "cost").stdout; !strings.Contains(got, "no budget is set") {
		t.Errorf("the absence of a budget is not explained: %q", got)
	}
}

// setBudgetOnDisk rewrites the scaffolded budget, which is how a project that
// does not want one says so.
func setBudgetOnDisk(t *testing.T, root string, usd float64) {
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
	budget, ok := cfg["budget"].(map[string]any)
	if !ok {
		t.Fatalf("the scaffolded configuration has no budget: %v", cfg)
	}
	budget["per_story_usd"] = usd

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatal(err)
	}
}
