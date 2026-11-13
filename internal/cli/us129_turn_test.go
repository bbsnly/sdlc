package cli

// US-129 AC-4, for the landing's turn: a stop that waits for another landing
// asks trunk's tip, the worktrees and the landing's facts again once its turn
// comes, because the landing in front moves trunk while it waits. A stop that
// reused what it asked before the wait would hand update-ref a stale old
// value and refuse a landing that should go ahead (US-129 security review,
// F-2). TestAC13AStopWaitsForTheLandingInFrontOfItAndThenLands moves trunk
// before the stop starts; this one moves it during the wait. It passes at the
// base, which keeps no answer.

import (
	"testing"

	"github.com/bbsnly/sdlc/internal/model"
)

func TestAC4AStopWhoseTurnCameAfterTrunkMovedDuringItsWaitLandsOnTheMovedTrunk(t *testing.T) {
	t.Parallel()
	main, wt, _ := otWalked(t, ldWalk{finished: true, bookkept: true})
	before := ldTrunk(t, main)
	inFront := otLandingInFront(t, main)

	stop := otStopBeside(wt, "--json")
	stop.waits(t)
	moved := ldMoveTrunk(t, main)
	if moved == before {
		t.Fatal("trunk did not move")
	}

	inFront.Release()
	r := stop.ended(t)
	if r.code != 0 {
		t.Fatalf("AC-4 the stop exited %d once its turn came, after trunk moved while it waited:\n%s%s", r.code, r.stdout, r.stderr)
	}
	if got := decode[stopPayload](t, r); !got.OK || got.Story != "US-001" || !got.Done {
		t.Errorf("AC-4 stop --json = %+v, want US-001 done", got)
	}
	landed := ldTrunk(t, main)
	if _, ok := swGitTry(t, main, "merge-base", "--is-ancestor", moved, landed); !ok || landed == moved {
		t.Errorf("AC-4 trunk is at %s after the stop, want the story landed on top of %s, where trunk moved during the wait", landed, moved)
	}
	if got := wtStatuses(t, main)["US-001"]; got != string(model.StatusDone) {
		t.Errorf("AC-4 US-001 is %s after it landed, want done", got)
	}
}
