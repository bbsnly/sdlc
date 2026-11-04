package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// US-117 AC-2 and the person's decision 7: `<important|nit> <path>: <text>`. The
// path is optional, and the value is split on its first ": ", so a text keeps a
// colon of its own. The path is repository-relative, slash-separated and cleaned.
func TestAC2AFindingReadsAsItsImportanceAnOptionalPathAndItsText(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		value, importance, path, text string
	}{
		{"important internal/cli/review.go: the count is wrong", "important", "internal/cli/review.go", "the count is wrong"},
		{"nit docs/commands.md: a long line", "nit", "docs/commands.md", "a long line"},
		{"nit the names are long", "nit", "", "the names are long"},
		{"important nothing checks the reason", "important", "", "nothing checks the reason"},
		// Only the first ": " parts the path from the text.
		{"important a/b.go: x: y", "important", "a/b.go", "x: y"},
		// A colon with no space after it is not where the path ends.
		{"nit a/b.go:12: the line is long", "nit", "a/b.go:12", "the line is long"},
		// A comma is text, not a second finding.
		{"nit a.go: one, two, three", "nit", "a.go", "one, two, three"},
		// Cleaned.
		{"important ./internal//cli/./review.go: wrong", "important", "internal/cli/review.go", "wrong"},
		{"nit internal/: a directory", "nit", "internal", "a directory"},
	} {
		t.Run("AC-2 "+c.value, func(t *testing.T) {
			t.Parallel()
			f, err := ParseFinding(c.value)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if f.Importance != c.importance || f.Path != c.path || f.Text != c.text {
				t.Errorf("read as importance %q, path %q, text %q; want %q, %q, %q",
					f.Importance, f.Path, f.Text, c.importance, c.path, c.text)
			}
		})
	}
}

// Decision 7: a path that leaves the repository, or is not one of its own, is
// refused rather than kept. A finding nobody can place, or no text at all, says
// nothing a closing could answer.
func TestAC2AFindingThatCannotBeReadIsRefused(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"empty":                        "",
		"blank":                        "   ",
		"no importance":                "internal/x.go: wrong",
		"an importance the loop lacks": "major internal/x.go: wrong",
		"an importance with no text":   "important",
		"a nit with no text":           "nit",
		"a path with no text":          "important internal/x.go: ",
		"an absolute path":             "important /etc/passwd: wrong",
		"a path out of the repository": "important ../outside.go: wrong",
		"a path that climbs out":       "nit internal/../../outside.go: wrong",
		"a Windows absolute path":      `important C:/repo/x.go: wrong`,
	} {
		t.Run("AC-2 "+name, func(t *testing.T) {
			t.Parallel()
			if f, err := ParseFinding(value); err == nil {
				t.Errorf("%q was read as %+v", value, f)
			}
		})
	}
}

// AC-2 and decision 2: F-n is numbered per gate and role across all rounds, so a
// second round goes on from where the first stopped, and another role or another
// gate starts at F-1.
func TestAC2FindingsAreNumberedPerGateAndRoleAcrossRounds(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	findings := func(t *testing.T, values ...string) []Finding {
		t.Helper()
		var out []Finding
		for _, v := range values {
			f, err := ParseFinding(v)
			if err != nil {
				t.Fatalf("%q: %v", v, err)
			}
			out = append(out, f)
		}
		return out
	}
	ids := func(rev Review) []string {
		var out []string
		for _, f := range rev.Findings {
			out = append(out, f.ID)
		}
		return out
	}
	want := func(t *testing.T, what string, rev Review, ids0 ...string) {
		t.Helper()
		if got := ids(rev); strings.Join(got, ",") != strings.Join(ids0, ",") {
			t.Errorf("%s: ids %v, want %v", what, got, ids0)
		}
	}

	var record Record
	one, two := 1, 2
	first := record.AddReview(Review{Gate: GateCodeReview, Role: "code-reviewer", Verdict: VerdictBlock, Important: &one, Nits: &two,
		Findings: findings(t, "important a.go: wrong", "nit a.go: long", "nit b.go: longer")}, at)
	want(t, "round 1", first, "F-1", "F-2", "F-3")

	second := record.AddReview(Review{Gate: GateCodeReview, Role: "code-reviewer", Verdict: VerdictApprove, Nits: &two,
		Findings: findings(t, "nit c.go: still long", "nit the names")}, at)
	want(t, "round 2", second, "F-4", "F-5")

	// A round with none takes no number, and the next goes on after it.
	record.AddReview(Review{Gate: GateCodeReview, Role: "code-reviewer", Verdict: VerdictApprove}, at)
	third := record.AddReview(Review{Gate: GateCodeReview, Role: "code-reviewer", Verdict: VerdictApprove, Nits: &one,
		Findings: findings(t, "nit d.go: one more")}, at)
	want(t, "round 4", third, "F-6")

	other := record.AddReview(Review{Gate: GateCodeReview, Role: "security", Verdict: VerdictApprove, Nits: &one,
		Findings: findings(t, "nit e.go: a name")}, at)
	want(t, "another role", other, "F-1")

	gate := record.AddReview(Review{Gate: GateDesignReview, Role: "security", Verdict: VerdictApprove, Nits: &one,
		Findings: findings(t, "nit the plan: a name")}, at)
	want(t, "another gate", gate, "F-1")

	// What the record keeps is what was returned.
	if got, ok := record.LatestReview(GateCodeReview, "code-reviewer"); !ok || strings.Join(ids(got), ",") != "F-6" {
		t.Errorf("the record kept %v", ids(got))
	}
}

// Decisions 1 and 4: the important findings number exactly --important, the nit
// findings no more than --nits, which counts every nit found, the ones past the
// policy's cap included. At the design and code reviews a count above 0 with no
// finding listed does not agree; elsewhere a review need list none.
func TestAC2TheFindingsAgreeWithTheCountsOrTheReviewIsNotRecorded(t *testing.T) {
	t.Parallel()
	n := func(i int) *int { return &i }
	parse := func(t *testing.T, values ...string) []Finding {
		t.Helper()
		var out []Finding
		for _, v := range values {
			f, err := ParseFinding(v)
			if err != nil {
				t.Fatalf("%q: %v", v, err)
			}
			out = append(out, f)
		}
		return out
	}
	for _, c := range []struct {
		name            string
		gate            Gate
		important, nits *int
		findings        []string
		agree           bool
	}{
		{"nothing given", GateCodeReview, nil, nil, nil, true},
		{"zero counts and no findings", GateCodeReview, n(0), n(0), nil, true},
		{"one important, listed", GateCodeReview, n(1), nil, []string{"important a.go: wrong"}, true},
		{"two nits, listed", GateDesignReview, nil, n(2), []string{"nit a.go: x", "nit b.go: y"}, true},
		{"nine nits, five listed under the cap", GateCodeReview, n(0), n(9),
			[]string{"nit a: 1", "nit b: 2", "nit c: 3", "nit d: 4", "nit e: 5"}, true},
		{"an important and nits", GateCodeReview, n(2), n(1),
			[]string{"important a.go: x", "nit b.go: y", "important c.go: z"}, true},
		{"one important counted, none listed but a nit", GateCodeReview, n(1), n(1), []string{"nit a.go: x"}, false},
		{"two important counted, one listed", GateCodeReview, n(2), nil, []string{"important a.go: x"}, false},
		{"one important counted, two listed", GateCodeReview, n(1), nil, []string{"important a.go: x", "important b.go: y"}, false},
		{"an important listed and none counted", GateCodeReview, nil, n(1), []string{"important a.go: x"}, false},
		{"an important listed and zero counted", GateCodeReview, n(0), nil, []string{"important a.go: x"}, false},
		{"more nits listed than counted", GateCodeReview, nil, n(1), []string{"nit a.go: x", "nit b.go: y"}, false},
		{"a nit listed and none counted", GateDesignReview, nil, nil, []string{"nit a.go: x"}, false},
		{"important counted and nothing listed at the code review", GateCodeReview, n(1), nil, nil, false},
		{"nits counted and nothing listed at the code review", GateCodeReview, nil, n(3), nil, false},
		{"nits counted and nothing listed at the design review", GateDesignReview, n(0), n(2), nil, false},
		{"important counted and nothing listed at the design review", GateDesignReview, n(1), n(0), nil, false},
		{"nits counted and nothing listed at the verifier's review", GateVerifierReview, nil, n(3), nil, true},
		{"important counted and nothing listed at the verifier's review", GateVerifierReview, n(1), nil, nil, true},
		{"listed at the verifier's review, and counted", GateVerifierReview, n(1), nil, []string{"important a.go: x"}, true},
		{"listed at the verifier's review, and not counted", GateVerifierReview, nil, nil, []string{"important a.go: x"}, false},
		{"nits counted and nothing listed at Gate 1", GateDoR, nil, n(2), nil, true},
	} {
		t.Run("AC-2 "+c.name, func(t *testing.T) {
			t.Parallel()
			rev := Review{Gate: c.gate, Role: "someone", Verdict: VerdictApprove, Important: c.important, Nits: c.nits,
				Findings: parse(t, c.findings...)}
			if got := rev.FindingsMatchCounts(); got != c.agree {
				t.Errorf("FindingsMatchCounts() = %v, want %v", got, c.agree)
			}
		})
	}
}

// The findings are kept on the record and read back as they were written, and a
// record from before them reads and goes on numbering from one.
func TestAC2FindingsAreKeptOnTheRecordAndAnOldRecordReadsWithout(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	const old = `{"story":"A-1","reviews":[{"at":"2026-09-10T08:31:00Z","gate":"code_review",` +
		`"role":"code-reviewer","verdict":"approve","round":1,"nits":2}]}`
	var record Record
	if err := json.Unmarshal([]byte(old), &record); err != nil {
		t.Fatal(err)
	}
	if got, ok := record.LatestReview(GateCodeReview, "code-reviewer"); !ok || len(got.Findings) != 0 {
		t.Fatalf("the old review read as %+v", got)
	}
	f, err := ParseFinding("important internal/a.go: the reason is not checked")
	if err != nil {
		t.Fatal(err)
	}
	one := 1
	record.AddReview(Review{Gate: GateCodeReview, Role: "code-reviewer", Verdict: VerdictBlock, Important: &one,
		Findings: []Finding{f}}, at)
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var back Record
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	got, ok := back.LatestReview(GateCodeReview, "code-reviewer")
	if !ok || len(got.Findings) != 1 {
		t.Fatalf("the finding did not come back:\n%s", raw)
	}
	if g := got.Findings[0]; g.ID != "F-1" || g.Importance != "important" || g.Path != "internal/a.go" || g.Text != "the reason is not checked" {
		t.Errorf("came back as %+v", g)
	}
	if strings.Count(string(raw), "the reason is not checked") != 1 {
		t.Errorf("the finding is not kept once, on its review:\n%s", raw)
	}
	// The old review had none, and stays without.
	if first := back.Reviews[0]; len(first.Findings) != 0 {
		t.Errorf("the old review gained findings: %+v", first.Findings)
	}
}
