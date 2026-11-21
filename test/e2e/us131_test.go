package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

// US-131 AC-11. Through the real binary and the plugin's launcher, in a story's
// worktree of a real git repository: the runbook records a decision, which `sdlc
// log` lists and `sdlc status` counts until a person's `sdlc ack`; each malformed
// `sdlc decide` of AC-4 is refused with SDLC-E0094 and records nothing; `sdlc
// escalate product_decision` is taken (the settled IQ-1: escalate on the story
// under way, its arguments unchanged); and an arbiter's review stands in place of
// the reviewer's block at the design review. Every command is put to the hook
// first. The decision's words hold no character the hook's reading of the shell
// line would take for more than a word.

// dc11Unread is decisions_unread from `sdlc status --json` in dir.
func dc11Unread(t *testing.T, sb sandbox, dir string) int {
	t.Helper()
	stdout, stderr, code := sb.outcome(t, sandboxSession, dir, "", "status", "--json")
	if code != 0 {
		t.Fatalf("sdlc status --json exited %d:\n%s%s", code, stdout, stderr)
	}
	var status struct {
		DecisionsUnread *int `json:"decisions_unread"`
	}
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		t.Fatalf("sdlc status --json printed something that is not JSON: %v\n%s", err, stdout)
	}
	if status.DecisionsUnread == nil {
		t.Fatalf("AC-11 sdlc status --json has no decisions_unread:\n%s", stdout)
	}
	return *status.DecisionsUnread
}

// dc11Decisions is the story's DECISIONS.md in the main checkout, "" where there is none.
func dc11Decisions(t *testing.T, sb sandbox) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(sb.repo, ".sdlc", "stories", "US-001", "DECISIONS.md"))
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestAC11ADecisionIsRecordedListedAndCountedUntilAckAndAnArbitersReviewStands(t *testing.T) {
	parallel(t)
	sb := newSandbox(t)
	main, tree := sb.repo, sb.tree()
	sb.works(t, main, "", "", "start")

	const question, chosen = "Which total does AC-1 mean", "the net total"
	decide := []string{"decide", "US-001", "--question", question, "--option", "the gross total", "--option", chosen,
		"--chosen", chosen, "--reason", "AC-1 says what the customer pays", "--by", "researcher"}

	t.Run("AC-11 each malformed decision is refused and records nothing", func(t *testing.T) {
		before, _ := rpStored(t, sb)
		for name, args := range map[string][]string{
			"no question":       {"decide", "US-001", "--option", "a", "--option", "b", "--chosen", "a", "--reason", "r", "--by", "researcher"},
			"nothing chosen":    {"decide", "US-001", "--question", "q", "--option", "a", "--option", "b", "--reason", "r", "--by", "researcher"},
			"no reason":         {"decide", "US-001", "--question", "q", "--option", "a", "--option", "b", "--chosen", "a", "--by", "researcher"},
			"one option":        {"decide", "US-001", "--question", "q", "--option", "a", "--chosen", "a", "--reason", "r", "--by", "researcher"},
			"a choice not made": {"decide", "US-001", "--question", "q", "--option", "a", "--option", "b", "--chosen", "c", "--reason", "r", "--by", "researcher"},
		} {
			r := sb.does(t, tree, "", "", args...)
			if r.code == 0 || !strings.Contains(r.stderr, "SDLC-E0094") {
				t.Errorf("AC-11 %s: exit %d, want a refusal with SDLC-E0094\n%s%s", name, r.code, r.stdout, r.stderr)
			}
			if after, _ := rpStored(t, sb); after != before {
				t.Errorf("AC-11 %s was refused and changed the record", name)
			}
		}
		if doc := dc11Decisions(t, sb); doc != "" {
			t.Errorf("AC-11 a refused decision wrote DECISIONS.md:\n%s", doc)
		}
	})

	out := sb.works(t, tree, "", "", decide...)
	if strings.Count(out, "\n") != 1 {
		t.Errorf("AC-11 decide printed more than one line:\n%s", out)
	}
	if doc := dc11Decisions(t, sb); !strings.Contains(doc, question) || !strings.Contains(doc, chosen) {
		t.Errorf("AC-11 DECISIONS.md does not hold the decision:\n%s", doc)
	}
	if n := dc11Unread(t, sb, tree); n != 1 {
		t.Errorf("AC-11 decisions_unread is %d after one decision, want 1", n)
	}

	t.Run("AC-11 escalate product_decision is taken", func(t *testing.T) {
		r := sb.does(t, tree, "", "", "escalate", "product_decision", "--message",
			"Q1 who may refund. A the owner B any admin. I recommend A")
		if r.code != 0 || !strings.Contains(r.stdout, "waiting for a person: product_decision") {
			t.Fatalf("AC-11 escalate product_decision: exit %d\n%s%s", r.code, r.stdout, r.stderr)
		}
		if got := storiesOf(t, main); got["US-001"] != "awaiting_human" {
			t.Errorf("AC-11 the story is %q after the escalation, want awaiting_human", got["US-001"])
		}
		if r := sb.person(t, tree, "approve", "US-001"); r.code != 0 {
			t.Fatalf("AC-11 approve, by the harness: exit %d\n%s%s", r.code, r.stdout, r.stderr)
		}
		sb.works(t, tree, "", "", "start", "US-001")
	})

	for gate := sb.nextGate(t); gate != string(model.GateDesignReview); gate = sb.nextGate(t) {
		if gate == "" {
			t.Fatal("the story passed its last gate without stopping at the design review")
		}
		sb.worksGate(t, model.Gate(gate))
	}

	t.Run("AC-11 an arbiter's review stands in place of the reviewer's block", func(t *testing.T) {
		block := []string{"review", "add", "design_review", "architect", "block", "--important", "1",
			"--finding", "important the plan has no step for AC-1", "--note", "AC-1 has no step"}
		sb.works(t, tree, "architect", "# architect\n\nAC-1 has no step.\n", block...)
		second := sb.works(t, tree, "architect", "# architect\n\nAC-1 still has no step.\n", append(block, "--json")...)
		var said struct {
			HandedOver string `json:"handed_over"`
			Arbiter    string `json:"arbiter"`
		}
		if err := json.Unmarshal([]byte(second), &said); err != nil {
			t.Fatalf("review add --json printed something that is not JSON: %v\n%s", err, second)
		}
		if said.HandedOver != "" || said.Arbiter != "architect-arbiter" {
			t.Fatalf("AC-11 the architect's second block: %s, want the gate sent to architect-arbiter", second)
		}
		// The arbiter declines the finding each block raised with a reason, records
		// the ruling, and approves.
		for _, id := range []string{"F-1", "F-2"} {
			sb.works(t, tree, "architect", "", "finding", "close", "design_review", "architect", id, "--declined",
				"--reason", "step 2 of the plan covers AC-1")
		}
		sb.works(t, tree, "", "", "decide", "US-001", "--question", "Does the plan need a step for AC-1",
			"--option", "add one", "--option", "decline F-1", "--chosen", "decline F-1",
			"--reason", "step 2 of the plan covers AC-1", "--by", "architect-arbiter")
		sb.works(t, tree, "architect", "# architect arbiter\n\nStep 2 covers AC-1.\n",
			"review", "add", "design_review", "architect-arbiter", "approve")
		for _, r := range model.ReviewersFor(model.GateDesignReview) {
			if r.Role != "architect" {
				sb.works(t, tree, r.Role, "# "+r.Role+"\n", "review", "add", "design_review", r.Role, "approve")
			}
		}
		sb.works(t, tree, "", "", "gate", "design_review", "pass", "--note", "the arbiter ruled")
	})

	sb.worksEveryGate(t)
	sb.works(t, tree, "", "", "stop")

	t.Run("AC-11 the log lists the decision and status counts it until the ack", func(t *testing.T) {
		stdout, stderr, code := sb.outcome(t, sandboxSession, main, "", "log")
		if code != 0 || !strings.Contains(stdout, question) || !strings.Contains(stdout, chosen) {
			t.Errorf("AC-11 sdlc log does not list the decision: exit %d\n%s%s", code, stdout, stderr)
		}
		if n := dc11Unread(t, sb, main); n != 2 {
			t.Errorf("AC-11 decisions_unread is %d once the story landed, want 2", n)
		}
		if r := sb.person(t, main, "ack", "--through", "HEAD"); r.code != 0 {
			t.Fatalf("AC-11 ack, by the harness: exit %d\n%s%s", r.code, r.stdout, r.stderr)
		}
		if n := dc11Unread(t, sb, main); n != 0 {
			t.Errorf("AC-11 decisions_unread is %d after the person's ack, want 0", n)
		}
	})
}
