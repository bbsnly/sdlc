package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// US-115 AC-3 and the person's Q5. The counts are optional on the record, so a
// reviewer who said nothing stays distinct from one who counted 0: written as
// plain ints with omitempty the two were one, and an old review would have read
// as a claim of no findings. Through JSON, because the record is what comes back.
func TestAC3ACountStaysDistinctBetweenNotGivenAndZeroThroughARecord(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name, review       string
		important, nits    string
		noImportant, noNit bool
	}{
		{name: "neither given, as an old review", review: `{}`, noImportant: true, noNit: true},
		{name: "both zero", review: `{"important":0,"nits":0}`, important: `"important":0`, nits: `"nits":0`},
		{name: "both counted", review: `{"important":2,"nits":7}`, important: `"important":2`, nits: `"nits":7`},
		{name: "zero important, nits not given", review: `{"important":0}`, important: `"important":0`, noNit: true},
		{name: "nits only", review: `{"nits":0}`, nits: `"nits":0`, noImportant: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			var rev Review
			if err := json.Unmarshal([]byte(c.review), &rev); err != nil {
				t.Fatalf("a review with these counts does not read: %v", err)
			}
			rev.Gate, rev.Role, rev.Verdict = GateDesignReview, "architect", VerdictApprove

			var record Record
			record.AddReview(rev, at)
			raw, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			var back Record
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatal(err)
			}
			again, err := json.Marshal(back)
			if err != nil {
				t.Fatal(err)
			}
			for _, written := range []string{string(raw), string(again)} {
				if c.important != "" && !strings.Contains(written, c.important) {
					t.Errorf("the record lost %s:\n%s", c.important, written)
				}
				if c.nits != "" && !strings.Contains(written, c.nits) {
					t.Errorf("the record lost %s:\n%s", c.nits, written)
				}
				if c.noImportant && strings.Contains(written, `"important"`) {
					t.Errorf("a count nobody gave is on the record:\n%s", written)
				}
				if c.noNit && strings.Contains(written, `"nits"`) {
					t.Errorf("a count nobody gave is on the record:\n%s", written)
				}
			}
		})
	}
}

// A record written before the counts existed reads with everything it had, and
// a later round is numbered from it as ever.
func TestAC3AnOldRecordWithNoCountsReadsAndGoesOnCountingRounds(t *testing.T) {
	t.Parallel()
	const old = `{"story":"A-1","reviews":[{"at":"2026-09-10T08:31:00Z","gate":"design_review",` +
		`"role":"architect","verdict":"block","round":1,"subject":"abc","note":"no step"}]}`
	var record Record
	if err := json.Unmarshal([]byte(old), &record); err != nil {
		t.Fatal(err)
	}
	if got, ok := record.LatestReview(GateDesignReview, "architect"); !ok || got.Verdict != VerdictBlock || got.Note != "no step" {
		t.Fatalf("the old review did not read: %+v", got)
	}
	var given Review
	if err := json.Unmarshal([]byte(`{"gate":"design_review","role":"architect","verdict":"block","important":1}`), &given); err != nil {
		t.Fatal(err)
	}
	if got := record.AddReview(given, time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)); got.Round != 2 {
		t.Errorf("the next round is %d, want 2", got.Round)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(raw), `"important"`) != 1 {
		t.Errorf("the count belongs to the new review alone:\n%s", raw)
	}
}
