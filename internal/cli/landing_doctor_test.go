package cli

// US-029.3 (PLAN.md step 14, advocate 4): where trunk tracks files under the
// loop's state directory, a worktree story cannot land (L5 refuses them), and
// doctor says so before a story starts, naming what trunk tracks and how to
// stop tracking it. Where trunk tracks none, doctor says nothing of it.

import (
	"strings"
	"testing"
)

func TestDoctorWarnsBeforeAStoryStartsWhereTrunkTracksTheLoopsState(t *testing.T) {
	tracking := ldRepository(t, func(root string) {
		writeFile(t, root, ".gitignore", "")
		writeFile(t, root, ".sdlc/state/tracked.json", "{}\n")
	})
	r := wtRun(t, tracking, "", "", "doctor")
	out := r.stdout + r.stderr
	if !wtNames(out, ".sdlc/state/tracked.json") && !strings.Contains(out, ".sdlc/state") {
		t.Errorf("doctor does not say that trunk tracks the loop's state:\n%s", out)
	}
	suSays(t, out, "land")

	clean := ldRepository(t, nil)
	r = wtRun(t, clean, "", "", "doctor")
	if strings.Contains(r.stdout+r.stderr, "tracked.json") {
		t.Errorf("doctor names a tracked state file in a repository that has none:\n%s", r.stdout+r.stderr)
	}
}
