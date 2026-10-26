package hook

// US-120, the third unfreeze: the verifier's round-2 block, asked of the hook
// on a real disk where the story's record is there to be named.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// AC-3: after a `cd` the walk cannot place, removing a story's record by its
// id alone is refused as loop state, as it is when the walk is placed. The
// command could be in .sdlc/stories, where `rm -rf A-1` takes the story's gate
// history (THREATS T5); heldFrom names the directories the fixed held paths
// run through and leaves .sdlc/stories out, so after `cd -` the id alone is
// let through. A name that is no story here stays allowed, as the released
// `cd "$D" && rm -rf build` does.
func TestAC3ThroughTheHookAStoryRecordRemovedByItsIdAfterAnUnplacedCdIsRefused(t *testing.T) {
	root, home, base, payload, side := u120World(t)
	write(t, root, ".sdlc/stories/A-1/gate-record.json", "{}\n")
	write(t, root, ".sdlc/stories/A-1/ANALYSIS.md", "x\n")
	// Placed, the same removals are refused today: the unplaced rows must
	// reach the same rule.
	for _, row := range []string{
		"rm -rf .sdlc/stories/A-1",
		"rm -f .sdlc/stories/A-1/gate-record.json",
		"cd .sdlc/stories && rm -rf A-1",
	} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-3 placed "+row, func(t *testing.T) { u120Refused(t, "AC-3", root, cmd, noEnv, u120LoopState) })
	}
	for _, row := range []string{
		"cd - && rm -rf A-1",
		"cd {side} && cd - && rm -rf A-1",
		"cd - && rm -f A-1/gate-record.json",
		`cd "$D" && rm -rf A-1`,
	} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-3 "+row, func(t *testing.T) { u120Refused(t, "AC-3", root, cmd, noEnv, u120LoopState) })
	}
	for _, row := range []string{"cd - && rm -rf build", `cd "$D" && rm -rf build`} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-3 "+row, func(t *testing.T) { u120Allowed(t, "AC-3", root, cmd) })
	}
}

// US-120, the fourth unfreeze: the verifier's round-3 gap 1, the branch of
// storyRecords that no frozen row held.

// AC-3 (verification round 3, gap 1): through the hook, where .sdlc/stories
// holds more names than the reader lists for one command, the listing is cut
// short and any first name could be a story's, so after `cd -` a removal of
// build is refused as loop state, which the complete listing of the test
// above lets through. The same removal placed stays allowed. Where the
// checkout that keeps the records cannot be found, the listing failed, and
// is short in the same way.
func TestAC3ThroughTheHookARemovalAfterAnUnplacedCdIsRefusedWhereTheStoriesListingIsCutShort(t *testing.T) {
	root, home, base, payload, side := u120World(t)
	write(t, root, ".sdlc/stories/A-1/gate-record.json", "{}\n")
	stories := filepath.Join(root, ".sdlc", "stories")
	for i := range maxListed {
		if err := os.WriteFile(filepath.Join(stories, fmt.Sprintf("S-%05d", i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, row := range []string{"cd - && rm -rf build", `cd "$D" && rm -rf build`, "cd {side} && cd - && rm -rf build"} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-3 "+row, func(t *testing.T) { u120Refused(t, "AC-3", root, cmd, noEnv, u120LoopState) })
	}
	for _, row := range []string{"rm -rf build", "cd {side} && rm -rf build"} {
		cmd := u120Expand(row, root, home, base, payload, side)
		t.Run("AC-3 placed "+row, func(t *testing.T) { u120Allowed(t, "AC-3", root, cmd) })
	}
	t.Run("AC-3 records not found", func(t *testing.T) {
		got := newDiskReader(root).stories(func() (string, bool) { return "", false })()
		u120Read(t, "AC-3", "the stories of no checkout", got, nil, false, false, listingFailed)
	})
}
