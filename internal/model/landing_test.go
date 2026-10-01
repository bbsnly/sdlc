package model

// US-029.3 (PLAN.md step 6): the record of a story worked in its own worktree
// carries where its branch was cut (cut), the change the reviewers saw
// (change, measured from change_base), and, while a landing is between moving
// trunk and writing the record, the landing's mark ({from, to}). All four are
// additive: a record without them reads, and is written, as before.
//
// These go through the record's JSON, which is the on-disk format and so the
// API, and name no field the story adds. LatestDecision is asked for through
// the method set, as LatestDecision() (Approval, bool), after the pattern of
// PendingEscalation.

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

var lmAt = time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)

const (
	lmCut  = "1111111111111111111111111111111111111111"
	lmBase = "2222222222222222222222222222222222222222"
	lmFrom = "3333333333333333333333333333333333333333"
	lmTo   = "4444444444444444444444444444444444444444"
	lmTip  = "5555555555555555555555555555555555555555"
)

// lmFinished is a record, as JSON, with every gate passed, the commit gate's
// range recorded, and the four landing fields set.
func lmFinished(t *testing.T) *Record {
	t.Helper()
	gates := map[string]any{}
	for _, g := range Gates {
		gates[string(g)] = map[string]any{"status": "pass", "at": Timestamp(lmAt)}
	}
	raw, err := json.Marshal(map[string]any{
		"story": "US-001", "created": Timestamp(lmAt), "base": lmTip,
		"commit_base": lmCut, "commit": lmTip,
		"cut": lmCut, "change": "sha256:abc", "change_base": lmBase,
		"landing": map[string]any{"from": lmFrom, "to": lmTo},
		"gates":   gates, "events": []any{}, "escalations": []any{}, "approvals": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

// lmJSON is the record as it is written, key by key.
func lmJSON(t *testing.T, r *Record) map[string]any {
	t.Helper()
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// The four fields are read and written back as they were, and a record that
// has none of them writes none of them.
func TestTheLandingFieldsAreKeptOnTheRecordAndLeftOutWhenEmpty(t *testing.T) {
	got := lmJSON(t, lmFinished(t))
	for key, want := range map[string]any{
		"cut": lmCut, "change": "sha256:abc", "change_base": lmBase,
		"landing": map[string]any{"from": lmFrom, "to": lmTo},
	} {
		if !reflect.DeepEqual(got[key], want) {
			t.Errorf("%s is written as %v, want %v", key, got[key], want)
		}
	}

	empty := lmJSON(t, &Record{Story: "US-001", Created: Timestamp(lmAt)})
	for _, key := range []string{"cut", "change", "change_base", "landing"} {
		if v, ok := empty[key]; ok {
			t.Errorf("a record without %s writes it as %v", key, v)
		}
	}
}

// PLAN.md step 6: once the commit gate no longer stands passed -- failed
// itself, or reopened by a gate before it -- the change it recorded, its base
// and any landing mark go with the range's end; where the branch was cut
// stays.
func TestAReopenedCommitGateTakesTheChangeAndTheMarkButKeepsTheCut(t *testing.T) {
	for _, tc := range []struct {
		name string
		gate Gate
	}{
		{"the commit gate failed", GateCommit},
		{"a gate before it failed", GateVerification},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := lmFinished(t)
			r.SetGate(tc.gate, GateFail, "reopened", lmAt)
			got := lmJSON(t, r)
			for _, key := range []string{"change", "change_base", "landing", "commit"} {
				if v, ok := got[key]; ok {
					t.Errorf("%s is still %v on a record whose commit gate no longer stands", key, v)
				}
			}
			if got["cut"] != lmCut {
				t.Errorf("cut is %v, want the branch's cut %s kept", got["cut"], lmCut)
			}
		})
	}

	r := lmFinished(t)
	r.SetGate(GateRetro, GateFail, "the retro again", lmAt)
	if got := lmJSON(t, r); got["change"] != "sha256:abc" || got["change_base"] != lmBase {
		t.Errorf("a gate after the commit gate took the change with it: %v / %v", got["change"], got["change_base"])
	}
}

type lmDecider interface {
	LatestDecision() (Approval, bool)
}

// L0 (b): the landing asks for the latest answer a person gave, to tell an
// approval from a rejection.
func TestLatestDecisionIsTheLastAnswerAPersonGave(t *testing.T) {
	var r any = &Record{}
	d, ok := r.(lmDecider)
	if !ok {
		t.Fatal("a record has no LatestDecision() (Approval, bool)")
	}
	if _, ok := d.LatestDecision(); ok {
		t.Error("a record nobody answered has a decision")
	}
	rec := r.(*Record)
	rec.Approvals = []Approval{
		{At: Timestamp(lmAt), Decision: DecisionApproved, Type: "pre_commit_approval"},
		{At: Timestamp(lmAt.Add(time.Hour)), Decision: DecisionRejected, Reason: "not this week"},
	}
	got, ok := d.LatestDecision()
	if !ok || got.Decision != DecisionRejected || got.Reason != "not this week" {
		t.Errorf("LatestDecision = %+v, %v; want the rejection given last", got, ok)
	}
	rec.Approvals = append(rec.Approvals, Approval{At: Timestamp(lmAt.Add(2 * time.Hour)), Decision: DecisionApproved})
	if got, ok := d.LatestDecision(); !ok || got.Decision != DecisionApproved {
		t.Errorf("LatestDecision = %+v, %v; want the approval given after the rejection", got, ok)
	}
}
