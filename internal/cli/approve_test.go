package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// A question the loop stops to ask is worth nothing if the loop can carry on
// past it, so a story handed to a person stays with them until they answer.
func TestAnEscalatedStoryWaitsForAPersonToAnswer(t *testing.T) {
	gitProject(t)
	initialised(t)

	if r := run(t, "escalate", "spec_unclear", "--message", "which one?"); !strings.Contains(r.stderr, "no story being worked on") {
		t.Errorf("an escalation with no iteration running was not refused:\n%s", r.stderr)
	}
	mustRun(t, "start")
	if r := run(t, "approve", "US-001"); !strings.Contains(r.stderr, "SDLC-E0035") {
		t.Errorf("an answer to a question nobody asked was recorded:\n%s", r.stderr)
	}
	if r := run(t, "escalate", "spec_unclear"); r.code == 0 {
		t.Fatal("an escalation with no question in it was recorded")
	}

	const question = "AC-2 contradicts AC-3"
	escalated := decode[escalatePayload](t, mustRun(t, "escalate", "spec_unclear", "--message", question, "--json"))
	if escalated.Story != "US-001" || escalated.Tree == "" {
		t.Errorf("escalate = %+v", escalated)
	}

	status := decode[statusPayload](t, mustRun(t, "status", "--json"))
	if status.Active != "" {
		t.Error("the iteration is still running after the story was handed over")
	}
	if len(status.Waiting) != 1 || status.Waiting[0].Story != "US-001" || status.Waiting[0].Message != question {
		t.Errorf("waiting = %+v", status.Waiting)
	}
	if status.Next != nil {
		t.Errorf("status offers %+v to start, and start refuses it", status.Next)
	}
	if status.Backlog["awaiting_human"] != 1 {
		t.Errorf("backlog = %v", status.Backlog)
	}
	if text := mustRun(t, "status"); !strings.Contains(text.stdout, "sdlc approve US-001") {
		t.Errorf("status does not say who has to do what:\n%s", text.stdout)
	}

	for _, args := range [][]string{{"start"}, {"start", "US-001"}} {
		r := run(t, args...)
		if r.code == 0 {
			t.Fatalf("sdlc %s started a story that is waiting for a person", strings.Join(args, " "))
		}
		if !strings.Contains(r.stderr, "SDLC-E0036") || !strings.Contains(r.stderr, question) {
			t.Errorf("sdlc %s:\n%s", strings.Join(args, " "), r.stderr)
		}
	}

	answer := decode[approvePayload](t, mustRun(t, "approve", "US-001", "--json"))
	if answer.Decision != "approved" || answer.Type != "spec_unclear" || answer.Tree == "" {
		t.Errorf("approve = %+v", answer)
	}
	// Answered, the story is back to work, not still waiting in the backlog.
	if story := onlyStory(t, decode[storyListPayload](t, mustRun(t, "story", "list", "--json"))); story.Status != string(model.StatusInProgress) {
		t.Errorf("%s = %q after a person answered", story.ID, story.Status)
	}
	if r := run(t, "approve", "US-001"); !strings.Contains(r.stderr, "SDLC-E0035") {
		t.Errorf("one question was answered twice:\n%s", r.stderr)
	}
	again := decode[startPayload](t, mustRun(t, "start", "--json"))
	if again.Story != "US-001" || !again.Resume {
		t.Errorf("start after the answer = %+v", again)
	}
}

// human_gates.pre_commit_pause_tiers was written into every configuration and
// read by nothing, so a high-risk story went to trunk with nobody asked.
func TestAStoryInAPausedTierIsCommittedOnlyOnceAPersonApprovesIt(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"PAY-1","title":"Refunds","status":"ready","risk_tier":"High","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN a paid invoice is refunded, the money goes back"}]}]}`)
	mustRun(t, "start")
	reach(t, root, model.GateCommit)

	commitEverything(t, root)
	r := run(t, "gate", "commit", "pass", "--note", "on trunk")
	if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0037") {
		t.Fatalf("a high-risk story was committed with nobody asked:\n%s", r.stderr)
	}

	mustRun(t, "escalate", "pre_commit_approval", "--message", "refunds are ready")
	mustRun(t, "approve", "PAY-1", "--reject", "refunds skip the audit log")
	mustRun(t, "start")
	commitEverything(t, root)
	if r := run(t, "gate", "commit", "pass"); !strings.Contains(r.stderr, "refunds skip the audit log") {
		t.Errorf("work a person sent back was committed:\n%s", r.stderr)
	}

	// Asked again about the same work, the latest answer is the one that counts.
	mustRun(t, "escalate", "pre_commit_approval", "--message", "the audit log is out of scope")
	mustRun(t, "approve", "PAY-1")
	mustRun(t, "start")
	commitEverything(t, root)
	if r := run(t, "gate", "commit", "pass", "--note", "on trunk"); r.code != 0 {
		t.Fatalf("the work a person approved was refused:\n%s", r.stderr)
	}
}

// And a story in a tier that is not paused is committed as it always was.
func TestAStoryInATierThatIsNotPausedIsNotHeld(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	setConfigOnDisk(t, root, "human_gates", map[string]any{"pre_commit_pause_tiers": []string{}})
	writeFile(t, root, "user_stories.json", `{"stories":[
	  {"id":"PAY-1","title":"Refunds","status":"ready","risk_tier":"high","priority":1,
	   "acceptance_criteria":[{"id":"AC-1","text":"WHEN a paid invoice is refunded, the money goes back"}]}]}`)
	mustRun(t, "start")
	reach(t, root, model.GateCommit)
	commitEverything(t, root)

	if r := run(t, "gate", "commit", "pass", "--note", "on trunk"); r.code != 0 {
		t.Fatalf("a project that pauses no tier held the commit:\n%s", r.stderr)
	}
}

// Sending the work back tells whoever picks it up what to change, so the reason
// is not optional, and it goes on the record where the next session reads it.
func TestARejectionNeedsAReasonAndRecordsIt(t *testing.T) {
	root := gitProject(t)
	initialised(t)
	mustRun(t, "start")
	mustRun(t, "escalate", "pre_commit_approval", "--message", "ready to commit?")

	if r := run(t, "approve", "--reject", "the migration has no way back"); !strings.Contains(r.stderr, "no story being worked on") {
		t.Errorf("an answer naming no story, with nothing running, was not refused:\n%s", r.stderr)
	}
	if r := run(t, "approve", "US-001", "--reject", " "); r.code == 0 {
		t.Fatal("a rejection with no reason was recorded")
	}

	const why = "the migration has no way back"
	mustRun(t, "approve", "US-001", "--reject", why)
	record, err := os.ReadFile(filepath.Join(root, ".sdlc", "stories", "US-001", "gate-record.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{why, `"rejected"`, "pre_commit_approval"} {
		if !strings.Contains(string(record), want) {
			t.Errorf("the record is missing %s:\n%s", want, record)
		}
	}
}
