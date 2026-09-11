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
	mustRun(t, "init")
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
	mustRun(t, "init")
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

// An amount that arrives empty from a shell substitution that produced nothing
// would otherwise be recorded as a free story -- the one wrong answer nobody
// would question.
func TestAnAmountThatIsNotAnAmountIsRefused(t *testing.T) {
	gitProject(t)
	mustRun(t, "init")
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
	mustRun(t, "init")
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
