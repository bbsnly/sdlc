package model

// US-026: the one rule the gate and the hook share for who a staffing leaves
// out. `cli` and `hook` may not import each other, so it lives here, and both
// ask it each time: applied once, when the staffing was stored, a record edited
// by hand or stored before the analysis said the story was sensitive would keep
// out an agent the floor does not let go (AC-4, T1, T3).

import (
	"slices"
	"testing"
	"time"
)

// staffedRecord is a record whose staffing leaves out everybody named.
func staffedRecord(leftOut ...string) *Record {
	r := NewRecord("A-1", time.Date(2026, 9, 10, 8, 30, 0, 0, time.UTC))
	r.Staffing = &Staffing{At: "2026-09-10T08:30:00Z"}
	for _, agent := range Agents {
		r.Staffing.Agents = append(r.Staffing.Agents, Staffed{Agent: agent, Needed: !slices.Contains(leftOut, agent)})
	}
	return r
}

// AC-4: an agent that does a gate's work, the verifier and the code reviewer
// are never left out; security is not on a story that is security-sensitive.
func TestAC4TheFloorNamesWhoAStaffingMayNeverLeaveOut(t *testing.T) {
	for _, d := range Doers {
		for _, sensitive := range []bool{true, false} {
			if !NeverLeftOut(d.Agent, sensitive) {
				t.Errorf("%s does the work of %s and may be left out (sensitive %v)", d.Agent, d.Gate, sensitive)
			}
		}
	}
	for _, agent := range []string{"verifier", "code-reviewer", "project-manager"} {
		for _, sensitive := range []bool{true, false} {
			if !NeverLeftOut(agent, sensitive) {
				t.Errorf("%s may be left out (sensitive %v)", agent, sensitive)
			}
		}
	}
	if !NeverLeftOut("security", true) {
		t.Error("security may be left out of a story that is security-sensitive")
	}
	// Who is the staffing's to decide (decision 4): security on a story that
	// is not sensitive, and the reviewers that advise.
	for agent, sensitive := range map[string][]bool{
		"security": {false}, "architect": {true, false}, "red-team": {true, false},
		"perf": {true, false}, "human-advocate": {true, false},
	} {
		for _, s := range sensitive {
			if NeverLeftOut(agent, s) {
				t.Errorf("%s may not be left out (sensitive %v), and the staffing decides that", agent, s)
			}
		}
	}
	// An agent nobody ships reviews nothing, and is nobody a staffing leaves out.
	if !NeverLeftOut("designer", false) {
		t.Error("an agent the plugin does not ship may be left out")
	}
}

// AC-12: the staffing covers the twelve agents, each once, and every agent
// that does a gate's work is one of them.
func TestAC12TheAgentsAStaffingCoversAreTheTwelveAndIncludeWhoeverDoesAGatesWork(t *testing.T) {
	if len(Agents) != 12 {
		t.Errorf("a staffing covers %d agents, want 12", len(Agents))
	}
	seen := map[string]bool{}
	for _, a := range Agents {
		if seen[a] {
			t.Errorf("%s is in the list twice", a)
		}
		seen[a] = true
	}
	for _, d := range Doers {
		if !seen[d.Agent] {
			t.Errorf("%s does the work of %s and is not an agent a staffing covers", d.Agent, d.Gate)
		}
	}
	for _, r := range Reviewers {
		if !seen[r.Role] {
			t.Errorf("%s reviews at %s and is not an agent a staffing covers", r.Role, r.Gate)
		}
	}
	if !seen["project-manager"] || IsReviewRole("project-manager") {
		t.Error("the project manager is not covered, or is a reviewer")
	}
}

// AC-3 and AC-10: with no staffing recorded nobody is left out, and with one,
// only who it says and the floor allows.
func TestAC3ARecordLeavesOutOnlyWhoItsStaffingSaysAndTheFloorAllows(t *testing.T) {
	none := NewRecord("A-1", time.Now())
	for _, agent := range Agents {
		if none.LeftOut(agent) {
			t.Errorf("%s is left out of a story with no staffing recorded", agent)
		}
	}

	r := staffedRecord("perf", "red-team")
	r.SetSecuritySensitive(false)
	for _, agent := range Agents {
		want := agent == "perf" || agent == "red-team"
		if got := r.LeftOut(agent); got != want {
			t.Errorf("LeftOut(%s) = %v, want %v", agent, got, want)
		}
	}
	if r.LeftOut("designer") {
		t.Error("an agent the staffing does not name is left out")
	}

	// A record that says what no command would have stored: edited by hand, or
	// written by something else. The floor is asked all the same.
	forged := staffedRecord(Agents...)
	forged.SetSecuritySensitive(false)
	for _, agent := range []string{"researcher", "sdet", "implementer", "bookkeeper", "verifier", "code-reviewer", "project-manager"} {
		if forged.LeftOut(agent) {
			t.Errorf("a record that leaves out %s is believed", agent)
		}
	}
	for _, agent := range []string{"architect", "red-team", "perf", "human-advocate", "security"} {
		if !forged.LeftOut(agent) {
			t.Errorf("%s is not left out of a story that is not sensitive, and the staffing says it is", agent)
		}
	}
}

// AC-4 and T3: the floor for security follows what the record says of the
// story now, and a record that says nothing reads as sensitive.
func TestAC4SecurityIsLeftOutOnlyWhileTheRecordSaysTheStoryIsNotSensitive(t *testing.T) {
	r := staffedRecord("security")
	if r.LeftOut("security") {
		t.Error("security is left out of a story whose analysis has said nothing, which reads as sensitive")
	}
	r.SetSecuritySensitive(false)
	if !r.LeftOut("security") {
		t.Error("security is not left out of a story recorded as not sensitive, and the staffing leaves it out")
	}
	r.SetSecuritySensitive(true)
	if r.LeftOut("security") {
		t.Error("security is left out of a story recorded as sensitive after the staffing was")
	}
}

// AC-3 and AC-7, for the hook: a reviewer has stood down when the staffing
// leaves it out and no changed path brought it back when code review last
// passed. Only then does its stale review hold nothing.
func TestAC7AReviewerALeftOutPathBroughtBackHasNotStoodDown(t *testing.T) {
	r := staffedRecord("perf", "security", "human-advocate")
	r.SetSecuritySensitive(false)
	r.Staffing.BroughtBack = map[string]string{"security": "internal/hook/extra.go"}

	for role, want := range map[string]bool{
		"perf":           true,  // left out, not brought back
		"human-advocate": true,  // the same
		"security":       false, // left out, and a path brought it back
		"code-reviewer":  false, // never left out
		"architect":      false, // needed
	} {
		if got := r.StoodDown(role); got != want {
			t.Errorf("StoodDown(%s) = %v, want %v", role, got, want)
		}
	}
	// Brought back by a path and then needed again by the floor: still in.
	r.SetSecuritySensitive(true)
	if r.StoodDown("security") {
		t.Error("security has stood down on a story recorded as sensitive")
	}
	// No staffing: nobody has stood down, and nothing is read from one.
	if NewRecord("A-1", time.Now()).StoodDown("perf") {
		t.Error("perf has stood down on a story with no staffing recorded")
	}
}
