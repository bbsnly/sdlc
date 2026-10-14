package model

// US-094: the record keeps the trunk test files a gate accepted until a later
// gate judges them again. Recording a gate used to end by forgetting
// trunk_tests whenever the commit gate did not stand, so the field was empty
// after every gate before the commit gate, though the gate had said what it
// accepted.
//
// What these hold, through SetGate and Reopen as they are and the record's
// JSON, which is the on-disk format:
//
//   - AC-1: a gate recorded while the commit gate does not stand leaves
//     trunk_tests as it was;
//   - AC-3: the commit gate going from passed to not passed takes trunk_tests
//     with the rest of what that gate recorded, and that rest still goes
//     whenever the commit gate does not stand, whatever it was before;
//   - forks, cut, commit_base and base are never among what is forgotten.
//
// A guarded gate recorded as passed is left to internal/cli: what the record
// then holds is what that gate accepted, and the model does not know it.
// Helpers carry a km prefix; lmJSON and lmAt are landing_test.go's.

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

const (
	kmCut   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	kmTrunk = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	kmLater = "cccccccccccccccccccccccccccccccccccccccc"
	kmFirst = "dddddddddddddddddddddddddddddddddddddddd"
	kmTip   = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	kmOld   = "ffffffffffffffffffffffffffffffffffffffff"
)

// kmForgotten is what the commit gate recorded besides trunk_tests, which a
// record whose commit gate does not stand never holds.
var kmForgotten = []string{"commit", "range_unknown", "head_past_commit", "change", "change_base", "landing", "replay"}

// kmTaken is one acceptance, as the record writes it.
type kmTaken map[string]any

// The acceptances the rows use: none of them the same commit and files.
var (
	kmOneFile  = kmTaken{"commit": kmTrunk, "files": []any{"internal/refund_test.go"}}
	kmTwoFiles = kmTaken{"commit": kmLater, "files": []any{"internal/later_test.go", "internal/tax_test.go"}}
	kmOneGone  = kmTaken{"commit": kmLater, "files": []any{"checks/ledger.spec"}, "gone": []any{"internal/tax_test.go"}}
)

// kmRecord is a record whose gates before upTo are passed and the others not
// recorded, holding everything the commit gate records, a list of the story's
// own commits, and taken as its trunk_tests. Where the commit gate is not
// among the passed, that is a record nothing writes today: an older binary or
// a run cut short could have left it.
func kmRecord(t *testing.T, upTo Gate, taken kmTaken) *Record {
	t.Helper()
	gates := map[string]any{}
	for _, g := range Gates {
		if g == upTo {
			break
		}
		gates[string(g)] = map[string]any{"status": "pass", "at": Timestamp(lmAt)}
	}
	doc := map[string]any{
		"story": "US-001", "created": Timestamp(lmAt), "base": kmTip,
		"commit_base": kmCut, "commit": kmTip, "range_unknown": true, "head_past_commit": kmOld,
		"cut": kmCut, "change": "sha256:abc", "change_base": kmTrunk,
		"forks":   []any{map[string]any{"from": kmCut}, map[string]any{"from": kmTrunk, "first": kmFirst}},
		"landing": map[string]any{"from": kmTrunk, "to": kmTip},
		"replay":  map[string]any{"from": kmOld, "to": kmTip, "onto": kmTrunk},
		"gates":   gates, "events": []any{}, "escalations": []any{}, "approvals": []any{},
	}
	if taken != nil {
		doc["trunk_tests"] = map[string]any(taken)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var r Record
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

// kmStays holds the record to what no gate forgets: whose the commits are,
// where the branch was cut and where the story's commits start.
func kmStays(t *testing.T, before, after map[string]any) {
	t.Helper()
	for _, key := range []string{"forks", "cut", "commit_base", "base"} {
		if before[key] == nil {
			t.Fatalf("precondition: the record had no %s to keep", key)
		}
		if !reflect.DeepEqual(after[key], before[key]) {
			t.Errorf("%s is %v after the gate was recorded, want it kept as %v", key, after[key], before[key])
		}
	}
}

// kmNoEvent holds the model to writing no trunk_tests event of its own: only
// the gate that accepted the files says so.
func kmNoEvent(t *testing.T, r *Record) {
	t.Helper()
	for _, e := range r.Events {
		if e.Type == "trunk_tests" {
			t.Errorf("recording the gate wrote a trunk_tests event: %q", e.Message)
		}
	}
}

// kmStep is one gate recorded, or reopened where status is empty.
type kmStep struct {
	name   string
	upTo   Gate // the first gate not passed before the step; "" for a finished story
	gate   Gate
	status GateStatus
	taken  kmTaken
}

func (s kmStep) do(r *Record) {
	at := lmAt.Add(time.Hour)
	if s.status == "" {
		r.Reopen(s.gate, "the freeze was lifted", at)
		return
	}
	r.SetGate(s.gate, s.status, "recorded again", at)
}

// kmNotStanding is every way a gate is recorded while the commit gate does not
// stand, short of a guarded gate passing: the gate that accepted the files, one
// before it and one after it recorded as failed or pending, the commit gate
// itself recorded as failed or pending once more, and a gate that does not
// check the freeze recorded as passed.
var kmNotStanding = []kmStep{
	{"the implementation failed after the code review", GateCommit, GateImplementation, GateFail, kmOneFile},
	{"the implementation put back to pending", GateVerification, GateImplementation, GatePending, kmTwoFiles},
	{"the verification failed with nothing passed after it", GateVerification, GateVerification, GateFail, kmOneGone},
	{"the tests_frozen gate failed", GateCommit, GateTestsFrozen, GateFail, kmTwoFiles},
	{"the plan put back to pending", GateImplementation, GatePlan, GatePending, kmOneFile},
	{"the first gate failed", GateCodeReview, GateDoR, GateFail, kmOneGone},
	{"the code review failed", GateCommit, GateCodeReview, GateFail, kmOneFile},
	{"the commit gate failed where it did not stand", GateCommit, GateCommit, GateFail, kmTwoFiles},
	{"the commit gate put to pending where it did not stand", GateCommit, GateCommit, GatePending, kmOneGone},
	{"the design review passed", GateDesignReview, GateDesignReview, GatePass, kmOneFile},
	{"the verifier's review passed", GateVerifierReview, GateVerifierReview, GatePass, kmTwoFiles},
	{"the code review passed", GateCodeReview, GateCodeReview, GatePass, kmOneGone},
	{"the design review passed again under later passes", GateCommit, GateDesignReview, GatePass, kmOneFile},
}

// AC-1: a gate recorded while the commit gate does not stand does not judge
// the test files again, so the record still names the trunk commit, the files
// and the ones gone that the last gate accepted, and gains no event for them.
func TestAC1AGateRecordedWhileTheCommitGateDoesNotStandLeavesTheRecordsTrunkTests(t *testing.T) {
	for _, step := range kmNotStanding {
		t.Run("AC-1 "+step.name, func(t *testing.T) {
			r := kmRecord(t, step.upTo, step.taken)
			if r.Pass(GateCommit) {
				t.Fatal("precondition: the commit gate stands on this record")
			}
			before := lmJSON(t, r)
			step.do(r)
			after := lmJSON(t, r)
			if got, ok := after["trunk_tests"]; !ok {
				t.Errorf("the record has no trunk_tests after %s was recorded %s; it had %v, and no gate judged the files again",
					step.gate, step.status, before["trunk_tests"])
			} else if !reflect.DeepEqual(got, before["trunk_tests"]) {
				t.Errorf("trunk_tests is %v after %s was recorded %s, want it as it was: %v", got, step.gate, step.status, before["trunk_tests"])
			}
			kmNoEvent(t, r)
			kmStays(t, before, after)
		})
	}
}

// AC-3, the half a narrower rule for trunk_tests must not take with it: the
// replay mark, the reviewed change's fingerprint and the rest of what the
// commit gate recorded go whenever the commit gate does not stand once a gate
// is recorded, whether or not it stood before. A landing trusts a record that
// holds them to have a passed commit gate behind them. The rows are AC-1's,
// with a guarded gate recorded as passed and a reopened tests_frozen besides.
func TestAC3WhatTheCommitGateRecordedStillGoesAtAnyGateRecordedWhileItDoesNotStand(t *testing.T) {
	steps := append([]kmStep{
		{"the implementation passed", GateImplementation, GateImplementation, GatePass, kmOneFile},
		{"the verification passed again under later passes", GateCommit, GateVerification, GatePass, kmTwoFiles},
		{"the tests_frozen gate reopened", GateCommit, GateTestsFrozen, "", kmOneGone},
	}, kmNotStanding...)
	for _, step := range steps {
		t.Run("AC-3 "+step.name, func(t *testing.T) {
			r := kmRecord(t, step.upTo, step.taken)
			before := lmJSON(t, r)
			for _, key := range kmForgotten {
				if before[key] == nil {
					t.Fatalf("precondition: the record had no %s to forget", key)
				}
			}
			step.do(r)
			after := lmJSON(t, r)
			for _, key := range kmForgotten {
				if v, ok := after[key]; ok {
					t.Errorf("%s is still %v on a record whose commit gate does not stand", key, v)
				}
			}
			kmNoEvent(t, r)
			kmStays(t, before, after)
		})
	}
}

// AC-3: the commit gate going from passed to not passed -- recorded as failed
// or pending itself, reopened by a gate before it that failed or went back to
// pending, by one that does not check the freeze and passed, or by the freeze
// being lifted -- takes trunk_tests with everything else it recorded. Whose
// the commits are and where they start stay.
func TestAC3TheCommitGateOpenedAgainForgetsTheRecordsTrunkTestsWithTheRestItRecorded(t *testing.T) {
	for _, step := range []kmStep{
		{name: "the commit gate failed", gate: GateCommit, status: GateFail, taken: kmOneFile},
		{name: "the commit gate put back to pending", gate: GateCommit, status: GatePending, taken: kmTwoFiles},
		{name: "the code review failed", gate: GateCodeReview, status: GateFail, taken: kmOneGone},
		{name: "the verification put back to pending", gate: GateVerification, status: GatePending, taken: kmOneFile},
		{name: "the tests_frozen gate failed", gate: GateTestsFrozen, status: GateFail, taken: kmTwoFiles},
		{name: "the first gate failed", gate: GateDoR, status: GateFail, taken: kmOneGone},
		{name: "the verifier's review passed again", gate: GateVerifierReview, status: GatePass, taken: kmOneFile},
		{name: "the design review passed again", gate: GateDesignReview, status: GatePass, taken: kmTwoFiles},
		{name: "the code review passed again", gate: GateCodeReview, status: GatePass, taken: kmOneGone},
		{name: "the tests_frozen gate reopened", gate: GateTestsFrozen, taken: kmOneFile},
		{name: "the plan reopened", gate: GatePlan, taken: kmTwoFiles},
	} {
		t.Run("AC-3 "+step.name, func(t *testing.T) {
			r := kmRecord(t, "", step.taken)
			if !r.Pass(GateCommit) {
				t.Fatal("precondition: the commit gate does not stand on this record")
			}
			before := lmJSON(t, r)
			if before["trunk_tests"] == nil {
				t.Fatal("precondition: the record had no trunk_tests")
			}
			step.do(r)
			if r.Pass(GateCommit) {
				t.Fatalf("precondition: the commit gate still stands after %s", step.name)
			}
			after := lmJSON(t, r)
			if v, ok := after["trunk_tests"]; ok {
				t.Errorf("trunk_tests is still %v on a record whose commit gate was opened again", v)
			}
			for _, key := range kmForgotten {
				if v, ok := after[key]; ok {
					t.Errorf("%s is still %v on a record whose commit gate was opened again", key, v)
				}
			}
			kmNoEvent(t, r)
			kmStays(t, before, after)
		})
	}
}

// AC-1, while the commit gate stands: the retro recorded with any outcome, the
// retro reopened, and the commit gate recorded as passed once more leave
// trunk_tests and everything else the commit gate recorded as they were.
func TestAC1AGateRecordedWhileTheCommitGateStandsLeavesWhatItRecorded(t *testing.T) {
	for _, step := range []kmStep{
		{name: "the retro failed", gate: GateRetro, status: GateFail, taken: kmOneFile},
		{name: "the retro put back to pending", gate: GateRetro, status: GatePending, taken: kmTwoFiles},
		{name: "the retro passed again", gate: GateRetro, status: GatePass, taken: kmOneGone},
		{name: "the retro reopened", gate: GateRetro, taken: kmOneFile},
		{name: "the commit gate passed again", gate: GateCommit, status: GatePass, taken: kmTwoFiles},
	} {
		t.Run("AC-1 "+step.name, func(t *testing.T) {
			r := kmRecord(t, "", step.taken)
			before := lmJSON(t, r)
			step.do(r)
			if !r.Pass(GateCommit) {
				t.Fatalf("precondition: the commit gate no longer stands after %s", step.name)
			}
			after := lmJSON(t, r)
			for _, key := range append([]string{"trunk_tests"}, kmForgotten...) {
				if !reflect.DeepEqual(after[key], before[key]) {
					t.Errorf("%s is %v after %s, want it as it was: %v", key, after[key], step.name, before[key])
				}
			}
			kmNoEvent(t, r)
			kmStays(t, before, after)
		})
	}
}

// AC-1 and AC-3 where the record has no trunk_tests: recording a gate invents
// none, whether or not the commit gate stood.
func TestAC1AGateRecordedOnARecordWithoutTrunkTestsWritesNone(t *testing.T) {
	for _, step := range []kmStep{
		{name: "AC-1 the implementation failed before the commit gate", upTo: GateCommit, gate: GateImplementation, status: GateFail},
		{name: "AC-1 the code review passed before the commit gate", upTo: GateCodeReview, gate: GateCodeReview, status: GatePass},
		{name: "AC-3 the commit gate failed", gate: GateCommit, status: GateFail},
		{name: "AC-1 the retro passed again", gate: GateRetro, status: GatePass},
	} {
		t.Run(step.name, func(t *testing.T) {
			r := kmRecord(t, step.upTo, nil)
			step.do(r)
			if v, ok := lmJSON(t, r)["trunk_tests"]; ok {
				t.Errorf("the record has trunk_tests %v, and it had none before the gate was recorded", v)
			}
			kmNoEvent(t, r)
		})
	}
}
