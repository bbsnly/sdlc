package cli

// US-112, design review round 5, red team V18: a story still under way has no
// first-pass verdict while nothing has blocked it, and has lost it once a
// review of the code has blocked, end or no end. Round 4's null for a story
// under way is not a null for every story under way.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/bbsnly/sdlc/internal/model"
)

// underWayBlocked is a story that passed the plan and the code, had its code
// review block, and has not reached the commit gate.
func underWayBlocked(id string) *gateRecord {
	g := newGateRecord(id)
	for i, gate := range []model.Gate{model.GateDoR, model.GateAnalysis, model.GateTestsFrozen, model.GatePlan,
		model.GateDesignReview, model.GateImplementation, model.GateVerification, model.GateVerifierReview} {
		g.pass(gate, time.Duration(i+1)*time.Minute)
	}
	return g.review(model.GateCodeReview, "code-reviewer", model.VerdictBlock, 1, 20*time.Minute)
}

// AC-19, AC-20, V18: in --all --json, false and not null; in the text, "no"
// and not "not yet".
func TestAC19AC20AStoryUnderWayWithABlockAlreadyHasNoFirstPassSuccess(t *testing.T) {
	t.Parallel()
	root := gitProjectInitialised(t)
	traceStories(t, root, "T-2", "T-7", "T-9")
	walkedStory("T-2", 5*time.Hour, "").save(t, root)
	newGateRecord("T-7").pass(model.GateDoR, time.Minute).pass(model.GateAnalysis, 2*time.Minute).save(t, root)
	underWayBlocked("T-9").save(t, root)

	var doc struct {
		Stories []map[string]json.RawMessage `json:"stories"`
	}
	if err := json.Unmarshal([]byte(mustRun(t, "trace", "--all", "--json").stdout), &doc); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range doc.Stories {
		var id string
		_ = json.Unmarshal(s["story"], &id)
		got[id] = string(s["first_pass"]) + " end " + string(s["end"])
	}
	for id, want := range map[string]string{"T-9": "false end null", "T-7": "null end null"} {
		if got[id] != want {
			t.Errorf("AC-19 %s: first_pass and end = %q, want %q", id, got[id], want)
		}
	}

	text := mustRun(t, "trace", "--all").stdout
	var row string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "T-9 ") {
			row = line
		}
	}
	fields := strings.Fields(row)
	switch {
	case row == "":
		t.Fatalf("AC-19 no row of sdlc trace --all is T-9's:\n%s", text)
	case !strings.Contains(row, "in progress"):
		t.Errorf("AC-19 T-9's row does not say it is in progress: %q", row)
	case fields[len(fields)-1] != "no" || strings.HasSuffix(row, "not yet"):
		t.Errorf("AC-19 T-9's row does not end in \"no\" under first pass, with its code review blocked: %q", row)
	}
}
